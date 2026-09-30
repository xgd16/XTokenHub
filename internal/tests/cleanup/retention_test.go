// Package cleanup_test 保留期清理任务测试：真实 :memory: SQLite + 阻塞仓储模拟并发。
package cleanup_test

import (
	"context"
	"testing"
	"time"

	"xtokenhub/internal/cleanup"
	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/pagination"
	"xtokenhub/internal/repository"
	"xtokenhub/internal/tests/testutil"
)

func seedLogs(t *testing.T, repo repository.RequestLogRepository, logs []model.RequestLog) {
	t.Helper()
	for i := range logs {
		if err := repo.Create(context.Background(), &logs[i]); err != nil {
			t.Fatal(err)
		}
	}
}

// staticSettings 固定配置的设置源（每轮执行前读取，改完即生效的注入点）。
func staticSettings(st cleanup.Settings) cleanup.SettingsProvider {
	return func(context.Context) (cleanup.Settings, error) { return st, nil }
}

// intervalSettings interval 模式常用配置。
func intervalSettings(maxDays, batch int, vacuum bool) cleanup.Settings {
	return cleanup.Settings{
		Enabled: true, Mode: cleanup.ModeInterval, IntervalHours: 24,
		MaxDays: maxDays, BatchSize: batch, Vacuum: vacuum,
	}
}

// TestRetentionCleanupOnce 清理只删超期行，Result 计数正确，状态快照可读。
func TestRetentionCleanupOnce(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	repo := repository.NewRequestLogRepository(db)
	ctx := context.Background()

	now := time.Now()
	seedLogs(t, repo, []model.RequestLog{
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, CreatedAt: now.AddDate(0, 0, -40)},
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, CreatedAt: now.AddDate(0, 0, -30)},
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, CreatedAt: now.Add(-time.Hour)},
	})

	ret := cleanup.NewRetention(repo, db, staticSettings(intervalSettings(30, 1000, false)), nil)
	res, err := ret.CleanupOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.DeletedRows != 2 {
		t.Fatalf("deleted = %d, want 2", res.DeletedRows)
	}
	if res.Cutoff.IsZero() || res.DurationMS < 0 {
		t.Errorf("结果字段不完整: %+v", res)
	}

	status, err := ret.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Enabled || status.MaxDays != 30 || status.LastRunAt == nil || status.LastDeleted != 2 {
		t.Fatalf("状态快照异常: %+v", status)
	}
	// 仅剩保留期内 1 行
	_, total, err := repo.List(ctx, repository.LogFilter{}, pagination.Normalize(1, 10))
	if err != nil || total != 1 {
		t.Fatalf("remaining = %d, err = %v, want 1", total, err)
	}
}

// TestRetentionCleanupNothing 无超期行时正常返回、删除数为 0。
func TestRetentionCleanupNothing(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	repo := repository.NewRequestLogRepository(db)
	ctx := context.Background()

	now := time.Now()
	seedLogs(t, repo, []model.RequestLog{
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, CreatedAt: now.Add(-time.Hour)},
	})

	ret := cleanup.NewRetention(repo, db, staticSettings(intervalSettings(30, 1000, true)), nil)
	res, err := ret.CleanupOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.DeletedRows != 0 {
		t.Fatalf("deleted = %d, want 0", res.DeletedRows)
	}
}

// blockRepo 嵌入接口满足 RequestLogRepository，仅重写 DeleteBefore 阻塞语义。
type blockRepo struct {
	repository.RequestLogRepository
	started chan struct{}
	release chan struct{}
}

func (b *blockRepo) DeleteBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	close(b.started)
	<-b.release
	return 0, nil
}

// TestRetentionTriggerConcurrent 清理运行中再次触发返回业务错误。
func TestRetentionTriggerConcurrent(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	repo := repository.NewRequestLogRepository(db)
	ctx := context.Background()

	br := &blockRepo{RequestLogRepository: repo, started: make(chan struct{}), release: make(chan struct{})}
	ret := cleanup.NewRetention(br, db, staticSettings(intervalSettings(30, 10, false)), nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = ret.Trigger(ctx)
	}()
	<-br.started // 确保首个 Trigger 已持有执行锁

	if _, err := ret.Trigger(ctx); err == nil {
		t.Fatal("并发 Trigger 未返回错误")
	} else if e := errs.From(err); e.Code != errs.CodeInvalidParams {
		t.Fatalf("并发 Trigger 错误码 = %d, want %d", e.Code, errs.CodeInvalidParams)
	}

	close(br.release)
	<-done
}

// TestRetentionTrimHeaders 请求头瘦身：只置空早于瘦身线（7 天）的行，
// 保留线内的请求头不动；超保留线的行整行删除。
func TestRetentionTrimHeaders(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	repo := repository.NewRequestLogRepository(db)
	ctx := context.Background()

	now := time.Now()
	seedLogs(t, repo, []model.RequestLog{
		// 40 天前：超保留线（30 天）→ 整行删除
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, RequestHeaders: `{"gone":1}`, CreatedAt: now.AddDate(0, 0, -40)},
		// 20 天前：保留期内、超瘦身线 → 置空请求头
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, RequestHeaders: `{"x":"old"}`, CreatedAt: now.AddDate(0, 0, -20)},
		// 1 小时前：瘦身线内 → 保留请求头
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, RequestHeaders: `{"x":"keep"}`, CreatedAt: now.Add(-time.Hour)},
	})

	st := intervalSettings(30, 1000, false)
	st.TrimHeaders = true
	st.TrimHeadersAfterDays = 7
	ret := cleanup.NewRetention(repo, db, staticSettings(st), nil)
	res, err := ret.CleanupOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.DeletedRows != 1 || res.TrimmedRows != 1 {
		t.Fatalf("deleted=%d trimmed=%d, want 1/1", res.DeletedRows, res.TrimmedRows)
	}

	items, _, err := repo.List(ctx, repository.LogFilter{}, pagination.Normalize(1, 10))
	if err != nil || len(items) != 2 {
		t.Fatalf("remaining = %d, err = %v, want 2", len(items), err)
	}
	for _, it := range items {
		if it.CreatedAt.Before(now.AddDate(0, 0, -7)) && it.RequestHeaders != "" {
			t.Errorf("瘦身线外的请求头未被置空: %s", it.RequestHeaders)
		}
		if it.CreatedAt.After(now.Add(-7*24*time.Hour)) && it.RequestHeaders != `{"x":"keep"}` {
			t.Errorf("瘦身线内的请求头被误置空: %s", it.RequestHeaders)
		}
	}
}

// TestRetentionPersistResult 结果通过 onFinish 持久化（锁内同步调用，重启不丢）。
func TestRetentionPersistResult(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	repo := repository.NewRequestLogRepository(db)
	ctx := context.Background()

	now := time.Now()
	seedLogs(t, repo, []model.RequestLog{
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, CreatedAt: now.AddDate(0, 0, -40)},
	})

	var got *cleanup.Result
	ret := cleanup.NewRetention(repo, db, staticSettings(intervalSettings(30, 1000, false)),
		func(_ context.Context, res *cleanup.Result) { got = res })
	if _, err := ret.CleanupOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got == nil || got.DeletedRows != 1 || got.DoneAt.IsZero() {
		t.Fatalf("onFinish 未收到完整结果: %+v", got)
	}
}

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

	ret := cleanup.NewRetention(repo, db, cleanup.Options{Enabled: true, MaxDays: 30, IntervalHrs: 24, BatchSize: 1000})
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

	status := ret.Status()
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

	ret := cleanup.NewRetention(repo, db, cleanup.Options{MaxDays: 30, IntervalHrs: 24, BatchSize: 1000, Vacuum: true})
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
	ret := cleanup.NewRetention(br, db, cleanup.Options{MaxDays: 30, IntervalHrs: 24, BatchSize: 10})

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

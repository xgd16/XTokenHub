package repository_test

import (
	"encoding/json"
	"xtokenhub/internal/repository"

	"context"
	"testing"
	"time"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/pagination"
)

func seedLogs(t *testing.T, repo repository.RequestLogRepository, logs []model.RequestLog) {
	t.Helper()
	for i := range logs {
		if err := repo.Create(context.Background(), &logs[i]); err != nil {
			t.Fatal(err)
		}
	}
}

func boolPtr(b bool) *bool { return &b }

func TestRequestLogListFilters(t *testing.T) {
	db := NewTestDB(t)
	repo := repository.NewRequestLogRepository(db)
	ctx := context.Background()

	now := time.Now()
	seedLogs(t, repo, []model.RequestLog{
		{Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, ChannelID: 1, ChannelName: "ch1", Model: "gpt-4o", PromptTokens: 100, CompletionTokens: 50, CachedTokens: 40, DurationMS: 200, CreatedAt: now.Add(-2 * time.Hour)},
		{Protocol: model.ProtocolMessages, ForwardMode: model.ForwardConverted, ChannelID: 2, ChannelName: "ch2", Model: "claude-3", PromptTokens: 200, CompletionTokens: 80, CachedTokens: 0, DurationMS: 400, CreatedAt: now.Add(-1 * time.Hour)},
		{Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, ChannelID: 1, ChannelName: "ch1", Model: "gpt-4o", Error: "upstream 500", DurationMS: 50, CreatedAt: now},
	})

	// 全量
	_, total, err := repo.List(ctx, repository.LogFilter{}, pagination.Normalize(1, 10))
	if err != nil || total != 3 {
		t.Fatalf("全量: %v total=%d", err, total)
	}

	// 按 protocol
	_, total, _ = repo.List(ctx, repository.LogFilter{Protocol: model.ProtocolMessages}, pagination.Normalize(1, 10))
	if total != 1 {
		t.Errorf("protocol 过滤 total=%d, want 1", total)
	}

	// 按 forward_mode
	_, total, _ = repo.List(ctx, repository.LogFilter{ForwardMode: model.ForwardNativePassthrough}, pagination.Normalize(1, 10))
	if total != 2 {
		t.Errorf("forward_mode 过滤 total=%d, want 2", total)
	}

	// 按 model
	_, total, _ = repo.List(ctx, repository.LogFilter{Model: "gpt-4o"}, pagination.Normalize(1, 10))
	if total != 2 {
		t.Errorf("model 过滤 total=%d, want 2", total)
	}

	// stream
	_, total, _ = repo.List(ctx, repository.LogFilter{Stream: boolPtr(false)}, pagination.Normalize(1, 10))
	if total != 3 {
		t.Errorf("stream 过滤 total=%d, want 3", total)
	}

	// 时间窗
	since := now.Add(-90 * time.Minute)
	_, total, _ = repo.List(ctx, repository.LogFilter{StartTime: &since}, pagination.Normalize(1, 10))
	if total != 2 {
		t.Errorf("时间过滤 total=%d, want 2", total)
	}

	// error only
	_, total, _ = repo.List(ctx, repository.LogFilter{ErrorOnly: true}, pagination.Normalize(1, 10))
	if total != 1 {
		t.Errorf("errorOnly total=%d, want 1", total)
	}

	// 分页
	items, total, _ := repo.List(ctx, repository.LogFilter{}, pagination.Normalize(1, 2))
	if total != 3 || len(items) != 2 {
		t.Fatalf("分页: total=%d len=%d", total, len(items))
	}
	if items[0].ID <= items[1].ID {
		t.Errorf("应按 id DESC 排序: %d %d", items[0].ID, items[1].ID)
	}
}

func TestRequestLogSummary(t *testing.T) {
	db := NewTestDB(t)
	repo := repository.NewRequestLogRepository(db)
	ctx := context.Background()
	now := time.Now()

	seedLogs(t, repo, []model.RequestLog{
		{Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, Model: "gpt-4o", PromptTokens: 100, CompletionTokens: 100, CachedTokens: 50, DurationMS: 100, CreatedAt: now},
		{Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardConverted, Model: "gpt-4o", PromptTokens: 100, CompletionTokens: 300, CachedTokens: 0, DurationMS: 300, CreatedAt: now},
		{Protocol: model.ProtocolMessages, ForwardMode: model.ForwardNativePassthrough, Model: "claude-3", PromptTokens: 200, CompletionTokens: 0, CachedTokens: 200, DurationMS: 200, CreatedAt: now, Error: "boom"},
	})

	s, err := repo.Summary(ctx, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalRequests != 3 || s.SuccessReqs != 2 || s.ErrorReqs != 1 {
		t.Errorf("请求计数错误: %+v", s)
	}
	// usage 只计成功请求：prompt=200, completion=400, cached=50
	if s.PromptTokens != 200 || s.CompletionTok != 400 || s.TotalTokens != 600 || s.CachedTokens != 50 {
		t.Errorf("token 汇总错误: %+v", s)
	}
	if s.CacheHitRate <= 0.24 || s.CacheHitRate >= 0.26 {
		t.Errorf("cache_hit_rate = %f, want 0.25", s.CacheHitRate)
	}
	if s.NativeRatio <= 0.65 || s.NativeRatio >= 0.67 {
		t.Errorf("native_ratio = %f, want 0.667", s.NativeRatio)
	}
	if s.AvgDurationMS != 200 {
		t.Errorf("avg_ms = %f, want 200", s.AvgDurationMS)
	}
}

func TestRequestLogTrendAndGroups(t *testing.T) {
	db := NewTestDB(t)
	repo := repository.NewRequestLogRepository(db)
	ctx := context.Background()
	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)

	seedLogs(t, repo, []model.RequestLog{
		{Model: "gpt-4o", ChannelName: "ch1", PromptTokens: 10, CompletionTokens: 5, CachedTokens: 5, DurationMS: 100, CreatedAt: now},
		{Model: "gpt-4o", ChannelName: "ch1", PromptTokens: 20, CompletionTokens: 5, CachedTokens: 0, DurationMS: 200, CreatedAt: now},
		{Model: "claude-3", ChannelName: "ch2", PromptTokens: 30, CompletionTokens: 10, CachedTokens: 10, DurationMS: 300, CreatedAt: yesterday},
	})

	trend, err := repo.TrendByDay(ctx, now.Add(-48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(trend) != 2 {
		t.Fatalf("trend len = %d, want 2", len(trend))
	}
	if trend[0].Requests != 1 || trend[1].Requests != 2 {
		t.Errorf("trend 顺序/计数错误: %+v", trend)
	}

	byModel, err := repo.GroupByModel(ctx, now.Add(-48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(byModel) != 2 || byModel[0].Name != "gpt-4o" || byModel[0].Requests != 2 {
		t.Errorf("GroupByModel: %+v", byModel)
	}
	if byModel[0].TotalTokens != 40 { // (10+5)+(20+5)
		t.Errorf("GroupByModel tokens = %d, want 40", byModel[0].TotalTokens)
	}

	byCh, err := repo.GroupByChannel(ctx, now.Add(-48*time.Hour))
	if err != nil || len(byCh) != 2 {
		t.Fatalf("GroupByChannel: %v %+v", err, byCh)
	}
}

// TestModelUsageWindows 多时间窗聚合：1h/24h/7d/30d 边界与窗口外数据排除。
func TestModelUsageWindows(t *testing.T) {
	db := NewTestDB(t)
	repo := repository.NewRequestLogRepository(db)
	ctx := context.Background()
	now := time.Now()

	seedLogs(t, repo, []model.RequestLog{
		{Model: "gpt-4o", PromptTokens: 10, CompletionTokens: 5, CachedTokens: 3, CreatedAt: now.Add(-30 * time.Minute)}, // 仅 1h 窗
		{Model: "gpt-4o", PromptTokens: 20, CompletionTokens: 5, CreatedAt: now.Add(-2 * time.Hour)},                     // 24h/7d/30d
		{Model: "claude-3", PromptTokens: 30, CompletionTokens: 10, CreatedAt: now.Add(-3 * 24 * time.Hour)},             // 7d/30d
		{Model: "deepseek", PromptTokens: 40, CompletionTokens: 20, CreatedAt: now.Add(-20 * 24 * time.Hour)},            // 仅 30d
		{Model: "too-old", PromptTokens: 99, CompletionTokens: 99, CreatedAt: now.Add(-40 * 24 * time.Hour)},             // 30d 之外
	})

	usage, err := repo.ModelUsage(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 3 { // too-old 不出现
		t.Fatalf("len = %d, want 3: %+v", len(usage), usage)
	}
	byModel := map[string]repository.ModelUsageStat{}
	for _, m := range usage {
		byModel[m.Model] = m
	}
	gpt := byModel["gpt-4o"]
	if gpt.W1h.Requests != 1 || gpt.W1h.TotalTokens != 15 || gpt.W1h.CachedToken != 3 {
		t.Errorf("gpt-4o 1h 窗错误: %+v", gpt.W1h)
	}
	if gpt.W24h.Requests != 2 || gpt.W24h.TotalTokens != 40 {
		t.Errorf("gpt-4o 24h 窗错误: %+v", gpt.W24h)
	}
	if gpt.W7d.Requests != 2 || gpt.W30d.Requests != 2 || gpt.W30d.TotalTokens != 40 {
		t.Errorf("gpt-4o 7d/30d 窗错误: %+v %+v", gpt.W7d, gpt.W30d)
	}
	claude := byModel["claude-3"]
	if claude.W1h.Requests != 0 || claude.W24h.Requests != 0 {
		t.Errorf("claude-3 近窗应为零: %+v %+v", claude.W1h, claude.W24h)
	}
	if claude.W7d.Requests != 1 || claude.W7d.TotalTokens != 40 || claude.W30d.Requests != 1 {
		t.Errorf("claude-3 7d/30d 窗错误: %+v %+v", claude.W7d, claude.W30d)
	}
	ds := byModel["deepseek"]
	if ds.W7d.Requests != 0 || ds.W30d.Requests != 1 || ds.W30d.TotalTokens != 60 {
		t.Errorf("deepseek 窗口错误: %+v %+v", ds.W7d, ds.W30d)
	}
	// 排序：30d token 降序，平序按名称升序（deepseek 60 > claude-3 = gpt-4o 40）
	if usage[0].Model != "deepseek" || usage[1].Model != "claude-3" || usage[2].Model != "gpt-4o" {
		t.Errorf("排序错误: %s %s %s", usage[0].Model, usage[1].Model, usage[2].Model)
	}
}

// 空表查询必须返回非 nil 空 slice —— JSON 序列化为 [] 而非 null（前端数组约定）。
func TestEmptyResultsMarshalAsEmptyArray(t *testing.T) {
	db := NewTestDB(t)
	chRepo := repository.NewChannelRepository(db)
	logRepo := repository.NewRequestLogRepository(db)
	ctx := context.Background()

	chs, _, err := chRepo.List(ctx, pagination.Normalize(1, 10))
	if err != nil {
		t.Fatal(err)
	}
	assertJSONArray(t, chs)

	enabled, err := chRepo.ListEnabled(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONArray(t, enabled)

	logs, _, err := logRepo.List(ctx, repository.LogFilter{}, pagination.Normalize(1, 10))
	if err != nil {
		t.Fatal(err)
	}
	assertJSONArray(t, logs)

	points, err := logRepo.TrendByDay(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	assertJSONArray(t, points)
}

func assertJSONArray(t *testing.T, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "[]" {
		t.Errorf("空集合应序列化为 []，实际 %s", b)
	}
}

// TestTrendByBucket 分桶趋势：同一小时的请求归入一个桶，Ts 为 epoch 对齐的桶起点。
func TestTrendByBucket(t *testing.T) {
	db := NewTestDB(t)
	repo := repository.NewRequestLogRepository(db)
	ctx := context.Background()
	now := time.Now()
	hourAgo := now.Add(-time.Hour)

	seedLogs(t, repo, []model.RequestLog{
		{Model: "gpt-4o", PromptTokens: 10, CompletionTokens: 5, CreatedAt: now},
		{Model: "gpt-4o", PromptTokens: 20, CompletionTokens: 5, CreatedAt: now.Add(-2 * time.Minute)},
		{Model: "claude-3", PromptTokens: 30, CompletionTokens: 10, CreatedAt: hourAgo},
	})

	points, err := repo.TrendByBucket(ctx, now.Add(-3*time.Hour), 3600)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 {
		t.Fatalf("bucket len = %d, want 2", len(points))
	}
	// 按时间升序：1 小时前桶 1 条，当前桶 2 条
	if points[0].Requests != 1 || points[1].Requests != 2 {
		t.Errorf("bucket 计数错误: %+v", points)
	}
	// Ts 为 epoch 对齐整点（3600 整除）
	for _, p := range points {
		if p.Ts == 0 || p.Ts%3600 != 0 {
			t.Errorf("bucket ts 未对齐: %+v", p)
		}
	}
}

// TestRequestLogDeleteBefore 保留期删除：只删 cutoff 前行，保留期内不动。
func TestRequestLogDeleteBefore(t *testing.T) {
	db := NewTestDB(t)
	repo := repository.NewRequestLogRepository(db)
	ctx := context.Background()

	now := time.Now()
	seedLogs(t, repo, []model.RequestLog{
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, CreatedAt: now.AddDate(0, 0, -120)},
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, CreatedAt: now.AddDate(0, 0, -100)},
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, CreatedAt: now.AddDate(0, 0, -91)}, // 临界：刚好 cutoff 前
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, CreatedAt: now.AddDate(0, 0, -89)},
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, CreatedAt: now.Add(-time.Hour)},
	})

	cutoff := now.AddDate(0, 0, -90)
	n, err := repo.DeleteBefore(ctx, cutoff, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("deleted = %d, want 3", n)
	}
	// 保留期内 2 行仍在
	_, total, err := repo.List(ctx, repository.LogFilter{}, pagination.Normalize(1, 10))
	if err != nil || total != 2 {
		t.Fatalf("remaining = %d, err = %v, want 2", total, err)
	}
}

// TestRequestLogDeleteBeforeBatch 单批 limit 生效：分批调用直至删完。
func TestRequestLogDeleteBeforeBatch(t *testing.T) {
	db := NewTestDB(t)
	repo := repository.NewRequestLogRepository(db)
	ctx := context.Background()

	now := time.Now()
	for i := 0; i < 5; i++ {
		if err := repo.Create(ctx, &model.RequestLog{
			Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough,
			CreatedAt: now.AddDate(0, 0, -100),
		}); err != nil {
			t.Fatal(err)
		}
	}

	cutoff := now.AddDate(0, 0, -90)
	var total int64
	for i := 0; i < 10; i++ {
		n, err := repo.DeleteBefore(ctx, cutoff, 2)
		if err != nil {
			t.Fatal(err)
		}
		total += n
		if n < 2 {
			break
		}
	}
	if total != 5 {
		t.Fatalf("batch deleted = %d, want 5", total)
	}
	if _, cnt, err := repo.List(ctx, repository.LogFilter{}, pagination.Normalize(1, 10)); err != nil || cnt != 0 {
		t.Fatalf("remaining = %d, err = %v, want 0", cnt, err)
	}
}

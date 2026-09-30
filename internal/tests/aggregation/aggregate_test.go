// Package aggregation_test 日级聚合任务测试：回填对账、迟到写入滚动重建、
// 水位线推进、清理明细后聚合行保留（累计不缩水的核心保证）、费用重算区间重建。
package aggregation_test

import (
	"context"
	"testing"
	"time"

	"xtokenhub/internal/aggregation"
	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/pagination"
	"xtokenhub/internal/repository"
	"xtokenhub/internal/tests/testutil"
)

func seed(ctx context.Context, t *testing.T, repo repository.RequestLogRepository, logs []model.RequestLog) {
	t.Helper()
	for i := range logs {
		if err := repo.Create(ctx, &logs[i]); err != nil {
			t.Fatal(err)
		}
	}
}

func noonOf(daysAgo int) time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.Local).AddDate(0, 0, -daysAgo)
}

// TestAggregationBackfill 全量回填对账：请求数/token/错误数/费用/最大耗时与明细一致，
// 日 × 模型聚合同步生成。
func TestAggregationBackfill(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	logRepo := repository.NewRequestLogRepository(db)
	ctx := context.Background()

	seed(ctx, t, logRepo, []model.RequestLog{
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, PromptTokens: 10, CompletionTokens: 5, CachedTokens: 3, CostUSD: 0.5, DurationMS: 100, CreatedAt: noonOf(3)},
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, PromptTokens: 400, CompletionTokens: 100, CachedTokens: 40, CostUSD: 2.5, DurationMS: 5000, CreatedAt: noonOf(1)},
		{Model: "m2", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardConverted, PromptTokens: 150, CompletionTokens: 50, CostUSD: 1, DurationMS: 3000, CreatedAt: noonOf(0)},
		// 失败请求：不参与缓存/最大耗时，但 token 计入（与 DailyUsage/TrendByDay 明细口径一致）
		{Model: "m2", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardConverted, PromptTokens: 999, DurationMS: 99999, Error: "upstream", CreatedAt: noonOf(0).Add(time.Hour)},
	})

	daily := testutil.SeedDailyStats(t, db)
	now := time.Now()
	today := repository.DayBucketOf(now)

	rows, err := daily.ListDays(ctx, 0, today)
	if err != nil {
		t.Fatal(err)
	}
	var requests, tokens, cached, errors int64
	var cost float64
	var maxDuration int64
	days := map[string]model.DailyUsageStat{}
	for _, r := range rows {
		requests += r.Requests
		tokens += r.PromptTokens + r.CompletionTokens
		cached += r.CachedTokens
		errors += r.ErrorRequests
		cost += r.CostUSD
		if r.MaxDurationMS > maxDuration {
			maxDuration = r.MaxDurationMS
		}
		days[r.Date] = r
	}
	if requests != 4 || tokens != 1714 || cached != 43 || errors != 1 {
		t.Fatalf("聚合对账: req=%d tok=%d cached=%d err=%d, want 4/1714/43/1（token 含失败请求，与 DailyUsage 口径一致）", requests, tokens, cached, errors)
	}
	if cost < 3.999 || cost > 4.001 {
		t.Errorf("cost = %f, want 4.0", cost)
	}
	if maxDuration != 5000 {
		t.Errorf("max_duration = %d, want 5000（失败请求不计入）", maxDuration)
	}

	// 峰值日（昨天）与 3 天前的单日正确性
	peak, ok := days[noonOf(1).Format("2006-01-02")]
	if !ok || peak.Requests != 1 || peak.PromptTokens+peak.CompletionTokens != 500 || peak.MaxDurationMS != 5000 {
		t.Fatalf("峰值日聚合异常: %+v (ok=%v)", peak, ok)
	}
	first, ok := days[noonOf(3).Format("2006-01-02")]
	if !ok || first.Requests != 1 || first.CachedTokens != 3 {
		t.Fatalf("最早日聚合异常: %+v (ok=%v)", first, ok)
	}

	// 日 × 模型：m1 两天各一行
	models, err := daily.ListModelDays(ctx, 0, today)
	if err != nil {
		t.Fatal(err)
	}
	var m1Tokens int64
	for _, m := range models {
		if m.Model == "m1" {
			m1Tokens += m.PromptTokens + m.CompletionTokens
		}
	}
	if m1Tokens != 515 {
		t.Errorf("m1 聚合 token = %d, want 515", m1Tokens)
	}
}

// TestAggregationLateWrite 水位线内迟到写入由滚动重建吸收，水位线推进到 today-2。
func TestAggregationLateWrite(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	logRepo := repository.NewRequestLogRepository(db)
	ctx := context.Background()

	now := time.Now()
	today := repository.DayBucketOf(now)
	seed(ctx, t, logRepo, []model.RequestLog{
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, PromptTokens: 10, CreatedAt: noonOf(1)},
	})

	daily := testutil.SeedDailyStats(t, db) // 内部已 RebuildAll 并置水位线
	settings := repository.NewDataSettingsRepository(db)
	agg := aggregation.New(daily, settings)

	st, err := settings.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.AggWatermarkDay != today-2 {
		t.Fatalf("回填后水位线 = %d, want %d", st.AggWatermarkDay, today-2)
	}

	// 迟到写入（昨天）：下一轮重建吸收
	seed(ctx, t, logRepo, []model.RequestLog{
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, PromptTokens: 20, CreatedAt: noonOf(1).Add(2 * time.Hour)},
	})
	if err := agg.TickOnce(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := daily.ListDays(ctx, today-1, today-1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("昨天聚合行 = %v, err = %v, want 1 行", rows, err)
	}
	if rows[0].Requests != 2 || rows[0].PromptTokens+rows[0].CompletionTokens != 30 {
		t.Fatalf("迟到写入未进聚合: %+v", rows[0])
	}
	// 未跨天，水位线不动
	st, _ = settings.Get(ctx)
	if st.AggWatermarkDay != today-2 {
		t.Errorf("未跨天水位线不应推进: %d, want %d", st.AggWatermarkDay, today-2)
	}
}

// TestAggregationSurvivesCleanup 核心保证：明细被保留期清理后，
// 封存日的聚合行保留（RebuildDay 对源日无明细的天不做任何改动），累计不缩水。
func TestAggregationSurvivesCleanup(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	logRepo := repository.NewRequestLogRepository(db)
	ctx := context.Background()

	now := time.Now()
	seed(ctx, t, logRepo, []model.RequestLog{
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, PromptTokens: 100, CreatedAt: noonOf(40)},
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, PromptTokens: 50, CreatedAt: noonOf(0)},
	})

	daily := testutil.SeedDailyStats(t, db)
	oldDay := repository.DayBucketOf(noonOf(40))

	// 模拟保留期清理：删除 30 天前的明细
	if _, err := logRepo.DeleteBefore(ctx, now.AddDate(0, 0, -30), 1000); err != nil {
		t.Fatal(err)
	}
	// 对已无明细的封存日重建：必须是无操作，不得清掉聚合行
	if err := daily.RebuildDay(ctx, oldDay); err != nil {
		t.Fatal(err)
	}
	rows, err := daily.ListDays(ctx, 0, repository.DayBucketOf(now))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("清理后聚合行 = %d, want 2（封存行保留）", len(rows))
	}
	var tokens int64
	for _, r := range rows {
		tokens += r.PromptTokens + r.CompletionTokens
	}
	if tokens != 150 {
		t.Errorf("清理后累计 token = %d, want 150（不缩水）", tokens)
	}
}

// TestAggregationRebuildRange 费用重算回写后按区间重建，修正费用进入聚合表。
func TestAggregationRebuildRange(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	logRepo := repository.NewRequestLogRepository(db)
	ctx := context.Background()

	now := time.Now()
	seed(ctx, t, logRepo, []model.RequestLog{
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, PromptTokens: 10, CreatedAt: noonOf(5)},
	})

	daily := testutil.SeedDailyStats(t, db)
	agg := aggregation.New(daily, repository.NewDataSettingsRepository(db))

	items, _, err := logRepo.List(ctx, repository.LogFilter{}, pagination.Normalize(1, 10))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("明细行 = %d, want 1", len(items))
	}
	// 模拟费用重算回写
	if err := logRepo.UpdateCost(ctx, items[0].ID, 1.25, model.UsageStyle("openai"), model.PricePeriod("")); err != nil {
		t.Fatal(err)
	}
	if err := agg.RebuildRange(ctx, now.AddDate(0, 0, -6), now); err != nil {
		t.Fatal(err)
	}
	sum, err := daily.SumDays(ctx, 0, repository.DayBucketOf(now))
	if err != nil {
		t.Fatal(err)
	}
	if sum.CostUSD < 1.249 || sum.CostUSD > 1.251 {
		t.Errorf("重算后聚合费用 = %f, want 1.25", sum.CostUSD)
	}
}

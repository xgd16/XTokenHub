package repository_test

import (
	"context"
	"testing"
	"time"

	"xtokenhub/internal/model"
	"xtokenhub/internal/repository"
	"xtokenhub/internal/tests/testutil"
)

// seedCostLogs 写入带费用的日志：今天两条（1.5 + 0.5）、昨天一条 2、前天一条 1。
func seedCostLogs(t *testing.T, repo repository.RequestLogRepository) time.Time {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	logs := []model.RequestLog{
		{Model: "gpt-4o", ChannelName: "ch-a", KeyName: "k1", Protocol: model.ProtocolChatCompletions,
			ForwardMode: model.ForwardNativePassthrough, PromptTokens: 100, CompletionTokens: 20,
			TotalTokens: 120, CostUSD: 1.5, DurationMS: 10, CreatedAt: midnight.Add(time.Minute)},
		{Model: "gpt-4o", ChannelName: "ch-a", KeyName: "k1", Protocol: model.ProtocolChatCompletions,
			ForwardMode: model.ForwardNativePassthrough, PromptTokens: 50, CompletionTokens: 10,
			TotalTokens: 60, CostUSD: 0.5, DurationMS: 20, CreatedAt: midnight.Add(2 * time.Minute)},
		{Model: "claude-sonnet-4", ChannelName: "ch-b", KeyName: "k2", Protocol: model.ProtocolMessages,
			ForwardMode: model.ForwardConverted, PromptTokens: 200, CompletionTokens: 40,
			TotalTokens: 240, CostUSD: 2, DurationMS: 30, CreatedAt: midnight.AddDate(0, 0, -1).Add(time.Hour)},
		{Model: "unpriced-model", ChannelName: "ch-b", KeyName: "k2", Protocol: model.ProtocolMessages,
			ForwardMode: model.ForwardConverted, PromptTokens: 300, CompletionTokens: 60,
			TotalTokens: 360, CostUSD: 0, DurationMS: 40, CreatedAt: midnight.AddDate(0, 0, -2).Add(time.Hour)},
	}
	for i := range logs {
		if err := repo.Create(ctx, &logs[i]); err != nil {
			t.Fatalf("写入日志: %v", err)
		}
	}
	return midnight
}

func TestSummaryIncludesCost(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	repo := repository.NewRequestLogRepository(db)
	midnight := seedCostLogs(t, repo)

	s, err := repo.Summary(context.Background(), midnight)
	if err != nil {
		t.Fatal(err)
	}
	if s.CostUSD != 2 {
		t.Errorf("当日费用 = %v, want 2", s.CostUSD)
	}

	// 全窗口：1.5 + 0.5 + 2 + 0 = 4
	all, err := repo.Summary(context.Background(), midnight.AddDate(0, 0, -7))
	if err != nil {
		t.Fatal(err)
	}
	if all.CostUSD != 4 {
		t.Errorf("全窗口费用 = %v, want 4", all.CostUSD)
	}
}

func TestGroupStatsIncludeCost(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	repo := repository.NewRequestLogRepository(db)
	midnight := seedCostLogs(t, repo)
	ctx := context.Background()
	since := midnight.AddDate(0, 0, -7)

	byModel, err := repo.GroupByModel(ctx, since)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]float64{}
	for _, g := range byModel {
		byName[g.Name] = g.CostUSD
	}
	if byName["gpt-4o"] != 2 {
		t.Errorf("gpt-4o 费用 = %v, want 2", byName["gpt-4o"])
	}
	if byName["unpriced-model"] != 0 {
		t.Errorf("未定价模型费用 = %v, want 0", byName["unpriced-model"])
	}

	byChannel, err := repo.GroupByChannel(ctx, since)
	if err != nil {
		t.Fatal(err)
	}
	chCost := map[string]float64{}
	for _, g := range byChannel {
		chCost[g.Name] = g.CostUSD
	}
	if chCost["ch-a"] != 2 || chCost["ch-b"] != 2 {
		t.Errorf("渠道费用 = %+v, want ch-a=2 ch-b=2", chCost)
	}

	byKey, err := repo.GroupByKey(ctx, since)
	if err != nil {
		t.Fatal(err)
	}
	keyCost := map[string]float64{}
	for _, g := range byKey {
		keyCost[g.Name] = g.CostUSD
	}
	if keyCost["k1"] != 2 || keyCost["k2"] != 2 {
		t.Errorf("密钥费用 = %+v, want k1=2 k2=2", keyCost)
	}
}

func TestTrendsIncludeCost(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	repo := repository.NewRequestLogRepository(db)
	midnight := seedCostLogs(t, repo)
	ctx := context.Background()

	days, err := repo.TrendByDay(ctx, midnight.AddDate(0, 0, -7))
	if err != nil {
		t.Fatal(err)
	}
	todayKey := midnight.Format("2006-01-02")
	var todayCost float64
	for _, p := range days {
		if p.Date == todayKey {
			todayCost = p.CostUSD
		}
	}
	if todayCost != 2 {
		t.Errorf("当日趋势费用 = %v, want 2（days=%+v）", todayCost, days)
	}

	buckets, err := repo.TrendByBucket(ctx, midnight, 3600)
	if err != nil {
		t.Fatal(err)
	}
	if len(buckets) == 0 {
		t.Fatal("小时桶为空")
	}
	var bucketSum float64
	for _, b := range buckets {
		bucketSum += b.CostUSD
	}
	if bucketSum != 2 {
		t.Errorf("小时桶费用合计 = %v, want 2", bucketSum)
	}
}

func TestCostSinceAndByDay(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	repo := repository.NewRequestLogRepository(db)
	midnight := seedCostLogs(t, repo)
	ctx := context.Background()

	spent, reqs, err := repo.CostSince(ctx, midnight, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if spent != 2 || reqs != 2 {
		t.Errorf("CostSince = (%v,%d), want (2,2)", spent, reqs)
	}

	// 半开区间：until 落在最后一条之前
	spent2, reqs2, err := repo.CostSince(ctx, midnight, midnight.Add(90*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if spent2 != 1.5 || reqs2 != 1 {
		t.Errorf("半开区间 CostSince = (%v,%d), want (1.5,1)", spent2, reqs2)
	}

	byDay, err := repo.CostByDay(ctx, midnight.AddDate(0, 0, -7))
	if err != nil {
		t.Fatal(err)
	}
	if len(byDay) != 3 {
		t.Fatalf("按日聚合天数 = %d, want 3（%+v）", len(byDay), byDay)
	}
	if byDay[len(byDay)-1].Date != midnight.Format("2006-01-02") {
		t.Errorf("最后一天 = %s", byDay[len(byDay)-1].Date)
	}
	if byDay[len(byDay)-1].CostUSD != 2 {
		t.Errorf("当日费用 = %v", byDay[len(byDay)-1].CostUSD)
	}
}

// TestModelsWithUsageScoping 覆盖用量聚合：只按时间窗过滤，不受价格表影响。
// 是否「未定价」由 service 层用别名规则判定，仓储层不做该判断。
func TestModelsWithUsageScoping(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	logRepo := repository.NewRequestLogRepository(db)
	midnight := seedCostLogs(t, logRepo)
	ctx := context.Background()

	// 七天窗：三个模型，按 total_tokens 倒序（unpriced 360 / claude 240 / gpt-4o 180）。
	items, err := logRepo.ModelsWithUsage(ctx, midnight.AddDate(0, 0, -7), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("用量模型数 = %d (%+v), 期望 3", len(items), items)
	}
	if items[0].Model != "unpriced-model" || items[0].TotalTokens != 360 || items[0].Requests != 1 {
		t.Errorf("首位 = %+v, 期望 unpriced-model/360/1", items[0])
	}

	// 仅今日：只剩 gpt-4o 两条。
	today, err := logRepo.ModelsWithUsage(ctx, midnight, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(today) != 1 || today[0].Model != "gpt-4o" || today[0].Requests != 2 {
		t.Errorf("今日用量 = %+v, 期望 gpt-4o/2", today)
	}
}

func TestRecomputeBatchAndUpdateCost(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	repo := repository.NewRequestLogRepository(db)
	midnight := seedCostLogs(t, repo)
	ctx := context.Background()
	from := midnight.AddDate(0, 0, -7)

	// onlyMissing：只返回费用 <= 0 的行
	missing, err := repo.RecomputeBatch(ctx, from, time.Time{}, 0, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0].Model != "unpriced-model" {
		t.Fatalf("待补算行 = %+v, want [unpriced-model]", missing)
	}

	// 全量：4 条，且按 id 升序、游标可用
	all, err := repo.RecomputeBatch(ctx, from, time.Time{}, 0, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].ID >= all[1].ID {
		t.Fatalf("首屏全量 = %+v, 期望按 id 升序两条", all)
	}
	next, err := repo.RecomputeBatch(ctx, from, time.Time{}, all[1].ID, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != 2 || next[0].ID <= all[1].ID {
		t.Errorf("游标续取 = %+v, 期望 id > %d", next, all[1].ID)
	}

	// 回写费用、口径与时段
	if err := repo.UpdateCost(ctx, missing[0].ID, 0.75, model.UsageStyleAnthropic, model.PricePeriodOffPeak); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, missing[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CostUSD != 0.75 || got.UsageStyle != model.UsageStyleAnthropic {
		t.Errorf("回写结果 = (%v,%q)", got.CostUSD, got.UsageStyle)
	}
	if got.PricePeriod != model.PricePeriodOffPeak {
		t.Errorf("price_period = %q, want off_peak", got.PricePeriod)
	}
	// 回写不应动其他字段
	if got.PromptTokens != 300 || got.Model != "unpriced-model" {
		t.Errorf("回写污染了其他字段: %+v", got)
	}
}

// TestRecomputeBatchNullCost 覆盖旧库升级场景：AutoMigrate 新增 cost_usd 列时
// 历史行是 NULL，而 onlyMissing 必须把 NULL 视为「待补算」。若退化为 cost_usd <= 0，
// SQL 的 NULL 比较结果为 NULL，历史行会被整体漏掉。
func TestRecomputeBatchNullCost(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	repo := repository.NewRequestLogRepository(db)
	midnight := seedCostLogs(t, repo)
	ctx := context.Background()

	// 直接置 NULL，模拟旧库升级后未回填的历史行。
	if err := db.Exec(
		"UPDATE request_logs SET cost_usd = NULL, usage_style = '' WHERE model = ?", "gpt-4o",
	).Error; err != nil {
		t.Fatalf("置 NULL: %v", err)
	}

	missing, err := repo.RecomputeBatch(ctx, midnight.AddDate(0, 0, -7), time.Time{}, 0, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	// 两条 NULL 的 gpt-4o 加上一条 cost_usd = 0 的 unpriced-model。
	if len(missing) != 3 {
		t.Fatalf("待补算行数 = %d (%+v), 期望 3（含 NULL 行）", len(missing), missing)
	}
	for _, l := range missing {
		if l.Model == "gpt-4o" && l.CostUSD != 0 {
			t.Errorf("NULL 行读出应为零值, got %v", l.CostUSD)
		}
	}
}

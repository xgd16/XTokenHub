package service_test

import (
	"context"
	"testing"
	"time"

	"xtokenhub/internal/model"
	"xtokenhub/internal/repository"
	"xtokenhub/internal/service"
	"xtokenhub/internal/tests/testutil"
)

const day = 24 * time.Hour

// TestProject 外推算法与守卫条件。
func TestProject(t *testing.T) {
	// 7 个完整日、日均 2 美元
	sevenDays := []float64{1, 2, 3, 1, 2, 3, 2}

	cases := []struct {
		name       string
		in         service.ProjectInput
		wantNil    bool
		wantUSD    float64
		wantBasis  string
		wantReason string
	}{
		{
			name:    "周期无效",
			in:      service.ProjectInput{SpentUSD: 5, Total: 0},
			wantNil: true, wantReason: "周期长度无效",
		},
		{
			name:    "周期刚开始（不足 10 分钟）",
			in:      service.ProjectInput{SpentUSD: 5, Elapsed: time.Minute, Total: 30 * day},
			wantNil: true, wantReason: "周期刚开始，样本不足",
		},
		{
			// 已过 12 小时 / 共 1 天 = 50% > 1% 与 10 分钟，但还没花钱
			name:    "本周期尚无花费",
			in:      service.ProjectInput{SpentUSD: 0, Elapsed: 12 * time.Hour, Total: day},
			wantNil: true, wantReason: "本周期暂无花费",
		},
		{
			// 无完整日数据 -> 本周期线性外推：花 2 美元用了 6 小时，全天约 8 美元
			name:      "无完整日数据走线性外推",
			in:        service.ProjectInput{SpentUSD: 2, Elapsed: 6 * time.Hour, Total: day},
			wantUSD:   8,
			wantBasis: "linear",
		},
		{
			// 有完整日数据 -> 剩余 12 小时按日均 2 美元推进：2 + 2*(12/24) = 3
			name:      "有完整日数据走日均速率",
			in:        service.ProjectInput{SpentUSD: 2, Elapsed: 12 * time.Hour, Total: day, DailyCosts: sevenDays},
			wantUSD:   3,
			wantBasis: "run_rate",
		},
		{
			// 完整日都不花钱（日均 0）时退回线性，避免预测恒等于已花费
			name:      "完整日无花费时退回线性",
			in:        service.ProjectInput{SpentUSD: 2, Elapsed: 6 * time.Hour, Total: day, DailyCosts: []float64{0, 0, 0, 0}},
			wantUSD:   8,
			wantBasis: "linear",
		},
		{
			// 完整日样本不足 3 天 -> 线性
			name:      "完整日样本不足退回线性",
			in:        service.ProjectInput{SpentUSD: 2, Elapsed: 6 * time.Hour, Total: day, DailyCosts: []float64{1, 1}},
			wantUSD:   8,
			wantBasis: "linear",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := service.Project(c.in)
			if c.wantNil {
				if got.ProjectedUSD != nil {
					t.Fatalf("应不给预测, got %v", *got.ProjectedUSD)
				}
				if got.Reason != c.wantReason {
					t.Errorf("原因 = %q, want %q", got.Reason, c.wantReason)
				}
				return
			}
			if got.ProjectedUSD == nil {
				t.Fatalf("应有预测, reason=%q", got.Reason)
			}
			approx(t, *got.ProjectedUSD, c.wantUSD, "预测值")
			if got.Basis != c.wantBasis {
				t.Errorf("基准 = %q, want %q", got.Basis, c.wantBasis)
			}
		})
	}
}

// 日均窗口最多取近 7 天：更早的旧数据不应拉低速率。
func TestProjectRunRateWindow(t *testing.T) {
	// 近 3 天每天 10 美元，更早 7 天每天 1 美元
	daily := []float64{1, 1, 1, 1, 1, 1, 1, 10, 10, 10}
	got := service.Project(service.ProjectInput{
		SpentUSD: 10, Elapsed: 12 * time.Hour, Total: day, DailyCosts: daily,
	})
	if got.ProjectedUSD == nil {
		t.Fatal("应有预测")
	}
	// 近 7 天 = [1,1,1,1,10,10,10] -> 日均 34/7 ≈ 4.857；10 + 4.857*0.5 ≈ 12.43
	avg := 34.0 / 7.0
	approx(t, *got.ProjectedUSD, 10+avg*0.5, "7 日窗口日均")
}

func TestProjectConfidence(t *testing.T) {
	cases := []struct {
		elapsed time.Duration
		want    string
	}{
		{time.Hour, "low"},        // 1/24 ≈ 4.2%
		{5 * time.Hour, "medium"}, // ≈ 20.8%
		{12 * time.Hour, "high"},  // 50%
		{23 * time.Hour, "high"},
	}
	for _, c := range cases {
		got := service.Project(service.ProjectInput{SpentUSD: 1, Elapsed: c.elapsed, Total: day})
		if got.Confidence != c.want {
			t.Errorf("elapsed=%v 置信度 = %q, want %q", c.elapsed, got.Confidence, c.want)
		}
	}
}

// Forecast 端到端：按日费用入账后，今日/本月已花费与预测、预算超支时点。
func TestForecast(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	logRepo := repository.NewRequestLogRepository(db)
	billingRepo := repository.NewBillingSettingsRepository(db)
	svc := service.NewCostService(
		repository.NewModelPriceRepository(db), logRepo, billingRepo,
		testutil.NewStubPricingSource(sonnetEntry()),
		service.CostOptions{Enabled: true, InitialBilling: model.BillingSettings{DisplayCurrency: model.CurrencyUSD, USDRate: 7.2}},
	)
	ctx := context.Background()
	if err := svc.EnsureDefaults(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	for _, l := range []model.RequestLog{
		// 今天：已花 3 USD
		{Model: "claude-sonnet-4", PromptTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: 3, CreatedAt: midnight.Add(time.Minute)},
		// 昨天 / 前天 / 大前天：各 2 USD，构成日均基准（需 >= 3 个完整日）
		{Model: "claude-sonnet-4", PromptTokens: 1, TotalTokens: 1, CostUSD: 2, CreatedAt: midnight.AddDate(0, 0, -1).Add(time.Hour)},
		{Model: "claude-sonnet-4", PromptTokens: 1, TotalTokens: 1, CostUSD: 2, CreatedAt: midnight.AddDate(0, 0, -2).Add(time.Hour)},
		{Model: "claude-sonnet-4", PromptTokens: 1, TotalTokens: 1, CostUSD: 2, CreatedAt: midnight.AddDate(0, 0, -3).Add(time.Hour)},
		// 上月：不计入本月
		{Model: "claude-sonnet-4", PromptTokens: 1, TotalTokens: 1, CostUSD: 99, CreatedAt: midnight.AddDate(0, 0, -40)},
	} {
		if err := logRepo.Create(ctx, &l); err != nil {
			t.Fatal(err)
		}
	}

	before := time.Now()
	today, err := svc.Forecast(ctx, "today")
	if err != nil {
		t.Fatal(err)
	}
	if today.Period != "today" {
		t.Errorf("period = %q", today.Period)
	}
	approx(t, today.SpentUSD, 3, "今日已花费")
	if today.ProjectedUSD == nil {
		t.Fatalf("今日应有预测, reason=%q", today.Reason)
	}
	if today.Basis != "run_rate" {
		t.Errorf("基准 = %q, want run_rate", today.Basis)
	}
	// 日均 2 USD：今日预测 = 3 + 2 * 剩余天数。now 与 Forecast 内部取时相差毫秒级，
	// 用宽松容差而非精确比较。
	remainingDays := today.PeriodEnd.Sub(before).Hours() / 24
	approxTol(t, *today.ProjectedUSD, 3+2*remainingDays, 1e-4, "今日预测")
	approx(t, today.DailyAvgUSD, 2, "日均基准")

	month, err := svc.Forecast(ctx, "month")
	if err != nil {
		t.Fatal(err)
	}
	// 本月 = 今天 3 + 前三天各 2（上月 99 不计入）
	approx(t, month.SpentUSD, 9, "本月已花费")
	if month.BudgetUSD != 0 {
		t.Errorf("未设预算时 BudgetUSD = %v, want 0", month.BudgetUSD)
	}
	if month.ProjectedExceededDate != "" {
		t.Error("未设预算不应给出超支时点")
	}
	if month.ProjectedUSD == nil || *month.ProjectedUSD <= month.SpentUSD {
		t.Errorf("本月预测应大于已花费: %v vs %v", month.ProjectedUSD, month.SpentUSD)
	}

	// 设一个必然超支的预算 -> 给出触及时间
	if _, err := svc.UpdateBilling(ctx, &service.UpdateBillingInput{
		DisplayCurrency: "USD", MonthlyBudgetUSD: 10,
	}); err != nil {
		t.Fatal(err)
	}
	month2, err := svc.Forecast(ctx, "month")
	if err != nil {
		t.Fatal(err)
	}
	if month2.BudgetUSD != 10 {
		t.Errorf("预算 = %v, want 10", month2.BudgetUSD)
	}
	if month2.BurnPerHourUSD <= 0 {
		t.Fatal("每小时消耗应大于 0")
	}
	if month2.ProjectedExceededDate == "" {
		t.Error("预计超支时应给出触及时间")
	}

	// 非法周期
	if _, err := svc.Forecast(ctx, "week"); err == nil {
		t.Error("不支持的 period 应报错")
	}
}

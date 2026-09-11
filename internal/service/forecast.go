package service

import (
	"context"
	"time"

	"xtokenhub/internal/pkg/errs"
)

// 预测口径常量。
const (
	// forecastMinElapsed 周期刚开始时的最小观察时长，另受「周期 1%」约束。
	forecastMinElapsed = 10 * time.Minute
	// forecastRunRateMinDays 采用日均基准所需的最少完整天数。
	forecastRunRateMinDays = 3
	// forecastRunRateWindow 日均基准的观察窗口（完整日）。
	forecastRunRateWindow = 7
)

// ProjectInput 预测输入。
type ProjectInput struct {
	SpentUSD   float64       // 周期内已花费
	Elapsed    time.Duration // 周期已过去时长
	Total      time.Duration // 周期总时长
	DailyCosts []float64     // 最近若干完整日的费用（升序，不含当天）
}

// Projection 预测结果。ProjectedUSD 为 nil 表示样本不足、不给出预测。
type Projection struct {
	ProjectedUSD *float64 `json:"projected_usd"`
	BurnPerHour  float64  `json:"burn_per_hour_usd"`
	Basis        string   `json:"basis"`      // run_rate | linear
	Confidence   string   `json:"confidence"` // low | medium | high
	Reason       string   `json:"reason,omitempty"`
}

// Project 花费外推：
//   - 有 >= 3 个完整日数据时，剩余时段按近 7 日日均速率推进（run_rate，抗周期内波动）
//   - 否则按本周期已花速率线性外推（linear）
//
// 守卫：周期刚开始（已过时长 < max(10min, 周期 1%)）或本周期尚无花费时不给出预测，
// 避免用极少样本外推出误导性的大数字。
func Project(in ProjectInput) Projection {
	if in.Total <= 0 {
		return Projection{Confidence: "low", Reason: "周期长度无效"}
	}
	minElapsed := in.Total / 100
	if minElapsed < forecastMinElapsed {
		minElapsed = forecastMinElapsed
	}
	if in.Elapsed < minElapsed {
		return Projection{Confidence: "low", Reason: "周期刚开始，样本不足"}
	}
	if in.SpentUSD <= 0 {
		return Projection{Confidence: "low", Reason: "本周期暂无花费"}
	}

	remaining := in.Total - in.Elapsed
	if remaining < 0 {
		remaining = 0
	}
	out := Projection{
		BurnPerHour: in.SpentUSD / in.Elapsed.Hours(),
		Basis:       "linear",
		Confidence:  confidenceOf(in.Elapsed, in.Total),
	}
	total := in.SpentUSD + out.BurnPerHour*remaining.Hours()

	if avg, ok := dailyAverage(in.DailyCosts); ok {
		out.Basis = "run_rate"
		total = in.SpentUSD + avg*(remaining.Hours()/24)
	}
	out.ProjectedUSD = &total
	return out
}

// dailyAverage 近 N 个完整日的日均费用；样本不足或均值为 0 时返回 false。
func dailyAverage(daily []float64) (float64, bool) {
	n := len(daily)
	if n < forecastRunRateMinDays {
		return 0, false
	}
	if n > forecastRunRateWindow {
		daily = daily[n-forecastRunRateWindow:]
	}
	var sum float64
	for _, v := range daily {
		sum += v
	}
	avg := sum / float64(len(daily))
	if avg <= 0 {
		return 0, false
	}
	return avg, true
}

// confidenceOf 按周期已过比例分级。
func confidenceOf(elapsed, total time.Duration) string {
	f := float64(elapsed) / float64(total)
	switch {
	case f < 0.05:
		return "low"
	case f < 0.25:
		return "medium"
	default:
		return "high"
	}
}

// CostForecast 花费预测结果。
type CostForecast struct {
	Period         string    `json:"period"` // today | month
	PeriodStart    time.Time `json:"period_start"`
	PeriodEnd      time.Time `json:"period_end"`
	SpentUSD       float64   `json:"spent_usd"`
	ProjectedUSD   *float64  `json:"projected_usd"`
	BurnPerHourUSD float64   `json:"burn_per_hour_usd"`
	DailyAvgUSD    float64   `json:"daily_avg_usd"` // 近 7 个完整日的日均花费
	Basis          string    `json:"basis"`
	Confidence     string    `json:"confidence"`
	Reason         string    `json:"reason,omitempty"`
	// BudgetUSD 月度预算（0 = 未设），仅 month 周期用于推算超支时点。
	BudgetUSD             float64 `json:"budget_usd"`
	ProjectedExceededDate string  `json:"projected_exceeded_date,omitempty"`
}

// Forecast 计算今日/本月已花费与期末预测。
func (s *CostService) Forecast(ctx context.Context, period string) (*CostForecast, error) {
	now := time.Now()
	start, end, err := periodRange(period, now)
	if err != nil {
		return nil, err
	}

	spent, _, err := s.logs.CostSince(ctx, start, time.Time{})
	if err != nil {
		return nil, errs.Wrap(errs.CodeInternal, "统计周期花费失败", err)
	}
	out := &CostForecast{
		Period:      period,
		PeriodStart: start,
		PeriodEnd:   end,
		SpentUSD:    spent,
	}

	// 近 7 个完整日（不含当天）作为日均基准
	dayCosts, err := s.logs.CostByDay(ctx, startOfDay(now).AddDate(0, 0, -(forecastRunRateWindow+1)))
	if err != nil {
		return nil, errs.Wrap(errs.CodeInternal, "统计按日花费失败", err)
	}
	todayKey := startOfDay(now).Format("2006-01-02")
	daily := make([]float64, 0, len(dayCosts))
	for _, d := range dayCosts {
		if d.Date >= todayKey {
			continue // 只取已完整的自然日
		}
		daily = append(daily, d.CostUSD)
	}
	if avg, ok := dailyAverage(daily); ok {
		out.DailyAvgUSD = avg
	}

	proj := Project(ProjectInput{
		SpentUSD:   spent,
		Elapsed:    now.Sub(start),
		Total:      end.Sub(start),
		DailyCosts: daily,
	})
	out.ProjectedUSD = proj.ProjectedUSD
	out.BurnPerHourUSD = proj.BurnPerHour
	out.Basis = proj.Basis
	out.Confidence = proj.Confidence
	out.Reason = proj.Reason

	if st, berr := s.Billing(ctx); berr == nil && st != nil {
		out.BudgetUSD = st.MonthlyBudgetUSD
	}
	// 按当前速率推算何时触及月度预算（仅在确实会超支时给出）
	if period == "month" && out.BudgetUSD > 0 && out.ProjectedUSD != nil &&
		*out.ProjectedUSD > out.BudgetUSD && out.BurnPerHourUSD > 0 {
		hours := (out.BudgetUSD - spent) / out.BurnPerHourUSD
		if hours >= 0 {
			out.ProjectedExceededDate = now.Add(
				time.Duration(hours * float64(time.Hour))).Format("2006-01-02 15:04")
		}
	}
	return out, nil
}

// periodRange 周期边界（本地时区，与仪表盘「今天」口径一致）。
func periodRange(period string, now time.Time) (start, end time.Time, err error) {
	switch period {
	case "", "today":
		start = startOfDay(now)
		return start, start.AddDate(0, 0, 1), nil
	case "month":
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		return start, start.AddDate(0, 1, 0), nil
	default:
		return time.Time{}, time.Time{}, errs.New(errs.CodeInvalidParams, "period 只支持 today 或 month")
	}
}

// startOfDay 本地时区当天零点。
func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

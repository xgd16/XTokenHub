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

// Lifetime 全历史累计统计：总 token / 峰值日 / 最长成功耗时 / 连续天数。
func TestStatsLifetime(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	logRepo := repository.NewRequestLogRepository(db)
	ctx := context.Background()

	now := time.Now()
	noon := func(daysAgo int) time.Time {
		return time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.Local).AddDate(0, 0, -daysAgo)
	}
	for _, l := range []model.RequestLog{
		// 3 天前：连击起点
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, PromptTokens: 10, DurationMS: 100, CreatedAt: noon(3)},
		// 2 天前：空档，连击重置
		// 昨天：峰值日
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, PromptTokens: 400, CompletionTokens: 100, DurationMS: 5000, CreatedAt: noon(1)},
		// 今天：最长成功耗时 + 一条错误请求（其 99999ms 不计入）
		{Model: "m2", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardConverted, PromptTokens: 150, CompletionTokens: 50, DurationMS: 3000, CreatedAt: noon(0)},
		{Model: "m2", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardConverted, DurationMS: 99999, Error: "upstream", CreatedAt: noon(0).Add(time.Hour)},
	} {
		if err := logRepo.Create(ctx, &l); err != nil {
			t.Fatal(err)
		}
	}

	svc := service.NewStatsService(logRepo)
	st, err := svc.Lifetime(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.TotalTokens != 710 {
		t.Errorf("TotalTokens=%d, want 710", st.TotalTokens)
	}
	if st.PeakDayTokens != 500 || st.PeakDay != noon(1).Format("2006-01-02") {
		t.Errorf("Peak=(%d,%s), want (500,%s)", st.PeakDayTokens, st.PeakDay, noon(1).Format("2006-01-02"))
	}
	if st.MaxDurationMS != 5000 {
		t.Errorf("MaxDurationMS=%d, want 5000（错误请求不计入）", st.MaxDurationMS)
	}
	if st.ActiveDays != 3 {
		t.Errorf("ActiveDays=%d, want 3", st.ActiveDays)
	}
	if st.MaxStreak != 2 {
		t.Errorf("MaxStreak=%d, want 2", st.MaxStreak)
	}
	if st.CurrentStreak != 2 {
		t.Errorf("CurrentStreak=%d, want 2（今天+昨天）", st.CurrentStreak)
	}

	// 按日×模型趋势（近 7 日）
	pts, err := svc.TrendByDayModel(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) == 0 {
		t.Fatal("TrendByDayModel 无数据")
	}
}

// SummaryFlex / ByModelFlex：since（Unix 秒）优先于 hours。
func TestStatsFlexSince(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	logRepo := repository.NewRequestLogRepository(db)
	ctx := context.Background()

	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	// 昨天 23:59：早于今天的日界（since=midnight 应排除），又在 now 起算的 24h 窗口内。
	// 不能写成 midnight-1h（=23:00）——当前时刻晚于 23:00 时 now-24h 会晚于 23:00，
	// 那条记录就落到窗口之外，测试在每天 23:00–24:00 必然失败。
	yesterday := midnight.Add(-time.Minute)
	for _, l := range []model.RequestLog{
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, PromptTokens: 10, CreatedAt: yesterday},
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, PromptTokens: 20, CreatedAt: now},
	} {
		if err := logRepo.Create(ctx, &l); err != nil {
			t.Fatal(err)
		}
	}

	svc := service.NewStatsService(logRepo)
	s, err := svc.SummaryFlex(ctx, midnight.Unix(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalRequests != 1 || s.PromptTokens != 20 {
		t.Errorf("since 口径: req=%d prompt=%d, want 1/20", s.TotalRequests, s.PromptTokens)
	}
	// since=0 回退 hours：24h 窗口包含两条
	s2, err := svc.SummaryFlex(ctx, 0, 24)
	if err != nil {
		t.Fatal(err)
	}
	if s2.TotalRequests != 2 {
		t.Errorf("hours 口径: req=%d, want 2", s2.TotalRequests)
	}
	m, err := svc.ByModelFlex(ctx, midnight.Unix(), 0)
	if err != nil || len(m) != 1 || m[0].Requests != 1 {
		t.Errorf("ByModelFlex: %v %+v", err, m)
	}
}

package repository_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"xtokenhub/internal/model"
	"xtokenhub/internal/repository"
)

// DailyUsage / TrendByDayModel：按本地日聚合 + 成功请求最大耗时口径。
func TestDailyUsageAndTrendByDayModel(t *testing.T) {
	db := NewTestDB(t)
	repo := repository.NewRequestLogRepository(db)
	ctx := context.Background()

	now := time.Now()
	noon := func(daysAgo int) time.Time {
		return time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.Local).AddDate(0, 0, -daysAgo)
	}
	seedLogs(t, repo, []model.RequestLog{
		// 昨天：两条，其中一条为错误（不计入 max_duration）
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, PromptTokens: 100, CompletionTokens: 50, DurationMS: 200, CreatedAt: noon(1)},
		{Model: "m2", Protocol: model.ProtocolMessages, ForwardMode: model.ForwardConverted, PromptTokens: 10, CompletionTokens: 5, DurationMS: 1000, Error: "x", CreatedAt: noon(1).Add(time.Hour)},
		// 今天
		{Model: "m1", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, PromptTokens: 200, CompletionTokens: 100, DurationMS: 500, CreatedAt: noon(0)},
	})

	days, err := repo.DailyUsage(ctx, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 2 {
		t.Fatalf("DailyUsage rows=%d, want 2", len(days))
	}
	d1, d2 := days[0], days[1]
	yesterday := noon(1).Format("2006-01-02")
	today := noon(0).Format("2006-01-02")
	if d1.Date != yesterday || d1.Requests != 2 || d1.TotalTokens != 165 || d1.MaxDurationMS != 200 {
		t.Errorf("day1: %+v (want date=%s req=2 tokens=165 maxMS=200)", d1, yesterday)
	}
	if d2.Date != today || d2.Requests != 1 || d2.TotalTokens != 300 || d2.MaxDurationMS != 500 {
		t.Errorf("day2: %+v (want date=%s req=1 tokens=300 maxMS=500)", d2, today)
	}

	pts, err := repo.TrendByDayModel(ctx, noon(1).Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 3 {
		t.Fatalf("TrendByDayModel rows=%d, want 3", len(pts))
	}
	want := map[string]int64{"m1|150": 0, "m2|15": 0, "m1|300": 0}
	for _, p := range pts {
		key := p.Model + "|" + strconv.FormatInt(p.TotalTokens, 10)
		if _, ok := want[key]; !ok {
			t.Errorf("意外聚合点: %+v", p)
		}
		delete(want, key)
	}
	if len(want) > 0 {
		t.Errorf("缺少聚合点: %v", want)
	}
}

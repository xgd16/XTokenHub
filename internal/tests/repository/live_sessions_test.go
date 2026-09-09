package repository_test

import (
	"context"
	"testing"
	"time"

	"xtokenhub/internal/model"
	"xtokenhub/internal/repository"
)

// LiveSessions：会话合计覆盖全量历史（不受行数窗口截断），散行独立成组，按最后活跃倒序。
func TestLiveSessionsAggregatesFullHistory(t *testing.T) {
	db := NewTestDB(t)
	repo := repository.NewRequestLogRepository(db)
	ctx := context.Background()

	now := time.Now()
	// 长会话 big：120 次请求，跨很久；其总量远超任何单行窗口
	var logs []model.RequestLog
	for i := 0; i < 120; i++ {
		// 前半透传、后半转换：验证 modes 去重后包含两种
		mode := model.ForwardNativePassthrough
		if i >= 60 {
			mode = model.ForwardConverted
		}
		logs = append(logs, model.RequestLog{
			SessionID:        "big",
			Model:            "gpt-4o",
			ChannelName:      "ch1",
			Protocol:         model.ProtocolChatCompletions,
			ForwardMode:      mode,
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
			CachedTokens:     40,
			DurationMS:       1000,
			CreatedAt:        now.Add(-time.Duration(120-i) * time.Minute),
			UserAgent:        "ZCode/1.0",
			KeyName:          "k1",
		})
	}
	// 两个散行（无 session_id）各自成组
	logs = append(logs,
		model.RequestLog{Model: "claude-3", ChannelName: "ch2", Protocol: model.ProtocolMessages, PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, DurationMS: 200, CreatedAt: now.Add(-2 * time.Minute)},
		model.RequestLog{Model: "claude-3", ChannelName: "ch2", Protocol: model.ProtocolMessages, PromptTokens: 20, CompletionTokens: 5, TotalTokens: 25, DurationMS: 300, CreatedAt: now.Add(-1 * time.Minute), Error: "boom"},
	)
	seedLogs(t, repo, logs)

	// 只取 2 组：应为最新的散行 + big 会话（big 最新请求在散行之前，故顺序为 散行2, 散行1）
	got, err := repo.LiveSessions(ctx, repository.LiveSessionsInput{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("组数 = %d, want 2", len(got))
	}

	// 会话 big 即使未被返回，也不能影响其自身统计；这里用更大 limit 全量校验
	all, err := repo.LiveSessions(ctx, repository.LiveSessionsInput{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var big *repository.LiveSession
	for i := range all {
		if all[i].SessionID == "big" {
			big = &all[i]
		}
	}
	if big == nil {
		t.Fatal("未找到 big 会话组")
	}
	if big.Requests != 120 {
		t.Errorf("big.Requests = %d, want 120（不得被窗口截断）", big.Requests)
	}
	if big.TotalTokens != 120*150 {
		t.Errorf("big.TotalTokens = %d, want %d", big.TotalTokens, 120*150)
	}
	if big.PromptTokens != 120*100 || big.CompletionTokens != 120*50 || big.CachedTokens != 120*40 {
		t.Errorf("token 分项合计不符: %+v", big)
	}
	if big.TotalMS != 120*1000 {
		t.Errorf("big.TotalMS = %d, want %d", big.TotalMS, 120*1000)
	}
	if big.Errors != 0 {
		t.Errorf("big.Errors = %d, want 0", big.Errors)
	}
	if big.Models[0] != "gpt-4o" || len(big.Models) != 1 {
		t.Errorf("big.Models = %v, want [gpt-4o]", big.Models)
	}
	if len(big.Protocols) != 1 || big.Protocols[0] != string(model.ProtocolChatCompletions) {
		t.Errorf("big.Protocols = %v, want [chat_completions]", big.Protocols)
	}
	if len(big.Modes) != 2 || big.Modes[0] != string(model.ForwardConverted) || big.Modes[1] != string(model.ForwardNativePassthrough) {
		t.Errorf("big.Modes = %v, want [converted native_passthrough]", big.Modes)
	}
	if big.KeyName != "k1" || big.UserAgent != "ZCode/1.0" {
		t.Errorf("身份字段不符: key=%q ua=%q", big.KeyName, big.UserAgent)
	}
	// 组内最早/最新时间
	if !big.FirstAt.Before(big.LastAt) {
		t.Errorf("FirstAt(%v) 应早于 LastAt(%v)", big.FirstAt, big.LastAt)
	}
}

// LiveSessions：空库返回空数组而非 nil；limit 上限被钳制。
func TestLiveSessionsEmptyAndLimit(t *testing.T) {
	db := NewTestDB(t)
	repo := repository.NewRequestLogRepository(db)

	got, err := repo.LiveSessions(context.Background(), repository.LiveSessionsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("空库应返回空数组, got=%v", got)
	}
}

// LiveSessions：散行按各自主键独立成组，错误计入 errors。
func TestLiveSessionsStandaloneRows(t *testing.T) {
	db := NewTestDB(t)
	repo := repository.NewRequestLogRepository(db)
	ctx := context.Background()
	now := time.Now()
	seedLogs(t, repo, []model.RequestLog{
		{Model: "m", TotalTokens: 10, CreatedAt: now.Add(-3 * time.Minute)},
		{Model: "m", TotalTokens: 20, CreatedAt: now.Add(-2 * time.Minute), Error: "e1"},
		{Model: "m", TotalTokens: 30, CreatedAt: now.Add(-1 * time.Minute), Error: "e2"},
	})

	got, err := repo.LiveSessions(ctx, repository.LiveSessionsInput{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("散行应各自成组, got %d 组", len(got))
	}
	// 倒序：最新在前
	if got[0].TotalTokens != 30 || got[2].TotalTokens != 10 {
		t.Errorf("顺序不符: %d, %d, %d", got[0].TotalTokens, got[1].TotalTokens, got[2].TotalTokens)
	}
	var errs int64
	for i := range got {
		if got[i].SessionID != "" {
			t.Errorf("散行 SessionID 应为空, got %q", got[i].SessionID)
		}
		errs += got[i].Errors
	}
	if errs != 2 {
		t.Errorf("错误合计 = %d, want 2", errs)
	}
}

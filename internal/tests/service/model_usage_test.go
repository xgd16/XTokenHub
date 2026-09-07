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

// TestModelServiceUsage 目录合并与多窗口用量：
// 渠道模型 ∪ 自定义组成员 ∪ 日志模型；停用渠道模型与组名不进目录；无调用模型为零值。
func TestModelServiceUsage(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	ctx := context.Background()

	logRepo := repository.NewRequestLogRepository(db)
	chRepo := repository.NewChannelRepository(db)
	customRepo := repository.NewCustomModelRepository(db)

	now := time.Now()
	seed := []model.RequestLog{
		{Model: "gpt-4o", PromptTokens: 10, CompletionTokens: 5, CreatedAt: now.Add(-time.Minute)},
		{Model: "gpt-4o", PromptTokens: 20, CompletionTokens: 10, CreatedAt: now.Add(-2 * time.Hour)},
		{Model: "member-real", PromptTokens: 100, CompletionTokens: 50, CreatedAt: now.Add(-time.Minute)},
	}
	for i := range seed {
		if err := logRepo.Create(ctx, &seed[i]); err != nil {
			t.Fatal(err)
		}
	}
	chs := []model.Channel{
		{Name: "openai", BaseURL: "https://api.openai.com", APIKey: "sk-x", Models: "gpt-4o,gpt-4o-mini", Status: model.ChannelEnabled},
		{Name: "off", BaseURL: "https://api.openai.com", APIKey: "sk-x", Models: "disabled-model"},
	}
	for i := range chs {
		if err := chRepo.Create(ctx, &chs[i]); err != nil {
			t.Fatal(err)
		}
	}
	// Status 为零值时 Create 会被 GORM 默认值覆盖成启用，须经 Update 显式停用
	off, err := chRepo.GetByName(ctx, "off")
	if err != nil {
		t.Fatal(err)
	}
	off.Status = model.ChannelDisabled
	if err := chRepo.Update(ctx, off); err != nil {
		t.Fatal(err)
	}
	group := model.CustomModel{Name: "free-group", Status: model.ChannelEnabled}
	group.SetMembers([]model.ModelMember{{Model: "member-real"}})
	if err := customRepo.Create(ctx, &group); err != nil {
		t.Fatal(err)
	}

	svc := service.NewModelService(logRepo, chRepo, customRepo)
	usage, err := svc.Usage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 3 {
		t.Fatalf("目录数 = %d, want 3（disabled-model/free-group 不应出现）: %+v", len(usage), usage)
	}
	// 排序：30d token 降序 member-real(150) > gpt-4o(45) > gpt-4o-mini(0)
	if usage[0].Model != "member-real" || usage[1].Model != "gpt-4o" || usage[2].Model != "gpt-4o-mini" {
		t.Fatalf("排序错误: %s %s %s", usage[0].Model, usage[1].Model, usage[2].Model)
	}
	mr := usage[0]
	if mr.W1h.Requests != 1 || mr.W1h.TotalTokens != 150 || mr.W24h.Requests != 1 {
		t.Errorf("member-real 窗口错误: %+v %+v", mr.W1h, mr.W24h)
	}
	gpt := usage[1]
	if gpt.W1h.Requests != 1 || gpt.W1h.TotalTokens != 15 || gpt.W24h.Requests != 2 || gpt.W24h.TotalTokens != 45 {
		t.Errorf("gpt-4o 窗口错误: %+v %+v", gpt.W1h, gpt.W24h)
	}
	mini := usage[2]
	if mini.W30d.Requests != 0 || mini.W1h.Requests != 0 {
		t.Errorf("无调用模型应为零值: %+v", mini)
	}
}

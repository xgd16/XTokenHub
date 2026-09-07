package repository_test

import (
	"xtokenhub/internal/repository"

	"context"
	"fmt"
	"sync"
	"testing"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/pagination"
)

func TestChannelCRUD(t *testing.T) {
	db := NewTestDB(t)
	repo := repository.NewChannelRepository(db)
	ctx := context.Background()

	// Create
	ch := &model.Channel{Name: "openai-main", Provider: model.ProviderOpenAICompatible, BaseURL: "https://api.openai.com", APIKey: "sk-1", Models: "gpt-4o,gpt-4o-mini"}
	ch.SetProtocols([]model.Protocol{model.ProtocolChatCompletions})
	if err := repo.Create(ctx, ch); err != nil {
		t.Fatal(err)
	}
	if ch.ID == 0 {
		t.Fatal("自增 ID 未回填")
	}

	// 重名应被唯一索引拒绝
	if err := repo.Create(ctx, &model.Channel{Name: "openai-main", Provider: model.ProviderOpenAICompatible, BaseURL: "x", APIKey: "y"}); err == nil {
		t.Error("重名渠道应创建失败")
	}

	// Get / GetByName
	got, err := repo.Get(ctx, ch.ID)
	if err != nil || got.Name != "openai-main" {
		t.Fatalf("Get: %v %+v", err, got)
	}
	if byName, err := repo.GetByName(ctx, "openai-main"); err != nil || byName.ID != ch.ID {
		t.Fatalf("GetByName: %v", err)
	}
	if _, err := repo.Get(ctx, 999); errs.From(err).Code != errs.CodeNotFound {
		t.Errorf("不存在的记录应返回 CodeNotFound, got %v", err)
	}

	// Update
	got.APIKey = "sk-2"
	got.Status = model.ChannelDisabled
	if err := repo.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	again, _ := repo.Get(ctx, ch.ID)
	if again.APIKey != "sk-2" || again.Status != model.ChannelDisabled {
		t.Errorf("Update 未生效: %+v", again)
	}
	if err := repo.Update(ctx, &model.Channel{ID: 424242, Name: "ghost"}); errs.From(err).Code != errs.CodeNotFound {
		t.Errorf("更新不存在的记录应 CodeNotFound, got %v", err)
	}

	// List
	for i := 0; i < 3; i++ {
		_ = repo.Create(ctx, &model.Channel{Name: fmt.Sprintf("c%d", i), Provider: model.ProviderAnthropic, BaseURL: "u", APIKey: "k"})
	}
	items, total, err := repo.List(ctx, pagination.Normalize(1, 2))
	if err != nil || total != 4 || len(items) != 2 {
		t.Fatalf("List: %v total=%d len=%d", err, total, len(items))
	}
	// priority 排序：默认 100，按 id ASC
	if items[0].Name != "openai-main" {
		t.Errorf("排序错误: %+v", items)
	}

	// ListEnabled
	enabled, err := repo.ListEnabled(ctx)
	if err != nil || len(enabled) != 3 {
		t.Fatalf("ListEnabled: %v len=%d", err, len(enabled))
	}

	// Delete
	if err := repo.Delete(ctx, ch.ID); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := repo.List(ctx, pagination.Normalize(1, 50)); total != 3 {
		t.Errorf("删除后 total=%d, want 3", total)
	}
	if err := repo.Delete(ctx, ch.ID); errs.From(err).Code != errs.CodeNotFound {
		t.Errorf("重复删除应 CodeNotFound, got %v", err)
	}
}

func TestChannelDeleteAndRecreate(t *testing.T) {
	db := NewTestDB(t)
	repo := repository.NewChannelRepository(db)
	ctx := context.Background()

	ch := &model.Channel{Name: "to-delete", Provider: model.ProviderAnthropic, BaseURL: "u", APIKey: "k"}
	_ = repo.Create(ctx, ch)
	_ = repo.Delete(ctx, ch.ID)

	if _, err := repo.GetByName(ctx, "to-delete"); errs.From(err).Code != errs.CodeNotFound {
		t.Error("删除后不应可查询")
	}
	// 硬删除后同名可重建
	ch2 := &model.Channel{Name: "to-delete", Provider: model.ProviderAnthropic, BaseURL: "u2", APIKey: "k"}
	if err := repo.Create(ctx, ch2); err != nil {
		t.Errorf("删除后同名重建失败: %v", err)
	}
	again, _ := repo.GetByName(ctx, "to-delete")
	if again.ID == ch.ID {
		t.Error("重建后应为新记录")
	}
}

func TestChannelConcurrentCreate(t *testing.T) {
	db := NewTestDB(t)
	repo := repository.NewChannelRepository(db)
	ctx := context.Background()

	var wg sync.WaitGroup
	errs1 := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := repo.Create(ctx, &model.Channel{Name: fmt.Sprintf("p-%d", i), Provider: model.ProviderOpenAICompatible, BaseURL: "u", APIKey: "k"})
			if err != nil {
				errs1 <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs1)
	for e := range errs1 {
		t.Errorf("并发创建失败: %v", e)
	}
	_, total, _ := repo.List(ctx, pagination.Normalize(1, 100))
	if total != 10 {
		t.Errorf("total = %d, want 10", total)
	}
}

func TestChannelListAll(t *testing.T) {
	db := NewTestDB(t)
	repo := repository.NewChannelRepository(db)
	ctx := context.Background()

	enabled := &model.Channel{Name: "on", Provider: model.ProviderOpenAICompatible, BaseURL: "u1", APIKey: "k", Priority: 10}
	disabled := &model.Channel{Name: "off", Provider: model.ProviderOpenAICompatible, BaseURL: "u2", APIKey: "k", Priority: 5, Status: model.ChannelDisabled}
	for _, ch := range []*model.Channel{enabled, disabled} {
		if err := repo.Create(ctx, ch); err != nil {
			t.Fatal(err)
		}
	}

	all, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("ListAll 应含停用渠道, got %d", len(all))
	}
	// 按 priority 升序：停用的 priority=5 在前
	if all[0].Name != "off" || all[1].Name != "on" {
		t.Errorf("排序错误: %s, %s", all[0].Name, all[1].Name)
	}

	// 空库返回空切片（非 nil）
	emptyRepo := repository.NewChannelRepository(NewTestDB(t))
	empty, err := emptyRepo.ListAll(ctx)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("空库 ListAll = %v, %v", empty, err)
	}
}

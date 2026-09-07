package repository_test

import (
	"context"
	"testing"
	"time"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/pagination"
	"xtokenhub/internal/repository"
)

func TestAPIKeyCRUD(t *testing.T) {
	db := NewTestDB(t)
	repo := repository.NewAPIKeyRepository(db)
	ctx := context.Background()

	k := &model.APIKey{Name: "agent-a", Key: "sk-xt-aaa", Status: model.KeyEnabled, Remark: "测试"}
	if err := repo.Create(ctx, k); err != nil {
		t.Fatal(err)
	}
	if k.ID == 0 {
		t.Fatal("id 未回填")
	}

	// Get / GetByName / GetByKey
	got, err := repo.Get(ctx, k.ID)
	if err != nil || got.Name != "agent-a" {
		t.Fatalf("get: %v %+v", err, got)
	}
	if byName, err := repo.GetByName(ctx, "agent-a"); err != nil || byName.ID != k.ID {
		t.Fatalf("getByName: %v", err)
	}
	if byKey, err := repo.GetByKey(ctx, "sk-xt-aaa"); err != nil || byKey.ID != k.ID {
		t.Fatalf("getByKey: %v", err)
	}
	if _, err := repo.GetByKey(ctx, "sk-xt-missing"); err == nil {
		t.Error("不存在的 key 应报错")
	}

	// Update
	got.Status = model.KeyDisabled
	got.Remark = "停用"
	if err := repo.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	after, _ := repo.Get(ctx, k.ID)
	if after.Status != model.KeyDisabled || after.Remark != "停用" {
		t.Errorf("update 未生效: %+v", after)
	}

	// List（非 nil 空数组约定）
	items, total, err := repo.List(ctx, pagination.Normalize(1, 10))
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("list: %v total=%d len=%d", err, total, len(items))
	}

	// Delete + 复查
	if err := repo.Delete(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, k.ID); err == nil {
		t.Error("删除后应查不到")
	}
	if items2, total2, _ := repo.List(ctx, pagination.Normalize(1, 10)); items2 == nil || total2 != 0 {
		t.Error("空列表应返回非 nil 切片")
	}
}

func TestAPIKeyUnique(t *testing.T) {
	db := NewTestDB(t)
	repo := repository.NewAPIKeyRepository(db)
	ctx := context.Background()

	if err := repo.Create(ctx, &model.APIKey{Name: "dup", Key: "sk-xt-dup"}); err != nil {
		t.Fatal(err)
	}
	// 重名
	if err := repo.Create(ctx, &model.APIKey{Name: "dup", Key: "sk-xt-other"}); err == nil {
		t.Error("重名应报唯一约束错误")
	}
	// key 原文重复
	if err := repo.Create(ctx, &model.APIKey{Name: "other", Key: "sk-xt-dup"}); err == nil {
		t.Error("重复 key 应报唯一约束错误")
	}
}

func TestLogFilterByKeyID(t *testing.T) {
	db := NewTestDB(t)
	repo := repository.NewRequestLogRepository(db)
	ctx := context.Background()

	mk := func(keyID int64, keyName string) model.RequestLog {
		return model.RequestLog{Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough,
			Model: "gpt-4o", KeyID: keyID, KeyName: keyName, PromptTokens: 10, CompletionTokens: 5}
	}
	seedLogs(t, repo, []model.RequestLog{mk(1, "agent-a"), mk(1, "agent-a"), mk(2, "agent-b"), mk(0, "")})

	items, total, err := repo.List(ctx, repository.LogFilter{KeyID: 1}, pagination.Normalize(1, 10))
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("按 key 筛选: %v total=%d", err, total)
	}
	for _, it := range items {
		if it.KeyID != 1 {
			t.Errorf("混入其他 key: %+v", it)
		}
	}
}

func TestGroupByKey(t *testing.T) {
	db := NewTestDB(t)
	repo := repository.NewRequestLogRepository(db)
	ctx := context.Background()

	mk := func(keyID int64, keyName string, prompt, compl, cached int64) model.RequestLog {
		return model.RequestLog{Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough,
			Model: "gpt-4o", KeyID: keyID, KeyName: keyName,
			PromptTokens: prompt, CompletionTokens: compl, CachedTokens: cached, DurationMS: 100}
	}
	seedLogs(t, repo, []model.RequestLog{
		mk(1, "agent-a", 100, 50, 40),
		mk(1, "agent-a", 200, 50, 0),
		mk(2, "agent-b", 10, 5, 0),
		mk(0, "", 1, 1, 0), // 匿名（未启用鉴权时的调用）
	})

	stats, err := repo.GroupByKey(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 3 {
		t.Fatalf("应为 3 组（含匿名）: %+v", stats)
	}
	byName := map[string]repository.GroupStat{}
	for _, g := range stats {
		byName[g.Name] = g
	}
	a := byName["agent-a"]
	if a.Requests != 2 || a.TotalTokens != 400 {
		t.Errorf("agent-a 聚合错误: %+v", a)
	}
	if b := byName["agent-b"]; b.Requests != 1 || b.TotalTokens != 15 {
		t.Errorf("agent-b 聚合错误: %+v", b)
	}
	if anon, ok := byName["(匿名)"]; !ok || anon.Requests != 1 {
		t.Errorf("匿名分组缺失: %+v", stats)
	}
}

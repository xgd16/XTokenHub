package repository_test

import (
	"context"
	"testing"

	"xtokenhub/internal/model"
	"xtokenhub/internal/repository"
	"xtokenhub/internal/tests/testutil"
)

// TestReasoningRewriteRepoCRUD 思维档位改写规则仓储。
func TestReasoningRewriteRepoCRUD(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	repo := repository.NewReasoningRewriteRepository(db)
	ctx := context.Background()

	rule := &model.ReasoningRewrite{
		Model: "mimo-v2.6-pro",
		Mapping: model.ReasoningMappingList{
			{Level: "none", Target: model.ReasoningOff},
			{Level: "low", Target: model.ReasoningOff},
			{Level: "max", Target: model.ReasoningOn},
		},
	}
	if err := repo.Create(ctx, rule); err != nil {
		t.Fatalf("create: %v", err)
	}

	// GetByModel 精确匹配
	got, err := repo.GetByModel(ctx, "mimo-v2.6-pro")
	if err != nil || got.TargetFor("low") != model.ReasoningOff || got.TargetFor("max") != model.ReasoningOn {
		t.Fatalf("GetByModel: %v %+v", err, got)
	}
	if _, err := repo.GetByModel(ctx, "不存在"); err == nil {
		t.Fatal("不存在的模型应返回错误")
	}

	// 模型名唯一
	dup := &model.ReasoningRewrite{Model: "mimo-v2.6-pro",
		Mapping: model.ReasoningMappingList{{Level: "low", Target: model.ReasoningOn}}}
	if err := repo.Create(ctx, dup); err == nil {
		t.Fatal("重复模型名应违反唯一约束")
	}

	// 更新
	got.Missing = model.ReasoningOff
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	got2, _ := repo.GetByModel(ctx, "mimo-v2.6-pro")
	if got2.Missing != model.ReasoningOff {
		t.Fatalf("update 未生效: %+v", got2)
	}

	// 分页 + 模糊过滤
	for _, m := range []string{"mimo-v2.5", "glm-5.3"} {
		if err := repo.Create(ctx, &model.ReasoningRewrite{Model: m,
			Mapping: model.ReasoningMappingList{{Level: "low", Target: model.ReasoningOn}}}); err != nil {
			t.Fatalf("seed %s: %v", m, err)
		}
	}
	items, total, err := repo.ListPaged(ctx, "mimo", 0, 10)
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("ListPaged(q=mimo): %v total=%d n=%d", err, total, len(items))
	}
	items, total, err = repo.ListPaged(ctx, "", 0, 10)
	if err != nil || total != 3 {
		t.Fatalf("ListPaged(全部): %v total=%d", err, total)
	}

	// 删除
	if err := repo.Delete(ctx, got.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.GetByModel(ctx, "mimo-v2.6-pro"); err == nil {
		t.Fatal("删除后应查不到")
	}
}

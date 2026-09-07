package service_test

import (
	"context"
	"strings"
	"testing"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/pagination"
	"xtokenhub/internal/repository"
	"xtokenhub/internal/service"
	"xtokenhub/internal/tests/testutil"
)

func TestGenerateKeyFormat(t *testing.T) {
	raw, err := service.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, model.KeyPrefix) {
		t.Errorf("前缀错误: %s", raw)
	}
	if len(raw) != len(model.KeyPrefix)+32 {
		t.Errorf("长度异常: %s (%d)", raw, len(raw))
	}
	// 随机性
	other, _ := service.GenerateKey()
	if raw == other {
		t.Error("两次生成不应相同")
	}
}

func TestKeyServiceCRUD(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	svc := service.NewKeyService(repository.NewAPIKeyRepository(db))
	ctx := context.Background()

	// Create：服务端生成 key
	k, err := svc.Create(ctx, &service.KeyInput{Name: "claude-code", Remark: "主力 agent"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(k.Key, model.KeyPrefix) || k.Status != model.KeyEnabled {
		t.Errorf("create 结果异常: %+v", k)
	}

	// 重名拒绝
	if _, err := svc.Create(ctx, &service.KeyInput{Name: "claude-code"}); err == nil {
		t.Error("重名应被拒绝")
	}

	// List
	items, total, err := svc.List(ctx, pagination.Normalize(1, 10))
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("list: %v total=%d", err, total)
	}

	// Update：改名 + 停用
	disabled := model.KeyDisabled
	up, err := svc.Update(ctx, k.ID, &service.KeyInput{Name: "claude-code-2", Status: &disabled, Remark: "临时停用"})
	if err != nil {
		t.Fatal(err)
	}
	if up.Name != "claude-code-2" || up.Status != model.KeyDisabled {
		t.Errorf("update 未生效: %+v", up)
	}

	// key 本体不可改：更新后 key 原文不变
	if up.Key != k.Key {
		t.Error("key 原文不应被更新改变")
	}

	// Delete
	if err := svc.Delete(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, k.ID); err == nil {
		t.Error("删除后应查不到")
	}
}

func TestKeyServiceCheck(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	svc := service.NewKeyService(repository.NewAPIKeyRepository(db))
	ctx := context.Background()

	k, err := svc.Create(ctx, &service.KeyInput{Name: "agent-a"})
	if err != nil {
		t.Fatal(err)
	}

	// 启用 -> 通过
	got, err := svc.Check(ctx, k.Key)
	if err != nil || got.ID != k.ID {
		t.Fatalf("check 启用 key: %v", err)
	}

	// 不存在 -> 拒绝
	if _, err := svc.Check(ctx, model.KeyPrefix+"ffffffffffffffffffffffffffffffff"); err == nil {
		t.Error("未知 key 应被拒绝")
	}
	if _, err := svc.Check(ctx, ""); err == nil {
		t.Error("空 key 应被拒绝")
	}

	// 停用 -> 拒绝
	disabled := model.KeyDisabled
	if _, err := svc.Update(ctx, k.ID, &service.KeyInput{Name: k.Name, Status: &disabled}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Check(ctx, k.Key); err == nil {
		t.Error("停用 key 应被拒绝")
	}
}

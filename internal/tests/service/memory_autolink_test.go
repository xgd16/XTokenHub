package service_test

import (
	"context"
	"testing"

	"xtokenhub/internal/model"
	"xtokenhub/internal/service"
)

// 语义近似的一对样例（部署主题），用于自动关联正例。
const (
	autoLinkDocA = "## 部署流程\n\n本机 make build GOOS=linux GOARCH=arm64 交叉编译（前端自动打进单二进制），scp 二进制与 deploy/install.sh 到目标机 192.168.1.110 后执行 sh install.sh 原子替换并重启 systemd 单元 xtokenhub.service。"
	autoLinkDocB = "## 部署补记\n\n目标机 192.168.1.110 的部署根在 /home/user/code/XTokenHub；重新部署用 make build GOOS=linux GOARCH=arm64 构建，再跑 deploy/install.sh 完成原子替换与 xtokenhub.service 重启。"
)

// TestMemoryAutoLinkOnAdd 相似内容写入后自动建立 related 出边（created_by=system）。
func TestMemoryAutoLinkOnAdd(t *testing.T) {
	svc, _, _, _ := newMemoryEnv(t)

	first := addMemory(t, svc, autoLinkDocA, "ops")
	res := addMemory(t, svc, autoLinkDocB, "ops")
	if len(res.AutoLinks) != 1 {
		t.Fatalf("期望自动关联 1 条，得到 %d: %+v", len(res.AutoLinks), res.AutoLinks)
	}
	l := res.AutoLinks[0]
	if l.FromID != res.Memory.ID || l.ToID != first.Memory.ID {
		t.Errorf("自动边方向错误：from=%d to=%d（期望 %d→%d）", l.FromID, l.ToID, res.Memory.ID, first.Memory.ID)
	}
	if l.Relation != model.MemoryLinkRelated {
		t.Errorf("自动边类型应为 related，得到 %s", l.Relation)
	}
	if l.CreatedBy != string(model.MemorySourceSystem) {
		t.Errorf("自动边建边者应为 system，得到 %s", l.CreatedBy)
	}
}

// TestMemoryAutoLinkSkipsUnrelated 同命名空间但主题无关的记忆不自动关联。
func TestMemoryAutoLinkSkipsUnrelated(t *testing.T) {
	svc, _, _, _ := newMemoryEnv(t)

	addMemory(t, svc, "PostgreSQL 连接池调优：max_open_conns 设为 20，空闲连接 5 分钟回收，慢查询阈值 200ms。", "mixed")
	res := addMemory(t, svc, "封面图规格：输出 WebP 品质 82，宽度 1200 像素，圆角 12 像素，夜间模式叠加暗色遮罩。", "mixed")
	if len(res.AutoLinks) != 0 {
		t.Fatalf("无关记忆不应自动关联，得到 %+v", res.AutoLinks)
	}
}

// TestMemoryAutoLinkSameNamespaceOnly 自动关联只发生在同命名空间内。
func TestMemoryAutoLinkSameNamespaceOnly(t *testing.T) {
	svc, _, _, _ := newMemoryEnv(t)

	addMemory(t, svc, autoLinkDocA, "ops")
	res := addMemory(t, svc, autoLinkDocB, "other")
	if len(res.AutoLinks) != 0 {
		t.Fatalf("跨命名空间不应自动关联，得到 %+v", res.AutoLinks)
	}
}

// TestMemoryAutoLinkLimit 单条记忆自动关联数不超过上限（3 条）。
func TestMemoryAutoLinkLimit(t *testing.T) {
	svc, _, _, _ := newMemoryEnv(t)

	for i := 0; i < 4; i++ {
		addMemory(t, svc, "## 部署流程 分册"+string(rune('A'+i))+"\n\n"+autoLinkDocA, "ops")
	}
	res := addMemory(t, svc, "## 部署流程 汇总\n\n"+autoLinkDocA, "ops")
	if len(res.AutoLinks) != 3 {
		t.Fatalf("自动关联应截断为 3 条，得到 %d: %+v", len(res.AutoLinks), res.AutoLinks)
	}
}

// TestMemoryAutoLinkSkipsPairWithExistingEdge 已有显式边的点对不重复自动建边。
func TestMemoryAutoLinkSkipsPairWithExistingEdge(t *testing.T) {
	svc, _, _, _ := newMemoryEnv(t)

	first := addMemory(t, svc, autoLinkDocA, "ops")
	res := addMemory(t, svc, autoLinkDocB, "ops", func(in *service.MemoryAddInput) {
		in.Links = []service.MemoryLinkInput{{ToID: first.Memory.ID, Relation: "related", Note: "显式关系"}}
	})
	for _, l := range res.AutoLinks {
		if l.ToID == first.Memory.ID {
			t.Fatalf("已有点对（显式边）不应再自动建边：%+v", l)
		}
	}
}

// TestMemoryUpdateRefreshAutoLinks 内容/标题/归属变化时重算自动边；显式边不受影响。
func TestMemoryUpdateRefreshAutoLinks(t *testing.T) {
	svc, _, _, links := newMemoryEnv(t)
	ctx := context.Background()

	first := addMemory(t, svc, autoLinkDocA, "ops")
	second := addMemory(t, svc, autoLinkDocB, "ops")
	if len(second.AutoLinks) != 1 {
		t.Fatalf("前置：应自动建边 1 条，得到 %+v", second.AutoLinks)
	}

	// 内容改成完全无关主题 → 旧自动边应被回收。
	wineTitle := "葡萄酒品鉴笔记"
	wineContent := "赤霞珠单宁厚重，适合搭配红肉；雷司令偏酸，配海鲜更佳。"
	if _, err := svc.Update(ctx, second.Memory.ID, service.MemoryUpdateInput{Title: &wineTitle, Content: &wineContent}); err != nil {
		t.Fatalf("更新记忆: %v", err)
	}
	for _, l := range mustLinks(t, links, second.Memory.ID) {
		if l.CreatedBy == string(model.MemorySourceSystem) {
			t.Fatalf("旧自动边应被回收，仍在：%+v", l)
		}
	}

	// 再改回部署主题 → 自动边应重建到 first。
	opsTitle := "部署流程重写"
	opsContent := "目标机 192.168.1.110 上用 deploy/install.sh 原子替换二进制并重启 xtokenhub.service；构建命令 make build GOOS=linux GOARCH=arm64。"
	if _, err := svc.Update(ctx, second.Memory.ID, service.MemoryUpdateInput{Title: &opsTitle, Content: &opsContent}); err != nil {
		t.Fatalf("更新记忆: %v", err)
	}
	rebuilt := false
	for _, l := range mustLinks(t, links, second.Memory.ID) {
		if l.CreatedBy == string(model.MemorySourceSystem) && l.ToID == first.Memory.ID {
			rebuilt = true
		}
	}
	if !rebuilt {
		t.Fatalf("改回相似内容后应重建自动边（→#%d）", first.Memory.ID)
	}
}

// TestMemoryUpdateKeepsExplicitLinks 重算只回收 system 自动边，显式关系保留。
func TestMemoryUpdateKeepsExplicitLinks(t *testing.T) {
	svc, _, _, links := newMemoryEnv(t)
	ctx := context.Background()

	first := addMemory(t, svc, autoLinkDocA, "ops")
	unrelated := addMemory(t, svc, "封面图规格：输出 WebP 品质 82，宽度 1200 像素。", "ops")
	if _, err := svc.AddLink(ctx, unrelated.Memory.ID, first.Memory.ID, "related", "人工建边", "tester"); err != nil {
		t.Fatalf("显式建边: %v", err)
	}
	content := "更新后的无关内容：葡萄酒单宁与酸度平衡。"
	if _, err := svc.Update(ctx, unrelated.Memory.ID, service.MemoryUpdateInput{Content: &content}); err != nil {
		t.Fatalf("更新记忆: %v", err)
	}
	kept := false
	for _, l := range mustLinks(t, links, unrelated.Memory.ID) {
		if l.CreatedBy == "tester" {
			kept = true
		}
	}
	if !kept {
		t.Fatal("显式边不应被自动重算回收")
	}
}

// TestMemoryRebuildAutoLinks 全库重算：被删掉的自动边会补回。
func TestMemoryRebuildAutoLinks(t *testing.T) {
	svc, _, _, links := newMemoryEnv(t)
	ctx := context.Background()

	addMemory(t, svc, autoLinkDocA, "ops")
	second := addMemory(t, svc, autoLinkDocB, "ops")
	if len(second.AutoLinks) != 1 {
		t.Fatalf("前置：应自动建边 1 条，得到 %+v", second.AutoLinks)
	}
	if err := links.Delete(ctx, second.AutoLinks[0].ID); err != nil {
		t.Fatalf("删除自动边: %v", err)
	}
	res, err := svc.RebuildAutoLinks(ctx)
	if err != nil {
		t.Fatalf("全库重算: %v", err)
	}
	if res.Processed != 2 || res.Created < 1 {
		t.Fatalf("重算统计异常：processed=%d created=%d", res.Processed, res.Created)
	}
	found := false
	for _, l := range mustLinks(t, links, second.Memory.ID) {
		if l.CreatedBy == string(model.MemorySourceSystem) && l.Relation == model.MemoryLinkRelated {
			found = true
		}
	}
	if !found {
		t.Fatalf("重算后应补回自动边，edges=%+v", mustLinks(t, links, second.Memory.ID))
	}
}

// TestMemoryAutoLinkDuplicateNoop 重复内容命中去重分支，不再重复建边。
func TestMemoryAutoLinkDuplicateNoop(t *testing.T) {
	svc, _, _, _ := newMemoryEnv(t)

	addMemory(t, svc, autoLinkDocA, "ops")
	res := addMemory(t, svc, autoLinkDocA, "ops")
	if !res.Duplicated {
		t.Fatal("同内容二次写入应命中去重")
	}
	if len(res.AutoLinks) != 0 {
		t.Fatalf("重复写入不应再建边：%+v", res.AutoLinks)
	}
}

// mustLinks 读取某条记忆的全部边（测试辅助）。
func mustLinks(t *testing.T, links interface {
	ListByMemory(ctx context.Context, id int64) ([]model.MemoryLink, error)
}, id int64) []model.MemoryLink {
	t.Helper()
	items, err := links.ListByMemory(context.Background(), id)
	if err != nil {
		t.Fatalf("读取关系边: %v", err)
	}
	return items
}

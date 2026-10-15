package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/repository"
	"xtokenhub/internal/service"
	"xtokenhub/internal/tests/testutil"
)

// newMemoryEnv 内存库 + 记忆库服务装配（时钟固定，便于断言排序）。
func newMemoryEnv(t *testing.T) (*service.MemoryService, repository.MemoryRepository, repository.MemoryNamespaceRepository, repository.MemoryLinkRepository) {
	t.Helper()
	db := testutil.NewMemoryDB(t)
	memories := repository.NewMemoryRepository(db)
	namespaces := repository.NewMemoryNamespaceRepository(db)
	links := repository.NewMemoryLinkRepository(db)
	settings := repository.NewMemorySettingsRepository(db)
	if err := settings.Save(context.Background(), &model.MemorySettings{ID: 1}); err != nil {
		t.Fatalf("播种设置行: %v", err)
	}
	svc := service.NewMemoryService(memories, namespaces, links, settings, service.MemoryOptions{})
	svc.SetClock(func() time.Time { return time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC) })
	return svc, memories, namespaces, links
}

func addMemory(t *testing.T, svc *service.MemoryService, content, ns string, opts ...func(*service.MemoryAddInput)) *service.MemoryAddResult {
	t.Helper()
	in := service.MemoryAddInput{Content: content, Namespace: ns, Source: string(model.MemorySourceAPI), CreatedBy: "agent-a"}
	for _, o := range opts {
		o(&in)
	}
	res, err := svc.Add(context.Background(), in)
	if err != nil {
		t.Fatalf("写入记忆: %v", err)
	}
	return res
}

func TestMemoryAddAndDedupe(t *testing.T) {
	svc, _, namespaces, _ := newMemoryEnv(t)
	ctx := context.Background()

	res := addMemory(t, svc, "网关使用 FTS5 加中文 bigram 做检索。", "design",
		func(in *service.MemoryAddInput) {
			in.Kind = string(model.MemoryKindDecision)
			in.Tags = []string{"检索", "记忆"}
			in.Importance = 4
		})
	m := res.Memory
	if m.ID == 0 || res.Duplicated {
		t.Fatalf("首次写入: %+v", m)
	}
	if m.Title == "" || m.Kind != model.MemoryKindDecision || m.Importance != 4 {
		t.Fatalf("字段落库异常: %+v", m)
	}
	if m.NamespaceSource != model.NamespaceSourceExplicit {
		t.Fatalf("显式归属来源 = %q", m.NamespaceSource)
	}
	ns, err := namespaces.GetByName(ctx, "design")
	if err != nil || ns.CreatedBy != string(model.MemorySourceAPI) {
		t.Fatalf("命名空间未自动创建: %v %+v", err, ns)
	}

	// 同命名空间同内容（空白/大小写差异）去重。
	dup := addMemory(t, svc, "  网关使用  FTS5 加中文 bigram 做检索。 ", "design")
	if !dup.Duplicated || dup.Memory.ID != m.ID {
		t.Fatalf("去重失败: %+v", dup.Memory)
	}
	// 其他命名空间不参与去重。
	other := addMemory(t, svc, "网关使用 FTS5 加中文 bigram 做检索。", "ops")
	if other.Duplicated || other.Memory.ID == m.ID {
		t.Fatal("不同命名空间不应去重")
	}
	// 未指定命名空间 = 未分类。
	plain := addMemory(t, svc, "未分类的记忆内容。", "")
	if plain.Memory.Namespace != "" || plain.Memory.NamespaceSource != model.NamespaceSourceNone {
		t.Fatalf("未分类记忆 = %+v", plain.Memory)
	}
}

func TestMemoryAddValidation(t *testing.T) {
	svc, memories, _, _ := newMemoryEnv(t)
	ctx := context.Background()

	tooManyTags := make([]string, 0, 17)
	for i := 0; i < 17; i++ {
		tooManyTags = append(tooManyTags, fmt.Sprintf("tag%d", i))
	}
	cases := []struct {
		name     string
		in       service.MemoryAddInput
		wantCode int
	}{
		{"空内容", service.MemoryAddInput{Content: "  "}, errs.CodeInvalidParams},
		{"非法类型", service.MemoryAddInput{Content: "x", Kind: "unknown"}, errs.CodeInvalidParams},
		{"非法命名空间名", service.MemoryAddInput{Content: "x", Namespace: "none"}, errs.CodeInvalidParams},
		{"命名空间名 all 保留", service.MemoryAddInput{Content: "x", Namespace: "all"}, errs.CodeInvalidParams},
		{"标签过多", service.MemoryAddInput{Content: "x", Tags: tooManyTags}, errs.CodeInvalidParams},
		{"链接目标不存在", service.MemoryAddInput{Content: "x", Links: []service.MemoryLinkInput{{ToID: 9999}}}, errs.CodeNotFound},
		{"链接关系非法", service.MemoryAddInput{Content: "x", Links: []service.MemoryLinkInput{{ToID: 1, Relation: "bogus"}}}, errs.CodeInvalidParams},
		{"替代目标不存在", service.MemoryAddInput{Content: "x", Supersedes: []int64{9999}}, errs.CodeNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Add(ctx, tc.in); err == nil || errs.From(err).Code != tc.wantCode {
				t.Fatalf("want code=%d, got %v", tc.wantCode, err)
			}
		})
	}
	// 关联项校验失败不得留下半写入。
	items, total, err := memories.List(ctx, repository.MemoryFilter{}, "", 0, 10)
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("校验失败不应落库: total=%d items=%+v err=%v", total, items, err)
	}
}

func TestMemorySearchDegradeAndRank(t *testing.T) {
	svc, memories, _, _ := newMemoryEnv(t)
	ctx := context.Background()

	// 旧记忆（30 天前更新，低重要度）。
	old := addMemory(t, svc, "缓存 命中率 策略 设计", "ops", func(in *service.MemoryAddInput) {
		in.Importance = 1
	}).Memory
	stale := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	old.UpdatedAt = stale
	if err := memories.Update(ctx, old); err != nil {
		t.Fatal(err)
	}
	// 新记忆（同词、高重要度、置顶）。
	fresh := addMemory(t, svc, "缓存 命中率 记录", "ops", func(in *service.MemoryAddInput) {
		in.Importance = 5
		in.Pinned = true
	}).Memory

	res, err := svc.Search(ctx, service.MemoryQuery{Q: "缓存", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != "fts" || res.Count != 2 {
		t.Fatalf("fts 检索: mode=%s count=%d", res.Mode, res.Count)
	}
	if res.Items[0].ID != fresh.ID {
		t.Fatalf("置顶+高重要度+新近应排前: got %d want %d", res.Items[0].ID, fresh.ID)
	}
	// 命中后访问统计已刷新（检索即强化）。
	if got, _ := memories.Get(ctx, fresh.ID); got.AccessCount != 1 {
		t.Fatalf("访问强化未生效: %d", got.AccessCount)
	}
	// 命名空间命中分布。
	if len(res.Namespaces) != 1 || res.Namespaces[0].Namespace != "ops" || res.Namespaces[0].Count != 2 {
		t.Fatalf("facets = %+v", res.Namespaces)
	}

	// 单个汉字走 LIKE 兜底。
	res, err = svc.Search(ctx, service.MemoryQuery{Q: "缓", Limit: 10})
	if err != nil || res.Mode != "like" || res.Count != 2 {
		t.Fatalf("单字兜底: err=%v mode=%s count=%d", err, res.Mode, res.Count)
	}
	// 无匹配词：0 结果且不报错。
	res, err = svc.Search(ctx, service.MemoryQuery{Q: "不存在的词", Limit: 10})
	if err != nil || res.Count != 0 {
		t.Fatalf("无结果检索: err=%v count=%d", err, res.Count)
	}
	// 浏览模式。
	res, err = svc.Search(ctx, service.MemoryQuery{Limit: 10})
	if err != nil || res.Mode != "list" || res.Count != 2 {
		t.Fatalf("浏览模式: err=%v mode=%s count=%d", err, res.Mode, res.Count)
	}
	// 未分类过滤（保留值 none）。
	res, _ = svc.Search(ctx, service.MemoryQuery{Q: "缓存", Namespace: "none", Limit: 10})
	if res.Count != 0 {
		t.Fatalf("未分类过滤应 0 条, got %d", res.Count)
	}
	// 非法 status。
	if _, err := svc.Search(ctx, service.MemoryQuery{Status: "bogus"}); err == nil {
		t.Fatal("非法 status 应报错")
	}
}

func TestMemorySearchLooseFallback(t *testing.T) {
	svc, _, _, _ := newMemoryEnv(t)
	addMemory(t, svc, "支付回调 需要使用 幂等 处理", "pay")
	addMemory(t, svc, "支付回调 需要签名校验", "pay")

	// 需要 AND 全部命中：只有第一条同时含「幂等」与「签名校验」→ 严格 0 命中，宽松命中。
	res, err := svc.Search(context.Background(), service.MemoryQuery{Q: "幂等 签名校验", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != "fts_loose" || res.Count != 2 {
		t.Fatalf("宽松降级: mode=%s count=%d", res.Mode, res.Count)
	}
}

func TestMemorySupersedeAndForget(t *testing.T) {
	svc, memories, _, links := newMemoryEnv(t)
	ctx := context.Background()

	oldMem := addMemory(t, svc, "回调超时时间设为 5s", "pay").Memory
	res := addMemory(t, svc, "回调超时时间改为 30s", "pay", func(in *service.MemoryAddInput) {
		in.Supersedes = []int64{oldMem.ID}
	})
	if len(res.Superseded) != 1 {
		t.Fatalf("supersedes 未执行: %+v", res)
	}
	got, _ := memories.Get(ctx, oldMem.ID)
	if got.Status != model.MemoryStatusSuperseded || got.SupersededBy != res.Memory.ID {
		t.Fatalf("旧记忆未置为被替代: %+v", got)
	}
	edge, _ := links.ListByMemory(ctx, oldMem.ID)
	if len(edge) != 1 || edge[0].FromID != res.Memory.ID || edge[0].ToID != oldMem.ID ||
		edge[0].Relation != model.MemoryLinkSupersedes {
		t.Fatalf("supersedes 边方向应为 新→旧: %+v", edge)
	}
	// 默认检索不再命中被替代记忆。
	search, _ := svc.Search(ctx, service.MemoryQuery{Q: "回调超时时间", Limit: 10})
	if search.Count != 1 || search.Items[0].ID != res.Memory.ID {
		t.Fatalf("被替代记忆不应出现在默认检索: %+v", search.Items)
	}
	// status=all 可见。
	search, _ = svc.Search(ctx, service.MemoryQuery{Q: "回调超时时间", Status: "all", Limit: 10})
	if search.Count != 2 {
		t.Fatalf("status=all 应可见两条, got %d", search.Count)
	}

	// 替代自身 / 非法替代者。
	if err := svc.Supersede(ctx, oldMem.ID, oldMem.ID, "agent-a"); err == nil {
		t.Fatal("不能替代自身")
	}
	if err := svc.Supersede(ctx, oldMem.ID, 9999, "agent-a"); err == nil {
		t.Fatal("替代者不存在应报错")
	}

	// 软删除。
	if err := svc.Forget(ctx, res.Memory.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = memories.Get(ctx, res.Memory.ID)
	if got.Status != model.MemoryStatusDeleted {
		t.Fatalf("软删除状态 = %s", got.Status)
	}
	search, _ = svc.Search(ctx, service.MemoryQuery{Q: "回调超时时间", Limit: 10})
	if search.Count != 0 {
		t.Fatalf("软删除后检索应为空, got %d", search.Count)
	}
	// 「全部状态」= 生效 + 被替代 + 归档，**不含已删除**：回收站只由 status=deleted 查看。
	// 曾经 status=all 等于完全不过滤，把软删除记录一并带出（比 Stats.Total 还多）。
	search, _ = svc.Search(ctx, service.MemoryQuery{Q: "回调超时时间", Status: "all", Limit: 10})
	if search.Count != 1 || search.Items[0].ID != oldMem.ID {
		t.Fatalf("status=all 不应包含已删除, got %d (%+v)", search.Count, search.Items)
	}
	search, _ = svc.Search(ctx, service.MemoryQuery{Q: "回调超时时间", Status: "deleted", Limit: 10})
	if search.Count != 1 || search.Items[0].ID != res.Memory.ID {
		t.Fatalf("status=deleted 应只返回回收站记录, got %d (%+v)", search.Count, search.Items)
	}
}

// TestMemoryStatsConsistency 统计口径自洽性（回归：前端「总量 15」但实际只有 14）。
//
// 三处口径必须一致：Stats.Total == 未分类 + Σ 命名空间 == 默认列表条数。
// 曾经 Total 把 superseded/archived 也计入，于是「记忆总量」比命名空间卡片之和多，
// 而「全部记忆」卡片写着 Total、点进去只列出 Active 条。
func TestMemoryStatsConsistency(t *testing.T) {
	svc, memories, _, _ := newMemoryEnv(t)
	ctx := context.Background()

	addMemory(t, svc, "活跃记忆甲", "pay")
	oldMem := addMemory(t, svc, "会被替代的记忆乙", "pay").Memory
	addMemory(t, svc, "未分类的记忆丙", "")
	// 造一条被替代 + 一条归档，使库里同时存在四种状态
	addMemory(t, svc, "推翻旧结论的记忆丁", "pay", func(in *service.MemoryAddInput) {
		in.Supersedes = []int64{oldMem.ID}
	})
	arch := addMemory(t, svc, "归档的记忆戊", "pay").Memory
	arched, err := memories.Get(ctx, arch.ID)
	if err != nil {
		t.Fatal(err)
	}
	arched.Status = model.MemoryStatusArchived
	if err := memories.Update(ctx, arched); err != nil {
		t.Fatal(err)
	}

	st, err := svc.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Superseded != 1 || st.Archived != 1 {
		t.Fatalf("前置条件不成立（应有 1 被替代 + 1 归档）: %+v", st)
	}

	sum := st.Uncategorized
	for _, n := range st.ByNamespace {
		sum += n
	}
	if st.Total != sum {
		t.Fatalf("Total(%d) 应等于 未分类(%d)+Σ命名空间(%d): %+v", st.Total, st.Uncategorized, sum, st)
	}

	// 默认列表（status 空 = active）条数必须等于 Total
	_, activeTotal, err := svc.List(ctx, service.MemoryQuery{Limit: 100}, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if activeTotal != st.Total {
		t.Fatalf("默认列表 %d 条应等于 Total %d: %+v", activeTotal, st.Total, st)
	}
	// 「全部状态」= Total + 被替代 + 归档，且不含已删除
	_, allTotal, err := svc.List(ctx, service.MemoryQuery{Status: "all", Limit: 100}, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := st.Total + st.Superseded + st.Archived
	if allTotal != want {
		t.Fatalf("「全部状态」应为 %d 条（生效+被替代+归档），实际 %d: %+v", want, allTotal, st)
	}
}

func TestMemoryUpdateAndBatchMove(t *testing.T) {
	svc, memories, _, _ := newMemoryEnv(t)
	ctx := context.Background()

	m := addMemory(t, svc, "原始内容", "", func(in *service.MemoryAddInput) {
		in.Kind = string(model.MemoryKindNote)
	}).Memory

	title := "新标题"
	ns := "ops"
	imp := 9
	st := string(model.MemoryStatusArchived)
	updated, err := svc.Update(ctx, m.ID, service.MemoryUpdateInput{
		Title: &title, Namespace: &ns, Importance: &imp, Status: &st,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "新标题" || updated.Namespace != "ops" || updated.Importance != 5 ||
		updated.NamespaceSource != model.NamespaceSourceManual || updated.Status != model.MemoryStatusArchived {
		t.Fatalf("更新结果异常: %+v", updated)
	}
	// status 只允许 active/archived。
	bad := string(model.MemoryStatusDeleted)
	if _, err := svc.Update(ctx, m.ID, service.MemoryUpdateInput{Status: &bad}); err == nil {
		t.Fatal("PATCH 不允许直接置 deleted")
	}
	// 更新正文会重算哈希与标题缺省。
	content := "全新内容首行\n第二行"
	updated, err = svc.Update(ctx, m.ID, service.MemoryUpdateInput{Content: &content})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Content != content || updated.Title != "新标题" {
		t.Fatalf("正文更新异常: %+v", updated)
	}

	// 批量移动回未分类。
	moved, err := svc.BatchMoveNamespace(ctx, []int64{m.ID, 9999}, "")
	if err != nil || moved != 1 {
		t.Fatalf("批量移动 moved=%d err=%v", moved, err)
	}
	got, _ := memories.Get(ctx, m.ID)
	if got.Namespace != "" || got.NamespaceSource != model.NamespaceSourceNone {
		t.Fatalf("批量移动结果: %+v", got)
	}
	if _, err := svc.BatchMoveNamespace(ctx, nil, "ops"); err == nil {
		t.Fatal("空 ids 应报错")
	}
}

func TestMemoryLinksAndGraph(t *testing.T) {
	svc, _, _, _ := newMemoryEnv(t)
	ctx := context.Background()

	a := addMemory(t, svc, "节点 A 内容", "design").Memory
	b := addMemory(t, svc, "节点 B 内容", "design").Memory
	c := addMemory(t, svc, "节点 C 内容", "").Memory

	link, err := svc.AddLink(ctx, a.ID, b.ID, "related", "同主题", "agent-a")
	if err != nil {
		t.Fatal(err)
	}
	if again, err := svc.AddLink(ctx, a.ID, b.ID, "related", "", "agent-a"); err != nil || again.ID != link.ID {
		t.Fatalf("建边应幂等: %v %+v", err, again)
	}
	if _, err := svc.AddLink(ctx, a.ID, a.ID, "", "", ""); err == nil {
		t.Fatal("自环应报错")
	}
	if _, err := svc.AddLink(ctx, a.ID, b.ID, "bogus", "", ""); err == nil {
		t.Fatal("非法关系应报错")
	}
	if _, err := svc.AddLink(ctx, a.ID, 9999, "", "", ""); err == nil {
		t.Fatal("目标不存在应报错")
	}

	g, err := svc.Graph(ctx, service.MemoryQuery{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 3 || len(g.Edges) != 1 || g.Truncated {
		t.Fatalf("图数据: nodes=%d edges=%d truncated=%v", len(g.Nodes), len(g.Edges), g.Truncated)
	}
	deg := map[int64]int{}
	for _, n := range g.Nodes {
		deg[n.ID] = n.Degree
	}
	if deg[a.ID] != 1 || deg[b.ID] != 1 || deg[c.ID] != 0 {
		t.Fatalf("度数统计异常: %+v", deg)
	}
	// 限 1 个节点 → 裁剪且无边。
	g, _ = svc.Graph(ctx, service.MemoryQuery{}, 1)
	if len(g.Nodes) != 1 || !g.Truncated || len(g.Edges) != 0 {
		t.Fatalf("裁剪异常: %+v", g)
	}
	// 按命名空间过滤。
	g, _ = svc.Graph(ctx, service.MemoryQuery{Namespace: "design"}, 10)
	if len(g.Nodes) != 2 {
		t.Fatalf("命名空间过滤后节点数 = %d", len(g.Nodes))
	}

	if err := svc.RemoveLink(ctx, link.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveLink(ctx, link.ID); err == nil {
		t.Fatal("重复删除应报错")
	}
}

func TestMemoryNamespacesAndStats(t *testing.T) {
	svc, _, _, _ := newMemoryEnv(t)
	ctx := context.Background()

	addMemory(t, svc, "设计记忆一", "design")
	addMemory(t, svc, "设计记忆二", "design")
	addMemory(t, svc, "未分类记忆", "")

	views, err := svc.Namespaces(ctx)
	if err != nil || len(views) != 1 || views[0].Name != "design" || views[0].Count != 2 {
		t.Fatalf("命名空间视图: err=%v %+v", err, views)
	}
	// 手工新建 / 改名 / 更新描述。
	created, err := svc.CreateNamespace(ctx, "ops", "运维相关")
	if err != nil || created.Name != "ops" {
		t.Fatalf("新建命名空间: %v %+v", err, created)
	}
	if _, err := svc.CreateNamespace(ctx, "ops", ""); err == nil {
		t.Fatal("重复新建应报错")
	}
	if _, err := svc.UpdateNamespace(ctx, created.ID, "运维与部署"); err != nil {
		t.Fatal(err)
	}
	if moved, err := svc.RenameNamespace(ctx, "ops", "ops2"); err != nil || moved != 0 {
		t.Fatalf("改名 moved=%d err=%v", moved, err)
	}
	if moved, err := svc.MergeNamespaces(ctx, "ops2", "design"); err != nil || moved != 0 {
		t.Fatalf("合并 moved=%d err=%v", moved, err)
	}
	// 删除命名空间：记忆回落未分类。
	views, _ = svc.Namespaces(ctx)
	if len(views) != 1 {
		t.Fatalf("namespaces = %+v", views)
	}
	if moved, err := svc.DeleteNamespace(ctx, views[0].ID); err != nil || moved != 2 {
		t.Fatalf("删除命名空间 moved=%d err=%v", moved, err)
	}
	st, err := svc.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Active != 3 || st.Uncategorized != 3 || st.Total != 3 {
		t.Fatalf("统计: %+v", st)
	}
}

func TestMemoryClassifyConfig(t *testing.T) {
	svc, _, _, _ := newMemoryEnv(t)
	ctx := context.Background()

	cfg, err := svc.ClassifyConfig(ctx)
	if err != nil || cfg.Status != "" || cfg.ChannelID != 0 {
		t.Fatalf("初始配置: err=%v %+v", err, cfg)
	}
	if err := svc.UpdateClassifyConfig(ctx, 3, " mimo-v2 "); err != nil {
		t.Fatal(err)
	}
	cfg, _ = svc.ClassifyConfig(ctx)
	if cfg.ChannelID != 3 || cfg.Model != "mimo-v2" {
		t.Fatalf("配置写入异常: %+v", cfg)
	}
	if err := svc.UpdateClassifyConfig(ctx, 0, "x"); err == nil {
		t.Fatal("缺渠道应报错")
	}
	if err := svc.UpdateClassifyConfig(ctx, 1, ""); err == nil {
		t.Fatal("缺模型应报错")
	}
}

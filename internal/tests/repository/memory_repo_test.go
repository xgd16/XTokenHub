package repository_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/textseg"
	"xtokenhub/internal/repository"
)

// memoryRepos 一次性构造记忆库四个仓储。
func memoryRepos(t *testing.T, db *gorm.DB) (repository.MemoryRepository, repository.MemoryNamespaceRepository, repository.MemoryLinkRepository, repository.MemorySettingsRepository) {
	t.Helper()
	return repository.NewMemoryRepository(db),
		repository.NewMemoryNamespaceRepository(db),
		repository.NewMemoryLinkRepository(db),
		repository.NewMemorySettingsRepository(db)
}

// seedMemory 写入一条测试记忆（哈希用 content 派生的稳定串）。
func seedMemory(t *testing.T, repo repository.MemoryRepository, title, content, ns string, kind model.MemoryKind, tags ...string) *model.Memory {
	t.Helper()
	m := &model.Memory{
		Title:       title,
		Content:     content,
		Namespace:   ns,
		Kind:        kind,
		Importance:  3,
		Status:      model.MemoryStatusActive,
		Source:      model.MemorySourceAPI,
		ContentHash: "hash-" + content,
	}
	m.SetTags(tags)
	if err := repo.Create(context.Background(), m); err != nil {
		t.Fatalf("写入记忆: %v", err)
	}
	return m
}

func TestMemoryCreateGetAndFTS(t *testing.T) {
	db := NewTestDB(t)
	repo, _, _, _ := memoryRepos(t, db)
	ctx := context.Background()

	m := seedMemory(t, repo, "记忆库设计", "网关的记忆库设计：FTS5 加中文 bigram 双通道。", "design", model.MemoryKindDecision, "记忆", "检索")
	if m.ID == 0 {
		t.Fatal("id 未回填")
	}

	got, err := repo.Get(ctx, m.ID)
	if err != nil || got.Title != "记忆库设计" {
		t.Fatalf("get: %v %+v", err, got)
	}
	if _, err := repo.Get(ctx, 9999); err == nil || errs.From(err).Code != errs.CodeNotFound {
		t.Fatalf("不存在的 id 应返回 NotFound, got %v", err)
	}

	active := repository.MemoryFilter{Status: string(model.MemoryStatusActive)}
	// 两字中文查询（bigram 通道）。
	hits, err := repo.Search(ctx, textseg.BuildMatch("记忆").Strict, active, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != m.ID {
		t.Fatalf("中文两字查询应命中 1 条, got %+v", hits)
	}
	// 多字中文短语（组内相邻）。
	hits, err = repo.Search(ctx, textseg.BuildMatch("中文 bigram").Strict, active, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("多组 AND 查询应命中 1 条, got %d", len(hits))
	}
	// 单字查询无 MATCH 表达式，走 LIKE 兜底。
	if expr := textseg.BuildMatch("库").Strict; expr != "" {
		t.Fatalf("单字不应产生 MATCH 表达式: %q", expr)
	}
	needle := textseg.LikeNeedle("库")
	if needle != "库" {
		t.Fatalf("单字 LIKE 兜底串 = %q", needle)
	}
	hits, err = repo.LikeSearch(ctx, needle, active, 10)
	if err != nil || len(hits) != 1 {
		t.Fatalf("LIKE 兜底应命中 1 条, err=%v hits=%d", err, len(hits))
	}
}

func TestMemorySearchRankingAndFilters(t *testing.T) {
	db := NewTestDB(t)
	repo, _, _, _ := memoryRepos(t, db)
	ctx := context.Background()

	// 短文档命中同一词时 BM25 应排前。
	short := seedMemory(t, repo, "缓存", "缓存", "ops", model.MemoryKindNote, "性能")
	long := seedMemory(t, repo, "长文", "缓存 命中率 与 缓存 策略 还有 更多 内容 用于 拉长 文档", "ops", model.MemoryKindNote, "性能")

	active := repository.MemoryFilter{Status: string(model.MemoryStatusActive)}
	hits, err := repo.Search(ctx, textseg.BuildMatch("缓存").Strict, active, 10)
	if err != nil || len(hits) != 2 {
		t.Fatalf("应命中 2 条, err=%v hits=%d", err, len(hits))
	}
	if hits[0].ID != short.ID {
		t.Fatalf("短文档应排前（BM25）: 首条=%d want=%d", hits[0].ID, short.ID)
	}

	// 命名空间过滤。
	other := seedMemory(t, repo, "别处缓存", "缓存", "other", model.MemoryKindNote)
	hits, _ = repo.Search(ctx, textseg.BuildMatch("缓存").Strict, repository.MemoryFilter{Namespace: "ops"}, 10)
	if len(hits) != 2 {
		t.Fatalf("命名空间过滤应命中 2 条, got %d", len(hits))
	}
	// 类型过滤。
	hits, _ = repo.Search(ctx, textseg.BuildMatch("缓存").Strict, repository.MemoryFilter{Kind: string(model.MemoryKindDecision)}, 10)
	if len(hits) != 0 {
		t.Fatalf("类型过滤应命中 0 条, got %d", len(hits))
	}
	// 标签过滤（AND）。
	hits, _ = repo.Search(ctx, textseg.BuildMatch("缓存").Strict, repository.MemoryFilter{Tags: []string{"性能"}}, 10)
	if len(hits) != 2 {
		t.Fatalf("标签过滤应命中 2 条（other 无标签）, got %d", len(hits))
	}
	// 未分类过滤。
	if hits, _ = repo.Search(ctx, textseg.BuildMatch("缓存").Strict, repository.MemoryFilter{Uncategorized: true}, 10); len(hits) != 0 {
		t.Fatalf("未分类过滤应命中 0 条, got %d", len(hits))
	}
	if hits, _ = repo.Search(ctx, textseg.BuildMatch("缓存").Strict, repository.MemoryFilter{Namespace: "other"}, 10); len(hits) != 1 || hits[0].ID != other.ID {
		t.Fatalf("other 命名空间应命中 1 条, got %+v", hits)
	}

	// 过期过滤。
	expired := time.Now().Add(-time.Hour)
	long.ExpiresAt = &expired
	if err := repo.Update(ctx, long); err != nil {
		t.Fatal(err)
	}
	hits, _ = repo.Search(ctx, textseg.BuildMatch("缓存").Strict,
		repository.MemoryFilter{Status: string(model.MemoryStatusActive), ExcludeExpired: true, Now: time.Now()}, 10)
	if len(hits) != 2 { // short + other，long 已过期
		t.Fatalf("排除过期后应命中 2 条, got %d", len(hits))
	}
}

func TestMemoryUpdateDeleteAndFTSSync(t *testing.T) {
	db := NewTestDB(t)
	repo, _, _, _ := memoryRepos(t, db)
	ctx := context.Background()
	active := repository.MemoryFilter{Status: string(model.MemoryStatusActive)}

	m := seedMemory(t, repo, "旧标题", "旧内容包含 苹果 ", "", model.MemoryKindNote)
	m.Title, m.Content = "新标题", "新内容包含 香蕉 "
	if err := repo.Update(ctx, m); err != nil {
		t.Fatal(err)
	}
	if hits, _ := repo.Search(ctx, textseg.BuildMatch("香蕉").Strict, active, 10); len(hits) != 1 {
		t.Fatalf("更新后新词应命中, got %d", len(hits))
	}
	if hits, _ := repo.Search(ctx, textseg.BuildMatch("苹果").Strict, repository.MemoryFilter{}, 10); len(hits) != 0 {
		t.Fatalf("更新后旧词不应命中, got %d", len(hits))
	}

	// 软删除：状态过滤生效。
	m.Status = model.MemoryStatusDeleted
	if err := repo.Update(ctx, m); err != nil {
		t.Fatal(err)
	}
	if hits, _ := repo.Search(ctx, textseg.BuildMatch("香蕉").Strict, active, 10); len(hits) != 0 {
		t.Fatalf("软删除后 active 检索不应命中, got %d", len(hits))
	}
	if hits, _ := repo.Search(ctx, textseg.BuildMatch("香蕉").Strict, repository.MemoryFilter{}, 10); len(hits) != 1 {
		t.Fatalf("不限状态检索应命中软删除行（控制台回收站语义）, got %d", len(hits))
	}
}

func TestMemoryListPaginationAndSort(t *testing.T) {
	db := NewTestDB(t)
	repo, _, _, _ := memoryRepos(t, db)
	ctx := context.Background()

	low := seedMemory(t, repo, "低", "低重要度 内容", "ns", model.MemoryKindNote)
	high := seedMemory(t, repo, "高", "高重要度 内容", "ns", model.MemoryKindNote)
	high.Importance, high.Pinned = 5, true
	if err := repo.Update(ctx, high); err != nil {
		t.Fatal(err)
	}
	low.AccessCount = 7
	if err := repo.Update(ctx, low); err != nil {
		t.Fatal(err)
	}

	items, total, err := repo.List(ctx, repository.MemoryFilter{Namespace: "ns"}, "", 0, 1)
	if err != nil || total != 2 || len(items) != 1 {
		t.Fatalf("分页: err=%v total=%d len=%d", err, total, len(items))
	}
	if items[0].ID != high.ID {
		t.Fatalf("默认排序应置顶优先, got %d", items[0].ID)
	}
	items, _, _ = repo.List(ctx, repository.MemoryFilter{Namespace: "ns"}, "access", 0, 10)
	if len(items) != 2 || items[0].ID != high.ID { // pinned 优先于 access
		t.Fatalf("access 排序应置顶优先, got %+v", items)
	}
	items, _, _ = repo.List(ctx, repository.MemoryFilter{Namespace: "ns"}, "unknown", 0, 10)
	if items[0].ID != high.ID {
		t.Fatalf("未知排序应回退默认, got %d", items[0].ID)
	}
}

func TestMemoryGraphNodesAndStats(t *testing.T) {
	db := NewTestDB(t)
	repo, nsRepo, linkRepo, _ := memoryRepos(t, db)
	ctx := context.Background()

	a := seedMemory(t, repo, "A", "节点 A 内容", "design", model.MemoryKindDecision)
	b := seedMemory(t, repo, "B", "节点 B 内容", "design", model.MemoryKindNote)
	c := seedMemory(t, repo, "C", "节点 C 内容", "", model.MemoryKindNote)
	if _, err := nsRepo.Ensure(ctx, "design", "设计相关", "api"); err != nil {
		t.Fatal(err)
	}

	nodes, err := repo.GraphNodes(ctx, repository.MemoryFilter{Status: string(model.MemoryStatusActive)}, 10)
	if err != nil || len(nodes) != 3 {
		t.Fatalf("图节点: err=%v n=%d", err, len(nodes))
	}

	if _, err := linkRepo.Add(ctx, &model.MemoryLink{FromID: a.ID, ToID: b.ID, Relation: model.MemoryLinkRelated}); err != nil {
		t.Fatal(err)
	}
	again, err := linkRepo.Add(ctx, &model.MemoryLink{FromID: a.ID, ToID: b.ID, Relation: model.MemoryLinkRelated})
	if err != nil || again.ID == 0 {
		t.Fatalf("重复建边应幂等返回既有行: %v %+v", err, again)
	}
	links, err := linkRepo.ListByMemory(ctx, b.ID)
	if err != nil || len(links) != 1 {
		t.Fatalf("按记忆查边: err=%v n=%d", err, len(links))
	}
	ids := []int64{a.ID, b.ID}
	links, err = linkRepo.ListForNodes(ctx, ids)
	if err != nil || len(links) != 1 {
		t.Fatalf("节点集内边: err=%v n=%d", err, len(links))
	}
	if links, _ = linkRepo.ListForNodes(ctx, []int64{a.ID, c.ID}); len(links) != 0 {
		t.Fatalf("端点不全在集合内应无边, got %d", len(links))
	}
	if err := linkRepo.Delete(ctx, again.ID); err != nil {
		t.Fatal(err)
	}
	if err := linkRepo.Delete(ctx, again.ID); err == nil || errs.From(err).Code != errs.CodeNotFound {
		t.Fatalf("删除不存在的边应 NotFound, got %v", err)
	}

	counts, err := repo.NamespaceCounts(ctx)
	if err != nil || counts["design"] != 2 || counts[""] != 1 {
		t.Fatalf("命名空间计数: err=%v %+v", err, counts)
	}
	st, err := repo.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 3 || st.Active != 3 || st.Uncategorized != 1 || st.ByKind[string(model.MemoryKindNote)] != 2 {
		t.Fatalf("统计口径异常: %+v", st)
	}
}

// TestMemoryStatsExcludesDeleted 统计口径：Total 只算生效记忆。
//
// Total 必须与「未分类 + Σ 命名空间」以及默认列表（status 空 = active）严格相等。
// 它曾经把 archived/superseded 也计入，导致首页「记忆总量」比命名空间卡片之和多 1~2，
// 「全部记忆」卡片写着 N、点进去列表只有 N-1 条。
func TestMemoryStatsExcludesDeleted(t *testing.T) {
	db := NewTestDB(t)
	repo, _, _, _ := memoryRepos(t, db)
	ctx := context.Background()

	seedMemory(t, repo, "一", "内容一", "", model.MemoryKindNote)
	gone := seedMemory(t, repo, "二", "内容二", "", model.MemoryKindNote)
	arch := seedMemory(t, repo, "三", "内容三", "", model.MemoryKindNote)
	sup := seedMemory(t, repo, "四", "内容四", "", model.MemoryKindNote)
	gone.Status = model.MemoryStatusDeleted
	arch.Status = model.MemoryStatusArchived
	sup.Status = model.MemoryStatusSuperseded
	for _, m := range []*model.Memory{gone, arch, sup} {
		if err := repo.Update(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	st, err := repo.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Active != 1 || st.Deleted != 1 || st.Archived != 1 || st.Superseded != 1 {
		t.Fatalf("分状态计数异常: %+v", st)
	}
	// 总量 = 生效（1）；归档/被替代/软删除都另有计数，不混进总量
	if st.Total != 1 {
		t.Fatalf("Total 应只算生效，实际 %d（%+v）", st.Total, st)
	}
	if st.Total != st.Active {
		t.Fatalf("Total(%d) 必须等于 Active(%d)", st.Total, st.Active)
	}
	counts, err := repo.NamespaceCounts(ctx)
	if err != nil || counts[""] != 1 {
		t.Fatalf("命名空间计数应只算 active: err=%v %+v", err, counts)
	}
	// 不变量：Total == 未分类 + Σ 命名空间
	sum := st.Uncategorized
	for _, n := range st.ByNamespace {
		sum += n
	}
	if st.Total != sum {
		t.Fatalf("Total(%d) 应等于 未分类+Σ命名空间(%d)（%+v）", st.Total, sum, st)
	}
}

func TestMemoryNamespaceLifecycle(t *testing.T) {
	db := NewTestDB(t)
	repo, nsRepo, _, _ := memoryRepos(t, db)
	ctx := context.Background()

	m1 := seedMemory(t, repo, "一", "内容一", "old", model.MemoryKindNote)
	m2 := seedMemory(t, repo, "二", "内容二", "old", model.MemoryKindNote)
	m3 := seedMemory(t, repo, "三", "内容三", "keep", model.MemoryKindNote)

	created, err := nsRepo.Ensure(ctx, "old", "旧", "api")
	if err != nil || !created {
		t.Fatalf("Ensure 新建: %v %v", created, err)
	}
	if created, _ := nsRepo.Ensure(ctx, "old", "重名", "api"); created {
		t.Fatal("重名 Ensure 不应重复创建")
	}
	if _, err := nsRepo.Ensure(ctx, "keep", "", "console"); err != nil {
		t.Fatal(err)
	}

	// 改名：记忆同步迁移。
	if moved, err := nsRepo.Rename(ctx, "old", "new"); err != nil || moved != 2 {
		t.Fatalf("改名迁移 %d 条 err=%v", moved, err)
	}
	if got, _ := repo.Get(ctx, m1.ID); got.Namespace != "new" {
		t.Fatalf("改名后记忆命名空间 = %q", got.Namespace)
	}
	// 改名到已存在：报参数错误。
	if _, err := nsRepo.Rename(ctx, "new", "keep"); err == nil || errs.From(err).Code != errs.CodeInvalidParams {
		t.Fatalf("改名冲突应报 InvalidParams, got %v", err)
	}
	if _, err := nsRepo.Rename(ctx, "missing", "x"); err == nil || errs.From(err).Code != errs.CodeNotFound {
		t.Fatalf("改名不存在应 NotFound, got %v", err)
	}

	// 合并：new -> keep。
	if moved, err := nsRepo.Merge(ctx, "new", "keep"); err != nil || moved != 2 {
		t.Fatalf("合并迁移 %d 条 err=%v", moved, err)
	}
	if got, _ := repo.Get(ctx, m2.ID); got.Namespace != "keep" {
		t.Fatalf("合并后记忆命名空间 = %q", got.Namespace)
	}
	if list, _ := nsRepo.List(ctx); len(list) != 1 || list[0].Name != "keep" {
		t.Fatalf("合并后应只剩 keep: %+v", list)
	}
	if moved, err := repo.ClearNamespace(ctx, "keep"); err != nil || moved != 3 {
		t.Fatalf("移回未分类 %d 条 err=%v", moved, err)
	}
	got, _ := repo.Get(ctx, m3.ID)
	if got.Namespace != "" || got.NamespaceSource != model.NamespaceSourceNone {
		t.Fatalf("移回未分类后应清空归属: %+v", got)
	}

	// 删除命名空间：记忆回落未分类。
	if _, err := nsRepo.Ensure(ctx, "tmp", "", "llm"); err != nil {
		t.Fatal(err)
	}
	tmp := seedMemory(t, repo, "临时", "临时内容", "tmp", model.MemoryKindNote)
	tmp.NamespaceSource = model.NamespaceSourceLLM
	if err := repo.Update(ctx, tmp); err != nil {
		t.Fatal(err)
	}
	ns, _ := nsRepo.GetByName(ctx, "tmp")
	if moved, err := nsRepo.Delete(ctx, ns.ID); err != nil || moved != 1 {
		t.Fatalf("删除命名空间迁移 %d 条 err=%v", moved, err)
	}
	if got, _ := repo.Get(ctx, tmp.ID); got.Namespace != "" || got.NamespaceSource != model.NamespaceSourceNone {
		t.Fatalf("删除命名空间后记忆应回未分类: %+v", got)
	}
	if moved, _ := nsRepo.Merge(ctx, "keep", "missing"); moved != 0 {
		t.Fatal("合并到不存在的目标应失败")
	}
}

func TestMemorySettingsDualWriter(t *testing.T) {
	db := NewTestDB(t)
	_, _, _, settings := memoryRepos(t, db)
	ctx := context.Background()

	if err := settings.Save(ctx, &model.MemorySettings{ID: 1}); err != nil {
		t.Fatal(err)
	}
	// 写配置列。
	if err := settings.UpdateConfig(ctx, &model.MemorySettings{ID: 1, ClassifierChannelID: 3, ClassifierModel: "mimo-v2"}); err != nil {
		t.Fatal(err)
	}
	// 写任务列。
	start := time.Now()
	if err := settings.UpdateJobStatus(ctx, repository.MemoryJobStatus{
		Status: model.MemoryJobRunning, Total: 10, Done: 4, StartedAt: &start,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := settings.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.ClassifierChannelID != 3 || got.ClassifierModel != "mimo-v2" {
		t.Fatalf("任务写回覆盖了配置列: %+v", got)
	}
	finish := time.Now()
	if err := settings.UpdateJobStatus(ctx, repository.MemoryJobStatus{
		Status: model.MemoryJobDone, Total: 10, Done: 10, Assigned: 8, NewNamespaces: 2,
		Message: "完成", StartedAt: &start, FinishedAt: &finish,
	}); err != nil {
		t.Fatal(err)
	}
	if err := settings.UpdateConfig(ctx, &model.MemorySettings{ID: 1, ClassifierChannelID: 5, ClassifierModel: "glm-4"}); err != nil {
		t.Fatal(err)
	}
	got, err = settings.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.ClassifierChannelID != 5 || got.ClassifierModel != "glm-4" {
		t.Fatalf("配置写回异常: %+v", got)
	}
	if got.JobStatus != model.MemoryJobDone || got.JobDone != 10 || got.JobAssigned != 8 || got.JobNewNamespaces != 2 {
		t.Fatalf("配置写回覆盖了任务状态列: %+v", got)
	}
}

func TestMemoryTouchAccessAndRebuildFTS(t *testing.T) {
	db := NewTestDB(t)
	repo, _, _, _ := memoryRepos(t, db)
	ctx := context.Background()

	a := seedMemory(t, repo, "A", "触摸 A 内容", "", model.MemoryKindNote)
	b := seedMemory(t, repo, "B", "触摸 B 内容", "", model.MemoryKindNote)
	now := time.Now()
	if err := repo.TouchAccess(ctx, []int64{a.ID, b.ID}, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.TouchAccess(ctx, []int64{a.ID}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.TouchAccess(ctx, nil, now); err != nil {
		t.Fatal(err)
	}
	gotA, _ := repo.Get(ctx, a.ID)
	gotB, _ := repo.Get(ctx, b.ID)
	if gotA.AccessCount != 2 || gotB.AccessCount != 1 || gotA.LastAccessedAt == nil {
		t.Fatalf("访问统计: A=%d B=%d", gotA.AccessCount, gotB.AccessCount)
	}

	// 人为破坏索引后重建应恢复检索。
	if err := db.Exec("DELETE FROM memories_fts").Error; err != nil {
		t.Fatal(err)
	}
	if hits, _ := repo.Search(ctx, textseg.BuildMatch("触摸").Strict, repository.MemoryFilter{}, 10); len(hits) != 0 {
		t.Fatal("清空索引后不应命中")
	}
	n, err := repo.RebuildFTS(ctx)
	if err != nil || n != 2 {
		t.Fatalf("重建索引 n=%d err=%v", n, err)
	}
	if hits, _ := repo.Search(ctx, textseg.BuildMatch("触摸").Strict, repository.MemoryFilter{}, 10); len(hits) != 2 {
		t.Fatalf("重建后应命中 2 条, got %d", len(hits))
	}
}

func TestMemoryGetByHash(t *testing.T) {
	db := NewTestDB(t)
	repo, _, _, _ := memoryRepos(t, db)
	ctx := context.Background()

	m := seedMemory(t, repo, "去重", "相同内容", "ns", model.MemoryKindNote)
	got, err := repo.GetByHash(ctx, "hash-相同内容", "ns")
	if err != nil || got.ID != m.ID {
		t.Fatalf("按哈希命中: %v %+v", err, got)
	}
	if _, err := repo.GetByHash(ctx, "hash-相同内容", "other"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("不同命名空间不应命中, got %v", err)
	}
	// 软删除后不再参与去重。
	m.Status = model.MemoryStatusDeleted
	if err := repo.Update(ctx, m); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetByHash(ctx, "hash-相同内容", "ns"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("软删除后不应参与去重, got %v", err)
	}
}

func TestMemoryTagAndImportanceHelpers(t *testing.T) {
	m := &model.Memory{}
	m.SetTags([]string{" b ", "a", "a", ""})
	if m.Tags != "b,a" {
		t.Fatalf("标签规范化 = %q", m.Tags)
	}
	if got := m.TagList(); len(got) != 2 || got[0] != "b" {
		t.Fatalf("标签解析 = %+v", got)
	}
	for _, tc := range []struct{ in, want int }{{0, 1}, {3, 3}, {9, 5}} {
		if got := model.ClampImportance(tc.in); got != tc.want {
			t.Errorf("ClampImportance(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
	_ = fmt.Sprintf("%v", model.AllMemoryKinds)
}

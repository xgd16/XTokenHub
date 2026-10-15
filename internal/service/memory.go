package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/textseg"
	"xtokenhub/internal/repository"
)

// 记忆库边界与排序权重。
const (
	// memoryContentMaxBytes 单条记忆正文上限（64KB，防滥用；FTS 只索引前 4000 字）。
	memoryContentMaxBytes = 64 << 10
	// memoryListMaxRunes 单次写入/更新的关联项上限（supersedes 与 links 各自）。
	memoryLinkMaxItems = 16
	// 排序权重：相关性 0.5 + 时间衰减 0.2 + 重要度 0.2 + 访问强化 0.1，置顶额外加成。
	rankWeightRelevance  = 0.5
	rankWeightRecency    = 0.2
	rankWeightImportance = 0.2
	rankWeightAccess     = 0.1
	rankPinnedBonus      = 0.25
	// memoryRecencyHalfLife 时间衰减半衰期：14 天前的记忆权重减半。
	memoryRecencyHalfLife = 14 * 24 * time.Hour
	// likePathRelevance LIKE 兜底路径无 bm25 可比，给固定中等相关性。
	likePathRelevance = 0.6
	// searchCandidateCap 送入重排的候选上限（bm25 前 N 与最终 limit 的倍数）。
	searchCandidateCap = 100
)

// MemoryOptions 记忆库运行参数。
type MemoryOptions struct {
	// SearchLimitDefault 检索默认返回条数。
	SearchLimitDefault int
	// SearchLimitMax 检索返回上限。
	SearchLimitMax int
}

// MemoryService 记忆库业务：写入去重、检索排序、关系与命名空间管理。
type MemoryService struct {
	memories   repository.MemoryRepository
	namespaces repository.MemoryNamespaceRepository
	links      repository.MemoryLinkRepository
	settings   repository.MemorySettingsRepository
	opts       MemoryOptions
	now        func() time.Time
}

// NewMemoryService 构造记忆库服务。
func NewMemoryService(
	memories repository.MemoryRepository,
	namespaces repository.MemoryNamespaceRepository,
	links repository.MemoryLinkRepository,
	settings repository.MemorySettingsRepository,
	opts MemoryOptions,
) *MemoryService {
	if opts.SearchLimitDefault <= 0 {
		opts.SearchLimitDefault = 10
	}
	if opts.SearchLimitMax <= 0 {
		opts.SearchLimitMax = 50
	}
	return &MemoryService{
		memories: memories, namespaces: namespaces, links: links, settings: settings,
		opts: opts, now: time.Now,
	}
}

// SetClock 注入时钟（测试用；默认 time.Now）。
func (s *MemoryService) SetClock(now func() time.Time) { s.now = now }

// EnsureDefaults 播种单行设置表（ID=1），双写者的 UpdateConfig/UpdateJobStatus 依赖它存在。
func (s *MemoryService) EnsureDefaults(ctx context.Context) error {
	if _, err := s.settings.Get(ctx); err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return errs.Wrap(errs.CodeInternal, "读取记忆库设置失败", err)
	}
	if err := s.settings.Save(ctx, &model.MemorySettings{ID: 1}); err != nil {
		return errs.Wrap(errs.CodeInternal, "播种记忆库设置失败", err)
	}
	return nil
}

// ---- 入参 ----

// MemoryQuery 检索/列表入参（HTTP、MCP、控制台共用）。
type MemoryQuery struct {
	// Q 关键词（空 = 浏览列表）。
	Q string
	// Namespace 命名空间过滤；保留值 "none" 表示只看未分类（空 = 全部）。
	Namespace string
	// Kind 记忆类型过滤（空 = 不限）。
	Kind string
	// Tags 标签过滤（全部命中）。
	Tags []string
	// Status 状态过滤：空 = active；"all" = 生效 + 被替代 + 归档（不含已删除）；
	// 否则按枚举精确匹配。
	Status string
	// PinnedOnly 只看置顶。
	PinnedOnly bool
	// Sort 列表排序：updated | created | importance | access（空 = updated）。
	Sort string
	// Limit 返回条数（检索用，0 = 配置默认值）。
	Limit int
}

// MemoryAddInput 写入入参。
type MemoryAddInput struct {
	Title         string
	Content       string
	Namespace     string
	Kind          string
	Tags          []string
	Importance    int
	Pinned        bool
	ExpiresAt     *time.Time
	Source        string
	CreatedBy     string
	SourceSession string
	SourceRef     string
	Supersedes    []int64
	Links         []MemoryLinkInput
}

// MemoryLinkInput 建边入参。
type MemoryLinkInput struct {
	// 必须带 json tag：MCP memory_add 的 links[] 与控制台 POST /memories 的 links
	// 都直接反序列化本结构。缺 tag 时 encoding/json 只能按字段名做大小写不敏感匹配，
	// 而 `to_id` 与 `ToID` 差一个下划线 → 匹配不上 → ToID 恒为 0，
	// 结果是所有显式 links 都被判「含非法 to_id」，建边功能形同虚设。
	ToID     int64  `json:"to_id"`
	Relation string `json:"relation"`
	Note     string `json:"note"`
}

// MemoryUpdateInput 更新入参（指针字段 = 提供才更新）。
type MemoryUpdateInput struct {
	Title        *string
	Content      *string
	Namespace    *string
	Kind         *string
	Tags         *[]string
	Importance   *int
	Pinned       *bool
	Status       *string
	ExpiresAt    *time.Time
	ClearExpires bool
}

// ---- 出参 ----

// MemoryAddResult 写入结果。
type MemoryAddResult struct {
	Memory     *model.Memory      `json:"memory"`
	Duplicated bool               `json:"duplicated"`
	Superseded []int64            `json:"superseded,omitempty"`
	Links      []model.MemoryLink `json:"links,omitempty"`
	// AutoLinks 按内容相似度自动建立的 related 边（created_by=system，重算时可回收）。
	AutoLinks []model.MemoryLink `json:"auto_links,omitempty"`
}

// MemorySearchResult 检索结果（附按命名空间的命中分布，便于感知主题结构）。
type MemorySearchResult struct {
	Items      []MemoryHitResult `json:"items"`
	Count      int               `json:"count"`
	Namespaces []MemoryFacet     `json:"namespaces"`
	Mode       string            `json:"mode"` // fts | fts_loose | like | list
}

// MemoryHitResult 单条命中（含综合分）。
type MemoryHitResult struct {
	model.Memory
	Score float64 `json:"score"`
}

// MemoryFacet 命名空间命中分布。
type MemoryFacet struct {
	Namespace string `json:"namespace"`
	Count     int    `json:"count"`
}

// MemoryGraph 关系图数据（节点按命名空间着色，边为记忆间关系）。
type MemoryGraph struct {
	Nodes     []MemoryGraphNode `json:"nodes"`
	Edges     []MemoryGraphEdge `json:"edges"`
	Truncated bool              `json:"truncated"`
}

// MemoryGraphNode 关系图节点。
type MemoryGraphNode struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Namespace   string    `json:"namespace"`
	Kind        string    `json:"kind"`
	Importance  int       `json:"importance"`
	Pinned      bool      `json:"pinned"`
	AccessCount int64     `json:"access_count"`
	Degree      int       `json:"degree"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// MemoryGraphEdge 关系图边。
type MemoryGraphEdge struct {
	ID       int64  `json:"id"`
	FromID   int64  `json:"from_id"`
	ToID     int64  `json:"to_id"`
	Relation string `json:"relation"`
	Note     string `json:"note"`
}

// MemoryNamespaceView 命名空间视图（注册表 + 记忆计数）。
type MemoryNamespaceView struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedBy   string `json:"created_by"`
	Count       int64  `json:"count"`
}

// MemoryClassifyConfig 分类配置与最近一次任务状态。
type MemoryClassifyConfig struct {
	ChannelID     int64      `json:"channel_id"`
	Model         string     `json:"model"`
	Status        string     `json:"status"`
	Total         int        `json:"total"`
	Done          int        `json:"done"`
	Assigned      int        `json:"assigned"`
	NewNamespaces int        `json:"new_namespaces"`
	Message       string     `json:"message"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
}

// ---- 写入 ----

// Add 写入一条记忆：校验 → 同命名空间同内容去重 → 落库 → 建 supersede 边与新记忆关联边。
// 显式指定命名空间时自动创建命名空间注册行。
func (s *MemoryService) Add(ctx context.Context, in MemoryAddInput) (*MemoryAddResult, error) {
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return nil, errs.New(errs.CodeInvalidParams, "content 不能为空")
	}
	if len(content) > memoryContentMaxBytes {
		return nil, errs.New(errs.CodeInvalidParams, "content 过大（上限 64KB）")
	}
	ns := strings.TrimSpace(in.Namespace)
	if err := validateNamespaceName(ns); err != nil {
		return nil, err
	}
	kind, err := normalizeKind(in.Kind)
	if err != nil {
		return nil, err
	}
	tags, err := normalizeTags(in.Tags)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = textseg.MarkdownTitle(content, model.MemoryTitleMaxRunes)
	} else {
		title = textseg.Clip(title, model.MemoryTitleMaxRunes)
	}
	if len(in.Supersedes) > memoryLinkMaxItems {
		return nil, errs.New(errs.CodeInvalidParams, "supersedes 过多（上限 16 条）")
	}
	if len(in.Links) > memoryLinkMaxItems {
		return nil, errs.New(errs.CodeInvalidParams, "links 过多（上限 16 条）")
	}
	// 关联项前置校验：避免记忆已落库、建边时才发现目标非法而留下半写入。
	for _, id := range in.Supersedes {
		if id <= 0 {
			return nil, errs.New(errs.CodeInvalidParams, "supersedes 含非法 id")
		}
		if _, err := s.memories.Get(ctx, id); err != nil {
			return nil, err
		}
	}
	for _, l := range in.Links {
		if l.ToID <= 0 {
			return nil, errs.New(errs.CodeInvalidParams, "links 含非法 to_id")
		}
		if _, err := normalizeRelation(l.Relation); err != nil {
			return nil, err
		}
		if _, err := s.memories.Get(ctx, l.ToID); err != nil {
			return nil, err
		}
	}

	source := sourceOf(in.Source)
	hash := memoryContentHash(content)
	// 去重：同命名空间内内容一致的活跃记忆直接返回。
	if existing, err := s.memories.GetByHash(ctx, hash, ns); err == nil {
		if existing.Status == model.MemoryStatusActive {
			return &MemoryAddResult{Memory: existing, Duplicated: true}, nil
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.Wrap(errs.CodeInternal, "查询重复记忆失败", err)
	}

	// 显式归属：命名空间不存在则自动建（来源 = 写入来源）。
	nsSource := model.NamespaceSourceNone
	if ns != "" {
		if _, err := s.namespaces.Ensure(ctx, ns, "", source); err != nil {
			return nil, err
		}
		if source == string(model.MemorySourceConsole) {
			nsSource = model.NamespaceSourceManual
		} else {
			nsSource = model.NamespaceSourceExplicit
		}
	}

	m := &model.Memory{
		Title:           title,
		Content:         content,
		Namespace:       ns,
		NamespaceSource: nsSource,
		Kind:            kind,
		Importance:      model.ClampImportance(defaultInt(in.Importance, 3)),
		Pinned:          in.Pinned,
		Status:          model.MemoryStatusActive,
		ExpiresAt:       in.ExpiresAt,
		Source:          model.MemorySource(source),
		CreatedBy:       strings.TrimSpace(in.CreatedBy),
		SourceSession:   textseg.Clip(strings.TrimSpace(in.SourceSession), 128),
		SourceRef:       textseg.Clip(strings.TrimSpace(in.SourceRef), 256),
		ContentHash:     hash,
	}
	m.SetTags(tags)
	if err := s.memories.Create(ctx, m); err != nil {
		return nil, errs.Wrap(errs.CodeInternal, "写入记忆失败", err)
	}

	res := &MemoryAddResult{Memory: m}
	actor := m.CreatedBy
	if actor == "" {
		actor = source
	}
	for _, oldID := range in.Supersedes {
		if oldID == m.ID {
			continue
		}
		if err := s.Supersede(ctx, oldID, m.ID, actor); err != nil {
			return nil, err
		}
		res.Superseded = append(res.Superseded, oldID)
	}
	for _, l := range in.Links {
		link, err := s.AddLink(ctx, m.ID, l.ToID, l.Relation, l.Note, actor)
		if err != nil {
			return nil, err
		}
		res.Links = append(res.Links, *link)
	}
	// 自动关联：同命名空间内按内容相似度维护 related 出边（幂等，失败不影响写入）。
	res.AutoLinks = s.autoLink(ctx, m)
	return res, nil
}

// Update 更新一条记忆（指针字段 = 提供才更新）；改名空间会确保注册行存在。
func (s *MemoryService) Update(ctx context.Context, id int64, in MemoryUpdateInput) (*model.Memory, error) {
	m, err := s.memories.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	// 内容/标题/归属变化时重算自动关联（见函数尾部）。
	oldTitle, oldHash, oldNS := m.Title, m.ContentHash, m.Namespace
	if in.Title != nil {
		m.Title = textseg.Clip(strings.TrimSpace(*in.Title), model.MemoryTitleMaxRunes)
	}
	if in.Content != nil {
		content := strings.TrimSpace(*in.Content)
		if content == "" {
			return nil, errs.New(errs.CodeInvalidParams, "content 不能为空")
		}
		if len(content) > memoryContentMaxBytes {
			return nil, errs.New(errs.CodeInvalidParams, "content 过大（上限 64KB）")
		}
		m.Content = content
		m.ContentHash = memoryContentHash(content)
		if strings.TrimSpace(m.Title) == "" {
			m.Title = textseg.MarkdownTitle(content, model.MemoryTitleMaxRunes)
		}
	}
	if in.Namespace != nil {
		ns := strings.TrimSpace(*in.Namespace)
		if err := validateNamespaceName(ns); err != nil {
			return nil, err
		}
		if ns != m.Namespace {
			m.Namespace = ns
			if ns == "" {
				m.NamespaceSource = model.NamespaceSourceNone
			} else {
				if _, err := s.namespaces.Ensure(ctx, ns, "", string(model.MemorySourceConsole)); err != nil {
					return nil, err
				}
				m.NamespaceSource = model.NamespaceSourceManual
			}
		}
	}
	if in.Kind != nil {
		kind, err := normalizeKind(*in.Kind)
		if err != nil {
			return nil, err
		}
		m.Kind = kind
	}
	if in.Tags != nil {
		tags, err := normalizeTags(*in.Tags)
		if err != nil {
			return nil, err
		}
		m.SetTags(tags)
	}
	if in.Importance != nil {
		m.Importance = model.ClampImportance(*in.Importance)
	}
	if in.Pinned != nil {
		m.Pinned = *in.Pinned
	}
	if in.Status != nil {
		st := model.MemoryStatus(strings.TrimSpace(*in.Status))
		switch st {
		case model.MemoryStatusActive, model.MemoryStatusArchived:
			m.Status = st
		default:
			return nil, errs.New(errs.CodeInvalidParams, "status 只支持 active/archived；替代与删除请用对应接口")
		}
	}
	if in.ClearExpires {
		m.ExpiresAt = nil
	} else if in.ExpiresAt != nil {
		m.ExpiresAt = in.ExpiresAt
	}
	if err := s.memories.Update(ctx, m); err != nil {
		return nil, errs.Wrap(errs.CodeInternal, "更新记忆失败", err)
	}
	if m.Title != oldTitle || m.ContentHash != oldHash || m.Namespace != oldNS {
		// 只回收/重建 system 自动边，显式关系不受影响。
		s.refreshAutoLinks(ctx, m)
	}
	return m, nil
}

// Forget 软删除一条记忆（回收站语义，检索默认不再命中）。
func (s *MemoryService) Forget(ctx context.Context, id int64) error {
	m, err := s.memories.Get(ctx, id)
	if err != nil {
		return err
	}
	m.Status = model.MemoryStatusDeleted
	return s.memories.Update(ctx, m)
}

// Supersede 用 newID 替代 oldID：旧记忆转 superseded 并记下替代者，同时建 supersedes 边
// （方向：新记忆 → 旧记忆）。失效不删除，历史可追溯。
func (s *MemoryService) Supersede(ctx context.Context, oldID, newID int64, actor string) error {
	if oldID <= 0 || newID <= 0 {
		return errs.New(errs.CodeInvalidParams, "记忆 id 非法")
	}
	if oldID == newID {
		return errs.New(errs.CodeInvalidParams, "不能替代自身")
	}
	old, err := s.memories.Get(ctx, oldID)
	if err != nil {
		return err
	}
	neu, err := s.memories.Get(ctx, newID)
	if err != nil {
		return err
	}
	if neu.Status != model.MemoryStatusActive {
		return errs.New(errs.CodeInvalidParams, "替代者必须处于 active 状态")
	}
	if old.Status == model.MemoryStatusActive {
		old.Status = model.MemoryStatusSuperseded
		old.SupersededBy = newID
		if err := s.memories.Update(ctx, old); err != nil {
			return errs.Wrap(errs.CodeInternal, "标记被替代失败", err)
		}
	}
	_, err = s.links.Add(ctx, &model.MemoryLink{
		FromID: newID, ToID: oldID, Relation: model.MemoryLinkSupersedes, CreatedBy: actor,
	})
	return err
}

// ---- 读取 ----

// Get 读取一条记忆。
func (s *MemoryService) Get(ctx context.Context, id int64) (*model.Memory, error) {
	return s.memories.Get(ctx, id)
}

// Search 关键词检索：FTS 严格 → FTS 宽松 → LIKE 兜底 三级降级，再按综合分重排。
// 命中后批量刷新访问统计（检索即强化）。
func (s *MemoryService) Search(ctx context.Context, q MemoryQuery) (*MemorySearchResult, error) {
	f, err := s.filterOf(q)
	if err != nil {
		return nil, err
	}
	limit := s.clampLimit(q.Limit)
	keyword := strings.TrimSpace(q.Q)
	res := &MemorySearchResult{Items: []MemoryHitResult{}}

	switch {
	case keyword == "":
		// 浏览模式：按置顶 + 更新时间排序。
		items, _, err := s.memories.List(ctx, f, q.Sort, 0, limit)
		if err != nil {
			return nil, errs.Wrap(errs.CodeInternal, "读取记忆列表失败", err)
		}
		for _, m := range items {
			res.Items = append(res.Items, MemoryHitResult{Memory: m})
		}
		res.Mode = "list"
	default:
		match := textseg.BuildMatch(keyword)
		var hits []repository.MemoryHit
		mode := "fts"
		if match.Strict != "" {
			hits, err = s.memories.Search(ctx, match.Strict, f, searchCandidateCap)
			if err != nil {
				return nil, errs.Wrap(errs.CodeInternal, "全文检索失败", err)
			}
			if len(hits) == 0 && match.Loose != match.Strict {
				hits, err = s.memories.Search(ctx, match.Loose, f, searchCandidateCap)
				if err != nil {
					return nil, errs.Wrap(errs.CodeInternal, "全文检索失败", err)
				}
				mode = "fts_loose"
			}
		}
		if len(hits) == 0 {
			// 单字中文，或长文超出索引截断：LIKE 兜底（限定命名空间/状态并限行）。
			if needle := textseg.LikeNeedle(keyword); needle != "" {
				hits, err = s.memories.LikeSearch(ctx, needle, f, searchCandidateCap)
				if err != nil {
					return nil, errs.Wrap(errs.CodeInternal, "兜底检索失败", err)
				}
				mode = "like"
			} else if match.Strict == "" {
				mode = "fts"
			}
		}
		ranked := s.rank(hits)
		if len(ranked) > limit {
			ranked = ranked[:limit]
		}
		res.Items = ranked
		res.Mode = mode
		s.touch(ctx, ranked)
	}

	res.Count = len(res.Items)
	res.Namespaces = memoryFacets(res.Items)
	return res, nil
}

// List 分页浏览（控制台列表用），不做相关性排序。
func (s *MemoryService) List(ctx context.Context, q MemoryQuery, offset, limit int) ([]model.Memory, int64, error) {
	f, err := s.filterOf(q)
	if err != nil {
		return nil, 0, err
	}
	items, total, err := s.memories.List(ctx, f, q.Sort, offset, limit)
	if err != nil {
		return nil, 0, errs.Wrap(errs.CodeInternal, "读取记忆列表失败", err)
	}
	return items, total, nil
}

// Graph 关系图：节点按过滤条件取（超限裁剪），边为两端都在节点集内的关系。
func (s *MemoryService) Graph(ctx context.Context, q MemoryQuery, limit int) (*MemoryGraph, error) {
	f, err := s.filterOf(q)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 150
	}
	if limit > 400 {
		limit = 400
	}
	nodes, err := s.memories.GraphNodes(ctx, f, limit+1)
	if err != nil {
		return nil, errs.Wrap(errs.CodeInternal, "读取图节点失败", err)
	}
	g := &MemoryGraph{Nodes: []MemoryGraphNode{}, Edges: []MemoryGraphEdge{}}
	if len(nodes) > limit {
		g.Truncated = true
		nodes = nodes[:limit]
	}
	ids := make([]int64, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	links, err := s.links.ListForNodes(ctx, ids)
	if err != nil {
		return nil, errs.Wrap(errs.CodeInternal, "读取关系失败", err)
	}
	degree := make(map[int64]int, len(ids))
	for _, l := range links {
		degree[l.FromID]++
		degree[l.ToID]++
		g.Edges = append(g.Edges, MemoryGraphEdge{
			ID: l.ID, FromID: l.FromID, ToID: l.ToID, Relation: string(l.Relation), Note: l.Note,
		})
	}
	for _, n := range nodes {
		g.Nodes = append(g.Nodes, MemoryGraphNode{
			ID: n.ID, Title: n.Title, Namespace: n.Namespace, Kind: string(n.Kind),
			Importance: n.Importance, Pinned: n.Pinned, AccessCount: n.AccessCount,
			Degree: degree[n.ID], UpdatedAt: n.UpdatedAt,
		})
	}
	return g, nil
}

// ---- 关系 ----

// Links 与某条记忆相关的全部边。
func (s *MemoryService) Links(ctx context.Context, id int64) ([]model.MemoryLink, error) {
	if _, err := s.memories.Get(ctx, id); err != nil {
		return nil, err
	}
	return s.links.ListByMemory(ctx, id)
}

// AddLink 建边（幂等）。
func (s *MemoryService) AddLink(ctx context.Context, fromID, toID int64, relation, note, actor string) (*model.MemoryLink, error) {
	if fromID == toID {
		return nil, errs.New(errs.CodeInvalidParams, "不能与自己建边")
	}
	rel, err := normalizeRelation(relation)
	if err != nil {
		return nil, err
	}
	if _, err := s.memories.Get(ctx, fromID); err != nil {
		return nil, err
	}
	if _, err := s.memories.Get(ctx, toID); err != nil {
		return nil, err
	}
	return s.links.Add(ctx, &model.MemoryLink{
		FromID: fromID, ToID: toID, Relation: rel,
		Note: textseg.Clip(strings.TrimSpace(note), 512), CreatedBy: actor,
	})
}

// RemoveLink 删除一条边。
func (s *MemoryService) RemoveLink(ctx context.Context, linkID int64) error {
	return s.links.Delete(ctx, linkID)
}

// ---- 命名空间 ----

// Namespaces 命名空间视图（注册表 + 记忆计数）；同名注册行缺失时按记忆数据兜底补名。
func (s *MemoryService) Namespaces(ctx context.Context) ([]MemoryNamespaceView, error) {
	rows, err := s.namespaces.List(ctx)
	if err != nil {
		return nil, errs.Wrap(errs.CodeInternal, "读取命名空间失败", err)
	}
	counts, err := s.memories.NamespaceCounts(ctx)
	if err != nil {
		return nil, errs.Wrap(errs.CodeInternal, "统计命名空间失败", err)
	}
	out := make([]MemoryNamespaceView, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, r := range rows {
		seen[r.Name] = true
		out = append(out, MemoryNamespaceView{
			ID: r.ID, Name: r.Name, Description: r.Description,
			CreatedBy: r.CreatedBy, Count: counts[r.Name],
		})
	}
	// 兜底：记忆里出现但注册表没有的名字（历史数据）也要能筛出来。
	for name, n := range counts {
		if name == "" || seen[name] {
			continue
		}
		out = append(out, MemoryNamespaceView{Name: name, Count: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// CreateNamespace 手工新建命名空间。
func (s *MemoryService) CreateNamespace(ctx context.Context, name, description string) (*model.MemoryNamespace, error) {
	name = strings.TrimSpace(name)
	if err := validateNamespaceName(name); err != nil {
		return nil, err
	}
	created, err := s.namespaces.Ensure(ctx, name, textseg.Clip(strings.TrimSpace(description), 512), string(model.MemorySourceConsole))
	if err != nil {
		return nil, err
	}
	if !created {
		return nil, errs.New(errs.CodeInvalidParams, "命名空间「"+name+"」已存在")
	}
	return s.namespaces.GetByName(ctx, name)
}

// UpdateNamespace 更新命名空间描述。
func (s *MemoryService) UpdateNamespace(ctx context.Context, id int64, description string) (*model.MemoryNamespace, error) {
	ns, err := s.namespaces.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	ns.Description = textseg.Clip(strings.TrimSpace(description), 512)
	if err := s.namespaces.Update(ctx, ns); err != nil {
		return nil, errs.Wrap(errs.CodeInternal, "更新命名空间失败", err)
	}
	return ns, nil
}

// RenameNamespace 改名（记忆同步迁移）。
func (s *MemoryService) RenameNamespace(ctx context.Context, from, to string) (int64, error) {
	if err := validateNamespaceName(to); err != nil {
		return 0, err
	}
	return s.namespaces.Rename(ctx, strings.TrimSpace(from), strings.TrimSpace(to))
}

// MergeNamespaces 把 from 并入 to。
func (s *MemoryService) MergeNamespaces(ctx context.Context, from, to string) (int64, error) {
	return s.namespaces.Merge(ctx, strings.TrimSpace(from), strings.TrimSpace(to))
}

// DeleteNamespace 删除命名空间，其记忆回落未分类（可被一键分类重新归置）。
func (s *MemoryService) DeleteNamespace(ctx context.Context, id int64) (int64, error) {
	return s.namespaces.Delete(ctx, id)
}

// BatchMoveNamespace 批量移动记忆到指定命名空间（"" = 移回未分类）。
func (s *MemoryService) BatchMoveNamespace(ctx context.Context, ids []int64, namespace string) (int64, error) {
	ns := strings.TrimSpace(namespace)
	if err := validateNamespaceName(ns); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, errs.New(errs.CodeInvalidParams, "ids 不能为空")
	}
	if ns != "" {
		if _, err := s.namespaces.Ensure(ctx, ns, "", string(model.MemorySourceConsole)); err != nil {
			return 0, err
		}
	}
	var moved int64
	for _, id := range ids {
		m, err := s.memories.Get(ctx, id)
		if err != nil {
			if errs.From(err).Code == errs.CodeNotFound {
				continue
			}
			return moved, err
		}
		if m.Namespace == ns {
			continue
		}
		m.Namespace = ns
		if ns == "" {
			m.NamespaceSource = model.NamespaceSourceNone
		} else {
			m.NamespaceSource = model.NamespaceSourceManual
		}
		if err := s.memories.Update(ctx, m); err != nil {
			return moved, errs.Wrap(errs.CodeInternal, "批量移动失败", err)
		}
		moved++
	}
	return moved, nil
}

// Stats 记忆库总览。
func (s *MemoryService) Stats(ctx context.Context) (*repository.MemoryStats, error) {
	st, err := s.memories.Stats(ctx)
	if err != nil {
		return nil, errs.Wrap(errs.CodeInternal, "统计失败", err)
	}
	return st, nil
}

// ClassifyConfig 读取分类配置与最近任务状态。
func (s *MemoryService) ClassifyConfig(ctx context.Context) (*MemoryClassifyConfig, error) {
	st, err := s.settings.Get(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &MemoryClassifyConfig{}, nil
		}
		return nil, errs.Wrap(errs.CodeInternal, "读取分类设置失败", err)
	}
	return &MemoryClassifyConfig{
		ChannelID: st.ClassifierChannelID, Model: st.ClassifierModel,
		Status: st.JobStatus, Total: st.JobTotal, Done: st.JobDone,
		Assigned: st.JobAssigned, NewNamespaces: st.JobNewNamespaces,
		Message: st.JobMessage, StartedAt: st.JobStartedAt, FinishedAt: st.JobFinishedAt,
	}, nil
}

// UpdateClassifyConfig 保存分类配置（只写配置列，不动任务状态）。
func (s *MemoryService) UpdateClassifyConfig(ctx context.Context, channelID int64, classifierModel string) error {
	modelName := strings.TrimSpace(classifierModel)
	if channelID <= 0 && modelName != "" {
		return errs.New(errs.CodeInvalidParams, "选择分类模型时必须指定渠道")
	}
	if channelID > 0 && modelName == "" {
		return errs.New(errs.CodeInvalidParams, "选择渠道时必须指定模型")
	}
	return s.settings.UpdateConfig(ctx, &model.MemorySettings{
		ID: 1, ClassifierChannelID: channelID, ClassifierModel: modelName,
	})
}

// ---- 内部 ----

// filterOf 组装仓储过滤条件：默认只查 active 且未过期。
func (s *MemoryService) filterOf(q MemoryQuery) (repository.MemoryFilter, error) {
	f := repository.MemoryFilter{
		Kind:       strings.TrimSpace(q.Kind),
		PinnedOnly: q.PinnedOnly,
	}
	if f.Kind != "" && !model.MemoryKind(f.Kind).Valid() {
		return f, errs.New(errs.CodeInvalidParams, "kind 非法: "+q.Kind)
	}
	for _, tag := range q.Tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		f.Tags = append(f.Tags, tag)
	}
	ns := strings.TrimSpace(q.Namespace)
	switch ns {
	case "":
	case "none":
		f.Uncategorized = true
	default:
		f.Namespace = ns
	}
	switch strings.TrimSpace(q.Status) {
	case "", string(model.MemoryStatusActive):
		f.Status = string(model.MemoryStatusActive)
		f.ExcludeExpired = true
		f.Now = s.now()
	case "all":
		// 「全部状态」= 生效 + 被替代 + 归档，**不含已删除**。
		// 曾经留空 Status 等于完全不过滤，软删除的回收站记录被一并带出，
		// 结果是「全部状态」比 Stats.Total 还多（16 vs 15），且与 UI 里单独的
		// 「已删除」筛选项语义重叠。回收站只由 status=deleted 查看。
		f.ExcludeDeleted = true
	default:
		st := model.MemoryStatus(strings.TrimSpace(q.Status))
		if !st.Valid() {
			return f, errs.New(errs.CodeInvalidParams, "status 非法: "+q.Status)
		}
		f.Status = string(st)
	}
	return f, nil
}

// clampLimit 收敛检索条数。
func (s *MemoryService) clampLimit(limit int) int {
	if limit <= 0 {
		return s.opts.SearchLimitDefault
	}
	if limit > s.opts.SearchLimitMax {
		return s.opts.SearchLimitMax
	}
	return limit
}

// rank 综合重排：相关性 + 时间衰减 + 重要度 + 访问强化 + 置顶加成。
func (s *MemoryService) rank(hits []repository.MemoryHit) []MemoryHitResult {
	if len(hits) == 0 {
		return []MemoryHitResult{}
	}
	now := s.now()
	// bm25 为负值（越小越相关），做 min-max 归一到 [0,1]。
	minScore, maxScore := hits[0].Score, hits[0].Score
	for _, h := range hits {
		if h.Score < minScore {
			minScore = h.Score
		}
		if h.Score > maxScore {
			maxScore = h.Score
		}
	}
	span := maxScore - minScore
	out := make([]MemoryHitResult, 0, len(hits))
	for _, h := range hits {
		rel := likePathRelevance
		if span > 0 {
			rel = (maxScore - h.Score) / span
		} else if minScore != 0 {
			rel = 1 // 只有一条候选或完全同分：视为同等相关
		}
		score := rankWeightRelevance*rel +
			rankWeightRecency*recencyDecay(h.Memory, now) +
			rankWeightImportance*float64(h.Memory.Importance)/5 +
			rankWeightAccess*accessBoost(h.Memory.AccessCount)
		if h.Memory.Pinned {
			score += rankPinnedBonus
		}
		out = append(out, MemoryHitResult{Memory: h.Memory, Score: math.Round(score*1000) / 1000})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ID > out[j].ID
	})
	return out
}

// recencyDecay 时间衰减：取更新与上次访问的较近者，半衰期 14 天。
func recencyDecay(m model.Memory, now time.Time) float64 {
	at := m.UpdatedAt
	if m.LastAccessedAt != nil && m.LastAccessedAt.After(at) {
		at = *m.LastAccessedAt
	}
	if at.IsZero() {
		return 0.5
	}
	age := now.Sub(at)
	if age < 0 {
		age = 0
	}
	return math.Pow(0.5, float64(age)/float64(memoryRecencyHalfLife))
}

// accessBoost 访问强化：min(1, log10(1+次数)/2)。
func accessBoost(count int64) float64 {
	if count <= 0 {
		return 0
	}
	return math.Min(1, math.Log10(1+float64(count))/2)
}

// touch 刷新命中结果的访问统计（检索即强化）。
func (s *MemoryService) touch(ctx context.Context, items []MemoryHitResult) {
	if len(items) == 0 {
		return
	}
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	// 统计刷新失败不影响检索结果。
	_ = s.memories.TouchAccess(ctx, ids, s.now())
}

// memoryFacets 按命名空间的命中分布（按数量降序，同数量按名称升序，未分类排最后）。
func memoryFacets(items []MemoryHitResult) []MemoryFacet {
	counts := map[string]int{}
	for _, it := range items {
		counts[it.Namespace]++
	}
	out := make([]MemoryFacet, 0, len(counts))
	for ns, n := range counts {
		out = append(out, MemoryFacet{Namespace: ns, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if (out[i].Namespace == "") != (out[j].Namespace == "") {
			return out[j].Namespace == ""
		}
		return out[i].Namespace < out[j].Namespace
	})
	return out
}

// memoryContentHash 内容哈希（去重用）：归一化后 sha256。
func memoryContentHash(content string) string {
	sum := sha256.Sum256([]byte(textseg.Normalize(content)))
	return hex.EncodeToString(sum[:])
}

// normalizeRelation 校验并归一化关系类型；空值取默认 related。
func normalizeRelation(relation string) (model.MemoryLinkRelation, error) {
	rel := model.MemoryLinkRelation(strings.TrimSpace(relation))
	if rel == "" {
		return model.MemoryLinkRelated, nil
	}
	if !rel.Valid() {
		return "", errs.New(errs.CodeInvalidParams, "relation 非法: "+relation)
	}
	return rel, nil
}

// normalizeKind 校验类型；空值取默认 note。
func normalizeKind(kind string) (model.MemoryKind, error) {
	k := model.MemoryKind(strings.TrimSpace(kind))
	if k == "" {
		return model.MemoryKindNote, nil
	}
	if !k.Valid() {
		return "", errs.New(errs.CodeInvalidParams, "kind 非法: "+kind)
	}
	return k, nil
}

// normalizeTags 校验标签数量与长度。
func normalizeTags(tags []string) ([]string, error) {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if len([]rune(t)) > model.MemoryTagMaxRunes {
			return nil, errs.New(errs.CodeInvalidParams, "标签过长（上限 32 字）: "+t)
		}
		out = append(out, t)
	}
	if len(out) > model.MemoryTagsMax {
		return nil, errs.New(errs.CodeInvalidParams, "标签过多（上限 16 个）")
	}
	return out, nil
}

// validateNamespaceName 校验命名空间名：空（= 未分类）合法；"none" / "all" 为过滤保留值。
func validateNamespaceName(name string) error {
	switch name {
	case "none":
		return errs.New(errs.CodeInvalidParams, "「none」是未分类过滤保留值，不能作为命名空间名")
	case "all":
		return errs.New(errs.CodeInvalidParams, "「all」是全部记忆保留值，不能作为命名空间名")
	}
	if len([]rune(name)) > model.MemoryNamespaceMaxRunes {
		return errs.New(errs.CodeInvalidParams, "命名空间名过长（上限 64 字）")
	}
	return nil
}

// sourceOf 写入来源归一化（HTTP/MCP/控制台/系统任务）。
func sourceOf(source string) string {
	switch strings.TrimSpace(source) {
	case string(model.MemorySourceMCP):
		return string(model.MemorySourceMCP)
	case string(model.MemorySourceConsole):
		return string(model.MemorySourceConsole)
	case string(model.MemorySourceSystem):
		return string(model.MemorySourceSystem)
	default:
		return string(model.MemorySourceAPI)
	}
}

func defaultInt(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

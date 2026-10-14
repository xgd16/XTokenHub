package service

// 自动关联：写入/更新记忆时按内容相似度自动维护 related 出边（规则式、零 LLM 成本）。
// 打分用 IDF 加权覆盖度：稀有 token 权重高、通用词权重低；只连接同命名空间
// （未分类连未分类），避免跨主题误连。自动边统一 created_by=system（autoLinkActor），
// 重算时只回收它建出的边，人工/agent 的显式关系不受影响。

import (
	"context"
	"fmt"
	"math"
	"sort"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/logger"
	"xtokenhub/internal/pkg/textseg"
	"xtokenhub/internal/repository"
)

const (
	// autoLinkLimit 单条记忆最多自动关联的条数。
	autoLinkLimit = 3
	// autoLinkPoolMax 候选池上限（同命名空间按更新时间倒序取）。
	autoLinkPoolMax = 300
	// autoLinkContentRunes 参与相似度计算的正文截断（runes）。
	autoLinkContentRunes = 1200
	// autoLinkMinShared 至少共享的 token 数（挡住极短文本的误报）。
	autoLinkMinShared = 6
	// autoLinkMinScore IDF 加权覆盖度阈值（0~1；按真实库分布校准）。
	autoLinkMinScore = 0.10
	// autoLinkRebuildPage 全库重算的分页大小。
	autoLinkRebuildPage = 200
)

// autoLinkActor 自动边的建边者标记（与显式边区分，重算只回收它）。
const autoLinkActor = string(model.MemorySourceSystem)

// AutoLinkRebuildResult 全库重算自动关系的统计。
type AutoLinkRebuildResult struct {
	// Processed 处理的记忆条数。
	Processed int `json:"processed"`
	// Created 重算后存活的自动边条数。
	Created int `json:"created"`
}

// autoLinkHit 打分命中（目标 + 得分）。
type autoLinkHit struct {
	memory *model.Memory
	score  float64
}

// autoLinkTokens 记忆参与比较的 token 集：标题 + 正文前段（textseg 管线切分）。
func autoLinkTokens(m *model.Memory) map[string]struct{} {
	set := make(map[string]struct{}, 128)
	for _, t := range textseg.Tokens(m.Title + "\n" + textseg.Clip(m.Content, autoLinkContentRunes)) {
		set[t] = struct{}{}
	}
	return set
}

// autoLinkScore base 与候选的相似度（IDF 加权覆盖度：共享权重 ÷ 较矮一方总权重）
// 及共享 token 数。覆盖度比 Dice 稳：短笔记对长文档不被长文稀释。
func autoLinkScore(base, cand map[string]struct{}, idf map[string]float64) (float64, int) {
	shared, weight, sumA, sumB := 0, 0.0, 0.0, 0.0
	for t := range base {
		w := idf[t]
		sumA += w
		if _, ok := cand[t]; ok {
			shared++
			weight += w
		}
	}
	if shared == 0 {
		return 0, 0
	}
	for t := range cand {
		sumB += idf[t]
	}
	denom := sumA
	if sumB < denom {
		denom = sumB
	}
	if denom == 0 {
		return 0, 0
	}
	return weight / denom, shared
}

// autoLinkRank 候选池打分排序，返回达阈值的前 limit 条（分数降序）。
// IDF 由池内文档频率统计（含 base），保证同一池内分数可比。
func autoLinkRank(base map[string]struct{}, pool []model.Memory, limit int) []autoLinkHit {
	n := len(pool) + 1
	df := make(map[string]int, 512)
	for t := range base {
		df[t]++
	}
	sets := make([]map[string]struct{}, len(pool))
	for i := range pool {
		sets[i] = autoLinkTokens(&pool[i])
		for t := range sets[i] {
			df[t]++
		}
	}
	idf := make(map[string]float64, len(df))
	for t, c := range df {
		idf[t] = math.Log(float64(n+1)/float64(c+1)) + 1
	}
	hits := make([]autoLinkHit, 0, len(pool))
	for i := range pool {
		score, shared := autoLinkScore(base, sets[i], idf)
		if shared < autoLinkMinShared || score < autoLinkMinScore {
			continue
		}
		hits = append(hits, autoLinkHit{memory: &pool[i], score: score})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// autoLinkPool 候选池：同命名空间（未分类则同未分类池）的活跃未过期记忆（不含自身）。
func (s *MemoryService) autoLinkPool(ctx context.Context, m *model.Memory) ([]model.Memory, error) {
	f := repository.MemoryFilter{
		Status: string(model.MemoryStatusActive), ExcludeExpired: true, Now: s.now(),
	}
	if m.Namespace == "" {
		f.Uncategorized = true
	} else {
		f.Namespace = m.Namespace
	}
	items, _, err := s.memories.List(ctx, f, "updated", 0, autoLinkPoolMax)
	if err != nil {
		return nil, err
	}
	out := make([]model.Memory, 0, len(items))
	for i := range items {
		if items[i].ID != m.ID {
			out = append(out, items[i])
		}
	}
	return out, nil
}

// autoLink 为一条记忆建立自动 related 出边；已有任何边（任意方向）的两点不重复建。
// 失败只记日志，不影响写入主流程。
func (s *MemoryService) autoLink(ctx context.Context, m *model.Memory) []model.MemoryLink {
	base := autoLinkTokens(m)
	if len(base) < autoLinkMinShared {
		return nil // 内容过短，无法可靠比较
	}
	pool, err := s.autoLinkPool(ctx, m)
	if err != nil {
		logger.L("memory-autolink").Warn("读取候选池失败", logger.Err(err), "id", m.ID)
		return nil
	}
	if len(pool) == 0 {
		return nil
	}
	hits := autoLinkRank(base, pool, autoLinkLimit)
	if len(hits) == 0 {
		return nil
	}
	// 已有点对（显式/自动、任意方向）不重复建边。
	existing := make(map[int64]struct{}, 8)
	if links, err := s.links.ListByMemory(ctx, m.ID); err == nil {
		for _, l := range links {
			if l.FromID == m.ID {
				existing[l.ToID] = struct{}{}
			} else {
				existing[l.FromID] = struct{}{}
			}
		}
	}
	out := make([]model.MemoryLink, 0, len(hits))
	for _, h := range hits {
		if _, ok := existing[h.memory.ID]; ok {
			continue
		}
		link, err := s.links.Add(ctx, &model.MemoryLink{
			FromID: m.ID, ToID: h.memory.ID, Relation: model.MemoryLinkRelated,
			Note:      fmt.Sprintf("自动关联（相似度 %d%%）", int(math.Round(h.score*100))),
			CreatedBy: autoLinkActor,
		})
		if err != nil {
			logger.L("memory-autolink").Warn("建立自动边失败", logger.Err(err), "from", m.ID, "to", h.memory.ID)
			continue
		}
		out = append(out, *link)
	}
	return out
}

// refreshAutoLinks 重算一条记忆的自动出边：先回收旧自动边，再按最新内容重建。
func (s *MemoryService) refreshAutoLinks(ctx context.Context, m *model.Memory) []model.MemoryLink {
	if _, err := s.links.DeleteAutoByMemory(ctx, m.ID, autoLinkActor); err != nil {
		logger.L("memory-autolink").Warn("回收旧自动边失败", logger.Err(err), "id", m.ID)
		return nil
	}
	return s.autoLink(ctx, m)
}

// RebuildAutoLinks 全库重算自动关系（控制台「重算关系」）：逐条刷新出边，返回统计。
func (s *MemoryService) RebuildAutoLinks(ctx context.Context) (*AutoLinkRebuildResult, error) {
	res := &AutoLinkRebuildResult{}
	f := repository.MemoryFilter{Status: string(model.MemoryStatusActive)}
	for offset := 0; ; offset += autoLinkRebuildPage {
		items, total, err := s.memories.List(ctx, f, "updated", offset, autoLinkRebuildPage)
		if err != nil {
			return nil, errs.Wrap(errs.CodeInternal, "读取记忆失败", err)
		}
		for i := range items {
			res.Created += len(s.refreshAutoLinks(ctx, &items[i]))
			res.Processed++
		}
		if len(items) == 0 || int64(offset+len(items)) >= total {
			break
		}
	}
	return res, nil
}

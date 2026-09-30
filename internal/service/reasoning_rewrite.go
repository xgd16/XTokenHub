package service

import (
	"context"
	"strings"
	"sync"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/logger"
	"xtokenhub/internal/pkg/pagination"
	"xtokenhub/internal/repository"
)

// ReasoningRewriteService 思维档位改写规则管理：按上游模型把客户端的思维档位
// （reasoning_effort 等）翻译成 thinking 开关，供只认开/关的上游（如 MiMo）正常解析。
type ReasoningRewriteService struct {
	repo repository.ReasoningRewriteRepository

	mu    sync.RWMutex
	index map[string]model.ReasoningRewrite // key: 上游真实模型名
	stale bool
}

// NewReasoningRewriteService 构造。
func NewReasoningRewriteService(repo repository.ReasoningRewriteRepository) *ReasoningRewriteService {
	return &ReasoningRewriteService{repo: repo, stale: true}
}

// ReasoningRewriteInput 创建/更新入参。Mapping 为档位映射数组（与线上 schema 一致），
// 目标只允许 enabled/disabled；Missing 为客户端未带档位时的注入（空 = 不注入）。
type ReasoningRewriteInput struct {
	Model   string                   `json:"model" binding:"required,max=128"`
	Mapping []model.ReasoningMapping `json:"mapping"`
	Missing string                   `json:"missing"`
	Status  *int                     `json:"status"`
	Remark  string                   `json:"remark" binding:"max=512"`
}

// build 由入参构造规范化实体：档位归一化小写、目标只允许开关值、档位去重。
func (s *ReasoningRewriteService) build(in *ReasoningRewriteInput) (*model.ReasoningRewrite, error) {
	in.Model = strings.TrimSpace(in.Model)
	if in.Model == "" {
		return nil, errs.New(errs.CodeInvalidParams, "模型名不能为空")
	}
	rule := &model.ReasoningRewrite{Model: in.Model, Remark: in.Remark}
	seen := map[string]bool{}
	for _, m := range in.Mapping {
		level := model.NormalizeTier(m.Level)
		target := strings.TrimSpace(m.Target)
		if !model.IsReasoningTier(level) {
			return nil, errs.New(errs.CodeInvalidParams, "未知档位: "+m.Level)
		}
		if !model.IsReasoningSwitch(target) {
			return nil, errs.New(errs.CodeInvalidParams, "档位目标只支持 enabled/disabled")
		}
		if seen[level] {
			return nil, errs.New(errs.CodeInvalidParams, "档位重复: "+level)
		}
		seen[level] = true
		rule.Mapping = append(rule.Mapping, model.ReasoningMapping{Level: level, Target: target})
	}
	missing := strings.TrimSpace(in.Missing)
	if missing != "" && !model.IsReasoningSwitch(missing) {
		return nil, errs.New(errs.CodeInvalidParams, "missing 只支持 enabled/disabled 或留空")
	}
	rule.Missing = missing
	if in.Status != nil {
		rule.Status = *in.Status
	} else {
		rule.Status = 1
	}
	if rule.Status != 0 && rule.Status != 1 {
		return nil, errs.New(errs.CodeInvalidParams, "status 只支持 0/1")
	}
	return rule, nil
}

// Create 创建规则（模型名唯一）。
func (s *ReasoningRewriteService) Create(ctx context.Context, in *ReasoningRewriteInput) (*model.ReasoningRewrite, error) {
	rule, err := s.build(in)
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.GetByModel(ctx, rule.Model); err == nil {
		return nil, errs.New(errs.CodeInvalidParams, "该模型已有改写规则: "+rule.Model)
	}
	if err := s.repo.Create(ctx, rule); err != nil {
		return nil, err
	}
	s.invalidate()
	logger.L("reasoning-rewrite").Info("created", "model", rule.Model)
	return rule, nil
}

// Update 更新规则。
func (s *ReasoningRewriteService) Update(ctx context.Context, id int64, in *ReasoningRewriteInput) (*model.ReasoningRewrite, error) {
	rule, err := s.build(in)
	if err != nil {
		return nil, err
	}
	if dup, err := s.repo.GetByModel(ctx, rule.Model); err == nil && dup.ID != id {
		return nil, errs.New(errs.CodeInvalidParams, "该模型已有改写规则: "+rule.Model)
	}
	rule.ID = id
	if err := s.repo.Update(ctx, rule); err != nil {
		return nil, err
	}
	updated, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s.invalidate()
	logger.L("reasoning-rewrite").Info("updated", "model", updated.Model)
	return updated, nil
}

// Delete 删除规则。
func (s *ReasoningRewriteService) Delete(ctx context.Context, id int64) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// Get 查询。
func (s *ReasoningRewriteService) Get(ctx context.Context, id int64) (*model.ReasoningRewrite, error) {
	return s.repo.GetByID(ctx, id)
}

// List 分页列表，q 为模型名模糊匹配（空 = 不限）。
func (s *ReasoningRewriteService) List(ctx context.Context, p pagination.Params, q string) ([]model.ReasoningRewrite, int64, error) {
	return s.repo.ListPaged(ctx, q, p.Offset, p.Limit)
}

// RewriteFor 网关热路径：按上游模型名取启用中的改写规则（内存索引，写时失效）。
// 读取失败时沿用旧索引，保证网关不受数据库故障拖垮。
func (s *ReasoningRewriteService) RewriteFor(ctx context.Context, upModel string) (model.ReasoningRewrite, bool) {
	if upModel == "" {
		return model.ReasoningRewrite{}, false
	}
	idx, err := s.loadIndex(ctx)
	if err != nil {
		return model.ReasoningRewrite{}, false
	}
	rule, ok := idx[upModel]
	if !ok || rule.Status != 1 {
		return model.ReasoningRewrite{}, false
	}
	return rule, true
}

// loadIndex 返回规则索引（双检加锁；stale 时从库重建）。
func (s *ReasoningRewriteService) loadIndex(ctx context.Context) (map[string]model.ReasoningRewrite, error) {
	s.mu.RLock()
	if !s.stale && s.index != nil {
		idx := s.index
		s.mu.RUnlock()
		return idx, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.stale && s.index != nil {
		return s.index, nil
	}
	rows, err := s.repo.List(ctx)
	if err != nil {
		// 保留旧索引继续服务（首次加载失败则视为无规则）
		s.stale = s.index == nil
		logger.L("reasoning-rewrite").Warn("读取改写规则失败，沿用旧索引", logger.Err(err))
		return s.index, err
	}
	idx := make(map[string]model.ReasoningRewrite, len(rows))
	for _, r := range rows {
		idx[r.Model] = r
	}
	s.index = idx
	s.stale = false
	return idx, nil
}

// invalidate 规则变更后使内存索引失效。
func (s *ReasoningRewriteService) invalidate() {
	s.mu.Lock()
	s.stale = true
	s.mu.Unlock()
}

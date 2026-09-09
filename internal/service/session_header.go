package service

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/repository"
)

// SessionHeaderConfigService 会话标识配置业务。
// 网关按此配置的 Header 列表识别调用方会话标识（默认 X-Session-Id）。
type SessionHeaderConfigService struct {
	repo repository.SessionHeaderConfigRepository

	// 启用的 Header 列表缓存：网关热路径每次请求都要读取，
	// 缓存 + 失效标记避免每个请求都打一次数据库。
	mu        sync.RWMutex
	cache     []string // 启用的 Header 名称（canonical MIME 格式）
	cacheAt   time.Time
	stale     bool // 配置变更后置为 true，下次读取强制刷新
	cacheTTL  time.Duration
}

// 默认会话 Header：ZCode 等客户端的标准会话标识。
const defaultSessionHeader = "X-Session-Id"

const sessionHeaderCacheTTL = 30 * time.Second

// NewSessionHeaderConfigService 构造。首次使用时种子化默认配置。
func NewSessionHeaderConfigService(repo repository.SessionHeaderConfigRepository) *SessionHeaderConfigService {
	return &SessionHeaderConfigService{repo: repo, cacheTTL: sessionHeaderCacheTTL}
}

// EnsureDefaults 种子化默认配置：表为空时写入 X-Session-Id（ZCode 默认会话标识）。
// 幂等，可重复调用；启动时执行一次即可。
func (s *SessionHeaderConfigService) EnsureDefaults(ctx context.Context) error {
	var count int64
	if err := s.repo.Count(ctx, &count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	key := headerKey(defaultSessionHeader)
	err := s.repo.Create(ctx, &model.SessionHeaderConfig{
		Key:         key,
		HeaderName:  defaultSessionHeader,
		Description: "ZCode 默认会话标识",
		Enabled:     true,
	})
	if err != nil {
		return err
	}
	slog.Info("已种子化默认会话标识配置", "header", defaultSessionHeader)
	s.invalidate()
	return nil
}

// headerKey 配置键：小写、空格转连字符（与 Header 名大小写不敏感语义对齐）。
func headerKey(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), " ", "-")
}

// List 获取所有配置。
func (s *SessionHeaderConfigService) List(ctx context.Context) ([]model.SessionHeaderConfig, error) {
	return s.repo.List(ctx)
}

// CreateInput 创建配置入参。
type CreateSessionHeaderInput struct {
	HeaderName  string `json:"header_name" binding:"required,max=256"`
	Description string `json:"description" binding:"max=512"`
}

// Create 创建配置。
func (s *SessionHeaderConfigService) Create(ctx context.Context, in *CreateSessionHeaderInput) (*model.SessionHeaderConfig, error) {
	name := strings.TrimSpace(in.HeaderName)
	if name == "" {
		return nil, errs.New(errs.CodeInvalidParams, "Header 名称不能为空")
	}
	key := headerKey(name)

	// 检查是否已存在（Header 名大小写不敏感，用 key 判重）
	existing, err := s.repo.GetByKey(ctx, key)
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	if existing != nil {
		return nil, errs.New(errs.CodeInvalidParams, "该 Header 配置已存在")
	}

	config := &model.SessionHeaderConfig{
		Key:         key,
		HeaderName:  name,
		Description: strings.TrimSpace(in.Description),
		Enabled:     true,
	}
	if err := s.repo.Create(ctx, config); err != nil {
		return nil, err
	}
	s.invalidate()
	return config, nil
}

// UpdateSessionHeaderInput 更新配置入参。
type UpdateSessionHeaderInput struct {
	Description string `json:"description" binding:"max=512"`
	Enabled     *bool  `json:"enabled"`
}

// Update 更新配置（Header 名称创建后不可改，避免历史日志的 session_id 与新配置对不上）。
func (s *SessionHeaderConfigService) Update(ctx context.Context, id int64, in *UpdateSessionHeaderInput) (*model.SessionHeaderConfig, error) {
	config, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	config.Description = strings.TrimSpace(in.Description)
	if in.Enabled != nil {
		config.Enabled = *in.Enabled
	}
	if err := s.repo.Update(ctx, config); err != nil {
		return nil, err
	}
	s.invalidate()
	return config, nil
}

// Delete 删除配置。
func (s *SessionHeaderConfigService) Delete(ctx context.Context, id int64) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// ToggleEnabled 切换启用状态（按配置 key）。
func (s *SessionHeaderConfigService) ToggleEnabled(ctx context.Context, key string) (*model.SessionHeaderConfig, error) {
	config, err := s.repo.GetByKey(ctx, key)
	if err != nil {
		return nil, err
	}
	config.Enabled = !config.Enabled
	if err := s.repo.Update(ctx, config); err != nil {
		return nil, err
	}
	s.invalidate()
	return config, nil
}

// EnabledHeaders 网关热路径使用：返回启用的会话 Header 名单（canonical 格式）。
// 走缓存 + TTL + 失效标记；数据库不可用时回退到默认 X-Session-Id，保证网关不受配置故障拖垮。
func (s *SessionHeaderConfigService) EnabledHeaders(ctx context.Context) []string {
	s.mu.RLock()
	if !s.stale && s.cache != nil && time.Since(s.cacheAt) < s.cacheTTL {
		defer s.mu.RUnlock()
		return s.cache
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	// 双检：拿写锁期间可能已被其他 goroutine 刷新
	if !s.stale && s.cache != nil && time.Since(s.cacheAt) < s.cacheTTL {
		return s.cache
	}
	configs, err := s.repo.ListEnabled(ctx)
	if err != nil {
		slog.Warn("读取会话 Header 配置失败，回退默认", slog.String("err", err.Error()))
		return []string{defaultSessionHeader}
	}
	headers := make([]string, 0, len(configs))
	for _, c := range configs {
		if h := strings.TrimSpace(c.HeaderName); h != "" {
			headers = append(headers, http.CanonicalHeaderKey(h))
		}
	}
	if len(headers) == 0 {
		headers = []string{defaultSessionHeader}
	}
	s.cache = headers
	s.cacheAt = time.Now()
	s.stale = false
	return headers
}

// invalidate 配置变更后使缓存失效。
func (s *SessionHeaderConfigService) invalidate() {
	s.mu.Lock()
	s.stale = true
	s.mu.Unlock()
}

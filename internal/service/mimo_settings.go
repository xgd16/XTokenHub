package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/repository"
)

// MimoSettingsService MiMo 网页余额凭证业务：存网页控制台的会话 Cookie，
// 供渠道余额查询按 Cookie 鉴权重放平台接口。凭证由小米 passport 服务端管理，
// 过期后余额查询报 401，需回网页复制最新 Cookie 更新。
type MimoSettingsService struct {
	repo repository.MimoSettingsRepository
	// onCookieChanged 凭证更新后回调（清余额缓存，让下次查询立即用新 Cookie）。
	onCookieChanged func()
}

// NewMimoSettingsService 构造 MiMo 网页凭证业务。
func NewMimoSettingsService(repo repository.MimoSettingsRepository) *MimoSettingsService {
	return &MimoSettingsService{repo: repo}
}

// SetOnCookieChanged 注入凭证变更回调（main 装配时指向渠道余额缓存失效）。
func (s *MimoSettingsService) SetOnCookieChanged(fn func()) {
	if fn == nil {
		return
	}
	s.onCookieChanged = fn
}

// EnsureDefaults 首次启动播种空行（与数据管理设置同款）。
func (s *MimoSettingsService) EnsureDefaults(ctx context.Context) error {
	_, err := s.repo.Get(ctx)
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return s.repo.Save(ctx, &model.MimoSettings{ID: 1, UpdatedAt: time.Now()})
}

// Get 读取当前凭证。
func (s *MimoSettingsService) Get(ctx context.Context) (*model.MimoSettings, error) {
	return s.repo.Get(ctx)
}

// Cookie 供余额查询读取当前凭证（行不存在视为未配置，返回空串）。
func (s *MimoSettingsService) Cookie(ctx context.Context) (string, error) {
	row, err := s.repo.Get(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return row.Cookie, nil
}

// UpdateCookie 保存新 Cookie：去掉首尾空白；非空时必须含 serviceToken
// （网页接口的最小凭证为 userId + api-platform_serviceToken，缺了必然 401）。
// 传空串清除凭证。保存后触发回调清余额缓存。
func (s *MimoSettingsService) UpdateCookie(ctx context.Context, cookie string) (*model.MimoSettings, error) {
	cookie = strings.TrimSpace(cookie)
	if cookie != "" && !strings.Contains(cookie, "serviceToken") {
		return nil, errs.New(errs.CodeInvalidParams,
			"Cookie 中未找到 serviceToken：请从浏览器 DevTools 复制完整 Cookie 请求头（最小需要 userId 与 api-platform_serviceToken）")
	}
	if err := s.repo.UpdateCookie(ctx, cookie); err != nil {
		return nil, errs.Wrap(errs.CodeInternal, "保存 MiMo 凭证失败", err)
	}
	if s.onCookieChanged != nil {
		s.onCookieChanged()
	}
	return s.repo.Get(ctx)
}

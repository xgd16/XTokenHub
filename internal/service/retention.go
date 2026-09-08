package service

import (
	"context"

	"xtokenhub/internal/cleanup"
)

// RetentionService 日志保留期清理业务：包装 cleanup.Retention，供 handler 层调用。
type RetentionService struct {
	retention *cleanup.Retention
}

// NewRetentionService 构造清理业务。
func NewRetentionService(retention *cleanup.Retention) *RetentionService {
	return &RetentionService{retention: retention}
}

// Trigger 手动触发一次清理。
func (s *RetentionService) Trigger(ctx context.Context) (*cleanup.Result, error) {
	return s.retention.Trigger(ctx)
}

// Status 清理状态。
func (s *RetentionService) Status() cleanup.StatusView {
	return s.retention.Status()
}

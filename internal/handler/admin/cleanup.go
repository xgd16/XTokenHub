// Package admin 管理端 HTTP handler 层：参数绑定与响应封装，业务逻辑在 service。
package admin

import (
	"github.com/gin-gonic/gin"

	"xtokenhub/internal/pkg/resp"
	"xtokenhub/internal/service"
)

// CleanupHandler 日志清理接口。
type CleanupHandler struct {
	svc *service.RetentionService
}

// NewCleanupHandler 构造。
func NewCleanupHandler(svc *service.RetentionService) *CleanupHandler {
	return &CleanupHandler{svc: svc}
}

// Trigger POST /api/v1/logs/cleanup —— 手动触发一次清理，返回本次删除行数/耗时/cutoff。
func (h *CleanupHandler) Trigger(c *gin.Context) {
	res, err := h.svc.Trigger(c.Request.Context())
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, res)
}

// Status GET /api/v1/logs/cleanup —— 清理状态（enabled、模式/时刻、上次运行信息）。
func (h *CleanupHandler) Status(c *gin.Context) {
	sv, err := h.svc.Status(c.Request.Context())
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, sv)
}

package admin

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/resp"
	"xtokenhub/internal/service"
)

// SessionHeaderConfigHandler 会话标识配置接口。
type SessionHeaderConfigHandler struct {
	svc *service.SessionHeaderConfigService
}

// NewSessionHeaderConfigHandler 构造。
func NewSessionHeaderConfigHandler(svc *service.SessionHeaderConfigService) *SessionHeaderConfigHandler {
	return &SessionHeaderConfigHandler{svc: svc}
}

// List GET /api/v1/settings/session-headers
func (h *SessionHeaderConfigHandler) List(c *gin.Context) {
	configs, err := h.svc.List(c.Request.Context())
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{"configs": configs})
}

// Create POST /api/v1/settings/session-headers
func (h *SessionHeaderConfigHandler) Create(c *gin.Context) {
	var in service.CreateSessionHeaderInput
	if err := c.ShouldBindJSON(&in); err != nil {
		resp.Fail(c, errs.Wrap(errs.CodeInvalidParams, "参数错误", err))
		return
	}
	config, err := h.svc.Create(c.Request.Context(), &in)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, config)
}

// Delete DELETE /api/v1/settings/session-headers/:id
func (h *SessionHeaderConfigHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "非法 id"))
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, nil)
}

// ToggleEnabled PUT /api/v1/settings/session-headers/:key/toggle
func (h *SessionHeaderConfigHandler) ToggleEnabled(c *gin.Context) {
	key := c.Param("key")
	if key == "" {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "key 不能为空"))
		return
	}
	config, err := h.svc.ToggleEnabled(c.Request.Context(), key)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, config)
}

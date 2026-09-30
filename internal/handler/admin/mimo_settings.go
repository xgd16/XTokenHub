package admin

import (
	"github.com/gin-gonic/gin"

	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/resp"
	"xtokenhub/internal/service"
)

// MimoSettingsHandler MiMo 网页余额凭证接口。
type MimoSettingsHandler struct {
	svc *service.MimoSettingsService
}

// NewMimoSettingsHandler 构造。
func NewMimoSettingsHandler(svc *service.MimoSettingsService) *MimoSettingsHandler {
	return &MimoSettingsHandler{svc: svc}
}

// Get GET /api/v1/settings/mimo —— 当前凭证（与渠道 api_key 同一安全姿态，明文回显）。
func (h *MimoSettingsHandler) Get(c *gin.Context) {
	row, err := h.svc.Get(c.Request.Context())
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, row)
}

// Update PUT /api/v1/settings/mimo —— 保存凭证（保存后即清余额缓存，下次查询用新值）。
func (h *MimoSettingsHandler) Update(c *gin.Context) {
	var in struct {
		Cookie string `json:"cookie"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		resp.Fail(c, errs.Wrap(errs.CodeInvalidParams, "参数错误", err))
		return
	}
	row, err := h.svc.UpdateCookie(c.Request.Context(), in.Cookie)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, row)
}

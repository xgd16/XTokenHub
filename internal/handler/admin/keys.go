package admin

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/pagination"
	"xtokenhub/internal/pkg/resp"
	"xtokenhub/internal/service"
)

// KeyHandler 网关密钥管理接口。
type KeyHandler struct {
	svc *service.KeyService
}

// NewKeyHandler 构造。
func NewKeyHandler(svc *service.KeyService) *KeyHandler {
	return &KeyHandler{svc: svc}
}

// List GET /api/v1/keys
func (h *KeyHandler) List(c *gin.Context) {
	p := pagination.FromGin(c)
	items, total, err := h.svc.List(c.Request.Context(), p)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKPage(c, items, total, p.Page, p.PerPage)
}

// Create POST /api/v1/keys —— 响应包含生成的 key 原文。
func (h *KeyHandler) Create(c *gin.Context) {
	var in service.KeyInput
	if err := c.ShouldBindJSON(&in); err != nil {
		resp.Fail(c, errs.Wrap(errs.CodeInvalidParams, "参数错误", err))
		return
	}
	k, err := h.svc.Create(c.Request.Context(), &in)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, k)
}

// Update PUT /api/v1/keys/:id
func (h *KeyHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "非法 id"))
		return
	}
	var in service.KeyInput
	if err := c.ShouldBindJSON(&in); err != nil {
		resp.Fail(c, errs.Wrap(errs.CodeInvalidParams, "参数错误", err))
		return
	}
	k, err := h.svc.Update(c.Request.Context(), id, &in)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, k)
}

// Delete DELETE /api/v1/keys/:id
func (h *KeyHandler) Delete(c *gin.Context) {
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

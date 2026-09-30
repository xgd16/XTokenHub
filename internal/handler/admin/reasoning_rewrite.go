package admin

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/pagination"
	"xtokenhub/internal/pkg/resp"
	"xtokenhub/internal/service"
)

// ReasoningRewriteHandler 思维档位改写规则管理接口。
type ReasoningRewriteHandler struct {
	svc *service.ReasoningRewriteService
}

// NewReasoningRewriteHandler 构造。
func NewReasoningRewriteHandler(svc *service.ReasoningRewriteService) *ReasoningRewriteHandler {
	return &ReasoningRewriteHandler{svc: svc}
}

// List GET /api/v1/reasoning-rewrites
func (h *ReasoningRewriteHandler) List(c *gin.Context) {
	p := pagination.FromGin(c)
	items, total, err := h.svc.List(c.Request.Context(), p, c.Query("q"))
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKPage(c, items, total, p.Page, p.PerPage)
}

// Create POST /api/v1/reasoning-rewrites
func (h *ReasoningRewriteHandler) Create(c *gin.Context) {
	var in service.ReasoningRewriteInput
	if err := c.ShouldBindJSON(&in); err != nil {
		resp.Fail(c, errs.Wrap(errs.CodeInvalidParams, "参数错误", err))
		return
	}
	rule, err := h.svc.Create(c.Request.Context(), &in)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, rule)
}

// Update PUT /api/v1/reasoning-rewrites/:id
func (h *ReasoningRewriteHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "非法 id"))
		return
	}
	var in service.ReasoningRewriteInput
	if err := c.ShouldBindJSON(&in); err != nil {
		resp.Fail(c, errs.Wrap(errs.CodeInvalidParams, "参数错误", err))
		return
	}
	rule, err := h.svc.Update(c.Request.Context(), id, &in)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, rule)
}

// Delete DELETE /api/v1/reasoning-rewrites/:id
func (h *ReasoningRewriteHandler) Delete(c *gin.Context) {
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

// Get GET /api/v1/reasoning-rewrites/:id
func (h *ReasoningRewriteHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "非法 id"))
		return
	}
	rule, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, rule)
}

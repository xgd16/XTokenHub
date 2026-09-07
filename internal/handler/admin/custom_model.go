package admin

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/pagination"
	"xtokenhub/internal/pkg/resp"
	"xtokenhub/internal/service"
)

// CustomModelHandler 自定义模型组管理接口。
type CustomModelHandler struct {
	svc *service.CustomModelService
}

// NewCustomModelHandler 构造。
func NewCustomModelHandler(svc *service.CustomModelService) *CustomModelHandler {
	return &CustomModelHandler{svc: svc}
}

// List GET /api/v1/custom-models
func (h *CustomModelHandler) List(c *gin.Context) {
	p := pagination.FromGin(c)
	items, total, err := h.svc.List(c.Request.Context(), p)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKPage(c, items, total, p.Page, p.PerPage)
}

// Create POST /api/v1/custom-models
func (h *CustomModelHandler) Create(c *gin.Context) {
	var in service.CustomModelInput
	if err := c.ShouldBindJSON(&in); err != nil {
		resp.Fail(c, errs.Wrap(errs.CodeInvalidParams, "参数错误", err))
		return
	}
	cm, err := h.svc.Create(c.Request.Context(), &in)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, cm)
}

// Update PUT /api/v1/custom-models/:id
func (h *CustomModelHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "非法 id"))
		return
	}
	var in service.CustomModelInput
	if err := c.ShouldBindJSON(&in); err != nil {
		resp.Fail(c, errs.Wrap(errs.CodeInvalidParams, "参数错误", err))
		return
	}
	cm, err := h.svc.Update(c.Request.Context(), id, &in)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, cm)
}

// Delete DELETE /api/v1/custom-models/:id
func (h *CustomModelHandler) Delete(c *gin.Context) {
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

// Get GET /api/v1/custom-models/:id
func (h *CustomModelHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "非法 id"))
		return
	}
	cm, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, cm)
}

package admin

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/pagination"
	"xtokenhub/internal/pkg/resp"
	"xtokenhub/internal/repository"
	"xtokenhub/internal/service"
)

// PricingHandler 计价相关接口：模型价格表、计费设置、花费预测与费用重算。
// 价格表与计费设置属设置域（/settings/*），预测与重算属统计域（/stats/cost/*）。
type PricingHandler struct {
	svc *service.CostService
}

// NewPricingHandler 构造。
func NewPricingHandler(svc *service.CostService) *PricingHandler {
	return &PricingHandler{svc: svc}
}

// ListPrices GET /api/v1/settings/prices?q=&used_only=&page=&per_page=
// 响应同时带上同步状态，避免设置页多打一次请求。
func (h *PricingHandler) ListPrices(c *gin.Context) {
	p := pagination.FromGin(c)
	usedOnly := c.Query("used_only") == "1" || c.Query("used_only") == "true"
	items, total, err := h.svc.ListPrices(c.Request.Context(), c.Query("q"), usedOnly, p.Offset, p.Limit)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{
		"items":    items,
		"total":    total,
		"page":     p.Page,
		"per_page": p.PerPage,
		"status":   h.svc.Status(c.Request.Context()),
		"unpriced": h.unpriced(c, 24*30),
		"billing":  h.billingOrNil(c),
	})
}

// CreatePrice POST /api/v1/settings/prices
func (h *PricingHandler) CreatePrice(c *gin.Context) {
	var in service.PriceInput
	if err := c.ShouldBindJSON(&in); err != nil {
		resp.Fail(c, errs.Wrap(errs.CodeInvalidParams, "参数错误", err))
		return
	}
	p, err := h.svc.CreatePrice(c.Request.Context(), &in)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, p)
}

// UpdatePrice PUT /api/v1/settings/prices/:id
func (h *PricingHandler) UpdatePrice(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "非法 id"))
		return
	}
	var in service.PriceInput
	if err := c.ShouldBindJSON(&in); err != nil {
		resp.Fail(c, errs.Wrap(errs.CodeInvalidParams, "参数错误", err))
		return
	}
	p, err := h.svc.UpdatePrice(c.Request.Context(), id, &in)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, p)
}

// DeletePrice DELETE /api/v1/settings/prices/:id
func (h *PricingHandler) DeletePrice(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "非法 id"))
		return
	}
	if err := h.svc.DeletePrice(c.Request.Context(), id); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, nil)
}

// SyncPrices POST /api/v1/settings/prices/sync —— 立即从公开价格表同步。
func (h *PricingHandler) SyncPrices(c *gin.Context) {
	res, err := h.svc.Sync(c.Request.Context())
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, res)
}

// GetBilling GET /api/v1/settings/billing
func (h *PricingHandler) GetBilling(c *gin.Context) {
	st, err := h.svc.Billing(c.Request.Context())
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, st)
}

// UpdateBilling PUT /api/v1/settings/billing
func (h *PricingHandler) UpdateBilling(c *gin.Context) {
	var in service.UpdateBillingInput
	if err := c.ShouldBindJSON(&in); err != nil {
		resp.Fail(c, errs.Wrap(errs.CodeInvalidParams, "参数错误", err))
		return
	}
	st, err := h.svc.UpdateBilling(c.Request.Context(), &in)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, st)
}

// Forecast GET /api/v1/stats/cost/forecast?period=today|month
func (h *PricingHandler) Forecast(c *gin.Context) {
	st, err := h.svc.Forecast(c.Request.Context(), c.DefaultQuery("period", "today"))
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, st)
}

// Unpriced GET /api/v1/stats/cost/unpriced?hours=720 —— 有用量但价格表未覆盖的模型。
func (h *PricingHandler) Unpriced(c *gin.Context) {
	hours, _ := strconv.Atoi(c.DefaultQuery("hours", "720"))
	if hours <= 0 {
		hours = 720
	}
	resp.OK(c, h.unpriced(c, hours))
}

// Recompute POST /api/v1/stats/cost/recompute —— 重算历史请求费用。
// 入参（均可选）：{"from":"unix秒","to":"unix秒","only_missing":true}
func (h *PricingHandler) Recompute(c *gin.Context) {
	var in struct {
		From        int64 `json:"from"`
		To          int64 `json:"to"`
		OnlyMissing *bool `json:"only_missing"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		resp.Fail(c, errs.Wrap(errs.CodeInvalidParams, "参数错误", err))
		return
	}
	onlyMissing := true
	if in.OnlyMissing != nil {
		onlyMissing = *in.OnlyMissing
	}
	from := time.Time{}
	if in.From > 0 {
		from = time.Unix(in.From, 0)
	}
	to := time.Time{}
	if in.To > 0 {
		to = time.Unix(in.To, 0)
	}
	res, err := h.svc.Recompute(c.Request.Context(), from, to, onlyMissing)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, res)
}

// unpriced 未定价模型列表；查询失败时返回空列表，不阻塞价格表展示。
func (h *PricingHandler) unpriced(c *gin.Context, hours int) []repository.ModelUsage {
	items, err := h.svc.UnpricedModels(c.Request.Context(), time.Now().Add(-time.Duration(hours)*time.Hour), 50)
	if err != nil {
		return []repository.ModelUsage{}
	}
	if items == nil {
		return []repository.ModelUsage{}
	}
	return items
}

// billingOrNil 计费设置；读取失败时返回 nil（前端用配置默认值兜底）。
func (h *PricingHandler) billingOrNil(c *gin.Context) any {
	st, err := h.svc.Billing(c.Request.Context())
	if err != nil {
		return nil
	}
	return st
}

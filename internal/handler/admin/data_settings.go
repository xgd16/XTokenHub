package admin

import (
	"github.com/gin-gonic/gin"

	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/resp"
	"xtokenhub/internal/service"
)

// DataSettingsHandler 数据管理接口：清理配置/状态、聚合状态与手动操作。
type DataSettingsHandler struct {
	svc *service.DataSettingsService
}

// NewDataSettingsHandler 构造。
func NewDataSettingsHandler(svc *service.DataSettingsService) *DataSettingsHandler {
	return &DataSettingsHandler{svc: svc}
}

// Get GET /api/v1/settings/data —— 清理配置/状态 + 聚合状态。
func (h *DataSettingsHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()
	row, err := h.svc.Settings(ctx)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	agg, err := h.svc.AggregateStatus(ctx)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{"cleanup": row, "aggregate": agg})
}

// Update PUT /api/v1/settings/data —— 更新清理配置（保存后后台任务下一轮生效）。
func (h *DataSettingsHandler) Update(c *gin.Context) {
	var in service.UpdateDataSettingsInput
	if err := c.ShouldBindJSON(&in); err != nil {
		resp.Fail(c, errs.Wrap(errs.CodeInvalidParams, "参数错误", err))
		return
	}
	row, err := h.svc.Update(c.Request.Context(), &in)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, row)
}

// RebuildAggregate POST /api/v1/settings/data/aggregate/rebuild —— 重建聚合。
// 入参（可选）：{"days": N}，N>0 重建近 N 天；缺省/0 全量重建（以现存明细为准，
// 已被清理的日期不会重建出来，累计统计不受影响）。
func (h *DataSettingsHandler) RebuildAggregate(c *gin.Context) {
	var in struct {
		Days int `json:"days"`
	}
	if err := c.ShouldBindJSON(&in); err != nil && err.Error() != "EOF" {
		resp.Fail(c, errs.Wrap(errs.CodeInvalidParams, "参数错误", err))
		return
	}
	if err := h.svc.RebuildAggregate(c.Request.Context(), in.Days); err != nil {
		resp.Fail(c, err)
		return
	}
	agg, err := h.svc.AggregateStatus(c.Request.Context())
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, agg)
}

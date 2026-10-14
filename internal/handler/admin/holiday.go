package admin

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/resp"
	"xtokenhub/internal/service"
)

// HolidayHandler 节假日年历接口：查询标注与手动同步（设置页）。
type HolidayHandler struct {
	svc *service.HolidayService
}

// NewHolidayHandler 构造。
func NewHolidayHandler(svc *service.HolidayService) *HolidayHandler {
	return &HolidayHandler{svc: svc}
}

// List GET /api/v1/settings/holidays?year=2026 —— 某年标注（默认今年）+ 同步状态。
// 前端峰谷卡片据此判定 !H / W 记号，避免各端各拉一份日历。
func (h *HolidayHandler) List(c *gin.Context) {
	year := time.Now().In(model.BeijingZone).Year()
	if q := c.Query("year"); q != "" {
		y, err := strconv.Atoi(q)
		if err != nil || y < 2000 || y > 2100 {
			resp.Fail(c, errs.New(errs.CodeInvalidParams, "year 非法: "+q))
			return
		}
		year = y
	}
	items, err := h.svc.ListYear(c.Request.Context(), year)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{
		"year":   year,
		"items":  items,
		"status": h.svc.Status(c.Request.Context()),
	})
}

// Sync POST /api/v1/settings/holidays/sync —— 立即同步节假日年历。
func (h *HolidayHandler) Sync(c *gin.Context) {
	res, err := h.svc.Sync(c.Request.Context())
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, res)
}

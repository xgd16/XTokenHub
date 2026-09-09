// Package admin 管理端 HTTP handler 层：参数绑定与响应封装，业务逻辑在 service。
package admin

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/pagination"
	"xtokenhub/internal/pkg/resp"
	"xtokenhub/internal/provider"
	"xtokenhub/internal/repository"
	"xtokenhub/internal/service"
	"xtokenhub/internal/ws"
)

// ChannelHandler 渠道管理接口。
type ChannelHandler struct {
	svc *service.ChannelService
}

// NewChannelHandler 构造。
func NewChannelHandler(svc *service.ChannelService) *ChannelHandler {
	return &ChannelHandler{svc: svc}
}

// List GET /api/v1/channels
func (h *ChannelHandler) List(c *gin.Context) {
	p := pagination.FromGin(c)
	items, total, err := h.svc.List(c.Request.Context(), p)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKPage(c, items, total, p.Page, p.PerPage)
}

// Create POST /api/v1/channels
func (h *ChannelHandler) Create(c *gin.Context) {
	var in service.CreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		resp.Fail(c, errs.Wrap(errs.CodeInvalidParams, "参数错误", err))
		return
	}
	ch, err := h.svc.Create(c.Request.Context(), &in)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, ch)
}

// Update PUT /api/v1/channels/:id
func (h *ChannelHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "非法 id"))
		return
	}
	var in service.CreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		resp.Fail(c, errs.Wrap(errs.CodeInvalidParams, "参数错误", err))
		return
	}
	ch, err := h.svc.Update(c.Request.Context(), id, &in)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, ch)
}

// Delete DELETE /api/v1/channels/:id
func (h *ChannelHandler) Delete(c *gin.Context) {
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

// Get GET /api/v1/channels/:id
func (h *ChannelHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "非法 id"))
		return
	}
	ch, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, ch)
}

// Probe POST /api/v1/channels/:id/probe
func (h *ChannelHandler) Probe(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "非法 id"))
		return
	}
	var in struct {
		Model string `json:"model"`
	}
	_ = c.ShouldBindJSON(&in) // body 可选
	report, err := h.svc.Probe(c.Request.Context(), id, in.Model)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, report)
}

// Balances GET /api/v1/channels/balances
// 批量查询渠道上游账户余额（按 BaseURL 推断厂家，当前支持 DeepSeek）；
// 不支持的渠道返回 supported=false，单渠道失败不阻塞整批。
func (h *ChannelHandler) Balances(c *gin.Context) {
	entries, err := h.svc.Balances(c.Request.Context())
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{"items": entries})
}

// LookupModels POST /api/v1/channels/lookup-models
// 用给定凭据拉取上游模型列表（渠道可尚未创建，供新建渠道时选择真实模型）。
// provider 可省略，按 base_url 自动推断接口风格。
func (h *ChannelHandler) LookupModels(c *gin.Context) {	var in struct {
		Provider model.ProviderType `json:"provider"`
		BaseURL  string             `json:"base_url" binding:"required,url,max=512"`
		APIKey   string             `json:"api_key" binding:"required,max=256"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		resp.Fail(c, errs.Wrap(errs.CodeInvalidParams, "参数错误", err))
		return
	}
	if in.Provider == "" {
		in.Provider = provider.InferProviderType(in.BaseURL)
	} else if !in.Provider.Valid() {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "不支持的接口风格: "+string(in.Provider)))
		return
	}
	models, err := h.svc.LookupModels(c.Request.Context(), in.Provider, in.BaseURL, in.APIKey)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{"models": models})
}

// LogHandler 请求日志接口。
type LogHandler struct {
	svc *service.LogService
}

// NewLogHandler 构造。
func NewLogHandler(svc *service.LogService) *LogHandler {
	return &LogHandler{svc: svc}
}

// List GET /api/v1/logs
func (h *LogHandler) List(c *gin.Context) {
	var in service.ListInput
	if err := c.ShouldBindQuery(&in); err != nil {
		resp.Fail(c, errs.Wrap(errs.CodeInvalidParams, "参数错误", err))
		return
	}
	p := pagination.FromGin(c)
	items, total, err := h.svc.List(c.Request.Context(), in, p)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKPage(c, items, total, p.Page, p.PerPage)
}

// StatsHandler 统计接口。
type StatsHandler struct {
	svc *service.StatsService
}

// NewStatsHandler 构造。
func NewStatsHandler(svc *service.StatsService) *StatsHandler {
	return &StatsHandler{svc: svc}
}

// parseSinceSec 读取 since 查询参数（Unix 秒，前端传本地零点即「当天」口径），缺省返回 0。
func parseSinceSec(c *gin.Context) int64 {
	v, _ := strconv.ParseInt(c.Query("since"), 10, 64)
	if v < 0 {
		return 0
	}
	return v
}

// Summary GET /api/v1/stats/summary?hours=24 或 ?since=<unix秒>（优先）
func (h *StatsHandler) Summary(c *gin.Context) {
	hours, _ := strconv.Atoi(c.DefaultQuery("hours", "24"))
	s, err := h.svc.SummaryFlex(c.Request.Context(), parseSinceSec(c), hours)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, s)
}

// Trend GET /api/v1/stats/trend?hours=168&bucket=day
// bucket: minute|hour|day；传 days 时按旧行为走按日聚合（兼容）。
func (h *StatsHandler) Trend(c *gin.Context) {
	var (
		points []repository.TrendPoint
		err    error
	)
	if c.Query("days") != "" && c.Query("hours") == "" {
		days, _ := strconv.Atoi(c.Query("days"))
		points, err = h.svc.TrendByDay(c.Request.Context(), days)
	} else {
		hours, _ := strconv.Atoi(c.DefaultQuery("hours", "168"))
		switch c.DefaultQuery("bucket", "day") {
		case "minute":
			points, err = h.svc.TrendByBucket(c.Request.Context(), hours, 60)
		case "hour":
			points, err = h.svc.TrendByBucket(c.Request.Context(), hours, 3600)
		default:
			days := (hours + 23) / 24
			points, err = h.svc.TrendByDay(c.Request.Context(), days)
		}
	}
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, points)
}

// ByModel GET /api/v1/stats/by-model?hours=24 或 ?since=<unix秒>（优先）
func (h *StatsHandler) ByModel(c *gin.Context) {
	hours, _ := strconv.Atoi(c.DefaultQuery("hours", "24"))
	items, err := h.svc.ByModelFlex(c.Request.Context(), parseSinceSec(c), hours)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, items)
}

// ByChannel GET /api/v1/stats/by-channel?hours=24
func (h *StatsHandler) ByChannel(c *gin.Context) {
	hours, _ := strconv.Atoi(c.DefaultQuery("hours", "24"))
	items, err := h.svc.ByChannel(c.Request.Context(), hours)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, items)
}

// ByKey GET /api/v1/stats/by-key?hours=24
func (h *StatsHandler) ByKey(c *gin.Context) {
	hours, _ := strconv.Atoi(c.DefaultQuery("hours", "24"))
	items, err := h.svc.ByKey(c.Request.Context(), hours)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, items)
}

// Lifetime GET /api/v1/stats/lifetime —— 全历史累计统计（总 token / 峰值日 / 最长单次耗时 / 连续天数）。
func (h *StatsHandler) Lifetime(c *gin.Context) {
	st, err := h.svc.Lifetime(c.Request.Context())
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, st)
}

// TrendByModel GET /api/v1/stats/trend-by-model?days=7 —— 按日 × 模型 token 用量（多模型趋势线）。
func (h *StatsHandler) TrendByModel(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	items, err := h.svc.TrendByDayModel(c.Request.Context(), days)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, items)
}

// LiveSessions GET /api/v1/stats/live-sessions?limit=20 —— 最近活跃会话聚合。
// 会话合计覆盖全量历史，前端只保留最近若干组，避免长会话被窗口截断后统计残缺。
func (h *StatsHandler) LiveSessions(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	items, err := h.svc.LiveSessions(c.Request.Context(), limit)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, items)
}

// ModelHandler 模型目录与用量接口。
type ModelHandler struct {
	svc *service.ModelService
}

// NewModelHandler 构造。
func NewModelHandler(svc *service.ModelService) *ModelHandler {
	return &ModelHandler{svc: svc}
}

// Usage GET /api/v1/models/usage：全部模型 + 1h/24h/7d/30d 使用量。
func (h *ModelHandler) Usage(c *gin.Context) {
	items, err := h.svc.Usage(c.Request.Context())
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, items)
}

// WSHandler WebSocket 推送端点。
type WSHandler struct {
	hub *ws.Hub
}

// NewWSHandler 构造。
func NewWSHandler(hub *ws.Hub) *WSHandler {
	return &WSHandler{hub: hub}
}

// Serve GET /api/v1/ws （升级为 WebSocket，阻塞直至断开）
func (h *WSHandler) Serve(c *gin.Context) {
	h.hub.ServeHTTP(c.Writer, c.Request)
}

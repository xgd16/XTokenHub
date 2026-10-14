// Package memory 记忆库 REST 接口：Agent 独立 API 与控制台共用同一实现，
// 挂载时用 Options 区分鉴权方式与写入身份。
package memory

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"xtokenhub/internal/middleware"
	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/pagination"
	"xtokenhub/internal/pkg/resp"
	"xtokenhub/internal/service"
)

// Options 单个挂载面的鉴权与写入身份。
type Options struct {
	// Source 写入来源标记：api | console | mcp。
	Source string
	// KeyRequired 是否强制校验 API Key（memory.require_key；控制台面传 false）。
	KeyRequired bool
	// Keys 密钥校验器（KeyRequired 时必须注入）。
	Keys middleware.KeyChecker
}

// Handler 记忆库接口。
type Handler struct {
	svc  *service.MemoryService
	opts Options
}

// NewHandler 构造。
func NewHandler(svc *service.MemoryService, opts Options) *Handler {
	return &Handler{svc: svc, opts: opts}
}

// Auth 该挂载面的鉴权中间件（控制台面为放行）。
func (h *Handler) Auth() gin.HandlerFunc {
	return middleware.APIKeyAuth(h.opts.Keys, h.opts.KeyRequired)
}

// Register 注册 Agent 面记忆 API（挂载于 /memory/v1，key 鉴权）。
func (h *Handler) Register(rg gin.IRouter) {
	h.registerMemories(rg)
	rg.GET("/namespaces", h.Namespaces)
	rg.GET("/graph", h.Graph)
	rg.GET("/stats", h.Stats)
}

// RegisterConsole 注册控制台面记忆 API（挂载于 /api/v1，沿用管理端无鉴权约定）。
// 图/统计挂在 /memories 前缀下，与 batch-move / classify 等控制台记忆接口保持同一前缀；
// 命名空间清单与管理由管理端 /api/v1/memory-namespaces 提供，不在此注册。
func (h *Handler) RegisterConsole(rg gin.IRouter) {
	h.registerMemories(rg)
	rg.GET("/memories/graph", h.Graph)
	rg.GET("/memories/stats", h.Stats)
}

// registerMemories 记忆 CRUD 与关系边（Agent 面与控制台面共用）。
func (h *Handler) registerMemories(rg gin.IRouter) {
	rg.POST("/memories", h.Add)
	rg.GET("/memories/search", h.Search)
	rg.GET("/memories", h.List)
	rg.GET("/memories/:id", h.Get)
	rg.PATCH("/memories/:id", h.Update)
	rg.DELETE("/memories/:id", h.Forget)
	rg.POST("/memories/:id/supersede", h.Supersede)
	rg.POST("/memories/:id/links", h.AddLink)
	rg.DELETE("/memories/:id/links/:linkId", h.RemoveLink)
}

// ---- 请求体 ----

type addRequest struct {
	Title         string                    `json:"title"`
	Content       string                    `json:"content"`
	Namespace     string                    `json:"namespace"`
	Kind          string                    `json:"kind"`
	Tags          []string                  `json:"tags"`
	Importance    int                       `json:"importance"`
	Pinned        bool                      `json:"pinned"`
	ExpiresAt     *string                   `json:"expires_at"`
	SourceSession string                    `json:"source_session"`
	SourceRef     string                    `json:"source_ref"`
	Supersedes    []int64                   `json:"supersedes"`
	Links         []service.MemoryLinkInput `json:"links"`
}

type updateRequest struct {
	Title        *string   `json:"title"`
	Content      *string   `json:"content"`
	Namespace    *string   `json:"namespace"`
	Kind         *string   `json:"kind"`
	Tags         *[]string `json:"tags"`
	Importance   *int      `json:"importance"`
	Pinned       *bool     `json:"pinned"`
	Status       *string   `json:"status"`
	ExpiresAt    *string   `json:"expires_at"`
	ClearExpires bool      `json:"clear_expires"`
}

type supersedeRequest struct {
	SuccessorID int64 `json:"successor_id"`
}

type linkRequest struct {
	ToID     int64  `json:"to_id"`
	Relation string `json:"relation"`
	Note     string `json:"note"`
}

// ---- 记忆 CRUD ----

// Add POST /memories —— 写入一条记忆（同命名空间同内容幂等，返回 duplicated）。
func (h *Handler) Add(c *gin.Context) {
	var req addRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "请求体非法: "+err.Error()))
		return
	}
	expiresAt, err := parseTimePtr(req.ExpiresAt)
	if err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "expires_at 非法（需 RFC3339）"))
		return
	}
	res, err := h.svc.Add(c.Request.Context(), service.MemoryAddInput{
		Title: req.Title, Content: req.Content, Namespace: req.Namespace,
		Kind: req.Kind, Tags: req.Tags, Importance: req.Importance, Pinned: req.Pinned,
		ExpiresAt: expiresAt, Source: h.opts.Source, CreatedBy: h.creator(c),
		SourceSession: req.SourceSession, SourceRef: req.SourceRef,
		Supersedes: req.Supersedes, Links: req.Links,
	})
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{
		"memory":     res.Memory,
		"duplicated": res.Duplicated,
		"superseded": res.Superseded,
		"links":      res.Links,
	})
}

// Search GET /memories/search?q=&namespace=&kind=&tags=&status=&pinned=&limit= —— 关键词检索。
func (h *Handler) Search(c *gin.Context) {
	q, err := h.queryOf(c)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	res, err := h.svc.Search(c.Request.Context(), q)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, res)
}

// List GET /memories —— 分页浏览（按置顶/更新时间等排序）。
func (h *Handler) List(c *gin.Context) {
	q, err := h.queryOf(c)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	p := pagination.FromGin(c)
	items, total, err := h.svc.List(c.Request.Context(), q, p.Offset, p.Limit)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKPage(c, items, total, p.Page, p.PerPage)
}

// Get GET /memories/:id —— 读取一条记忆。
func (h *Handler) Get(c *gin.Context) {
	id, ok := memoryID(c)
	if !ok {
		return
	}
	m, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	links, err := h.svc.Links(c.Request.Context(), id)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{"memory": m, "links": links})
}

// Update PATCH /memories/:id —— 更新（指针字段 = 提供才更新）。
func (h *Handler) Update(c *gin.Context) {
	id, ok := memoryID(c)
	if !ok {
		return
	}
	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "请求体非法: "+err.Error()))
		return
	}
	expiresAt, err := parseTimePtr(req.ExpiresAt)
	if err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "expires_at 非法（需 RFC3339）"))
		return
	}
	m, err := h.svc.Update(c.Request.Context(), id, service.MemoryUpdateInput{
		Title: req.Title, Content: req.Content, Namespace: req.Namespace, Kind: req.Kind,
		Tags: req.Tags, Importance: req.Importance, Pinned: req.Pinned, Status: req.Status,
		ExpiresAt: expiresAt, ClearExpires: req.ClearExpires,
	})
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, m)
}

// Forget DELETE /memories/:id —— 软删除（检索默认不再命中，可从库中追溯）。
func (h *Handler) Forget(c *gin.Context) {
	id, ok := memoryID(c)
	if !ok {
		return
	}
	if err := h.svc.Forget(c.Request.Context(), id); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{"id": id, "status": model.MemoryStatusDeleted})
}

// Supersede POST /memories/:id/supersede —— 标记 :id 被 successor_id 替代（失效不删除）。
func (h *Handler) Supersede(c *gin.Context) {
	id, ok := memoryID(c)
	if !ok {
		return
	}
	var req supersedeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "请求体非法: "+err.Error()))
		return
	}
	if err := h.svc.Supersede(c.Request.Context(), id, req.SuccessorID, h.creator(c)); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{"id": id, "superseded_by": req.SuccessorID})
}

// AddLink POST /memories/:id/links —— 建立关系边（幂等）。
func (h *Handler) AddLink(c *gin.Context) {
	id, ok := memoryID(c)
	if !ok {
		return
	}
	var req linkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "请求体非法: "+err.Error()))
		return
	}
	link, err := h.svc.AddLink(c.Request.Context(), id, req.ToID, req.Relation, req.Note, h.creator(c))
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, link)
}

// RemoveLink DELETE /memories/:id/links/:linkId —— 删除关系边。
func (h *Handler) RemoveLink(c *gin.Context) {
	id, ok := memoryID(c)
	if !ok {
		return
	}
	_ = id
	linkID, err := strconv.ParseInt(c.Param("linkId"), 10, 64)
	if err != nil || linkID <= 0 {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "非法 linkId"))
		return
	}
	if err := h.svc.RemoveLink(c.Request.Context(), linkID); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{"id": linkID})
}

// ---- 命名空间 / 图 / 统计 ----

// Namespaces GET /namespaces —— 命名空间清单（含记忆计数，供 agent 选归属）。
func (h *Handler) Namespaces(c *gin.Context) {
	items, err := h.svc.Namespaces(c.Request.Context())
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{"items": items})
}

// Graph GET /graph?namespace=&kind=&limit= —— 记忆关系图数据。
func (h *Handler) Graph(c *gin.Context) {
	q, err := h.queryOf(c)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "150"))
	g, err := h.svc.Graph(c.Request.Context(), q, limit)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, g)
}

// Stats GET /stats —— 记忆库总览。
func (h *Handler) Stats(c *gin.Context) {
	st, err := h.svc.Stats(c.Request.Context())
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, st)
}

// ---- 内部 ----

// creator 该挂载面的写入者：控制台固定 console；其余取 key 名（匿名为 anonymous）。
func (h *Handler) creator(c *gin.Context) string {
	if h.opts.Source == string(model.MemorySourceConsole) {
		return "console"
	}
	if v := c.GetString(middleware.CtxKeyName); v != "" {
		return v
	}
	return "anonymous"
}

// queryOf 解析检索/列表过滤参数（q/namespace/kind/tags/status/pinned/limit）。
func (h *Handler) queryOf(c *gin.Context) (service.MemoryQuery, error) {
	q := service.MemoryQuery{
		Q:         c.Query("q"),
		Namespace: c.Query("namespace"),
		Kind:      c.Query("kind"),
		Sort:      c.Query("sort"),
		Status:    c.Query("status"),
	}
	if tags := c.Query("tags"); tags != "" {
		for _, t := range strings.Split(tags, ",") {
			if t = strings.TrimSpace(t); t != "" {
				q.Tags = append(q.Tags, t)
			}
		}
	}
	q.PinnedOnly = c.Query("pinned") == "true" || c.Query("pinned") == "1"
	if limit := c.Query("limit"); limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil || n < 0 {
			return q, errs.New(errs.CodeInvalidParams, "limit 非法: "+limit)
		}
		q.Limit = n
	}
	return q, nil
}

// memoryID 解析 :id 路径参数。
func memoryID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "非法 id"))
		return 0, false
	}
	return id, true
}

// parseTimePtr 解析 RFC3339 时间指针（nil = 未提供）。
func parseTimePtr(s *string) (*time.Time, error) {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(*s))
	if err != nil {
		return nil, err
	}
	return &t, nil
}

package admin

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/resp"
	"xtokenhub/internal/service"
)

// MemoryHandler 记忆库管理接口（控制台专用）：命名空间管理、批量操作、LLM 分类配置与触发。
type MemoryHandler struct {
	svc        *service.MemoryService
	classifier *service.MemoryClassifier
}

// NewMemoryHandler 构造。
func NewMemoryHandler(svc *service.MemoryService, classifier *service.MemoryClassifier) *MemoryHandler {
	return &MemoryHandler{svc: svc, classifier: classifier}
}

// ---- 命名空间管理 ----

// ListNamespaces GET /api/v1/memory-namespaces —— 命名空间清单（含计数与描述）。
func (h *MemoryHandler) ListNamespaces(c *gin.Context) {
	items, err := h.svc.Namespaces(c.Request.Context())
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{"items": items})
}

// CreateNamespace POST /api/v1/memory-namespaces {name, description}
func (h *MemoryHandler) CreateNamespace(c *gin.Context) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "请求体非法: "+err.Error()))
		return
	}
	ns, err := h.svc.CreateNamespace(c.Request.Context(), req.Name, req.Description)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, ns)
}

// UpdateNamespace PATCH /api/v1/memory-namespaces/:id {description}
func (h *MemoryHandler) UpdateNamespace(c *gin.Context) {
	id, ok := namespaceID(c)
	if !ok {
		return
	}
	var req struct {
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "请求体非法: "+err.Error()))
		return
	}
	ns, err := h.svc.UpdateNamespace(c.Request.Context(), id, req.Description)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, ns)
}

// RenameNamespace POST /api/v1/memory-namespaces/rename {from, to}
func (h *MemoryHandler) RenameNamespace(c *gin.Context) {
	h.renameOrMerge(c, true)
}

// MergeNamespaces POST /api/v1/memory-namespaces/merge {from, to}
func (h *MemoryHandler) MergeNamespaces(c *gin.Context) {
	h.renameOrMerge(c, false)
}

func (h *MemoryHandler) renameOrMerge(c *gin.Context, rename bool) {
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "请求体非法: "+err.Error()))
		return
	}
	req.From, req.To = strings.TrimSpace(req.From), strings.TrimSpace(req.To)
	if req.From == "" || req.To == "" {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "from/to 不能为空"))
		return
	}
	var (
		moved int64
		err   error
	)
	if rename {
		moved, err = h.svc.RenameNamespace(c.Request.Context(), req.From, req.To)
	} else {
		moved, err = h.svc.MergeNamespaces(c.Request.Context(), req.From, req.To)
	}
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{"from": req.From, "to": req.To, "moved": moved})
}

// DeleteNamespace DELETE /api/v1/memory-namespaces/:id —— 删除命名空间，其记忆回落未分类。
func (h *MemoryHandler) DeleteNamespace(c *gin.Context) {
	id, ok := namespaceID(c)
	if !ok {
		return
	}
	moved, err := h.svc.DeleteNamespace(c.Request.Context(), id)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{"id": id, "moved": moved})
}

// ---- 批量操作 ----

// BatchMove POST /api/v1/memories/batch-move {ids, namespace}（namespace 空 = 移回未分类）。
func (h *MemoryHandler) BatchMove(c *gin.Context) {
	var req struct {
		IDs       []int64 `json:"ids"`
		Namespace string  `json:"namespace"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "请求体非法: "+err.Error()))
		return
	}
	moved, err := h.svc.BatchMoveNamespace(c.Request.Context(), req.IDs, req.Namespace)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{"moved": moved})
}

// ---- LLM 一键分类 ----

// ClassifyConfig GET /api/v1/memories/classify-config —— 分类配置与最近任务状态。
func (h *MemoryHandler) ClassifyConfig(c *gin.Context) {
	cfg, err := h.svc.ClassifyConfig(c.Request.Context())
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, cfg)
}

// UpdateClassifyConfig PUT /api/v1/memories/classify-config {channel_id, model}
func (h *MemoryHandler) UpdateClassifyConfig(c *gin.Context) {
	var req struct {
		ChannelID int64  `json:"channel_id"`
		Model     string `json:"model"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "请求体非法: "+err.Error()))
		return
	}
	if err := h.svc.UpdateClassifyConfig(c.Request.Context(), req.ChannelID, req.Model); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{"channel_id": req.ChannelID, "model": req.Model})
}

// TriggerClassify POST /api/v1/memories/classify —— 触发一轮 LLM 分类（异步，进度走 GET）。
func (h *MemoryHandler) TriggerClassify(c *gin.Context) {
	st, err := h.classifier.Start(c.Request.Context())
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, st)
}

// ClassifyStatus GET /api/v1/memories/classify —— 分类任务进度。
func (h *MemoryHandler) ClassifyStatus(c *gin.Context) {
	st, err := h.classifier.Status(c.Request.Context())
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, st)
}

// RebuildAutoLinks POST /api/v1/memories/auto-link —— 全库重算自动关系。
// 只回收/重建 created_by=system 的自动边，显式关系不受影响；同步执行。
func (h *MemoryHandler) RebuildAutoLinks(c *gin.Context) {
	res, err := h.svc.RebuildAutoLinks(c.Request.Context())
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, res)
}

// namespaceID 解析 :id 路径参数。
func namespaceID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		resp.Fail(c, errs.New(errs.CodeInvalidParams, "非法 id"))
		return 0, false
	}
	return id, true
}

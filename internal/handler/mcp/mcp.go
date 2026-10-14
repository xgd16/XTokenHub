// Package mcp 记忆库的 MCP 服务端（Streamable HTTP 传输，无状态）。
//
// 实现 MCP 规范的最小子集：initialize / notifications/initialized / ping /
// tools/list / tools/call；单请求 JSON 响应（不提供 SSE 服务端推送），GET/DELETE 返回 405。
// 记忆工具与 REST API 共用同一 service，写入来源标记为 mcp。
package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"xtokenhub/internal/middleware"
	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/service"
)

const (
	// serverName / serverVersion initialize 握手返回的服务标识。
	serverName    = "xtokenhub-memory"
	serverVersion = "1.0.0"
	// protocolVersionDefault 不认识客户端版本时回的默认协议版本。
	protocolVersionDefault = "2025-06-18"
	// maxBodyBytes MCP 请求体上限（工具入参都很小）。
	maxBodyBytes = 1 << 20
)

// supportedProtocolVersions 握手可回显的协议版本。
var supportedProtocolVersions = map[string]bool{
	"2025-03-26": true,
	"2025-06-18": true,
}

// Handler MCP 端点。
type Handler struct {
	svc         *service.MemoryService
	keys        middleware.KeyChecker
	keyRequired bool
}

// NewHandler 构造（keys 为空或 keyRequired=false 时匿名可用）。
func NewHandler(svc *service.MemoryService, keys middleware.KeyChecker, keyRequired bool) *Handler {
	return &Handler{svc: svc, keys: keys, keyRequired: keyRequired}
}

// Register 注册 /mcp 端点（GET/DELETE 明确返回 405）。
func (h *Handler) Register(r gin.IRouter) {
	auth := middleware.APIKeyAuth(h.keys, h.keyRequired)
	r.POST("/mcp", auth, h.handle)
	r.GET("/mcp", auth, methodNotAllowed)
	r.DELETE("/mcp", auth, methodNotAllowed)
}

func methodNotAllowed(c *gin.Context) {
	c.JSON(http.StatusMethodNotAllowed, gin.H{
		"jsonrpc": "2.0",
		"error":   gin.H{"code": -32600, "message": "仅支持 POST（Streamable HTTP 无服务端推送）"},
	})
}

// rpcRequest JSON-RPC 2.0 请求（通知无 id）。
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// rpcResponse JSON-RPC 2.0 响应。
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError JSON-RPC 错误对象。
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// handle POST /mcp：单请求 JSON-RPC 分发。
func (h *Handler) handle(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBodyBytes))
	if err != nil {
		writeRPCError(c, nil, -32700, "读取请求失败")
		return
	}
	// MCP（2025-06-18 起）不支持 JSON-RPC 批量请求。
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "[") {
		writeRPCError(c, nil, -32600, "不支持批量请求")
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeRPCError(c, nil, -32700, "JSON 解析失败: "+err.Error())
		return
	}
	if req.Method == "" {
		writeRPCError(c, req.ID, -32600, "缺少 method")
		return
	}

	// 通知（无 id）：无需响应体。
	if len(req.ID) == 0 {
		if req.Method == "notifications/initialized" || strings.HasPrefix(req.Method, "notifications/") {
			c.Status(http.StatusAccepted)
			return
		}
		writeRPCError(c, nil, -32600, "缺少 id")
		return
	}

	result, rpcErr := h.dispatch(c, req)
	if rpcErr != nil {
		writeRPCError(c, req.ID, rpcErr.Code, rpcErr.Message)
		return
	}
	c.JSON(http.StatusOK, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
}

// dispatch 方法分发。
func (h *Handler) dispatch(c *gin.Context, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return h.initialize(req.Params), nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolDefinitions()}, nil
	case "tools/call":
		return h.toolCall(c, req.Params)
	default:
		return nil, &rpcError{Code: -32601, Message: "不支持的方法: " + req.Method}
	}
}

// initialize 握手：回显可支持的协议版本，声明 tools 能力。
func (h *Handler) initialize(params json.RawMessage) any {
	version := protocolVersionDefault
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(params, &p); err == nil && supportedProtocolVersions[p.ProtocolVersion] {
		version = p.ProtocolVersion
	}
	return map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		"serverInfo":      map[string]any{"name": serverName, "version": serverVersion},
		"instructions":    "XTokenHub 记忆库：本环境的共享长期记忆（跨会话）。需要记住或回忆时一律用这套 memory_* 工具，不必等用户提醒：① 开始任务前先 memory_search 检索已有结论/约定/踩坑，避免重复探索；② 任务中形成结论、决策、约定、踩坑经验时主动 memory_add 写入；③ 旧结论被推翻时写新记忆并标 supersedes（memory_add 的 supersedes 参数或 memory_link），不要直接删除。写入规范：正文用 Markdown（标题/列表/代码块），namespace 填主题（不填=未分类），kind 标注类型。系统会在写入/更新后自动维护相似记忆的 related 关联（auto_links），无需手动补。",
	}
}

// toolCall 工具调用：失败也返回 result（isError），仅协议层错误走 JSON-RPC error。
func (h *Handler) toolCall(c *gin.Context, params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: -32602, Message: "params 非法: " + err.Error()}
	}
	out, err := h.runTool(c, p.Name, p.Arguments)
	if err != nil {
		return map[string]any{
			"content": []map[string]any{{"type": "text", "text": err.Error()}},
			"isError": true,
		}, nil
	}
	text, err := json.Marshal(out)
	if err != nil {
		return map[string]any{
			"content": []map[string]any{{"type": "text", "text": "序列化结果失败: " + err.Error()}},
			"isError": true,
		}, nil
	}
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(text)}},
		"isError": false,
	}, nil
}

// runTool 具体工具执行。
func (h *Handler) runTool(c *gin.Context, name string, args json.RawMessage) (any, error) {
	ctx := c.Request.Context()
	switch name {
	case "memory_add":
		var p memoryAddParams
		if err := unmarshalArgs(args, &p); err != nil {
			return nil, err
		}
		return h.svc.Add(ctx, service.MemoryAddInput{
			Title: p.Title, Content: p.Content, Namespace: p.Namespace, Kind: p.Kind,
			Tags: p.Tags, Importance: p.Importance, Pinned: p.Pinned, ExpiresAt: parseTime(p.ExpiresAt),
			Source: string(model.MemorySourceMCP), CreatedBy: h.creator(c),
			SourceRef: p.SourceRef, Supersedes: p.Supersedes, Links: p.Links,
		})
	case "memory_search":
		var p memorySearchParams
		if err := unmarshalArgs(args, &p); err != nil {
			return nil, err
		}
		return h.svc.Search(ctx, service.MemoryQuery{
			Q: p.Query, Namespace: p.Namespace, Kind: p.Kind, Tags: p.Tags, Limit: p.Limit,
		})
	case "memory_get":
		var p memoryIDParams
		if err := unmarshalArgs(args, &p); err != nil {
			return nil, err
		}
		m, err := h.svc.Get(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		links, err := h.svc.Links(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"memory": m, "links": links}, nil
	case "memory_update":
		var p memoryUpdateParams
		if err := unmarshalArgs(args, &p); err != nil {
			return nil, err
		}
		return h.svc.Update(ctx, p.ID, service.MemoryUpdateInput{
			Title: p.Title, Content: p.Content, Namespace: p.Namespace, Kind: p.Kind,
			Tags: p.Tags, Importance: p.Importance, Pinned: p.Pinned, Status: p.Status,
		})
	case "memory_forget":
		var p memoryIDParams
		if err := unmarshalArgs(args, &p); err != nil {
			return nil, err
		}
		if err := h.svc.Forget(ctx, p.ID); err != nil {
			return nil, err
		}
		return map[string]any{"id": p.ID, "status": model.MemoryStatusDeleted}, nil
	case "memory_link":
		var p memoryLinkParams
		if err := unmarshalArgs(args, &p); err != nil {
			return nil, err
		}
		return h.svc.AddLink(ctx, p.FromID, p.ToID, p.Relation, p.Note, h.creator(c))
	case "memory_namespaces":
		items, err := h.svc.Namespaces(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items}, nil
	default:
		return nil, errs.New(errs.CodeInvalidParams, "未知工具: "+name)
	}
}

// ---- 工具入参 ----

type memoryAddParams struct {
	Title      string                    `json:"title"`
	Content    string                    `json:"content"`
	Namespace  string                    `json:"namespace"`
	Kind       string                    `json:"kind"`
	Tags       []string                  `json:"tags"`
	Importance int                       `json:"importance"`
	Pinned     bool                      `json:"pinned"`
	ExpiresAt  *string                   `json:"expires_at"`
	SourceRef  string                    `json:"source_ref"`
	Supersedes []int64                   `json:"supersedes"`
	Links      []service.MemoryLinkInput `json:"links"`
}

type memorySearchParams struct {
	Query     string   `json:"query"`
	Namespace string   `json:"namespace"`
	Kind      string   `json:"kind"`
	Tags      []string `json:"tags"`
	Limit     int      `json:"limit"`
}

type memoryIDParams struct {
	ID int64 `json:"id"`
}

type memoryUpdateParams struct {
	ID         int64     `json:"id"`
	Title      *string   `json:"title"`
	Content    *string   `json:"content"`
	Namespace  *string   `json:"namespace"`
	Kind       *string   `json:"kind"`
	Tags       *[]string `json:"tags"`
	Importance *int      `json:"importance"`
	Pinned     *bool     `json:"pinned"`
	Status     *string   `json:"status"`
}

type memoryLinkParams struct {
	FromID   int64  `json:"from_id"`
	ToID     int64  `json:"to_id"`
	Relation string `json:"relation"`
	Note     string `json:"note"`
}

// ---- 工具定义（tools/list） ----

func toolDefinitions() []map[string]any {
	str := map[string]any{"type": "string"}
	num := map[string]any{"type": "integer"}
	boolT := map[string]any{"type": "boolean"}
	strArr := map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	intArr := map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}
	kindEnum := map[string]any{
		"type": "string",
		"enum": []string{"note", "fact", "decision", "convention", "preference", "task", "lesson"},
	}

	return []map[string]any{
		{
			"name":        "memory_add",
			"description": "写入一条长期记忆（同命名空间内同内容自动去重）。开发/执行任务中的结论、决策、约定、踩坑经验都值得写入。namespace 指定归属命名空间（主题），不指定则进「未分类」。正文请用 Markdown 组织（标题、列表、代码块），控制台会渲染展示。写入后系统会自动按内容相似度关联同命名空间内的记忆（见返回 auto_links），无需手动补 related 边。",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"content"},
				"properties": map[string]any{
					"content":    map[string]any{"type": "string", "description": "记忆正文，Markdown 格式（标题/列表/代码块/表格），控制台按 Markdown 渲染"},
					"title":      map[string]any{"type": "string", "description": "标题，缺省取正文首行"},
					"namespace":  map[string]any{"type": "string", "description": "归属命名空间（主题），不存在会自动创建"},
					"kind":       kindEnum,
					"tags":       strArr,
					"importance": map[string]any{"type": "integer", "minimum": 1, "maximum": 5},
					"pinned":     boolT,
					"expires_at": map[string]any{"type": "string", "description": "过期时间 RFC3339（可空）"},
					"source_ref": map[string]any{"type": "string", "description": "来源引用（仓库/文件/任务号）"},
					"supersedes": intArr,
					"links": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":       "object",
							"required":   []string{"to_id"},
							"properties": map[string]any{"to_id": num, "relation": str, "note": str},
						},
					},
				},
			},
		},
		{
			"name":        "memory_search",
			"description": "先检索再动手：任务开始或需要回忆过往时，先用它查已有结论/约定/踩坑。按关键词检索记忆（中文/英文子串可命中，按相关性、时间衰减、重要度、访问热度综合排序）。query 为空则按最近更新浏览。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":     map[string]any{"type": "string"},
					"namespace": map[string]any{"type": "string", "description": "限定命名空间；none=未分类；空=全部"},
					"kind":      kindEnum,
					"tags":      strArr,
					"limit":     map[string]any{"type": "integer", "minimum": 1, "maximum": 50},
				},
			},
		},
		{
			"name":        "memory_get",
			"description": "按 id 读取一条记忆（含关系边）。通常在 memory_search 命中后读全文。",
			"inputSchema": map[string]any{
				"type": "object", "required": []string{"id"},
				"properties": map[string]any{"id": num},
			},
		},
		{
			"name":        "memory_update",
			"description": "更新一条记忆（只更新给出的字段）。适合纠正/补充已有结论；推翻旧结论建议另写新记忆并标 supersedes。正文/标题/归属变化时，系统会自动重算该记忆的自动关联（只动系统自动边）。",
			"inputSchema": map[string]any{
				"type": "object", "required": []string{"id"},
				"properties": map[string]any{
					"id": num, "title": str,
					"content":   map[string]any{"type": "string", "description": "记忆正文，Markdown 格式（标题/列表/代码块/表格）"},
					"namespace": str,
					"kind":      kindEnum, "tags": strArr, "importance": num,
					"pinned": boolT,
					"status": map[string]any{"type": "string", "enum": []string{"active", "archived"}},
				},
			},
		},
		{
			"name":        "memory_forget",
			"description": "删除一条记忆（软删除，检索不再命中）。仅在内容确实无价值时使用；结论过时应优先 memory_update 或写新记忆标 supersedes 保留历史。",
			"inputSchema": map[string]any{
				"type": "object", "required": []string{"id"},
				"properties": map[string]any{"id": num},
			},
		},
		{
			"name":        "memory_link",
			"description": "为两条记忆建立关系边（related/supersedes/conflicts/derived_from），用于记忆关系图。系统自动维护 related；语义确定的关系（冲突/派生/替代）用它显式补充。已存在的同关系边幂等。",
			"inputSchema": map[string]any{
				"type": "object", "required": []string{"from_id", "to_id"},
				"properties": map[string]any{
					"from_id":  num,
					"to_id":    num,
					"relation": map[string]any{"type": "string", "enum": []string{"related", "supersedes", "conflicts", "derived_from"}},
					"note":     str,
				},
			},
		},
		{
			"name":        "memory_namespaces",
			"description": "列出全部命名空间（主题）及记忆数量，便于选择写入归属或按主题检索。",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}

// ---- 内部 ----

// creator 写入者标识：取 key 名，匿名为 anonymous。
func (h *Handler) creator(c *gin.Context) string {
	if v := c.GetString(middleware.CtxKeyName); v != "" {
		return v
	}
	return "anonymous"
}

// unmarshalArgs 解析工具入参。
func unmarshalArgs(args json.RawMessage, dst any) error {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	if err := json.Unmarshal(args, dst); err != nil {
		return errs.New(errs.CodeInvalidParams, "工具入参非法: "+err.Error())
	}
	return nil
}

// parseTime 解析 RFC3339 时间指针（解析失败按未提供处理，由 service 校验其余语义）。
func parseTime(s *string) *time.Time {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(*s))
	if err != nil {
		return nil
	}
	return &t
}

// writeRPCError 返回 JSON-RPC 错误响应。
func writeRPCError(c *gin.Context, id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	c.JSON(http.StatusOK, rpcResponse{
		JSONRPC: "2.0", ID: id,
		Error: &rpcError{Code: code, Message: message},
	})
}

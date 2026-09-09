package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"xtokenhub/internal/handler/admin"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/repository"
	"xtokenhub/internal/service"
	"xtokenhub/internal/tests/testutil"
)

// newSessionHeaderEnv 构造内存库 + service + admin handler（含默认种子）。
func newSessionHeaderEnv(t *testing.T) (*gin.Engine, *service.SessionHeaderConfigService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := testutil.NewMemoryDB(t)
	repo := repository.NewSessionHeaderConfigRepo(db)
	svc := service.NewSessionHeaderConfigService(repo)
	if err := svc.EnsureDefaults(context.Background()); err != nil {
		t.Fatalf("ensure defaults: %v", err)
	}

	r := gin.New()
	h := admin.NewSessionHeaderConfigHandler(svc)
	r.GET("/api/v1/settings/session-headers", h.List)
	r.POST("/api/v1/settings/session-headers", h.Create)
	r.DELETE("/api/v1/settings/session-headers/:id", h.Delete)
	r.PUT("/api/v1/settings/session-headers/:key/toggle", h.ToggleEnabled)
	return r, svc
}

// doJSON 执行请求并解析信封响应。
func doJSON(t *testing.T, r *gin.Engine, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var env map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode resp %q: %v", w.Body.String(), err)
	}
	return w.Code, env
}

// listNames 拉取配置并返回 header_name 列表。
func listNames(t *testing.T, r *gin.Engine) []string {
	t.Helper()
	_, env := doJSON(t, r, http.MethodGet, "/api/v1/settings/session-headers", nil)
	if env["code"] != float64(errs.CodeOK) {
		t.Fatalf("list failed: %v", env)
	}
	raws := env["data"].(map[string]any)["configs"].([]any)
	names := make([]string, 0, len(raws))
	for _, raw := range raws {
		names = append(names, raw.(map[string]any)["header_name"].(string))
	}
	return names
}

// TestSessionHeaderDefaults 空表种子化后应含默认 X-Session-Id（ZCode 会话标识）。
func TestSessionHeaderDefaults(t *testing.T) {
	r, _ := newSessionHeaderEnv(t)
	names := listNames(t, r)
	if len(names) != 1 || names[0] != "X-Session-Id" {
		t.Fatalf("default configs = %v, want [X-Session-Id]", names)
	}
}

// TestSessionHeaderCreateAndPersist 添加配置后重新读取应仍在（持久化），重复添加应被拒。
func TestSessionHeaderCreateAndPersist(t *testing.T) {
	r, _ := newSessionHeaderEnv(t)

	code, env := doJSON(t, r, http.MethodPost, "/api/v1/settings/session-headers", map[string]any{
		"header_name": "X-Conversation-Id",
		"description": "自定义会话标识",
	})
	if code != http.StatusOK || env["code"] != float64(errs.CodeOK) {
		t.Fatalf("create failed: http=%d env=%v", code, env)
	}

	// 大小写不敏感判重：重复添加应报业务错误
	_, env = doJSON(t, r, http.MethodPost, "/api/v1/settings/session-headers", map[string]any{
		"header_name": "x-conversation-id",
	})
	if env["code"] == float64(errs.CodeOK) {
		t.Fatalf("duplicate create should fail, got %v", env)
	}

	names := listNames(t, r)
	if len(names) != 2 || names[0] != "X-Session-Id" || names[1] != "X-Conversation-Id" {
		t.Fatalf("configs after create = %v", names)
	}
}

// TestSessionHeaderToggleAndDelete 切换启用状态影响 EnabledHeaders；删除后消失。
func TestSessionHeaderToggleAndDelete(t *testing.T) {
	r, svc := newSessionHeaderEnv(t)
	if _, err := svc.Create(context.Background(), &service.CreateSessionHeaderInput{HeaderName: "X-Custom-Session"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// 两项都启用时，EnabledHeaders 应含两者（canonical 格式）
	headers := svc.EnabledHeaders(context.Background())
	if len(headers) != 2 || headers[0] != "X-Session-Id" || headers[1] != "X-Custom-Session" {
		t.Fatalf("headers = %v, want [X-Session-Id X-Custom-Session]", headers)
	}

	// 禁用全部后回退默认名单，网关行为不中断
	if _, err := svc.ToggleEnabled(context.Background(), "x-session-id"); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	if _, err := svc.ToggleEnabled(context.Background(), "x-custom-session"); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	headers = svc.EnabledHeaders(context.Background())
	if len(headers) != 1 || headers[0] != "X-Session-Id" {
		t.Fatalf("fallback headers = %v, want [X-Session-Id]", headers)
	}

	// 恢复默认启用，删除自定义配置后其应从名单消失
	if _, err := svc.ToggleEnabled(context.Background(), "x-session-id"); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	code, env := doJSON(t, r, http.MethodDelete, "/api/v1/settings/session-headers/2", nil)
	if code != http.StatusOK || env["code"] != float64(errs.CodeOK) {
		t.Fatalf("delete failed: http=%d env=%v", code, env)
	}
	headers = svc.EnabledHeaders(context.Background())
	if len(headers) != 1 || headers[0] != "X-Session-Id" {
		t.Fatalf("headers after delete = %v, want [X-Session-Id]", headers)
	}
}

// TestGatewaySessionIDExtraction 网关按配置名单提取会话标识：自定义 header 非空时优先采用。
func TestGatewaySessionIDExtraction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutil.NewMemoryDB(t)
	repo := repository.NewSessionHeaderConfigRepo(db)
	svc := service.NewSessionHeaderConfigService(repo)
	if err := svc.EnsureDefaults(context.Background()); err != nil {
		t.Fatalf("ensure defaults: %v", err)
	}
	if _, err := svc.Create(context.Background(), &service.CreateSessionHeaderInput{HeaderName: "X-Conversation-Id"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// 借未注入鉴权的最小 Handler 结构验证 sessionID 提取次序
	gw := &gwSessionProbe{sessions: svc}
	r := gin.New()
	r.POST("/probe", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"sid": gw.extract(c)}) })

	// 仅自定义 header：应取自定义值
	req := httptest.NewRequest(http.MethodPost, "/probe", nil)
	req.Header.Set("X-Conversation-Id", "conv-1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if got := respSID(t, w); got != "conv-1" {
		t.Fatalf("sid = %q, want conv-1", got)
	}

	// 两者都有：按名单次序取默认 X-Session-Id
	req = httptest.NewRequest(http.MethodPost, "/probe", nil)
	req.Header.Set("X-Session-Id", "sess-1")
	req.Header.Set("X-Conversation-Id", "conv-1")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if got := respSID(t, w); got != "sess-1" {
		t.Fatalf("sid = %q, want sess-1", got)
	}

	// 都没有：应为空
	req = httptest.NewRequest(http.MethodPost, "/probe", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if got := respSID(t, w); got != "" {
		t.Fatalf("sid = %q, want empty", got)
	}
}

// respSID 从 /probe 响应中取出 sid。
func respSID(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		SID string `json:"sid"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body.SID
}

// gwSessionProbe 复用 gateway.Handler 的提取逻辑：仅暴露 sessionID 所需依赖。
// 为避免构造完整 Executor，这里直接内嵌 service 并按同一语义提取。
type gwSessionProbe struct {
	sessions *service.SessionHeaderConfigService
}

// extract 与 handler.gateway.Handler.sessionID 相同的语义：名单次序取第一个非空值。
func (p *gwSessionProbe) extract(c *gin.Context) string {
	for _, name := range p.sessions.EnabledHeaders(c.Request.Context()) {
		if v := trimSpace(c.GetHeader(name)); v != "" {
			return v
		}
	}
	return ""
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

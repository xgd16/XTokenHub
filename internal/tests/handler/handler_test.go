package handler_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"

	"xtokenhub/internal/config"
	"xtokenhub/internal/eventbus"
	"xtokenhub/internal/gateway"
	"xtokenhub/internal/handler/admin"
	gwhandler "xtokenhub/internal/handler/gateway"
	"xtokenhub/internal/provider"
	"xtokenhub/internal/repository"
	"xtokenhub/internal/router"
	"xtokenhub/internal/service"
	"xtokenhub/internal/tests/testutil"
	"xtokenhub/internal/ws"
)

// newTestEnv 构建完整路由 + 内存依赖 + mock 上游（网关不强制鉴权，便于既有用例直连）。
func newTestEnv(t *testing.T) (*gin.Engine, *httptest.Server) {
	t.Helper()
	engine, srv, _ := newTestEnvOpts(t, false)
	return engine, srv
}

// newTestEnvOpts 同 newTestEnv，可控制网关密钥强制开关并返回密钥服务。
func newTestEnvOpts(t *testing.T, requireKey bool) (*gin.Engine, *httptest.Server, *service.KeyService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := testutil.NewMemoryDB(t)
	bus := eventbus.New()
	t.Cleanup(bus.Wait)

	chRepo := repository.NewChannelRepository(db)
	keyRepo := repository.NewAPIKeyRepository(db)
	logRepo := repository.NewRequestLogRepository(db)
	prober := provider.NewProber(2 * time.Second)
	upstreamTimeout := 5 * time.Second

	channelSvc := service.NewChannelService(chRepo, prober, bus)
	keySvc := service.NewKeyService(keyRepo)
	logSvc := service.NewLogService(logRepo)
	statsSvc := service.NewStatsService(logRepo)
	exec := gateway.NewExecutor(chRepo, logRepo, bus, upstreamTimeout)
	hub := ws.NewHub(30*time.Second, 10*time.Second)

	engine := gin.New()
	engine.Use(gin.Recovery())
	// 嵌入一个假的静态 FS（index.html + assets/app.js）
	var memFS fstest.MapFS = fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html>SPA</html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log(1)")},
	}
	router.Register(engine, &router.Deps{
		Cfg:      &config.Config{},
		Channels: admin.NewChannelHandler(channelSvc),
		Keys:     admin.NewKeyHandler(keySvc),
		Logs:     admin.NewLogHandler(logSvc),
		Stats:    admin.NewStatsHandler(statsSvc),
		WS:       admin.NewWSHandler(hub),
		Gateway:  gwhandler.NewHandler(exec, keySvc, requireKey, 1<<20),
		WebFS:    memFS,
	})

	srv := httptest.NewServer(engine)
	t.Cleanup(func() {
		hub.Close()
		srv.Close()
	})
	return engine, srv, keySvc
}

func doJSON(t *testing.T, srv *httptest.Server, method, path string, body any) (int, map[string]any, []byte) {
	t.Helper()
	var reader *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = strings.NewReader(string(b))
	} else {
		reader = strings.NewReader("")
	}
	req, _ := http.NewRequest(method, srv.URL+path, reader)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io_readAll(resp)
	var env map[string]any
	_ = json.Unmarshal(raw, &env)
	return resp.StatusCode, env, raw
}

func io_readAll(resp *http.Response) ([]byte, error) {
	return io.ReadAll(resp.Body)
}

func TestAdminChannelCRUD(t *testing.T) {
	_, srv := newTestEnv(t)

	// 创建
	code, env, _ := doJSON(t, srv, "POST", "/api/v1/channels", map[string]any{
		"name": "openai-main", "provider": "openai_compatible",
		"base_url": "https://api.openai.com", "api_key": "sk-1",
		"models": []string{"gpt-4o"}, "priority": 10,
	})
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("create: %d %v", code, env)
	}
	ch := env["data"].(map[string]any)
	id := int64(ch["id"].(float64))

	// 列表
	code, env, _ = doJSON(t, srv, "GET", "/api/v1/channels", nil)
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("list: %d", code)
	}
	page := env["data"].(map[string]any)
	if page["total"] != float64(1) {
		t.Errorf("total = %v", page["total"])
	}

	// 更新
	code, env, _ = doJSON(t, srv, "PUT", "/api/v1/channels/"+itoa(id), map[string]any{
		"name": "openai-main", "provider": "openai_compatible",
		"base_url": "https://api2.openai.com", "api_key": "sk-2", "priority": 5,
	})
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("update: %d %v", code, env)
	}

	// 详情
	_, env, _ = doJSON(t, srv, "GET", "/api/v1/channels/"+itoa(id), nil)
	if env["data"].(map[string]any)["base_url"] != "https://api2.openai.com" {
		t.Errorf("update 未生效: %v", env["data"])
	}

	// 删除
	code, _, _ = doJSON(t, srv, "DELETE", "/api/v1/channels/"+itoa(id), nil)
	if code != 200 {
		t.Fatalf("delete: %d", code)
	}
	code, env, _ = doJSON(t, srv, "GET", "/api/v1/channels/"+itoa(id), nil)
	if code != 404 {
		t.Errorf("删除后 get 应 404: %d", code)
	}

	// 参数错误
	code, _, _ = doJSON(t, srv, "POST", "/api/v1/channels", map[string]any{"name": ""})
	if code != 400 {
		t.Errorf("非法参数应 400: %d", code)
	}
}

func itoa(n int64) string {
	return json.Number(int64String(n)).String()
}

func int64String(n int64) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	if neg {
		return "-" + digits
	}
	return digits
}

func TestAdminStatsEndpoints(t *testing.T) {
	_, srv := newTestEnv(t)
	for _, path := range []string{
		"/api/v1/stats/summary?hours=24",
		"/api/v1/stats/trend?days=7",
		"/api/v1/stats/by-model?hours=24",
		"/api/v1/stats/by-channel?hours=24",
		"/api/v1/logs?page=1&per_page=10",
	} {
		code, env, _ := doJSON(t, srv, "GET", path, nil)
		if code != 200 || env["code"] != float64(0) {
			t.Errorf("GET %s: %d %v", path, code, env)
		}
	}
}

func TestGatewayEndpointsValidation(t *testing.T) {
	_, srv := newTestEnv(t)

	// 非法 JSON -> 400
	code, _, raw := doJSON(t, srv, "POST", "/v1/chat/completions", map[string]any{"bad": 1})
	if code != 400 {
		t.Errorf("非法请求应 400: %d body=%s", code, raw)
	}
	// anthropic 协议缺 max_tokens -> 400
	code, _, _ = doJSON(t, srv, "POST", "/v1/messages", map[string]any{
		"model": "claude", "messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	if code != 400 {
		t.Errorf("缺 max_tokens 应 400: %d", code)
	}
	// responses 协议缺 input -> 400
	code, _, _ = doJSON(t, srv, "POST", "/v1/responses", map[string]any{"model": "m"})
	if code != 400 {
		t.Errorf("缺 input 应 400: %d", code)
	}
	// 无渠道 -> 503 + 错误日志落库（通过 logs 接口验证）
	code, _, raw = doJSON(t, srv, "POST", "/v1/chat/completions", map[string]any{
		"model": "gpt-4o", "messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	if code != 503 {
		t.Errorf("无渠道应 503: %d %s", code, raw)
	}
	_, env, _ := doJSON(t, srv, "GET", "/api/v1/logs", nil)
	items := env["data"].(map[string]any)["items"].([]any)
	if len(items) != 1 {
		t.Errorf("无渠道请求应记录日志: %d", len(items))
	}
}

// mockNativeUpstream 带原生 chat 端点的上游。
func mockNativeUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","choices":[{"message":{"role":"assistant","content":"pong"}}],
			"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestGatewayEndToEndPassthrough(t *testing.T) {
	_, srv := newTestEnv(t)
	up := mockNativeUpstream(t)

	// 建渠道并探测
	code, env, _ := doJSON(t, srv, "POST", "/api/v1/channels", map[string]any{
		"name": "mock", "provider": "openai_compatible", "base_url": up.URL, "api_key": "k",
	})
	if code != 200 {
		t.Fatalf("create: %d", code)
	}
	id := int64(env["data"].(map[string]any)["id"].(float64))
	code, env, _ = doJSON(t, srv, "POST", "/api/v1/channels/"+itoa(id)+"/probe", nil)
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("probe: %d %v", code, env)
	}

	// 网关请求（原生协议 -> 透传）
	req, _ := http.NewRequest("POST", srv.URL+"/v1/chat/completions",
		strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "claude-cli/2.0.1 (external, cli)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw := make([]byte, 4096)
	n, _ := resp.Body.Read(raw)
	if !strings.Contains(string(raw[:n]), `"content":"pong"`) {
		t.Errorf("透传响应: %s", raw[:n])
	}

	// 日志记录了透传模式、usage 与调用方 User-Agent
	_, env, _ = doJSON(t, srv, "GET", "/api/v1/logs", nil)
	item := env["data"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["forward_mode"] != "native_passthrough" || item["prompt_tokens"] != float64(3) {
		t.Errorf("日志错误: %v", item)
	}
	if item["user_agent"] != "claude-cli/2.0.1 (external, cli)" {
		t.Errorf("user_agent 未记录: %v", item["user_agent"])
	}
}

func TestStaticSPA(t *testing.T) {
	_, srv := newTestEnv(t)

	// 静态资源直出
	resp, err := http.Get(srv.URL + "/assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b := make([]byte, 64)
	n, _ := resp.Body.Read(b)
	if !strings.Contains(string(b[:n]), "console.log") {
		t.Errorf("静态资源: %s", b[:n])
	}

	// SPA fallback
	resp2, err := http.Get(srv.URL + "/some/spa/route")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	b2 := make([]byte, 64)
	n2, _ := resp2.Body.Read(b2)
	if !strings.Contains(string(b2[:n2]), "SPA") {
		t.Errorf("SPA fallback: %s", b2[:n2])
	}

	// /api 未知路径 -> 404 JSON（不 fallback 到 HTML）
	resp3, err := http.Get(srv.URL + "/api/v1/unknown")
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != 404 {
		t.Errorf("未知 api 应 404: %d", resp3.StatusCode)
	}
}

func TestHealthz(t *testing.T) {
	_, srv := newTestEnv(t)
	code, env, _ := doJSON(t, srv, "GET", "/healthz", nil)
	if code != 200 || env["status"] != "ok" {
		t.Errorf("healthz: %d %v", code, env)
	}
}

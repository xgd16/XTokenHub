package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

// newBalanceEnv 渠道余额端点测试环境：返回渠道服务（供注入余额客户端/厂家识别）。
func newBalanceEnv(t *testing.T) (*service.ChannelService, *httptest.Server, *eventbus.Bus) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := testutil.NewMemoryDB(t)
	bus := eventbus.New()
	t.Cleanup(bus.Wait)

	chRepo := repository.NewChannelRepository(db)
	keyRepo := repository.NewAPIKeyRepository(db)
	logRepo := repository.NewRequestLogRepository(db)
	prober := provider.NewProber(2 * time.Second)

	channelSvc := service.NewChannelService(chRepo, prober, bus)
	channelSvc.SetBalanceClient(provider.NewBalanceClient(2 * time.Second))
	keySvc := service.NewKeyService(keyRepo)
	logSvc := service.NewLogService(logRepo)
	statsSvc := service.NewStatsService(logRepo)
	exec := gateway.NewExecutor(chRepo, logRepo, bus, 5*time.Second)
	hub := ws.NewHub(30*time.Second, 10*time.Second)

	engine := gin.New()
	engine.Use(gin.Recovery())
	var memFS fstest.MapFS = fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>SPA</html>")}}
	router.Register(engine, &router.Deps{
		Cfg:      &config.Config{},
		Channels: admin.NewChannelHandler(channelSvc),
		Keys:     admin.NewKeyHandler(keySvc),
		Logs:     admin.NewLogHandler(logSvc),
		Stats:    admin.NewStatsHandler(statsSvc),
		WS:       admin.NewWSHandler(hub),
		Gateway:  gwhandler.NewHandler(exec, keySvc, false, 1<<20),
		WebFS:    memFS,
	})

	srv := httptest.NewServer(engine)
	t.Cleanup(func() {
		hub.Close()
		srv.Close()
	})
	return channelSvc, srv, bus
}

// balanceEntryItems 解析 /channels/balances 响应为按 channel_id 索引的 map。
func balanceEntryItems(t *testing.T, env map[string]any) map[int64]map[string]any {
	t.Helper()
	data, _ := json.Marshal(env["data"])
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("解析余额响应: %v (%s)", err, data)
	}
	out := map[int64]map[string]any{}
	for _, it := range body.Items {
		out[int64(it["channel_id"].(float64))] = it
	}
	return out
}

func TestChannelBalancesEndpoint(t *testing.T) {
	channelSvc, srv, bus := newBalanceEnv(t)

	// 假 DeepSeek 上游：余额端点 + 命中计数
	var hits atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/user/balance" {
			t.Errorf("余额端点路径 = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"110.00","granted_balance":"10.00","topped_up_balance":"100.00"}]}`))
	}))
	defer up.Close()
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead.Close() // 已关闭 -> 连接失败

	// 假上游 host 无 deepseek 字样，注入按前缀识别
	channelSvc.SetBalanceKindResolver(func(baseURL string) provider.BalanceProviderKind {
		switch {
		case strings.HasPrefix(baseURL, up.URL), strings.HasPrefix(baseURL, dead.URL):
			return provider.BalanceProviderDeepSeek
		default:
			return provider.InferBalanceProvider(baseURL)
		}
	})

	// 订阅余额事件（拉取成功应发布）
	var events atomic.Int64
	unsub := bus.Subscribe(eventbus.EventChannelBalance, func(eventbus.Event) { events.Add(1) })
	defer unsub()

	// 三个渠道：deepseek-ok / deepseek-dead / openai
	code, env, _ := doJSON(t, srv, "POST", "/api/v1/channels", map[string]any{
		"name": "ds", "provider": "openai_compatible", "base_url": up.URL + "/v1", "api_key": "sk-1",
	})
	if code != 200 {
		t.Fatalf("create ds: %d %v", code, env)
	}
	_, env, _ = doJSON(t, srv, "POST", "/api/v1/channels", map[string]any{
		"name": "ds-dead", "provider": "openai_compatible", "base_url": dead.URL, "api_key": "sk-2",
	})
	_, env, _ = doJSON(t, srv, "POST", "/api/v1/channels", map[string]any{
		"name": "openai", "provider": "openai_compatible", "base_url": "https://api.openai.com", "api_key": "sk-3",
	})

	// 首次批量查询
	code, env, raw := doJSON(t, srv, "GET", "/api/v1/channels/balances", nil)
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("balances: %d %s", code, raw)
	}
	items := balanceEntryItems(t, env)
	if len(items) != 3 {
		t.Fatalf("应返回 3 个渠道条目: %s", raw)
	}
	ds := items[1]
	if ds["supported"] != true || ds["ok"] != true {
		t.Errorf("deepseek 条目应 supported+ok: %v", ds)
	}
	if b, _ := ds["balance"].(map[string]any); b == nil || b["total"] != float64(110) || b["currency"] != "CNY" {
		t.Errorf("余额解析错误: %v", ds["balance"])
	}
	deadEntry := items[2]
	if deadEntry["ok"] != false || deadEntry["error"] == "" {
		t.Errorf("死上游条目应 ok=false 且带 error: %v", deadEntry)
	}
	oai := items[3]
	if oai["supported"] != false {
		t.Errorf("openai 条目应 supported=false: %v", oai)
	}
	bus.Wait()
	if events.Load() != 1 { // 只有成功的那次发布
		t.Errorf("余额事件应发布 1 次, got %d", events.Load())
	}

	// TTL 缓存：第二次不再打上游
	_, _, _ = doJSON(t, srv, "GET", "/api/v1/channels/balances", nil)
	if n := hits.Load(); n != 1 {
		t.Errorf("TTL 缓存内上游命中应仍为 1, got %d", n)
	}

	// 渠道更新（换 key）-> 缓存失效 -> 再查应重新打上游
	_, _, _ = doJSON(t, srv, "PUT", "/api/v1/channels/1", map[string]any{
		"name": "ds", "provider": "openai_compatible", "base_url": up.URL + "/v1", "api_key": "sk-1b",
	})
	_, env, _ = doJSON(t, srv, "GET", "/api/v1/channels/balances", nil)
	items = balanceEntryItems(t, env)
	if b, _ := items[1]["balance"].(map[string]any); b == nil {
		t.Errorf("缓存失效后应重新拉取: %v", items[1])
	}
	if n := hits.Load(); n != 2 {
		t.Errorf("渠道更新后应重新命中上游, got %d", n)
	}
}

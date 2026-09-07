package gateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"xtokenhub/internal/eventbus"
	"xtokenhub/internal/gateway"
	"xtokenhub/internal/model"
)

func init() { gin.SetMode(gin.TestMode) }

// openAIUpstream 启动一个 OpenAI 兼容 mock 上游（支持流式/非流式）。
func openAIUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		if !req.Stream {
			_, _ = w.Write([]byte(`{"id":"cc-1","object":"chat.completion","model":"gpt-4o",
				"choices":[{"index":0,"message":{"role":"assistant","content":"你好，世界"},"finish_reason":"stop"}],
				"usage":{"prompt_tokens":12,"completion_tokens":6,"total_tokens":18,
				"prompt_tokens_details":{"cached_tokens":4}}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"cc-1\",\"choices\":[{\"delta\":{\"content\":\"he\"}}]}\n\n" +
			"data: {\"id\":\"cc-1\",\"choices\":[{\"delta\":{\"content\":\"llo\"}}]}\n\n" +
			"data: {\"id\":\"cc-1\",\"choices\":[],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":2,\"total_tokens\":11}}\n\n" +
			"data: [DONE]\n\n"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// ginCtx 构造带 recorder 的 gin 上下文。
func ginCtx(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	return c, w
}

// subscribeEvent 预先订阅事件，返回收集通道（须在 Handle 之前调用；投递快照，勿重复消费）。
func subscribeEvent(bus *eventbus.Bus, typ string, n int) <-chan []eventbus.Event {
	ch := make(chan []eventbus.Event, 1)
	var (
		mu  sync.Mutex
		got []eventbus.Event
	)
	bus.Subscribe(typ, func(e eventbus.Event) {
		mu.Lock()
		got = append(got, e)
		reached := len(got) == n
		snapshot := make([]eventbus.Event, len(got))
		copy(snapshot, got)
		mu.Unlock()
		if reached {
			select {
			case ch <- snapshot:
			default:
			}
		}
	})
	return ch
}

func waitEvents(t *testing.T, ch <-chan []eventbus.Event, typ string, n int) []eventbus.Event {
	t.Helper()
	select {
	case evts := <-ch:
		if len(evts) < n {
			t.Fatalf("%s 事件不足: %d", typ, len(evts))
		}
		return evts
	case <-time.After(5 * time.Second):
		t.Fatalf("等待 %s x%d 超时", typ, n)
		return nil
	}
}

func TestExecutorPassthroughNonStream(t *testing.T) {
	srv := openAIUpstream(t)
	channels := &memChannels{items: []model.Channel{
		ch("oa", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
	}}
	channels.items[0].BaseURL = srv.URL

	bus := eventbus.New()
	defer bus.Wait()
	evCh := subscribeEvent(bus, eventbus.EventRequestCompleted, 1)
	logs := &memLogs{}
	exec := gateway.NewExecutor(channels, logs, bus, 10*time.Second)

	c, w := ginCtx(t)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolChatCompletions, body, false))

	// 透传：响应体原样
	if !strings.Contains(w.Body.String(), `"cached_tokens":4`) {
		t.Errorf("透传响应体被改写: %s", w.Body.String())
	}
	if w.Code != 200 {
		t.Errorf("status = %d", w.Code)
	}
	// 日志
	items := logs.snapshot()
	if len(items) != 1 {
		t.Fatalf("logs = %d", len(items))
	}
	lg := items[0]
	if lg.ForwardMode != model.ForwardNativePassthrough || lg.UpstreamStatus != 200 {
		t.Errorf("log = %+v", lg)
	}
	if lg.PromptTokens != 12 || lg.CompletionTokens != 6 || lg.CachedTokens != 4 {
		t.Errorf("usage 错误: %+v", lg)
	}
	if lg.CacheHitRate <= 0.32 || lg.CacheHitRate >= 0.34 {
		t.Errorf("cache_hit_rate = %f", lg.CacheHitRate)
	}
	if lg.TotalTokens != 18 {
		t.Errorf("total = %d", lg.TotalTokens)
	}
	// 事件
	evts := waitEvents(t, evCh, eventbus.EventRequestCompleted, 1)
	l, _ := evts[0].Payload.(*model.RequestLog)
	if l == nil || l.ChannelName != "oa" {
		t.Errorf("事件 payload 错误: %+v", evts[0].Payload)
	}
}

func TestExecutorConvertedNonStream(t *testing.T) {
	srv := openAIUpstream(t)
	channels := &memChannels{items: []model.Channel{
		// openai 兼容渠道、未探测出 messages 原生 -> 入站 anthropic 协议需转换
		ch("oa-conv", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
	}}
	channels.items[0].BaseURL = srv.URL

	logs := &memLogs{}
	exec := gateway.NewExecutor(channels, logs, eventbus.New(), 10*time.Second)

	c, w := ginCtx(t)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"max_tokens":32}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolMessages, body, false))

	// 客户端收到 anthropic 形态响应
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应非法 JSON: %s", w.Body.String())
	}
	if resp["type"] != "message" {
		t.Errorf("应为 anthropic message 形态: %v", resp)
	}
	usage := resp["usage"].(map[string]any)
	if usage["input_tokens"] != float64(12) || usage["output_tokens"] != float64(6) {
		t.Errorf("usage 转换错误: %v", usage)
	}
	lg := logs.snapshot()[0]
	if lg.ForwardMode != model.ForwardConverted {
		t.Errorf("forward_mode = %s", lg.ForwardMode)
	}
	if lg.PromptTokens != 12 || lg.CompletionTokens != 6 {
		t.Errorf("usage: %+v", lg)
	}
}

func TestExecutorStreamPassthrough(t *testing.T) {
	srv := openAIUpstream(t)
	channels := &memChannels{items: []model.Channel{
		ch("oa", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
	}}
	channels.items[0].BaseURL = srv.URL

	logs := &memLogs{}
	exec := gateway.NewExecutor(channels, logs, eventbus.New(), 10*time.Second)

	c, w := ginCtx(t)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"stream":true}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolChatCompletions, body, true))

	out := w.Body.String()
	if !strings.Contains(out, `"content":"he"`) || !strings.Contains(out, `[DONE]`) {
		t.Errorf("SSE 未透传: %s", out)
	}
	if w.Header().Get("Content-Type") != "text/event-stream; charset=utf-8" {
		t.Errorf("content-type = %s", w.Header().Get("Content-Type"))
	}
	lg := logs.snapshot()[0]
	if !lg.Stream || lg.PromptTokens != 9 || lg.CompletionTokens != 2 {
		t.Errorf("流式统计错误: %+v", lg)
	}
}

func TestExecutorFailover(t *testing.T) {
	// 渠道1: 域名指向不可达端口（网络失败）；渠道2: 正常 mock
	srv := openAIUpstream(t)
	channels := &memChannels{items: []model.Channel{
		ch("dead", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
		ch("alive", model.ProviderOpenAICompatible, 2, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
	}}
	channels.items[0].BaseURL = "http://127.0.0.1:1" // 不可达
	channels.items[1].BaseURL = srv.URL

	logs := &memLogs{}
	exec := gateway.NewExecutor(channels, logs, eventbus.New(), 3*time.Second)

	c, w := ginCtx(t)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolChatCompletions, body, false))

	if w.Code != 200 || !strings.Contains(w.Body.String(), "cached_tokens") {
		t.Errorf("failover 后应成功: code=%d body=%s", w.Code, w.Body.String())
	}
	lg := logs.snapshot()[0]
	if lg.ChannelName != "alive" {
		t.Errorf("应记录成功渠道: %+v", lg)
	}
}

func TestExecutorRetryableUpstreamError(t *testing.T) {
	// 渠道1 返回 500（可重试）-> 渠道2 成功
	failSrv := openAIUpstream(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	})
	failSrv2 := httptest.NewServer(mux)
	t.Cleanup(failSrv2.Close)

	channels := &memChannels{items: []model.Channel{
		ch("fail500", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
		ch("ok", model.ProviderOpenAICompatible, 2, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
	}}
	channels.items[0].BaseURL = failSrv2.URL
	channels.items[1].BaseURL = failSrv.URL

	logs := &memLogs{}
	exec := gateway.NewExecutor(channels, logs, eventbus.New(), 3*time.Second)

	c, w := ginCtx(t)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolChatCompletions, body, false))

	if !strings.Contains(w.Body.String(), "cached_tokens") {
		t.Errorf("500 后应 failover: %s", w.Body.String())
	}
	if logs.snapshot()[0].ChannelName != "ok" {
		t.Errorf("日志渠道: %+v", logs.snapshot()[0])
	}
}

func TestExecutorNoChannel(t *testing.T) {
	channels := &memChannels{items: []model.Channel{
		ch("disabled", model.ProviderOpenAICompatible, 1, 1, model.ChannelDisabled, "gpt-4o", "chat_completions"),
	}}
	logs := &memLogs{}
	exec := gateway.NewExecutor(channels, logs, eventbus.New(), time.Second)

	c, w := ginCtx(t)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolChatCompletions, body, false))

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
	lg := logs.snapshot()[0]
	if lg.Error == "" {
		t.Error("无渠道请求应记录错误日志")
	}
}

func TestExecutorEstimateFallback(t *testing.T) {
	// 上游不返回 usage -> 本地估算兜底
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"message":{"role":"assistant","content":"ok answer"},"finish_reason":"stop"}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	channels := &memChannels{items: []model.Channel{
		ch("oa", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
	}}
	channels.items[0].BaseURL = srv.URL

	logs := &memLogs{}
	exec := gateway.NewExecutor(channels, logs, eventbus.New(), time.Second)

	c, w := ginCtx(t)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"tell me something"}]}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolChatCompletions, body, false))

	lg := logs.snapshot()[0]
	if lg.PromptTokens <= 0 || lg.CompletionTokens <= 0 {
		t.Errorf("估算兜底未生效: %+v", lg)
	}
}

// TestExecutorStartedEventMatchesCompleted 受理即发 started 事件，且与完成事件 ReqID 配对。
func TestExecutorStartedEventMatchesCompleted(t *testing.T) {
	srv := openAIUpstream(t)
	channels := &memChannels{items: []model.Channel{
		ch("oa", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
	}}
	channels.items[0].BaseURL = srv.URL

	bus := eventbus.New()
	defer bus.Wait()
	startCh := subscribeEvent(bus, eventbus.EventRequestStarted, 1)
	doneCh := subscribeEvent(bus, eventbus.EventRequestCompleted, 1)
	exec := gateway.NewExecutor(channels, &memLogs{}, bus, 10*time.Second)

	c, w := ginCtx(t)
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolChatCompletions,
		`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`, false))

	started, _ := waitEvents(t, startCh, eventbus.EventRequestStarted, 1)[0].Payload.(*model.RequestLog)
	done, _ := waitEvents(t, doneCh, eventbus.EventRequestCompleted, 1)[0].Payload.(*model.RequestLog)
	if started.ReqID == 0 || started.ReqID != done.ReqID {
		t.Errorf("ReqID 不配对: started=%d completed=%d", started.ReqID, done.ReqID)
	}
	if started.CreatedAt.IsZero() {
		t.Error("started 事件应带受理时间戳")
	}
}

// capturingUpstream 记录收到的 model 字段并返回合法 chat completion 响应。
func capturingUpstream(t *testing.T, seen *[]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		*seen = append(*seen, req.Model)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cc-1","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestExecutorGroupRoutingRewritesModel 自定义模型组端到端：
// 客户端请求分组 ID（free-1M），上游实际收到成员模型名，日志与完成事件记录实际模型。
func TestExecutorGroupRoutingRewritesModel(t *testing.T) {
	var seen []string
	srv := capturingUpstream(t, &seen)
	channels := &memChannels{items: []model.Channel{
		ch("free-ch", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "glm-flash", "chat_completions"),
	}}
	channels.items[0].BaseURL = srv.URL
	group := model.CustomModel{Name: "free-1M", Status: model.ChannelEnabled}
	group.SetMembers([]model.ModelMember{{Model: "glm-flash", Priority: 0}})
	customs := &memCustoms{items: []model.CustomModel{group}}

	bus := eventbus.New()
	defer bus.Wait()
	evCh := subscribeEvent(bus, eventbus.EventRequestCompleted, 1)
	logs := &memLogs{}
	exec := gateway.NewExecutor(channels, logs, bus, 10*time.Second)
	exec.SetCustomModels(customs)

	c, w := ginCtx(t)
	body := `{"model":"free-1M","messages":[{"role":"user","content":"hi"}]}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolChatCompletions, body, false))

	if w.Code != 200 || !strings.Contains(w.Body.String(), `"content":"ok"`) {
		t.Fatalf("响应异常: code=%d body=%s", w.Code, w.Body.String())
	}
	if len(seen) != 1 || seen[0] != "glm-flash" {
		t.Errorf("上游应收到成员模型 glm-flash, got %v", seen)
	}
	snap := logs.snapshot()
	if len(snap) != 1 || snap[0].Model != "glm-flash" || snap[0].ChannelName != "free-ch" {
		t.Errorf("日志应记录实际成员模型与渠道, got %+v", snap)
	}
	evts := waitEvents(t, evCh, "request.completed", 1)
	lg, ok := evts[0].Payload.(*model.RequestLog)
	if !ok {
		t.Fatalf("事件负载类型错误: %T", evts[0].Payload)
	}
	if lg.Model != "glm-flash" {
		t.Errorf("完成事件 Model = %q, want glm-flash", lg.Model)
	}
}

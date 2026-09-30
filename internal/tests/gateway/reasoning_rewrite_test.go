package gateway_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"xtokenhub/internal/eventbus"
	"xtokenhub/internal/gateway"
	"xtokenhub/internal/model"
)

// memRewrites 内存改写规则源（实现 gateway.RewriteSource）。
type memRewrites struct {
	items map[string]model.ReasoningRewrite
}

func (m *memRewrites) RewriteFor(_ context.Context, upModel string) (model.ReasoningRewrite, bool) {
	r, ok := m.items[upModel]
	return r, ok
}

// mimoRewrites 预置映射（同官方推荐）：none/minimal/low -> disabled，其余档位 -> enabled。
func mimoRewrites(models ...string) *memRewrites {
	rule := model.ReasoningRewrite{Mapping: model.ReasoningMappingList{
		{Level: "none", Target: model.ReasoningOff},
		{Level: "minimal", Target: model.ReasoningOff},
		{Level: "low", Target: model.ReasoningOff},
		{Level: "medium", Target: model.ReasoningOn},
		{Level: "high", Target: model.ReasoningOn},
		{Level: "xhigh", Target: model.ReasoningOn},
		{Level: "max", Target: model.ReasoningOn},
	}}
	items := make(map[string]model.ReasoningRewrite, len(models))
	for _, m := range models {
		items[m] = rule
	}
	return &memRewrites{items: items}
}

// captureUpstream 捕获发往 /v1/chat/completions 的请求体（支持流式/非流式应答）。
type captureUpstream struct {
	mu   sync.Mutex
	body string
}

func (cu *captureUpstream) start(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		raw := make([]byte, 1<<20)
		n, _ := r.Body.Read(raw)
		cu.mu.Lock()
		cu.body = string(raw[:n])
		cu.mu.Unlock()
		var req struct {
			Stream bool `json:"stream"`
		}
		_ = json.Unmarshal(raw[:n], &req)
		if !req.Stream {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"cc","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
				"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n" +
			"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\ndata: [DONE]\n\n"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func (cu *captureUpstream) lastBody() string {
	cu.mu.Lock()
	defer cu.mu.Unlock()
	return cu.body
}

// assertUpstreamBody 校验上游收到体：thinking.type 等于期望（空 = 不应出现）且无/有 reasoning_effort。
func assertUpstreamBody(t *testing.T, body, wantThinking, wantEffort string) {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("上游体非法: %v: %s", err, body)
	}
	th, hasThinking := m["thinking"]
	if wantThinking == "" {
		if hasThinking {
			t.Fatalf("不应出现 thinking: %s", body)
		}
	} else if !hasThinking || !strings.Contains(string(th), `"`+wantThinking+`"`) {
		t.Fatalf("thinking 应为 %q: %s", wantThinking, body)
	}
	eff, hasEffort := m["reasoning_effort"]
	if wantEffort == "" {
		if hasEffort {
			t.Fatalf("不应出现 reasoning_effort: %s", body)
		}
	} else if !hasEffort || string(eff) != `"`+wantEffort+`"` {
		t.Fatalf("reasoning_effort 应为 %q: %s", wantEffort, body)
	}
}

// 端到端：chat 客户端带 reasoning_effort 请求 MiMo 型上游（透传路径），
// 上游应收到 thinking 开关而非档位串；无规则时原样透传。
func TestExecutorRewriteChatPassthrough(t *testing.T) {
	cu := &captureUpstream{}
	up := cu.start(t)
	channels := &memChannels{items: []model.Channel{
		ch("mimo", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "mimo-v2.6-pro,other-model", "chat_completions"),
	}}
	channels.items[0].BaseURL = up.URL

	bus := eventbus.New()
	defer bus.Wait()
	logs := &memLogs{}
	exec := gateway.NewExecutor(channels, logs, bus, 10*time.Second)
	exec.SetRewrites(mimoRewrites("mimo-v2.6-pro"))

	c, w := ginCtx(t)
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolChatCompletions,
		`{"model":"mimo-v2.6-pro","reasoning_effort":"low","messages":[{"role":"user","content":"hi"}]}`, false))
	assertUpstreamBody(t, cu.lastBody(), model.ReasoningOff, "")

	// 无规则的模型：档位原样透传
	c, w = ginCtx(t)
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolChatCompletions,
		`{"model":"other-model","reasoning_effort":"high","messages":[{"role":"user","content":"hi"}]}`, false))
	assertUpstreamBody(t, cu.lastBody(), "", "high")
}

// 流式透传同样改写。
func TestExecutorRewriteChatStream(t *testing.T) {
	cu := &captureUpstream{}
	up := cu.start(t)
	channels := &memChannels{items: []model.Channel{
		ch("mimo", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "mimo-v2.6-pro", "chat_completions"),
	}}
	channels.items[0].BaseURL = up.URL

	bus := eventbus.New()
	defer bus.Wait()
	exec := gateway.NewExecutor(channels, &memLogs{}, bus, 10*time.Second)
	exec.SetRewrites(mimoRewrites("mimo-v2.6-pro"))

	c, w := ginCtx(t)
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolChatCompletions,
		`{"model":"mimo-v2.6-pro","reasoning_effort":"max","messages":[{"role":"user","content":"hi"}]}`, true))
	assertUpstreamBody(t, cu.lastBody(), model.ReasoningOn, "")
}

// 转换路径：messages 客户端（thinking.type 伪档位）请求 chat 上游，
// 档位经 ReqParams 回写 reasoning_effort 后仍应被翻译成 thinking 开关。
func TestExecutorRewriteConvertedMessages(t *testing.T) {
	cu := &captureUpstream{}
	up := cu.start(t)
	channels := &memChannels{items: []model.Channel{
		ch("mimo-anthropic-client", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "mimo-v2.6-pro", "chat_completions"),
	}}
	channels.items[0].BaseURL = up.URL

	bus := eventbus.New()
	defer bus.Wait()
	exec := gateway.NewExecutor(channels, &memLogs{}, bus, 10*time.Second)
	exec.SetRewrites(mimoRewrites("mimo-v2.6-pro"))

	c, w := ginCtx(t)
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolMessages,
		`{"model":"mimo-v2.6-pro","max_tokens":64,"thinking":{"type":"low"},"messages":[{"role":"user","content":"hi"}]}`, false))
	assertUpstreamBody(t, cu.lastBody(), model.ReasoningOff, "")
}

// 未带档位 + 配置 missing：出站体注入 thinking 开关。
func TestExecutorRewriteMissingInjection(t *testing.T) {
	cu := &captureUpstream{}
	up := cu.start(t)
	channels := &memChannels{items: []model.Channel{
		ch("mimo", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "mimo-v2.6-pro", "chat_completions"),
	}}
	channels.items[0].BaseURL = up.URL

	rule := model.ReasoningRewrite{Missing: model.ReasoningOff}
	bus := eventbus.New()
	defer bus.Wait()
	exec := gateway.NewExecutor(channels, &memLogs{}, bus, 10*time.Second)
	exec.SetRewrites(&memRewrites{items: map[string]model.ReasoningRewrite{"mimo-v2.6-pro": rule}})

	c, w := ginCtx(t)
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolChatCompletions,
		`{"model":"mimo-v2.6-pro","messages":[{"role":"user","content":"hi"}]}`, false))
	assertUpstreamBody(t, cu.lastBody(), model.ReasoningOff, "")
}

// 事件订阅兼容性检查：改写不应影响完成事件与落库（复用既有事件契约）。
func TestExecutorRewriteStillLogs(t *testing.T) {
	cu := &captureUpstream{}
	up := cu.start(t)
	channels := &memChannels{items: []model.Channel{
		ch("mimo", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "mimo-v2.6-pro", "chat_completions"),
	}}
	channels.items[0].BaseURL = up.URL

	bus := eventbus.New()
	defer bus.Wait()
	logs := &memLogs{}
	exec := gateway.NewExecutor(channels, logs, bus, 10*time.Second)
	exec.SetRewrites(mimoRewrites("mimo-v2.6-pro"))

	c, w := ginCtx(t)
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolChatCompletions,
		`{"model":"mimo-v2.6-pro","reasoning_effort":"high","messages":[{"role":"user","content":"hi"}]}`, false))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	snap := logs.snapshot()
	if len(snap) != 1 || snap[0].TotalTokens != 2 {
		t.Fatalf("落库异常: %+v", snap)
	}
}

package gateway_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"xtokenhub/internal/eventbus"
	"xtokenhub/internal/gateway"
	"xtokenhub/internal/model"
	"xtokenhub/internal/service"
)

// stubPricing 固定费率的计价桩，直接复用 service.Calculate，验证网关传对了口径与 token。
type stubPricing struct {
	calls int
	seen  []model.UsageStyle
}

const (
	stubRateIn    = 0.000001  // 1 USD/M
	stubRateOut   = 0.000002  // 2 USD/M
	stubRateRead  = 0.0000001 // 0.1 USD/M
	stubRateWrite = 0.0000005 // 0.5 USD/M
)

func (s *stubPricing) Cost(l *model.RequestLog, style model.UsageStyle) float64 {
	s.calls++
	s.seen = append(s.seen, style)
	return service.Calculate(service.UsageInput{
		PromptTokens:     l.PromptTokens,
		CompletionTokens: l.CompletionTokens,
		CachedTokens:     l.CachedTokens,
		CacheWriteTokens: l.CacheWriteTokens,
		Style:            style,
	}, model.ModelPrice{
		InputCostPerToken:      stubRateIn,
		OutputCostPerToken:     stubRateOut,
		CacheReadCostPerToken:  stubRateRead,
		CacheWriteCostPerToken: stubRateWrite,
	}, time.Now(), 0)
}

// OpenAI 透传：prompt 含缓存命中，未命中部分要扣减；口径记为 openai。
func TestExecutorPricesOpenAIPassthrough(t *testing.T) {
	srv := openAIUpstream(t)
	channels := &memChannels{items: []model.Channel{
		ch("oa", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
	}}
	channels.items[0].BaseURL = srv.URL

	logs := &memLogs{}
	pricing := &stubPricing{}
	exec := gateway.NewExecutor(channels, logs, eventbus.New(), 10*time.Second)
	exec.SetPricing(pricing)

	c, w := ginCtx(t)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolChatCompletions, body, false))

	items := logs.snapshot()
	if len(items) != 1 {
		t.Fatalf("logs = %d", len(items))
	}
	lg := items[0]
	// mock 上游报 prompt=12 / cached=4 / completion=6
	if lg.PromptTokens != 12 || lg.CachedTokens != 4 || lg.CompletionTokens != 6 {
		t.Fatalf("usage 与断言不符: %+v", lg)
	}
	if lg.UsageStyle != model.UsageStyleOpenAI {
		t.Errorf("usage_style = %q, want openai", lg.UsageStyle)
	}
	// (12-4)*1e-6 + 4*1e-7 + 6*2e-6 = 8e-6 + 0.4e-6 + 12e-6 = 20.4e-6
	want := 8*stubRateIn + 4*stubRateRead + 6*stubRateOut
	if diff := lg.CostUSD - want; diff > 1e-12 || diff < -1e-12 {
		t.Errorf("cost_usd = %v, want %v", lg.CostUSD, want)
	}
	if pricing.seen[0] != model.UsageStyleOpenAI {
		t.Errorf("传给计价的口径 = %q", pricing.seen[0])
	}
}

// Anthropic 透传：input_tokens 不含缓存读写，三者独立计费；口径记为 anthropic。
func TestExecutorPricesAnthropicPassthrough(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","model":"claude-3","content":[{"type":"text","text":"ok"}],
			"usage":{"input_tokens":100,"output_tokens":20,
			"cache_read_input_tokens":40,"cache_creation_input_tokens":7}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	channels := &memChannels{items: []model.Channel{
		ch("claude", model.ProviderAnthropic, 1, 1, model.ChannelEnabled, "claude-3", "messages"),
	}}
	channels.items[0].BaseURL = srv.URL

	logs := &memLogs{}
	pricing := &stubPricing{}
	exec := gateway.NewExecutor(channels, logs, eventbus.New(), 10*time.Second)
	exec.SetPricing(pricing)

	c, w := ginCtx(t)
	body := `{"model":"claude-3","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolMessages, body, false))

	items := logs.snapshot()
	if len(items) != 1 {
		t.Fatalf("logs = %d", len(items))
	}
	lg := items[0]
	if lg.PromptTokens != 100 || lg.CachedTokens != 40 || lg.CacheWriteTokens != 7 {
		t.Fatalf("usage 与断言不符: %+v", lg)
	}
	if lg.UsageStyle != model.UsageStyleAnthropic {
		t.Errorf("usage_style = %q, want anthropic", lg.UsageStyle)
	}
	// 100*1e-6 + 40*1e-7 + 7*5e-7 + 20*2e-6
	want := 100*stubRateIn + 40*stubRateRead + 7*stubRateWrite + 20*stubRateOut
	if diff := lg.CostUSD - want; diff > 1e-12 || diff < -1e-12 {
		t.Errorf("cost_usd = %v, want %v", lg.CostUSD, want)
	}
}

// 转换模式下上游是 Anthropic：口径必须按上游判定，而不是按入站 chat 协议。
// 这正是「费用必须在网关写入时算」的原因——落库后无法从 inbound protocol 还原。
func TestExecutorPricesByUpstreamProtocolWhenConverted(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","model":"claude-3","content":[{"type":"text","text":"ok"}],
			"usage":{"input_tokens":100,"output_tokens":0,"cache_read_input_tokens":40}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	channels := &memChannels{items: []model.Channel{
		ch("claude", model.ProviderAnthropic, 1, 1, model.ChannelEnabled, "claude-3", "messages"),
	}}
	channels.items[0].BaseURL = srv.URL

	logs := &memLogs{}
	exec := gateway.NewExecutor(channels, logs, eventbus.New(), 10*time.Second)
	exec.SetPricing(&stubPricing{})

	c, w := ginCtx(t)
	// 入站 chat_completions，渠道只原生支持 messages -> 走转换
	body := `{"model":"claude-3","messages":[{"role":"user","content":"hi"}]}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolChatCompletions, body, false))

	items := logs.snapshot()
	if len(items) != 1 {
		t.Fatalf("logs = %d", len(items))
	}
	if items[0].ForwardMode != model.ForwardConverted {
		t.Fatalf("应为转换模式: %+v", items[0])
	}
	if items[0].Protocol != model.ProtocolChatCompletions {
		t.Fatalf("入站协议应记为 chat_completions: %q", items[0].Protocol)
	}
	if items[0].UsageStyle != model.UsageStyleAnthropic {
		t.Errorf("usage_style = %q, want anthropic（按上游而非入站协议）", items[0].UsageStyle)
	}
}

// 未注入计价源时不应计费，也不应影响请求处理。
func TestExecutorWithoutPricingLeavesCostZero(t *testing.T) {
	srv := openAIUpstream(t)
	channels := &memChannels{items: []model.Channel{
		ch("oa", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
	}}
	channels.items[0].BaseURL = srv.URL

	logs := &memLogs{}
	exec := gateway.NewExecutor(channels, logs, eventbus.New(), 10*time.Second)

	c, w := ginCtx(t)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolChatCompletions, body, false))

	items := logs.snapshot()
	if len(items) != 1 {
		t.Fatalf("logs = %d", len(items))
	}
	if items[0].CostUSD != 0 {
		t.Errorf("未注入计价源时费用 = %v, want 0", items[0].CostUSD)
	}
}

// windowPricing 带时段规则的计价桩：验证网关会把计价器写入的 price_period 落库。
type windowPricing struct {
	window model.PeakWindow
}

func (p *windowPricing) Cost(l *model.RequestLog, style model.UsageStyle) float64 {
	// 与 CostService.Cost 同样按请求开始时刻判定时段并回写。
	at := l.CreatedAt
	if at.IsZero() {
		at = time.Now()
	}
	l.PricePeriod = p.window.Period(at)
	return service.Calculate(service.UsageInput{
		PromptTokens:     l.PromptTokens,
		CompletionTokens: l.CompletionTokens,
		CachedTokens:     l.CachedTokens,
		CacheWriteTokens: l.CacheWriteTokens,
		Style:            style,
	}, model.ModelPrice{
		PeakWindow:                p.window,
		InputCostPerToken:         2e-6,
		OutputCostPerToken:        8e-6,
		OffPeakInputCostPerToken:  1e-6,
		OffPeakOutputCostPerToken: 4e-6,
	}, at, 0)
}

// 网关落库应保留计价时段，供请求日志审计「这笔为何更便宜」。
func TestExecutorPersistsPricePeriod(t *testing.T) {
	srv := openAIUpstream(t)
	channels := &memChannels{items: []model.Channel{
		ch("oa", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
	}}
	channels.items[0].BaseURL = srv.URL

	logs := &memLogs{}
	// 全周 00:00-23:59 为高峰不现实，这里用“仅周一 09:00-12:00 为高峰”，
	// 测试当前真实时刻落在哪个时段并断言落库值与之相符。
	pricing := &windowPricing{window: model.MustPeakWindow("1;09:00-12:00")}
	exec := gateway.NewExecutor(channels, logs, eventbus.New(), 10*time.Second)
	exec.SetPricing(pricing)

	c, w := ginCtx(t)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolChatCompletions, body, false))

	items := logs.snapshot()
	if len(items) != 1 {
		t.Fatalf("logs = %d", len(items))
	}
	lg := items[0]
	// 落库的时段必须与该请求开始时刻判定的一致（非空，因为配了时段规则）。
	want := pricing.window.Period(lg.CreatedAt)
	if want == "" {
		t.Fatal("测试前提异常：配了时段规则却得到空时段")
	}
	if lg.PricePeriod != want {
		t.Errorf("落库 price_period = %q, want %q", lg.PricePeriod, want)
	}
}

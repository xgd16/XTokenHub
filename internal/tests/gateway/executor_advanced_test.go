package gateway_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xtokenhub/internal/eventbus"
	"xtokenhub/internal/gateway"
	"xtokenhub/internal/model"
)

// anthropicStreamUpstream 返回 anthropic SSE 事件流的上游。
func anthropicStreamUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":20}}}\n\n" +
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"héllo \"}}\n\n" +
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"世界\"}}\n\n" +
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":5}}\n\n" +
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestExecutorStreamConverted(t *testing.T) {
	// 入站 chat/completions 流式 -> anthropic 上游（转换路径）
	srv := anthropicStreamUpstream(t)
	channels := &memChannels{items: []model.Channel{
		// 未探测出 chat_completions 原生 -> 走转换
		ch("anth-conv", model.ProviderAnthropic, 1, 1, model.ChannelEnabled, "claude-3", "messages"),
	}}
	channels.items[0].BaseURL = srv.URL

	logs := &memLogs{}
	exec := gateway.NewExecutor(channels, logs, eventbus.New(), 5*time.Second)

	c, w := ginCtx(t)
	body := `{"model":"claude-3","messages":[{"role":"user","content":"hi"}],"stream":true,"max_tokens":32}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolChatCompletions, body, true))

	out := w.Body.String()
	// 客户端应收到 openai chunk 格式 + usage + [DONE]
	if !strings.Contains(out, `"content":"héllo `) || !strings.Contains(out, "[DONE]") {
		t.Errorf("转换流输出异常: %s", out)
	}
	if !strings.Contains(out, `"prompt_tokens":20`) || !strings.Contains(out, `"completion_tokens":5`) {
		t.Errorf("缺少 usage 块: %s", out)
	}
	lg := logs.snapshot()[0]
	if lg.ForwardMode != model.ForwardConverted || !lg.Stream {
		t.Errorf("log = %+v", lg)
	}
	if lg.PromptTokens != 20 || lg.CompletionTokens != 5 {
		t.Errorf("usage: %+v", lg)
	}
}

func TestExecutorNonRetryableForwarded(t *testing.T) {
	// 上游 400（不可重试）-> 原样回传给客户端 + 记录错误日志
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid model"}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	channels := &memChannels{items: []model.Channel{
		ch("oa", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
	}}
	channels.items[0].BaseURL = srv.URL

	logs := &memLogs{}
	exec := gateway.NewExecutor(channels, logs, eventbus.New(), 3*time.Second)

	c, w := ginCtx(t)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolChatCompletions, body, false))

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid model") {
		t.Errorf("错误详情应回传: %s", w.Body.String())
	}
	lg := logs.snapshot()[0]
	if lg.Error == "" || lg.UpstreamStatus != 400 {
		t.Errorf("日志: %+v", lg)
	}
}

func TestExecutorAllCandidatesFail(t *testing.T) {
	// 唯一渠道持续 500 -> 502，日志带渠道信息与转发模式
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"overloaded"}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	channels := &memChannels{items: []model.Channel{
		ch("only", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
	}}
	channels.items[0].BaseURL = srv.URL

	logs := &memLogs{}
	exec := gateway.NewExecutor(channels, logs, eventbus.New(), 2*time.Second)

	c, w := ginCtx(t)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolChatCompletions, body, false))

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", w.Code)
	}
	lg := logs.snapshot()[0]
	if lg.ChannelName != "only" || lg.ForwardMode != model.ForwardNativePassthrough || lg.Error == "" {
		t.Errorf("失败日志应保留渠道信息: %+v", lg)
	}
}

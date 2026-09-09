package gateway_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xtokenhub/internal/eventbus"
	"xtokenhub/internal/gateway"
	"xtokenhub/internal/model"
)

// responsesNativeUpstream 原生支持 responses 协议的上游：记录命中的路径，
// 流式返回官方形态的 responses 事件。
func responsesNativeUpstream(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	hit := new(string)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, r *http.Request) {
		*hit = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		// 透传须保持 responses 形态（input 字段），而非被改写成 chat 的 messages
		if !strings.Contains(string(body), `"input"`) {
			t.Errorf("透传请求体被改写: %s", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n" +
				"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"usage\":{\"input_tokens\":3,\"output_tokens\":1}}}\n\n"))
	})
	mux.HandleFunc("/v1/chat/completions", func(http.ResponseWriter, *http.Request) {
		t.Errorf("responses 透传不应请求 /v1/chat/completions")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, hit
}

func TestExecutorMessagesClientFromChatUpstreamCarriesTools(t *testing.T) {
	// messages 客户端含 tool_use/tool_result 历史 -> chat 上游：
	// 工具调用/结果应完整映射（assistant.tool_calls / role=tool），
	// 且不产生空 content 消息（否则上游报 400）。
	var gotBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"data: {\"id\":\"c1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n" +
				"data: {\"id\":\"c1\",\"model\":\"m\",\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":1,\"total_tokens\":6}}\n\n" +
				"data: [DONE]\n\n"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	channels := &memChannels{items: []model.Channel{
		ch("oa", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "m", "chat_completions"),
	}}
	channels.items[0].BaseURL = srv.URL

	exec := gateway.NewExecutor(channels, &memLogs{}, eventbus.New(), 5*time.Second)
	c, w := ginCtx(t)
	body := `{"model":"m","max_tokens":16,"stream":true,"messages":[
		{"role":"user","content":"hi"},
		{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"f","input":{}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"result"}]},
		{"role":"assistant","content":[{"type":"text","text":"done"}]}]}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolMessages, body, true))

	up := string(gotBody)
	if strings.Contains(up, `"content":""`) {
		t.Errorf("上游请求含空 content 消息: %s", up)
	}
	if n := strings.Count(up, `"role"`); n != 4 {
		t.Errorf("应保留全部 4 条消息（含工具历史）, got %d: %s", n, up)
	}
	for _, want := range []string{`"tool_calls"`, `"tool_call_id":"t1"`, `"name":"f"`} {
		if !strings.Contains(up, want) {
			t.Errorf("上游请求缺 %s: %s", want, up)
		}
	}
	if !strings.Contains(w.Body.String(), `"text":"ok"`) {
		t.Errorf("anthropic 客户端事件异常: %s", w.Body.String())
	}
}

func TestExecutorResponsesClientFromAnthropicUpstream(t *testing.T) {
	// responses 客户端 -> anthropic 上游（转换）：请求转 messages、响应转 responses 事件。
	// anthropic 协议 max_tokens 必填，responses 请求缺 max_output_tokens 时应兜底。
	var gotBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_9\",\"model\":\"claude-3\",\"usage\":{\"input_tokens\":7}}}\n\n" +
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n" +
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n" +
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	channels := &memChannels{items: []model.Channel{
		ch("claude", model.ProviderAnthropic, 1, 1, model.ChannelEnabled, "claude-3", "messages"),
	}}
	channels.items[0].BaseURL = srv.URL

	logs := &memLogs{}
	exec := gateway.NewExecutor(channels, logs, eventbus.New(), 5*time.Second)

	c, w := ginCtx(t)
	body := `{"model":"claude-3","input":"hi","stream":true}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolResponses, body, true))

	// 上游请求体：messages 形态 + max_tokens 兜底
	up := string(gotBody)
	if !strings.Contains(up, `"messages"`) || !strings.Contains(up, `"max_tokens":1024`) {
		t.Errorf("转换后的 anthropic 请求体异常: %s", up)
	}
	// 客户端：responses 事件流
	out := w.Body.String()
	for _, want := range []string{"event: response.created\n", `"type":"response.output_text.delta"`, `"delta":"hi"`, "event: response.completed\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("缺少 %q: %s", want, out)
		}
	}
	lg := logs.snapshot()[0]
	if lg.ForwardMode != model.ForwardConverted || lg.PromptTokens != 7 || lg.CompletionTokens != 2 {
		t.Errorf("log = %+v", lg)
	}
}

func TestExecutorResponsesClientNonStream(t *testing.T) {
	// chat 上游非流式 -> responses 客户端：响应体须满足官方 SDK 的必填字段
	srv := openAIUpstream(t)
	channels := &memChannels{items: []model.Channel{
		ch("oa-conv", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
	}}
	channels.items[0].BaseURL = srv.URL

	logs := &memLogs{}
	exec := gateway.NewExecutor(channels, logs, eventbus.New(), 5*time.Second)

	c, w := ginCtx(t)
	body := `{"model":"gpt-4o","input":"hi"}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolResponses, body, false))

	out := w.Body.String()
	for _, want := range []string{
		`"object":"response"`, `"created_at"`, `"parallel_tool_calls"`, `"tool_choice"`, `"tools"`,
		`"type":"output_text"`, `"annotations"`, `"output_tokens_details"`, "你好，世界",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("响应体缺少 %q: %s", want, out)
		}
	}
	lg := logs.snapshot()[0]
	if lg.ForwardMode != model.ForwardConverted || lg.PromptTokens != 12 || lg.CompletionTokens != 6 {
		t.Errorf("log = %+v", lg)
	}
}

func TestExecutorResponsesPassthroughEndpoint(t *testing.T) {
	// 渠道原生支持 responses -> 透传，必须打到 /v1/responses 且请求体原样
	srv, hit := responsesNativeUpstream(t)
	channels := &memChannels{items: []model.Channel{
		ch("ds-native", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "gpt-4o", "chat_completions,responses"),
	}}
	channels.items[0].BaseURL = srv.URL

	logs := &memLogs{}
	exec := gateway.NewExecutor(channels, logs, eventbus.New(), 5*time.Second)

	c, w := ginCtx(t)
	body := `{"model":"gpt-4o","input":"hi","stream":true}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolResponses, body, true))

	if *hit != "/v1/responses" {
		t.Fatalf("上游路径 = %q, want /v1/responses", *hit)
	}
	out := w.Body.String()
	if !strings.Contains(out, `"type":"response.completed"`) {
		t.Errorf("透传响应体异常: %s", out)
	}
	lg := logs.snapshot()[0]
	if lg.ForwardMode != model.ForwardNativePassthrough || lg.PromptTokens != 3 || lg.CompletionTokens != 1 {
		t.Errorf("log = %+v", lg)
	}
}

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
	// chat 协议无 event: 行（仅 data:），事件名行只用于 messages/responses 客户端
	if strings.Contains(out, "event: ") {
		t.Errorf("chat 客户端不应带 event: 行: %s", out)
	}
	lg := logs.snapshot()[0]
	if lg.ForwardMode != model.ForwardConverted || !lg.Stream {
		t.Errorf("log = %+v", lg)
	}
	if lg.PromptTokens != 20 || lg.CompletionTokens != 5 {
		t.Errorf("usage: %+v", lg)
	}
}

func TestExecutorStreamConvertedToResponses(t *testing.T) {
	// 入站 responses 流式 -> chat/completions 上游（转换路径）：
	// 客户端应收到官方形态的事件流（含 event: 行）与严格校验所需字段。
	srv := openAIUpstream(t)
	channels := &memChannels{items: []model.Channel{
		ch("oa-conv", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
	}}
	channels.items[0].BaseURL = srv.URL

	logs := &memLogs{}
	exec := gateway.NewExecutor(channels, logs, eventbus.New(), 5*time.Second)

	c, w := ginCtx(t)
	body := `{"model":"gpt-4o","input":"hi","stream":true}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolResponses, body, true))

	out := w.Body.String()
	for _, want := range []string{
		"event: response.created\n", "event: response.output_item.added\n",
		"event: response.content_part.added\n", "event: response.output_text.delta\n",
		"event: response.output_text.done\n", "event: response.completed\n",
		`"delta":"he"`, `"delta":"llo"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("缺少 %q: %s", want, out)
		}
	}
	// 官方 SDK 严格校验的必填字段
	for _, want := range []string{`"created_at"`, `"parallel_tool_calls"`, `"tool_choice"`, `"tools"`, `"logprobs"`} {
		if !strings.Contains(out, want) {
			t.Errorf("缺少必填字段 %s: %s", want, out)
		}
	}
	// responses 流以终止事件收尾，无 [DONE]
	if strings.Contains(out, "[DONE]") {
		t.Errorf("responses 流不应含 [DONE]: %s", out)
	}
	lg := logs.snapshot()[0]
	if lg.ForwardMode != model.ForwardConverted || lg.PromptTokens != 9 || lg.CompletionTokens != 2 {
		t.Errorf("log = %+v", lg)
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

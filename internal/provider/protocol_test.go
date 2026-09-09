package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"xtokenhub/internal/model"
)

func TestEndpointPath(t *testing.T) {
	tests := []struct {
		p    model.Protocol
		want string
	}{
		{model.ProtocolChatCompletions, "chat/completions"},
		{model.ProtocolResponses, "responses"},
		{model.ProtocolMessages, "messages"},
		{model.Protocol("xxx"), ""},
	}
	for _, tt := range tests {
		if got := EndpointPath(tt.p); got != tt.want {
			t.Errorf("EndpointPath(%s) = %q, want %q", tt.p, got, tt.want)
		}
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"https://api.openai.com/v1", "https://api.openai.com", true},
		{"https://api.openai.com/v1/", "https://api.openai.com", true},
		{"https://api.openai.com", "https://api.openai.com", true},
		{"https://api.openai.com//", "https://api.openai.com", true},
		{"  https://api.openai.com/v1  ", "https://api.openai.com", true},
		{"https://api.deepseek.com/anthropic", "https://api.deepseek.com/anthropic", true},
		{"https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/paas/v4", true},
		{"http://localhost:8080/v1", "http://localhost:8080", true},
		{"", "", false},
		{"   ", "", false},
		{"//v1", "", false},        // 去 /v1 后为空
		{"ftp://x.com", "", false}, // 非 http(s)
		{"not a url", "", false},
		{"HTTPS://API.OPENAI.COM/V1", "HTTPS://API.OPENAI.COM", true}, // 大小写协议头
	}
	for _, tt := range tests {
		got, ok := NormalizeBaseURL(tt.in)
		if ok != tt.ok || got != tt.want {
			t.Errorf("NormalizeBaseURL(%q) = %q,%v want %q,%v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestEndpointURL(t *testing.T) {
	tests := []struct {
		base string
		p    model.Protocol
		want string
	}{
		// 普通根：自动补 /v1
		{"https://api.openai.com", model.ProtocolChatCompletions, "https://api.openai.com/v1/chat/completions"},
		// 带 /v1：去重不重复拼接
		{"https://api.openai.com/v1", model.ProtocolMessages, "https://api.openai.com/v1/messages"},
		// 版本化挂载点：直接拼资源路径
		{"https://open.bigmodel.cn/api/paas/v4", model.ProtocolChatCompletions, "https://open.bigmodel.cn/api/paas/v4/chat/completions"},
		{"https://open.bigmodel.cn/api/paas/v4", model.ProtocolResponses, "https://open.bigmodel.cn/api/paas/v4/responses"},
		// 挂载点 + /v1 结尾
		{"https://x.com/api/paas/v1", model.ProtocolChatCompletions, "https://x.com/api/paas/v1/chat/completions"},
		// v 后非纯数字：不视为版本段
		{"https://x.com/v1beta", model.ProtocolChatCompletions, "https://x.com/v1beta/v1/chat/completions"},
	}
	for _, tt := range tests {
		got, err := EndpointURL(tt.base, tt.p)
		if err != nil || got != tt.want {
			t.Errorf("EndpointURL(%q,%s) = %q,%v want %q", tt.base, tt.p, got, err, tt.want)
		}
	}
	// 非法 BaseURL
	if _, err := EndpointURL("bad", model.ProtocolChatCompletions); err == nil ||
		!strings.Contains(err.Error(), "非法 BaseURL") {
		t.Errorf("非法 BaseURL 应报错, got %v", err)
	}
	// 非法协议
	if _, err := EndpointURL("https://x.com", model.Protocol("xxx")); err == nil ||
		!strings.Contains(err.Error(), "不支持的协议") {
		t.Errorf("非法协议应报错, got %v", err)
	}
}

func TestInferProviderType(t *testing.T) {
	tests := []struct {
		in   string
		want model.ProviderType
	}{
		{"https://api.anthropic.com", model.ProviderAnthropic},
		{"https://api.deepseek.com/anthropic", model.ProviderAnthropic},
		{"https://x.com/api/ANTHROPIC/v1", model.ProviderAnthropic},
		{"https://api.openai.com/v1", model.ProviderOpenAICompatible},
		{"https://api.deepseek.com", model.ProviderOpenAICompatible},
		{"not a url", model.ProviderOpenAICompatible},
		{"", model.ProviderOpenAICompatible},
	}
	for _, tt := range tests {
		if got := InferProviderType(tt.in); got != tt.want {
			t.Errorf("InferProviderType(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestAPIHeaders(t *testing.T) {
	h := APIHeaders(model.ProviderAnthropic, "sk-a")
	if h["x-api-key"] != "sk-a" || h["anthropic-version"] != "2023-06-01" {
		t.Errorf("anthropic headers = %v", h)
	}
	h2 := APIHeaders(model.ProviderOpenAICompatible, "sk-b")
	if h2["Authorization"] != "Bearer sk-b" {
		t.Errorf("openai headers = %v", h2)
	}
	// 未知厂家类型走 OpenAI 兼容兜底
	h3 := APIHeaders(model.ProviderType("xxx"), "sk-c")
	if h3["Authorization"] != "Bearer sk-c" {
		t.Errorf("兜底 headers = %v", h3)
	}
}

func TestDefaultProbeModelAndBaseline(t *testing.T) {
	if DefaultProbeModel[model.ProviderOpenAICompatible] == "" ||
		DefaultProbeModel[model.ProviderAnthropic] == "" {
		t.Error("探测兜底模型不应为空")
	}
	if len(BaselineProtocols[model.ProviderOpenAICompatible]) != 1 ||
		BaselineProtocols[model.ProviderOpenAICompatible][0] != model.ProtocolChatCompletions {
		t.Errorf("openai 基线 = %v", BaselineProtocols[model.ProviderOpenAICompatible])
	}
	if len(BaselineProtocols[model.ProviderAnthropic]) != 1 ||
		BaselineProtocols[model.ProviderAnthropic][0] != model.ProtocolMessages {
		t.Errorf("anthropic 基线 = %v", BaselineProtocols[model.ProviderAnthropic])
	}
}

// ---------- 覆盖分支补充 ----------

func TestParseRequestUnknownProtocol(t *testing.T) {
	if _, _, err := ParseRequest(model.Protocol("xxx"), []byte(`{}`)); err == nil ||
		!strings.Contains(err.Error(), "不支持的协议") {
		t.Fatalf("got %v", err)
	}
}

func TestRawToStringsInvalid(t *testing.T) {
	// stop 为数字等非法形态：返回 nil 而非报错
	if got := rawToStrings([]byte(`123`)); got != nil {
		t.Errorf("rawToStrings(123) = %v", got)
	}
	if got := rawToStrings(nil); got != nil {
		t.Errorf("rawToStrings(nil) = %v", got)
	}
}

func TestWriteAnthropicUpstreamMinimal(t *testing.T) {
	// 无 system、无采样参数：最小请求体
	out, err := WriteUpstreamRequest(model.ProtocolMessages,
		Conversation{Messages: []Message{{Role: RoleUser, Text: "hi"}}}, ReqParams{}, "m")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "system") || strings.Contains(s, "temperature") || strings.Contains(s, "top_p") {
		t.Errorf("最小请求不应包含可选字段: %s", s)
	}
	if !strings.Contains(s, `"max_tokens":1024`) {
		t.Errorf("max_tokens 兜底缺失: %s", s)
	}
}

func TestStreamUpstreamFinishPassthrough(t *testing.T) {
	// 未知结束原因原样透传；tool_use 归一为 tool_calls
	c := &chatChunkConverter{parser: NewStreamParser(model.ProtocolMessages)}
	c.parser.Feed([]byte(`{"type":"message_delta","delta":{"stop_reason":"tool_use"}}`))
	if got := c.upstreamFinish(); got != "tool_calls" {
		t.Errorf("tool_use → %q", got)
	}
	c2 := &chatChunkConverter{parser: NewStreamParser(model.ProtocolMessages)}
	c2.parser.Feed([]byte(`{"type":"message_delta","delta":{"stop_reason":"pause_turn"}}`))
	if got := c2.upstreamFinish(); got != "pause_turn" {
		t.Errorf("未知原因应透传, got %q", got)
	}
	c3 := &chatChunkConverter{parser: NewStreamParser(model.ProtocolMessages)}
	c3.parser.Feed([]byte(`{"type":"message_delta","delta":{"stop_reason":"stop_sequence"}}`))
	if got := c3.upstreamFinish(); got != "stop" {
		t.Errorf("stop_sequence → %q", got)
	}
}

func TestChatConverterFinishBlockShape(t *testing.T) {
	// finish 块 delta 为空对象且带 finish_reason；无 finish 时 finish_reason 缺省
	c := &chatChunkConverter{parser: NewStreamParser(model.ProtocolChatCompletions), id: "i", model: "m"}
	blk := c.chunk("", nil, nil)
	var m map[string]any
	json.Unmarshal(blk, &m)
	ch := m["choices"].([]any)[0].(map[string]any)
	if _, has := ch["finish_reason"]; has {
		t.Errorf("无 finish 时不应带 finish_reason: %v", ch)
	}
	if _, has := ch["delta"]; !has {
		t.Errorf("delta 字段应始终存在")
	}
}

func TestUpstreamUsageResponsesInputDetails(t *testing.T) {
	// responses/messages usage 携带 input_tokens_details.cached_tokens
	u := upstreamUsage{InputTokens: i64(9), OutputTokens: i64(1),
		InputDetails: &struct {
			CachedTokens *int64 `json:"cached_tokens"`
		}{CachedTokens: i64(6)}}
	got := u.toUsage()
	if got.CachedTokens != 6 {
		t.Errorf("cached = %d, want 6", got.CachedTokens)
	}
}

func TestAnthropicStreamParserInvalidChunk(t *testing.T) {
	p := NewStreamParser(model.ProtocolMessages)
	if err := p.Feed([]byte(`{bad`)); err != nil {
		t.Errorf("非法块应静默跳过, got %v", err)
	}
	if p.Text() != "" {
		t.Errorf("text = %q", p.Text())
	}
}

func TestWriteAnthropicUpstreamTopP(t *testing.T) {
	topP := 0.5
	out, err := WriteUpstreamRequest(model.ProtocolMessages,
		Conversation{Messages: []Message{{Role: RoleUser, Text: "hi"}}},
		ReqParams{TopP: &topP}, "m")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"top_p":0.5`) {
		t.Errorf("top_p 缺失: %s", out)
	}
}

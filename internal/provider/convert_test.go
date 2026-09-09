package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"xtokenhub/internal/model"
)

// ---------- ValidateRequest ----------

func TestValidateRequest(t *testing.T) {
	tests := []struct {
		name    string
		p       model.Protocol
		body    string
		wantErr string
	}{
		{"openai 正常", model.ProtocolChatCompletions, `{"messages":[{"role":"user","content":"hi"}]}`, ""},
		{"openai 非法 JSON", model.ProtocolChatCompletions, `{bad`, "非法 JSON"},
		{"openai messages 空", model.ProtocolChatCompletions, `{"messages":[]}`, "messages 不能为空"},
		{"openai messages 缺失", model.ProtocolChatCompletions, `{}`, "messages 不能为空"},
		{"responses 正常(string)", model.ProtocolResponses, `{"input":"hi"}`, ""},
		{"responses 正常(items)", model.ProtocolResponses, `{"input":[{"role":"user","content":"hi"}]}`, ""},
		{"responses input 缺失", model.ProtocolResponses, `{}`, "input 不能为空"},
		{"responses 非法 JSON", model.ProtocolResponses, `{bad`, "input 不能为空"},
		{"anthropic 正常", model.ProtocolMessages, `{"messages":[{"role":"user","content":"hi"}],"max_tokens":10}`, ""},
		{"anthropic 缺 max_tokens", model.ProtocolMessages, `{"messages":[{"role":"user","content":"hi"}]}`, "max_tokens 为必填"},
		{"anthropic messages 空", model.ProtocolMessages, `{"messages":[],"max_tokens":1}`, "messages 不能为空"},
		{"anthropic 非法 JSON", model.ProtocolMessages, `{bad`, "非法 JSON"},
		{"未知协议", model.Protocol("xxx"), `{}`, "不支持的协议"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRequest(tt.p, []byte(tt.body))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("期望通过, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("期望错误含 %q, got %v", tt.wantErr, err)
			}
		})
	}
}

// ---------- contentToText ----------

func TestContentToText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"空", ``, ""},
		{"字符串", `"hello"`, "hello"},
		{"openai parts", `[{"type":"text","text":"a"},{"type":"text","text":"b"}]`, "a\nb"},
		{"parts 空文本被跳过", `[{"type":"text","text":""},{"type":"text","text":"b"}]`, "b"},
		{"anthropic blocks", `[{"type":"text","text":"你好"}]`, "你好"},
		{"非文本内容忽略", `[{"type":"image_url","image_url":{"url":"http://x"}}]`, ""},
		{"对象不识别", `{"foo":1}`, ""},
		{"数字不识别", `123`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contentToText(json.RawMessage(tt.in)); got != tt.want {
				t.Errorf("contentToText = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------- ParseRequest: openai chat ----------

func TestParseOpenAIChatRequest(t *testing.T) {
	body := `{
		"model":"gpt-4o",
		"messages":[
			{"role":"system","content":"be nice"},
			{"role":"developer","content":"be terse"},
			{"role":"system","content":"be safe"},
			{"role":"user","content":[{"type":"text","text":"hi"},{"type":"text","text":"there"}]},
			{"role":"assistant","content":"hello"},
			{"role":"tool","content":"tool output"}
		],
		"max_tokens":100,
		"temperature":0.5,
		"top_p":0.9,
		"stop":"END",
		"stream":true
	}`
	conv, params, err := ParseRequest(model.ProtocolChatCompletions, []byte(body))
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}
	if conv.System != "be nice\nbe terse\nbe safe" {
		t.Errorf("System = %q", conv.System)
	}
	if len(conv.Messages) != 2 {
		t.Fatalf("messages = %d", len(conv.Messages))
	}
	if conv.Messages[0].Role != RoleUser || conv.Messages[0].Text != "hi\nthere" {
		t.Errorf("msg0 = %+v", conv.Messages[0])
	}
	if conv.Messages[1].Role != RoleAssistant || conv.Messages[1].Text != "hello" {
		t.Errorf("msg1 = %+v", conv.Messages[1])
	}
	// tool 消息缺 tool_call_id 无法与调用配对，应跳过（不再按 user 归一）
	if params.Model != "gpt-4o" || params.MaxTokens != 100 || params.Stream != true {
		t.Errorf("params = %+v", params)
	}
	if params.Temperature == nil || *params.Temperature != 0.5 || params.TopP == nil || *params.TopP != 0.9 {
		t.Errorf("采样参数 = %+v", params)
	}
	if len(params.Stop) != 1 || params.Stop[0] != "END" {
		t.Errorf("stop = %v", params.Stop)
	}
}

func TestParseOpenAIChatRequestStopArray(t *testing.T) {
	_, params, err := ParseRequest(model.ProtocolChatCompletions,
		[]byte(`{"messages":[{"role":"user","content":"x"}],"stop":["a","b"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(params.Stop) != 2 || params.Stop[0] != "a" || params.Stop[1] != "b" {
		t.Errorf("stop = %v", params.Stop)
	}
}

func TestParseOpenAIChatRequestBadJSON(t *testing.T) {
	_, _, err := ParseRequest(model.ProtocolChatCompletions, []byte(`{bad`))
	if err == nil || !strings.Contains(err.Error(), "解析 openai 请求") {
		t.Fatalf("期望解析错误, got %v", err)
	}
}

// ---------- ParseRequest: responses ----------

func TestParseResponsesRequest(t *testing.T) {
	body := `{
		"model":"gpt-5",
		"instructions":"sys prompt",
		"input":[
			{"role":"user","content":"q1"},
			{"role":"assistant","content":[{"type":"output_text","text":"a1"}]},
			{"type":"message","role":"user","content":"q2"}
		],
		"max_output_tokens":64,
		"temperature":0.7,
		"top_p":0.8,
		"stream":true
	}`
	conv, params, err := ParseRequest(model.ProtocolResponses, []byte(body))
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}
	if conv.System != "sys prompt" {
		t.Errorf("System = %q", conv.System)
	}
	if len(conv.Messages) != 3 ||
		conv.Messages[0].Text != "q1" || conv.Messages[1].Text != "a1" || conv.Messages[2].Text != "q2" {
		t.Errorf("messages = %+v", conv.Messages)
	}
	if conv.Messages[1].Role != RoleAssistant {
		t.Errorf("msg1 role = %s", conv.Messages[1].Role)
	}
	if params.MaxTokens != 64 || !params.Stream {
		t.Errorf("params = %+v", params)
	}
}

func TestParseResponsesRequestInputString(t *testing.T) {
	conv, _, err := ParseRequest(model.ProtocolResponses, []byte(`{"input":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(conv.Messages) != 1 || conv.Messages[0].Role != RoleUser || conv.Messages[0].Text != "hello" {
		t.Errorf("conv = %+v", conv)
	}
}

func TestParseResponsesRequestErrors(t *testing.T) {
	if _, _, err := ParseRequest(model.ProtocolResponses, []byte(`{bad`)); err == nil ||
		!strings.Contains(err.Error(), "解析 responses 请求") {
		t.Fatalf("bad json: %v", err)
	}
	if _, _, err := ParseRequest(model.ProtocolResponses, []byte(`{"input":123}`)); err == nil ||
		!strings.Contains(err.Error(), "input 须为 string 或 items 数组") {
		t.Fatalf("bad input: %v", err)
	}
	if _, _, err := ParseRequest(model.ProtocolResponses, []byte(`{"input":[]}`)); err == nil ||
		!strings.Contains(err.Error(), "input 不能为空") {
		t.Fatalf("empty input: %v", err)
	}
}

// ---------- ParseRequest: anthropic ----------

func TestParseAnthropicRequest(t *testing.T) {
	body := `{
		"model":"claude-3-5-sonnet",
		"system":"be kind",
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":[{"type":"text","text":"hey"}]},
			{"role":"user","content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}
		],
		"max_tokens":256,
		"temperature":0.2,
		"top_p":0.3,
		"stop_sequences":["STOP"],
		"stream":true
	}`
	conv, params, err := ParseRequest(model.ProtocolMessages, []byte(body))
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}
	if conv.System != "be kind" {
		t.Errorf("System = %q", conv.System)
	}
	if len(conv.Messages) != 3 || conv.Messages[1].Role != RoleAssistant || conv.Messages[2].Text != "a\nb" {
		t.Errorf("messages = %+v", conv.Messages)
	}
	if params.MaxTokens != 256 || !params.Stream || len(params.Stop) != 1 {
		t.Errorf("params = %+v", params)
	}
}

func TestParseAnthropicRequestSystemVariants(t *testing.T) {
	// system 为字符串原始 JSON
	conv, _, err := ParseRequest(model.ProtocolMessages, []byte(`{"system":"plain","messages":[{"role":"user","content":"x"}],"max_tokens":1}`))
	if err != nil || conv.System != "plain" {
		t.Errorf("plain system: %v %q", err, conv.System)
	}
	// system 为空字符串：走 contentToText 兜底 unmarshal
	conv, _, err = ParseRequest(model.ProtocolMessages, []byte(`{"system":"","messages":[{"role":"user","content":"x"}],"max_tokens":1}`))
	if err != nil || conv.System != "" {
		t.Errorf("empty system: %v %q", err, conv.System)
	}
	// system 为 block 数组
	conv, _, err = ParseRequest(model.ProtocolMessages, []byte(`{"system":[{"type":"text","text":"sys"}],"messages":[{"role":"user","content":"x"}],"max_tokens":1}`))
	if err != nil || conv.System != "sys" {
		t.Errorf("block system: %v %q", err, conv.System)
	}
	// assistant 以外角色（含未知）归一为 user
	conv, _, err = ParseRequest(model.ProtocolMessages, []byte(`{"messages":[{"role":"weird","content":"x"}],"max_tokens":1}`))
	if err != nil || conv.Messages[0].Role != RoleUser {
		t.Errorf("weird role: %v %+v", err, conv.Messages)
	}
}

func TestParseAnthropicRequestBadJSON(t *testing.T) {
	_, _, err := ParseRequest(model.ProtocolMessages, []byte(`{bad`))
	if err == nil || !strings.Contains(err.Error(), "解析 anthropic 请求") {
		t.Fatalf("期望解析错误, got %v", err)
	}
}

// ---------- WriteUpstreamRequest ----------

func TestWriteUpstreamRequestOpenAI(t *testing.T) {
	temp, topP := 0.4, 0.9
	params := ReqParams{
		Model: "client-model", MaxTokens: 77, Temperature: &temp, TopP: &topP,
		Stop: []string{"END"}, Stream: true,
	}
	conv := Conversation{
		System: "sys",
		Messages: []Message{
			{Role: RoleUser, Text: "hi"},
			{Role: RoleAssistant, Text: "hello"},
		},
	}
	out, err := WriteUpstreamRequest(model.ProtocolChatCompletions, conv, params, "up-model")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "up-model" {
		t.Errorf("model = %v", got["model"])
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %d", len(msgs))
	}
	m0 := msgs[0].(map[string]any)
	if m0["role"] != "system" || m0["content"] != "sys" {
		t.Errorf("msg0 = %v", m0)
	}
	if got["max_tokens"] != float64(77) || got["temperature"] != 0.4 || got["top_p"] != 0.9 {
		t.Errorf("params = %v", got)
	}
	// 单个 stop 序列化为字符串
	if got["stop"] != "END" {
		t.Errorf("stop = %v", got["stop"])
	}
	if got["stream"] != true {
		t.Errorf("stream = %v", got["stream"])
	}
	so := got["stream_options"].(map[string]any)
	if so["include_usage"] != true {
		t.Errorf("stream_options = %v", so)
	}
}

func TestWriteUpstreamRequestOpenAIMultiStop(t *testing.T) {
	params := ReqParams{Stop: []string{"a", "b"}}
	out, err := WriteUpstreamRequest(model.ProtocolChatCompletions,
		Conversation{Messages: []Message{{Role: RoleUser, Text: "x"}}}, params, "m")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	json.Unmarshal(out, &got)
	if s, ok := got["stop"].([]any); !ok || len(s) != 2 {
		t.Errorf("stop = %v", got["stop"])
	}
	if _, ok := got["max_tokens"]; ok {
		t.Errorf("无 max_tokens 时不应输出")
	}
}

func TestWriteUpstreamRequestAnthropic(t *testing.T) {
	temp := 0.6
	params := ReqParams{MaxTokens: 0, Temperature: &temp, Stop: []string{"STOP"}, Stream: true}
	conv := Conversation{
		System:   "sys",
		Messages: []Message{{Role: RoleUser, Text: "hi"}, {Role: RoleAssistant, Text: "ok"}},
	}
	out, err := WriteUpstreamRequest(model.ProtocolMessages, conv, params, "up-claude")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	// anthropic max_tokens 必填：未提供时兜底 1024
	if got["max_tokens"] != float64(1024) {
		t.Errorf("max_tokens = %v", got["max_tokens"])
	}
	if got["system"] != "sys" || got["model"] != "up-claude" || got["stream"] != true {
		t.Errorf("out = %v", got)
	}
	if got["temperature"] != 0.6 {
		t.Errorf("temperature = %v", got["temperature"])
	}
	msgs := got["messages"].([]any)
	m0 := msgs[0].(map[string]any)
	content := m0["content"].([]any)
	blk := content[0].(map[string]any)
	if m0["role"] != "user" || blk["type"] != "text" || blk["text"] != "hi" {
		t.Errorf("msg0 = %v", m0)
	}
	if ss, ok := got["stop_sequences"].([]any); !ok || len(ss) != 1 || ss[0] != "STOP" {
		t.Errorf("stop_sequences = %v", got["stop_sequences"])
	}
}

func TestWriteUpstreamRequestResponsesUnsupported(t *testing.T) {
	// 上游为 responses：转换路径明确拒绝
	_, err := WriteUpstreamRequest(model.ProtocolResponses, Conversation{}, ReqParams{}, "m")
	if err == nil || !strings.Contains(err.Error(), "不支持 responses 上游") {
		t.Fatalf("期望 responses 上游错误, got %v", err)
	}
	// 未知协议
	_, err = WriteUpstreamRequest(model.Protocol("xxx"), Conversation{}, ReqParams{}, "m")
	if err == nil || !strings.Contains(err.Error(), "不支持的协议") {
		t.Fatalf("期望不支持协议错误, got %v", err)
	}
}

// ---------- ParseUpstreamResponse ----------

func TestParseUpstreamResponseOpenAI(t *testing.T) {
	body := `{
		"id":"chatcmpl-1","model":"gpt-4o",
		"choices":[{"message":{"role":"assistant","content":"answer"},"finish_reason":"length"}],
		"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,
			"prompt_tokens_details":{"cached_tokens":3}}
	}`
	r, err := ParseUpstreamResponse(model.ProtocolChatCompletions, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "chatcmpl-1" || r.Model != "gpt-4o" || r.Text != "answer" || r.FinishReason != "length" {
		t.Errorf("result = %+v", r)
	}
	if r.Usage.PromptTokens != 10 || r.Usage.CompletionTokens != 5 || r.Usage.TotalTokens != 15 || r.Usage.CachedTokens != 3 {
		t.Errorf("usage = %+v", r.Usage)
	}
	if r.Usage.Source != UsageFromUpstream {
		t.Errorf("source = %s", r.Usage.Source)
	}
}

func TestParseUpstreamResponseOpenAINoUsage(t *testing.T) {
	r, err := ParseUpstreamResponse(model.ProtocolChatCompletions,
		[]byte(`{"id":"x","choices":[{"message":{"content":"hi"},"finish_reason":"stop"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.Usage.PromptTokens != 0 || r.Usage.Source != UsageFromUpstream {
		t.Errorf("usage = %+v", r.Usage)
	}
	// choices 为空时不出错
	if _, err = ParseUpstreamResponse(model.ProtocolChatCompletions, []byte(`{"id":"x"}`)); err != nil {
		t.Errorf("空 choices: %v", err)
	}
}

func TestParseUpstreamResponseAnthropic(t *testing.T) {
	body := `{
		"id":"msg_1","model":"claude-3-5-sonnet",
		"content":[{"type":"text","text":"part1"},{"type":"text","text":"part2"},{"type":"tool_use","id":"t"}],
		"stop_reason":"max_tokens",
		"usage":{"input_tokens":20,"output_tokens":8,
			"cache_read_input_tokens":4,"cache_creation_input_tokens":2}
	}`
	r, err := ParseUpstreamResponse(model.ProtocolMessages, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "part1part2" {
		t.Errorf("text = %q", r.Text)
	}
	if r.FinishReason != "max_tokens" {
		t.Errorf("finish = %q", r.FinishReason)
	}
	if r.Usage.PromptTokens != 20 || r.Usage.CompletionTokens != 8 ||
		r.Usage.CachedTokens != 4 || r.Usage.CacheWriteTokens != 2 {
		t.Errorf("usage = %+v", r.Usage)
	}
}

func TestParseUpstreamResponseErrors(t *testing.T) {
	if _, err := ParseUpstreamResponse(model.ProtocolChatCompletions, []byte(`{bad`)); err == nil ||
		!strings.Contains(err.Error(), "解析上游 openai 响应") {
		t.Fatalf("openai bad json: %v", err)
	}
	if _, err := ParseUpstreamResponse(model.ProtocolMessages, []byte(`{bad`)); err == nil ||
		!strings.Contains(err.Error(), "解析上游 anthropic 响应") {
		t.Fatalf("anthropic bad json: %v", err)
	}
	if _, err := ParseUpstreamResponse(model.ProtocolResponses, []byte(`{}`)); err == nil ||
		!strings.Contains(err.Error(), "不支持的协议") {
		t.Fatalf("responses: %v", err)
	}
}

// ---------- ConvertRequest / ConvertResponse ----------

func TestConvertRequestAllDirections(t *testing.T) {
	openaiBody := `{"model":"m","messages":[{"role":"system","content":"s"},{"role":"user","content":"hi"}],"max_tokens":50}`
	anthropicBody := `{"model":"m","system":"s","messages":[{"role":"user","content":"hi"}],"max_tokens":50}`
	responsesBody := `{"model":"m","instructions":"s","input":"hi","max_output_tokens":50}`

	t.Run("openai→anthropic", func(t *testing.T) {
		out, err := ConvertRequest(model.ProtocolChatCompletions, model.ProtocolMessages, []byte(openaiBody), "up")
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		json.Unmarshal(out, &got)
		if got["max_tokens"] != float64(50) || got["system"] != "s" {
			t.Errorf("out = %v", got)
		}
	})
	t.Run("anthropic→openai", func(t *testing.T) {
		out, err := ConvertRequest(model.ProtocolMessages, model.ProtocolChatCompletions, []byte(anthropicBody), "up")
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		json.Unmarshal(out, &got)
		if got["max_tokens"] != float64(50) {
			t.Errorf("out = %v", got)
		}
	})
	t.Run("responses→anthropic", func(t *testing.T) {
		out, err := ConvertRequest(model.ProtocolResponses, model.ProtocolMessages, []byte(responsesBody), "up")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), `"max_tokens":50`) {
			t.Errorf("out = %s", out)
		}
	})
	t.Run("responses→openai", func(t *testing.T) {
		out, err := ConvertRequest(model.ProtocolResponses, model.ProtocolChatCompletions, []byte(responsesBody), "up")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), `"content":"s"`) {
			t.Errorf("out = %s", out)
		}
	})
	t.Run("同协议拒绝", func(t *testing.T) {
		if _, err := ConvertRequest(model.ProtocolChatCompletions, model.ProtocolChatCompletions, []byte(openaiBody), "up"); err == nil {
			t.Fatal("同协议应拒绝")
		}
	})
	t.Run("入站解析失败透传", func(t *testing.T) {
		if _, err := ConvertRequest(model.ProtocolMessages, model.ProtocolChatCompletions, []byte(`{bad`), "up"); err == nil {
			t.Fatal("解析失败应报错")
		}
	})
	t.Run("上游 responses 拒绝", func(t *testing.T) {
		if _, err := ConvertRequest(model.ProtocolMessages, model.ProtocolResponses, []byte(anthropicBody), "up"); err == nil {
			t.Fatal("responses 上游应拒绝")
		}
	})
}

func TestConvertResponse(t *testing.T) {
	upstream := `{"id":"cmpl-1","choices":[{"message":{"content":"ans"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
	out, usage, err := ConvertResponse(model.ProtocolMessages, model.ProtocolChatCompletions, []byte(upstream), "client-model")
	if err != nil {
		t.Fatal(err)
	}
	if usage.PromptTokens != 3 || usage.CompletionTokens != 2 {
		t.Errorf("usage = %+v", usage)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got["type"] != "message" || got["model"] != "client-model" {
		t.Errorf("out = %v", got)
	}
	if got["stop_reason"] != "end_turn" {
		t.Errorf("stop_reason = %v", got["stop_reason"])
	}
	if _, _, err := ConvertResponse(model.ProtocolMessages, model.ProtocolResponses, []byte(`{}`), "m"); err == nil {
		t.Fatal("responses 上游应报错")
	}
}

// ---------- WriteClientResponse ----------

func TestWriteClientResponseAllProtocols(t *testing.T) {
	r := Result{
		ID: "r1", Model: "m", Text: "answer", FinishReason: "length",
		Usage: Usage{PromptTokens: 7, CompletionTokens: 4, TotalTokens: 11, CachedTokens: 2, CacheWriteTokens: 1},
	}

	t.Run("chat/completions", func(t *testing.T) {
		out, err := WriteClientResponse(model.ProtocolChatCompletions, r)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		json.Unmarshal(out, &got)
		if got["object"] != "chat.completion" || got["id"] != "r1" {
			t.Errorf("out = %v", got)
		}
		// created 为官方 SDK 必填字段（缺失时客户端解析失败）
		if _, ok := got["created"]; !ok {
			t.Errorf("缺 created 必填字段: %v", got)
		}
		choice := got["choices"].([]any)[0].(map[string]any)
		if choice["finish_reason"] != "length" {
			t.Errorf("finish_reason = %v", choice["finish_reason"])
		}
		msg := choice["message"].(map[string]any)
		if msg["content"] != "answer" || msg["role"] != "assistant" {
			t.Errorf("message = %v", msg)
		}
		usage := got["usage"].(map[string]any)
		if usage["prompt_tokens"] != float64(7) || usage["completion_tokens"] != float64(4) || usage["total_tokens"] != float64(11) {
			t.Errorf("usage = %v", usage)
		}
		details := usage["prompt_tokens_details"].(map[string]any)
		if details["cached_tokens"] != float64(2) {
			t.Errorf("cached = %v", details["cached_tokens"])
		}
	})
	t.Run("messages", func(t *testing.T) {
		out, err := WriteClientResponse(model.ProtocolMessages, r)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		json.Unmarshal(out, &got)
		if got["type"] != "message" || got["role"] != "assistant" {
			t.Errorf("out = %v", got)
		}
		if got["stop_reason"] != "max_tokens" { // length → max_tokens
			t.Errorf("stop_reason = %v", got["stop_reason"])
		}
		content := got["content"].([]any)[0].(map[string]any)
		if content["type"] != "text" || content["text"] != "answer" {
			t.Errorf("content = %v", content)
		}
		usage := got["usage"].(map[string]any)
		if usage["input_tokens"] != float64(7) || usage["output_tokens"] != float64(4) ||
			usage["cache_read_input_tokens"] != float64(2) || usage["cache_creation_input_tokens"] != float64(1) {
			t.Errorf("usage = %v", usage)
		}
	})
	t.Run("responses", func(t *testing.T) {
		out, err := WriteClientResponse(model.ProtocolResponses, r)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		json.Unmarshal(out, &got)
		// r.FinishReason = length：官方语义为 incomplete（截断），非 completed
		if got["object"] != "response" || got["status"] != "incomplete" {
			t.Errorf("out = %v", got)
		}
		if d, ok := got["incomplete_details"].(map[string]any); !ok || d["reason"] != "max_output_tokens" {
			t.Errorf("incomplete_details = %v", got["incomplete_details"])
		}
		item := got["output"].([]any)[0].(map[string]any)
		part := item["content"].([]any)[0].(map[string]any)
		if item["type"] != "message" || part["type"] != "output_text" || part["text"] != "answer" {
			t.Errorf("output = %v", item)
		}
		if item["status"] != "incomplete" {
			t.Errorf("item status = %v, want incomplete", item["status"])
		}
		// 官方 SDK 严格校验的必填字段（缺失会导致客户端解析失败）
		for _, k := range []string{"created_at", "parallel_tool_calls", "tool_choice", "tools"} {
			if _, ok := got[k]; !ok {
				t.Errorf("缺必填字段 %s: %v", k, got)
			}
		}
		if item["id"] == nil {
			t.Errorf("output 条目缺 id: %v", item)
		}
		if _, ok := part["annotations"]; !ok {
			t.Errorf("content part 缺 annotations: %v", part)
		}
		usage := got["usage"].(map[string]any)
		if usage["input_tokens"] != float64(7) || usage["output_tokens"] != float64(4) || usage["total_tokens"] != float64(11) {
			t.Errorf("usage = %v", usage)
		}
		if _, ok := usage["output_tokens_details"]; !ok {
			t.Errorf("usage 缺 output_tokens_details: %v", usage)
		}
	})
	t.Run("未知协议", func(t *testing.T) {
		if _, err := WriteClientResponse(model.Protocol("xxx"), r); err == nil ||
			!strings.Contains(err.Error(), "不支持的协议") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestMapFinishAndMapStop(t *testing.T) {
	// 空 finish → stop / end_turn
	if mapFinish("") != "stop" {
		t.Errorf("mapFinish(\"\") = %s", mapFinish(""))
	}
	if mapFinish("tool_calls") != "tool_calls" {
		t.Errorf("mapFinish 原样透传失败")
	}
	// anthropic stop_reason 映射
	for in, want := range map[string]string{
		"length": "max_tokens", "max_tokens": "max_tokens", "": "end_turn",
		"end_turn": "end_turn", "stop_sequence": "end_turn", "tool_use": "tool_use",
		"tool_calls": "tool_use", "refusal": "end_turn",
	} {
		if got := mapStop(in); got != want {
			t.Errorf("mapStop(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---------- Conversation.FullText ----------

func TestConversationFullText(t *testing.T) {
	c := Conversation{
		System:   "sys",
		Messages: []Message{{Role: RoleUser, Text: "q"}, {Role: RoleAssistant, Text: "a"}},
	}
	if got := c.FullText(); got != "sys\nq\na\n" {
		t.Errorf("FullText = %q", got)
	}
	// 无 system
	c2 := Conversation{Messages: []Message{{Role: RoleUser, Text: "q"}}}
	if got := c2.FullText(); got != "q\n" {
		t.Errorf("FullText = %q", got)
	}
}

func TestParseRequestCarriesToolItems(t *testing.T) {
	// 工具调用/结果条目纳入中间表示，供跨协议转换
	conv, params, err := ParseRequest(model.ProtocolResponses, []byte(`{
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"run ls"}]},
			{"type":"function_call","name":"shell","arguments":"{\"cmd\":\"ls\"}","call_id":"c1"},
			{"type":"function_call_output","call_id":"c1","output":"a.txt"},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}
		],
		"tools":[{"type":"function","name":"shell","description":"run shell","parameters":{"type":"object"}}],
		"tool_choice":"auto"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(conv.Messages) != 4 {
		t.Fatalf("messages = %+v", conv.Messages)
	}
	m1, m2 := conv.Messages[1], conv.Messages[2]
	if m1.Role != RoleAssistant || len(m1.ToolCalls) != 1 ||
		m1.ToolCalls[0].ID != "c1" || m1.ToolCalls[0].Name != "shell" || m1.ToolCalls[0].Arguments != `{"cmd":"ls"}` {
		t.Fatalf("function_call = %+v", m1)
	}
	if m2.Role != RoleTool || m2.ToolCallID != "c1" || m2.Text != "a.txt" {
		t.Fatalf("function_call_output = %+v", m2)
	}
	if len(params.Tools) != 1 || params.Tools[0].Name != "shell" || params.ToolChoice.Mode != "auto" {
		t.Fatalf("tools = %+v choice = %+v", params.Tools, params.ToolChoice)
	}
	// 孤儿工具结果（缺 call_id）跳过；全部为非配对项时视为空输入
	if _, _, err := ParseRequest(model.ProtocolResponses, []byte(`{"input":[{"type":"function_call_output","output":"x"}]}`)); err == nil ||
		!strings.Contains(err.Error(), "input 不能为空") {
		t.Fatalf("got %v", err)
	}
	// chat 协议：空 tool 结果但带 tool_call_id 应保留（合法的空结果）
	conv2, _, err := ParseRequest(model.ProtocolChatCompletions, []byte(`{
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"tool","content":"","tool_call_id":"c1"},
			{"role":"assistant","content":"ok"}
		]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(conv2.Messages) != 3 || conv2.Messages[1].Role != RoleTool || conv2.Messages[1].ToolCallID != "c1" {
		t.Fatalf("messages = %+v", conv2.Messages)
	}
	// anthropic 块级 tool_use / tool_result
	conv3, params3, err := ParseRequest(model.ProtocolMessages, []byte(`{
		"model":"m","max_tokens":100,
		"messages":[
			{"role":"user","content":"run ls"},
			{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"shell","input":{"cmd":"ls"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"a.txt"}]}
		],
		"tools":[{"name":"shell","description":"run shell","input_schema":{"type":"object"}}],
		"tool_choice":{"type":"any"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(conv3.Messages) != 3 {
		t.Fatalf("messages = %+v", conv3.Messages)
	}
	if len(conv3.Messages[1].ToolCalls) != 1 || conv3.Messages[1].ToolCalls[0].ID != "t1" ||
		conv3.Messages[1].ToolCalls[0].Arguments != `{"cmd":"ls"}` {
		t.Fatalf("tool_use = %+v", conv3.Messages[1])
	}
	if conv3.Messages[2].Role != RoleTool || conv3.Messages[2].ToolCallID != "t1" || conv3.Messages[2].Text != "a.txt" {
		t.Fatalf("tool_result = %+v", conv3.Messages[2])
	}
	if len(params3.Tools) != 1 || string(params3.Tools[0].Parameters) != `{"type":"object"}` || params3.ToolChoice.Mode != "required" {
		t.Fatalf("tools = %+v choice = %+v", params3.Tools, params3.ToolChoice)
	}
}

func TestWriteClientResponseResponsesCompletedOnStop(t *testing.T) {
	// 正常结束（stop）应报 completed 且 incomplete_details 为 null
	out, err := WriteClientResponse(model.ProtocolResponses, Result{
		ID: "r1", Model: "m", Text: "ok", FinishReason: "stop",
		Usage: Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	json.Unmarshal(out, &got)
	if got["status"] != "completed" {
		t.Errorf("status = %v", got["status"])
	}
	if got["incomplete_details"] != nil {
		t.Errorf("incomplete_details 应为 null: %v", got["incomplete_details"])
	}
}

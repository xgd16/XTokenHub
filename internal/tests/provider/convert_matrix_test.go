package provider_test

// 三协议（chat_completions / responses / messages）互转的完整矩阵测试：
//   请求方向：in -> up 全组合 + 转换产物回解析的往返一致性 + 线格式细节；
//   响应方向：up -> in 全组合 + 客户端响应结构 + usage/结束原因映射；
//   流式方向：客户端协议 × 上游协议全组合的事件生命周期与 usage。

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"xtokenhub/internal/model"
	"xtokenhub/internal/provider"
)

// ---------- 通用小工具 ----------

func toObj(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("非法 JSON %s: %v", b, err)
	}
	return m
}

func toArr(t *testing.T, v any) []any {
	t.Helper()
	a, ok := v.([]any)
	if !ok {
		t.Fatalf("期望数组，实际: %v (%T)", v, v)
	}
	return a
}

func toMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("期望对象，实际: %v (%T)", v, v)
	}
	return m
}

// 三协议等价的标准请求：System="sys"、[user "hello", assistant "hi"]、
// MaxTokens=100、Temperature=0、TopP=0.9、Stop=["END"]（responses 无此参数）、Stream=true。
var canonicalRequests = map[model.Protocol]string{
	model.ProtocolChatCompletions: `{"model":"gpt-4o","messages":[` +
		`{"role":"system","content":"sys"},` +
		`{"role":"user","content":"hello"},` +
		`{"role":"assistant","content":"hi"}],` +
		`"max_tokens":100,"temperature":0,"top_p":0.9,"stop":["END"],"stream":true}`,
	model.ProtocolMessages: `{"model":"claude-3","system":"sys","messages":[` +
		`{"role":"user","content":[{"type":"text","text":"hello"}]},` +
		`{"role":"assistant","content":"hi"}],` +
		`"max_tokens":100,"temperature":0,"top_p":0.9,"stop_sequences":["END"],"stream":true}`,
	model.ProtocolResponses: `{"model":"gpt-4o","instructions":"sys","input":[` +
		`{"role":"user","content":"hello"},` +
		`{"role":"assistant","content":"hi"}],` +
		`"max_output_tokens":100,"temperature":0,"top_p":0.9,"stream":true}`,
}

// ---------- 请求方向：全组合 + 往返一致性 ----------

func TestConvertRequestRoundTripMatrix(t *testing.T) {
	upModel := map[model.Protocol]string{
		model.ProtocolChatCompletions: "gpt-up",
		model.ProtocolMessages:        "claude-up",
	}
	for _, in := range model.AllProtocols {
		convIn, paramsIn, err := provider.ParseRequest(in, []byte(canonicalRequests[in]))
		if err != nil {
			t.Fatalf("parse %s: %v", in, err)
		}
		for _, up := range []model.Protocol{model.ProtocolChatCompletions, model.ProtocolMessages} {
			if up == in {
				continue // 同协议组合在下方单独断言拒绝
			}
			body, err := provider.ConvertRequest(in, up, []byte(canonicalRequests[in]), upModel[up])
			if err != nil {
				t.Fatalf("%s -> %s: %v", in, up, err)
			}
			convUp, paramsUp, err := provider.ParseRequest(up, body)
			if err != nil {
				t.Fatalf("%s -> %s 产物不可回解析: %v\n%s", in, up, err, body)
			}
			if !reflect.DeepEqual(convIn, convUp) {
				t.Errorf("%s -> %s 会话往返不一致:\n want %+v\n got  %+v", in, up, convIn, convUp)
			}
			if paramsUp.Model != upModel[up] {
				t.Errorf("%s -> %s 模型名未替换: got %q", in, up, paramsUp.Model)
			}
			paramsIn.Model, paramsUp.Model = "", ""
			if !reflect.DeepEqual(paramsIn, paramsUp) {
				t.Errorf("%s -> %s 采样参数往返不一致:\n want %+v\n got  %+v", in, up, paramsIn, paramsUp)
			}
		}
		// responses 只能作为客户端协议：任何入站协议转 responses 上游都应报错
		if _, err := provider.ConvertRequest(in, model.ProtocolResponses, []byte(canonicalRequests[in]), "m"); err == nil {
			t.Errorf("%s -> responses 应报错（responses 上游仅支持原生透传）", in)
		}
		// 相同协议应走透传而非转换
		if _, err := provider.ConvertRequest(in, in, []byte(canonicalRequests[in]), "m"); err == nil {
			t.Errorf("%s -> %s 相同协议应拒绝转换", in, in)
		}
	}
}

func TestConvertRequestWireShapes(t *testing.T) {
	cc := canonicalRequests[model.ProtocolChatCompletions]
	ms := canonicalRequests[model.ProtocolMessages]
	rs := canonicalRequests[model.ProtocolResponses]

	// CC -> MS：system 单字段、stop_sequences、content 文本块
	b, err := provider.ConvertRequest(model.ProtocolChatCompletions, model.ProtocolMessages, []byte(cc), "claude-up")
	if err != nil {
		t.Fatal(err)
	}
	am := toObj(t, b)
	if am["system"] != "sys" {
		t.Errorf("system = %v", am["system"])
	}
	if stop := toArr(t, am["stop_sequences"]); len(stop) != 1 || stop[0] != "END" {
		t.Errorf("stop_sequences = %v", am["stop_sequences"])
	}
	if _, ok := am["stop"]; ok {
		t.Error("CC->MS 不应出现 openai 的 stop 字段")
	}
	msgs := toArr(t, am["messages"])
	if len(msgs) != 2 {
		t.Fatalf("messages 应为 2 条（system 单列）: %v", msgs)
	}
	first := toMap(t, msgs[0])
	content := toArr(t, first["content"])
	c0 := toMap(t, content[0])
	if first["role"] != "user" || c0["type"] != "text" || c0["text"] != "hello" {
		t.Errorf("anthropic 消息块: %v", first)
	}
	if am["temperature"] != float64(0) {
		t.Errorf("temperature=0 应保留（指针非 nil）: %v", am["temperature"])
	}
	if am["top_p"] != 0.9 || am["max_tokens"] != float64(100) || am["stream"] != true {
		t.Errorf("采样参数: %s", b)
	}

	// MS -> CC：system 变首条消息、单 stop 回写字符串、stream_options.include_usage
	b2, err := provider.ConvertRequest(model.ProtocolMessages, model.ProtocolChatCompletions, []byte(ms), "gpt-up")
	if err != nil {
		t.Fatal(err)
	}
	om := toObj(t, b2)
	msgs2 := toArr(t, om["messages"])
	if len(msgs2) != 3 {
		t.Fatalf("messages 应为 3 条: %v", msgs2)
	}
	if toMap(t, msgs2[0])["role"] != "system" || toMap(t, msgs2[0])["content"] != "sys" {
		t.Errorf("system 消息: %v", msgs2[0])
	}
	if toMap(t, msgs2[1])["content"] != "hello" {
		t.Errorf("内容应回写为纯字符串: %v", msgs2[1])
	}
	if om["stop"] != "END" {
		t.Errorf("单元素 stop 应回写为字符串（openai 惯例）: %v", om["stop"])
	}
	if so := toMap(t, om["stream_options"]); so["include_usage"] != true {
		t.Errorf("流式必须带 stream_options.include_usage: %s", b2)
	}
	if _, ok := om["system"]; ok {
		t.Error("MS->CC 不应出现 anthropic 的 system 字段")
	}

	// RS -> CC / MS：responses 无 stop 参数，产物不应带 stop 相关键
	b3, err := provider.ConvertRequest(model.ProtocolResponses, model.ProtocolChatCompletions, []byte(rs), "gpt-up")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := toObj(t, b3)["stop"]; ok {
		t.Error("RS->CC 不应出现 stop 字段")
	}
	b4, err := provider.ConvertRequest(model.ProtocolResponses, model.ProtocolMessages, []byte(rs), "claude-up")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := toObj(t, b4)["stop_sequences"]; ok {
		t.Error("RS->MS 不应出现 stop_sequences 字段")
	}

	// CC 缺 max_tokens -> MS 默认 1024（anthropic 必填）
	b5, err := provider.ConvertRequest(model.ProtocolChatCompletions, model.ProtocolMessages,
		[]byte(`{"model":"m","messages":[{"role":"user","content":"q"}]}`), "claude-up")
	if err != nil {
		t.Fatal(err)
	}
	if toObj(t, b5)["max_tokens"] != float64(1024) {
		t.Errorf("max_tokens 默认值: %s", b5)
	}

	// 无 system：两端都不应产生 system 载体
	noSysCC := `{"model":"m","messages":[{"role":"user","content":"q"}]}`
	b6, _ := provider.ConvertRequest(model.ProtocolChatCompletions, model.ProtocolMessages, []byte(noSysCC), "m")
	if _, ok := toObj(t, b6)["system"]; ok {
		t.Error("无 system 入站不应产生 system 字段")
	}
	noSysMS := `{"model":"m","messages":[{"role":"user","content":"q"}],"max_tokens":5}`
	b7, _ := provider.ConvertRequest(model.ProtocolMessages, model.ProtocolChatCompletions, []byte(noSysMS), "m")
	om7 := toObj(t, b7)
	if got := toArr(t, om7["messages"]); toMap(t, got[0])["role"] == "system" {
		t.Error("无 system 入站不应产生 system 消息")
	}

	// 多段 stop：数组形态双向保留
	multiCC := `{"model":"m","messages":[{"role":"user","content":"q"}],"stop":["A","B"]}`
	b8, _ := provider.ConvertRequest(model.ProtocolChatCompletions, model.ProtocolMessages, []byte(multiCC), "m")
	if stop := toArr(t, toObj(t, b8)["stop_sequences"]); len(stop) != 2 || stop[0] != "A" || stop[1] != "B" {
		t.Errorf("多段 stop: %s", b8)
	}
	multiMS := `{"model":"m","messages":[{"role":"user","content":"q"}],"max_tokens":5,"stop_sequences":["A","B"]}`
	b9, _ := provider.ConvertRequest(model.ProtocolMessages, model.ProtocolChatCompletions, []byte(multiMS), "m")
	if stop := toArr(t, toObj(t, b9)["stop"]); len(stop) != 2 || stop[0] != "A" || stop[1] != "B" {
		t.Errorf("多段 stop: %s", b9)
	}
}

func TestParseRequestRoleAndContentNormalization(t *testing.T) {
	// CC：system/developer 归并为 system（\n 连接），tool 消息缺 tool_call_id 跳过，
	// content parts 数组以 \n 拼接
	conv, _, err := provider.ParseRequest(model.ProtocolChatCompletions, []byte(
		`{"messages":[`+
			`{"role":"system","content":"a"},`+
			`{"role":"developer","content":"b"},`+
			`{"role":"tool","content":"x"},`+
			`{"role":"user","content":[{"type":"text","text":"p1"},{"type":"text","text":"p2"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if conv.System != "a\nb" {
		t.Errorf("system 合并: %q", conv.System)
	}
	if len(conv.Messages) != 1 ||
		conv.Messages[0].Role != provider.RoleUser || conv.Messages[0].Text != "p1\np2" {
		t.Errorf("角色/内容归一: %+v", conv.Messages)
	}

	// MS：system 支持块数组
	conv2, _, err := provider.ParseRequest(model.ProtocolMessages, []byte(
		`{"messages":[{"role":"user","content":"q"}],"max_tokens":1,`+
			`"system":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if conv2.System != "a\nb" {
		t.Errorf("system 块数组合并: %q", conv2.System)
	}

	// RS：未知角色归为 user；input 为对象/空数组时报错；
	// 缺 content 的 item 宽松解析为空文本消息（不报错）
	conv3, _, err := provider.ParseRequest(model.ProtocolResponses, []byte(
		`{"input":[{"role":"weirdo","content":"x"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(conv3.Messages) != 1 || conv3.Messages[0].Role != provider.RoleUser || conv3.Messages[0].Text != "x" {
		t.Errorf("RS 角色归一: %+v", conv3.Messages)
	}
	conv4, _, err := provider.ParseRequest(model.ProtocolResponses, []byte(`{"input":[{"role":"user"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(conv4.Messages) != 1 || conv4.Messages[0].Text != "" {
		t.Errorf("缺 content 应宽容解析为空文本: %+v", conv4.Messages)
	}
	for _, bad := range []string{`{"input":{"a":1}}`, `{"input":[]}`} {
		if _, err := provider.ConvertRequest(model.ProtocolResponses, model.ProtocolChatCompletions, []byte(bad), "m"); err == nil {
			t.Errorf("RS 非法 input 应报错: %s", bad)
		}
	}
}

// ---------- 响应方向：全组合 + 结构 + usage/结束原因映射 ----------

const upstreamOpenAIResp = `{"id":"chatcmpl-1","model":"gpt-up","choices":[` +
	`{"index":0,"message":{"role":"assistant","content":"你好"},"finish_reason":"length"}],` +
	`"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19,"prompt_tokens_details":{"cached_tokens":6}}}`

const upstreamAnthropicResp = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-up",` +
	`"content":[{"type":"text","text":"你"},{"type":"text","text":"好"}],` +
	`"stop_reason":"max_tokens",` +
	`"usage":{"input_tokens":33,"output_tokens":8,"cache_creation_input_tokens":4,"cache_read_input_tokens":11}}`

func assertClientResponseShape(t *testing.T, in model.Protocol, m map[string]any, wantText, wantID string, u provider.Usage, finish string) {
	t.Helper()
	if m["model"] != "client-model" {
		t.Errorf("[%s] 模型名应为客户端模型: %v", in, m["model"])
	}
	switch in {
	case model.ProtocolChatCompletions:
		if m["object"] != "chat.completion" || m["id"] != wantID {
			t.Errorf("[chat] 头部: %v", m)
		}
		choices := toArr(t, m["choices"])
		if len(choices) != 1 {
			t.Fatalf("[chat] choices: %v", choices)
		}
		ch := toMap(t, choices[0])
		if ch["index"] != float64(0) || ch["finish_reason"] != finish {
			t.Errorf("[chat] choice: %v", ch)
		}
		msg := toMap(t, ch["message"])
		if msg["role"] != "assistant" || msg["content"] != wantText {
			t.Errorf("[chat] message: %v", msg)
		}
		usage := toMap(t, m["usage"])
		if usage["prompt_tokens"] != float64(u.PromptTokens) ||
			usage["completion_tokens"] != float64(u.CompletionTokens) ||
			usage["total_tokens"] != float64(u.TotalTokens) {
			t.Errorf("[chat] usage: %v", usage)
		}
		if got := toMap(t, usage["prompt_tokens_details"])["cached_tokens"]; got != float64(u.CachedTokens) {
			t.Errorf("[chat] cached_tokens: %v", got)
		}
	case model.ProtocolMessages:
		if m["type"] != "message" || m["role"] != "assistant" || m["id"] != wantID {
			t.Errorf("[messages] 头部: %v", m)
		}
		blocks := toArr(t, m["content"])
		if len(blocks) != 1 {
			t.Fatalf("[messages] content 块: %v", blocks)
		}
		blk := toMap(t, blocks[0])
		if blk["type"] != "text" || blk["text"] != wantText {
			t.Errorf("[messages] content 块: %v", blk)
		}
		if m["stop_reason"] != finish {
			t.Errorf("[messages] stop_reason: %v", m["stop_reason"])
		}
		usage := toMap(t, m["usage"])
		if usage["input_tokens"] != float64(u.PromptTokens) ||
			usage["output_tokens"] != float64(u.CompletionTokens) ||
			usage["cache_creation_input_tokens"] != float64(u.CacheWriteTokens) ||
			usage["cache_read_input_tokens"] != float64(u.CachedTokens) {
			t.Errorf("[messages] usage: %v", usage)
		}
	case model.ProtocolResponses:
		// finish 为 length/max_tokens 时官方语义是 incomplete（截断），否则 completed
		wantStatus := "completed"
		if finish == "length" || finish == "max_tokens" {
			wantStatus = "incomplete"
		}
		if m["object"] != "response" || m["status"] != wantStatus || m["id"] != wantID {
			t.Errorf("[responses] 头部: %v", m)
		}
		output := toArr(t, m["output"])
		if len(output) != 1 {
			t.Fatalf("[responses] output: %v", output)
		}
		item := toMap(t, output[0])
		if item["type"] != "message" || item["role"] != "assistant" || item["status"] != wantStatus {
			t.Errorf("[responses] output item: %v", item)
		}
		parts := toArr(t, item["content"])
		part := toMap(t, parts[0])
		if part["type"] != "output_text" || part["text"] != wantText {
			t.Errorf("[responses] output_text: %v", part)
		}
		usage := toMap(t, m["usage"])
		if usage["input_tokens"] != float64(u.PromptTokens) ||
			usage["output_tokens"] != float64(u.CompletionTokens) ||
			usage["total_tokens"] != float64(u.TotalTokens) {
			t.Errorf("[responses] usage: %v", usage)
		}
		if got := toMap(t, usage["input_tokens_details"])["cached_tokens"]; got != float64(u.CachedTokens) {
			t.Errorf("[responses] cached_tokens: %v", got)
		}
	}
}

func TestConvertResponseFullMatrix(t *testing.T) {
	wantUsage := map[model.Protocol]provider.Usage{
		model.ProtocolChatCompletions: {PromptTokens: 12, CompletionTokens: 7, TotalTokens: 19, CachedTokens: 6, Source: provider.UsageFromUpstream},
		model.ProtocolMessages:        {PromptTokens: 33, CompletionTokens: 8, TotalTokens: 41, CachedTokens: 11, CacheWriteTokens: 4, Source: provider.UsageFromUpstream},
	}
	upstream := map[model.Protocol][]byte{
		model.ProtocolChatCompletions: []byte(upstreamOpenAIResp),
		model.ProtocolMessages:        []byte(upstreamAnthropicResp),
	}
	wantID := map[model.Protocol]string{
		model.ProtocolChatCompletions: "chatcmpl-1",
		model.ProtocolMessages:        "msg_1",
	}
	finish := map[[2]model.Protocol]string{
		{model.ProtocolChatCompletions, model.ProtocolChatCompletions}: "length",
		{model.ProtocolMessages, model.ProtocolChatCompletions}:        "max_tokens", // length -> anthropic stop_reason
		{model.ProtocolResponses, model.ProtocolChatCompletions}:       "length",
		{model.ProtocolChatCompletions, model.ProtocolMessages}:        "max_tokens",
		{model.ProtocolMessages, model.ProtocolMessages}:               "max_tokens",
		{model.ProtocolResponses, model.ProtocolMessages}:              "max_tokens",
	}
	for _, up := range []model.Protocol{model.ProtocolChatCompletions, model.ProtocolMessages} {
		for _, in := range model.AllProtocols {
			body, usage, err := provider.ConvertResponse(in, up, upstream[up], "client-model")
			if err != nil {
				t.Fatalf("%s 上游 -> %s 客户端: %v", up, in, err)
			}
			if usage != wantUsage[up] {
				t.Errorf("%s 上游 -> %s 客户端 usage = %+v", up, in, usage)
			}
			assertClientResponseShape(t, in, toObj(t, body), "你好", wantID[up], wantUsage[up], finish[[2]model.Protocol{in, up}])
		}
	}
	// responses 不能作为上游被解析（仅支持原生透传）
	if _, _, err := provider.ConvertResponse(model.ProtocolChatCompletions, model.ProtocolResponses, []byte(`{}`), "m"); err == nil {
		t.Error("responses 上游解析应报错")
	}
	// 非法 JSON 上游体
	for _, up := range []model.Protocol{model.ProtocolChatCompletions, model.ProtocolMessages} {
		if _, _, err := provider.ConvertResponse(model.ProtocolChatCompletions, up, []byte(`not-json`), "m"); err == nil {
			t.Errorf("%s 上游非法 JSON 应报错", up)
		}
	}
}

func TestConvertResponseFinishReasonMapping(t *testing.T) {
	cases := []struct {
		up     model.Protocol
		reason string
		in     model.Protocol
		want   string // chat 客户端的 finish_reason / messages 客户端的 stop_reason
	}{
		{model.ProtocolChatCompletions, "length", model.ProtocolChatCompletions, "length"},
		{model.ProtocolChatCompletions, "length", model.ProtocolMessages, "max_tokens"},
		{model.ProtocolChatCompletions, "", model.ProtocolChatCompletions, "stop"},
		{model.ProtocolChatCompletions, "", model.ProtocolMessages, "end_turn"},
		{model.ProtocolChatCompletions, "tool_calls", model.ProtocolMessages, "tool_use"},
		{model.ProtocolMessages, "max_tokens", model.ProtocolChatCompletions, "max_tokens"},
		{model.ProtocolMessages, "max_tokens", model.ProtocolMessages, "max_tokens"},
		{model.ProtocolMessages, "end_turn", model.ProtocolChatCompletions, "stop"},
		{model.ProtocolMessages, "stop_sequence", model.ProtocolChatCompletions, "stop"},
		{model.ProtocolMessages, "stop_sequence", model.ProtocolMessages, "end_turn"},
		{model.ProtocolMessages, "", model.ProtocolChatCompletions, "stop"},
		{model.ProtocolMessages, "", model.ProtocolMessages, "end_turn"},
	}
	for _, tc := range cases {
		var upstream []byte
		if tc.up == model.ProtocolChatCompletions {
			upstream = mustJSON(t, map[string]any{
				"choices": []map[string]any{{
					"message":       map[string]any{"role": "assistant", "content": "x"},
					"finish_reason": tc.reason,
				}},
			})
		} else {
			upstream = mustJSON(t, map[string]any{
				"content":     []map[string]any{{"type": "text", "text": "x"}},
				"stop_reason": tc.reason,
			})
		}
		body, _, err := provider.ConvertResponse(tc.in, tc.up, upstream, "m")
		if err != nil {
			t.Fatalf("%s/%s -> %s: %v", tc.up, tc.reason, tc.in, err)
		}
		om := toObj(t, body)
		var got any
		if tc.in == model.ProtocolChatCompletions {
			got = toMap(t, toArr(t, om["choices"])[0])["finish_reason"]
		} else {
			got = om["stop_reason"]
		}
		if got != tc.want {
			t.Errorf("%s 上游 %q -> %s 客户端: got %v want %q", tc.up, tc.reason, tc.in, got, tc.want)
		}
	}
}

func TestConvertResponseUsageAndEdgeMapping(t *testing.T) {
	// anthropic 缓存字段 -> chat / responses 客户端的 cached_tokens
	up := mustJSON(t, map[string]any{
		"content": []map[string]any{{"type": "text", "text": "ok"}},
		"usage":   map[string]any{"input_tokens": 30, "output_tokens": 5, "cache_creation_input_tokens": 7, "cache_read_input_tokens": 9},
	})
	body, u, err := provider.ConvertResponse(model.ProtocolChatCompletions, model.ProtocolMessages, up, "m")
	if err != nil {
		t.Fatal(err)
	}
	if u.PromptTokens != 30 || u.CompletionTokens != 5 || u.TotalTokens != 35 || u.CachedTokens != 9 || u.CacheWriteTokens != 7 {
		t.Errorf("usage = %+v", u)
	}
	if got := toMap(t, toObj(t, body)["usage"])["prompt_tokens_details"]; got == nil {
		t.Error("chat 客户端应带 prompt_tokens_details")
	}
	body2, _, err := provider.ConvertResponse(model.ProtocolResponses, model.ProtocolMessages, up, "m")
	if err != nil {
		t.Fatal(err)
	}
	details := toMap(t, toObj(t, body2)["usage"])["input_tokens_details"]
	if toMap(t, details)["cached_tokens"] != float64(9) {
		t.Errorf("responses 客户端 cached_tokens: %v", details)
	}

	// 上游未报告 usage：全部为零值，客户端 usage 结构仍完整
	upNoUsage := mustJSON(t, map[string]any{
		"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "x"}, "finish_reason": "stop"}},
	})
	body3, u3, err := provider.ConvertResponse(model.ProtocolMessages, model.ProtocolChatCompletions, upNoUsage, "m")
	if err != nil {
		t.Fatal(err)
	}
	if u3.PromptTokens != 0 || u3.CompletionTokens != 0 || u3.TotalTokens != 0 || u3.CachedTokens != 0 {
		t.Errorf("无 usage 应为零值: %+v", u3)
	}
	usage3 := toMap(t, toObj(t, body3)["usage"])
	if usage3["input_tokens"] != float64(0) || usage3["output_tokens"] != float64(0) {
		t.Errorf("客户端 usage 应为零值结构: %v", usage3)
	}

	// 空 choices：文本为空、不 panic
	upEmpty := mustJSON(t, map[string]any{"id": "e", "choices": []any{}})
	body4, _, err := provider.ConvertResponse(model.ProtocolMessages, model.ProtocolChatCompletions, upEmpty, "m")
	if err != nil {
		t.Fatal(err)
	}
	blocks := toArr(t, toObj(t, body4)["content"])
	if toMap(t, blocks[0])["text"] != "" {
		t.Errorf("空 choices 文本应为空: %v", blocks)
	}
}

// ---------- 流式方向：全组合 + 事件生命周期 ----------

var upChatStreamEvents = []string{
	`{"id":"cc-1","model":"gpt-x","choices":[{"index":0,"delta":{"role":"assistant","content":"你"}}]}`,
	`{"id":"cc-1","model":"gpt-x","choices":[{"index":0,"delta":{"content":"好"}}]}`,
	`{"id":"cc-1","model":"gpt-x","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	`{"id":"cc-1","usage":{"prompt_tokens":11,"completion_tokens":2,"total_tokens":13,"prompt_tokens_details":{"cached_tokens":3}}}`,
}

var upAnthropicStreamEvents = []string{
	`{"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":11,"cache_read_input_tokens":3,"cache_creation_input_tokens":1}}}`,
	`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"你"}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"好"}}`,
	`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
	`{"type":"message_stop"}`,
}

// assertChatClientStream 校验 chat/completions 客户端的全流：
// feed 阶段只有内容增量；收尾固定 [finish 块, usage 块, [DONE]]。
func assertChatClientStream(t *testing.T, feed, done [][]byte, wantText, wantID, wantModel string, u provider.Usage) {
	t.Helper()
	all := append(append([][]byte{}, feed...), done...)
	var text strings.Builder
	for _, c := range all {
		if string(c) == "[DONE]" {
			continue
		}
		m := toObj(t, c)
		if m["object"] != "chat.completion.chunk" {
			t.Fatalf("非 chat chunk: %s", c)
		}
		if m["id"] != wantID || m["model"] != wantModel {
			t.Errorf("chunk 头部 id=%v model=%v, want %q/%q", m["id"], m["model"], wantID, wantModel)
		}
		// created 为官方 SDK 必填字段（缺失时客户端解析失败）
		if _, ok := m["created"]; !ok {
			t.Errorf("chunk 缺 created 必填字段: %v", m)
		}
		ch := toMap(t, toArr(t, m["choices"])[0])
		delta := toMap(t, ch["delta"])
		if s, ok := delta["content"]; ok {
			if s == "" {
				t.Error("不应发出空 content 增量")
			}
			text.WriteString(s.(string))
		}
	}
	if text.String() != wantText {
		t.Errorf("流式文本 = %q, want %q", text.String(), wantText)
	}
	// feed 阶段不应有 finish/usage
	for _, c := range feed {
		m := toObj(t, c)
		if _, ok := m["usage"]; ok {
			t.Error("feed 阶段不应出现 usage 块")
		}
		if ch := toMap(t, toArr(t, m["choices"])[0]); ch["finish_reason"] != nil {
			t.Error("feed 阶段不应出现 finish_reason")
		}
	}
	// 收尾三段式
	if len(done) != 3 || string(done[2]) != "[DONE]" {
		t.Fatalf("收尾应为 [finish, usage, [DONE]]: %v", done)
	}
	finishChunk := toObj(t, done[0])
	ch := toMap(t, toArr(t, finishChunk["choices"])[0])
	if ch["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v", ch["finish_reason"])
	}
	usageChunk := toObj(t, done[1])
	usage := toMap(t, usageChunk["usage"])
	if usage["prompt_tokens"] != float64(u.PromptTokens) ||
		usage["completion_tokens"] != float64(u.CompletionTokens) ||
		usage["total_tokens"] != float64(u.TotalTokens) {
		t.Errorf("usage 块: %v (want %+v)", usage, u)
	}
	if got := toMap(t, usage["prompt_tokens_details"])["cached_tokens"]; got != float64(u.CachedTokens) {
		t.Errorf("cached_tokens: %v", got)
	}
}

// assertAnthropicClientStream 校验 messages 客户端的全流：
// message_start -> content_block_start -> text_delta… -> [content_block_stop, message_delta, message_stop]。
func assertAnthropicClientStream(t *testing.T, feed, done [][]byte, wantText string, u provider.Usage) {
	t.Helper()
	var text strings.Builder
	for i, c := range feed {
		m := toObj(t, c)
		typ, _ := m["type"].(string)
		switch typ {
		case "message_start":
			if i != 0 {
				t.Error("message_start 必须是首个事件")
			}
			msg := toMap(t, m["message"])
			if msg["type"] != "message" || msg["role"] != "assistant" {
				t.Errorf("message_start: %v", msg)
			}
			// 官方 SDK 严格校验 message.id/model/usage 为必填
			for _, k := range []string{"id", "model", "usage"} {
				if _, ok := msg[k]; !ok {
					t.Errorf("message_start.message 缺必填字段 %s: %v", k, msg)
				}
			}
			if msg["id"] == "" {
				t.Errorf("message_start.message.id 不应为空: %v", msg)
			}
		case "content_block_start":
			blk := toMap(t, m["content_block"])
			if m["index"] != float64(0) || blk["type"] != "text" {
				t.Errorf("content_block_start: %v", m)
			}
		case "content_block_delta":
			if m["index"] != float64(0) {
				t.Errorf("delta index: %v", m["index"])
			}
			d := toMap(t, m["delta"])
			if d["type"] != "text_delta" {
				t.Errorf("delta 类型: %v", d)
			}
			text.WriteString(d["text"].(string))
		default:
			t.Errorf("feed 阶段不应出现 %s", typ)
		}
	}
	if text.String() != wantText {
		t.Errorf("流式文本 = %q, want %q", text.String(), wantText)
	}
	// 收尾三段式
	wantDone := []string{"content_block_stop", "message_delta", "message_stop"}
	if len(done) != 3 {
		t.Fatalf("收尾事件数: %d (%v)", len(done), done)
	}
	for i, c := range done {
		m := toObj(t, c)
		if m["type"] != wantDone[i] {
			t.Errorf("收尾事件 %d: got %v want %s", i, m["type"], wantDone[i])
		}
	}
	md := toObj(t, done[1])
	mu := toMap(t, md["usage"])
	if got := mu["output_tokens"]; got != float64(u.CompletionTokens) {
		t.Errorf("message_delta output_tokens = %v, want %d", got, u.CompletionTokens)
	}
	// chat 上游的 prompt_tokens 流末才到：message_start 已发出，须在 message_delta 补报
	if got := mu["input_tokens"]; got != float64(u.PromptTokens) {
		t.Errorf("message_delta input_tokens = %v, want %d", got, u.PromptTokens)
	}
	if got := toMap(t, md["delta"])["stop_reason"]; got != "end_turn" {
		t.Errorf("stop_reason = %v", got)
	}
}

// assertResponsesClientStream 校验 responses 客户端的全流：
// 事件顺序、sequence_number 连续递增、item_id 归属、completed 携带 usage，
// 以及官方 SDK 严格校验的必填字段（created_at/parallel_tool_calls/tool_choice/tools、
// delta 的 logprobs、usage 明细）。
func assertResponsesClientStream(t *testing.T, feed, done [][]byte, wantText, wantID, wantModel string, u provider.Usage) {
	t.Helper()
	all := append(append([][]byte{}, feed...), done...)
	var types []string
	var text strings.Builder
	prevSeq := int64(-1)
	for _, c := range all {
		m := toObj(t, c)
		typ, _ := m["type"].(string)
		types = append(types, typ)
		seq, ok := m["sequence_number"].(float64)
		if !ok || int64(seq) != prevSeq+1 {
			t.Fatalf("sequence_number 不连续: %v", types)
		}
		prevSeq = int64(seq)
		switch typ {
		case "response.created", "response.in_progress":
			resp := toMap(t, m["response"])
			if resp["status"] != "in_progress" || resp["object"] != "response" {
				t.Errorf("%s response: %v", typ, resp)
			}
			if resp["model"] != wantModel {
				t.Errorf("response model = %v, want %q", resp["model"], wantModel)
			}
			for _, k := range []string{"created_at", "parallel_tool_calls", "tool_choice", "tools"} {
				if _, ok := resp[k]; !ok {
					t.Errorf("%s response 缺必填字段 %s", typ, k)
				}
			}
			if got := toArr(t, resp["output"]); len(got) != 0 {
				t.Errorf("初始 output 应为空: %v", got)
			}
		case "response.output_item.added":
			if m["output_index"] != float64(0) {
				t.Errorf("output_index: %v", m["output_index"])
			}
			item := toMap(t, m["item"])
			if item["id"] != "msg_0" || item["type"] != "message" || item["role"] != "assistant" || item["status"] != "in_progress" {
				t.Errorf("output_item.added: %v", item)
			}
			if got := toArr(t, item["content"]); len(got) != 0 {
				t.Errorf("item 初始 content 应为空: %v", got)
			}
		case "response.content_part.added":
			if m["item_id"] != "msg_0" || m["output_index"] != float64(0) || m["content_index"] != float64(0) {
				t.Errorf("content_part.added 归属: %v", m)
			}
			part := toMap(t, m["part"])
			if part["type"] != "output_text" || part["text"] != "" {
				t.Errorf("part.added: %v", part)
			}
		case "response.output_text.delta":
			if m["item_id"] != "msg_0" {
				t.Errorf("delta item_id: %v", m["item_id"])
			}
			if _, ok := m["logprobs"]; !ok {
				t.Errorf("delta 缺 logprobs 必填字段: %v", m)
			}
			text.WriteString(m["delta"].(string))
		case "response.output_text.done":
			if m["text"] != wantText {
				t.Errorf("output_text.done 应携带全量原文: %v vs %q", m["text"], wantText)
			}
			if _, ok := m["logprobs"]; !ok {
				t.Errorf("output_text.done 缺 logprobs 必填字段: %v", m)
			}
		case "response.content_part.done":
			part := toMap(t, m["part"])
			if part["type"] != "output_text" || part["text"] != wantText {
				t.Errorf("content_part.done: %v", part)
			}
		case "response.output_item.done":
			item := toMap(t, m["item"])
			if item["id"] != "msg_0" || item["status"] != "completed" {
				t.Errorf("output_item.done: %v", item)
			}
			part := toMap(t, toArr(t, item["content"])[0])
			if part["type"] != "output_text" || part["text"] != wantText {
				t.Errorf("item 完成文本: %v", part)
			}
		case "response.completed":
			resp := toMap(t, m["response"])
			if resp["id"] != wantID || resp["status"] != "completed" || resp["object"] != "response" {
				t.Errorf("completed response: %v", resp)
			}
			output := toArr(t, resp["output"])
			if toMap(t, output[0])["id"] != "msg_0" {
				t.Errorf("completed output: %v", output)
			}
			usage := toMap(t, resp["usage"])
			if usage["input_tokens"] != float64(u.PromptTokens) ||
				usage["output_tokens"] != float64(u.CompletionTokens) ||
				usage["total_tokens"] != float64(u.TotalTokens) {
				t.Errorf("completed usage: %v (want %+v)", usage, u)
			}
			details := toMap(t, usage["input_tokens_details"])
			if details["cached_tokens"] != float64(u.CachedTokens) {
				t.Errorf("completed cached_tokens: %v", details)
			}
			if _, ok := details["cache_write_tokens"]; !ok {
				t.Errorf("usage 缺 cache_write_tokens 必填字段: %v", details)
			}
			if _, ok := usage["output_tokens_details"]; !ok {
				t.Errorf("usage 缺 output_tokens_details 必填字段: %v", usage)
			}
		default:
			t.Errorf("未知事件类型 %s", typ)
		}
	}
	if text.String() != wantText {
		t.Errorf("流式文本 = %q, want %q", text.String(), wantText)
	}
	// feed 前缀固定：created -> in_progress -> output_item.added
	prefix := []string{"response.created", "response.in_progress", "response.output_item.added"}
	if len(types) < len(prefix) || !reflect.DeepEqual(types[:len(prefix)], prefix) {
		t.Fatalf("feed 前缀错误: %v", types)
	}
	if wantText != "" && types[len(prefix)] != "response.content_part.added" {
		t.Errorf("delta 前必须有 content_part.added: %v", types)
	}
	// 收尾序列
	var wantDone []string
	if wantText != "" {
		wantDone = []string{"response.output_text.done", "response.content_part.done", "response.output_item.done", "response.completed"}
	} else {
		wantDone = []string{"response.output_item.done", "response.completed"}
	}
	gotDone := types[len(types)-len(wantDone):]
	if !reflect.DeepEqual(gotDone, wantDone) {
		t.Errorf("收尾序列: got %v want %v", gotDone, wantDone)
	}
}

func TestStreamConvertFullMatrix(t *testing.T) {
	type upstreamCase struct {
		events []string
		id     string
		model  string
		usage  provider.Usage
	}
	upstreams := map[model.Protocol]upstreamCase{
		model.ProtocolChatCompletions: {
			events: upChatStreamEvents, id: "cc-1", model: "gpt-x",
			usage: provider.Usage{PromptTokens: 11, CompletionTokens: 2, TotalTokens: 13, CachedTokens: 3, Source: provider.UsageFromUpstream},
		},
		model.ProtocolMessages: {
			events: upAnthropicStreamEvents, id: "msg_1",
			usage: provider.Usage{PromptTokens: 11, CompletionTokens: 2, TotalTokens: 13, CachedTokens: 3, CacheWriteTokens: 1, Source: provider.UsageFromUpstream},
		},
	}
	for _, up := range []model.Protocol{model.ProtocolChatCompletions, model.ProtocolMessages} {
		src := upstreams[up]
		for _, in := range []model.Protocol{model.ProtocolChatCompletions, model.ProtocolMessages, model.ProtocolResponses} {
			if in == up {
				if _, err := provider.NewStreamConverter(in, up); err == nil {
					t.Errorf("%s -> %s 相同协议应拒绝构造（走透传）", in, up)
				}
				continue
			}
			conv, err := provider.NewStreamConverter(in, up)
			if err != nil {
				t.Fatalf("%s <- %s: %v", in, up, err)
			}
			feed, err := feedAll(t, conv, src.events)
			if err != nil {
				t.Fatalf("%s <- %s feed: %v", in, up, err)
			}
			done, usage := conv.Finalize()
			if usage != src.usage {
				t.Errorf("%s <- %s usage = %+v, want %+v", in, up, usage, src.usage)
			}
			if conv.Text() != "你好" {
				t.Errorf("%s <- %s 累积文本 = %q", in, up, conv.Text())
			}
			switch in {
			case model.ProtocolChatCompletions:
				assertChatClientStream(t, feed, done, "你好", src.id, src.model, src.usage)
			case model.ProtocolMessages:
				assertAnthropicClientStream(t, feed, done, "你好", src.usage)
			case model.ProtocolResponses:
				wantID := src.id
				if wantID == "" {
					wantID = "resp_0"
				}
				assertResponsesClientStream(t, feed, done, "你好", wantID, src.model, src.usage)
			}
		}
	}
}

func TestStreamConvertEstimateAndZeroText(t *testing.T) {
	// 上游只报告 prompt（缺 output_tokens）：completion 走本地估算，来源标记 estimate
	conv, err := provider.NewStreamConverter(model.ProtocolMessages, model.ProtocolChatCompletions)
	if err != nil {
		t.Fatal(err)
	}
	feed, err := feedAll(t, conv, []string{
		`{"id":"c","choices":[{"delta":{"content":"hello world"}}]}`,
		`{"id":"c","usage":{"prompt_tokens":7}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	done, usage := conv.Finalize()
	if usage.Source != provider.UsageFromEstimate {
		t.Errorf("应标记估算来源: %+v", usage)
	}
	if usage.PromptTokens != 7 || usage.CompletionTokens <= 0 {
		t.Errorf("usage = %+v", usage)
	}
	assertAnthropicClientStream(t, feed, done, "hello world", usage)

	// 零文本 + 上游已报告 usage：usage 原样保留，不引入估算
	conv2, err := provider.NewStreamConverter(model.ProtocolChatCompletions, model.ProtocolMessages)
	if err != nil {
		t.Fatal(err)
	}
	feed2, err := feedAll(t, conv2, []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":5}}}`,
		`{"type":"message_stop"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	done2, usage2 := conv2.Finalize()
	if usage2.Source != provider.UsageFromUpstream || usage2.PromptTokens != 5 || usage2.CompletionTokens != 0 {
		t.Errorf("零文本 usage = %+v", usage2)
	}
	assertChatClientStream(t, feed2, done2, "", "", "", usage2)

	// 零文本 + 上游未报告 usage：纯估算兜底，全部为零
	conv3, err := provider.NewStreamConverter(model.ProtocolMessages, model.ProtocolChatCompletions)
	if err != nil {
		t.Fatal(err)
	}
	feed3, err := feedAll(t, conv3, []string{`{"id":"c","choices":[{"delta":{}}]}`})
	if err != nil {
		t.Fatal(err)
	}
	done3, usage3 := conv3.Finalize()
	if usage3.Source != provider.UsageFromEstimate || usage3.PromptTokens != 0 || usage3.CompletionTokens != 0 || usage3.TotalTokens != 0 {
		t.Errorf("纯估算 usage = %+v", usage3)
	}
	assertAnthropicClientStream(t, feed3, done3, "", usage3)
}

func TestStreamConverterGuards(t *testing.T) {
	for _, p := range model.AllProtocols {
		if _, err := provider.NewStreamConverter(p, p); err == nil {
			t.Errorf("%s 相同协议应拒绝构造", p)
		}
	}
	if _, err := provider.NewStreamConverter(model.Protocol("nope"), model.ProtocolChatCompletions); err == nil {
		t.Error("未知客户端协议应报错")
	}
	// responses 作为上游时转换器仍可构造（openai 解析器承担 responses 事件解析）；
	// 选择器保证转换路径不会选中 responses 上游，此处仅锁定解析行为本身。
	conv, err := provider.NewStreamConverter(model.ProtocolMessages, model.ProtocolResponses)
	if err != nil {
		t.Fatal(err)
	}
	feed, err := feedAll(t, conv, []string{
		`{"type":"response.output_text.delta","delta":"he"}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":4,"output_tokens":1,"total_tokens":5}}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	done, usage := conv.Finalize()
	if usage.PromptTokens != 4 || usage.CompletionTokens != 1 || usage.TotalTokens != 5 {
		t.Errorf("usage = %+v", usage)
	}
	assertAnthropicClientStream(t, feed, done, "he", usage)
}

// ---------- 中间表示 ----------

func TestConversationFullText(t *testing.T) {
	c := provider.Conversation{
		System:   "sys",
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "q"}, {Role: provider.RoleAssistant, Text: "a"}},
	}
	if c.FullText() != "sys\nq\na\n" {
		t.Errorf("FullText = %q", c.FullText())
	}
	var empty provider.Conversation
	if empty.FullText() != "" {
		t.Error("空会话 FullText 应为空串")
	}
}

func TestConvertResponseToolCalls(t *testing.T) {
	// chat 上游 tool_calls -> 三种客户端
	chatUp := mustJSON(t, map[string]any{
		"id": "chatcmpl-9", "model": "gpt-up",
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role": "assistant", "content": nil,
				"tool_calls": []map[string]any{{
					"id": "call_1", "type": "function",
					"function": map[string]any{"name": "get_weather", "arguments": `{"city":"北京"}`},
				}},
			},
			"finish_reason": "tool_calls",
		}},
		"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
	})
	// anthropic 上游 tool_use -> 三种客户端
	msUp := mustJSON(t, map[string]any{
		"id": "msg_9", "type": "message", "role": "assistant", "model": "claude-up",
		"content": []map[string]any{
			{"type": "text", "text": "查一下"},
			{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": map[string]any{"city": "北京"}},
		},
		"stop_reason": "tool_use",
		"usage":       map[string]any{"input_tokens": 10, "output_tokens": 5},
	})
	for _, tc := range []struct {
		up   model.Protocol
		body []byte
	}{
		{model.ProtocolChatCompletions, chatUp},
		{model.ProtocolMessages, msUp},
	} {
		// chat 客户端
		body, _, err := provider.ConvertResponse(model.ProtocolChatCompletions, tc.up, tc.body, "m")
		if err != nil {
			t.Fatalf("%s -> chat: %v", tc.up, err)
		}
		om := toObj(t, body)
		ch := toMap(t, toArr(t, om["choices"])[0])
		if ch["finish_reason"] != "tool_calls" {
			t.Errorf("%s -> chat finish_reason = %v", tc.up, ch["finish_reason"])
		}
		msg := toMap(t, ch["message"])
		tcs := toArr(t, msg["tool_calls"])
		if len(tcs) != 1 {
			t.Fatalf("%s -> chat tool_calls: %v", tc.up, msg)
		}
		fn := toMap(t, toMap(t, tcs[0])["function"])
		if fn["name"] != "get_weather" || fn["arguments"] != `{"city":"北京"}` {
			t.Errorf("%s -> chat tool_call: %v", tc.up, fn)
		}
		// messages 客户端
		body, _, err = provider.ConvertResponse(model.ProtocolMessages, tc.up, tc.body, "m")
		if err != nil {
			t.Fatalf("%s -> messages: %v", tc.up, err)
		}
		om = toObj(t, body)
		if om["stop_reason"] != "tool_use" {
			t.Errorf("%s -> messages stop_reason = %v", tc.up, om["stop_reason"])
		}
		var sawToolUse bool
		for _, b := range toArr(t, om["content"]) {
			blk := toMap(t, b)
			if blk["type"] == "tool_use" {
				sawToolUse = true
				if blk["name"] != "get_weather" || toMap(t, blk["input"])["city"] != "北京" {
					t.Errorf("%s -> messages tool_use: %v", tc.up, blk)
				}
			}
		}
		if !sawToolUse {
			t.Errorf("%s -> messages 缺 tool_use 块: %v", tc.up, om["content"])
		}
		// responses 客户端
		body, _, err = provider.ConvertResponse(model.ProtocolResponses, tc.up, tc.body, "m")
		if err != nil {
			t.Fatalf("%s -> responses: %v", tc.up, err)
		}
		om = toObj(t, body)
		if om["status"] != "completed" {
			t.Errorf("%s -> responses status = %v（工具调用属正常完成）", tc.up, om["status"])
		}
		var sawFC bool
		for _, it := range toArr(t, om["output"]) {
			item := toMap(t, it)
			if item["type"] == "function_call" {
				sawFC = true
				if item["name"] != "get_weather" || item["call_id"] == nil || item["status"] != "completed" {
					t.Errorf("%s -> responses function_call: %v", tc.up, item)
				}
			}
		}
		if !sawFC {
			t.Errorf("%s -> responses 缺 function_call 条目: %v", tc.up, om["output"])
		}
	}
}

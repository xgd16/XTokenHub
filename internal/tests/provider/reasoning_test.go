package provider_test

import (
	"encoding/json"
	"testing"

	"xtokenhub/internal/model"
	"xtokenhub/internal/provider"
)

// mimoRule 预置映射（同官方推荐）：none/minimal/low -> disabled，medium/high/xhigh/max -> enabled。
func mimoRule(missing string) model.ReasoningRewrite {
	return model.ReasoningRewrite{
		Mapping: model.ReasoningMappingList{
			{Level: "none", Target: model.ReasoningOff},
			{Level: "minimal", Target: model.ReasoningOff},
			{Level: "low", Target: model.ReasoningOff},
			{Level: "medium", Target: model.ReasoningOn},
			{Level: "high", Target: model.ReasoningOn},
			{Level: "xhigh", Target: model.ReasoningOn},
			{Level: "max", Target: model.ReasoningOn},
		},
		Missing: missing,
	}
}

func bodyField(t *testing.T, body []byte, key string) json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("解析出站体: %v: %s", err, body)
	}
	return m[key]
}

func TestApplyReasoningRewriteChatTiers(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantOn   bool // thinking.type 期望值；nil 表示不应出现 thinking
		noThink  bool
		noEffort bool
	}{
		{name: "low->disabled", body: `{"model":"m","reasoning_effort":"low","messages":[]}`, wantOn: false, noEffort: true},
		{name: "high->enabled", body: `{"model":"m","reasoning_effort":"HIGH","messages":[]}`, wantOn: true, noEffort: true},
		{name: "max->enabled", body: `{"model":"m","reasoning_effort":"max","messages":[]}`, wantOn: true, noEffort: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := provider.ApplyReasoningRewrite([]byte(tc.body), model.ProtocolChatCompletions, mimoRule(""))
			th := bodyField(t, out, "thinking")
			if tc.noThink {
				if len(th) != 0 {
					t.Fatalf("不应出现 thinking: %s", out)
				}
				return
			}
			var v struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(th, &v); err != nil {
				t.Fatalf("thinking 解析失败: %s", out)
			}
			want := model.ReasoningOff
			if tc.wantOn {
				want = model.ReasoningOn
			}
			if v.Type != want {
				t.Fatalf("thinking.type = %q, want %q: %s", v.Type, want, out)
			}
		})
	}
}

// 未命中档位保留原样（含未知档位串与未配置映射的档位）。
func TestApplyReasoningRewriteChatMissKeepsOriginal(t *testing.T) {
	rule := mimoRule("")
	rule.Mapping = rule.Mapping[:3] // 去掉 medium，模拟未配置该档位

	for _, body := range []string{
		`{"model":"m","reasoning_effort":"ultra","messages":[]}`,
		`{"model":"m","reasoning_effort":"medium","messages":[]}`,
	} {
		out := provider.ApplyReasoningRewrite([]byte(body), model.ProtocolChatCompletions, rule)
		if string(body) != string(out) {
			t.Fatalf("未命中应原样返回: %s -> %s", body, out)
		}
	}
}

// body 已有 thinking：只删档位字段，不覆盖已有配置。
func TestApplyReasoningRewriteChatKeepsExistingThinking(t *testing.T) {
	body := `{"model":"m","reasoning_effort":"high","thinking":{"type":"disabled","x":1},"messages":[]}`
	out := provider.ApplyReasoningRewrite([]byte(body), model.ProtocolChatCompletions, mimoRule(""))
	if eff := bodyField(t, out, "reasoning_effort"); len(eff) != 0 {
		t.Fatalf("档位字段应删除: %s", out)
	}
	th := bodyField(t, out, "thinking")
	var v struct {
		Type string `json:"type"`
		X    int    `json:"x"`
	}
	if err := json.Unmarshal(th, &v); err != nil || v.Type != model.ReasoningOff || v.X != 1 {
		t.Fatalf("已有 thinking 应保留: %s", out)
	}
}

// 客户端未带档位：配置了 missing 时注入，未配置时不动。
func TestApplyReasoningRewriteChatMissing(t *testing.T) {
	body := `{"model":"m","messages":[]}`
	out := provider.ApplyReasoningRewrite([]byte(body), model.ProtocolChatCompletions, mimoRule("disabled"))
	var v struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(bodyField(t, out, "thinking"), &v); err != nil || v.Type != model.ReasoningOff {
		t.Fatalf("应注入 disabled: %s", out)
	}

	out = provider.ApplyReasoningRewrite([]byte(body), model.ProtocolChatCompletions, mimoRule(""))
	if th := bodyField(t, out, "thinking"); len(th) != 0 {
		t.Fatalf("missing 未配置不应注入: %s", out)
	}
}

// messages 出站体：thinking.type 伪档位翻译成官方开关；标准开关原样保留。
func TestApplyReasoningRewriteMessages(t *testing.T) {
	out := provider.ApplyReasoningRewrite(
		[]byte(`{"model":"m","thinking":{"type":"high"},"max_tokens":10}`),
		model.ProtocolMessages, mimoRule(""))
	var v struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(bodyField(t, out, "thinking"), &v); err != nil || v.Type != model.ReasoningOn {
		t.Fatalf("伪档位 high 应翻译为 enabled: %s", out)
	}

	out = provider.ApplyReasoningRewrite(
		[]byte(`{"model":"m","thinking":{"type":"enabled","budget_tokens":1024},"max_tokens":10}`),
		model.ProtocolMessages, mimoRule(""))
	var v2 struct {
		Type         string `json:"type"`
		BudgetTokens int64  `json:"budget_tokens"`
	}
	if err := json.Unmarshal(bodyField(t, out, "thinking"), &v2); err != nil ||
		v2.Type != model.ReasoningOn || v2.BudgetTokens != 1024 {
		t.Fatalf("标准 thinking 应原样保留: %s", out)
	}
}

// responses 上游原生支持档位：不改写。
func TestApplyReasoningRewriteResponsesUntouched(t *testing.T) {
	body := `{"model":"m","reasoning":{"effort":"high"},"input":"hi"}`
	out := provider.ApplyReasoningRewrite([]byte(body), model.ProtocolResponses, mimoRule(""))
	if string(out) != body {
		t.Fatalf("responses 上游不应改写: %s", out)
	}
}

// 转换路径：chat 入站的 reasoning_effort 经 ReqParams 提取后回写到 chat 上游体。
func TestConvertRequestCarriesReasoningEffort(t *testing.T) {
	out, err := provider.ConvertRequest(model.ProtocolMessages, model.ProtocolChatCompletions,
		[]byte(`{"model":"m","max_tokens":10,"thinking":{"type":"low"},"messages":[{"role":"user","content":"hi"}]}`), "mimo-v2.6-pro")
	if err != nil {
		t.Fatal(err)
	}
	if eff := bodyField(t, out, "reasoning_effort"); string(eff) != `"low"` {
		t.Fatalf("messages 伪档位应回写 reasoning_effort=low: %s", out)
	}

	out, err = provider.ConvertRequest(model.ProtocolResponses, model.ProtocolChatCompletions,
		[]byte(`{"model":"m","input":"hi","reasoning":{"effort":"high"}}`), "mimo-v2.6-pro")
	if err != nil {
		t.Fatal(err)
	}
	if eff := bodyField(t, out, "reasoning_effort"); string(eff) != `"high"` {
		t.Fatalf("responses reasoning.effort 应回写: %s", out)
	}
}

package provider

import (
	"encoding/json"

	"xtokenhub/internal/model"
)

// ---------- 思维档位改写：档位串 -> thinking 开关 ----------

// ApplyReasoningRewrite 按规则把出站请求体里的思维档位改写为 thinking 开关，
// 供只认 thinking: {"type": "enabled"|"disabled"} 的上游（如 MiMo）正常解析。
//
// 按上游协议识别档位载体：
//   - chat:      顶层 reasoning_effort（透传与转换路径统一在此翻译）
//   - messages:  thinking.type 伪档位（客户端把档位串塞进 type 的兼容写法）
//   - responses: 上游原生支持档位，不改写
//
// 语义：命中映射 -> 删档位字段并写 thinking；body 已有 thinking -> 只删档位字段
// 不覆盖；未命中映射 -> 保留原样；未带档位且规则配置了 Missing -> 注入。
func ApplyReasoningRewrite(body []byte, up model.Protocol, rule model.ReasoningRewrite) []byte {
	switch up {
	case model.ProtocolChatCompletions:
		return rewriteChatThinking(body, rule)
	case model.ProtocolMessages:
		return rewriteMessagesThinking(body, rule)
	default:
		return body
	}
}

// rewriteChatThinking chat 出站体：reasoning_effort -> thinking。
func rewriteChatThinking(body []byte, rule model.ReasoningRewrite) []byte {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	_, hasThinking := m["thinking"]
	tier, hasTier := tierField(m, "reasoning_effort")
	switch {
	case hasTier:
		target := rule.TargetFor(tier)
		if target == "" {
			return body // 未命中档位：保留原样
		}
		delete(m, "reasoning_effort")
		if !hasThinking {
			if !setThinkingSwitch(m, target) {
				return body
			}
		}
	case !hasThinking && rule.Missing != "":
		if !setThinkingSwitch(m, rule.Missing) {
			return body
		}
	default:
		return body
	}
	return remarshalBody(m, body)
}

// rewriteMessagesThinking messages 出站体：thinking.type 伪档位 -> 官方开关。
// 标准 enabled/disabled 原样保留；其余字段（如 budget_tokens）不动。
func rewriteMessagesThinking(body []byte, rule model.ReasoningRewrite) []byte {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	raw, hasThinking := m["thinking"]
	if !hasThinking {
		if rule.Missing == "" {
			return body
		}
		if !setThinkingSwitch(m, rule.Missing) {
			return body
		}
		return remarshalBody(m, body)
	}
	var t struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &t) != nil || !model.IsReasoningTier(t.Type) {
		return body // 标准开关或无法解析：原样
	}
	target := rule.TargetFor(t.Type)
	if target == "" {
		return body // 未命中档位：保留原样
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return body
	}
	typed, err := json.Marshal(target)
	if err != nil {
		return body
	}
	obj["type"] = typed
	fixed, err := json.Marshal(obj)
	if err != nil {
		return body
	}
	m["thinking"] = fixed
	return remarshalBody(m, body)
}

// tierField 读取顶层档位字段（字符串形态）；缺失 / null / 空串视为未携带。
func tierField(m map[string]json.RawMessage, key string) (tier string, ok bool) {
	raw, exists := m[key]
	if !exists {
		return "", false
	}
	if err := json.Unmarshal(raw, &tier); err != nil || model.NormalizeTier(tier) == "" {
		return "", false
	}
	return model.NormalizeTier(tier), true
}

// setThinkingSwitch 写入 thinking 开关；写入失败（不可能的固定串）返回 false 由调用方兜底原样返回。
func setThinkingSwitch(m map[string]json.RawMessage, target string) bool {
	raw, err := json.Marshal(map[string]string{"type": target})
	if err != nil {
		return false
	}
	m["thinking"] = raw
	return true
}

// remarshalBody 序列化改写后的 body；失败时原样返回，由上游报错触发 failover。
func remarshalBody(m map[string]json.RawMessage, origin []byte) []byte {
	out, err := json.Marshal(m)
	if err != nil {
		return origin
	}
	return out
}

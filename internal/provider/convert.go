package provider

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"xtokenhub/internal/model"
)

// ---------- 中间表示：三种协议的会话公共形态 ----------
// 承载文本对话、函数工具定义/调用/结果与常用采样参数；
// 多模态等复杂载荷仅在原生透传路径中保证完整。

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool" // 工具结果消息（chat: role=tool；responses: function_call_output）
)

// ToolDef 工具定义（统一扁平形态；Parameters 为 JSON Schema 对象）。
type ToolDef struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// ToolCall 模型发起的工具调用；Arguments 为参数 JSON 串。
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// ToolChoice 统一形态：auto/none/required，或 Mode="function" 指定函数。
type ToolChoice struct {
	Mode string
	Name string
}

// Conversation 归一化会话。
type Conversation struct {
	System   string
	Messages []Message
}

// Message 归一化消息。
type Message struct {
	Role       Role
	Text       string
	ToolCalls  []ToolCall // assistant 消息附带的工具调用
	ToolCallID string     // tool 消息对应的调用 ID
}

// ReqParams 常用采样参数与工具配置。
type ReqParams struct {
	Model       string     `json:"-"`
	MaxTokens   int64      `json:"-"`
	Temperature *float64   `json:"-"`
	TopP        *float64   `json:"-"`
	Stop        []string   `json:"-"`
	Stream      bool       `json:"-"`
	Tools       []ToolDef  `json:"-"`
	ToolChoice  ToolChoice `json:"-"`
}

// FullText 会话全部文本（用于估算输入 token）。
func (c *Conversation) FullText() string {
	var b strings.Builder
	if c.System != "" {
		b.WriteString(c.System)
		b.WriteString("\n")
	}
	for _, m := range c.Messages {
		b.WriteString(m.Text)
		b.WriteString("\n")
		for _, tc := range m.ToolCalls {
			b.WriteString(tc.Name)
			b.WriteString(" ")
			b.WriteString(tc.Arguments)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// ValidateRequest 校验入站请求体并可解析性（不做协议转换时也用于 fail-fast）。
func ValidateRequest(p model.Protocol, body []byte) error {
	switch p {
	case model.ProtocolChatCompletions:
		var req struct {
			Messages []json.RawMessage `json:"messages"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			return fmt.Errorf("非法 JSON: %w", err)
		}
		if len(req.Messages) == 0 {
			return fmt.Errorf("messages 不能为空")
		}
	case model.ProtocolResponses:
		var req struct {
			Input json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(body, &req); err != nil || len(req.Input) == 0 {
			return fmt.Errorf("input 不能为空")
		}
	case model.ProtocolMessages:
		var req struct {
			Messages  []json.RawMessage `json:"messages"`
			MaxTokens *int64            `json:"max_tokens"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			return fmt.Errorf("非法 JSON: %w", err)
		}
		if len(req.Messages) == 0 {
			return fmt.Errorf("messages 不能为空")
		}
		if req.MaxTokens == nil {
			return fmt.Errorf("max_tokens 为必填(anthropic 协议)")
		}
	default:
		return errUnsupportedProtocol(p)
	}
	return nil
}

// ---------- 入站请求解析 ----------

func contentToText(v json.RawMessage) string {
	if len(v) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(v, &s); err == nil {
		return s
	}
	// content parts 数组：拼接 text 段
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(v, &parts); err == nil {
		var b strings.Builder
		for _, p := range parts {
			if p.Text != "" {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				b.WriteString(p.Text)
			}
		}
		return b.String()
	}
	return ""
}

// ParseRequest 将入站请求解析为中间表示。
func ParseRequest(p model.Protocol, body []byte) (Conversation, ReqParams, error) {
	switch p {
	case model.ProtocolChatCompletions:
		return parseOpenAIChatRequest(body)
	case model.ProtocolResponses:
		return parseResponsesRequest(body)
	case model.ProtocolMessages:
		return parseAnthropicRequest(body)
	default:
		return Conversation{}, ReqParams{}, errUnsupportedProtocol(p)
	}
}

func parseOpenAIChatRequest(body []byte) (Conversation, ReqParams, error) {
	var req struct {
		Model       string          `json:"model"`
		Messages    []openAIMessage `json:"messages"`
		MaxTokens   *int64          `json:"max_tokens"`
		Temperature *float64        `json:"temperature"`
		TopP        *float64        `json:"top_p"`
		Stop        json.RawMessage `json:"stop"`
		Stream      bool            `json:"stream"`
		Tools       []struct {
			Type     string `json:"type"`
			Function struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Parameters  json.RawMessage `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
		ToolChoice json.RawMessage `json:"tool_choice"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return Conversation{}, ReqParams{}, fmt.Errorf("解析 openai 请求: %w", err)
	}
	var conv Conversation
	for _, m := range req.Messages {
		text := contentToText(m.Content)
		switch m.Role {
		case "system", "developer":
			if conv.System != "" {
				conv.System += "\n"
			}
			conv.System += text
		case "assistant":
			msg := Message{Role: RoleAssistant, Text: text}
			for _, tc := range m.ToolCalls {
				if tc.ID == "" && tc.Function.Name == "" {
					continue
				}
				msg.ToolCalls = append(msg.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
			}
			if text == "" && len(msg.ToolCalls) == 0 {
				continue
			}
			conv.Messages = append(conv.Messages, msg)
		case "tool":
			if m.ToolCallID == "" {
				// 缺 tool_call_id 无法与调用配对，发往上游会被拒绝：跳过
				continue
			}
			conv.Messages = append(conv.Messages, Message{Role: RoleTool, Text: text, ToolCallID: m.ToolCallID})
		case "function":
			// 旧版函数消息：无 tool_call_id，按文本归一为 user
			if text == "" {
				continue
			}
			conv.Messages = append(conv.Messages, Message{Role: RoleUser, Text: text})
		default:
			conv.Messages = append(conv.Messages, Message{Role: RoleUser, Text: text})
		}
	}
	params := ReqParams{
		Model: req.Model, MaxTokens: deref(req.MaxTokens), Temperature: req.Temperature,
		TopP: req.TopP, Stop: rawToStrings(req.Stop), Stream: req.Stream,
		ToolChoice: parseToolChoice(req.ToolChoice),
	}
	for _, t := range req.Tools {
		if t.Function.Name == "" {
			continue
		}
		params.Tools = append(params.Tools, ToolDef{Name: t.Function.Name, Description: t.Function.Description, Parameters: t.Function.Parameters})
	}
	return conv, params, nil
}

type openAIMessage struct {
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	ToolCalls []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
	ToolCallID string `json:"tool_call_id"`
}

// parseToolChoice 解析 chat/responses 的 tool_choice（字符串或对象指定函数）。
func parseToolChoice(v json.RawMessage) ToolChoice {
	if len(v) == 0 {
		return ToolChoice{}
	}
	var s string
	if err := json.Unmarshal(v, &s); err == nil {
		switch s {
		case "auto", "none", "required", "any":
			return ToolChoice{Mode: s}
		}
		return ToolChoice{}
	}
	var o struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(v, &o) == nil {
		switch o.Type {
		case "function":
			name := o.Name
			if name == "" {
				name = o.Function.Name
			}
			return ToolChoice{Mode: "function", Name: name}
		case "auto", "none", "required", "any":
			return ToolChoice{Mode: o.Type}
		}
	}
	return ToolChoice{}
}

func parseResponsesRequest(body []byte) (Conversation, ReqParams, error) {
	var req struct {
		Model           string          `json:"model"`
		Input           json.RawMessage `json:"input"`
		Instructions    string          `json:"instructions"`
		MaxOutputTokens *int64          `json:"max_output_tokens"`
		Temperature     *float64        `json:"temperature"`
		TopP            *float64        `json:"top_p"`
		Stream          bool            `json:"stream"`
		Tools           []struct {
			Type        string          `json:"type"`
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		} `json:"tools"`
		ToolChoice json.RawMessage `json:"tool_choice"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return Conversation{}, ReqParams{}, fmt.Errorf("解析 responses 请求: %w", err)
	}
	var conv Conversation
	conv.System = req.Instructions
	// input: string 或 items 数组（message/function_call/function_call_output 等）
	var s string
	if err := json.Unmarshal(req.Input, &s); err == nil {
		conv.Messages = append(conv.Messages, Message{Role: RoleUser, Text: s})
	} else {
		var items []struct {
			Type      string          `json:"type"`
			Role      string          `json:"role"`
			Content   json.RawMessage `json:"content"`
			CallID    string          `json:"call_id"`
			Name      string          `json:"name"`
			Arguments string          `json:"arguments"`
			Output    json.RawMessage `json:"output"`
		}
		if err := json.Unmarshal(req.Input, &items); err != nil {
			return Conversation{}, ReqParams{}, fmt.Errorf("input 须为 string 或 items 数组")
		}
		for _, it := range items {
			switch it.Type {
			case "function_call":
				// assistant 侧历史工具调用
				if it.CallID == "" && it.Name == "" {
					continue
				}
				conv.Messages = append(conv.Messages, Message{Role: RoleAssistant,
					ToolCalls: []ToolCall{{ID: it.CallID, Name: it.Name, Arguments: it.Arguments}}})
			case "function_call_output":
				// user 侧工具结果（output 为字符串或 content parts）；缺 call_id 无法配对：跳过
				if it.CallID == "" {
					continue
				}
				conv.Messages = append(conv.Messages, Message{Role: RoleTool, Text: contentToText(it.Output), ToolCallID: it.CallID})
			case "", "message":
				text := contentToText(it.Content)
				switch it.Role {
				case "assistant":
					conv.Messages = append(conv.Messages, Message{Role: RoleAssistant, Text: text})
				default:
					conv.Messages = append(conv.Messages, Message{Role: RoleUser, Text: text})
				}
			default:
				// reasoning / item_reference 等非文本条目：跳过
			}
		}
	}
	if len(conv.Messages) == 0 {
		return Conversation{}, ReqParams{}, fmt.Errorf("input 不能为空")
	}
	params := ReqParams{Model: req.Model, MaxTokens: deref(req.MaxOutputTokens), Temperature: req.Temperature, TopP: req.TopP, Stream: req.Stream,
		ToolChoice: parseToolChoice(req.ToolChoice)}
	for _, t := range req.Tools {
		// responses 工具定义为扁平形态；web_search/file_search 等内置工具不映射
		if t.Name == "" || (t.Type != "" && t.Type != "function") {
			continue
		}
		params.Tools = append(params.Tools, ToolDef{Name: t.Name, Description: t.Description, Parameters: t.Parameters})
	}
	return conv, params, nil
}

func parseAnthropicRequest(body []byte) (Conversation, ReqParams, error) {
	var req struct {
		Model    string          `json:"model"`
		System   json.RawMessage `json:"system"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		MaxTokens   int64    `json:"max_tokens"`
		Temperature *float64 `json:"temperature"`
		TopP        *float64 `json:"top_p"`
		Stop        []string `json:"stop_sequences"`
		Stream      bool     `json:"stream"`
		Tools       []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"input_schema"`
		} `json:"tools"`
		ToolChoice *struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"tool_choice"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return Conversation{}, ReqParams{}, fmt.Errorf("解析 anthropic 请求: %w", err)
	}
	var conv Conversation
	conv.System = contentToText(req.System)
	if conv.System == "" && len(req.System) > 0 {
		_ = json.Unmarshal(req.System, &conv.System)
	}
	for _, m := range req.Messages {
		role := RoleUser
		if m.Role == "assistant" {
			role = RoleAssistant
		}
		var text string
		var calls []ToolCall
		var results []Message
		var s string
		if err := json.Unmarshal(m.Content, &s); err == nil {
			text = s // content 为纯字符串的宽松兼容
		} else {
			var blocks []struct {
				Type      string          `json:"type"`
				Text      string          `json:"text"`
				ID        string          `json:"id"`
				Name      string          `json:"name"`
				Input     json.RawMessage `json:"input"`
				ToolUseID string          `json:"tool_use_id"`
				Content   json.RawMessage `json:"content"`
			}
			if err := json.Unmarshal(m.Content, &blocks); err != nil {
				continue
			}
			for _, b := range blocks {
				switch b.Type {
				case "text":
					if text != "" {
						text += "\n"
					}
					text += b.Text
				case "tool_use":
					if b.ID == "" && b.Name == "" {
						continue
					}
					calls = append(calls, ToolCall{ID: b.ID, Name: b.Name, Arguments: argsFromRaw(b.Input)})
				case "tool_result":
					if b.ToolUseID == "" {
						// 缺 tool_use_id 无法与调用配对：跳过
						continue
					}
					results = append(results, Message{Role: RoleTool, Text: contentToText(b.Content), ToolCallID: b.ToolUseID})
				}
			}
		}
		if role == RoleAssistant {
			if text == "" && len(calls) == 0 {
				continue
			}
			conv.Messages = append(conv.Messages, Message{Role: RoleAssistant, Text: text, ToolCalls: calls})
		} else {
			// tool_result 须紧跟 assistant 工具调用之后（chat 上游要求 tool 消息成对），
			// 故先产出 tool 结果消息，再产出本轮 user 文本
			conv.Messages = append(conv.Messages, results...)
			if text != "" {
				conv.Messages = append(conv.Messages, Message{Role: RoleUser, Text: text})
			}
		}
	}
	params := ReqParams{Model: req.Model, MaxTokens: req.MaxTokens, Temperature: req.Temperature, TopP: req.TopP, Stop: req.Stop, Stream: req.Stream}
	for _, t := range req.Tools {
		if t.Name == "" {
			continue
		}
		params.Tools = append(params.Tools, ToolDef{Name: t.Name, Description: t.Description, Parameters: t.InputSchema})
	}
	if req.ToolChoice != nil {
		switch req.ToolChoice.Type {
		case "auto", "none":
			params.ToolChoice = ToolChoice{Mode: req.ToolChoice.Type}
		case "any":
			params.ToolChoice = ToolChoice{Mode: "required"}
		case "tool":
			params.ToolChoice = ToolChoice{Mode: "function", Name: req.ToolChoice.Name}
		}
	}
	return conv, params, nil
}

// argsFromRaw 工具参数对象转 JSON 串（null/缺失归为空串）。
func argsFromRaw(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	return string(raw)
}

func rawToStrings(v json.RawMessage) []string {
	if len(v) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(v, &s); err == nil {
		return []string{s}
	}
	var arr []string
	if err := json.Unmarshal(v, &arr); err == nil {
		return arr
	}
	return nil
}

// ---------- 上游请求序列化 ----------

// WriteUpstreamRequest 将中间表示序列化为上游协议请求体。
func WriteUpstreamRequest(up model.Protocol, conv Conversation, params ReqParams, upstreamModel string) ([]byte, error) {
	switch up {
	case model.ProtocolChatCompletions:
		return writeOpenAIChatUpstream(conv, params, upstreamModel)
	case model.ProtocolMessages:
		return writeAnthropicUpstream(conv, params, upstreamModel)
	case model.ProtocolResponses:
		// 上游为 responses 协议时仅支持原生透传（不做向 responses 的转换写入）
		return nil, fmt.Errorf("转换路径不支持 responses 上游，请使用原生渠道")
	default:
		return nil, errUnsupportedProtocol(up)
	}
}

func writeOpenAIChatUpstream(conv Conversation, params ReqParams, upstreamModel string) ([]byte, error) {
	msgs := make([]map[string]any, 0, len(conv.Messages)+1)
	if conv.System != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": conv.System})
	}
	for _, m := range conv.Messages {
		switch m.Role {
		case RoleTool:
			msgs = append(msgs, map[string]any{"role": "tool", "tool_call_id": m.ToolCallID, "content": m.Text})
		case RoleAssistant:
			msg := map[string]any{"role": "assistant"}
			if m.Text != "" {
				msg["content"] = m.Text
			} else {
				msg["content"] = nil
			}
			if len(m.ToolCalls) > 0 {
				tcs := make([]map[string]any, 0, len(m.ToolCalls))
				for i, tc := range m.ToolCalls {
					id := tc.ID
					if id == "" {
						id = fmt.Sprintf("call_%d", i+1)
					}
					tcs = append(tcs, map[string]any{"id": id, "type": "function",
						"function": map[string]any{"name": tc.Name, "arguments": tc.Arguments}})
				}
				msg["tool_calls"] = tcs
			}
			msgs = append(msgs, msg)
		default:
			msgs = append(msgs, map[string]any{"role": string(m.Role), "content": m.Text})
		}
	}
	out := map[string]any{
		"model":    upstreamModel,
		"messages": msgs,
	}
	if len(params.Tools) > 0 {
		tools := make([]map[string]any, 0, len(params.Tools))
		for _, t := range params.Tools {
			fn := map[string]any{"name": t.Name}
			if t.Description != "" {
				fn["description"] = t.Description
			}
			if len(t.Parameters) > 0 && string(t.Parameters) != "null" {
				fn["parameters"] = t.Parameters
			}
			tools = append(tools, map[string]any{"type": "function", "function": fn})
		}
		out["tools"] = tools
	}
	switch params.ToolChoice.Mode {
	case "auto", "none", "required":
		out["tool_choice"] = params.ToolChoice.Mode
	case "function":
		out["tool_choice"] = map[string]any{"type": "function", "function": map[string]any{"name": params.ToolChoice.Name}}
	}
	if params.MaxTokens > 0 {
		out["max_tokens"] = params.MaxTokens
	}
	if params.Temperature != nil {
		out["temperature"] = *params.Temperature
	}
	if params.TopP != nil {
		out["top_p"] = *params.TopP
	}
	if len(params.Stop) > 0 {
		if len(params.Stop) == 1 {
			out["stop"] = params.Stop[0]
		} else {
			out["stop"] = params.Stop
		}
	}
	if params.Stream {
		out["stream"] = true
		out["stream_options"] = map[string]any{"include_usage": true}
	}
	return json.Marshal(out)
}

func writeAnthropicUpstream(conv Conversation, params ReqParams, upstreamModel string) ([]byte, error) {
	msgs := make([]map[string]any, 0, len(conv.Messages))
	for i := 0; i < len(conv.Messages); {
		m := conv.Messages[i]
		switch m.Role {
		case RoleTool:
			// 相邻工具结果合并进同一条 user 消息（anthropic 要求 tool_result 挂在 user 侧）
			content := []map[string]any{{"type": "tool_result", "tool_use_id": m.ToolCallID, "content": m.Text}}
			j := i + 1
			for ; j < len(conv.Messages) && conv.Messages[j].Role == RoleTool; j++ {
				content = append(content, map[string]any{"type": "tool_result", "tool_use_id": conv.Messages[j].ToolCallID, "content": conv.Messages[j].Text})
			}
			msgs = append(msgs, map[string]any{"role": "user", "content": content})
			i = j
			continue
		case RoleAssistant:
			content := make([]map[string]any, 0, len(m.ToolCalls)+1)
			if m.Text != "" {
				content = append(content, map[string]any{"type": "text", "text": m.Text})
			}
			for k, tc := range m.ToolCalls {
				id := tc.ID
				if id == "" {
					id = fmt.Sprintf("toolu_%d", k+1)
				}
				content = append(content, map[string]any{"type": "tool_use", "id": id, "name": tc.Name, "input": parseJSONObject(tc.Arguments)})
			}
			msgs = append(msgs, map[string]any{"role": "assistant", "content": content})
		default:
			msgs = append(msgs, map[string]any{
				"role":    string(m.Role),
				"content": []map[string]any{{"type": "text", "text": m.Text}},
			})
		}
		i++
	}
	out := map[string]any{
		"model":    upstreamModel,
		"messages": msgs,
	}
	if len(params.Tools) > 0 {
		tools := make([]map[string]any, 0, len(params.Tools))
		for _, t := range params.Tools {
			tt := map[string]any{"name": t.Name}
			if t.Description != "" {
				tt["description"] = t.Description
			}
			schema := t.Parameters
			if len(schema) == 0 || string(schema) == "null" {
				schema = json.RawMessage(`{"type":"object"}`)
			}
			tt["input_schema"] = schema
			tools = append(tools, tt)
		}
		out["tools"] = tools
	}
	switch params.ToolChoice.Mode {
	case "auto":
		out["tool_choice"] = map[string]any{"type": "auto"}
	case "none":
		out["tool_choice"] = map[string]any{"type": "none"}
	case "required":
		out["tool_choice"] = map[string]any{"type": "any"}
	case "function":
		out["tool_choice"] = map[string]any{"type": "tool", "name": params.ToolChoice.Name}
	}
	// anthropic 协议 max_tokens 必填
	mt := params.MaxTokens
	if mt <= 0 {
		mt = 1024
	}
	out["max_tokens"] = mt
	if conv.System != "" {
		out["system"] = conv.System
	}
	if params.Temperature != nil {
		out["temperature"] = *params.Temperature
	}
	if params.TopP != nil {
		out["top_p"] = *params.TopP
	}
	if len(params.Stop) > 0 {
		out["stop_sequences"] = params.Stop
	}
	if params.Stream {
		out["stream"] = true
	}
	return json.Marshal(out)
}

// parseJSONObject 参数 JSON 串解析为对象（anthropic input 须为对象；非法时回退空对象）。
func parseJSONObject(s string) any {
	if s == "" || s == "null" {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil || v == nil {
		return map[string]any{}
	}
	return v
}

// ---------- 上游响应解析为统一结果 ----------

// Result 上游非流式响应的统一形态。
type Result struct {
	ID           string
	Model        string
	Text         string
	ToolCalls    []ToolCall
	FinishReason string
	Usage        Usage
}

// ParseUpstreamResponse 解析上游非流式响应（按上游协议）。
func ParseUpstreamResponse(up model.Protocol, body []byte) (Result, error) {
	switch up {
	case model.ProtocolChatCompletions:
		var resp struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Choices []struct {
				Message      openAIMessage `json:"message"`
				FinishReason string        `json:"finish_reason"`
			} `json:"choices"`
			Usage *upstreamUsage `json:"usage"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return Result{}, fmt.Errorf("解析上游 openai 响应: %w", err)
		}
		r := Result{ID: resp.ID, Model: resp.Model, Usage: Usage{Source: UsageFromUpstream}}
		if len(resp.Choices) > 0 {
			m := resp.Choices[0].Message
			r.Text = contentToText(m.Content)
			r.FinishReason = resp.Choices[0].FinishReason
			for _, tc := range m.ToolCalls {
				if tc.ID == "" && tc.Function.Name == "" {
					continue
				}
				r.ToolCalls = append(r.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
			}
		}
		if resp.Usage != nil {
			r.Usage = resp.Usage.toUsage()
		}
		return r, nil
	case model.ProtocolMessages:
		var resp struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Content []struct {
				Type  string          `json:"type"`
				Text  string          `json:"text"`
				ID    string          `json:"id"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			} `json:"content"`
			StopReason string         `json:"stop_reason"`
			Usage      *upstreamUsage `json:"usage"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return Result{}, fmt.Errorf("解析上游 anthropic 响应: %w", err)
		}
		r := Result{ID: resp.ID, Model: resp.Model, Usage: Usage{Source: UsageFromUpstream}}
		var b strings.Builder
		for _, c := range resp.Content {
			switch c.Type {
			case "text":
				b.WriteString(c.Text)
			case "tool_use":
				if c.ID == "" && c.Name == "" {
					continue
				}
				r.ToolCalls = append(r.ToolCalls, ToolCall{ID: c.ID, Name: c.Name, Arguments: argsFromRaw(c.Input)})
			}
		}
		r.Text = b.String()
		r.FinishReason = resp.StopReason
		if resp.Usage != nil {
			r.Usage = resp.Usage.toUsage()
		}
		return r, nil
	default:
		return Result{}, errUnsupportedProtocol(up)
	}
}

// ---------- 端到端组合：请求/响应转换 ----------

// ConvertRequest 将入站协议请求体转换为上游协议请求体（仅转换路径；相同协议应走透传）。
// 采样参数原样迁移，模型名替换为上游渠道模型。
func ConvertRequest(in, up model.Protocol, body []byte, upstreamModel string) ([]byte, error) {
	if in == up {
		return nil, fmt.Errorf("原生协议无需转换（%s）", in)
	}
	conv, params, err := ParseRequest(in, body)
	if err != nil {
		return nil, err
	}
	return WriteUpstreamRequest(up, conv, params, upstreamModel)
}

// ConvertResponse 将上游非流式响应转换为客户端协议响应，返回 usage（上游未报告时为零值）。
// 上游采样参数已在上游生效，此处仅做响应形态回转。
func ConvertResponse(in, up model.Protocol, upstreamBody []byte, clientModel string) ([]byte, Usage, error) {
	res, err := ParseUpstreamResponse(up, upstreamBody)
	if err != nil {
		return nil, Usage{}, err
	}
	res.Model = clientModel
	out, err := WriteClientResponse(in, res)
	return out, res.Usage, err
}

// ---------- 入站响应序列化（转换回客户端协议） ----------

// WriteClientResponse 将统一结果序列化为客户端协议的非流式响应。
func WriteClientResponse(in model.Protocol, r Result) ([]byte, error) {
	switch in {
	case model.ProtocolChatCompletions:
		msg := map[string]any{"role": "assistant"}
		finish := mapFinish(r.FinishReason)
		if len(r.ToolCalls) > 0 {
			// 官方语义：发起工具调用时 finish_reason=tool_calls（length 截断除外）
			if finish == "stop" || finish == "" {
				finish = "tool_calls"
			}
			tcs := make([]map[string]any, 0, len(r.ToolCalls))
			for i, tc := range r.ToolCalls {
				id := tc.ID
				if id == "" {
					id = fmt.Sprintf("call_%d", i+1)
				}
				tcs = append(tcs, map[string]any{"id": id, "type": "function",
					"function": map[string]any{"name": tc.Name, "arguments": tc.Arguments}})
			}
			msg["tool_calls"] = tcs
			if r.Text != "" {
				msg["content"] = r.Text
			} else {
				msg["content"] = nil
			}
		} else {
			msg["content"] = r.Text
		}
		// created 为官方 SDK 必填字段（缺失时客户端解析失败）
		return json.Marshal(map[string]any{
			"id":      r.ID,
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   r.Model,
			"choices": []map[string]any{{
				"index":         0,
				"message":       msg,
				"finish_reason": finish,
			}},
			"usage": map[string]any{
				"prompt_tokens":         r.Usage.PromptTokens,
				"completion_tokens":     r.Usage.CompletionTokens,
				"total_tokens":          r.Usage.TotalTokens,
				"prompt_tokens_details": map[string]any{"cached_tokens": r.Usage.CachedTokens},
			},
		})
	case model.ProtocolMessages:
		content := []map[string]any{}
		if r.Text != "" {
			content = append(content, map[string]any{"type": "text", "text": r.Text})
		}
		for _, tc := range r.ToolCalls {
			id := tc.ID
			if id == "" {
				id = "toolu_0"
			}
			content = append(content, map[string]any{"type": "tool_use", "id": id, "name": tc.Name, "input": parseJSONObject(tc.Arguments)})
		}
		if len(content) == 0 {
			content = append(content, map[string]any{"type": "text", "text": ""})
		}
		stop := mapStop(r.FinishReason)
		if len(r.ToolCalls) > 0 && stop != "max_tokens" {
			stop = "tool_use"
		}
		return json.Marshal(map[string]any{
			"id":            r.ID,
			"type":          "message",
			"role":          "assistant",
			"model":         r.Model,
			"content":       content,
			"stop_reason":   stop,
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":                r.Usage.PromptTokens,
				"output_tokens":               r.Usage.CompletionTokens,
				"cache_creation_input_tokens": r.Usage.CacheWriteTokens,
				"cache_read_input_tokens":     r.Usage.CachedTokens,
			},
		})
	case model.ProtocolResponses:
		// 官方 SDK 对 response 对象做严格校验：created_at/parallel_tool_calls/
		// tool_choice/tools、output 条目的 id、content 的 annotations 均为必填。
		// 截断（length）对应官方 incomplete 语义，而非 completed。
		status, incomplete := "completed", any(nil)
		if r.FinishReason == "length" || r.FinishReason == "max_tokens" {
			status = "incomplete"
			incomplete = map[string]any{"reason": "max_output_tokens"}
		}
		output := make([]map[string]any, 0, len(r.ToolCalls)+1)
		for i, tc := range r.ToolCalls {
			callID := tc.ID
			if callID == "" {
				callID = fmt.Sprintf("call_%d", i+1)
			}
			output = append(output, map[string]any{
				"id": callID, "call_id": callID, "type": "function_call", "name": tc.Name,
				"arguments": tc.Arguments, "status": "completed",
			})
		}
		if r.Text != "" || len(r.ToolCalls) == 0 {
			output = append(output, map[string]any{
				"id": "msg_0", "type": "message", "role": "assistant", "status": status,
				"content": []map[string]any{{
					"type": "output_text", "text": r.Text, "annotations": []any{}, "logprobs": []any{},
				}},
			})
		}
		return json.Marshal(map[string]any{
			"id":                  r.ID,
			"object":              "response",
			"created_at":          time.Now().Unix(),
			"model":               r.Model,
			"status":              status,
			"parallel_tool_calls": true,
			"tool_choice":         "auto",
			"tools":               []any{},
			"text":                map[string]any{"format": map[string]any{"type": "text"}},
			"truncation":          "disabled",
			"metadata":            map[string]any{},
			"error":               nil,
			"incomplete_details":  incomplete,
			"output":              output,
			"usage":               responsesUsageObj(r.Usage),
		})
	default:
		return nil, errUnsupportedProtocol(in)
	}
}

func mapFinish(reason string) string {
	switch reason {
	case "tool_use", "tool_calls", "function_call":
		return "tool_calls"
	case "end_turn", "stop_sequence", "refusal":
		// anthropic 结束原因归一为 chat 官方语义 stop
		return "stop"
	case "":
		return "stop"
	default:
		return reason
	}
}

func mapStop(reason string) string {
	switch reason {
	case "length", "max_tokens":
		return "max_tokens"
	case "tool_calls", "tool_use", "function_call":
		return "tool_use"
	default:
		return "end_turn"
	}
}

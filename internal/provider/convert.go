package provider

import (
	"encoding/json"
	"fmt"
	"strings"

	"xtokenhub/internal/model"
)

// ---------- 中间表示：三种协议的文本会话公共形态 ----------
// v1 转换路径聚焦文本对话（system/user/assistant + 常用采样参数）；
// 工具调用、多模态等复杂载荷仅在原生透传路径中保证完整。

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Conversation 归一化会话。
type Conversation struct {
	System   string
	Messages []Message
}

// Message 归一化消息。
type Message struct {
	Role Role
	Text string
}

// ReqParams 常用采样参数。
type ReqParams struct {
	Model       string   `json:"-"`
	MaxTokens   int64    `json:"-"`
	Temperature *float64 `json:"-"`
	TopP        *float64 `json:"-"`
	Stop        []string `json:"-"`
	Stream      bool     `json:"-"`
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
			conv.Messages = append(conv.Messages, Message{Role: RoleAssistant, Text: text})
		default:
			conv.Messages = append(conv.Messages, Message{Role: RoleUser, Text: text})
		}
	}
	params := ReqParams{
		Model: req.Model, MaxTokens: deref(req.MaxTokens), Temperature: req.Temperature,
		TopP: req.TopP, Stop: rawToStrings(req.Stop), Stream: req.Stream,
	}
	return conv, params, nil
}

type openAIMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
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
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return Conversation{}, ReqParams{}, fmt.Errorf("解析 responses 请求: %w", err)
	}
	var conv Conversation
	conv.System = req.Instructions
	// input: string 或 items 数组（message 项取 content 文本）
	var s string
	if err := json.Unmarshal(req.Input, &s); err == nil {
		conv.Messages = append(conv.Messages, Message{Role: RoleUser, Text: s})
	} else {
		var items []struct {
			Type    string          `json:"type"`
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(req.Input, &items); err != nil {
			return Conversation{}, ReqParams{}, fmt.Errorf("input 须为 string 或 items 数组")
		}
		for _, it := range items {
			text := contentToText(it.Content)
			switch it.Role {
			case "assistant":
				conv.Messages = append(conv.Messages, Message{Role: RoleAssistant, Text: text})
			default:
				conv.Messages = append(conv.Messages, Message{Role: RoleUser, Text: text})
			}
		}
	}
	if len(conv.Messages) == 0 {
		return Conversation{}, ReqParams{}, fmt.Errorf("input 不能为空")
	}
	params := ReqParams{Model: req.Model, MaxTokens: deref(req.MaxOutputTokens), Temperature: req.Temperature, TopP: req.TopP, Stream: req.Stream}
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
		conv.Messages = append(conv.Messages, Message{Role: role, Text: contentToText(m.Content)})
	}
	params := ReqParams{Model: req.Model, MaxTokens: req.MaxTokens, Temperature: req.Temperature, TopP: req.TopP, Stop: req.Stop, Stream: req.Stream}
	return conv, params, nil
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
	default:
		// 上游为 responses 协议时仅支持原生透传（不做向 responses 的转换写入）
		return nil, fmt.Errorf("转换路径不支持 responses 上游，请使用原生渠道")
	}
}

func writeOpenAIChatUpstream(conv Conversation, params ReqParams, upstreamModel string) ([]byte, error) {
	msgs := make([]map[string]any, 0, len(conv.Messages)+1)
	if conv.System != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": conv.System})
	}
	for _, m := range conv.Messages {
		msgs = append(msgs, map[string]any{"role": string(m.Role), "content": m.Text})
	}
	out := map[string]any{
		"model":    upstreamModel,
		"messages": msgs,
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
	for _, m := range conv.Messages {
		msgs = append(msgs, map[string]any{
			"role":    string(m.Role),
			"content": []map[string]any{{"type": "text", "text": m.Text}},
		})
	}
	out := map[string]any{
		"model":    upstreamModel,
		"messages": msgs,
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

// ---------- 上游响应解析为统一结果 ----------

// Result 上游非流式响应的统一形态。
type Result struct {
	ID           string
	Model        string
	Text         string
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
			r.Text = contentToText(resp.Choices[0].Message.Content)
			r.FinishReason = resp.Choices[0].FinishReason
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
				Type string `json:"type"`
				Text string `json:"text"`
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
			if c.Type == "text" {
				b.WriteString(c.Text)
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
		return json.Marshal(map[string]any{
			"id":     r.ID,
			"object": "chat.completion",
			"model":  r.Model,
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": r.Text},
				"finish_reason": mapFinish(r.FinishReason),
			}},
			"usage": map[string]any{
				"prompt_tokens":         r.Usage.PromptTokens,
				"completion_tokens":     r.Usage.CompletionTokens,
				"total_tokens":          r.Usage.TotalTokens,
				"prompt_tokens_details": map[string]any{"cached_tokens": r.Usage.CachedTokens},
			},
		})
	case model.ProtocolMessages:
		return json.Marshal(map[string]any{
			"id":            r.ID,
			"type":          "message",
			"role":          "assistant",
			"model":         r.Model,
			"content":       []map[string]any{{"type": "text", "text": r.Text}},
			"stop_reason":   mapStop(r.FinishReason),
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":                r.Usage.PromptTokens,
				"output_tokens":               r.Usage.CompletionTokens,
				"cache_creation_input_tokens": r.Usage.CacheWriteTokens,
				"cache_read_input_tokens":     r.Usage.CachedTokens,
			},
		})
	case model.ProtocolResponses:
		return json.Marshal(map[string]any{
			"id":     r.ID,
			"object": "response",
			"model":  r.Model,
			"status": "completed",
			"output": []map[string]any{{
				"type":    "message",
				"role":    "assistant",
				"status":  "completed",
				"content": []map[string]any{{"type": "output_text", "text": r.Text}},
			}},
			"usage": map[string]any{
				"input_tokens":         r.Usage.PromptTokens,
				"output_tokens":        r.Usage.CompletionTokens,
				"total_tokens":         r.Usage.TotalTokens,
				"input_tokens_details": map[string]any{"cached_tokens": r.Usage.CachedTokens},
			},
		})
	default:
		return nil, errUnsupportedProtocol(in)
	}
}

func mapFinish(reason string) string {
	if reason == "" {
		return "stop"
	}
	return reason
}

func mapStop(reason string) string {
	switch reason {
	case "length", "max_tokens":
		return "max_tokens"
	case "":
		return "end_turn"
	default:
		return "end_turn"
	}
}

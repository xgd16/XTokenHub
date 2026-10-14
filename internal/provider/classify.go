package provider

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"xtokenhub/internal/model"
)

// ChannelClassifier 按渠道 + 模型发起一次非流式补全（记忆库 LLM 分类等内部任务复用）。
// 结构上满足 service.ClassifierLLM：协议选择与请求装配都在 provider 侧完成。
type ChannelClassifier struct {
	caller *ClassifierCaller
}

// NewChannelClassifier 构造分类调用适配器（timeout 为单次调用超时）。
func NewChannelClassifier(timeout time.Duration) *ChannelClassifier {
	return &ChannelClassifier{caller: NewClassifierCaller(timeout)}
}

// Classify 发起一次补全并返回文本（system 为任务指令，user 为待处理内容）。
func (a *ChannelClassifier) Classify(ctx context.Context, ch *model.Channel, modelName, system, user string) (string, error) {
	if ch == nil {
		return "", fmt.Errorf("渠道为空")
	}
	return a.caller.Call(ctx, ch.BaseURL, ch.APIKey, ch.Provider, ClassifierProtocol(ch),
		modelName, system, user, 2048)
}

// ClassifierProtocol 选择分类可用的原生协议：优先渠道原生的 chat_completions / messages；
// 渠道未探测时按厂家类型推断（Anthropic 用 messages，其余用 chat_completions）。
// responses-only 渠道暂不支持分类（转换层只写 chat/messages 上游）。
func ClassifierProtocol(ch *model.Channel) model.Protocol {
	for _, p := range ch.NativeProtocols() {
		if p == model.ProtocolChatCompletions || p == model.ProtocolMessages {
			return p
		}
	}
	if ch.Provider == model.ProviderAnthropic {
		return model.ProtocolMessages
	}
	return model.ProtocolChatCompletions
}

// ClassifierCaller 非流式补全客户端（复用 UpstreamClient 与既有协议转换/解析）。
type ClassifierCaller struct {
	client  *UpstreamClient
	timeout time.Duration
}

// NewClassifierCaller 构造补全客户端。
func NewClassifierCaller(timeout time.Duration) *ClassifierCaller {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &ClassifierCaller{client: NewUpstreamClient(timeout), timeout: timeout}
}

// Call 发起一次非流式补全并返回文本。仅支持 chat_completions / messages 上游。
func (c *ClassifierCaller) Call(ctx context.Context, baseURL, apiKey string, providerType model.ProviderType, p model.Protocol, upstreamModel, system, user string, maxTokens int64) (string, error) {
	if p != model.ProtocolChatCompletions && p != model.ProtocolMessages {
		return "", fmt.Errorf("分类调用仅支持 chat_completions / messages 协议，当前 %s", p)
	}
	conv := Conversation{System: system, Messages: []Message{{Role: RoleUser, Text: user}}}
	params := ReqParams{Model: upstreamModel, MaxTokens: maxTokens}
	body, err := WriteUpstreamRequest(p, conv, params, upstreamModel)
	if err != nil {
		return "", fmt.Errorf("构造分类请求: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.client.Do(ctx, baseURL, providerType, apiKey, p, body, nil)
	if err != nil {
		return "", err
	}
	defer Drain(resp)
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("上游 HTTP %d: %s", resp.StatusCode, ExtractErrorDetail(data))
	}
	result, err := ParseUpstreamResponse(p, data)
	if err != nil {
		return "", fmt.Errorf("解析分类响应: %w", err)
	}
	return strings.TrimSpace(result.Text), nil
}

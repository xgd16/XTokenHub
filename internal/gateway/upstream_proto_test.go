package gateway

import (
	"testing"

	"xtokenhub/internal/model"
)

func TestUpstreamProtoFor(t *testing.T) {
	tests := []struct {
		name    string
		ch      model.Channel
		inbound model.Protocol
		want    model.Protocol
	}{
		{"openai chat 渠道承接 responses", model.Channel{Provider: model.ProviderOpenAICompatible, Protocols: "chat_completions"}, model.ProtocolResponses, model.ProtocolChatCompletions},
		{"openai 双协议优先 chat", model.Channel{Provider: model.ProviderOpenAICompatible, Protocols: "chat_completions,responses"}, model.ProtocolMessages, model.ProtocolChatCompletions},
		{"anthropic 渠道承接 chat", model.Channel{Provider: model.ProviderAnthropic, Protocols: "messages"}, model.ProtocolChatCompletions, model.ProtocolMessages},
		{"仅 responses 渠道暴露明确错误", model.Channel{Provider: model.ProviderOpenAICompatible, Protocols: "responses"}, model.ProtocolChatCompletions, model.ProtocolResponses},
		{"未探测渠道回退厂家默认", model.Channel{Provider: model.ProviderOpenAICompatible}, model.ProtocolMessages, model.ProtocolChatCompletions},
		{"未探测 anthropic 回退 messages", model.Channel{Provider: model.ProviderAnthropic}, model.ProtocolChatCompletions, model.ProtocolMessages},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := upstreamProtoFor(&tt.ch, tt.inbound); got != tt.want {
				t.Errorf("upstreamProtoFor(%q, %s) = %s, want %s", tt.ch.Protocols, tt.inbound, got, tt.want)
			}
		})
	}
}

func TestSelectCandidatesExcludesResponsesOnlyForConversion(t *testing.T) {
	channels := []model.Channel{
		{Name: "resp-only", Provider: model.ProviderOpenAICompatible, Models: "m", Protocols: "responses", Status: model.ChannelEnabled, Weight: 1},
		{Name: "chat-ch", Provider: model.ProviderOpenAICompatible, Models: "m", Protocols: "chat_completions", Status: model.ChannelEnabled, Weight: 1},
	}
	// chat 入站：仅 responses 的渠道无转换目标，应被排除
	cands := SelectCandidates(channels, model.ProtocolChatCompletions, "m", nil)
	if len(cands) != 1 || cands[0].Channel.Name != "chat-ch" {
		t.Fatalf("chat 入站候选 = %+v", cands)
	}
	// responses 入站：resp-only 原生透传，chat-ch 为转换候选
	cands = SelectCandidates(channels, model.ProtocolResponses, "m", nil)
	if len(cands) != 2 || cands[0].Channel.Name != "resp-only" || cands[0].Mode != model.ForwardNativePassthrough {
		t.Fatalf("responses 入站候选 = %+v", cands)
	}
	if cands[1].Mode != model.ForwardConverted {
		t.Errorf("chat-ch 应为转换候选: %+v", cands[1])
	}
}

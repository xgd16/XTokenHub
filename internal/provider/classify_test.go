package provider

import (
	"context"
	"testing"
	"time"

	"xtokenhub/internal/model"
)

func TestClassifierProtocol(t *testing.T) {
	tests := []struct {
		name string
		ch   *model.Channel
		want model.Protocol
	}{
		{"原生 chat 优先", &model.Channel{
			Provider: model.ProviderOpenAICompatible, Protocols: "responses,chat_completions",
		}, model.ProtocolChatCompletions},
		{"原生 messages", &model.Channel{
			Provider: model.ProviderAnthropic, Protocols: "messages",
		}, model.ProtocolMessages},
		{"未探测时按厂家推断(anthropic)", &model.Channel{
			Provider: model.ProviderAnthropic,
		}, model.ProtocolMessages},
		{"未探测时按厂家推断(openai)", &model.Channel{
			Provider: model.ProviderOpenAICompatible,
		}, model.ProtocolChatCompletions},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifierProtocol(tt.ch); got != tt.want {
				t.Errorf("ClassifierProtocol() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestClassifierCallerRejectsResponses(t *testing.T) {
	c := NewClassifierCaller(time.Second)
	if _, err := c.Call(context.Background(), "https://api.example.com", "sk", model.ProviderOpenAICompatible,
		model.ProtocolResponses, "m", "s", "u", 128); err == nil {
		t.Fatal("responses 协议应拒绝")
	}
	if _, err := c.Call(context.Background(), "://bad", "sk", model.ProviderOpenAICompatible,
		model.ProtocolChatCompletions, "m", "s", "u", 128); err == nil {
		t.Fatal("非法 BaseURL 应报错")
	}
}

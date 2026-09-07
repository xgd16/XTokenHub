package model_test

import (
	"testing"

	"xtokenhub/internal/model"
)

func TestProtocolValid(t *testing.T) {
	for _, p := range model.AllProtocols {
		if !p.Valid() {
			t.Errorf("%s 应为合法协议", p)
		}
	}
	if model.Protocol("bogus").Valid() {
		t.Error("非法协议应返回 false")
	}
}

func TestProviderTypeValid(t *testing.T) {
	if !model.ProviderOpenAICompatible.Valid() || !model.ProviderAnthropic.Valid() {
		t.Error("内置厂家类型应合法")
	}
	if model.ProviderType("openai").Valid() {
		t.Error("未知厂家类型应非法")
	}
}

func TestChannelProtocolsRoundtrip(t *testing.T) {
	ch := &model.Channel{}
	ch.SetProtocols([]model.Protocol{model.ProtocolMessages, model.ProtocolChatCompletions, model.Protocol("bogus"), model.ProtocolMessages})
	want := "chat_completions,messages"
	if ch.Protocols != want {
		t.Errorf("Protocols = %q, want %q", ch.Protocols, want)
	}
	if !ch.IsNative(model.ProtocolMessages) || ch.IsNative(model.ProtocolResponses) {
		t.Errorf("IsNative 判定错误: %v", ch.NativeProtocols())
	}
}

func TestSupportsModel(t *testing.T) {
	tests := []struct {
		name   string
		models string
		query  string
		want   bool
	}{
		{"空列表全支持", "", "gpt-4o", true},
		{"命中", "gpt-4o,claude-3", "claude-3", true},
		{"未命中", "gpt-4o", "claude-3", false},
		{"含空格仍命中", " gpt-4o , claude-3 ", "claude-3", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := &model.Channel{Models: tt.models}
			if got := ch.SupportsModel(tt.query); got != tt.want {
				t.Errorf("SupportsModel(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

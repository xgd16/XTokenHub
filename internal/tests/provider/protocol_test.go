package provider_test

import (
	"xtokenhub/internal/provider"

	"testing"

	"xtokenhub/internal/model"
)

func TestEndpointURL(t *testing.T) {
	tests := []struct {
		name    string
		base    string
		p       model.Protocol
		want    string
		wantErr bool
	}{
		{"域名根+chat", "https://api.openai.com", model.ProtocolChatCompletions, "https://api.openai.com/v1/chat/completions", false},
		{"带v1根+messages", "https://api.anthropic.com/v1/", model.ProtocolMessages, "https://api.anthropic.com/v1/messages", false},
		{"带v1根+responses", "https://x.com/v1", model.ProtocolResponses, "https://x.com/v1/responses", false},
		{"版本化挂载+chat(智谱)", "https://open.bigmodel.cn/api/paas/v4", model.ProtocolChatCompletions, "https://open.bigmodel.cn/api/paas/v4/chat/completions", false},
		{"版本化挂载带斜杠+responses", "https://open.bigmodel.cn/api/paas/v4/", model.ProtocolResponses, "https://open.bigmodel.cn/api/paas/v4/responses", false},
		{"版本化挂载+messages", "https://open.bigmodel.cn/api/paas/v4", model.ProtocolMessages, "https://open.bigmodel.cn/api/paas/v4/messages", false},
		{"伪版本段不命中", "https://x.com/v4chat", model.ProtocolChatCompletions, "https://x.com/v4chat/v1/chat/completions", false},
		{"http允许", "http://localhost:9000", model.ProtocolMessages, "http://localhost:9000/v1/messages", false},
		{"空URL", "  ", model.ProtocolChatCompletions, "", true},
		{"非法协议", "https://x.com", "nope", "", true},
		{"非http前缀", "ftp://x.com", model.ProtocolChatCompletions, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := provider.EndpointURL(tt.base, tt.p)
			if (err != nil) != tt.wantErr {
				t.Fatalf("provider.EndpointURL(%q) err = %v, wantErr %v", tt.base, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("provider.EndpointURL(%q) = %q, want %q", tt.base, got, tt.want)
			}
		})
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"https://a.com/", "https://a.com", true},
		{" https://a.com/v1 ", "https://a.com", true},
		{"https://A.com/v1", "https://A.com", true},
		{"", "", false},
		{"/only/path", "", false},
	}
	for _, tt := range tests {
		got, ok := provider.NormalizeBaseURL(tt.in)
		if ok != tt.ok || got != tt.want {
			t.Errorf("provider.NormalizeBaseURL(%q) = %q,%v want %q,%v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestAPIHeaders(t *testing.T) {
	anth := provider.APIHeaders(model.ProviderAnthropic, "key-1")
	if anth["x-api-key"] != "key-1" || anth["anthropic-version"] == "" {
		t.Errorf("anthropic 头错误: %v", anth)
	}
	if _, has := anth["Authorization"]; has {
		t.Error("anthropic 不应带 Authorization")
	}
	oa := provider.APIHeaders(model.ProviderOpenAICompatible, "key-2")
	if oa["Authorization"] != "Bearer key-2" {
		t.Errorf("openai 头错误: %v", oa)
	}
}

func TestBaselineProtocols(t *testing.T) {
	if len(provider.BaselineProtocols[model.ProviderAnthropic]) != 1 || provider.BaselineProtocols[model.ProviderAnthropic][0] != model.ProtocolMessages {
		t.Errorf("anthropic 基线错误: %v", provider.BaselineProtocols[model.ProviderAnthropic])
	}
	if len(provider.BaselineProtocols[model.ProviderOpenAICompatible]) != 1 || provider.BaselineProtocols[model.ProviderOpenAICompatible][0] != model.ProtocolChatCompletions {
		t.Errorf("openai 基线错误: %v", provider.BaselineProtocols[model.ProviderOpenAICompatible])
	}
}

func TestEncodeProtocols(t *testing.T) {
	got := provider.EncodeProtocols([]model.Protocol{model.ProtocolMessages, model.ProtocolChatCompletions})
	if got != "chat_completions,messages" {
		t.Errorf("provider.EncodeProtocols = %q", got)
	}
	if provider.EncodeProtocols(nil) != "" {
		t.Error("空列表应返回空串")
	}
}

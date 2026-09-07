package provider

import (
	"net/http"
	"regexp"
	"testing"

	"xtokenhub/internal/model"
)

const openCodeBase = "https://opencode.ai/zen"

func TestIsOpenCode(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"https://opencode.ai", true},
		{"https://opencode.ai/zen/v1", true},
		{"https://OPENCODE.ai/zen", true},
		{"https://api.opencode.ai/v1/", true},
		{"https://api.openai.com/v1", false},
		{"https://api.anthropic.com", false},
		{"https://x.com/api/opencode-proxy", true}, // 路径含 opencode 同样命中
		{"", false},
		{"not a url", false},
		{"ftp://opencode.ai", false},
	}
	for _, tt := range tests {
		if got := isOpenCode(tt.in); got != tt.want {
			t.Errorf("isOpenCode(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestOpenCodeUpstreamKey(t *testing.T) {
	tests := []struct {
		base, key, want string
	}{
		{openCodeBase, "", "public"},               // opencode 渠道未配置 key：兜底免费 key
		{openCodeBase, "  ", "public"},             // 空白 key 同样兜底
		{openCodeBase, "sk-zen-xxx", "sk-zen-xxx"}, // 显式配置优先（付费模型用真实 key）
		{"https://api.openai.com", "", ""},         // 非 opencode 渠道不兜底
		{"https://api.openai.com", "sk-x", "sk-x"},
	}
	for _, tt := range tests {
		if got := openCodeUpstreamKey(tt.base, tt.key); got != tt.want {
			t.Errorf("openCodeUpstreamKey(%q, %q) = %q, want %q", tt.base, tt.key, got, tt.want)
		}
	}
}

func TestApplyOpenCodeHeadersNoop(t *testing.T) {
	h := http.Header{"User-Agent": []string{"openai-python/1.82.0"}}
	applyOpenCodeHeaders("https://api.openai.com", h)
	if len(h) != 1 || h.Get("User-Agent") != "openai-python/1.82.0" {
		t.Errorf("非 opencode 渠道应保持原头不变，got %v", h)
	}
}

func TestApplyOpenCodeHeadersFill(t *testing.T) {
	h := http.Header{}
	applyOpenCodeHeaders(openCodeBase, h)

	if h.Get("User-Agent") != openCodeUserAgent {
		t.Errorf("User-Agent = %q, want %q", h.Get("User-Agent"), openCodeUserAgent)
	}
	if h.Get("x-opencode-client") != "cli" {
		t.Errorf("x-opencode-client = %q, want cli", h.Get("x-opencode-client"))
	}
	if h.Get("x-opencode-project") != openCodeProjectID {
		t.Errorf("x-opencode-project = %q, want %q", h.Get("x-opencode-project"), openCodeProjectID)
	}
	idRe := regexp.MustCompile(`^(msg|ses)_[0-9a-f]{12}[A-Za-z0-9]{14}$`)
	for _, k := range []string{"x-opencode-request", "x-opencode-session"} {
		if v := h.Get(k); !idRe.MatchString(v) {
			t.Errorf("%s = %q, 不符合 CLI 标识格式", k, v)
		}
	}
}

func TestApplyOpenCodeHeadersKeepExisting(t *testing.T) {
	h := http.Header{
		"X-Opencode-Client":  []string{"vscode"},
		"X-Opencode-Project": []string{"proj_custom"},
		"X-Opencode-Request": []string{"msg_inbound000000000000001"},
		"X-Opencode-Session": []string{"ses_inbound000000000000001"},
		"User-Agent":         []string{"opencode/1.2.3 custom-runtime"},
	}
	applyOpenCodeHeaders(openCodeBase, h)
	for k, want := range map[string]string{
		"x-opencode-client":  "vscode",
		"x-opencode-project": "proj_custom",
		"x-opencode-request": "msg_inbound000000000000001",
		"x-opencode-session": "ses_inbound000000000000001",
		"User-Agent":         "opencode/1.2.3 custom-runtime",
	} {
		if got := h.Get(k); got != want {
			t.Errorf("入站已携带的 %s 应保留原值: got %q, want %q", k, got, want)
		}
	}
}

func TestApplyOpenCodeHeadersRewriteGenericUA(t *testing.T) {
	// 泛用 SDK 的 UA 不属于 opencode 专属头：保留会暴露网关流量，需改写
	h := http.Header{"User-Agent": []string{"Go-http-client/2.0"}}
	applyOpenCodeHeaders(openCodeBase, h)
	if h.Get("User-Agent") != openCodeUserAgent {
		t.Errorf("泛用 UA 应改写为 CLI 特征串, got %q", h.Get("User-Agent"))
	}
}

func TestNewOpenCodeID(t *testing.T) {
	re := regexp.MustCompile(`^(msg|ses)_[0-9a-f]{12}[A-Za-z0-9]{14}$`)
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := newOpenCodeID("msg_")
		if !re.MatchString(id) {
			t.Fatalf("newOpenCodeID = %q, 不符合格式", id)
		}
		if seen[id] {
			t.Fatalf("newOpenCodeID 重复: %q", id)
		}
		seen[id] = true
	}
}

// TestSetUpstreamHeaders 验证 Do/DoStream 的头部装配序列：
// 鉴权兜底、Accept 区分、以及「入站已携带 opencode 专属头则保留原值」。
func TestSetUpstreamHeaders(t *testing.T) {
	inbound := http.Header{
		"User-Agent":         []string{"opencode/1.18.29 ai-sdk/provider-utils/4.0.23 runtime/bun/1.3.14"},
		"X-Opencode-Session": []string{"ses_inbound000000000000001"},
	}
	h := http.Header{}
	ForwardHeaders(h, inbound)
	setUpstreamHeaders(h, openCodeBase, model.ProviderOpenAICompatible, "", "application/json")

	if got := h.Get("Authorization"); got != "Bearer public" {
		t.Errorf("Authorization = %q, want Bearer public", got)
	}
	if h.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", h.Get("Content-Type"))
	}
	if h.Get("Accept") != "application/json" {
		t.Errorf("Accept = %q", h.Get("Accept"))
	}
	if got := h.Get("x-opencode-session"); got != "ses_inbound000000000000001" {
		t.Errorf("入站 session 应保留, got %q", got)
	}
	if got := h.Get("User-Agent"); got != openCodeUserAgent {
		t.Errorf("UA 应与入站一致, got %q", got)
	}

	// 流式：Accept 换为 event-stream
	s := http.Header{}
	setUpstreamHeaders(s, openCodeBase, model.ProviderOpenAICompatible, "sk-real", "text/event-stream")
	if s.Get("Accept") != "text/event-stream" {
		t.Errorf("流式 Accept = %q", s.Get("Accept"))
	}
	if got := s.Get("Authorization"); got != "Bearer sk-real" {
		t.Errorf("显式 key 应优先, got %q", got)
	}
}

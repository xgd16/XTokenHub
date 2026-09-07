package provider_test

import (
	"net/http"
	"testing"

	"xtokenhub/internal/provider"
)

func TestForwardHeaders(t *testing.T) {
	src := http.Header{}
	src.Set("X-Opencode-Session", "sess-123")
	src.Set("User-Agent", "opencode/1.0")
	src.Set("Authorization", "Bearer client-key")
	src.Set("X-Api-Key", "client-key")
	src.Set("Content-Type", "application/json")
	src.Set("Content-Length", "128")
	src.Set("Accept-Encoding", "gzip")
	src.Set("Connection", "keep-alive")
	src.Set("X-Forwarded-For", "1.2.3.4")

	dst := http.Header{}
	provider.ForwardHeaders(dst, src)

	if got := dst.Get("X-Opencode-Session"); got != "sess-123" {
		t.Fatalf("会话头未透传: %q", got)
	}
	if got := dst.Get("User-Agent"); got != "opencode/1.0" {
		t.Fatalf("User-Agent 未透传: %q", got)
	}
	for _, k := range []string{"Authorization", "X-Api-Key", "Content-Type", "Content-Length", "Accept-Encoding", "Connection", "X-Forwarded-For"} {
		if got := dst.Get(k); got != "" {
			t.Fatalf("保留头 %s 不应透传，实际: %q", k, got)
		}
	}
}

func TestForwardHeadersNil(t *testing.T) {
	dst := http.Header{}
	provider.ForwardHeaders(dst, nil) // 不应 panic
	if len(dst) != 0 {
		t.Fatalf("nil 源应产生空结果: %v", dst)
	}
}

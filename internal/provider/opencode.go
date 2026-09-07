package provider

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// opencode 官方 API 对免费模型按调用方特征限流：网关转发时模拟 opencode CLI 的
// 专属请求头，使上游流量与真实 CLI 调用一致；入站请求已携带同名头时保留原值
// （调用方本身就是 opencode 客户端直连网关的场景）。
const (
	// openCodeAuthKey opencode 免费模型的公开鉴权 key（渠道未配置 APIKey 时兜底）。
	openCodeAuthKey = "public"
	// openCodeUserAgent opencode CLI（Bun 运行时 + AI SDK）的 User-Agent 特征串。
	openCodeUserAgent = "opencode/1.18.29 ai-sdk/provider-utils/4.0.23 runtime/bun/1.3.14"
	// openCodeProjectID 随请求上报的项目标识（固定伪装值，入站可覆盖）。
	openCodeProjectID = "db3b61f151c806050fc07f1878d5f1ae5a93577e"
)

// isOpenCode 判断 URL 是否指向 opencode 官方 API（主机或路径含 opencode）。
func isOpenCode(rawURL string) bool {
	root, ok := NormalizeBaseURL(rawURL)
	if !ok {
		return false
	}
	u, err := url.Parse(root)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(u.Host+u.Path), "opencode")
}

// openCodeUpstreamKey 上游鉴权 key：opencode 渠道未配置 APIKey 时兜底免费 key public。
func openCodeUpstreamKey(baseURL, apiKey string) string {
	if strings.TrimSpace(apiKey) == "" && isOpenCode(baseURL) {
		return openCodeAuthKey
	}
	return apiKey
}

// applyOpenCodeHeaders 为发往 opencode 的请求补齐 CLI 专属请求头（非 opencode 渠道为空操作）：
//   - x-opencode-client/project/request/session：入站已携带则保留原值，缺失才补默认值；
//     request/session 缺省时按 CLI 标识格式逐请求生成；
//   - User-Agent：入站 UA 已具 opencode 特征时保留，否则改写为 CLI 特征串
//     （泛用 SDK 的 UA 不属于 opencode 专属头，透传会暴露网关流量）。
func applyOpenCodeHeaders(baseURL string, h http.Header) {
	if !isOpenCode(baseURL) {
		return
	}
	if h.Get("x-opencode-client") == "" {
		h.Set("x-opencode-client", "cli")
	}
	if h.Get("x-opencode-project") == "" {
		h.Set("x-opencode-project", openCodeProjectID)
	}
	if h.Get("x-opencode-request") == "" {
		h.Set("x-opencode-request", newOpenCodeID("msg_"))
	}
	if h.Get("x-opencode-session") == "" {
		h.Set("x-opencode-session", newOpenCodeID("ses_"))
	}
	if ua := h.Get("User-Agent"); !strings.Contains(strings.ToLower(ua), "opencode") {
		h.Set("User-Agent", openCodeUserAgent)
	}
}

// newOpenCodeID 生成 opencode CLI 风格的标识符：前缀 + 12 位小写 hex + 14 位大小写
// 字母数字（形如 msg_07b1fbb02001QEJtS3dqC18kTp / ses_f84e0453cffeNd8BQGyY3e7vDX）。
func newOpenCodeID(prefix string) string {
	const (
		hexDigits  = "0123456789abcdef"
		alnumChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
		hexLen     = 12
		alnumLen   = 14
	)
	b := make([]byte, hexLen+alnumLen)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败仅见于极端环境：时间戳兜底保证唯一性
		return fmt.Sprintf("%s%x", prefix, time.Now().UnixNano())
	}
	out := make([]byte, 0, len(prefix)+hexLen+alnumLen)
	out = append(out, prefix...)
	for i := range b[:hexLen] {
		out = append(out, hexDigits[int(b[i])%len(hexDigits)])
	}
	for _, c := range b[hexLen:] {
		out = append(out, alnumChars[int(c)%len(alnumChars)])
	}
	return string(out)
}

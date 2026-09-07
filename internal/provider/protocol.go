// Package provider 封装上游厂家对接：协议端点定位、原生协议探测、
// 协议转换（兜底路径）、usage/token 解析与本地估算。
package provider

import (
	"net/url"
	"strings"

	"xtokenhub/internal/model"
)

// EndpointPath 返回协议对应的 API 路径（相对 API 根，不含版本段时自动补 /v1）。
func EndpointPath(p model.Protocol) string {
	switch p {
	case model.ProtocolChatCompletions:
		return "chat/completions"
	case model.ProtocolResponses:
		return "responses"
	case model.ProtocolMessages:
		return "messages"
	default:
		return ""
	}
}

// NormalizeBaseURL 规范化渠道 BaseURL：
//   - 去除尾部斜杠；
//   - 兼容多种写法：带 /v1 结尾（OpenAI SDK 风格）、裸域名、带子路径挂载点
//     （如 https://api.deepseek.com/anthropic）与带版本段的挂载点
//     （如 https://open.bigmodel.cn/api/paas/v4），统一为「无 /v1 结尾的根」，
//     最终端点 = root + endpointPrefix(root) + path。
func NormalizeBaseURL(base string) (string, bool) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return "", false
	}
	lower := strings.ToLower(base)
	if strings.HasSuffix(lower, "/v1") {
		base = base[:len(base)-3]
	}
	if base == "" {
		return "", false
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		return "", false
	}
	return base, true
}

// versionedMount 判断 root 是否以 API 版本段结尾（如 /v2、/v4）。
// 此类挂载点（如智谱 https://open.bigmodel.cn/api/paas/v4）自身已含版本号，
// 端点直接拼资源路径，不再插入 /v1。root 已被 NormalizeBaseURL 去掉 /v1 结尾，
// 故此处不会命中 v1。
func versionedMount(root string) bool {
	i := strings.LastIndex(root, "/")
	seg := strings.ToLower(root[i+1:])
	if len(seg) < 2 || seg[0] != 'v' {
		return false
	}
	for _, r := range seg[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// endpointPrefix 端点路径前缀：版本化挂载点直接拼，其余补 /v1。
func endpointPrefix(root string) string {
	if versionedMount(root) {
		return "/"
	}
	return "/v1/"
}

// EndpointURL 构造上游端点完整 URL。
func EndpointURL(baseURL string, p model.Protocol) (string, error) {
	root, ok := NormalizeBaseURL(baseURL)
	if !ok {
		return "", errInvalidBaseURL(baseURL)
	}
	path := EndpointPath(p)
	if path == "" {
		return "", errUnsupportedProtocol(p)
	}
	return root + endpointPrefix(root) + path, nil
}

// InferProviderType 按 BaseURL 推断接口风格（挂载点约定：域名或路径含 anthropic
// 的端点使用 x-api-key 鉴权，如 https://api.deepseek.com/anthropic、https://api.anthropic.com），
// 其余按 OpenAI 兼容（Bearer）处理。推断结果仅为默认值，可被显式指定覆盖。
func InferProviderType(baseURL string) model.ProviderType {
	if root, ok := NormalizeBaseURL(baseURL); ok {
		if u, err := url.Parse(root); err == nil {
			lower := strings.ToLower(u.Host + u.Path)
			if strings.Contains(lower, "anthropic") {
				return model.ProviderAnthropic
			}
		}
	}
	return model.ProviderOpenAICompatible
}

// DefaultProbeModel 各厂家类型的探测兜底模型。
var DefaultProbeModel = map[model.ProviderType]string{
	model.ProviderOpenAICompatible: "gpt-4o-mini",
	model.ProviderAnthropic:        "claude-3-5-haiku-latest",
}

// BaselineProtocols 厂家类型的协议预设基线（探测结果在其上扩展）。
var BaselineProtocols = map[model.ProviderType][]model.Protocol{
	model.ProviderOpenAICompatible: {model.ProtocolChatCompletions},
	model.ProviderAnthropic:        {model.ProtocolMessages},
}

// APIHeaders 按厂家类型构造上游认证头（不含 Content-Type）。
func APIHeaders(provider model.ProviderType, apiKey string) map[string]string {
	switch provider {
	case model.ProviderAnthropic:
		return map[string]string{
			"x-api-key":         apiKey,
			"anthropic-version": "2023-06-01",
		}
	default:
		return map[string]string{
			"Authorization": "Bearer " + apiKey,
		}
	}
}

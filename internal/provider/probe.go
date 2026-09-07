package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"time"

	"xtokenhub/internal/model"
)

// ProbeItem 单协议探测结果。
type ProbeItem struct {
	Protocol model.Protocol `json:"protocol"`
	OK       bool           `json:"ok"`
	Status   int            `json:"status"`
	Detail   string         `json:"detail"`
}

// ProbeReport 一次渠道探测的完整报告。
type ProbeReport struct {
	ProbeModel      string           `json:"probe_model"`
	NativeProtocols []model.Protocol `json:"native_protocols"`
	Items           []ProbeItem      `json:"items"`
	ProbedAt        time.Time        `json:"probed_at"`
}

// EncodeProtocols 协议列表 -> 逗号串（按稳定顺序）。
func EncodeProtocols(ps []model.Protocol) string {
	if len(ps) == 0 {
		return ""
	}
	sorted := make([]model.Protocol, len(ps))
	copy(sorted, ps)
	sort.Slice(sorted, func(i, j int) bool { return protocolRank(sorted[i]) < protocolRank(sorted[j]) })
	b := bytes.Buffer{}
	for i, p := range sorted {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(string(p))
	}
	return b.String()
}

func protocolRank(p model.Protocol) int {
	for i, v := range model.AllProtocols {
		if v == p {
			return i
		}
	}
	return len(model.AllProtocols)
}

// Prober 原生协议探测器。
type Prober struct {
	client *UpstreamClient
	// perRequestTimeout 单次探测请求超时。
	perRequestTimeout time.Duration
}

// NewProber 构造探测器。
func NewProber(timeout time.Duration) *Prober {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Prober{client: NewUpstreamClient(timeout), perRequestTimeout: timeout}
}

// FamilyProtocols 各厂家协议家族（同厂家的全部协议端点）。
var FamilyProtocols = map[model.ProviderType][]model.Protocol{
	model.ProviderOpenAICompatible: {model.ProtocolChatCompletions, model.ProtocolResponses},
	model.ProviderAnthropic:        {model.ProtocolMessages},
}

// ProbeProtocols 本渠道应探测的协议集合：
//   - 裸域名 BaseURL：探测全部协议（中转站常同时原生支持多协议，自动识别）；
//   - 子路径 BaseURL（如 DeepSeek 的 https://api.deepseek.com/anthropic）：
//     子路径是协议挂载点，只探测该厂家协议家族，不向其发其它协议的探测请求。
func ProbeProtocols(providerType model.ProviderType, baseURL string) []model.Protocol {
	if root, ok := NormalizeBaseURL(baseURL); ok {
		if u, err := url.Parse(root); err == nil && u.Path != "" && u.Path != "/" {
			if family := FamilyProtocols[providerType]; len(family) > 0 {
				return family
			}
		}
	}
	return model.AllProtocols
}

// Probe 逐一探测各协议端点，判定渠道原生支持哪些协议。
// 成功判定：HTTP 2xx 且响应体可按该协议解析（或非空 JSON）。
// 仅以真实探测结果为准（401/404/5xx 均不算原生），需要修正时由用户手工编辑 native_protocols。
func (pr *Prober) Probe(ctx context.Context, baseURL, apiKey string, providerType model.ProviderType, probeModel string) *ProbeReport {
	if _, ok := NormalizeBaseURL(baseURL); !ok {
		return &ProbeReport{Items: []ProbeItem{{
			Protocol: model.ProtocolChatCompletions, OK: false, Detail: "BaseURL 非法",
		}}, ProbedAt: time.Now()}
	}
	if probeModel == "" {
		probeModel = DefaultProbeModel[providerType]
	}
	report := &ProbeReport{ProbeModel: probeModel, ProbedAt: time.Now()}

	for _, p := range ProbeProtocols(providerType, baseURL) {
		item := pr.probeOne(ctx, baseURL, apiKey, providerType, probeModel, p)
		report.Items = append(report.Items, item)
		if item.OK {
			report.NativeProtocols = append(report.NativeProtocols, p)
		}
	}
	return report
}

func (pr *Prober) probeOne(ctx context.Context, baseURL, apiKey string, providerType model.ProviderType, probeModel string, p model.Protocol) ProbeItem {
	item := ProbeItem{Protocol: p}
	endpoint, err := EndpointURL(baseURL, p)
	if err != nil {
		item.Detail = truncate(err.Error(), 256)
		return item
	}
	ctx, cancel := context.WithTimeout(ctx, pr.perRequestTimeout)
	defer cancel()

	body := minimalProbeBody(p, probeModel)
	if body == nil {
		item.Detail = "不支持的协议"
		return item
	}

	resp, err := pr.client.Do(ctx, baseURL, providerType, apiKey, p, body, nil)
	if err != nil {
		// 带上完整端点 URL，便于从探测详情直接定位实际请求路径
		item.Detail = truncate(fmt.Sprintf("POST %s: %s", endpoint, err.Error()), 256)
		return item
	}
	defer Drain(resp)
	item.Status = resp.StatusCode

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		item.OK = true
		item.Detail = "ok"
	default:
		item.Detail = truncate(fmt.Sprintf("POST %s → HTTP %d: %s", endpoint, resp.StatusCode, ExtractErrorDetail(data)), 256)
	}
	return item
}

// minimalProbeBody 各协议的最小探测请求。
func minimalProbeBody(p model.Protocol, probeModel string) []byte {
	switch p {
	case model.ProtocolChatCompletions:
		b, _ := json.Marshal(map[string]any{
			"model": probeModel,
			"messages": []map[string]string{
				{"role": "user", "content": "hi"},
			},
			"max_tokens": 1,
		})
		return b
	case model.ProtocolResponses:
		b, _ := json.Marshal(map[string]any{
			"model":             probeModel,
			"input":             "hi",
			"max_output_tokens": 16, // OpenAI 要求最小 16
		})
		return b
	case model.ProtocolMessages:
		b, _ := json.Marshal(map[string]any{
			"model": probeModel,
			"messages": []map[string]any{
				{"role": "user", "content": "hi"},
			},
			"max_tokens": 1,
		})
		return b
	default:
		return nil
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

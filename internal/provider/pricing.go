package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"xtokenhub/internal/model"
)

// LiteLLMPricingURL LiteLLM 公开价格表（MIT）：扁平 JSON，键为模型名，
// 费率为 USD / 单 token，含缓存读写与长上下文分档。
const LiteLLMPricingURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

// maxPricingBytes 价格表体积上限（当前约 2.4MB），防止异常响应打爆内存。
const maxPricingBytes = 64 << 20

// ModelPriceEntry 价格表中的一条记录（费率单位：该条目 Currency 对应的单 token 金额）。
//
// Currency / OffPeak* / PeakWindow 不来自公开价格表（LiteLLM 无这些字段），只为让
// 本地价格表行的附加信息能在内存索引里原样往返；同步写入时 Currency 恒为 USD。
type ModelPriceEntry struct {
	Model                   string
	InputCostPerToken       float64
	OutputCostPerToken      float64
	CacheReadCostPerToken   float64
	CacheWriteCostPerToken  float64
	ThresholdTokens         int64 // 长上下文分档阈值，0 = 无分档
	InputCostAbovePerToken  float64
	OutputCostAbovePerToken float64
	CacheReadAbovePerToken  float64
	CacheWriteAbovePerToken float64
	Provider                string

	// Currency 费率币种：USD | CNY（空按 USD）。
	Currency string
	// PeakWindow 高峰时段规则，模型专属；空 = 不启用时段价。
	PeakWindow                    model.PeakWindow
	OffPeakInputCostPerToken      float64
	OffPeakOutputCostPerToken     float64
	OffPeakCacheReadCostPerToken  float64
	OffPeakCacheWriteCostPerToken float64
}

// PricingClient 拉取公开价格表。
type PricingClient struct {
	http *http.Client
}

// NewPricingClient 构造；timeout <= 0 时取 20 秒。
func NewPricingClient(timeout time.Duration) *PricingClient {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &PricingClient{http: &http.Client{Timeout: timeout}}
}

// Fetch 拉取并解析价格表。url 为空时使用 LiteLLMPricingURL。
func (c *PricingClient) Fetch(ctx context.Context, url string) ([]ModelPriceEntry, error) {
	if url == "" {
		url = LiteLLMPricingURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("构造价格表请求: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("拉取价格表: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("价格表返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPricingBytes))
	if err != nil {
		return nil, fmt.Errorf("读取价格表: %w", err)
	}
	entries, err := ParsePricing(body)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("价格表解析结果为空")
	}
	return entries, nil
}

// 非文本生成类条目与网关无关，同步时跳过。
var skipModes = map[string]bool{
	"embedding":           true,
	"image_generation":    true,
	"audio_transcription": true,
	"audio_speech":        true,
	"moderation":          true,
	"rerank":              true,
	"search":              true,
	"ocr":                 true,
	"realtime":            true,
	"video_generation":    true,
	"code_interpreter":    true,
	"computer_use":        true,
}

// aboveThresholdRe 超阈值费率键，例如 input_cost_per_token_above_200k_tokens。
// 注意 "above_1hr_above_200k_tokens"（1 小时缓存写档）不会匹配 —— 数字必须紧跟 above_。
var aboveThresholdRe = regexp.MustCompile(`^(input_cost_per_token|output_cost_per_token|cache_read_input_token_cost|cache_creation_input_token_cost)_above_(\d+)k_tokens$`)

// dateSuffixRe 模型名尾部日期版本（如 claude-sonnet-4-20250514）。
var dateSuffixRe = regexp.MustCompile(`-(?:19|20)\d{6}$`)

// ParsePricing 解析价格表 JSON。无价格的条目与网关用不到的非文本生成模式会被跳过。
func ParsePricing(body []byte) ([]ModelPriceEntry, error) {
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析价格表 JSON: %w", err)
	}

	entries := make([]ModelPriceEntry, 0, len(raw))
	for name, rec := range raw {
		// sample_spec 是字段说明样例，不是真实模型
		if name == "sample_spec" || strings.TrimSpace(name) == "" {
			continue
		}
		if skipModes[jsonString(rec["mode"])] {
			continue
		}
		e := ModelPriceEntry{
			Model:                  name,
			InputCostPerToken:      jsonFloat(rec["input_cost_per_token"]),
			OutputCostPerToken:     jsonFloat(rec["output_cost_per_token"]),
			CacheReadCostPerToken:  jsonFloat(rec["cache_read_input_token_cost"]),
			CacheWriteCostPerToken: jsonFloat(rec["cache_creation_input_token_cost"]),
			Provider:               jsonString(rec["litellm_provider"]),
		}
		if e.InputCostPerToken <= 0 && e.OutputCostPerToken <= 0 {
			continue // 无价条目（含免费模型）不落表，交由「未定价模型」提示
		}
		applyAboveThresholds(&e, rec)
		entries = append(entries, e)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("价格表中没有可用条目")
	}
	return entries, nil
}

// applyAboveThresholds 从记录里挑出长上下文分档：按阈值分组，取最小阈值那一组。
// 多档（tiered_pricing）只保留第一档，超出部分按该档费率计费，属已知近似。
func applyAboveThresholds(e *ModelPriceEntry, rec map[string]json.RawMessage) {
	thresholds := map[int64]bool{}
	for key := range rec {
		if m := aboveThresholdRe.FindStringSubmatch(key); m != nil {
			if n, err := strconv.ParseInt(m[2], 10, 64); err == nil && n > 0 {
				thresholds[n*1000] = true
			}
		}
	}
	if len(thresholds) == 0 {
		return
	}
	all := make([]int64, 0, len(thresholds))
	for t := range thresholds {
		all = append(all, t)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	chosen := all[0]

	e.ThresholdTokens = chosen
	e.InputCostAbovePerToken = jsonFloat(rec[aboveKey("input_cost_per_token", chosen)])
	e.OutputCostAbovePerToken = jsonFloat(rec[aboveKey("output_cost_per_token", chosen)])
	e.CacheReadAbovePerToken = jsonFloat(rec[aboveKey("cache_read_input_token_cost", chosen)])
	e.CacheWriteAbovePerToken = jsonFloat(rec[aboveKey("cache_creation_input_token_cost", chosen)])
}

func aboveKey(base string, thresholdTokens int64) string {
	return fmt.Sprintf("%s_above_%dk_tokens", base, thresholdTokens/1000)
}

// IndexPrices 建立小写模型名索引。
//
// 两轮写入以保证优先级：先放完整名（canonical 名），再补「去厂商前缀 / 去日期后缀」的别名，
// 且只在别名尚未占用时写入。这样 "gpt-4o" 始终指向无前缀的规范条目，
// 而只以带前缀形式收录的模型（如 openrouter/meta/llama-3）也能被裸名匹配到。
func IndexPrices(entries []ModelPriceEntry) map[string]ModelPriceEntry {
	idx := make(map[string]ModelPriceEntry, len(entries))
	for _, e := range entries {
		key := strings.ToLower(strings.TrimSpace(e.Model))
		if key == "" {
			continue
		}
		if _, exists := idx[key]; !exists {
			idx[key] = e
		}
	}
	for _, e := range entries {
		key := strings.ToLower(strings.TrimSpace(e.Model))
		if key == "" {
			continue
		}
		for _, alias := range []string{
			trimVendorPrefix(key),
			dateSuffixRe.ReplaceAllString(key, ""),
			dateSuffixRe.ReplaceAllString(trimVendorPrefix(key), ""),
		} {
			if alias == "" {
				continue
			}
			if _, exists := idx[alias]; !exists {
				idx[alias] = e
			}
		}
	}
	return idx
}

// MatchPrice 按候选名依次匹配价格：完整名 -> 去厂商前缀末段 -> 去日期后缀。
// 全部未命中返回 false，调用方按「未定价」处理（费用记 0 并在设置页提示）。
func MatchPrice(index map[string]ModelPriceEntry, model string) (ModelPriceEntry, bool) {
	for _, cand := range candidateNames(model) {
		if e, ok := index[cand]; ok {
			return e, true
		}
	}
	return ModelPriceEntry{}, false
}

// candidateNames 归一化候选名（已小写、去重、保持优先级）。
func candidateNames(model string) []string {
	name := strings.ToLower(strings.TrimSpace(model))
	if name == "" {
		return nil
	}
	out := make([]string, 0, 4)
	seen := map[string]bool{}
	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	add(name)
	add(trimVendorPrefix(name))
	add(dateSuffixRe.ReplaceAllString(name, ""))
	add(dateSuffixRe.ReplaceAllString(trimVendorPrefix(name), ""))
	return out
}

// trimVendorPrefix 去掉 "provider/model" 形式的厂商前缀，保留最后一段。
func trimVendorPrefix(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 && i+1 < len(name) {
		return name[i+1:]
	}
	return name
}

func jsonFloat(raw json.RawMessage) float64 {
	if len(raw) == 0 {
		return 0
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return f
	}
	return 0
}

func jsonString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.ToLower(strings.TrimSpace(s))
	}
	return ""
}

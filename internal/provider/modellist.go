package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"xtokenhub/internal/model"
)

// ModelsURL 构造上游模型列表端点（OpenAI 兼容与 Anthropic 同为 GET …/models，
// 路径前缀随挂载点形态：版本化挂载点直接拼，其余补 /v1）。
func ModelsURL(baseURL string) (string, error) {
	root, ok := NormalizeBaseURL(baseURL)
	if !ok {
		return "", errInvalidBaseURL(baseURL)
	}
	return root + endpointPrefix(root) + "models", nil
}

// modelListCandidates 模型列表候选端点（按优先级）：
// 子路径 BaseURL（如 DeepSeek 的 https://api.deepseek.com/anthropic）先试自身的 models，
// 不可用再降级尝试主域名 /v1/models（部分厂家仅在主域名提供列表接口）。
func modelListCandidates(baseURL string) ([]string, error) {
	self, err := ModelsURL(baseURL)
	if err != nil {
		return nil, err
	}
	candidates := []string{self}
	root, ok := NormalizeBaseURL(baseURL)
	if !ok {
		return nil, errInvalidBaseURL(baseURL)
	}
	if u, err := url.Parse(root); err == nil && u.Path != "" && u.Path != "/" {
		candidates = append(candidates, u.Scheme+"://"+u.Host+"/v1/models")
	}
	return candidates, nil
}

// FetchModels 拉取上游可用模型 ID 列表（仅渠道自身的 /v1/models）。
// 探测选模的第一优先级：列表不可用时应退回渠道配置的模型，而非主域名兜底列表。
func (pr *Prober) FetchModels(ctx context.Context, baseURL string, providerType model.ProviderType, apiKey string) ([]string, error) {
	url, err := ModelsURL(baseURL)
	if err != nil {
		return nil, err
	}
	return pr.fetchModelsAt(ctx, url, providerType, apiKey, false)
}

// FetchModelsFallback 带 main-domain 兜底的模型列表拉取：
// 先试渠道自身 /v1/models，失败（或为空）再试主域名 /v1/models。
// 主域名兜底属于跨协议面的猜测性请求，同时携带两种鉴权头
// （如 anthropic 渠道降级到厂家 OpenAI 面的 /v1/models 时需要 Bearer）。
// 用于管理端「拉取模型列表」与探测选模的最后一级兜底。
func (pr *Prober) FetchModelsFallback(ctx context.Context, baseURL string, providerType model.ProviderType, apiKey string) ([]string, error) {
	candidates, err := modelListCandidates(baseURL)
	if err != nil {
		return nil, err
	}
	var firstErr error
	var empty []string
	for i, u := range candidates {
		ms, err := pr.fetchModelsAt(ctx, u, providerType, apiKey, i > 0)
		if err == nil {
			if len(ms) > 0 {
				return ms, nil
			}
			empty = ms // 某级返回空列表：记录，继续尝试下一级
			continue
		}
		if firstErr == nil {
			firstErr = err // 保留首个（渠道自身端点）的错误，最具诊断价值
		}
	}
	if empty != nil {
		return empty, nil
	}
	return nil, firstErr
}

// fetchModelsAt 请求单个模型列表端点并解析。dualAuth 时额外补另一家的鉴权头。
func (pr *Prober) fetchModelsAt(ctx context.Context, url string, providerType model.ProviderType, apiKey string, dualAuth bool) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, pr.perRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("构造请求: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range APIHeaders(providerType, openCodeUpstreamKey(url, apiKey)) {
		req.Header.Set(k, v)
	}
	applyOpenCodeHeaders(url, req.Header)
	if dualAuth {
		if providerType == model.ProviderAnthropic {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		} else {
			req.Header.Set("x-api-key", apiKey)
		}
	}
	resp, err := pr.client.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("上游请求失败: %w", err)
	}
	defer Drain(resp)

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s → HTTP %d: %s", url, resp.StatusCode, truncate(ExtractErrorDetail(data), 200))
	}
	return parseModelList(data)
}

// parseModelList 解析模型列表响应：{"data":[{"id":...}]} 或裸数组 [{"id":...}]。
func parseModelList(data []byte) ([]string, error) {
	type idEntry struct {
		ID string `json:"id"`
	}
	var envelope struct {
		Data []idEntry `json:"data"`
	}
	var entries []idEntry
	if err := json.Unmarshal(data, &envelope); err == nil {
		entries = envelope.Data
	} else if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("解析模型列表失败: %w", err)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.ID == "" || seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		out = append(out, e.ID)
	}
	return out, nil
}

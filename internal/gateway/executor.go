package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync/atomic"
	"time"

	"xtokenhub/internal/eventbus"
	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/logger"
	"xtokenhub/internal/provider"
)

// ChannelSource 渠道来源接口（由 repository 实现，测试可 mock）。
type ChannelSource interface {
	ListEnabled(ctx context.Context) ([]model.Channel, error)
}

// CustomModelSource 自定义模型组数据源接口（由 repository 实现，测试可 mock）。
type CustomModelSource interface {
	ListEnabled(ctx context.Context) ([]model.CustomModel, error)
}

// LogSink 日志落库接口。
type LogSink interface {
	Create(ctx context.Context, log *model.RequestLog) error
}

// Request 一次网关请求的上下文。
type Request struct {
	Protocol     model.Protocol         // 入站协议
	Conv         *provider.Conversation // 入站请求解析结果（供估算与转换）
	Params       provider.ReqParams
	Body         []byte // 原始入站请求体
	ClientIP     string
	UserAgent    string      // 调用方 User-Agent，识别 agent 工具
	SessionID    string      // 调用方会话标识（入站 X-Session-Id 头）
	ClientHeader http.Header // 入站请求头（透传会话路由类头到上游，保留头除外）
	KeyID        int64       // 调用方密钥（0 = 匿名）
	KeyName      string      // 调用方密钥名快照
}

// Executor 网关执行器。
type Executor struct {
	channels     ChannelSource
	customs      CustomModelSource // 自定义模型组（nil = 未启用分组路由）
	logs         LogSink
	bus          *eventbus.Bus
	client       *provider.UpstreamClient
	maxRespBytes int64 // 非流式响应读取上限
	throughput   *Throughput
	reqSeq       atomic.Int64 // 进程内请求序号（进行中/完成事件配对）
}

// NewExecutor 构造执行器。
func NewExecutor(channels ChannelSource, logs LogSink, bus *eventbus.Bus, upstreamTimeout time.Duration) *Executor {
	return &Executor{
		channels:     channels,
		logs:         logs,
		bus:          bus,
		client:       provider.NewUpstreamClient(upstreamTimeout),
		maxRespBytes: 32 << 20,
	}
}

// SetThroughput 注入实时吞吐跟踪器（nil 表示不统计）。
func (e *Executor) SetThroughput(t *Throughput) { e.throughput = t }

// SetCustomModels 注入自定义模型组数据源（nil 表示不启用分组路由）。
func (e *Executor) SetCustomModels(src CustomModelSource) { e.customs = src }

// AvailableModels 聚合全部启用渠道显式配置的模型与启用的自定义模型 ID（去重、字典序排序），
// 供 GET /v1/models 对外暴露能力清单。渠道 models 为空的视为「支持全部」，无法枚举，不产生条目。
func (e *Executor) AvailableModels(ctx context.Context) ([]string, error) {
	channels, err := e.channels.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for i := range channels {
		if channels[i].Status != model.ChannelEnabled {
			continue
		}
		for _, m := range channels[i].ModelList() {
			if m != "" && !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	if e.customs != nil {
		groups, gerr := e.customs.ListEnabled(ctx)
		if gerr != nil {
			return nil, gerr
		}
		for i := range groups {
			if groups[i].Status != model.ChannelEnabled {
				continue
			}
			if n := groups[i].Name; n != "" && !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// Handle 处理请求并将上游响应写到 w；完成后落库并推送事件。
// 失败处理契约：在向客户端写出任何字节之前失败 -> 尝试下一候选（failover）；
// 已开始写出后失败 -> 终止，记录错误日志。
func (e *Executor) Handle(ctx context.Context, w http.ResponseWriter, req *Request) {
	start := time.Now()
	log := &model.RequestLog{
		Protocol:  req.Protocol,
		Model:     req.Params.Model,
		Stream:    req.Params.Stream,
		ClientIP:  req.ClientIP,
		UserAgent: truncate(req.UserAgent, 250),
		SessionID: truncate(req.SessionID, 120),
		KeyID:     req.KeyID,
		KeyName:   req.KeyName,
	}
	// 序列化请求头用于调试
	if req.ClientHeader != nil {
		if b, err := json.Marshal(req.ClientHeader); err == nil {
			log.RequestHeaders = string(b)
		}
	}
	// 受理即广播：前端先渲染"运行中"行，完成后按 ReqID 原位更新。
	// 发布快照副本：log 随后还会被写入（错误路径），总线是延迟序列化的。
	log.ReqID = e.reqSeq.Add(1)
	log.CreatedAt = start
	if e.bus != nil {
		started := *log
		e.bus.Publish(eventbus.EventRequestStarted, &started)
	}

	channels, err := e.channels.ListEnabled(ctx)
	if err != nil {
		logger.L("gateway").Error("list channels", logger.Err(err))
		writeJSONError(w, http.StatusInternalServerError, req.Protocol, "内部错误")
		return
	}

	group, err := e.lookupGroup(ctx, req.Params.Model)
	if err != nil {
		logger.L("gateway").Error("lookup custom model group", logger.Err(err))
		writeJSONError(w, http.StatusInternalServerError, req.Protocol, "内部错误")
		return
	}

	candidates := SelectCandidates(channels, req.Protocol, req.Params.Model, group)
	if len(candidates) == 0 {
		log.Error = "no available channel"
		log.DurationMS = time.Since(start).Milliseconds()
		e.finish(ctx, log)
		writeJSONError(w, http.StatusServiceUnavailable, req.Protocol, "没有可用渠道")
		return
	}

	var lastErr error
	var lastAttempt *model.RequestLog
	clientGone := false
loop:
	for _, cand := range candidates {
		attempt := *log
		attempt.ChannelID = cand.Channel.ID
		attempt.ChannelName = cand.Channel.Name
		attempt.ForwardMode = cand.Mode
		// 分组路由时记录实际使用的成员模型（请求日志的 Model 始终等于发往上游的模型）
		if cand.UpstreamModel != "" {
			attempt.Model = cand.UpstreamModel
		}
		lastAttempt = &attempt

		var handlerErr error
		if req.Params.Stream {
			handlerErr = e.handleStream(ctx, w, req, cand, &attempt, start)
		} else {
			handlerErr = e.handleNonStream(ctx, w, req, cand, &attempt, start)
		}

		switch {
		case handlerErr == nil:
			return // 已完成（成功或已把错误回传客户端，日志已落库）
		case attempt.Committed:
			// 已向客户端写出（流式中途失败）：日志已在 handleStream 内落库
			return
		case ctx.Err() != nil:
			// 客户端已断开（inbound ctx 取消会连带取消上游请求）：
			// 后续候选必然同样失败，不再空转
			clientGone = true
			lastErr = handlerErr
			break loop
		default:
			lastErr = handlerErr
			logger.L("gateway").Warn("candidate failed, trying next",
				"channel", cand.Channel.Name, "mode", cand.Mode, logger.Err(handlerErr))
		}
	}

	// 所有候选失败：沿用最后一次尝试的渠道信息落库
	if lastAttempt == nil {
		lastAttempt = log
	}
	if clientGone {
		lastAttempt.Error = truncate("客户端连接中断", 900)
	} else {
		lastAttempt.Error = truncate("所有渠道均失败: "+errString(lastErr), 900)
	}
	lastAttempt.DurationMS = time.Since(start).Milliseconds()
	e.finish(ctx, lastAttempt)
	writeJSONError(w, http.StatusBadGateway, req.Protocol, "所有渠道均失败")
}

func errString(err error) string {
	if err == nil {
		return "unknown"
	}
	return err.Error()
}

// lookupGroup 查找与请求模型名匹配的启用自定义模型组；未启用或无匹配返回 nil。
func (e *Executor) lookupGroup(ctx context.Context, name string) (*model.CustomModel, error) {
	if e.customs == nil || name == "" {
		return nil, nil
	}
	groups, err := e.customs.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	for i := range groups {
		if groups[i].Name == name {
			return &groups[i], nil
		}
	}
	return nil, nil
}

// upstreamModelOf 候选实际发往上游的模型（分组路由为成员模型，普通请求为入站模型）。
func upstreamModelOf(cand Candidate, req *Request) string {
	if cand.UpstreamModel != "" {
		return cand.UpstreamModel
	}
	return req.Params.Model
}

// withModel 将请求体顶层 "model" 字段替换为实际上游模型（自定义模型组路由用）。
// 解析失败时原样返回，由上游报错触发 failover。
func withModel(body []byte, name string) []byte {
	if name == "" {
		return body
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	raw, err := json.Marshal(name)
	if err != nil {
		return body
	}
	m["model"] = raw
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

// handleNonStream 非流式：缓冲上游响应 -> 统计 -> (转换) -> 写客户端。
// 返回 error 仅表示"可 failover 的失败"；完成回写（含错误回传）返回 nil。
func (e *Executor) handleNonStream(ctx context.Context, w http.ResponseWriter, req *Request, cand Candidate, log *model.RequestLog, start time.Time) error {
	ch := cand.Channel
	up := upstreamProtoFor(ch)
	upModel := upstreamModelOf(cand, req)

	var upstreamBody []byte
	var err error
	if cand.Mode == model.ForwardNativePassthrough {
		var status int
		upstreamBody, status, err = e.sendNonStream(ctx, ch, req.Protocol, withModel(req.Body, upModel), req.ClientHeader)
		log.UpstreamStatus = status
	} else {
		var convBody []byte
		var status int
		convBody, status, err = e.sendConverted(ctx, ch, up, req, upModel)
		upstreamBody = convBody
		log.UpstreamStatus = status
	}
	if err != nil {
		return err // 网络/构造失败：可 failover
	}

	if !isSuccessStatus(log.UpstreamStatus) {
		detail := provider.ExtractErrorDetail(upstreamBody)
		if retryableStatus(log.UpstreamStatus) {
			return fmt.Errorf("upstream HTTP %d: %s", log.UpstreamStatus, truncate(detail, 200))
		}
		// 不可重试（4xx 语义）：回传客户端并记录
		log.Error = truncate(detail, 900)
		log.DurationMS = time.Since(start).Milliseconds()
		e.finish(ctx, log)
		forwardErrorResponse(w, req.Protocol, log.UpstreamStatus, log.Error)
		return nil
	}

	// usage 统计
	log.DurationMS = time.Since(start).Milliseconds()
	usage, ok := provider.ParseUsage(up, upstreamBody)
	if ok {
		log.PromptTokens = usage.PromptTokens
		log.CompletionTokens = usage.CompletionTokens
		log.CachedTokens = usage.CachedTokens
		log.CacheWriteTokens = usage.CacheWriteTokens
		log.TotalTokens = usage.TotalTokens
	} else {
		// 上游未报告：本地估算（completion 尝试从响应文本提取）
		est := e.estimateUsage(req, nil)
		if res, perr := provider.ParseUpstreamResponse(up, upstreamBody); perr == nil && res.Text != "" {
			est.CompletionTokens = provider.EstimateTokens(res.Text)
		}
		applyUsage(log, est)
	}
	if e.throughput != nil {
		e.throughput.Add(log.CompletionTokens)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(log.UpstreamStatus)
	if cand.Mode == model.ForwardNativePassthrough {
		_, _ = w.Write(upstreamBody)
	} else {
		clientBody, _, cerr := provider.ConvertResponse(req.Protocol, up, upstreamBody, req.Params.Model)
		if cerr != nil {
			log.Error = truncate("响应转换失败: "+cerr.Error(), 900)
			e.finish(ctx, log)
			writeJSONError(w, http.StatusBadGateway, req.Protocol, log.Error)
			return nil
		}
		_, _ = w.Write(clientBody)
	}
	e.finish(ctx, log)
	return nil
}

// handleStream 流式：逐块转发 + 旁路统计。
// 返回 error 仅表示"写出前的失败（可 failover）"；一旦开始写出，内部自行收尾。
func (e *Executor) handleStream(ctx context.Context, w http.ResponseWriter, req *Request, cand Candidate, log *model.RequestLog, start time.Time) error {
	ch := cand.Channel
	up := upstreamProtoFor(ch)
	upModel := upstreamModelOf(cand, req)

	var upstreamBody []byte
	var err error
	if cand.Mode == model.ForwardNativePassthrough {
		upstreamBody = withModel(req.Body, upModel)
	} else {
		upstreamBody, err = e.sendConvertedBody(ch, up, req, upModel)
		if err != nil {
			return err
		}
	}

	resp, err := e.client.DoStream(ctx, ch.BaseURL, ch.Provider, ch.APIKey, up, upstreamBody, req.ClientHeader)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		provider.Drain(resp)
		log.UpstreamStatus = resp.StatusCode
		detail := provider.ExtractErrorDetail(data)
		if retryableStatus(resp.StatusCode) {
			return fmt.Errorf("upstream HTTP %d: %s", resp.StatusCode, truncate(detail, 200))
		}
		log.Error = truncate(detail, 900)
		log.DurationMS = time.Since(start).Milliseconds()
		e.finish(ctx, log)
		forwardErrorResponse(w, req.Protocol, resp.StatusCode, log.Error)
		return nil
	}
	defer resp.Body.Close()

	// ---- 从这里开始向客户端写出（committed）----
	log.Committed = true
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	log.UpstreamStatus = resp.StatusCode

	var parser provider.StreamUsageParser
	var converter provider.StreamConverter
	if cand.Mode == model.ForwardNativePassthrough {
		parser = provider.NewStreamParser(up)
	} else {
		converter, err = provider.NewStreamConverter(req.Protocol, up)
		if err != nil {
			// 构造转换器失败属配置问题，但已 committed：收尾落库
			log.Error = truncate("流式转换构造失败: "+err.Error(), 900)
			log.DurationMS = time.Since(start).Milliseconds()
			e.finish(ctx, log)
			return nil
		}
	}

	// 实时吞吐：会话进行中上报当前已生成的 completion token；结束时用精确值补齐。
	var sid int64
	var finalCompletion int64
	if e.throughput != nil {
		sid = e.throughput.StreamBegin()
		defer func() {
			if e.throughput != nil && sid != 0 {
				e.throughput.StreamUpdate(sid, finalCompletion)
				e.throughput.StreamEnd(sid)
			}
		}()
	}

	// 当前累计输出文本获取函数（透传走 parser，转换走 converter）。
	var textFn func() string
	if converter != nil {
		textFn = converter.Text
	} else if parser != nil {
		textFn = parser.Text
	}

	// SSE 分割：透传模式旁路解析；转换模式同时产出客户端事件
	splitter := NewSSESplitter(func(data []byte) {
		if parser != nil {
			_ = parser.Feed(data)
		}
		if converter != nil {
			chunks, ferr := converter.Feed(data)
			if ferr == nil {
				for _, chunk := range chunks {
					writeSSEData(w, flusher, chunk)
				}
			}
		}
		if e.throughput != nil && sid != 0 && textFn != nil {
			// 每块估算一次 completion token（估算基于累计文本，成本 O(len)；块频不高可接受）
			e.throughput.StreamUpdate(sid, provider.EstimateTokens(textFn()))
		}
	})

	buf := make([]byte, 32*1024)
	clientBroken := false
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if cand.Mode == model.ForwardNativePassthrough {
				if _, werr := w.Write(buf[:n]); werr != nil {
					clientBroken = true
					log.Error = truncate("客户端连接中断: "+werr.Error(), 900)
					break
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
			_, _ = splitter.Write(buf[:n])
		}
		if rerr != nil {
			if rerr != io.EOF && !clientBroken {
				if ctx.Err() != nil {
					// inbound ctx 取消（客户端断开）会连带中断上游流读取
					clientBroken = true
					log.Error = "客户端连接中断"
				} else {
					log.Error = truncate("上游流读取中断: "+rerr.Error(), 900)
				}
			}
			break
		}
	}
	splitter.Flush()

	// 收尾：转换模式补齐客户端协议结束事件
	var usage provider.Usage
	var usageOK bool
	if converter != nil {
		done, u := converter.Finalize()
		if !clientBroken {
			for _, chunk := range done {
				writeSSEData(w, flusher, chunk)
			}
		}
		usage, usageOK = u, true
	} else if parser != nil {
		usage, usageOK = parser.Usage()
	}
	if usageOK || (usage.PromptTokens > 0 || usage.CompletionTokens > 0) {
		applyUsage(log, usage)
	} else {
		applyUsage(log, e.estimateUsage(req, parser))
	}
	finalCompletion = log.CompletionTokens // 供吞吐会话收尾用精确值补齐
	log.DurationMS = time.Since(start).Milliseconds()
	// 流式无论成功/中断均已 committed，统一在此落库
	e.finish(ctx, log)
	return nil
}

// sendNonStream 发送非流式请求并读取完整响应体；状态码写入 log。
func (e *Executor) sendNonStream(ctx context.Context, ch *model.Channel, p model.Protocol, body []byte, clientHeader http.Header) ([]byte, int, error) {
	resp, err := e.client.Do(ctx, ch.BaseURL, ch.Provider, ch.APIKey, p, body, clientHeader)
	if err != nil {
		return nil, 0, fmt.Errorf("上游请求失败: %w", err)
	}
	defer provider.Drain(resp)
	data, err := io.ReadAll(io.LimitReader(resp.Body, e.maxRespBytes))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("读取上游响应: %w", err)
	}
	return data, resp.StatusCode, nil
}

// sendConverted 转换请求后发送（非流式）。
func (e *Executor) sendConverted(ctx context.Context, ch *model.Channel, up model.Protocol, req *Request, upModel string) ([]byte, int, error) {
	body, err := e.sendConvertedBody(ch, up, req, upModel)
	if err != nil {
		return nil, 0, err
	}
	return e.sendNonStream(ctx, ch, up, body, req.ClientHeader)
}

// sendConvertedBody 构造转换后的上游请求体（upModel 为实际上游模型）。
func (e *Executor) sendConvertedBody(ch *model.Channel, up model.Protocol, req *Request, upModel string) ([]byte, error) {
	body, err := provider.ConvertRequest(req.Protocol, up, req.Body, upModel)
	if err != nil {
		return nil, fmt.Errorf("请求转换失败: %w", err)
	}
	return body, nil
}

// estimateUsage 本地估算兜底：prompt 来自入站文本，completion 来自流式累积文本。
func (e *Executor) estimateUsage(req *Request, parser provider.StreamUsageParser) provider.Usage {
	u := provider.Usage{Source: provider.UsageFromEstimate}
	if req.Conv != nil {
		u.PromptTokens = provider.EstimateTokens(req.Conv.FullText())
	}
	if parser != nil {
		u.CompletionTokens = provider.EstimateTokens(parser.Text())
	}
	return u.Normalize()
}

// applyUsage 将 usage 写入日志并计算缓存命中率。
func applyUsage(log *model.RequestLog, u provider.Usage) {
	log.PromptTokens = u.PromptTokens
	log.CompletionTokens = u.CompletionTokens
	log.TotalTokens = u.TotalTokens
	if u.CachedTokens > 0 {
		log.CachedTokens = u.CachedTokens
	}
	if u.CacheWriteTokens > 0 {
		log.CacheWriteTokens = u.CacheWriteTokens
	}
	if log.PromptTokens > 0 {
		log.CacheHitRate = float64(log.CachedTokens) / float64(log.PromptTokens)
	}
}

// finish 落库并推送实时事件。落库使用与请求解耦的 context：
// 客户端断开（ctx 取消）不影响统计入账。
func (e *Executor) finish(ctx context.Context, log *model.RequestLog) {
	if log.TotalTokens == 0 {
		log.TotalTokens = log.PromptTokens + log.CompletionTokens
	}
	if log.PromptTokens > 0 {
		log.CacheHitRate = float64(log.CachedTokens) / float64(log.PromptTokens)
	}
	if err := e.logs.Create(context.WithoutCancel(ctx), log); err != nil {
		logger.L("gateway").Error("save request log", logger.Err(err))
	}
	if e.bus != nil {
		e.bus.Publish(eventbus.EventRequestCompleted, log)
		e.bus.Publish(eventbus.EventStatsUpdated, nil)
	}
}

// upstreamProtoFor 渠道转换路径的上游协议。
func upstreamProtoFor(ch *model.Channel) model.Protocol {
	if ch.Provider == model.ProviderAnthropic {
		return model.ProtocolMessages
	}
	return model.ProtocolChatCompletions
}

func isSuccessStatus(code int) bool { return code >= 200 && code < 300 }

func retryableStatus(code int) bool {
	switch code {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusUnauthorized, http.StatusForbidden,
		http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

func writeSSEData(w http.ResponseWriter, flusher http.Flusher, payload []byte) {
	if _, err := w.Write(append(append([]byte("data: "), payload...), '\n', '\n')); err == nil && flusher != nil {
		flusher.Flush()
	}
}

func writeJSONError(w http.ResponseWriter, status int, in model.Protocol, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	var body any
	switch in {
	case model.ProtocolMessages:
		body = map[string]any{"type": "error", "error": map[string]any{"type": "gateway_error", "message": msg}}
	default:
		body = map[string]any{"error": map[string]any{"message": msg, "type": "gateway_error"}}
	}
	_ = json.NewEncoder(w).Encode(body)
}

// forwardErrorResponse 按客户端协议形态回传上游错误。
func forwardErrorResponse(w http.ResponseWriter, in model.Protocol, status int, detail string) {
	if status == 0 {
		status = http.StatusBadGateway
	}
	writeJSONError(w, status, in, detail)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

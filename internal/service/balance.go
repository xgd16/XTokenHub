package service

import (
	"context"
	"sort"
	"sync"
	"time"

	"xtokenhub/internal/eventbus"
	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/logger"
	"xtokenhub/internal/provider"
)

// 余额查询：批量拉取各渠道上游账户余额（当前支持 DeepSeek，按 BaseURL host 推断），
// 结果带 TTL 缓存并经事件总线推送（channel.balance_updated），前端余额列实时更新。
const (
	// balanceTTL 成功结果缓存时长。
	balanceTTL = 5 * time.Minute
	// balanceErrTTL 失败结果缓存时长：上游故障时避免每次刷新都打到超时。
	balanceErrTTL = time.Minute
	// balanceConcurrency 单批并发拉取上限。
	balanceConcurrency = 4
)

// balanceCacheEntry 单渠道余额缓存条目。
type balanceCacheEntry struct {
	entry   BalanceEntry
	expires time.Time
}

// BalanceEntry 单渠道余额查询结果。
type BalanceEntry struct {
	ChannelID   int64                      `json:"channel_id"`
	ChannelName string                     `json:"channel_name"`
	Provider    provider.BalanceProviderKind `json:"provider"` // 空 = 该 BaseURL 无对应余额接口
	Supported   bool                       `json:"supported"`
	OK          bool                       `json:"ok"`
	Balance     *provider.BalanceInfo      `json:"balance,omitempty"`
	Error       string                     `json:"error,omitempty"`
	FetchedAt   time.Time                  `json:"fetched_at,omitempty"`
}

// SetBalanceClient 注入余额查询客户端（nil = 关闭余额查询）。
func (s *ChannelService) SetBalanceClient(c *provider.BalanceClient) {
	s.balMu.Lock()
	defer s.balMu.Unlock()
	s.balClient = c
	s.balCache = map[int64]balanceCacheEntry{}
}

// SetBalanceKindResolver 覆盖「渠道 -> 余额厂家」推断函数（默认按 BaseURL host 判定）。
// 主要供测试注入：假上游地址不含厂家域名时，可按前缀识别。
func (s *ChannelService) SetBalanceKindResolver(f func(baseURL string) provider.BalanceProviderKind) {
	if f == nil {
		return
	}
	s.balMu.Lock()
	defer s.balMu.Unlock()
	s.balKind = f
}

// SetBalanceCookieProvider 注入网页凭证提供方（当前供 MiMo 余额查询取会话 Cookie）。
func (s *ChannelService) SetBalanceCookieProvider(f func(ctx context.Context) (string, error)) {
	if f == nil {
		return
	}
	s.balMu.Lock()
	defer s.balMu.Unlock()
	s.balCookie = f
}

// InvalidateBalances 清空全部余额缓存（Cookie 等凭证变化后，下次查询立即用新值）。
func (s *ChannelService) InvalidateBalances() {
	s.balMu.Lock()
	s.balCache = map[int64]balanceCacheEntry{}
	s.balMu.Unlock()
}

// balInvalidate 渠道更新/删除后清掉对应缓存（api_key 可能已变化）。
func (s *ChannelService) balInvalidate(id int64) {
	s.balMu.Lock()
	delete(s.balCache, id)
	s.balMu.Unlock()
}

// Balances 批量查询渠道余额：不支持的渠道直接标记；支持的渠道命中 TTL 缓存不重复请求上游。
func (s *ChannelService) Balances(ctx context.Context) ([]BalanceEntry, error) {
	channels, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	s.balMu.Lock()
	client := s.balClient
	kindOf := s.balKind
	now := time.Now()
	entries := make([]BalanceEntry, 0, len(channels))
	type job struct {
		ch   model.Channel
		kind provider.BalanceProviderKind
	}
	var jobs []job
	for _, ch := range channels {
		kind := provider.BalanceProviderKind("")
		if kindOf != nil {
			kind = kindOf(ch.BaseURL)
		}
		entry := BalanceEntry{ChannelID: ch.ID, ChannelName: ch.Name, Provider: kind}
		if kind == "" || client == nil {
			entries = append(entries, entry) // unsupported
			continue
		}
		entry.Supported = true
		if ce, ok := s.balCache[ch.ID]; ok && ce.expires.After(now) {
			entries = append(entries, ce.entry)
			continue
		}
		jobs = append(jobs, job{ch: ch, kind: kind})
	}
	s.balMu.Unlock()

	if len(jobs) > 0 {
		var (
			wg  sync.WaitGroup
			sem = make(chan struct{}, balanceConcurrency)
			mu  sync.Mutex
		)
		for _, j := range jobs {
			wg.Add(1)
			sem <- struct{}{}
			go func(j job) {
				defer wg.Done()
				defer func() { <-sem }()
				entry := s.fetchBalance(ctx, client, j.ch, j.kind)
				mu.Lock()
				entries = append(entries, entry)
				mu.Unlock()
			}(j)
		}
		wg.Wait()
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].ChannelID < entries[j].ChannelID })
	return entries, nil
}

// fetchBalance 拉取单渠道余额并写缓存（成功/失败分别缓存，失败短缓存）。
func (s *ChannelService) fetchBalance(ctx context.Context, client *provider.BalanceClient, ch model.Channel, kind provider.BalanceProviderKind) BalanceEntry {
	entry := BalanceEntry{
		ChannelID: ch.ID, ChannelName: ch.Name, Provider: kind, Supported: true, FetchedAt: time.Now(),
	}
	// MiMo 走网页会话 Cookie 鉴权，凭证由设置页维护；取不到凭证直接按失败处理
	cookie := ""
	if kind == provider.BalanceProviderMiMo {
		s.balMu.Lock()
		providerFn := s.balCookie
		s.balMu.Unlock()
		if providerFn == nil {
			entry.Error = "未配置 MiMo 网页 Cookie，请在 设置 → MiMo 余额凭证 中填写"
			s.cacheBalance(ch.ID, entry, true)
			return entry
		}
		c, err := providerFn(ctx)
		if err != nil {
			entry.Error = "读取 MiMo 凭证失败: " + err.Error()
			s.cacheBalance(ch.ID, entry, true)
			return entry
		}
		cookie = c
	}
	info, err := client.Fetch(ctx, kind, ch.BaseURL, ch.APIKey, cookie)
	switch {
	case err != nil:
		entry.Error = err.Error()
	case info != nil:
		entry.OK = true
		entry.Balance = info
		entry.FetchedAt = info.FetchedAt
		if s.bus != nil {
			s.bus.Publish(eventbus.EventChannelBalance, map[string]any{
				"channel_id": ch.ID, "channel_name": ch.Name, "balance": info,
			})
		}
		logger.L("channel").Info("balance fetched",
			"channel", ch.Name, "currency", info.Currency, "total", info.Total)
	}
	s.cacheBalance(ch.ID, entry, err != nil)
	return entry
}

// cacheBalance 写入余额缓存：失败短缓存防超时风暴，成功长缓存。
func (s *ChannelService) cacheBalance(id int64, entry BalanceEntry, failed bool) {
	ttl := balanceTTL
	if failed {
		ttl = balanceErrTTL
	}
	s.balMu.Lock()
	if s.balCache == nil {
		s.balCache = map[int64]balanceCacheEntry{}
	}
	s.balCache[id] = balanceCacheEntry{entry: entry, expires: time.Now().Add(ttl)}
	s.balMu.Unlock()
}

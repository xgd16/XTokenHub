// Package gateway 网关编排层：渠道选择（原生透传优先）、转发执行
// （透传/协议转换 × 流式/非流式）、usage 统计与日志落库。
package gateway

import (
	"math/rand"
	"sort"

	"xtokenhub/internal/model"
)

// Candidate 一个可尝试的候选。
type Candidate struct {
	Channel       *model.Channel
	Mode          model.ForwardMode // native_passthrough | converted
	UpstreamModel string            // 实际发往上游的模型（普通请求=入站模型；分组路由=成员模型）
}

// SelectCandidates 生成渠道候选序列（纯函数）：
//  1. 过滤：启用状态 && 支持该模型；
//  2. 原生渠道（native_protocols 含入站协议）排在最前 —— 零转换透传；
//  3. 其余渠道为转换候选（协议转换兜底，responses 上游除外）；
//  4. 同层内按 priority 升序，同 priority 按 weight 加权随机洗牌，保证负载分布。
//
// group 非 nil 时为自定义模型组路由：按成员展开（成员模型 × 支持渠道），
// 排序键为「成员 priority（越小越优先） -> 渠道 priority -> 原生优先」，
// 同键组内按渠道 weight 加权随机；候选携带成员模型作为实际上游模型。
func SelectCandidates(channels []model.Channel, inbound model.Protocol, reqModel string, group *model.CustomModel) []Candidate {
	if group == nil {
		var native, convertible []model.Channel
		for i := range channels {
			ch := &channels[i]
			if ch.Status != model.ChannelEnabled || !ch.SupportsModel(reqModel) {
				continue
			}
			if ch.IsNative(inbound) {
				native = append(native, *ch)
				continue
			}
			// 转换上游仅支持 chat_completions / messages（responses 上游不提供转换写入）
			convertible = append(convertible, *ch)
		}

		out := make([]Candidate, 0, len(native)+len(convertible))
		orderedNative := shuffleByWeight(native)
		for i := range orderedNative {
			out = append(out, Candidate{Channel: &orderedNative[i], Mode: model.ForwardNativePassthrough, UpstreamModel: reqModel})
		}
		orderedConvertible := shuffleByWeight(convertible)
		for i := range orderedConvertible {
			out = append(out, Candidate{Channel: &orderedConvertible[i], Mode: model.ForwardConverted, UpstreamModel: reqModel})
		}
		return out
	}
	return selectGroupCandidates(channels, inbound, group)
}

// groupEntry 分组路由候选条目。
type groupEntry struct {
	member model.ModelMember
	ch     model.Channel
	native bool
}

// selectGroupCandidates 自定义模型组路由：成员优先级 -> 渠道优先级 -> 原生优先。
func selectGroupCandidates(channels []model.Channel, inbound model.Protocol, group *model.CustomModel) []Candidate {
	var entries []groupEntry
	for _, m := range group.MemberList() {
		for i := range channels {
			ch := &channels[i]
			if ch.Status != model.ChannelEnabled || !ch.SupportsModel(m.Model) {
				continue
			}
			entries = append(entries, groupEntry{member: m, ch: *ch, native: ch.IsNative(inbound)})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].member.Priority != entries[j].member.Priority {
			return entries[i].member.Priority < entries[j].member.Priority
		}
		if entries[i].ch.Priority != entries[j].ch.Priority {
			return entries[i].ch.Priority < entries[j].ch.Priority
		}
		return entries[i].native && !entries[j].native
	})

	out := make([]Candidate, 0, len(entries))
	for i := 0; i < len(entries); {
		j := i
		for j < len(entries) &&
			entries[j].member.Priority == entries[i].member.Priority &&
			entries[j].ch.Priority == entries[i].ch.Priority &&
			entries[j].native == entries[i].native {
			j++
		}
		weights := make([]int, j-i)
		for k := i; k < j; k++ {
			weights[k-i] = entries[k].ch.Weight
		}
		for _, idx := range weightedOrder(weights) {
			e := entries[i+idx]
			mode := model.ForwardConverted
			if e.native {
				mode = model.ForwardNativePassthrough
			}
			ch := e.ch
			out = append(out, Candidate{Channel: &ch, Mode: mode, UpstreamModel: e.member.Model})
		}
		i = j
	}
	return out
}

// shuffleByWeight 分层加权随机洗牌：priority 升序分组，组内 weight 加权随机抽取。
func shuffleByWeight(chs []model.Channel) []model.Channel {
	// 先按 priority 分组
	sort.SliceStable(chs, func(i, j int) bool { return chs[i].Priority < chs[j].Priority })
	var out []model.Channel
	for i := 0; i < len(chs); {
		j := i
		for j < len(chs) && chs[j].Priority == chs[i].Priority {
			j++
		}
		weights := make([]int, j-i)
		for k := i; k < j; k++ {
			weights[k-i] = chs[k].Weight
		}
		for _, idx := range weightedOrder(weights) {
			out = append(out, chs[i+idx])
		}
		i = j
	}
	return out
}

// weightedOrder 组内按权重加权随机返回下标顺序（weight<=0 视为 1）。
func weightedOrder(weights []int) []int {
	idx := make([]int, len(weights))
	for i := range idx {
		idx[i] = i
	}
	out := make([]int, 0, len(idx))
	for len(idx) > 0 {
		total := 0
		for _, i := range idx {
			w := weights[i]
			if w <= 0 {
				w = 1
			}
			total += w
		}
		r := rand.Intn(total)
		pick := 0
		for k, i := range idx {
			w := weights[i]
			if w <= 0 {
				w = 1
			}
			r -= w
			if r < 0 {
				pick = k
				break
			}
		}
		out = append(out, idx[pick])
		idx = append(idx[:pick], idx[pick+1:]...)
	}
	return out
}

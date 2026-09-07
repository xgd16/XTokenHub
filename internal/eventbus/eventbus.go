// Package eventbus 提供进程内发布/订阅总线，解耦业务事件与推送（WebSocket）等消费者。
// 语义：发布异步、非阻塞，但单个订阅者按发布顺序串行收到事件（先后发的先到），
// 保证如 request.started -> request.completed 的先后关系不会因并发分发而颠倒。
package eventbus

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// 事件类型常量（WS 消息 type 与之一一对应）。
const (
	EventRequestStarted      = "request.started"        // 网关请求已受理（尚未完成，供前端进行中行）
	EventRequestCompleted    = "request.completed"      // 网关请求结束
	EventStatsUpdated        = "stats.updated"          // 统计聚合变化
	EventThroughput          = "stats.throughput"       // 实时输出 token 速度（2Hz 推送）
	EventChannelProbeResult  = "channel.probe_result"   // 渠道探测完成
	EventChannelStatusChange = "channel.status_changed" // 渠道启停
	EventChannelBalance      = "channel.balance_updated" // 渠道余额/套餐用量更新
)

// queueSize 单订阅者事件队列容量；打满时丢弃新事件（丢弃优于阻塞发布方）。
const queueSize = 1024

// Event 总线事件。
type Event struct {
	Type    string
	Payload any
	Time    time.Time
}

// Handler 事件处理函数；同一订阅者的事件被串行调用，须自行保证并发安全。
type Handler func(Event)

type subscription struct {
	typ     string
	handler Handler
	ch      chan Event
	quit    chan struct{}
	pending sync.WaitGroup // 已入队未处理完的事件计数（Wait 用）
}

// Bus 内存事件总线。每个订阅者一个独立消费 goroutine，按发布顺序串行分发。
type Bus struct {
	mu      sync.RWMutex
	subs    map[*subscription]struct{}
	dropped atomic.Int64 // 队列满被丢弃的事件数（诊断用）
}

// New 构造事件总线。
func New() *Bus {
	return &Bus{subs: make(map[*subscription]struct{})}
}

// Subscribe 订阅某类型事件，返回退订函数（并发安全，可重复调用）。
func (b *Bus) Subscribe(typ string, h Handler) (unsubscribe func()) {
	sub := &subscription{
		typ:     typ,
		handler: h,
		ch:      make(chan Event, queueSize),
		quit:    make(chan struct{}),
	}
	b.mu.Lock()
	b.subs[sub] = struct{}{}
	b.mu.Unlock()

	go b.consume(sub)

	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, sub)
			b.mu.Unlock()
			close(sub.quit)
		})
	}
}

// consume 订阅者消费循环：FIFO 串行调用 handler，直至退订并排干剩余事件。
func (b *Bus) consume(sub *subscription) {
	for {
		select {
		case ev := <-sub.ch:
			b.dispatch(sub, ev)
		case <-sub.quit:
			for {
				select {
				case ev := <-sub.ch:
					b.dispatch(sub, ev)
				default:
					return
				}
			}
		}
	}
}

// dispatch 调用 handler；panic 不影响消费循环。
func (b *Bus) dispatch(sub *subscription, ev Event) {
	defer sub.pending.Done()
	defer func() {
		if r := recover(); r != nil {
			slog.Error("eventbus handler panic", "type", ev.Type, "panic", fmt.Sprint(r))
		}
	}()
	sub.handler(ev)
}

// Publish 发布事件（立即返回；入队成功即保证该订阅者按此顺序消费）。
// 队列满时丢弃事件并告警，绝不阻塞发布方。
func (b *Bus) Publish(typ string, payload any) {
	ev := Event{Type: typ, Payload: payload, Time: time.Now()}

	b.mu.RLock()
	targets := make([]*subscription, 0, len(b.subs))
	for sub := range b.subs {
		if sub.typ == typ {
			targets = append(targets, sub)
		}
	}
	b.mu.RUnlock()

	for _, sub := range targets {
		sub.pending.Add(1)
		select {
		case sub.ch <- ev:
		default:
			sub.pending.Done()
			total := b.dropped.Add(1)
			slog.Warn("eventbus queue full, event dropped", "type", typ, "dropped_total", total)
		}
	}
}

// Wait 等待所有已入队事件分发完成（测试与优雅退出用）。
func (b *Bus) Wait() {
	b.mu.RLock()
	subs := make([]*subscription, 0, len(b.subs))
	for sub := range b.subs {
		subs = append(subs, sub)
	}
	b.mu.RUnlock()
	for _, sub := range subs {
		sub.pending.Wait()
	}
}

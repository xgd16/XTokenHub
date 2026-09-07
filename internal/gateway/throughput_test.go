package gateway

import (
	"context"
	"sync"
	"testing"
	"time"

	"xtokenhub/internal/eventbus"
)

// collectThroughput 订阅 stats.throughput 并收集样本。
type collectThroughput struct {
	mu   sync.Mutex
	last ThroughputPayload
	n    int
	max  float64
}

func (c *collectThroughput) unsub(bus *eventbus.Bus) func() {
	return bus.Subscribe(eventbus.EventThroughput, func(ev eventbus.Event) {
		p, ok := ev.Payload.(ThroughputPayload)
		if !ok {
			return
		}
		c.mu.Lock()
		c.last = p
		c.n++
		if p.TokensPerSec > c.max {
			c.max = p.TokensPerSec
		}
		c.mu.Unlock()
	})
}

func (c *collectThroughput) latest() (ThroughputPayload, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last, c.n
}

func (c *collectThroughput) peak() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.max
}

func TestThroughputRotatesWindow(t *testing.T) {
	bus := eventbus.New()
	col := &collectThroughput{}
	unsub := col.unsub(bus)
	defer unsub()

	interval := 20 * time.Millisecond
	window := 100 * time.Millisecond // 5 桶，测试段内可填满
	tp := NewThroughput(bus, interval, window)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tp.Run(ctx)

	// 匀速上报：每 interval 内 5 tok（持续生成，填满并进入稳态）
	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		tp.Add(5)
		time.Sleep(interval / 3)
	}
	time.Sleep(interval * 3)

	_, n := col.latest()
	if n == 0 {
		t.Fatalf("未收到任何 stats.throughput 事件")
	}
	// 稳态峰值：每 interval 贡献 5*3=15 tok / 0.02s = 750 tok/s
	if col.peak() < 500 || col.peak() > 900 {
		t.Fatalf("TokensPerSec 峰值不合理，peak=%v（期望 ~750）", col.peak())
	}
	// 停止生成后滑窗被空桶填满，速率回落接近 0
	time.Sleep(window)
	got2, _ := col.latest()
	if got2.TokensPerSec > 200 {
		t.Fatalf("停止生成后速率未回落，got=%v", got2.TokensPerSec)
	}
}

func TestThroughputStreamSessionIncremental(t *testing.T) {
	bus := eventbus.New()
	col := &collectThroughput{}
	unsub := col.unsub(bus)
	defer unsub()

	interval := 20 * time.Millisecond
	window := 100 * time.Millisecond
	tp := NewThroughput(bus, interval, window)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tp.Run(ctx)

	id := tp.StreamBegin()
	// 会话中分批生成：累计 10 -> 25 -> 40
	tp.StreamUpdate(id, 10)
	tp.StreamUpdate(id, 25)
	tp.StreamUpdate(id, 40)
	// 重复上报更小值应被忽略（不产生负数 delta）
	tp.StreamUpdate(id, 30)

	tp.StreamEnd(id)
	time.Sleep(interval * 3)

	got, n := col.latest()
	if n == 0 {
		t.Fatalf("未收到 stats.throughput 事件")
	}
	if got.ActiveStreams != 0 {
		t.Fatalf("会话结束后 ActiveStreams 应为 0，got=%d", got.ActiveStreams)
	}
	if got.TokensPerSec <= 0 {
		t.Fatalf("会话 token 未计入速率，got=%v", got.TokensPerSec)
	}
}

func TestThroughputEndedSessionIgnored(t *testing.T) {
	bus := eventbus.New()
	tp := NewThroughput(bus, 10*time.Millisecond, 100*time.Millisecond)

	id := tp.StreamBegin()
	tp.StreamUpdate(id, 10)
	tp.StreamEnd(id)

	// 已结束的会话上报应被忽略
	tp.StreamUpdate(id, 100)
	if tp.Active() != 0 {
		t.Fatalf("结束会话不应仍处于 active，got=%d", tp.Active())
	}
}

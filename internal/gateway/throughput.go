package gateway

import (
	"context"
	"sync"
	"time"

	"xtokenhub/internal/eventbus"
	"xtokenhub/internal/pkg/logger"
)

// throughputWindow 实时速率滑窗长度。
const throughputWindow = 5 * time.Second

// ThroughputPayload `stats.throughput` 事件载荷。
type ThroughputPayload struct {
	TokensPerSec  float64 `json:"tokens_per_sec"` // 滑窗内平均输出 token/s
	ActiveStreams int     `json:"active_streams"` // 进行中的流式会话数
}

// Throughput 实时输出 token 速度跟踪器：各流式会话在生成过程中上报当前累计
// completion token，本跟踪器每 interval 计算滑窗速率并经事件总线广播。
// 非流式请求完成时一次性上报其 completion token，同样计入速率。
type Throughput struct {
	bus      *eventbus.Bus
	interval time.Duration
	window   int64 // 滑窗 bucket 数（window/interval）

	mu      sync.Mutex
	cur     int64           // 当前 interval 已生成的 completion token
	buckets []int64         // 最近 window 个 interval 的 token 计数
	sess    map[int64]int64 // 进行中流式会话：已上报的累计 completion token
	seq     int64
}

// NewThroughput 构造跟踪器；interval<=0 时默认 500ms，window<=0 时默认 5s。
func NewThroughput(bus *eventbus.Bus, interval, window time.Duration) *Throughput {
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	if window <= 0 {
		window = throughputWindow
	}
	w := int64(window / interval)
	if w < 1 {
		w = 1
	}
	return &Throughput{
		bus:      bus,
		interval: interval,
		window:   w,
		buckets:  make([]int64, w),
		sess:     make(map[int64]int64),
	}
}

// StreamBegin 开启一个进行中的流式会话，返回会话 id。
func (t *Throughput) StreamBegin() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seq++
	id := t.seq
	t.sess[id] = 0
	return id
}

// StreamUpdate 上报某流式会话当前的累计 completion token（单调不减）。
// 入参为该会话"已生成"的总量；增量部分计入当前 interval，实现会话中实时计量。
func (t *Throughput) StreamUpdate(id int64, total int64) {
	if total <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	prev, ok := t.sess[id]
	if !ok {
		return
	}
	if total > prev {
		t.sess[id] = total
		t.cur += total - prev
	}
}

// StreamEnd 流式会话结束，移除会话。
func (t *Throughput) StreamEnd(id int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.sess, id)
}

// Add 一次性计入 token（非流式请求完成、或流式最终兜底新增量）。n 为 completion token。
func (t *Throughput) Add(n int64) {
	if n <= 0 {
		return
	}
	t.mu.Lock()
	t.cur += n
	t.mu.Unlock()
}

// Run 阻塞运行：按 interval 轮转 bucket 并广播速率。ctx 取消时退出。
func (t *Throughput) Run(ctx context.Context) {
	tick := time.NewTicker(t.interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			t.rotate()
		}
	}
}

// rotate 将当前 interval 计数推入滑窗，计算速率并广播。
func (t *Throughput) rotate() {
	t.mu.Lock()
	// 右移：历史前移，最后一个位置放入 latest
	for i := 0; i < len(t.buckets)-1; i++ {
		t.buckets[i] = t.buckets[i+1]
	}
	t.buckets[len(t.buckets)-1] = t.cur
	t.cur = 0
	active := len(t.sess)

	var sum int64
	for _, b := range t.buckets {
		sum += b
	}
	secs := float64(int64(t.interval)*t.window) / float64(time.Second)
	payload := ThroughputPayload{
		TokensPerSec:  float64(sum) / secs,
		ActiveStreams: active,
	}
	t.mu.Unlock()

	if t.bus != nil {
		t.bus.Publish(eventbus.EventThroughput, payload)
	} else {
		logger.L("throughput").Debug("throughput sample", "tokens_per_sec", payload.TokensPerSec, "active", active)
	}
}

// Active 当前进行中的流式会话数。
func (t *Throughput) Active() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sess)
}

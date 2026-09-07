package eventbus_test

import (
	"xtokenhub/internal/eventbus"

	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitIdle(t *testing.T, bus *eventbus.Bus) {
	t.Helper()
	done := make(chan struct{})
	go func() { bus.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("等待事件分发超时")
	}
}

func TestSubscribeAndPublish(t *testing.T) {
	bus := eventbus.New()
	var got atomic.Int32
	unsub := bus.Subscribe("request.completed", func(e eventbus.Event) {
		got.Add(int32(e.Payload.(int)))
	})
	bus.Publish("request.completed", 5)
	bus.Publish("other.type", 100) // 不匹配类型
	waitIdle(t, bus)

	if got.Load() != 5 {
		t.Fatalf("got = %d, want 5", got.Load())
	}
	unsub()
	bus.Publish("request.completed", 7)
	waitIdle(t, bus)
	if got.Load() != 5 {
		t.Errorf("退订后仍收到事件: got = %d", got.Load())
	}
}

func TestMultipleSubscribers(t *testing.T) {
	bus := eventbus.New()
	var mu sync.Mutex
	seen := map[string]int{}
	for _, name := range []string{"a", "b", "c"} {
		bus.Subscribe("evt", func(e eventbus.Event) {
			mu.Lock()
			seen[name]++
			mu.Unlock()
		})
	}
	for i := 0; i < 10; i++ {
		bus.Publish("evt", nil)
	}
	waitIdle(t, bus)
	mu.Lock()
	defer mu.Unlock()
	for _, name := range []string{"a", "b", "c"} {
		if seen[name] != 10 {
			t.Errorf("订阅者 %s 收到 %d 次, want 10", name, seen[name])
		}
	}
}

func TestUnsubscribeIdempotent(t *testing.T) {
	bus := eventbus.New()
	unsub := bus.Subscribe("x", func(eventbus.Event) {})
	unsub()
	unsub() // 重复退订应安全
	unsub()
}

func TestPanicIsolated(t *testing.T) {
	bus := eventbus.New()
	var okCount atomic.Int32
	bus.Subscribe("boom", func(eventbus.Event) { panic("handler exploded") })
	bus.Subscribe("boom", func(eventbus.Event) { okCount.Add(1) })

	bus.Publish("boom", nil)
	waitIdle(t, bus)
	if okCount.Load() != 1 {
		t.Errorf("panic 不应影响其他订阅者: ok = %d", okCount.Load())
	}
}

// TestPublishOrderPreservedPerSubscriber 同一订阅者必须按发布顺序收到事件：
// 快速连发 started/completed，顺序颠倒会导致前端"运行中"行永远无法被完成事件替换。
func TestPublishOrderPreservedPerSubscriber(t *testing.T) {
	bus := eventbus.New()
	t.Cleanup(bus.Wait)

	const n = 200
	done := make(chan []int, 1)
	go func() {
		var mu sync.Mutex
		var got []int
		unsub := bus.Subscribe("t", func(e eventbus.Event) {
			mu.Lock()
			got = append(got, e.Payload.(int))
			reached := len(got) == n
			snapshot := append([]int(nil), got...)
			mu.Unlock()
			if reached {
				select {
				case done <- snapshot:
				default:
				}
			}
		})
		defer unsub()
		for i := 0; i < n; i++ {
			bus.Publish("t", i)
		}
	}()

	select {
	case got := <-done:
		for i, v := range got {
			if v != i {
				t.Fatalf("顺序错乱: idx=%d got=%d want=%d", i, v, i)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("等待事件超时")
	}
}

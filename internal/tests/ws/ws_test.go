package ws_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"xtokenhub/internal/eventbus"
	"xtokenhub/internal/ws"
)

// dial 新建测试 WS 连接。
func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("拨号 %s: %v", url, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func newTestHub(t *testing.T, pingInterval, writeTimeout time.Duration) (*ws.Hub, *httptest.Server) {
	t.Helper()
	hub := ws.NewHub(pingInterval, writeTimeout)
	mux := http.NewServeMux()
	mux.Handle("/ws", http.HandlerFunc(hub.ServeHTTP))
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		hub.Close()
		srv.Close()
	})
	return hub, srv
}

func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + "/ws"
}

func readMessage(t *testing.T, c *websocket.Conn) ws.Message {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, data, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("读消息: %v", err)
	}
	var msg ws.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("解析消息 %q: %v", data, err)
	}
	return msg
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("等待条件超时")
}

func TestBroadcastToClients(t *testing.T) {
	hub, srv := newTestHub(t, 30*time.Second, 5*time.Second)
	c1 := dial(t, wsURL(srv.URL))
	c2 := dial(t, wsURL(srv.URL))

	waitFor(t, func() bool { return hub.ClientCount() == 2 })

	hub.Broadcast(ws.NewMessage("request.completed", map[string]any{"id": 1}))

	m1 := readMessage(t, c1)
	m2 := readMessage(t, c2)
	if m1.Type != "request.completed" || m2.Type != "request.completed" {
		t.Errorf("消息类型错误: %v %v", m1.Type, m2.Type)
	}
	payload, _ := m1.Payload.(map[string]any)
	if payload["id"] != float64(1) {
		t.Errorf("payload 错误: %v", m1.Payload)
	}
	if m1.Ts == 0 {
		t.Error("缺少时间戳")
	}
}

func TestAttachBusEvents(t *testing.T) {
	hub, srv := newTestHub(t, 30*time.Second, 5*time.Second)
	bus := eventbus.New()
	hub.AttachBus(bus, eventbus.EventRequestCompleted, eventbus.EventChannelProbeResult)
	defer bus.Wait()

	c := dial(t, wsURL(srv.URL))
	waitFor(t, func() bool { return hub.ClientCount() == 1 })

	bus.Publish(eventbus.EventRequestCompleted, map[string]any{"model": "gpt-4o"})
	msg := readMessage(t, c)
	if msg.Type != eventbus.EventRequestCompleted {
		t.Fatalf("type = %s", msg.Type)
	}
	payload, _ := msg.Payload.(map[string]any)
	if payload["model"] != "gpt-4o" {
		t.Errorf("payload = %v", msg.Payload)
	}

	// Close 退订并断开全部客户端
	hub.Close()
	bus.Publish(eventbus.EventRequestCompleted, map[string]any{})
	if hub.ClientCount() != 0 {
		t.Error("Close 后应有 0 客户端")
	}
}

func TestClientPingPong(t *testing.T) {
	// 客户端发 {"type":"ping"} 文本心跳，服务端应回 pong（前端据此探测出向链路假活）
	hub, srv := newTestHub(t, 30*time.Second, 5*time.Second)
	c := dial(t, wsURL(srv.URL))
	waitFor(t, func() bool { return hub.ClientCount() == 1 })

	if err := c.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatalf("发送 ping: %v", err)
	}
	msg := readMessage(t, c)
	if msg.Type != ws.MsgPong {
		t.Errorf("应回复 pong，实际 %q", msg.Type)
	}
}

func TestClientGarbageIgnored(t *testing.T) {
	// 非法/普通文本消息不回 pong、不断连
	hub, srv := newTestHub(t, 30*time.Second, 5*time.Second)
	c := dial(t, wsURL(srv.URL))
	waitFor(t, func() bool { return hub.ClientCount() == 1 })

	for _, data := range []string{"not-json", `{"type":"other"}`} {
		if err := c.WriteMessage(websocket.TextMessage, []byte(data)); err != nil {
			t.Fatalf("发送 %q: %v", data, err)
		}
	}
	// 短暂等待后连接仍在线，且没有回包
	time.Sleep(100 * time.Millisecond)
	if hub.ClientCount() != 1 {
		t.Error("普通文本消息不应断开连接")
	}
	_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, _, err := c.ReadMessage(); err == nil {
		t.Error("非 ping 消息不应收到回复")
	}
}

func TestClientDisconnectCleansUp(t *testing.T) {
	hub, srv := newTestHub(t, 30*time.Second, 5*time.Second)
	conn := dial(t, wsURL(srv.URL))
	waitFor(t, func() bool { return hub.ClientCount() == 1 })

	_ = conn.Close()
	waitFor(t, func() bool { return hub.ClientCount() == 0 })
}

func TestPingKeepalive(t *testing.T) {
	// 100ms ping -> 读超时 200ms；客户端运行读循环（gorilla 自动回 pong）应保持在线
	hub, srv := newTestHub(t, 100*time.Millisecond, 1*time.Second)
	c := dial(t, wsURL(srv.URL))
	waitFor(t, func() bool { return hub.ClientCount() == 1 })

	// 持续读取以触发自动 pong
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
				if _, _, err := c.ReadMessage(); err != nil {
					return
				}
			}
		}
	}()
	defer close(stop)

	time.Sleep(400 * time.Millisecond) // 超过 2 个 ping 周期
	if hub.ClientCount() != 1 {
		t.Error("正常回 pong 的客户端不应被踢出")
	}
}

func TestBroadcastResilientToAbruptClose(t *testing.T) {
	// 一个客户端突然断开不应影响向其余客户端的广播
	hub, srv := newTestHub(t, 30*time.Second, 5*time.Second)
	gone := dial(t, wsURL(srv.URL))
	stay := dial(t, wsURL(srv.URL))
	waitFor(t, func() bool { return hub.ClientCount() == 2 })

	_ = gone.Close() // 不等待清理完成
	hub.Broadcast(ws.NewMessage("x", 1))
	msg := readMessage(t, stay)
	if msg.Type != "x" {
		t.Errorf("存活客户端未收到广播: %v", msg)
	}
	waitFor(t, func() bool { return hub.ClientCount() == 1 })
}

func TestSlowClientDropped(t *testing.T) {
	// 真实连接不消费消息：大量大消息广播后触发慢消费者丢弃
	hub, srv := newTestHub(t, 30*time.Second, 5*time.Second)
	_ = dial(t, wsURL(srv.URL))
	waitFor(t, func() bool { return hub.ClientCount() == 1 })

	big := strings.Repeat("x", 64*1024) // 64KB/条，send 缓冲 64 条
	for i := 0; i < 300; i++ {
		hub.Broadcast(ws.NewMessage("bulk", big))
	}
	waitFor(t, func() bool { return hub.DroppedCount() > 0 })
	// 慢消费者应被服务端断开
	waitFor(t, func() bool { return hub.ClientCount() == 0 })
}

func TestClientCountEmpty(t *testing.T) {
	hub, _ := newTestHub(t, 30*time.Second, 5*time.Second)
	if hub.ClientCount() != 0 || hub.DroppedCount() != 0 {
		t.Error("初始应为空")
	}
}

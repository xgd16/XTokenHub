// Package ws 基于 gorilla/websocket 的推送中心：连接注册、心跳保活、异步广播。
package ws

import (
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"xtokenhub/internal/eventbus"
	"xtokenhub/internal/pkg/logger"
)

// Message 推送消息信封。
type Message struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
	Ts      int64  `json:"ts"` // UnixMilli
}

// NewMessage 构造消息。
func NewMessage(typ string, payload any) Message {
	return Message{Type: typ, Payload: payload, Ts: time.Now().UnixMilli()}
}

const (
	sendBufferSize  = 256  // 突发流量下减少慢消费者误判（仍非阻塞，打满才断开）
	closeSlowClient = true // 缓冲打满即断开，避免慢消费者拖垮广播
	pongWaitFactor  = 2    // 读超时 = ping 间隔的倍数
)

// 传输层心跳消息类型：客户端发 ping 文本帧探测出向链路，服务端回 pong。
const (
	MsgPing = "ping"
	MsgPong = "pong"
)

// Client 单个前端连接。
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

// Hub 连接管理中心，订阅事件总线并向所有在线客户端广播。
type Hub struct {
	mu           sync.RWMutex
	clients      map[*Client]struct{}
	upgrader     websocket.Upgrader
	pingInterval time.Duration
	writeTimeout time.Duration
	unsubs       []func()
	dropped      atomic.Int64 // 因慢消费者丢弃的消息数（诊断用）
}

// NewHub 构造 Hub；参数为零值时使用默认值（ping 30s / 写超时 10s）。
func NewHub(pingInterval, writeTimeout time.Duration) *Hub {
	if pingInterval <= 0 {
		pingInterval = 30 * time.Second
	}
	if writeTimeout <= 0 {
		writeTimeout = 10 * time.Second
	}
	return &Hub{
		clients: make(map[*Client]struct{}),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 4096,
			// 同源部署下浏览器带 Origin；开放校验由反代/网关层控制，这里放行。
			CheckOrigin: func(*http.Request) bool { return true },
		},
		pingInterval: pingInterval,
		writeTimeout: writeTimeout,
	}
}

// AttachBus 订阅事件总线：总线事件序列化后广播给所有客户端。
func (h *Hub) AttachBus(bus *eventbus.Bus, types ...string) {
	for _, typ := range types {
		typ := typ
		h.unsubs = append(h.unsubs, bus.Subscribe(typ, func(ev eventbus.Event) {
			h.Broadcast(NewMessage(typ, ev.Payload))
		}))
	}
}

// Close 退订总线事件并断开全部客户端。
func (h *Hub) Close() {
	for _, u := range h.unsubs {
		u()
	}
	h.mu.Lock()
	clients := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.clients = make(map[*Client]struct{})
	h.mu.Unlock()
	for _, c := range clients {
		if c.conn != nil {
			_ = c.conn.Close()
		}
	}
}

// ServeHTTP 处理 WebSocket 升级并托管该连接直至断开（阻塞）。
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.L("ws").Warn("upgrade failed", logger.Err(err))
		return
	}
	c := &Client{hub: h, conn: conn, send: make(chan []byte, sendBufferSize)}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()

	go c.writePump()
	go c.readPump()
}

// ClientCount 在线客户端数。
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// DroppedCount 广播因慢消费者丢弃的消息总数（监控用）。
func (h *Hub) DroppedCount() int64 {
	return h.dropped.Load()
}

// Broadcast 向所有客户端异步推送；非阻塞，缓冲满的客户端被判定为慢消费者并断开。
func (h *Hub) Broadcast(msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		logger.L("ws").Error("marshal message", logger.Err(err), "type", msg.Type)
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		select {
		case c.send <- data:
		default:
			h.dropped.Add(1)
			if closeSlowClient && c.conn != nil {
				// 关闭触发 readPump/writePump 退出并清理注册
				_ = c.conn.Close()
				logger.L("ws").Warn("slow client closed", "remote", c.conn.RemoteAddr().String())
			}
		}
	}
}

func (h *Hub) unregister(c *Client) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
	h.mu.Unlock()
}

// readPump 读协程：感知断开、刷新读超时（协议层 pong），并回复客户端文本心跳。
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister(c)
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(4 * 1024) // 客户端仅发心跳文本，限制读入量
	_ = c.conn.SetReadDeadline(time.Now().Add(c.hub.pingInterval * pongWaitFactor))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(c.hub.pingInterval * pongWaitFactor))
	})
	for {
		mt, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		// 任何入站帧都证明链路存活，顺带刷新读超时
		_ = c.conn.SetReadDeadline(time.Now().Add(c.hub.pingInterval * pongWaitFactor))
		// 客户端 {"type":"ping"} 心跳 -> 回 pong（经 send 通道，避免与 writePump 并发写）
		if mt == websocket.TextMessage && isPing(data) {
			select {
			case c.send <- pongFrame():
			default: // 缓冲满则跳过，客户端下个心跳周期会重试
			}
		}
	}
}

// isPing 判断客户端文本消息是否为心跳 ping。
func isPing(data []byte) bool {
	var probe struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &probe) != nil {
		return false
	}
	return probe.Type == MsgPing
}

// pongFrame 构造 pong 回执帧。
func pongFrame() []byte {
	data, _ := json.Marshal(NewMessage(MsgPong, nil))
	return data
}

// writePump 写协程：消息队列 -> conn；周期 ping 保活。
func (c *Client) writePump() {
	ticker := time.NewTicker(c.hub.pingInterval)
	defer func() {
		ticker.Stop()
		c.hub.unregister(c)
		_ = c.conn.Close()
	}()
	for {
		select {
		case data, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.hub.writeTimeout))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.hub.writeTimeout))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

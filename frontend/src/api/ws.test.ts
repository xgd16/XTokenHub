import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, renderHook } from '@testing-library/react'
import {
  GatewayWS,
  WS_EVENTS,
  getWS,
  initWS,
  resetWS,
  useWsStatus,
  type GatewayOptions,
  type WsMessage,
} from './ws'

/** 可编程 mock WebSocket（close() 触发 onclose，与浏览器一致）。 */
class MockWebSocket {
  static instances: MockWebSocket[] = []
  static OPEN = 1
  readyState = 0
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((ev: { data: string }) => void) | null = null
  sent: string[] = []
  closed = false

  constructor(public url: string) {
    MockWebSocket.instances.push(this)
  }

  // 测试驱动
  serverAccept() {
    this.readyState = 1
    this.onopen?.()
  }
  serverPush(msg: WsMessage) {
    this.onmessage?.({ data: JSON.stringify(msg) })
  }
  serverDrop() {
    this.readyState = 3
    this.onclose?.()
  }
  close() {
    if (this.closed) return
    this.closed = true
    this.onclose?.() // 浏览器语义：close() 必然触发 close 事件
  }
  send(data: string) {
    this.sent.push(data)
  }
}

const factory = (url: string) => new MockWebSocket(url) as unknown as WebSocket

describe('GatewayWS', () => {
  const created: GatewayWS[] = []
  // 统一走 make 以便 afterEach 清理（实例的 lifecycle 监听会跨测试残留）
  const make = (opts?: GatewayOptions) => {
    const ws = new GatewayWS('ws://test', opts)
    created.push(ws)
    return ws
  }

  beforeEach(() => {
    vi.useFakeTimers()
    MockWebSocket.instances = []
  })
  afterEach(() => {
    created.forEach((ws) => ws.close())
    created.length = 0
    vi.useRealTimers()
  })

  it('连接成功后分发对应类型消息', () => {
    const ws = make({ factory })
    const got: WsMessage[] = []
    ws.on(WS_EVENTS.requestCompleted, (m) => got.push(m))

    ws.connect()
    const sock = MockWebSocket.instances[0]
    sock.serverAccept()
    sock.serverPush({ type: WS_EVENTS.requestCompleted, payload: { id: 1 }, ts: 1 })
    sock.serverPush({ type: WS_EVENTS.statsUpdated, payload: null, ts: 2 })

    expect(got).toHaveLength(1)
    expect(got[0].payload).toEqual({ id: 1 })
  })

  it('非法 JSON 消息被忽略，不崩溃', () => {
    const ws = make({ factory })
    ws.connect()
    const sock = MockWebSocket.instances[0]
    sock.serverAccept()
    expect(() => sock.onmessage?.({ data: 'not-json' })).not.toThrow()
  })

  it('断线后按指数退避重连：1s/2s/4s', () => {
    const ws = make({ factory, backoff: { baseMs: 1000, maxMs: 60_000 } })
    ws.connect()
    let sock = MockWebSocket.instances[0]
    sock.serverAccept()

    sock.serverDrop()
    vi.advanceTimersByTime(999)
    expect(MockWebSocket.instances).toHaveLength(1)
    vi.advanceTimersByTime(1)
    expect(MockWebSocket.instances).toHaveLength(2)

    sock = MockWebSocket.instances[1]
    sock.serverDrop()
    vi.advanceTimersByTime(1999)
    expect(MockWebSocket.instances).toHaveLength(2)
    vi.advanceTimersByTime(1)
    expect(MockWebSocket.instances).toHaveLength(3)

    sock = MockWebSocket.instances[2]
    sock.serverDrop()
    vi.advanceTimersByTime(3999)
    expect(MockWebSocket.instances).toHaveLength(3)
    vi.advanceTimersByTime(1)
    expect(MockWebSocket.instances).toHaveLength(4)
  })

  it('重连成功后重置退避', () => {
    const ws = make({ factory, backoff: { baseMs: 1000, maxMs: 60_000 } })
    ws.connect()
    let sock = MockWebSocket.instances[0]
    sock.serverAccept()
    sock.serverDrop()
    vi.advanceTimersByTime(1000)
    sock = MockWebSocket.instances[1]
    sock.serverAccept() // 重连成功 -> attempts 归零
    sock.serverDrop()
    vi.advanceTimersByTime(1000)
    expect(MockWebSocket.instances).toHaveLength(3)
  })

  it('退避封顶 maxMs', () => {
    const ws = make({ factory, backoff: { baseMs: 1000, maxMs: 3000 } })
    ws.connect()
    // 连续失败 10 次
    for (let i = 0; i < 10; i++) {
      const sock = MockWebSocket.instances[i]
      sock.serverDrop()
      vi.advanceTimersByTime(3000)
    }
    expect(MockWebSocket.instances.length).toBeGreaterThanOrEqual(10)
    // 最大间隔不超过 3000ms
    const before = MockWebSocket.instances.length
    MockWebSocket.instances[before - 1].serverDrop()
    vi.advanceTimersByTime(2999)
    expect(MockWebSocket.instances.length).toBe(before)
    vi.advanceTimersByTime(1)
    expect(MockWebSocket.instances.length).toBe(before + 1)
  })

  it('close() 后不再重连', () => {
    const ws = make({ factory })
    ws.connect()
    const sock = MockWebSocket.instances[0]
    sock.serverAccept()
    ws.close()
    sock.serverDrop()
    vi.advanceTimersByTime(60_000)
    expect(MockWebSocket.instances).toHaveLength(1)
  })

  it('onStatus 订阅即回放当前状态，并通知后续流转', () => {
    const ws = make({ factory })
    const statuses: string[] = []
    ws.onStatus((s) => statuses.push(s)) // 订阅即回放 'closed'
    expect(statuses).toEqual(['closed'])
    ws.connect()
    const sock = MockWebSocket.instances[0]
    sock.serverAccept()
    sock.serverDrop()
    expect(statuses).toEqual(['closed', 'connecting', 'open', 'closed'])
  })

  it('退订后不再收到消息', () => {
    const ws = make({ factory })
    const got: WsMessage[] = []
    const unsub = ws.on(WS_EVENTS.requestCompleted, (m) => got.push(m))
    ws.connect()
    const sock = MockWebSocket.instances[0]
    sock.serverAccept()
    unsub()
    sock.serverPush({ type: WS_EVENTS.requestCompleted, payload: 1, ts: 1 })
    expect(got).toHaveLength(0)
  })

  it('握手挂起时看门狗掐断连接并走重连（修复永久卡 SYNC）', () => {
    const ws = make({ factory, connectTimeoutMs: 5000, backoff: { baseMs: 1000, maxMs: 60_000 } })
    ws.connect()
    const sock = MockWebSocket.instances[0]
    // 不调用 serverAccept：模拟休眠唤醒/网络切换时握手永久挂起
    vi.advanceTimersByTime(5000)
    expect(sock.closed).toBe(true)
    expect(ws.status).toBe('closed')
    vi.advanceTimersByTime(1000)
    expect(MockWebSocket.instances).toHaveLength(2) // 正常退避重连
  })

  it('握手及时完成则看门狗不误伤', () => {
    const ws = make({ factory, connectTimeoutMs: 5000 })
    ws.connect()
    MockWebSocket.instances[0].serverAccept()
    vi.advanceTimersByTime(60_000)
    expect(MockWebSocket.instances).toHaveLength(1)
    expect(MockWebSocket.instances[0].closed).toBe(false)
  })

  it('心跳：定期发 ping，收到 pong 保持连接', () => {
    const ws = make({ factory, pingIntervalMs: 1000, deadAfterMs: 2500 })
    ws.connect()
    const sock = MockWebSocket.instances[0]
    sock.serverAccept()
    for (let i = 0; i < 5; i++) {
      vi.advanceTimersByTime(1000)
      expect(sock.sent[i]).toBe('{"type":"ping"}')
      sock.serverPush({ type: 'pong', payload: null, ts: 1 })
    }
    expect(sock.closed).toBe(false)
    expect(ws.status).toBe('open')
  })

  it('心跳：超时未收到任何消息判定链路假活并重连', () => {
    const ws = make({
      factory,
      pingIntervalMs: 1000,
      deadAfterMs: 2500,
      backoff: { baseMs: 1000, maxMs: 60_000 },
    })
    ws.connect()
    const sock = MockWebSocket.instances[0]
    sock.serverAccept()
    vi.advanceTimersByTime(1000) // tick1：发 ping，无回复
    vi.advanceTimersByTime(1000) // tick2：距上次消息 2s < 2.5s，再发 ping
    expect(sock.closed).toBe(false)
    vi.advanceTimersByTime(1000) // tick3：3s > 2.5s -> 掐断
    expect(sock.closed).toBe(true)
    expect(ws.status).toBe('closed')
    vi.advanceTimersByTime(1000)
    expect(MockWebSocket.instances).toHaveLength(2)
  })

  it('网络恢复（online）跳过退避立即重连', () => {
    const ws = make({ factory, backoff: { baseMs: 1000, maxMs: 60_000 } })
    ws.connect()
    const sock = MockWebSocket.instances[0]
    sock.serverAccept()
    sock.serverDrop() // 1s 退避定时器挂起中
    window.dispatchEvent(new Event('online'))
    expect(MockWebSocket.instances).toHaveLength(2) // 未等待退避
    expect(ws.reconnectAttempts).toBe(0)
    MockWebSocket.instances[1].serverAccept()
    vi.advanceTimersByTime(60_000) // 旧退避定时器已被清除，不再产生新连接
    expect(MockWebSocket.instances).toHaveLength(2)
    expect(sock.closed).toBe(false)
  })

  it('页面回前台跳过退避立即重连', () => {
    const ws = make({ factory, backoff: { baseMs: 1000, maxMs: 60_000 } })
    ws.connect()
    MockWebSocket.instances[0].serverAccept()
    MockWebSocket.instances[0].serverDrop()
    vi.advanceTimersByTime(999) // 退避还差 1ms
    document.dispatchEvent(new Event('visibilitychange'))
    expect(MockWebSocket.instances).toHaveLength(2) // 不等剩余退避
    MockWebSocket.instances[1].serverAccept()
    vi.advanceTimersByTime(60_000)
    expect(MockWebSocket.instances).toHaveLength(2)
  })

  it('回前台时对假活 open 连接立即探活', () => {
    // 心跳周期 30s 大于推进时长：保证 advance 期间不会触发 tick 提前掐断
    const ws = make({ factory, pingIntervalMs: 30_000, deadAfterMs: 2500 })
    ws.connect()
    const sock = MockWebSocket.instances[0]
    sock.serverAccept()
    vi.advanceTimersByTime(20_000) // 后台期间长时间无消息
    sock.sent.length = 0
    document.dispatchEvent(new Event('visibilitychange')) // 回前台立即探活
    expect(sock.closed).toBe(true) // 假活连接当场掐断
  })

  it('连接健康时回前台/网络恢复不打扰现有连接', () => {
    const ws = make({ factory, pingIntervalMs: 1000, deadAfterMs: 2500 })
    ws.connect()
    const sock = MockWebSocket.instances[0]
    sock.serverAccept()
    vi.advanceTimersByTime(1000)
    sock.serverPush({ type: 'pong', payload: null, ts: 1 })
    window.dispatchEvent(new Event('online'))
    document.dispatchEvent(new Event('visibilitychange'))
    expect(MockWebSocket.instances).toHaveLength(1)
    expect(sock.closed).toBe(false)
    expect(ws.status).toBe('open')
  })

  it('onReconnected 仅在断线恢复后触发（首次连接不触发）', () => {
    const ws = make({ factory, backoff: { baseMs: 1000, maxMs: 60_000 } })
    let fires = 0
    ws.onReconnected(() => {
      fires += 1
    })
    ws.connect()
    MockWebSocket.instances[0].serverAccept()
    expect(fires).toBe(0) // 首次连接
    MockWebSocket.instances[0].serverDrop()
    vi.advanceTimersByTime(1000)
    MockWebSocket.instances[1].serverAccept()
    expect(fires).toBe(1) // 断线恢复
    MockWebSocket.instances[1].serverDrop()
    vi.advanceTimersByTime(1000)
    MockWebSocket.instances[2].serverDrop() // 重连又失败
    vi.advanceTimersByTime(2000)
    MockWebSocket.instances[3].serverAccept()
    expect(fires).toBe(2) // 每次成功恢复各触发一次
  })
})

describe('全局单例', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    // 惰性单例走默认 factory（new WebSocket），stub 全局使其实例化 mock
    vi.stubGlobal('WebSocket', MockWebSocket)
    MockWebSocket.instances = []
    resetWS()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
    resetWS()
  })

  it('getWS 未初始化时惰性创建并立即连接（不抛错）', () => {
    const ws = getWS()
    expect(ws).toBeInstanceOf(GatewayWS)
    expect(MockWebSocket.instances).toHaveLength(1)
  })

  it('getWS 幂等：多次调用返回同一实例', () => {
    const a = getWS()
    expect(getWS()).toBe(a)
    expect(MockWebSocket.instances).toHaveLength(1)
  })

  it('initWS 显式指定 url，并与 getWS 共享单例', () => {
    const a = initWS('ws://custom')
    expect(MockWebSocket.instances[0].url).toBe('ws://custom')
    expect(getWS()).toBe(a)
    expect(MockWebSocket.instances).toHaveLength(1)
  })

  it('resetWS 后重新获取会新建连接', () => {
    getWS()
    resetWS()
    getWS()
    expect(MockWebSocket.instances).toHaveLength(2)
  })
})

describe('useWsStatus 状态回放', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.stubGlobal('WebSocket', MockWebSocket)
    MockWebSocket.instances = []
    resetWS()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
    resetWS()
  })

  it('订阅后状态流转正常驱动更新', () => {
    const { result } = renderHook(() => useWsStatus())
    expect(result.current).toBe('connecting')
    act(() => {
      MockWebSocket.instances[0].serverAccept()
    })
    expect(result.current).toBe('open')
    act(() => {
      MockWebSocket.instances[0].serverDrop()
    })
    expect(result.current).toBe('closed')
  })

  it('订阅时连接已 open：直接回放 open，不再经过 SYNC（回归用例）', () => {
    getWS()
    act(() => {
      MockWebSocket.instances[0].serverAccept()
    })
    // 关键：hook 在连接已 open 后才挂载（如 LiveBadge 重挂载），不能显示 connecting
    const { result } = renderHook(() => useWsStatus())
    expect(result.current).toBe('open')
  })
})

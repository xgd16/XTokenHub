import { useEffect, useRef, useSyncExternalStore } from 'react'

/** 服务端推送消息信封 {type, payload, ts}。 */
export interface WsMessage<T = unknown> {
  type: string
  payload: T
  ts: number
}

export type WsStatus = 'connecting' | 'open' | 'closed'

export type WsHandler = (msg: WsMessage) => void

export interface BackoffPolicy {
  baseMs: number
  maxMs: number
}

/** 事件类型常量（与后端 eventbus 一致）。 */
export const WS_EVENTS = {
  requestStarted: 'request.started',
  requestCompleted: 'request.completed',
  statsUpdated: 'stats.updated',
  throughput: 'stats.throughput',
  channelProbeResult: 'channel.probe_result',
  channelStatusChanged: 'channel.status_changed',
  channelBalanceUpdated: 'channel.balance_updated',
} as const

export interface GatewayOptions {
  factory?: (url: string) => WebSocket
  backoff?: BackoffPolicy
  /** 握手看门狗：超过该时长仍未 open 则主动掐断走重连（休眠唤醒/网络切换时浏览器可能长期不触发 onopen/onclose）。 */
  connectTimeoutMs?: number
  /** 客户端心跳间隔：定期发送 {"type":"ping"}，服务端回 pong。 */
  pingIntervalMs?: number
  /** 探活阈值：超过该时长未收到任何消息（含 pong）则判定链路假活并重连。 */
  deadAfterMs?: number
}

/** 默认参数：握手看门狗 10s / 心跳 25s / 65s 无消息判死（后台标签页定时器被节流到 1/min 时也不会误判）。 */
const DEFAULT_CONNECT_TIMEOUT_MS = 10_000
const DEFAULT_PING_INTERVAL_MS = 25_000
const DEFAULT_DEAD_AFTER_MS = 65_000

/** 传输层心跳消息类型（与后端 ws 包约定）。 */
const PING_TYPE = 'ping'

/**
 * 实时推送客户端：
 * - 自动重连（指数退避，封顶 maxMs）；页面回前台/网络恢复时清退避立即重连
 * - 握手看门狗：连接建立超时主动掐断，避免状态永久卡在 connecting
 * - 心跳探活：定期 ping/pong，出向链路假活（LIVE 但无数据）时主动重连
 * - 按消息类型订阅/退订；断线重连成功后触发 resync（页面补拉数据）
 * - 连接状态订阅即回放当前值（useWsStatus 不会错过已发生的状态）
 */
export class GatewayWS {
  private url: string
  private factory: (url: string) => WebSocket
  private policy: BackoffPolicy
  private connectTimeoutMs: number
  private pingIntervalMs: number
  private deadAfterMs: number
  private handlers = new Map<string, Set<WsHandler>>()
  private statusHandlers = new Set<(s: WsStatus) => void>()
  private resyncHandlers = new Set<() => void>()
  private ws: WebSocket | null = null
  private attempts = 0
  private timer: ReturnType<typeof setTimeout> | null = null
  private connectWatch: ReturnType<typeof setTimeout> | null = null
  private heartbeat: ReturnType<typeof setInterval> | null = null
  private lastMsgAt = 0
  private statusValue: WsStatus = 'closed'
  private hadDisconnect = false
  private started = false
  private stopped = false
  private lifecycleBound = false

  constructor(url: string, opts?: GatewayOptions) {
    this.url = url
    this.factory = opts?.factory ?? ((u) => new WebSocket(u))
    this.policy = opts?.backoff ?? { baseMs: 1000, maxMs: 30_000 }
    this.connectTimeoutMs = opts?.connectTimeoutMs ?? DEFAULT_CONNECT_TIMEOUT_MS
    this.pingIntervalMs = opts?.pingIntervalMs ?? DEFAULT_PING_INTERVAL_MS
    this.deadAfterMs = opts?.deadAfterMs ?? DEFAULT_DEAD_AFTER_MS
  }

  connect(): void {
    this.stopped = false
    this.started = true
    this.bindLifecycle()
    this.open()
  }

  private open(): void {
    this.emitStatus('connecting')
    let ws: WebSocket
    try {
      ws = this.factory(this.url)
    } catch {
      this.scheduleReconnect()
      return
    }
    this.ws = ws
    this.armConnectWatchdog(ws)

    ws.onopen = () => {
      this.disarmConnectWatchdog()
      this.attempts = 0
      this.startHeartbeat()
      this.emitStatus('open')
      this.notifyResync()
    }
    ws.onmessage = (ev) => {
      // 任何入站字节（含 pong/非法数据）都证明链路存活
      this.lastMsgAt = Date.now()
      let msg: WsMessage
      try {
        msg = JSON.parse(ev.data as string)
      } catch {
        return
      }
      this.dispatch(msg)
    }
    ws.onclose = () => this.handleClose(ws)
    ws.onerror = () => {
      // onclose 会随后触发并负责重连
    }
  }

  /** 页面回前台/网络恢复：清退避定时器、归零退避、掐断残连接后立即重连。 */
  private forceReconnect(): void {
    if (this.stopped || !this.started || this.statusValue === 'open') return
    const ws = this.ws
    if (ws) {
      this.ws = null
      ws.onclose = null // 不走常规关闭流程，由下面的 open() 接管
      try {
        ws.close()
      } catch {
        // 已关闭
      }
    }
    this.clearReconnectTimer()
    this.disarmConnectWatchdog()
    this.stopHeartbeat()
    this.attempts = 0
    this.open()
  }

  private onVisible = (): void => {
    if (this.stopped || !this.started) return
    if (typeof document !== 'undefined' && document.visibilityState !== 'visible') return
    if (this.statusValue === 'open') {
      // 长时间后台后立即探活：假活连接当场掐断，不等下个心跳周期
      this.onHeartbeatTick()
    } else {
      this.forceReconnect()
    }
  }

  private onOnline = (): void => {
    if (this.stopped || !this.started || this.statusValue === 'open') return
    this.forceReconnect()
  }

  private bindLifecycle(): void {
    if (this.lifecycleBound) return
    if (typeof window === 'undefined' || typeof document === 'undefined') return
    this.lifecycleBound = true
    window.addEventListener('online', this.onOnline)
    document.addEventListener('visibilitychange', this.onVisible)
  }

  private unbindLifecycle(): void {
    if (!this.lifecycleBound) return
    this.lifecycleBound = false
    window.removeEventListener('online', this.onOnline)
    document.removeEventListener('visibilitychange', this.onVisible)
  }

  /** 握手看门狗：CONNECTING 挂起时 onopen/onclose 都不会来，超时主动掐断以触发重连。 */
  private armConnectWatchdog(ws: WebSocket): void {
    this.disarmConnectWatchdog()
    this.connectWatch = setTimeout(() => {
      if (this.ws !== ws) return
      this.terminate(ws)
    }, this.connectTimeoutMs)
  }

  private disarmConnectWatchdog(): void {
    if (this.connectWatch) {
      clearTimeout(this.connectWatch)
      this.connectWatch = null
    }
  }

  /** 主动掐断连接：close() 并确保关闭流程（状态/重连）只走一次（close 事件晚到时幂等）。 */
  private terminate(ws: WebSocket): void {
    if (this.ws !== ws) return
    try {
      ws.close()
    } catch {
      // 已关闭
    }
    this.handleClose(ws)
  }

  private handleClose(ws: WebSocket): void {
    if (this.ws !== ws) return // 已被其他流程（close()/forceReconnect/重复 close 事件）处理
    this.disarmConnectWatchdog()
    this.stopHeartbeat()
    this.ws = null
    this.emitStatus('closed')
    if (!this.stopped) {
      this.scheduleReconnect()
    }
  }

  private startHeartbeat(): void {
    this.stopHeartbeat()
    this.lastMsgAt = Date.now()
    this.heartbeat = setInterval(() => this.onHeartbeatTick(), this.pingIntervalMs)
  }

  private stopHeartbeat(): void {
    if (this.heartbeat) {
      clearInterval(this.heartbeat)
      this.heartbeat = null
    }
  }

  private onHeartbeatTick(): void {
    const ws = this.ws
    if (!ws || this.statusValue !== 'open') return
    if (Date.now() - this.lastMsgAt > this.deadAfterMs) {
      // 长时间无任何消息（服务端 pong 都没来）：出向链路假活，掐断走重连
      this.terminate(ws)
      return
    }
    try {
      ws.send(JSON.stringify({ type: PING_TYPE }))
    } catch {
      // 发送异常交由 close 事件处理
    }
  }

  private scheduleReconnect(): void {
    this.clearReconnectTimer()
    const delay = Math.min(this.policy.baseMs * 2 ** this.attempts, this.policy.maxMs)
    this.attempts += 1
    this.timer = setTimeout(() => this.open(), delay)
  }

  private clearReconnectTimer(): void {
    if (this.timer) {
      clearTimeout(this.timer)
      this.timer = null
    }
  }

  private dispatch(msg: WsMessage): void {
    const set = this.handlers.get(msg.type)
    if (set) {
      set.forEach((h) => {
        try {
          h(msg)
        } catch (e) {
          console.error('[ws] handler error', e)
        }
      })
    }
  }

  private emitStatus(s: WsStatus): void {
    this.statusValue = s
    if (s === 'closed') {
      this.hadDisconnect = true
    }
    this.statusHandlers.forEach((h) => h(s))
  }

  private notifyResync(): void {
    if (!this.hadDisconnect) return
    this.hadDisconnect = false
    this.resyncHandlers.forEach((h) => {
      try {
        h()
      } catch (e) {
        console.error('[ws] resync handler error', e)
      }
    })
  }

  /** 订阅某类型消息；返回退订函数。 */
  on(type: string, handler: WsHandler): () => void {
    let set = this.handlers.get(type)
    if (!set) {
      set = new Set()
      this.handlers.set(type, set)
    }
    set.add(handler)
    return () => {
      set?.delete(handler)
      if (set && set.size === 0) {
        this.handlers.delete(type)
      }
    }
  }

  /** 订阅连接状态变化（订阅时立即回放当前状态）；返回退订函数。 */
  onStatus(handler: (s: WsStatus) => void): () => void {
    this.statusHandlers.add(handler)
    handler(this.statusValue)
    return () => {
      this.statusHandlers.delete(handler)
    }
  }

  /** 订阅「断线后重连成功」事件（首次连接不触发）；页面用于重连后全量补拉数据。 */
  onReconnected(handler: () => void): () => void {
    this.resyncHandlers.add(handler)
    return () => {
      this.resyncHandlers.delete(handler)
    }
  }

  close(): void {
    this.stopped = true
    this.clearReconnectTimer()
    this.disarmConnectWatchdog()
    this.stopHeartbeat()
    this.unbindLifecycle()
    if (this.ws) {
      const ws = this.ws
      this.ws = null
      ws.onclose = null
      ws.close()
    }
  }

  /** 当前连接状态（供 useSyncExternalStore 快照读取）。 */
  get status(): WsStatus {
    return this.statusValue
  }

  /** 当前重连次数（测试用）。 */
  get reconnectAttempts(): number {
    return this.attempts
  }
}

/** 默认端点：跟随页面协议（https → wss，避免混合内容被浏览器拦截）。 */
export function defaultWSUrl(): string {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${location.host}/api/v1/ws`
}

/** 全局单例：页面组件通过 hooks 使用；getWS 惰性创建，不依赖挂载顺序。 */
let globalWS: GatewayWS | null = null
let globalUrl = defaultWSUrl()

/**
 * 显式初始化全局 WS 并立即连接（幂等：已初始化时原样返回，忽略传入 url）。
 * 通常无需手动调用 —— getWS/hook 会按需惰性创建。
 */
export function initWS(url = globalUrl): GatewayWS {
  if (!globalWS) {
    globalUrl = url
    globalWS = new GatewayWS(url)
    globalWS.connect()
  }
  return globalWS
}

/** 获取全局单例；未初始化时自动创建并连接。 */
export function getWS(): GatewayWS {
  return initWS()
}

/** 关闭并清空全局单例（测试 / 需要重建连接时用）。 */
export function resetWS(): void {
  globalWS?.close()
  globalWS = null
}

/** 订阅某类型实时消息（组件级，自动退订）。 */
export function useWsEvent(type: string, handler: WsHandler): void {
  const ref = useRef(handler)
  ref.current = handler
  useEffect(() => {
    const ws = getWS()
    return ws.on(type, (msg) => ref.current(msg))
  }, [type])
}

function statusSnapshot(): WsStatus {
  return globalWS ? globalWS.status : 'connecting'
}

function subscribeStatus(onChange: () => void): () => void {
  return getWS().onStatus(onChange)
}

/**
 * 连接状态 hook（驱动顶栏呼吸灯）。
 * 基于 useSyncExternalStore：订阅即读到当前状态，不会出现「连接已 open 却停留在 SYNC」。
 */
export function useWsStatus(): WsStatus {
  return useSyncExternalStore(subscribeStatus, statusSnapshot, statusSnapshot)
}

/** 订阅「断线重连成功」事件（组件级，自动退订）：断连窗口内事件会丢失，页面借此全量补拉。 */
export function useWsReconnected(handler: () => void): void {
  const ref = useRef(handler)
  ref.current = handler
  useEffect(() => {
    const ws = getWS()
    return ws.onReconnected(() => ref.current())
  }, [])
}

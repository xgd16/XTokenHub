import { useCallback, useEffect, useRef, useState } from 'react'
import { statsApi, todaySince, type Summary } from '../api/stats'
import { WS_EVENTS, useWsEvent, useWsReconnected } from '../api/ws'
import { compactNumber } from '../utils/format'
import { useMoneyFormat } from '../utils/useMoneyFormat'

/** 遥测读数格：微标签（mono）+ 等宽数字，值变化时闪一下（复用全局 .tick）。 */
function Readout({ label, value, tone }: { label: string; value: string; tone?: 'accent' | 'warn' }) {
  return (
    <div className="ticker-cell">
      <span className="ticker-label">{label}</span>
      {/* key 随值变化：重挂载以重放 .tick 的数字跳动 */}
      <span key={value} className={`ticker-value mono tick${tone ? ` is-${tone}` : ''}`}>
        {value}
      </span>
    </div>
  )
}

/**
 * 顶栏遥测条：当日请求 / Token / 费用（错误数 > 0 时额外亮一格告警）。
 * 数据源与仪表盘一致（summary + todaySince），随 WS 推送节流刷新，兜底每分钟轮询。
 */
export default function TopTicker() {
  const [sum, setSum] = useState<Summary | null>(null)
  const { format: money } = useMoneyFormat()
  const lastPush = useRef(0)

  const load = useCallback(() => {
    statsApi
      .summary(24, todaySince())
      .then(setSum)
      .catch(() => {
        /* 顶栏读数失败不打扰：保留上一次的值 */
      })
  }, [])

  useEffect(() => {
    void load()
    const timer = window.setInterval(load, 60_000)
    return () => window.clearInterval(timer)
  }, [load])

  // 重连后补一次，缺口用轮询兜底
  useWsReconnected(load)

  // 推送密集时段按 3s 节流，避免每条请求都打一次统计接口
  useWsEvent(WS_EVENTS.statsUpdated, () => {
    const now = Date.now()
    if (now - lastPush.current < 3000) return
    lastPush.current = now
    void load()
  })

  return (
    <div className="bar-ticker" aria-live="off">
      <Readout label="今日请求" value={sum ? compactNumber(sum.total_requests) : '—'} />
      <Readout label="今日 Tokens" value={sum ? compactNumber(sum.total_tokens) : '—'} />
      <Readout label="今日费用" value={sum ? money(sum.cost_usd) : '—'} tone="accent" />
      {sum && sum.error_requests > 0 && (
        <Readout label="错误" value={compactNumber(sum.error_requests)} tone="warn" />
      )}
    </div>
  )
}
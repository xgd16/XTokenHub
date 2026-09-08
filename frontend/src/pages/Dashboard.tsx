import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { TableColumnsType } from 'antd'
import { Button, Card, Col, Drawer, Row, Segmented, Table, Tag, Tooltip, Typography } from 'antd'
import { LoadingOutlined } from '@ant-design/icons'
import TrendChart from '../components/TrendChart'
import LiveDuration from '../components/LiveDuration'
import LogDetail, { DetailRows } from '../components/LogDetail'
import TokenHeatmap from '../components/TokenHeatmap'
import ModelTrendChart from '../components/ModelTrendChart'
import ModelDonut from '../components/ModelDonut'
import { AnimatedNumber } from '../components/AnimatedNumber'
import { Reveal } from '../utils/motion'
import {
  lifetimeApi,
  statsApi,
  todaySince,
  type GroupStat,
  type Lifetime,
  type ModelDayPoint,
  type Summary,
  type TrendBucket,
  type TrendPoint,
} from '../api/stats'
import { logApi, type RequestLog } from '../api/log'
import { WS_EVENTS, useWsEvent, useWsReconnected } from '../api/ws'
import { agentShort, compactCN, compactNumber, duration, durationLong, fullTime, hitRateColor, percent, protocolShort, timeOf, tokenSpeed } from '../utils/format'
import { groupBySession, toBucketSeries, toDonut, toHeatmap, toModelRank, toModelTrend, toStatCards, toTrendSeries, type HeatMode, type ModelRank, type SessionGroup } from '../utils/transform'
import { useIsMobile } from '../utils/useIsMobile'
import { useChartPalette } from '../theme'

const { Text } = Typography

/** 百分比格式化：入参为 0~100 的数值（配合 AnimatedNumber 滚动）。 */
const pct = (n: number) => `${Math.round(n * 10) / 10}%`
/** 连续天数格式化。 */
const streak = (n: number) => `${n} 天`

/** 实时流容量：初始拉取与 WS 累积上限（足量行支撑按会话合并）。 */
const LIVE_FEED_MAX = 30

/** 进行中请求：实时推送来的行尚未落库（无 id），完成事件按 req_id 原位替换。 */
const isPending = (r: RequestLog) => !r.id && !!r.req_id

/** 移动端保留的列 key，其余列收进展开行（外层会话行 / 内层请求行各一套）。 */
const MOBILE_SESSION_KEYS = new Set(['time', 'session', 'model', 'client', 'tokens', 'status', 'actions'])
const MOBILE_REQUEST_KEYS = new Set(['time', 'model', 'client', 'tokens', 'status', 'actions'])

/** 趋势图时间范围。 */
type TrendRangeKey = 'live' | 'hour' | 'd7' | 'd30'
const TREND_RANGES: { key: TrendRangeKey; label: string; hours: number; bucket: TrendBucket; count?: number; days?: number }[] = [
  { key: 'live', label: '实时', hours: 1, bucket: 'minute', count: 60 },
  { key: 'hour', label: '小时', hours: 24, bucket: 'hour', count: 24 },
  { key: 'd7', label: '7 天', hours: 168, bucket: 'day', days: 7 },
  { key: 'd30', label: '30 天', hours: 720, bucket: 'day', days: 30 },
]

/** 统计卡片：数字驱动 + 弹簧滚动；value 为 null 显示占位。 */
function StatCard(props: {
  label: string
  value: number | null
  format?: (n: number) => string
  sub?: string
  tick?: boolean
}) {
  const { label, value, format, sub, tick } = props
  const [flash, setFlash] = useState(false)
  useEffect(() => {
    if (tick) {
      setFlash(true)
      const t = setTimeout(() => setFlash(false), 900)
      return () => clearTimeout(t)
    }
  }, [value, tick])
  const num = value == null
  return (
    <div className="panel" style={{ display: 'flex', flexDirection: 'column', height: '100%', padding: '16px 18px', overflow: 'hidden' }}>
      <Text style={{ color: 'var(--text-secondary)', fontSize: 12, letterSpacing: '0.06em' }}>{label}</Text>
      <div
        className={`mono ${flash ? 'tick' : ''}`}
        style={{ fontSize: 26, fontWeight: 600, marginTop: 6, lineHeight: 1.2 }}
      >
        {num ? '—' : <AnimatedNumber value={value} format={format ?? String} />}
      </div>
      {sub && <div style={{ fontSize: 12, color: 'var(--text-faint)', marginTop: 4 }}>{sub}</div>}
    </div>
  )
}

/** 通用 TOP 条形列表（模型 TOP / 调用方 TOP 共用）。 */
function UsageBars({ rows }: { rows: ModelRank[] }) {
  const max = Math.max(...rows.map((x) => x.requests), 1)
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {rows.map((m) => (
        <div key={m.name}>
          <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 6 }}>
            <span className="mono" style={{ fontSize: 12, color: 'var(--text-primary)' }}>{m.name}</span>
            <span className="mono" style={{ fontSize: 12, color: 'var(--text-faint)' }}>
              {compactCN(m.requests)} 次 · {compactCN(m.tokens)} tok
            </span>
          </div>
          <div style={{ height: 4, borderRadius: 2, background: 'var(--track-bg)', overflow: 'hidden' }}>
            <div
              style={{
                height: '100%',
                width: `${(m.requests / max) * 100}%`,
                borderRadius: 2,
                background: 'var(--accent)',
                boxShadow: '0 0 6px var(--accent-glow)',
                transition: 'width 0.5s ease',
              }}
            />
          </div>
        </div>
      ))}
      {rows.length === 0 && (
        <div style={{ color: 'var(--text-faint)', padding: '24px 0', textAlign: 'center' }}>
          暂无数据 · 网关请求产生后实时出现
        </div>
      )}
    </div>
  )
}

/** 请求级列：会话展开后的明细表（即旧版单行视图的列）。 */
function requestColumns(isMobile: boolean, onHeaders: (r: RequestLog) => void): TableColumnsType<RequestLog> {
  return ([
    {
      key: 'time',
      title: '时间',
      width: 90,
      render: (_, r) => <span className="mono" style={{ color: 'var(--text-faint)' }}>{r.created_at ? timeOf(r.created_at) : ''}</span>,
    },
    {
      key: 'protocol',
      title: '协议',
      width: 110,
      render: (_, r) => <Tag color="cyan" style={{ background: 'transparent' }}>{protocolShort(r.protocol)}</Tag>,
    },
    {
      key: 'mode',
      title: '模式',
      width: 90,
      render: (_, r) =>
        isPending(r) || !r.forward_mode ? (
          <span style={{ color: 'var(--text-faint)' }}>—</span>
        ) : r.forward_mode === 'native_passthrough' ? (
          <Tag style={{ background: 'transparent', color: 'var(--accent)', borderColor: 'var(--accent)' }}>透传</Tag>
        ) : (
          <Tag style={{ background: 'transparent', color: 'var(--amber)', borderColor: 'var(--amber)' }}>转换</Tag>
        ),
    },
    { key: 'model', title: '模型', dataIndex: 'model', width: 160, ellipsis: true, render: (v) => <Tooltip title={v}><span className="mono">{v}</span></Tooltip> },
    { key: 'channel', title: '渠道', dataIndex: 'channel_name', width: 130, ellipsis: true },
    {
      key: 'client',
      title: '客户端',
      width: 120,
      ellipsis: true,
      render: (_, r) => (
        <Tooltip title={<span className="mono" style={{ fontSize: 12 }}>{r.user_agent || '未提供 User-Agent'}</span>}>
          <span className="mono" style={{ fontSize: 12 }}>{agentShort(r.user_agent)}</span>
        </Tooltip>
      ),
    },
    {
      key: 'tokens',
      title: 'Tokens',
      width: 170,
      render: (_, r) =>
        isPending(r) ? (
          <span style={{ color: 'var(--text-faint)' }}>—</span>
        ) : (
          <Tooltip title={`total ${compactCN(r.total_tokens)} · prompt ${compactCN(r.prompt_tokens)} · 输出 ${compactCN(r.completion_tokens)}`}>
            <div className="mono" style={{ display: 'inline-flex', alignItems: 'baseline', gap: 6 }}>
              <span>{compactCN(r.total_tokens)}</span>
              <span style={{ fontSize: 11, color: 'var(--text-faint)' }}>出{compactCN(r.completion_tokens)}</span>
            </div>
          </Tooltip>
        ),
    },
    {
      key: 'cache',
      title: '缓存命中',
      width: 160,
      render: (_, r) => {
        if (isPending(r)) return <span style={{ color: 'var(--text-faint)' }}>—</span>
        const rate = r.cache_hit_rate ?? 0
        const pct = Math.max(0, Math.min(1, rate)) * 100
        return (
          <Tooltip
            title={
              <div style={{ fontSize: 12 }}>
                <div className="mono">缓存命中率 {percent(rate)}</div>
                <div className="mono" style={{ color: 'var(--text-faint)', marginTop: 2 }}>
                  cached {compactCN(r.cached_tokens)} / prompt {compactCN(r.prompt_tokens)}
                </div>
              </div>
            }
          >
            <div style={{ display: 'inline-flex', alignItems: 'center', gap: 8, width: '100%' }}>
              <div style={{ flex: 1, height: 4, borderRadius: 2, background: 'var(--track-bg)', overflow: 'hidden' }}>
                <div
                  style={{
                    height: '100%',
                    width: `${pct}%`,
                    borderRadius: 2,
                    background: hitRateColor(rate),
                    transition: 'width 0.4s ease',
                  }}
                />
              </div>
              <span className="mono" style={{ fontSize: 12, color: hitRateColor(rate), width: 48, textAlign: 'right' }}>
                {percent(rate)}
              </span>
            </div>
          </Tooltip>
        )
      },
    },
    {
      key: 'duration',
      title: '耗时',
      width: 90,
      render: (_, r) => (isPending(r) ? <LiveDuration from={r.created_at} /> : <span className="mono">{duration(r.duration_ms)}</span>),
    },
    {
      key: 'speed',
      title: '输出速度',
      width: 110,
      render: (_, r) => (
        <span className="mono" style={{ color: 'var(--text-secondary)' }}>{isPending(r) ? '—' : tokenSpeed(r.completion_tokens, r.duration_ms)}</span>
      ),
    },
    {
      key: 'status',
      title: '状态',
      width: 80,
      render: (_, r) =>
        isPending(r) ? (
          <Tag icon={<LoadingOutlined spin />} color="processing" style={{ background: 'transparent' }}>运行中</Tag>
        ) : r.error ? (
          <Tooltip title={r.error}>
            <Tag color="error" style={{ background: 'transparent' }}>{r.upstream_status || 'ERR'}</Tag>
          </Tooltip>
        ) : (
          <Tag color="success" style={{ background: 'transparent' }}>{r.upstream_status}</Tag>
        ),
    },
    {
      key: 'actions',
      title: '操作',
      width: 80,
      render: (_, r) => (
        <Button size="small" type="text" onClick={() => onHeaders(r)}>
          查看头
        </Button>
      ),
    },
  ] as TableColumnsType<RequestLog>).filter((c) => !isMobile || MOBILE_REQUEST_KEYS.has(String(c.key)))
}

/** 会话聚合列：外层每行 = 一个调用方会话（或无会话标识的单次请求）。 */
function sessionColumns(isMobile: boolean, onHeaders: (r: RequestLog) => void): TableColumnsType<SessionGroup> {
  return ([
    {
      key: 'time',
      title: '时间',
      width: isMobile ? 90 : 110,
      render: (_, g) => {
        const t = <span className="mono" style={{ color: 'var(--text-faint)' }}>{g.firstAt ? timeOf(g.firstAt) : ''}</span>
        return g.requests.length > 1 ? (
          <Tooltip title={<span className="mono" style={{ fontSize: 12 }}>{fullTime(g.firstAt)} ~ {fullTime(g.lastAt)}</span>}>{t}</Tooltip>
        ) : t
      },
    },
    {
      key: 'session',
      title: '会话',
      width: isMobile ? 120 : 180,
      ellipsis: true,
      render: (_, g) =>
        !g.sessionId ? (
          <span style={{ color: 'var(--text-faint)' }}>—</span>
        ) : (
          <Tooltip title={<span className="mono" style={{ fontSize: 12 }}>{g.sessionId}</span>}>
            <span className="mono" style={{ display: 'inline-flex', alignItems: 'baseline', gap: 6 }}>
              <span>{g.sessionId.slice(0, 8)}</span>
              <span style={{ fontSize: 11, color: 'var(--accent)' }}>×{g.requests.length}</span>
            </span>
          </Tooltip>
        ),
    },
    {
      key: 'model',
      title: '模型',
      width: 160,
      ellipsis: true,
      render: (_, g) => {
        const v = g.models.join('、') || '—'
        return (
          <Tooltip title={v}>
            <span className="mono">{v}</span>
          </Tooltip>
        )
      },
    },
    { key: 'channel', title: '渠道', width: 120, ellipsis: true, render: (_, g) => g.channels.join('、') || '—' },
    {
      key: 'client',
      title: '客户端',
      width: 120,
      ellipsis: true,
      render: (_, g) => (
        <Tooltip title={<span className="mono" style={{ fontSize: 12 }}>{g.userAgent || '未提供 User-Agent'}</span>}>
          <span className="mono" style={{ fontSize: 12 }}>{agentShort(g.userAgent)}</span>
        </Tooltip>
      ),
    },
    {
      key: 'tokens',
      title: 'Tokens',
      width: 170,
      render: (_, g) =>
        g.requests.length === 1 && isPending(g.requests[0]) ? (
          <span style={{ color: 'var(--text-faint)' }}>—</span>
        ) : (
          <Tooltip title={`total ${compactCN(g.totalTokens)} · prompt ${compactCN(g.promptTokens)} · 输出 ${compactCN(g.completionTokens)}`}>
            <div className="mono" style={{ display: 'inline-flex', alignItems: 'baseline', gap: 6 }}>
              <span>{compactCN(g.totalTokens)}</span>
              <span style={{ fontSize: 11, color: 'var(--text-faint)' }}>出{compactCN(g.completionTokens)}</span>
            </div>
          </Tooltip>
        ),
    },
    {
      key: 'cache',
      title: '缓存命中',
      width: 160,
      render: (_, g) => {
        if (g.requests.length === 1 && isPending(g.requests[0])) return <span style={{ color: 'var(--text-faint)' }}>—</span>
        const rate = g.promptTokens > 0 ? g.cachedTokens / g.promptTokens : 0
        const pct = Math.max(0, Math.min(1, rate)) * 100
        return (
          <Tooltip
            title={
              <div style={{ fontSize: 12 }}>
                <div className="mono">缓存命中率 {percent(rate)}</div>
                <div className="mono" style={{ color: 'var(--text-faint)', marginTop: 2 }}>
                  cached {compactCN(g.cachedTokens)} / prompt {compactCN(g.promptTokens)}
                </div>
              </div>
            }
          >
            <div style={{ display: 'inline-flex', alignItems: 'center', gap: 8, width: '100%' }}>
              <div style={{ flex: 1, height: 4, borderRadius: 2, background: 'var(--track-bg)', overflow: 'hidden' }}>
                <div
                  style={{
                    height: '100%',
                    width: `${pct}%`,
                    borderRadius: 2,
                    background: hitRateColor(rate),
                    transition: 'width 0.4s ease',
                  }}
                />
              </div>
              <span className="mono" style={{ fontSize: 12, color: hitRateColor(rate), width: 48, textAlign: 'right' }}>
                {percent(rate)}
              </span>
            </div>
          </Tooltip>
        )
      },
    },
    {
      key: 'duration',
      title: '耗时',
      width: 90,
      render: (_, g) => {
        if (g.requests.length === 1) {
          const r = g.requests[0]
          return isPending(r) ? <LiveDuration from={r.created_at} /> : <span className="mono">{duration(r.duration_ms)}</span>
        }
        return (
          <Tooltip title={<span className="mono" style={{ fontSize: 12 }}>合计 {duration(g.totalMs)} · 平均 {duration(Math.round(g.totalMs / g.requests.length))}</span>}>
            <span className="mono">{duration(g.totalMs)}</span>
          </Tooltip>
        )
      },
    },
    {
      key: 'speed',
      title: '输出速度',
      width: 110,
      render: (_, g) => {
        if (g.requests.length === 1 && isPending(g.requests[0])) return <span style={{ color: 'var(--text-faint)' }}>—</span>
        return <span className="mono" style={{ color: 'var(--text-secondary)' }}>{tokenSpeed(g.completionTokens, g.totalMs)}</span>
      },
    },
    {
      key: 'status',
      title: '状态',
      width: 90,
      render: (_, g) => {
        if (g.running > 0) {
          return <Tag icon={<LoadingOutlined spin />} color="processing" style={{ background: 'transparent' }}>运行中</Tag>
        }
        if (g.requests.length === 1) {
          const r = g.requests[0]
          if (r.error) {
            return (
              <Tooltip title={r.error}>
                <Tag color="error" style={{ background: 'transparent' }}>{r.upstream_status || 'ERR'}</Tag>
              </Tooltip>
            )
          }
          return <Tag color="success" style={{ background: 'transparent' }}>{r.upstream_status}</Tag>
        }
        if (g.errors > 0) {
          return (
            <Tooltip title={`${g.errors} / ${g.requests.length} 次失败`}>
              <Tag color="error" style={{ background: 'transparent' }}>{g.errors} 错</Tag>
            </Tooltip>
          )
        }
        return <Tag color="success" style={{ background: 'transparent' }}>{g.requests.length} 次</Tag>
      },
    },
    {
      key: 'actions',
      title: '操作',
      width: 80,
      render: (_, g) =>
        g.requests.length === 1 ? (
          <Button size="small" type="text" onClick={() => onHeaders(g.requests[0])}>
            查看头
          </Button>
        ) : null,
    },
  ] as TableColumnsType<SessionGroup>).filter((c) => !isMobile || MOBILE_SESSION_KEYS.has(String(c.key)))
}

/** 会话组展开内容：该会话内的请求级明细。 */
function SessionDetail(props: { g: SessionGroup; isMobile: boolean; onHeaders: (r: RequestLog) => void }) {
  const { g, isMobile, onHeaders } = props
  return (
    <Table<RequestLog>
      size="small"
      rowKey={(r) => (r.req_id && !r.id ? `live-${r.req_id}` : `${r.id}-${r.created_at}`)}
      dataSource={g.requests}
      pagination={false}
      style={{ margin: '2px 0 4px' }}
      expandable={isMobile ? { expandedRowRender: (r) => <LogDetail r={r} pending={isPending(r)} /> } : undefined}
      columns={requestColumns(isMobile, onHeaders)}
    />
  )
}

/** 仪表盘：当天概览 + 全历史累计 + 热力图 + 模型趋势/用量 + 实时请求流。 */
export default function Dashboard() {
  const isMobile = useIsMobile()
  const pal = useChartPalette()
  const [summary, setSummary] = useState<Summary | null>(null)
  const [trend, setTrend] = useState<TrendPoint[]>([])
  const [byModel, setByModel] = useState<GroupStat[]>([])
  const [live, setLive] = useState<RequestLog[]>([])

  // 全历史累计（lifetime）与 Token 活动热力图（近一年按日）
  const [lifetime, setLifetime] = useState<Lifetime | null>(null)
  const [heatPoints, setHeatPoints] = useState<TrendPoint[]>([])
  const [heatMode, setHeatMode] = useState<HeatMode>('daily')

  // 每日模型趋势 / 模型用量（近 7 日 / 近 30 日）
  const [rangeDays, setRangeDays] = useState<7 | 30>(7)
  const [modelTrend, setModelTrend] = useState<ModelDayPoint[]>([])

  // 调用方 TOP（近 30 天，按密钥聚合）
  const [byKey, setByKey] = useState<GroupStat[]>([])

  const tickRef = useRef(0)
  const [, setTick] = useState(0)

  // 实时输出 token 吞吐：由后端 stats.throughput 事件以 2Hz 推送
  const [tps, setTps] = useState(0)

  // 趋势图范围（默认实时）；refresh 经 ref 读取当前值，避免重建回调断开 WS 订阅
  const [range, setRange] = useState<TrendRangeKey>('live')
  const rangeRef = useRef(range)
  rangeRef.current = range
  const rangeDaysRef = useRef(rangeDays)
  rangeDaysRef.current = rangeDays
  const lastLiveRefreshRef = useRef(0)
  const lastStatsRefreshRef = useRef(0)
  const lastSlowRefreshRef = useRef(0)

  // 快速刷新：当天汇总 + 请求趋势 + 当天模型 TOP
  const refresh = useCallback(async () => {
    tickRef.current += 1
    const r = TREND_RANGES.find((x) => x.key === rangeRef.current) ?? TREND_RANGES[2]
    const [s, t, m] = await Promise.all([
      statsApi.summary(24, todaySince()),
      statsApi.trend(r.hours, r.bucket),
      statsApi.byModel(24, todaySince()),
    ])
    setSummary(s)
    setTrend(t)
    setByModel(m)
    setTick((x) => x + 1)
  }, [])

  // 慢速刷新：全历史累计 + 热力图（26 周）+ 模型趋势/用量 + 调用方 TOP（按日聚合，无需高频）
  const refreshSlow = useCallback(async () => {
    const days = rangeDaysRef.current
    const [lt, hp, mt, bk] = await Promise.all([
      lifetimeApi.get(),
      statsApi.trendByDay(190),
      lifetimeApi.trendByModel(days),
      statsApi.byKey(720),
    ])
    setLifetime(lt)
    setHeatPoints(hp)
    setModelTrend(mt)
    setByKey(bk)
  }, [])

  useEffect(() => {
    void refresh()
    void refreshSlow()
    // 初始拉取最近请求流
    void logApi.list({ page: 1, per_page: LIVE_FEED_MAX }).then((page) => setLive(page.items ?? []))
  }, [refresh, refreshSlow])

  // 实时：请求受理即滑入"运行中"行；完成事件按 req_id 原位替换为最终结果。
  // 幂等保护：completed 偶发先于 started 到达（或重复推送）时，以先到者为准，避免行错乱。
  useWsEvent(WS_EVENTS.requestStarted, (msg) => {
    const l = msg.payload as RequestLog
    setLive((prev) =>
      l.req_id && prev.some((x) => x.req_id === l.req_id)
        ? prev
        : [{ ...l }, ...prev].slice(0, LIVE_FEED_MAX),
    )
  })
  useWsEvent(WS_EVENTS.requestCompleted, (msg) => {
    const l = msg.payload as RequestLog
    setLive((prev) => {
      if (l.req_id && prev.some((x) => x.req_id === l.req_id && !!x.id)) return prev
      const idx = l.req_id ? prev.findIndex((x) => x.req_id === l.req_id && !x.id) : -1
      if (idx < 0) return [l, ...prev].slice(0, LIVE_FEED_MAX)
      const next = [...prev]
      next[idx] = l
      return next
    })
  })
  useWsEvent(WS_EVENTS.throughput, (msg) => {
    const p = msg.payload as { tokens_per_sec?: number }
    setTps(Number.isFinite(p?.tokens_per_sec) ? p.tokens_per_sec! : 0)
    // 实时档：借 2Hz 吞吐推送节流刷新趋势（约 4s 一次），无流量时也能看到空桶推进
    if (rangeRef.current === 'live') {
      const now = Date.now()
      if (now - lastLiveRefreshRef.current > 4000) {
        lastLiveRefreshRef.current = now
        void refresh()
      }
    }
  })
  useWsEvent(WS_EVENTS.statsUpdated, () => {
    // 突发请求时该事件高频触发：快速数据节流 1s，慢速数据节流 60s
    const now = Date.now()
    if (now - lastStatsRefreshRef.current > 1000) {
      lastStatsRefreshRef.current = now
      void refresh()
    }
    if (now - lastSlowRefreshRef.current > 60_000) {
      lastSlowRefreshRef.current = now
      void refreshSlow()
    }
  })

  // 断线重连后全量补拉：断连窗口内的事件已丢失，节流窗口一并重置
  useWsReconnected(() => {
    lastStatsRefreshRef.current = 0
    lastSlowRefreshRef.current = 0
    lastLiveRefreshRef.current = 0
    void refresh()
    void refreshSlow()
    void logApi.list({ page: 1, per_page: LIVE_FEED_MAX }).then((p) => setLive(p.items ?? []))
  })

  // 兜底清理：WS 断连窗口内 completed 丢失的"运行中"行，超过 5 分钟自动移除
  useEffect(() => {
    const t = setInterval(() => {
      setLive((prev) => {
        const cutoff = Date.now() - 5 * 60 * 1000
        const next = prev.filter((r) => !isPending(r) || new Date(r.created_at).getTime() > cutoff)
        return next.length === prev.length ? prev : next
      })
    }, 10_000)
    return () => clearInterval(t)
  }, [])

  // 查看请求头
  const [headerLog, setHeaderLog] = useState<RequestLog | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const showHeaders = useCallback((r: RequestLog) => {
    setHeaderLog(r)
    setDrawerOpen(true)
  }, [])

  // 实时流按会话聚合展示（无 X-Session-Id 的调用方保持单行）
  const liveGroups = useMemo(() => groupBySession(live), [live])

  const cards = summary ? toStatCards(summary) : null
  const rangeConf = TREND_RANGES.find((x) => x.key === range) ?? TREND_RANGES[2]
  const series = useMemo(
    () =>
      rangeConf.bucket === 'day'
        ? toTrendSeries(trend, rangeConf.days ?? 7)
        : toBucketSeries(trend, rangeConf.bucket === 'minute' ? 60 : 3600, rangeConf.count ?? 24),
    [trend, rangeConf],
  )
  const ranked = useMemo(() => toModelRank(byModel, 6), [byModel])
  const keyRanked = useMemo(() => toModelRank(byKey, 6), [byKey])

  const heat = useMemo(() => toHeatmap(heatPoints, heatMode, new Date(), 26), [heatPoints, heatMode])
  const mt = useMemo(() => toModelTrend(modelTrend, rangeDays), [modelTrend, rangeDays])
  const donut = useMemo(() => toDonut(mt.series, 5), [mt])

  const panelHeaderStyle = { fontSize: 13, color: 'var(--text-secondary)', letterSpacing: '0.06em' } as const

  return (
    <Reveal style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <Row gutter={[16, 16]} align="stretch">
        <Col xs={12} md={8} lg={4}>
          <StatCard label="请求总数 · 今天" value={cards ? cards.requests : null} format={compactNumber} tick />
        </Col>
        <Col xs={12} md={8} lg={4}>
          <StatCard label="错误请求 · 今天" value={cards ? cards.errors : null} format={compactNumber} sub="当天失败请求" />
        </Col>
        <Col xs={12} md={8} lg={4}>
          <StatCard label="Token 用量" value={summary ? summary.total_tokens : null} format={compactCN} sub="今天 · prompt + completion" tick />
        </Col>
        <Col xs={12} md={8} lg={4}>
          <StatCard label="缓存命中率" value={summary ? summary.cache_hit_rate * 100 : null} format={pct} sub="cached / prompt" />
        </Col>
        <Col xs={12} md={8} lg={4}>
          <StatCard label="平均耗时" value={cards ? cards.avgMs : null} format={duration} sub="今天成功请求均值" />
        </Col>
        <Col xs={12} md={8} lg={4}>
          <StatCard label="原生透传占比" value={summary ? summary.native_ratio * 100 : null} format={pct} sub="零转换直连上游" />
        </Col>
      </Row>

      <Row gutter={[16, 16]} align="stretch">
        <Col xs={12} md={8} lg={4}>
          <StatCard label="累计 Token 数" value={lifetime ? lifetime.total_tokens : null} format={compactCN} sub={`共 ${lifetime?.active_days ?? 0} 天有用量`} tick />
        </Col>
        <Col xs={12} md={8} lg={4}>
          <StatCard label="峰值 Token 数" value={lifetime ? lifetime.peak_day_tokens : null} format={compactCN} sub={lifetime?.peak_day || '单日最高'} />
        </Col>
        <Col xs={12} md={8} lg={4}>
          <StatCard label="最长聊天时长" value={lifetime ? lifetime.max_duration_ms : null} format={durationLong} sub="单次成功请求" />
        </Col>
        <Col xs={12} md={8} lg={4}>
          <StatCard label="当前连续天数" value={lifetime ? lifetime.current_streak : null} format={streak} sub="每天有请求即延续" />
        </Col>
        <Col xs={12} md={8} lg={4}>
          <StatCard label="最长连续天数" value={lifetime ? lifetime.max_streak : null} format={streak} sub="历史最长连击" />
        </Col>
      </Row>

      <Row gutter={[16, 16]} align="stretch">
        <Col xs={24} lg={14}>
          <Card
            className="panel"
            style={{ height: '100%' }}
            styles={{ body: { padding: '12px 16px 10px' }, header: { borderBottom: '1px solid var(--border-faint)' } }}
            title={
              <div style={{ display: 'flex', flexWrap: 'wrap', rowGap: 6, justifyContent: 'space-between', alignItems: 'center', gap: 12 }}>
                <span style={{ ...panelHeaderStyle, whiteSpace: 'nowrap' }}>请求趋势</span>
                <div style={{ display: 'inline-flex', alignItems: 'center', gap: 14 }}>
                  <span className="mono" style={{ fontSize: 12, color: 'var(--text-faint)' }}>
                    实时输出 <span style={{ color: 'var(--accent)' }}>{compactCN(Math.round(tps))}</span> tok/s
                  </span>
                  <Segmented
                    size="small"
                    value={range}
                    onChange={(v) => {
                      const k = v as TrendRangeKey
                      rangeRef.current = k // 立即生效，refresh 不依赖重渲染时序
                      setRange(k)
                      void refresh()
                    }}
                    options={TREND_RANGES.map((r) => ({ value: r.key, label: r.label }))}
                  />
                </div>
              </div>
            }
          >
            <TrendChart data={series} height={isMobile ? 190 : 232} ariaLabel={`${rangeConf.label}请求趋势`} />
          </Card>
        </Col>
        <Col xs={24} lg={10}>
          <Card
            className="panel"
            style={{ height: '100%' }}
            styles={{ body: { padding: '14px 16px 16px' }, header: { borderBottom: '1px solid var(--border-faint)' } }}
            title={<span style={panelHeaderStyle}>模型 TOP · 今天</span>}
          >
<UsageBars rows={ranked} />
          </Card>
        </Col>
      </Row>

      <Row gutter={[16, 16]} align="stretch">
        <Col xs={24} lg={14}>
          <Card
            className="panel"
            style={{ height: '100%' }}
            styles={{ body: { padding: '12px 16px 10px' }, header: { borderBottom: '1px solid var(--border-faint)' } }}
            title={
              <div style={{ display: 'flex', flexWrap: 'wrap', rowGap: 6, justifyContent: 'space-between', alignItems: 'center', gap: 12 }}>
                <span style={{ ...panelHeaderStyle, whiteSpace: 'nowrap' }}>每日 Token 趋势图</span>
                <Segmented
                  size="small"
                  value={rangeDays}
                  onChange={(v) => {
                    const d = v as 7 | 30
                    rangeDaysRef.current = d
                    setRangeDays(d)
                    void refreshSlow()
                  }}
                  options={[
                    { value: 7, label: '近 7 日' },
                    { value: 30, label: '近 30 日' },
                  ]}
                />
              </div>
            }
          >
            <ModelTrendChart data={mt} height={isMobile ? 200 : 244} />
          </Card>
        </Col>
        <Col xs={24} lg={10}>
          <Card
            className="panel"
            style={{ height: '100%' }}
            styles={{ body: { padding: '12px 16px' }, header: { borderBottom: '1px solid var(--border-faint)' } }}
            title={<span style={panelHeaderStyle}>模型用量</span>}
            extra={<span className="mono" style={{ fontSize: 12, color: 'var(--text-faint)' }}>{rangeDays === 7 ? '近 7 日' : '近 30 日'}</span>}
          >
            <ModelDonut slices={donut.slices} total={donut.total} size={isMobile ? 150 : 170} />
          </Card>
        </Col>
      </Row>

      <Row gutter={[16, 16]} align="stretch">
        <Col xs={24} lg={14}>
          <Card
            className="panel"
            style={{ height: '100%' }}
            styles={{ body: { padding: '14px 16px 12px' }, header: { borderBottom: '1px solid var(--border-faint)' } }}
            title={<span style={panelHeaderStyle}>Token 活动</span>}
            extra={
              <Segmented
                size="small"
                value={heatMode}
                onChange={(v) => setHeatMode(v as HeatMode)}
                options={[
                  { value: 'daily', label: '每日' },
                  { value: 'weekly', label: '每周' },
                  { value: 'cumulative', label: '累计' },
                ]}
              />
            }
          >
            <TokenHeatmap data={heat} mode={heatMode} />
            <div style={{ display: 'flex', justifyContent: 'flex-end', alignItems: 'center', gap: 4, marginTop: 8, fontSize: 11, color: 'var(--text-faint)' }}>
              <span>少</span>
              {[0, 1, 2, 3, 4].map((lv) => (
                <span
                  key={lv}
                  style={{
                    width: 10,
                    height: 10,
                    borderRadius: 2,
                    background: pal.heat[lv],
                  }}
                />
              ))}
              <span>多</span>
            </div>
          </Card>
        </Col>
        <Col xs={24} lg={10}>
          <Card
            className="panel"
            style={{ height: '100%' }}
            styles={{ body: { padding: '14px 16px 16px' }, header: { borderBottom: '1px solid var(--border-faint)' } }}
            title={<span style={panelHeaderStyle}>调用方 TOP · 30 天</span>}
            extra={<span className="mono" style={{ fontSize: 12, color: 'var(--text-faint)' }}>按密钥</span>}
          >
            <UsageBars rows={keyRanked} />
          </Card>
        </Col>
      </Row>

      <Card
        className="panel"
        styles={{ body: { padding: 0 }, header: { borderBottom: "1px solid var(--border-faint)" } }}
        title={
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
            <span className="live-dot" />
            <span style={panelHeaderStyle}>实时请求流</span>
          </span>
        }
      >
        <Table<SessionGroup>
          rowKey="key"
          dataSource={liveGroups}
          pagination={false}
          size="small"
          scroll={{ x: isMobile ? 830 : 1390 }}
          rowClassName={(_, i) => (i === 0 ? 'row-live' : '')}
          onRow={(_, i) => ({ className: i === 0 ? 'row-live' : '' })}
          expandable={{
            expandedRowRender: (g) =>
              g.sessionId ? (
                <SessionDetail g={g} isMobile={isMobile} onHeaders={showHeaders} />
              ) : isMobile ? (
                <LogDetail r={g.requests[0]} pending={isPending(g.requests[0])} />
              ) : null,
            rowExpandable: (g) => !!g.sessionId || isMobile,
          }}
          columns={sessionColumns(isMobile, showHeaders)}
        />
      </Card>

      <Drawer
        title="请求头"
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        width={isMobile ? '100%' : 520}
        destroyOnHidden
      >
        {headerLog?.request_headers ? (
          <DetailRows
            rows={Object.entries(JSON.parse(headerLog.request_headers)).map(([k, v]) => [
              k,
              Array.isArray(v) ? v.join(', ') : String(v),
            ])}
          />
        ) : (
          <div style={{ color: 'var(--text-faint)', padding: '24px 0', textAlign: 'center' }}>无请求头数据</div>
        )}
      </Drawer>

      <div style={{ color: 'var(--text-faint)', fontSize: 12 }} className="mono">
        统计窗口 今天（本地零点起） · 更新于 {summary ? fullTime(new Date().toISOString()) : '—'} · 数据经 WebSocket 实时推送
      </div>
    </Reveal>
  )
}

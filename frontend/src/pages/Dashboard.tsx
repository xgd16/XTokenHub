import { memo, useCallback, useEffect, useMemo, useRef, useState, type Key } from 'react'
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
  type LiveSession,
  type ModelDayPoint,
  type Summary,
  type TrendBucket,
  type TrendPoint,
} from '../api/stats'
import { logApi, type RequestLog } from '../api/log'
import { costApi, type CostForecast } from '../api/pricing'
import { WS_EVENTS, useWsEvent, useWsReconnected } from '../api/ws'
import { agentShort, compactCN, compactNumber, duration, durationLong, fullTime, hitRateColor, percent, protocolShort, timeOf, tokenSpeed } from '../utils/format'
import { mergeLiveEvents, mergeSessionViews, toBucketSeries, toDonut, toHeatmap, toModelRank, toModelTrend, toStatCards, toTrendSeries, type HeatMode, type LivePhase, type ModelRank, type SessionGroup } from '../utils/transform'
import { useIsMobile } from '../utils/useIsMobile'
import { useMoneyFormat } from '../utils/useMoneyFormat'
import { useChartPalette } from '../theme'

const { Text } = Typography

/** 百分比格式化：入参为 0~100 的数值（配合 AnimatedNumber 滚动）。 */
const pct = (n: number) => `${Math.round(n * 10) / 10}%`
/** 连续天数格式化（滚动中间值带小数，需自己取整）。 */
const streak = (n: number) => `${Math.round(n)} 天`
/** 未指定 format 的卡片默认格式（纯计数类，取整避免滚动时露小数）。 */
const intText = (n: number) => String(Math.round(n))

/** 预测基准的中文说明。 */
const basisLabel = (basis: string) => (basis === 'run_rate' ? '按近 7 日均值' : '按当前速率')
/** 预测置信度的中文说明。 */
const confidenceLabel = (c: string) => (c === 'high' ? '高' : c === 'medium' ? '中' : '低')

/**
 * 实时流容量：仅用于「尚未落库」的进行中行缓冲（后端已提供全量会话合计，
 * 前端不再靠自留窗口做聚合）。裁剪按会话整组移除，见 trimLiveRows。
 */
const LIVE_FEED_MAX = 200

/** 展示的会话组数上限：只保留最近 N 个活跃会话，合计口径由后端保证。 */
const LIVE_GROUP_MAX = 20

/** 会话展开明细条数上限：只拉最近 N 条，合计仍为后端全量口径。 */
const LIVE_DETAIL_MAX = 20

/** 会话视图刷新节流：突发流量下避免每个请求都打一次接口。 */
const LIVE_SESSIONS_THROTTLE_MS = 2000

/**
 * 实时事件合流窗口：同一窗口内的 started/completed 合并为一次 setState。
 * 突发流量下避免「每条事件一次全表重渲染」，同时保持 ≤120ms 的感知延迟。
 */
const LIVE_FLUSH_MS = 120

/** 进行中请求：实时推送来的行尚未落库（无 id），完成事件按 req_id 原位替换。 */
const isPending = (r: RequestLog) => !r.id && !!r.req_id

/** 移动端保留的列 key，其余列收进展开行（外层会话行 / 内层请求行各一套）。 */
const MOBILE_SESSION_KEYS = new Set(['time', 'session', 'protocol', 'mode', 'model', 'client', 'tokens', 'status', 'actions'])
const MOBILE_REQUEST_KEYS = new Set(['time', 'model', 'client', 'tokens', 'status', 'actions'])

/** 趋势图时间范围。 */
type TrendRangeKey = 'live' | 'hour' | 'd7' | 'd30'
const TREND_RANGES: { key: TrendRangeKey; label: string; hours: number; bucket: TrendBucket; count?: number; days?: number }[] = [
  { key: 'live', label: '实时', hours: 1, bucket: 'minute', count: 60 },
  { key: 'hour', label: '小时', hours: 24, bucket: 'hour', count: 24 },
  { key: 'd7', label: '7 天', hours: 168, bucket: 'day', days: 7 },
  { key: 'd30', label: '30 天', hours: 720, bucket: 'day', days: 30 },
]

/** 统计卡片：数字驱动 + 弹簧滚动；value 为 null 显示骨架占位。 */
const StatCard = memo(function StatCard(props: {
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
  const loading = value == null
  return (
    <div className="panel" style={{ display: 'flex', flexDirection: 'column', height: '100%', padding: '16px 18px', overflow: 'hidden' }}>
      <Text style={{ color: 'var(--text-secondary)', fontSize: 12, letterSpacing: '0.06em' }}>{label}</Text>
      {loading ? (
        <div style={{ marginTop: 6 }}>
          <div className="skeleton-stat-number" />
        </div>
      ) : (
        <div
          className={`mono ${flash ? 'tick' : ''}`}
          style={{ fontSize: 26, fontWeight: 600, marginTop: 6, lineHeight: 1.2 }}
        >
          <AnimatedNumber value={value} format={format ?? intText} />
        </div>
      )}
      {loading ? (
        <div className="skeleton-stat-sub" />
      ) : (
        sub && <div style={{ fontSize: 12, color: 'var(--text-faint)', marginTop: 4 }}>{sub}</div>
      )}
    </div>
  )
})

/** 实时输出吞吐标签：自行订阅 2Hz 吞吐事件，把高频刷新限制在这一个文本节点内，
 *  不再牵动表格 / 图表 / 卡片（此前它作为 Dashboard 状态会导致整页每秒多次重渲染）。 */
const ThroughputTicker = memo(function ThroughputTicker() {
  const [tps, setTps] = useState(0)
  useWsEvent(WS_EVENTS.throughput, (msg) => {
    const p = msg.payload as { tokens_per_sec?: number }
    setTps(Number.isFinite(p?.tokens_per_sec) ? p.tokens_per_sec! : 0)
  })
  return (
    <span className="mono" style={{ fontSize: 12, color: 'var(--text-faint)' }}>
      实时输出 <span style={{ color: 'var(--accent)' }}>{compactCN(Math.round(tps))}</span> tok/s
    </span>
  )
})

/** 通用 TOP 条形列表骨架（加载中）。 */
function UsageBarsSkeleton() {
  const widths = [80, 60, 45]
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      {widths.map((w, i) => (
        <div key={i}>
          <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 6 }}>
            <span className="skeleton-bar" style={{ width: `${w}%`, height: 12 }} />
            <span className="skeleton-bar" style={{ width: '25%', height: 12, animationDelay: '0.1s' }} />
          </div>
          <div className="skeleton-bar-track">
            <div className="skeleton-bar-fill" style={{ width: `${90 - i * 25}%`, animationDelay: `${i * 0.15}s` }} />
          </div>
        </div>
      ))}
    </div>
  )
}

/** 通用 TOP 条形列表（模型 TOP / 调用方 TOP 共用）。 */
const UsageBars = memo(function UsageBars({ rows, loading = false }: { rows: ModelRank[]; loading?: boolean }) {
  if (loading && rows.length === 0) return <UsageBarsSkeleton />
  const { format: money } = useMoneyFormat()
  const max = Math.max(...rows.map((x) => x.requests), 1)
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {rows.map((m) => (
        <div key={m.name}>
          <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 6 }}>
            <span className="mono" style={{ fontSize: 12, color: 'var(--text-primary)' }}>{m.name}</span>
            <span className="mono" style={{ fontSize: 12, color: 'var(--text-faint)' }}>
              {compactCN(m.requests)} 次 · {compactCN(m.tokens)} tok · {money(m.costUSD)}
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
})

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
  // 单次请求且明细已就位（散行 / 进行中的新会话）时才展示行级字段
  const singleOf = (g: SessionGroup): RequestLog | null =>
    g.count === 1 && g.requests.length === 1 ? g.requests[0] : null
  return ([
    {
      key: 'time',
      title: '时间',
      width: isMobile ? 90 : 110,
      render: (_, g) => {
        // 列表按最近活跃排序，故展示组内最新请求时间；悬停看首末范围
        const t = <span className="mono" style={{ color: 'var(--text-faint)' }}>{g.lastAt ? timeOf(g.lastAt) : ''}</span>
        return g.count > 1 ? (
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
              <span style={{ fontSize: 11, color: 'var(--accent)' }}>×{g.count}</span>
            </span>
          </Tooltip>
        ),
    },
    {
      key: 'protocol',
      title: '协议',
      width: 110,
      render: (_, g) => {
        const v = g.protocols.map(protocolShort).join('、') || '—'
        return (
          <Tooltip title={v}>
            <Tag color="cyan" style={{ background: 'transparent' }}>{v}</Tag>
          </Tooltip>
        )
      },
    },
    {
      key: 'mode',
      title: '模式',
      width: 90,
      render: (_, g) => {
        if (g.modes.length === 0) return <span style={{ color: 'var(--text-faint)' }}>—</span>
        const passthrough = g.modes.includes('native_passthrough')
        const converted = g.modes.includes('converted')
        const label = passthrough && converted ? '透传、转换' : passthrough ? '透传' : '转换'
        return (
          <Tag
            style={{
              background: 'transparent',
              color: converted && !passthrough ? 'var(--amber)' : 'var(--accent)',
              borderColor: converted && !passthrough ? 'var(--amber)' : 'var(--accent)',
            }}
          >
            {label}
          </Tag>
        )
      },
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
      render: (_, g) => {
        const r = singleOf(g)
        return r && isPending(r) ? (
          <span style={{ color: 'var(--text-faint)' }}>—</span>
        ) : (
          <Tooltip title={`total ${compactCN(g.totalTokens)} · prompt ${compactCN(g.promptTokens)} · 输出 ${compactCN(g.completionTokens)}`}>
            <div className="mono" style={{ display: 'inline-flex', alignItems: 'baseline', gap: 6 }}>
              <span>{compactCN(g.totalTokens)}</span>
              <span style={{ fontSize: 11, color: 'var(--text-faint)' }}>出{compactCN(g.completionTokens)}</span>
            </div>
          </Tooltip>
        )
      },
    },
    {
      key: 'cache',
      title: '缓存命中',
      width: 160,
      render: (_, g) => {
        const r = singleOf(g)
        if (r && isPending(r)) return <span style={{ color: 'var(--text-faint)' }}>—</span>
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
        const r = singleOf(g)
        if (r) {
          return isPending(r) ? <LiveDuration from={r.created_at} /> : <span className="mono">{duration(r.duration_ms)}</span>
        }
        return (
          <Tooltip title={<span className="mono" style={{ fontSize: 12 }}>合计 {duration(g.totalMs)} · 平均 {duration(Math.round(g.totalMs / g.count))}</span>}>
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
        const r = singleOf(g)
        if (r && isPending(r)) return <span style={{ color: 'var(--text-faint)' }}>—</span>
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
        const r = singleOf(g)
        if (r) {
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
            <Tooltip title={`${g.errors} / ${g.count} 次失败`}>
              <Tag color="error" style={{ background: 'transparent' }}>{g.errors} 错</Tag>
            </Tooltip>
          )
        }
        return <Tag color="success" style={{ background: 'transparent' }}>{g.count} 次</Tag>
      },
    },
    {
      key: 'actions',
      title: '操作',
      width: 80,
      render: (_, g) => {
        const r = singleOf(g)
        return r ? (
          <Button size="small" type="text" onClick={() => onHeaders(r)}>
            查看头
          </Button>
        ) : null
      },
    },
  ] as TableColumnsType<SessionGroup>).filter((c) => !isMobile || MOBILE_SESSION_KEYS.has(String(c.key)))
}

/** 骨架行：模拟展开加载时的表格行布局。 */
function DetailSkeleton() {
  return (
    <div style={{ padding: '10px 8px', display: 'flex', flexDirection: 'column', gap: 8 }}>
      {[0, 1, 2].map((i) => (
        <div key={i} style={{ display: 'flex', gap: 12, alignItems: 'center' }}>
          <span className="skeleton-bar short" style={{ animationDelay: `${i * 0.15}s` }} />
          <span className="skeleton-bar medium" style={{ animationDelay: `${i * 0.15 + 0.08}s` }} />
          <span className="skeleton-bar long" style={{ animationDelay: `${i * 0.15 + 0.16}s` }} />
        </div>
      ))}
    </div>
  )
}

/** 会话组展开内容：明细按需拉取最近 LIVE_DETAIL_MAX 条（后端聚合行不含全量行，
 *  展开时按 session_id 查询）；合计仍是全量口径，明细少于总数时给出提示。 */
const SessionDetail = memo(function SessionDetail(props: {
  g: SessionGroup
  isMobile: boolean
  onHeaders: (r: RequestLog) => void
  onLoad: (g: SessionGroup) => void
}) {
  const { g, isMobile, onHeaders, onLoad } = props
  useEffect(() => {
    if (!g.loaded) onLoad(g)
  }, [g, onLoad])
  if (!g.loaded) {
    return <DetailSkeleton />
  }
  const hidden = g.count - g.requests.length
  return (
    <div className="live-detail-loaded">
      <Table<RequestLog>
        size="small"
        rowKey={(r) => (r.req_id && !r.id ? `live-${r.req_id}` : `${r.id}-${r.created_at}`)}
        dataSource={g.requests}
        pagination={false}
        style={{ margin: '2px 0 4px' }}
        expandable={isMobile ? { expandedRowRender: (r) => <LogDetail r={r} pending={isPending(r)} /> } : undefined}
        columns={requestColumns(isMobile, onHeaders)}
      />
      {hidden > 0 && (
        <div style={{ color: 'var(--text-faint)', fontSize: 12, padding: '0 8px 8px' }}>
          仅显示最近 {g.requests.length} 条，另有 {hidden} 条未列出；上方合计为全部 {g.count} 次请求。
        </div>
      )}
    </div>
  )
})

/** 实时请求流表格：memo 化 + 列定义缓存，仅在 groups / 视口 / 回调变化时重渲染，
 *  不再被 2Hz 吞吐、每秒统计刷新等无关状态牵动（200 行 × 11 列的重渲染是此前的性能瓶颈）。 */
const LiveFeedTable = memo(function LiveFeedTable(props: {
  groups: SessionGroup[]
  isMobile: boolean
  expandedKeys: string[]
  onExpandedKeysChange: (keys: string[]) => void
  onHeaders: (r: RequestLog) => void
  onLoad: (g: SessionGroup) => void
}) {
  const { groups, isMobile, expandedKeys, onExpandedKeysChange, onHeaders, onLoad } = props
  const columns = useMemo(() => sessionColumns(isMobile, onHeaders), [isMobile, onHeaders])
  
  // 优化：将 expandedRowRender 提取为单独的回调，避免 expandable 对象每次都重新创建
  const expandedRowRender = useCallback(
    (g: SessionGroup) =>
      g.sessionId ? (
        <SessionDetail g={g} isMobile={isMobile} onHeaders={onHeaders} onLoad={onLoad} />
      ) : isMobile && g.requests[0] ? (
        <LogDetail r={g.requests[0]} pending={isPending(g.requests[0])} />
      ) : null,
    [isMobile, onHeaders, onLoad],
  )
  
  const rowExpandable = useCallback(
    (g: SessionGroup) => !!g.sessionId || (isMobile && !!g.requests[0]),
    [isMobile],
  )
  
  const onExpandedRowsChange = useCallback(
    (keys: readonly Key[]) => onExpandedKeysChange(keys.map(String)),
    [onExpandedKeysChange],
  )
  
  const expandable = useMemo(
    () => ({
      expandedRowKeys: expandedKeys,
      onExpandedRowsChange,
      expandedRowRender,
      rowExpandable,
    }),
    [expandedKeys, onExpandedRowsChange, expandedRowRender, rowExpandable],
  )
  const rowClassName = useCallback((_: SessionGroup, i: number) => (i === 0 ? 'row-live' : ''), [])
  return (
    <Table<SessionGroup>
      rowKey="key"
      dataSource={groups}
      pagination={false}
      size="small"
      scroll={{ x: isMobile ? 1030 : 1590 }}
      rowClassName={rowClassName}
      expandable={expandable}
      columns={columns}
    />
  )
})

/** 仪表盘：当天概览 + 全历史累计 + 热力图 + 模型趋势/用量 + 实时请求流。 */
export default function Dashboard() {
  const isMobile = useIsMobile()
  const pal = useChartPalette()
  const [summary, setSummary] = useState<Summary | null>(null)
  const [trend, setTrend] = useState<TrendPoint[]>([])
  const [byModel, setByModel] = useState<GroupStat[]>([])
  const [live, setLive] = useState<RequestLog[]>([])
  // 后端会话聚合（全量历史口径）与展开时按需拉取的明细（按组键缓存）
  const [liveViews, setLiveViews] = useState<LiveSession[]>([])
  const [liveDetails, setLiveDetails] = useState<Record<string, RequestLog[]>>({})
  // 已展开的会话组键：这些组的明细随实时刷新同步重拉，避免展开后停在旧行
  const [expandedKeys, setExpandedKeys] = useState<string[]>([])

  // 全历史累计（lifetime）与 Token 活动热力图（近一年按日）
  const [lifetime, setLifetime] = useState<Lifetime | null>(null)
  const [heatPoints, setHeatPoints] = useState<TrendPoint[]>([])
  const [heatMode, setHeatMode] = useState<HeatMode>('daily')

  // 每日模型趋势 / 模型用量（近 7 日 / 近 30 日）
  const [rangeDays, setRangeDays] = useState<7 | 30>(7)
  const [modelTrend, setModelTrend] = useState<ModelDayPoint[]>([])

  // 调用方 TOP（近 30 天，按密钥聚合）
  const [byKey, setByKey] = useState<GroupStat[]>([])

  // 花费：今日与本月已花费/期末预测。今日随快速刷新走，本月随慢速刷新走。
  const [costToday, setCostToday] = useState<CostForecast | null>(null)
  const [costMonth, setCostMonth] = useState<CostForecast | null>(null)

  // 趋势图范围（默认实时）；refresh 经 ref 读取当前值，避免重建回调断开 WS 订阅
  const [range, setRange] = useState<TrendRangeKey>('live')
  const rangeRef = useRef(range)
  rangeRef.current = range
  const rangeDaysRef = useRef(rangeDays)
  rangeDaysRef.current = rangeDays
  const lastLiveRefreshRef = useRef(0)
  const lastStatsRefreshRef = useRef(0)
  const lastSlowRefreshRef = useRef(0)

  // 初始加载追踪：首次 API 返回后切换为 false，给子组件传 loading 控制骨架显示
  const [statsLoaded, setStatsLoaded] = useState(false)
  const [slowLoaded, setSlowLoaded] = useState(false)

  // 快速刷新：当天汇总 + 请求趋势 + 当天模型 TOP
  const statsLoadedOnce = useRef(false)
  const refresh = useCallback(async () => {
    const r = TREND_RANGES.find((x) => x.key === rangeRef.current) ?? TREND_RANGES[2]
    const [s, t, m, ct] = await Promise.all([
      statsApi.summary(24, todaySince()),
      statsApi.trend(r.hours, r.bucket),
      statsApi.byModel(24, todaySince()),
      costApi.forecast('today'),
    ])
    setSummary(s)
    setTrend(t)
    setByModel(m)
    setCostToday(ct)
    if (!statsLoadedOnce.current) {
      statsLoadedOnce.current = true
      setStatsLoaded(true)
    }
  }, [])

  // 仅刷新趋势：实时档借吞吐推送（恒定 2Hz）推进空桶，无需连带重拉汇总/模型 TOP。
  const refreshTrend = useCallback(async () => {
    const r = TREND_RANGES.find((x) => x.key === rangeRef.current) ?? TREND_RANGES[2]
    setTrend(await statsApi.trend(r.hours, r.bucket))
  }, [])

  // 模型趋势/用量单独刷新：切换「近 7 日 / 近 30 日」只影响这一组数据，
  // 不必连带重拉 lifetime / 热力图 / 调用方 TOP 三个无关接口。
  const refreshModelTrend = useCallback(async () => {
    const days = rangeDaysRef.current
    setModelTrend(await lifetimeApi.trendByModel(days))
  }, [])

  // 慢速刷新：全历史累计 + 热力图（26 周）+ 模型趋势/用量 + 调用方 TOP（按日聚合，无需高频）
  const slowLoadedOnce = useRef(false)
  const refreshSlow = useCallback(async () => {
    const days = rangeDaysRef.current
    const [lt, hp, mt, bk, cm] = await Promise.all([
      lifetimeApi.get(),
      statsApi.trendByDay(190),
      lifetimeApi.trendByModel(days),
      statsApi.byKey(720),
      costApi.forecast('month'),
    ])
    setLifetime(lt)
    setHeatPoints(hp)
    setModelTrend(mt)
    setByKey(bk)
    setCostMonth(cm)
    if (!slowLoadedOnce.current) {
      slowLoadedOnce.current = true
      setSlowLoaded(true)
    }
  }, [])

  // 展开会话的明细：按组键拉最近 LIVE_DETAIL_MAX 条（后端聚合行不含全量行）。
  // 展开期间会随实时刷新重拉，避免面板停在展开那一刻的旧行。
  const expandedKeysRef = useRef<string[]>([])
  const setExpanded = useCallback((keys: string[]) => {
    expandedKeysRef.current = keys
    setExpandedKeys(keys)
  }, [])
  const loadingDetailRef = useRef<Set<string>>(new Set())
  const loadDetailForKey = useCallback(async (key: string) => {
    const sid = key.startsWith('sess:') ? key.slice('sess:'.length) : ''
    if (!sid || loadingDetailRef.current.has(key)) return
    loadingDetailRef.current.add(key)
    try {
      const page = await logApi.list({ page: 1, per_page: LIVE_DETAIL_MAX, session_id: sid })
      setLiveDetails((prev) => ({ ...prev, [key]: page.items ?? [] }))
    } finally {
      loadingDetailRef.current.delete(key)
    }
  }, [])

  // 实时会话刷新：后端按 session_id 全量聚合（合计准确），只取最近 LIVE_GROUP_MAX 组。
  // 节流窗口内被跳过时安排一次尾随刷新，保证窗口末端的完成行最终一定被后端计入。
  const lastLiveSessionsRef = useRef(0)
  const liveTrailTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const liveRef = useRef(live)
  liveRef.current = live
  const refreshLive = useCallback(async () => {
    const now = Date.now()
    const wait = LIVE_SESSIONS_THROTTLE_MS - (now - lastLiveSessionsRef.current)
    if (wait > 0) {
      if (!liveTrailTimerRef.current) {
        liveTrailTimerRef.current = setTimeout(() => {
          liveTrailTimerRef.current = null
          void refreshLive()
        }, wait)
      }
      return
    }
    lastLiveSessionsRef.current = now
    const views = await statsApi.liveSessions(LIVE_GROUP_MAX)
    setLiveViews(views ?? [])
    // 已展开的会话同步重拉明细，使展开面板与合计一起滚动更新。
    // 掉出最近 LIVE_GROUP_MAX 组的会话（行已不展示）停止重拉并清缓存，避免无谓请求堆积。
    const visible = new Set(views.map((v) => v.key))
    for (const r of liveRef.current) {
      if (r.id) continue
      const sid = (r.session_id ?? '').trim()
      if (sid) visible.add(`sess:${sid}`)
    }
    const keys = expandedKeysRef.current
    const stale = keys.filter((k) => !visible.has(k))
    const active = keys.filter((k) => visible.has(k))
    if (stale.length > 0) {
      setExpanded(active)
      setLiveDetails((prev) => {
        const next = { ...prev }
        for (const k of stale) delete next[k]
        return next
      })
    }
    if (active.length > 0) await Promise.all(active.map((k) => loadDetailForKey(k)))
  }, [loadDetailForKey, setExpanded])
  useEffect(
    () => () => {
      if (liveTrailTimerRef.current) clearTimeout(liveTrailTimerRef.current)
    },
    [],
  )

  useEffect(() => {
    void refresh()
    void refreshSlow()
    void refreshLive()
  }, [refresh, refreshSlow, refreshLive])

  // 实时：请求受理即滑入"运行中"行；完成事件按 req_id 原位替换为最终结果。
  // 幂等保护（completed 先于 started / 重复推送）与容量裁剪见 mergeLiveEvents。
  // 突发流量下事件按 LIVE_FLUSH_MS 合流，把「每条事件一次重渲染」降为每窗口一次。
  const pendingEventsRef = useRef<{ row: RequestLog; phase: LivePhase }[]>([])
  const flushTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const flushLive = useCallback(() => {
    if (flushTimerRef.current) {
      clearTimeout(flushTimerRef.current)
      flushTimerRef.current = null
    }
    const events = pendingEventsRef.current
    if (events.length === 0) return
    pendingEventsRef.current = []
    setLive((prev) => mergeLiveEvents(prev, events, LIVE_FEED_MAX))
    // 受理/完成都可能改变会话合计，节流刷新一次后端会话视图
    void refreshLive()
  }, [refreshLive])
  const pushLiveEvent = useCallback(
    (row: RequestLog, phase: LivePhase) => {
      pendingEventsRef.current.push({ row, phase })
      if (!flushTimerRef.current) {
        flushTimerRef.current = setTimeout(() => {
          flushTimerRef.current = null
          flushLive()
        }, LIVE_FLUSH_MS)
      }
    },
    [flushLive],
  )
  useWsEvent(WS_EVENTS.requestStarted, (msg) => pushLiveEvent(msg.payload as RequestLog, 'started'))
  useWsEvent(WS_EVENTS.requestCompleted, (msg) => pushLiveEvent(msg.payload as RequestLog, 'completed'))
  // 卸载时丢弃待合流事件（不 setState），避免定时器在组件销毁后仍触发更新
  useEffect(
    () => () => {
      if (flushTimerRef.current) clearTimeout(flushTimerRef.current)
      pendingEventsRef.current = []
    },
    [],
  )
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
  // 实时档：借吞吐推送节流刷新趋势（约 4s 一次），无流量时也能看到空桶推进。
  // 吞吐数值本身由 ThroughputTicker 独立订阅，避免 2Hz 状态更新牵动整页。
  useWsEvent(WS_EVENTS.throughput, () => {
    if (rangeRef.current === 'live') {
      const now = Date.now()
      if (now - lastLiveRefreshRef.current > 4000) {
        lastLiveRefreshRef.current = now
        void refreshTrend()
      }
    }
  })

  // 断线重连后全量补拉：断连窗口内的事件已丢失，节流窗口一并重置
  useWsReconnected(() => {
    lastStatsRefreshRef.current = 0
    lastSlowRefreshRef.current = 0
    lastLiveRefreshRef.current = 0
    lastLiveSessionsRef.current = 0
    pendingEventsRef.current = []
    setLive([])
    setLiveDetails({})
    void refresh()
    void refreshSlow()
    void refreshLive()
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

  // 会话展示：后端全量聚合 + 未落库实时行叠加（合计口径由后端保证，不因窗口截断而残缺）
  const liveGroups = useMemo(
    () => mergeSessionViews(liveViews, live, liveDetails, LIVE_GROUP_MAX),
    [liveViews, live, liveDetails],
  )

  // 展开会话时按需拉取该会话的最近 LIVE_DETAIL_MAX 条明细（合计由后端全量聚合保证）
  const loadSessionDetail = useCallback(
    async (g: SessionGroup) => {
      if (!g.sessionId) return
      await loadDetailForKey(g.key)
    },
    [loadDetailForKey],
  )

  // 展开集合变化时记录到 ref，供实时刷新重拉明细；收起时丢弃缓存避免内存堆积
  const handleExpandedKeysChange = useCallback(
    (keys: string[]) => {
      setExpanded(keys)
      setLiveDetails((prev) => {
        // 优化：只在实际有 key 被移除时才创建新对象
        const prevKeys = Object.keys(prev)
        const keysSet = new Set(keys)
        let hasRemoved = false
        for (const k of prevKeys) {
          if (!keysSet.has(k)) {
            hasRemoved = true
            break
          }
        }
        // 无变化时返回原引用，避免展开/收起触发多余重渲染
        if (!hasRemoved) return prev
        const next: Record<string, RequestLog[]> = {}
        for (const k of prevKeys) {
          if (keysSet.has(k)) next[k] = prev[k]
        }
        return next
      })
    },
    [setExpanded],
  )

  const cards = summary ? toStatCards(summary) : null
  // rangeConf 必须是稳定引用：否则 series 的 useMemo 每次都失效，趋势图会在任意无关重渲染时重算几何
  const rangeConf = useMemo(() => TREND_RANGES.find((x) => x.key === range) ?? TREND_RANGES[2], [range])
  const series = useMemo(
    () =>
      rangeConf.bucket === 'day'
        ? toTrendSeries(trend, rangeConf.days ?? 7)
        : toBucketSeries(trend, rangeConf.bucket === 'minute' ? 60 : 3600, rangeConf.count ?? 24),
    [trend, rangeConf],
  )
  const ranked = useMemo(() => toModelRank(byModel, 6), [byModel])
  const keyRanked = useMemo(() => toModelRank(byKey, 6), [byKey])
  const { format: money } = useMoneyFormat()

  // 预测卡片副标题：能预测时给出基准与置信度，样本不足时说明原因（不让用户看到无解释的空值）
  const todayCostSub = costToday
    ? costToday.projected_usd != null
      ? `预计今日 ${money(costToday.projected_usd)}`
      : costToday.reason || '样本不足'
    : undefined
  const monthCostSub = costMonth
    ? costMonth.projected_usd != null
      ? `${basisLabel(costMonth.basis)} · 置信度${confidenceLabel(costMonth.confidence)}`
      : costMonth.reason || '样本不足'
    : undefined
  const budgetSub =
    costMonth && costMonth.budget_usd > 0
      ? `月度预算 ${money(costMonth.budget_usd)}` +
        (costMonth.projected_exceeded_date ? ` · 预计 ${costMonth.projected_exceeded_date} 触及` : '')
      : '未设月度预算 · 可在设置页配置'

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
        <Col xs={24} md={8}>
          <StatCard label="今日花费" value={costToday ? costToday.spent_usd : null} format={money} sub={todayCostSub} tick />
        </Col>
        <Col xs={24} md={8}>
          <StatCard label="本月累计花费" value={costMonth ? costMonth.spent_usd : null} format={money} sub={budgetSub} />
        </Col>
        <Col xs={24} md={8}>
          <StatCard label="本月预计花费" value={costMonth ? costMonth.projected_usd : null} format={money} sub={monthCostSub} />
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
                  <ThroughputTicker />
                  <Segmented
                    size="small"
                    value={range}
                    onChange={(v) => {
                      const k = v as TrendRangeKey
                      rangeRef.current = k // 立即生效，refreshTrend 不依赖重渲染时序
                      setRange(k)
                      void refreshTrend()
                    }}
                    options={TREND_RANGES.map((r) => ({ value: r.key, label: r.label }))}
                  />
                </div>
              </div>
            }
          >
            <TrendChart data={series} height={isMobile ? 190 : 232} ariaLabel={`${rangeConf.label}请求趋势`} loading={!statsLoaded} />
          </Card>
        </Col>
        <Col xs={24} lg={10}>
          <Card
            className="panel"
            style={{ height: '100%' }}
            styles={{ body: { padding: '14px 16px 16px' }, header: { borderBottom: '1px solid var(--border-faint)' } }}
            title={<span style={panelHeaderStyle}>模型 TOP · 今天</span>}
          >
<UsageBars rows={ranked} loading={!statsLoaded} />
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
                    void refreshModelTrend()
                  }}
                  options={[
                    { value: 7, label: '近 7 日' },
                    { value: 30, label: '近 30 日' },
                  ]}
                />
              </div>
            }
          >
            <ModelTrendChart data={mt} height={isMobile ? 200 : 244} loading={!slowLoaded} />
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
            <ModelDonut slices={donut.slices} total={donut.total} size={isMobile ? 150 : 170} loading={!slowLoaded} />
          </Card>
        </Col>
      </Row>

      <Row gutter={[16, 16]} align="stretch">
        <Col xs={24} lg={14}>
          <Card
            className="panel"
            // 卡片自身作 flex 列容器，body 才能撑满被同行卡片拉高后的剩余高度；
            // 否则 body 按内容高度停在 120px，热力图永远长不大，卡片下半截留白。
            style={{ height: '100%', display: 'flex', flexDirection: 'column' }}
            styles={{
              body: { padding: '14px 16px 12px', display: 'flex', flexDirection: 'column', flex: 1, minHeight: 0 },
              header: { borderBottom: '1px solid var(--border-faint)', flexShrink: 0 },
            }}
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
            <TokenHeatmap data={heat} mode={heatMode} loading={!slowLoaded} />
            <div style={{ display: 'flex', flexShrink: 0, justifyContent: 'flex-end', alignItems: 'center', gap: 4, marginTop: 8, fontSize: 11, color: 'var(--text-faint)' }}>
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
            <UsageBars rows={keyRanked} loading={!slowLoaded} />
          </Card>
        </Col>
      </Row>

      <Card
        className="panel"
        // 表格背景为不透明直角，padding 0 时会盖住面板底部圆角边框；裁剪 body 底角（10px 圆角 - 1px 边框）对齐
        styles={{
          body: { padding: 0, overflow: 'hidden', borderBottomLeftRadius: 9, borderBottomRightRadius: 9 },
          header: { borderBottom: '1px solid var(--border-faint)' },
        }}
        title={
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
            <span className="live-dot" />
            <span style={panelHeaderStyle}>实时请求流</span>
          </span>
        }
      >
        <LiveFeedTable
          groups={liveGroups}
          isMobile={isMobile}
          expandedKeys={expandedKeys}
          onExpandedKeysChange={handleExpandedKeysChange}
          onHeaders={showHeaders}
          onLoad={loadSessionDetail}
        />
      </Card>

      <Drawer
        title="请求头"
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        size={isMobile ? '100%' : 520}
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

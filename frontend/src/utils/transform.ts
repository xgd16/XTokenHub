/** 统计数据 -> 图表配置的纯转换函数（便于 Vitest 覆盖）。 */
import type { RequestLog } from '../api/log'
import type { GroupStat, ModelDayPoint, Summary, TrendPoint } from '../api/stats'

export interface SeriesPoint {
  date: string
  requests: number
  errors: number
  tokens: number
}

/** 本地日期键：YYYY-MM-DD（与后端 DATE(created_at) 对齐）。 */
function localDateKey(d: Date): string {
  const p = (x: number) => String(x).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

/**
 * 趋势 -> 双序列点。后端只返回有数据的日期（GROUP BY DATE），
 * 这里按最近 days 天补齐空缺为 0，避免折线图只剩孤立点；
 * now 参数供测试固定时间，缺省取当前时间。
 */
export function toTrendSeries(
  points: TrendPoint[] | null | undefined,
  days = 7,
  now: Date = new Date(),
): SeriesPoint[] {
  const byDate = new Map<string, TrendPoint>()
  for (const p of points ?? []) byDate.set(p.date, p)
  const start = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  start.setDate(start.getDate() - (days - 1))
  const out: SeriesPoint[] = []
  for (let i = 0; i < days; i++) {
    const d = new Date(start)
    d.setDate(d.getDate() + i)
    const key = localDateKey(d)
    const p = byDate.get(key)
    out.push({
      date: key,
      requests: p?.requests ?? 0,
      errors: p?.error_requests ?? 0,
      tokens: p?.total_tokens ?? 0,
    })
  }
  return out
}

/**
 * 分桶趋势 -> 序列点。后端返回有数据的桶（epoch 对齐 Unix 秒），
 * 这里按最近 count 个桶补齐空缺为 0；x 标签按本地时间生成：
 * 桶宽 < 1h 用 HH:mm，>= 1h 用 MM-DD HH:00。now 供测试固定时间。
 */
export function toBucketSeries(
  points: TrendPoint[] | null | undefined,
  bucketSec: number,
  count: number,
  now: Date = new Date(),
): SeriesPoint[] {
  const byTs = new Map<number, TrendPoint>()
  for (const p of points ?? []) if (p.ts) byTs.set(p.ts, p)
  const end = Math.floor(now.getTime() / 1000 / bucketSec) * bucketSec
  const pad = (x: number) => String(x).padStart(2, '0')
  const out: SeriesPoint[] = []
  for (let i = 0; i < count; i++) {
    const ts = end - (count - 1 - i) * bucketSec
    const p = byTs.get(ts)
    const d = new Date(ts * 1000)
    out.push({
      date:
        bucketSec < 3600
          ? `${pad(d.getHours())}:${pad(d.getMinutes())}`
          : `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:00`,
      requests: p?.requests ?? 0,
      errors: p?.error_requests ?? 0,
      tokens: p?.total_tokens ?? 0,
    })
  }
  return out
}

export interface ModelRank {
  name: string
  requests: number
  tokens: number
  cachePercent: number
}

/** 按模型 Top N（空/缺失数据安全）。 */
export function toModelRank(rows: GroupStat[] | null | undefined, top = 8): ModelRank[] {
  return (rows ?? []).slice(0, top).map((g) => ({
    name: g.name || 'unknown',
    requests: g.requests,
    tokens: g.total_tokens,
    cachePercent: Math.round((g.cache_rate || 0) * 1000) / 10,
  }))
}

/** 卡片摘要计算。 */
export interface StatCardData {
  requests: number
  errors: number
  tokens: string
  hitPercent: string
  avgMs: number
  nativePercent: string
}

export function toStatCards(s: Summary): StatCardData {
  return {
    requests: s.total_requests,
    errors: s.error_requests,
    tokens: `${s.total_tokens}`,
    hitPercent: ((s.cache_hit_rate || 0) * 100).toFixed(1) + '%',
    avgMs: Math.round(s.avg_duration_ms || 0),
    nativePercent: ((s.native_ratio || 0) * 100).toFixed(1) + '%',
  }
}

// ==================== 仪表盘扩展：热力图 / 多模型趋势 / 环图 ====================

/** 多模型系列调色板（趋势线与环图共用，保证同模型同色）。 */
export const MODEL_COLORS = ['#3b82f6', '#22a06b', '#a855f7', '#ef4444', '#f59e0b', '#14b8a6']

/** 模型趋势数据：对齐日期轴 + 每模型值序列（长为 dates.length，缺日为 0）。 */
export interface ModelTrendData {
  dates: string[]
  series: { name: string; values: number[] }[]
}

/**
 * 按日×模型 token -> 多模型趋势序列。
 * 取近 days 天（补零），按总 token 取前 top 个模型，其余合并为「其他」。
 */
export function toModelTrend(
  points: ModelDayPoint[] | null | undefined,
  days = 7,
  top = 6,
  now: Date = new Date(),
): ModelTrendData {
  const dates: string[] = []
  const start = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  start.setDate(start.getDate() - (days - 1))
  for (let i = 0; i < days; i++) {
    const d = new Date(start)
    d.setDate(d.getDate() + i)
    dates.push(localDateKey(d))
  }
  // 聚合 (date, model) -> tokens
  const perModel = new Map<string, Map<string, number>>()
  let otherTotal = 0
  const totals = new Map<string, number>()
  for (const p of points ?? []) {
    const m = perModel.get(p.model) ?? new Map<string, number>()
    m.set(p.date, (m.get(p.date) ?? 0) + p.total_tokens)
    perModel.set(p.model, m)
    totals.set(p.model, (totals.get(p.model) ?? 0) + p.total_tokens)
  }
  const ranked = [...totals.entries()].sort((a, b) => b[1] - a[1])
  const keep = new Set(ranked.slice(0, top).map(([name]) => name))
  for (const [name, t] of ranked) if (!keep.has(name)) otherTotal += t
  const series = ranked.slice(0, top).map(([name]) => ({
    name,
    values: dates.map((d) => perModel.get(name)?.get(d) ?? 0),
  }))
  if (otherTotal > 0) {
    series.push({
      name: '其他',
      values: dates.map((d) => {
        let sum = 0
        for (const [name, m] of perModel) if (!keep.has(name)) sum += m.get(d) ?? 0
        return sum
      }),
    })
  }
  return { dates, series }
}

export interface HeatCell {
  date: string
  value: number
  level: number // 0~4，0 为无数据
}

/** 热力图数据：列为周（GitHub 风格），每列自上而下为周日~周六；weekly 模式仅一行。 */
export interface HeatmapData {
  columns: { cells: HeatCell[] }[]
  monthLabels: { index: number; label: string }[] // index 为列号
  max: number
}

export type HeatMode = 'daily' | 'weekly' | 'cumulative'

/**
 * 按日 token -> GitHub 风格日历热力图（近 weeks 周，默认 52，含今天所在周）。
 * daily: 当日值；weekly: 每周合计（每周一格）；cumulative: 累计值（颜色随累计增长）。
 */
export function toHeatmap(
  points: { date: string; total_tokens: number }[] | null | undefined,
  mode: HeatMode = 'daily',
  now: Date = new Date(),
  weeks = 52,
): HeatmapData {
  const byDate = new Map<string, number>()
  for (const p of points ?? []) byDate.set(p.date, (byDate.get(p.date) ?? 0) + p.total_tokens)

  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const start = new Date(today)
  start.setDate(start.getDate() - (weeks * 7 - 1))
  start.setDate(start.getDate() - start.getDay()) // 回到所在周的周日

  const cells: HeatCell[] = []
  for (let d = new Date(start); d <= today; d.setDate(d.getDate() + 1)) {
    const key = localDateKey(d)
    cells.push({ date: key, value: byDate.get(key) ?? 0, level: 0 })
  }

  // 累计模式：原位转为运行累计
  if (mode === 'cumulative') {
    let acc = 0
    for (const c of cells) {
      acc += c.value
      c.value = acc
    }
  }

  const max = cells.reduce((m, c) => Math.max(m, c.value), 0)
  const levelOf = (v: number) => (v <= 0 || max <= 0 ? 0 : Math.min(4, 1 + Math.floor((v / max) * 4)))
  for (const c of cells) c.level = levelOf(c.value)

  // 按周分列（第一列可能不满一周；weekly 模式每列聚合成单格单行）
  const columns: { cells: HeatCell[] }[] = []
  const weekTotals: number[] = []
  for (let i = 0; i < cells.length; i += 7) {
    const week = cells.slice(i, i + 7)
    columns.push({ cells: week })
    weekTotals.push(week.reduce((s, c) => s + c.value, 0))
  }

  // 月份标签：当列首日跨入新月份时标注
  const monthLabels: { index: number; label: string }[] = []
  let prevMonth = -1
  columns.forEach((col, i) => {
    const first = col.cells[0]
    if (!first) return
    const m = Number(first.date.slice(5, 7))
    if (m !== prevMonth) {
      monthLabels.push({ index: i, label: `${m}月` })
      prevMonth = m
    }
  })

  if (mode === 'weekly') {
    const wmax = Math.max(...weekTotals, 0)
    const wl = (v: number) => (v <= 0 || wmax <= 0 ? 0 : Math.min(4, 1 + Math.floor((v / wmax) * 4)))
    // 每周一格，横向排开（与日历同为列 = 周）；跨月列标注月份
    const weekCells = weekTotals.map((v, i) => ({
      date: columns[i].cells[0]?.date ?? '',
      value: v,
      level: wl(v),
    }))
    const monthLabelsW: { index: number; label: string }[] = []
    let prevMonthW = -1
    weekCells.forEach((c, i) => {
      if (!c.date) return
      const m = Number(c.date.slice(5, 7))
      if (m !== prevMonthW) {
        monthLabelsW.push({ index: i, label: `${m}月` })
        prevMonthW = m
      }
    })
    return {
      columns: weekCells.map((c) => ({ cells: [c] })),
      monthLabels: monthLabelsW,
      max: wmax,
    }
  }
  return { columns, monthLabels, max }
}

/** 环图切片。 */
export interface DonutSlice {
  name: string
  value: number
  percent: number // 0~100，一位小数
  color: string
}

/**
 * 多模型趋势序列 -> 环图切片：按总 token 取前 top 个模型，其余合并为「其他」。
 * 颜色沿用 MODEL_COLORS，「其他」固定灰色。
 */
export function toDonut(series: { name: string; values: number[] }[], top = 5): { slices: DonutSlice[]; total: number } {
  const totals = series.map((s) => ({ name: s.name, value: s.values.reduce((a, b) => a + b, 0) }))
  const total = totals.reduce((a, b) => a + b.value, 0)
  const sorted = totals.sort((a, b) => b.value - a.value)
  const keep = sorted.slice(0, top)
  const restValue = sorted.slice(top).reduce((a, b) => a + b.value, 0)
  const slices: DonutSlice[] = keep.map((k, i) => ({
    name: k.name,
    value: k.value,
    percent: total > 0 ? Math.round((k.value / total) * 1000) / 10 : 0,
    color: MODEL_COLORS[i % MODEL_COLORS.length],
  }))
  if (restValue > 0) {
    slices.push({
      name: '其他',
      value: restValue,
      percent: total > 0 ? Math.round((restValue / total) * 1000) / 10 : 0,
      color: '#5a6675',
    })
  }
  return { slices, total }
}

// ==================== 实时请求流：按会话聚合 ====================

/** 会话组：同一调用方会话（X-Session-Id）的请求聚合结果。 */
export interface SessionGroup {
  key: string
  sessionId: string // '' = 无会话标识的散行（单次请求）
  requests: RequestLog[] // 新→旧，与输入一致
  firstAt: string // 组内最早请求时间
  lastAt: string // 组内最新请求时间
  totalTokens: number
  completionTokens: number
  promptTokens: number
  cachedTokens: number
  totalMs: number // 各请求耗时合计
  running: number // 进行中请求数（实时推送、尚未落库的行）
  errors: number
  models: string[] // 去重，保持出现顺序
  channels: string[]
  protocols: string[]
  userAgent: string // 首个非空 UA
  keyName: string
}

/**
 * 请求日志 -> 会话组。带 session_id 的行按会话合并（组序 = 组内最新请求的次序，
 * 与输入新→旧一致）；无标识的行各自成组，展示等同单次请求。
 */
export function groupBySession(rows: RequestLog[]): SessionGroup[] {
  const groups = new Map<string, SessionGroup>()
  for (const r of rows) {
    const sid = (r.session_id ?? '').trim()
    const key = sid ? `sess:${sid}` : `req:${r.id || r.req_id || r.created_at}`
    let g = groups.get(key)
    if (!g) {
      g = {
        key,
        sessionId: sid,
        requests: [],
        firstAt: r.created_at,
        lastAt: r.created_at,
        totalTokens: 0,
        completionTokens: 0,
        promptTokens: 0,
        cachedTokens: 0,
        totalMs: 0,
        running: 0,
        errors: 0,
        models: [],
        channels: [],
        protocols: [],
        userAgent: '',
        keyName: '',
      }
      groups.set(key, g)
    } else if (Date.parse(r.created_at) < Date.parse(g.firstAt)) {
      g.firstAt = r.created_at
    }
    g.requests.push(r)
    g.totalTokens += r.total_tokens
    g.completionTokens += r.completion_tokens
    g.promptTokens += r.prompt_tokens
    g.cachedTokens += r.cached_tokens
    g.totalMs += r.duration_ms
    if (!r.id && r.req_id) g.running += 1
    if (r.error) g.errors += 1
    if (r.model && !g.models.includes(r.model)) g.models.push(r.model)
    if (r.channel_name && !g.channels.includes(r.channel_name)) g.channels.push(r.channel_name)
    if (r.protocol && !g.protocols.includes(r.protocol)) g.protocols.push(r.protocol)
    if (!g.userAgent && r.user_agent) g.userAgent = r.user_agent
    if (!g.keyName && r.key_name) g.keyName = r.key_name
  }
  return [...groups.values()]
}

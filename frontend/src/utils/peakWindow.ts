/**
 * 峰谷时段规则的浏览器端解析与判定，口径与后端 internal/model/peak_window.go 一致：
 * 规则 `<星期>;<时段>`，如 DeepSeek 工作日高峰 `1-5;09:00-12:00,14:00-18:00`。
 * 星期 1=周一..7=周日，支持 `1-5` 区间与 `1,6,7` 列表；时段为 [起, 止) 分钟，
 * 结束须晚于开始（空闲时段以「列出高峰窗口」表达，不支持跨零点）。
 */

export type PricePeriod = 'peak' | 'off_peak'

interface ParsedPeakWindow {
  days: boolean[] // 下标 1..7 有效
  spans: Array<[number, number]> // [起始分钟, 结束分钟)
}

/** 北京时间固定 UTC+8（中国无夏令时），与后端 BeijingZone 同口径。 */
const BEIJING_OFFSET_MS = 8 * 60 * 60 * 1000

const CLOCK_RE = /^(\d{1,2}):(\d{1,2})$/

function parseClock(s: string): number | null {
  const m = CLOCK_RE.exec(s.trim())
  if (!m) return null
  const h = Number(m[1])
  const min = Number(m[2])
  if (h > 23 || min > 59) return null
  return h * 60 + min
}

/** 解析规则字符串；空串或非法规则返回 null（展示层视为未配置）。 */
export function parsePeakWindow(rule: string): ParsedPeakWindow | null {
  const parts = rule.trim().split(';')
  if (parts.length !== 2) return null
  const dayPart = parts[0].trim()
  if (!dayPart) return null
  const days = new Array<boolean>(8).fill(false)
  for (const item of dayPart.split(',')) {
    const seg = item.trim()
    if (!seg) return null
    const dash = seg.indexOf('-')
    const lo = Number(dash >= 0 ? seg.slice(0, dash) : seg)
    const hi = Number(dash >= 0 ? seg.slice(dash + 1) : seg)
    if (!Number.isInteger(lo) || !Number.isInteger(hi) || lo < 1 || hi > 7 || lo > hi) return null
    for (let d = lo; d <= hi; d++) days[d] = true
  }
  const spanPart = parts[1].trim()
  if (!spanPart) return null
  const spans: Array<[number, number]> = []
  for (const item of spanPart.split(',')) {
    const seg = item.trim()
    if (!seg) return null
    const dash = seg.indexOf('-')
    if (dash < 0) return null
    const start = parseClock(seg.slice(0, dash))
    const end = parseClock(seg.slice(dash + 1))
    if (start === null || end === null || end <= start) return null
    spans.push([start, end])
  }
  return { days, spans }
}

/**
 * 返回 at 时刻（任意时区的同一瞬间）命中的计价时段；规则为空或非法时返回 null。
 * 判定统一换算到北京时间：给时间戳加固定 8h 后读 UTC 字段即北京墙上时刻。
 */
export function peakPeriodAt(rule: string, at: Date): PricePeriod | null {
  const parsed = parsePeakWindow(rule)
  return parsed ? periodOf(parsed, at) : null
}

function periodOf(parsed: ParsedPeakWindow, at: Date): PricePeriod {
  const bj = new Date(at.getTime() + BEIJING_OFFSET_MS)
  const day = bj.getUTCDay() === 0 ? 7 : bj.getUTCDay()
  if (!parsed.days[day]) return 'off_peak'
  const minute = bj.getUTCHours() * 60 + bj.getUTCMinutes()
  for (const [start, end] of parsed.spans) {
    if (minute >= start && minute < end) return 'peak'
  }
  return 'off_peak'
}

/** 下一次时段切换：切换时刻、切换到的时段、距现在毫秒数。 */
export interface PeakSwitch {
  at: Date
  to: PricePeriod
  ms: number
}

/** 当前时段区间（start <= at < end）。 */
export interface PeakBounds {
  start: Date
  end: Date
}

const DAY_MS = 86_400_000

/**
 * 枚举 [fromMs, toMs] 内真正发生峰/谷切换的瞬间（升序）。
 *
 * 候选点取每段高峰窗口的起止分钟与每日零点（覆盖 00:00 起的高峰窗口），
 * 再按「前后一分钟的时段是否不同」过滤掉首尾相接的无效边界。
 */
function switchPoints(parsed: ParsedPeakWindow, fromMs: number, toMs: number): number[] {
  const points: number[] = []
  const firstDayMs = Math.floor((fromMs + BEIJING_OFFSET_MS) / DAY_MS) * DAY_MS - DAY_MS
  for (let dayMs = firstDayMs; dayMs <= toMs + BEIJING_OFFSET_MS + DAY_MS; dayMs += DAY_MS) {
    const weekday = new Date(dayMs).getUTCDay()
    const day = weekday === 0 ? 7 : weekday
    const candidates = [dayMs]
    if (parsed.days[day]) {
      for (const [start, end] of parsed.spans) candidates.push(dayMs + start * 60_000, dayMs + end * 60_000)
    }
    for (const candidate of candidates) {
      const ms = candidate - BEIJING_OFFSET_MS
      if (ms < fromMs || ms > toMs) continue
      if (periodOf(parsed, new Date(ms - 60_000)) !== periodOf(parsed, new Date(ms))) points.push(ms)
    }
  }
  return points.sort((a, b) => a - b)
}

/**
 * 推算 from 之后最近一次峰/谷切换；规则为空或非法返回 null。
 *
 * 回溯/前推窗口取 8 天：规则以周为周期，单个高峰窗口之外的连续空闲段最长不超过一周，
 * 8 天足以覆盖任一种合法规则的完整切换序列。
 */
export function nextPeakSwitch(rule: string, from: Date): PeakSwitch | null {
  const parsed = parsePeakWindow(rule)
  if (!parsed) return null
  const fromMs = from.getTime()
  const next = switchPoints(parsed, fromMs, fromMs + 8 * DAY_MS).find((ms) => ms > fromMs)
  if (next === undefined) return null
  return { at: new Date(next), to: periodOf(parsed, new Date(next)), ms: next - fromMs }
}

/**
 * 当前时段区间的起止（画「距下次切换」进度条用）；规则为空或非法、
 * 或区间端点超出 8 天回溯窗口时返回 null。
 */
export function peakPeriodBounds(rule: string, at: Date): PeakBounds | null {
  const parsed = parsePeakWindow(rule)
  if (!parsed) return null
  const atMs = at.getTime()
  const points = switchPoints(parsed, atMs - 8 * DAY_MS, atMs + 8 * DAY_MS)
  const end = points.find((ms) => ms > atMs)
  let start: number | undefined
  for (const ms of points) {
    if (ms <= atMs) start = ms
  }
  if (end === undefined || start === undefined) return null
  return { start: new Date(start), end: new Date(end) }
}

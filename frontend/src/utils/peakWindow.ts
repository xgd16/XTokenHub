/**
 * 峰谷时段规则的浏览器端解析与判定，口径与后端 internal/model/peak_window.go 一致：
 * 规则 `<星期>;<时段>`，如 DeepSeek 工作日高峰 `1-5,!H;09:00-12:00,14:00-18:00`。
 * 星期 1=周一..7=周日，支持 `1-5` 区间与 `1,6,7` 列表；另可附加节假日记号（大小写不敏感）：
 * `!H` 放假日（含调休连休）全天不计高峰、`W` 调休补班日视为命中星期集合。
 * 时段为 [起, 止) 分钟，结束须晚于开始（空闲时段以「列出高峰窗口」表达，不支持跨零点）。
 *
 * 节假日判定查模块级年历（setHolidayCalendar 注入，数据来自 /api/v1/settings/holidays），
 * 与后端 HolidayKindOf 同口径；年历无数据时记号退化为按星期判定，不误判。
 */

export type PricePeriod = 'peak' | 'off_peak'

/** 节假日标注（后端 model.HolidayKind）：holiday=放假（含调休连休）| workday=调休补班上班。 */
export type HolidayKind = 'holiday' | 'workday'

/** 年历条目：date 为北京日期 YYYY-MM-DD；判定只用这两个字段，其余随 API 原样携带。 */
export interface HolidayCalendarItem {
  date: string
  kind: HolidayKind
}

interface ParsedPeakWindow {
  days: boolean[] // 下标 1..7 有效
  spans: Array<[number, number]> // [起始分钟, 结束分钟)
  skipHoliday: boolean // `!H`：放假日全天不计高峰
  makeupWorkday: boolean // `W`：调休补班日视为命中星期集合
}

/** 北京时间固定 UTC+8（中国无夏令时），与后端 BeijingZone 同口径。 */
const BEIJING_OFFSET_MS = 8 * 60 * 60 * 1000

const DAY_MS = 86_400_000

const CLOCK_RE = /^(\d{1,2}):(\d{1,2})$/

/** 星期/记号数值项：与后端 strconv.Atoi 同口径（十进制、可带符号），拒绝 0x、小数等写法。 */
const INTEGER_RE = /^[+-]?\d+$/

// ---------- 节假日年历（模块级注入） ----------

/** 北京日期 YYYY-MM-DD -> 标注；判定热路径同步查询，由 setHolidayCalendar 整体注入。 */
let holidayKinds: Map<string, HolidayKind> = new Map()

/**
 * 注入节假日年历（整体替换，与后端「按年整体替换」同口径）。
 * 来源为 GET /api/v1/settings/holidays 的 items；同一日期重复时以最后一个为准。
 */
export function setHolidayCalendar(items: Iterable<HolidayCalendarItem>): void {
  const next = new Map<string, HolidayKind>()
  for (const item of items) {
    if (item && typeof item.date === 'string' && (item.kind === 'holiday' || item.kind === 'workday')) {
      next.set(item.date, item.kind)
    }
  }
  holidayKinds = next
}

/** 清空年历（测试与「未同步」退化路径：记号退化为按星期判定）。 */
export function clearHolidayCalendar(): void {
  holidayKinds = new Map()
}

/** 按北京日期 YYYY-MM-DD 查标注；无标注返回 null（视为普通日期）。 */
export function holidayKindOf(date: string): HolidayKind | null {
  return holidayKinds.get(date) ?? null
}

/** 以 UTC 字段格式化 YYYY-MM-DD（入参已是「北京时间墙上时刻」的偏移形态时专用）。 */
function formatDate(t: Date): string {
  const m = String(t.getUTCMonth() + 1).padStart(2, '0')
  const d = String(t.getUTCDate()).padStart(2, '0')
  return `${t.getUTCFullYear()}-${m}-${d}`
}

/** 北京日期 YYYY-MM-DD（年历主键口径），同后端 beijingDate。 */
export function beijingDate(at: Date): string {
  return formatDate(new Date(at.getTime() + BEIJING_OFFSET_MS))
}

// ---------- 解析 ----------

function parseClock(s: string): number | null {
  const m = CLOCK_RE.exec(s.trim())
  if (!m) return null
  const h = Number(m[1])
  const min = Number(m[2])
  if (h > 23 || min > 59) return null
  return h * 60 + min
}

/** 解析单个星期值（1=周一..7=周日）；非整数或越界返回 null。 */
function parseWeekday(s: string): number | null {
  const t = s.trim()
  if (!INTEGER_RE.test(t)) return null
  const n = Number(t)
  return n >= 1 && n <= 7 ? n : null
}

/** 解析规则字符串；空串或非法规则返回 null（展示层视为未配置）。 */
export function parsePeakWindow(rule: string): ParsedPeakWindow | null {
  const parts = rule.trim().split(';')
  if (parts.length !== 2) return null
  const dayPart = parts[0].trim()
  if (!dayPart) return null
  const days = new Array<boolean>(8).fill(false)
  let skipHoliday = false
  let makeupWorkday = false
  for (const item of dayPart.split(',')) {
    const seg = item.trim()
    if (!seg) return null
    // 节假日记号（大小写不敏感）：`!H` 放假日不计峰、`W` 补班日按工作日（同后端 parseWeekdays）。
    const upper = seg.toUpperCase()
    if (upper === '!H') {
      skipHoliday = true
      continue
    }
    if (upper === 'W') {
      makeupWorkday = true
      continue
    }
    if (seg.startsWith('!')) return null // 未知节假日记号
    const dash = seg.indexOf('-')
    const lo = parseWeekday(dash >= 0 ? seg.slice(0, dash) : seg)
    const hi = parseWeekday(dash >= 0 ? seg.slice(dash + 1) : seg)
    if (lo === null || hi === null || lo > hi) return null
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
  return { days, spans, skipHoliday, makeupWorkday }
}

/** 规则是否带节假日记号（`!H` / `W`）；展示「依节假日年历判定」提示时用。 */
export function hasHolidayTokens(rule: string): boolean {
  const parsed = parsePeakWindow(rule)
  return parsed != null && (parsed.skipHoliday || parsed.makeupWorkday)
}

// ---------- 判定 ----------

/** 单日是否命中星期集合：常规星期命中，或（配 `W` 时）调休补班日视为命中。 */
function dayMatched(parsed: ParsedPeakWindow, day: number, kind: HolidayKind | null): boolean {
  return parsed.days[day] || (parsed.makeupWorkday && kind === 'workday')
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
  const kind = holidayKindOf(beijingDate(at))
  // `!H`：放假日（含调休连休）全天空闲，优先于星期与时段判定。
  if (parsed.skipHoliday && kind === 'holiday') return 'off_peak'
  const bj = new Date(at.getTime() + BEIJING_OFFSET_MS)
  const day = bj.getUTCDay() === 0 ? 7 : bj.getUTCDay()
  if (!dayMatched(parsed, day, kind)) return 'off_peak'
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

/**
 * 切换推算/区间回溯窗口（天）：规则以周为周期，但 `!H` 记号下春节等 8 天连休
 * 叠加相邻周末可产生超过一周的连续空闲段，取 16 天覆盖最长连休并留余量；
 * 窗口内仍无切换时返回 null（上层按无倒计时处理）。
 */
const SWITCH_WINDOW_DAYS = 16

/**
 * 枚举 [fromMs, toMs] 内真正发生峰/谷切换的瞬间（升序）。
 *
 * 候选点取每段高峰窗口的起止分钟与每日零点（覆盖 00:00 起的高峰窗口），
 * 再按「前后一分钟的时段是否不同」过滤掉首尾相接的无效边界。
 * 高峰窗口的起止只在当日命中星期集合（含 `W` 补班日）时才是候选；
 * `!H` 放假日即便命中星期也全天空闲，多余候选会被过滤掉。
 */
function switchPoints(parsed: ParsedPeakWindow, fromMs: number, toMs: number): number[] {
  const points: number[] = []
  const firstDayMs = Math.floor((fromMs + BEIJING_OFFSET_MS) / DAY_MS) * DAY_MS - DAY_MS
  for (let dayMs = firstDayMs; dayMs <= toMs + BEIJING_OFFSET_MS + DAY_MS; dayMs += DAY_MS) {
    const shifted = new Date(dayMs) // 北京零点的偏移形态，UTC 字段即北京日期
    const weekday = shifted.getUTCDay()
    const day = weekday === 0 ? 7 : weekday
    const kind = holidayKindOf(formatDate(shifted))
    const candidates = [dayMs]
    if (dayMatched(parsed, day, kind)) {
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
 * 规则带 `!H` / `W` 时按注入的节假日年历修正切换点（放假日跳过、补班日计入）。
 */
export function nextPeakSwitch(rule: string, from: Date): PeakSwitch | null {
  const parsed = parsePeakWindow(rule)
  if (!parsed) return null
  const fromMs = from.getTime()
  const next = switchPoints(parsed, fromMs, fromMs + SWITCH_WINDOW_DAYS * DAY_MS).find((ms) => ms > fromMs)
  if (next === undefined) return null
  return { at: new Date(next), to: periodOf(parsed, new Date(next)), ms: next - fromMs }
}

/**
 * 当前时段区间的起止（画「距下次切换」进度条用）；规则为空或非法、
 * 或区间端点超出回溯窗口时返回 null。
 */
export function peakPeriodBounds(rule: string, at: Date): PeakBounds | null {
  const parsed = parsePeakWindow(rule)
  if (!parsed) return null
  const atMs = at.getTime()
  const points = switchPoints(parsed, atMs - SWITCH_WINDOW_DAYS * DAY_MS, atMs + SWITCH_WINDOW_DAYS * DAY_MS)
  const end = points.find((ms) => ms > atMs)
  let start: number | undefined
  for (const ms of points) {
    if (ms <= atMs) start = ms
  }
  if (end === undefined || start === undefined) return null
  return { start: new Date(start), end: new Date(end) }
}

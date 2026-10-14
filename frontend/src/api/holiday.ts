import { request } from './http'
import { beijingDate, setHolidayCalendar, type HolidayKind } from '../utils/peakWindow'

/** 一条节假日标注（后端 model.HolidayDate）。 */
export interface HolidayDateItem {
  /** 北京日期 YYYY-MM-DD。 */
  date: string
  /** holiday=放假（含调休连休）| workday=调休补班上班。 */
  kind: HolidayKind
  /** 节日名（如 国庆节），仅展示。 */
  name: string
  year: number
  synced_at: string
}

/** 节假日年历同步状态（后端 service.HolidayStatus）。 */
export interface HolidayStatus {
  enabled: boolean
  auto_sync: boolean
  source_url: string
  interval_hours: number
  last_sync_at?: string
  last_error?: string
  last_sync_count: number
  last_sync_years?: number[]
  holiday_count: number
  workday_count: number
}

/** GET /api/v1/settings/holidays?year=YYYY 的响应：某年标注 + 同步状态。 */
export interface HolidayYearResponse {
  year: number
  items: HolidayDateItem[]
  status: HolidayStatus
}

export const holidayApi = {
  listYear: (year: number) =>
    request<HolidayYearResponse>({ url: '/api/v1/settings/holidays', method: 'GET', params: { year } }),
}

const DAY_MS = 86_400_000

/**
 * 年历需要覆盖的判定跨度（天）：peakWindow 的切换推算/区间回溯窗口
 * （见 utils/peakWindow 的 SWITCH_WINDOW_DAYS），跨年时把上/下一年一并拉上。
 */
const CALENDAR_SPAN_DAYS = 16

/** 模块级拉取缓存：整个会话只拉一次，多组件挂载共享同一请求。 */
let loader: Promise<void> | null = null

/**
 * 拉取（并注入 utils/peakWindow）节假日年历，供 `!H` / `W` 记号判定。
 *
 * 只拉一次并缓存；拉取失败不留缓存、下次挂载自动重试 —— 失败期间记号
 * 退化为按星期判定（与后端年历未同步时同口径），不打断页面。
 */
export function ensureHolidayCalendar(): Promise<void> {
  if (!loader) {
    loader = loadHolidayCalendar().catch((err) => {
      loader = null
      throw err
    })
  }
  return loader
}

/** 清空拉取缓存（测试与手动强刷前调用；已注入的年历不受影响）。 */
export function resetHolidayLoader(): void {
  loader = null
}

async function loadHolidayCalendar(): Promise<void> {
  const now = Date.now()
  const years = new Set<number>()
  for (const ms of [now - CALENDAR_SPAN_DAYS * DAY_MS, now, now + CALENDAR_SPAN_DAYS * DAY_MS]) {
    years.add(Number(beijingDate(new Date(ms)).slice(0, 4)))
  }
  const results = await Promise.all([...years].sort().map((year) => holidayApi.listYear(year)))
  setHolidayCalendar(results.flatMap((r) => r.items ?? []))
}

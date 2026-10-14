import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ensureHolidayCalendar, holidayApi, resetHolidayLoader, type HolidayYearResponse } from './holiday'
import { request } from './http'
import { clearHolidayCalendar, holidayKindOf, peakPeriodAt } from '../utils/peakWindow'

vi.mock('./http', () => ({ request: vi.fn() }))

const mockRequest = vi.mocked(request)

function yearResponse(year: number, dates: Array<{ date: string; kind: 'holiday' | 'workday' }>): HolidayYearResponse {
  return {
    year,
    items: dates.map((d) => ({ ...d, name: '', year, synced_at: '2026-09-01T00:00:00Z' })),
    status: {
      enabled: true,
      auto_sync: true,
      source_url: '',
      interval_hours: 24,
      last_sync_count: 0,
      holiday_count: 0,
      workday_count: 0,
    },
  }
}

describe('holidayApi.listYear', () => {
  beforeEach(() => mockRequest.mockReset())

  it('GET /api/v1/settings/holidays?year=YYYY 并解包 data', async () => {
    const resp = yearResponse(2026, [{ date: '2026-10-01', kind: 'holiday' }])
    mockRequest.mockResolvedValue(resp)
    const got = await holidayApi.listYear(2026)
    expect(got).toBe(resp)
    expect(mockRequest).toHaveBeenCalledWith({
      url: '/api/v1/settings/holidays',
      method: 'GET',
      params: { year: 2026 },
    })
  })
})

describe('ensureHolidayCalendar', () => {
  beforeEach(() => {
    mockRequest.mockReset()
    resetHolidayLoader()
    clearHolidayCalendar()
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    resetHolidayLoader()
    clearHolidayCalendar()
  })

  it('拉取判定窗口覆盖的年份并注入年历（跨年时含上/下一年）', async () => {
    // 2026-12-28 的 ±16 天跨入 2027，两年都要拉
    vi.setSystemTime(new Date('2026-12-28T10:00:00+08:00'))
    mockRequest.mockImplementation((config) =>
      Promise.resolve(
        yearResponse(Number((config.params as { year: number }).year), [
          { date: '2026-10-01', kind: 'holiday' },
        ]),
      ),
    )
    await ensureHolidayCalendar()
    const years = mockRequest.mock.calls.map((c) => (c[0].params as { year: number }).year)
    expect(years.sort()).toEqual([2026, 2027])
    expect(holidayKindOf('2026-10-01')).toBe('holiday')
  })

  it('年内只拉当年一年', async () => {
    vi.setSystemTime(new Date('2026-10-13T10:00:00+08:00'))
    mockRequest.mockResolvedValue(yearResponse(2026, []))
    await ensureHolidayCalendar()
    expect(mockRequest).toHaveBeenCalledTimes(1)
  })

  it('整个会话只拉一次（缓存命中不重复请求）', async () => {
    vi.setSystemTime(new Date('2026-10-13T10:00:00+08:00'))
    mockRequest.mockResolvedValue(yearResponse(2026, []))
    await ensureHolidayCalendar()
    await ensureHolidayCalendar()
    expect(mockRequest).toHaveBeenCalledTimes(1)
  })

  it('拉取失败不留缓存，下次调用重试', async () => {
    vi.setSystemTime(new Date('2026-10-13T10:00:00+08:00'))
    mockRequest.mockRejectedValueOnce(new Error('网络异常')).mockResolvedValueOnce(yearResponse(2026, [{ date: '2026-10-01', kind: 'holiday' }]))
    await expect(ensureHolidayCalendar()).rejects.toThrow('网络异常')
    await ensureHolidayCalendar()
    expect(mockRequest).toHaveBeenCalledTimes(2)
    expect(holidayKindOf('2026-10-01')).toBe('holiday')
  })

  it('注入后 peakPeriodAt 对 !H / W 记号生效', async () => {
    vi.setSystemTime(new Date('2026-09-28T10:00:00+08:00'))
    mockRequest.mockResolvedValue(
      yearResponse(2026, [
        { date: '2026-10-01', kind: 'holiday' },
        { date: '2026-10-10', kind: 'workday' },
      ]),
    )
    await ensureHolidayCalendar()
    const rule = '1-5,!H,W;09:00-12:00,14:00-18:00'
    expect(peakPeriodAt(rule, new Date('2026-10-01T10:00:00+08:00'))).toBe('off_peak') // 放假日全天空闲
    expect(peakPeriodAt(rule, new Date('2026-10-10T10:00:00+08:00'))).toBe('peak') // 补班周六按工作日
    expect(peakPeriodAt(rule, new Date('2026-10-13T10:00:00+08:00'))).toBe('peak') // 普通工作日
  })
})

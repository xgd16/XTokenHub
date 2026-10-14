import { afterEach, describe, expect, it } from 'vitest'
import {
  clearHolidayCalendar,
  hasHolidayTokens,
  holidayKindOf,
  nextPeakSwitch,
  peakPeriodAt,
  peakPeriodBounds,
  parsePeakWindow,
  setHolidayCalendar,
} from './peakWindow'

// DeepSeek 官方错峰规则：工作日 9:00-12:00、14:00-18:00 为高峰。
const deepseek = '1-5;09:00-12:00,14:00-18:00'

// 用带时区偏移的 ISO 串构造时刻，断言与测试运行环境的时区无关。
const bj = (iso: string) => new Date(iso)

describe('peakWindow', () => {
  // 节假日年历是模块级状态：每例清空，避免用例间互相污染
  afterEach(() => clearHolidayCalendar())

  it('空规则与非法规则返回 null（视为未配置）', () => {
    expect(parsePeakWindow('')).toBeNull()
    expect(peakPeriodAt('', new Date())).toBeNull()
    for (const bad of [
      '1-5', // 缺时段段
      '1-5;', // 空时段段
      ';09:00-12:00', // 空星期段
      '0;09:00-12:00', // 星期越界
      '8;09:00-12:00', // 星期越界
      '5-1;09:00-12:00', // 区间起点大于终点
      '1;12:00-09:00', // 结束早于开始（不支持跨零点）
      '1;09:00-09:00', // 零长区间
      '1;24:00-25:00', // 小时越界
      '1;09:60-12:00', // 分钟越界
      '1;0900-1200', // 缺冒号
      '1;09:00-12:00,', // 尾随逗号产生空项
      '1-5;09:00-12:00;14:00-18:00', // 分号段数过多
    ]) {
      expect(parsePeakWindow(bad)).toBeNull()
    }
  })

  it('合法规则可解析（区间/列表/空白容忍/单位数小时）', () => {
    for (const ok of [
      deepseek,
      '1;09:00-12:00',
      '1-7;00:00-23:59',
      '1,3,5;09:00-12:00,14:00-18:00',
      '6-7;10:00-11:00',
      ' 1-5 ; 09:00 - 12:00 ',
      '1;9:00-12:00',
    ]) {
      expect(parsePeakWindow(ok)).not.toBeNull()
    }
  })

  it('DeepSeek 工作日高峰边界判定（起含止不含）', () => {
    // 2026-01-05 是周一，01-03 是周六，01-04 是周日。
    const cases: Array<[string, 'peak' | 'off_peak']> = [
      ['2026-01-05T09:00:00+08:00', 'peak'],
      ['2026-01-05T10:30:00+08:00', 'peak'],
      ['2026-01-05T11:59:00+08:00', 'peak'],
      ['2026-01-05T12:00:00+08:00', 'off_peak'],
      ['2026-01-05T13:00:00+08:00', 'off_peak'],
      ['2026-01-05T14:00:00+08:00', 'peak'],
      ['2026-01-05T16:00:00+08:00', 'peak'],
      ['2026-01-05T18:00:00+08:00', 'off_peak'],
      ['2026-01-05T20:00:00+08:00', 'off_peak'],
      ['2026-01-05T03:00:00+08:00', 'off_peak'],
      ['2026-01-09T10:00:00+08:00', 'peak'], // 周五
      ['2026-01-03T10:00:00+08:00', 'off_peak'], // 周六
      ['2026-01-04T15:00:00+08:00', 'off_peak'], // 周日
      ['2026-01-05T23:59:00+08:00', 'off_peak'],
    ]
    for (const [iso, want] of cases) {
      expect(peakPeriodAt(deepseek, bj(iso))).toBe(want)
    }
  })

  it('判定按北京时间换算，与入参时刻的时区无关', () => {
    // 北京周一 10:00 == UTC 周一 02:00，同一瞬间两种写法
    expect(peakPeriodAt(deepseek, bj('2026-01-05T10:00:00+08:00'))).toBe('peak')
    expect(peakPeriodAt(deepseek, bj('2026-01-05T02:00:00Z'))).toBe('peak')
    // UTC 周一 18:30 已是北京周二凌晨：不落入「周一高峰」，跨天方向正确
    expect(peakPeriodAt(deepseek, bj('2026-01-05T18:30:00Z'))).toBe('off_peak')
    expect(peakPeriodAt('2;00:00-06:00', bj('2026-01-05T18:30:00Z'))).toBe('peak')
  })

  it('多时段窗口（夜间高峰）', () => {
    const rule = '1-7;00:00-06:00,22:00-23:59'
    expect(peakPeriodAt(rule, bj('2026-01-05T03:00:00+08:00'))).toBe('peak')
    expect(peakPeriodAt(rule, bj('2026-01-05T12:00:00+08:00'))).toBe('off_peak')
    expect(peakPeriodAt(rule, bj('2026-01-05T23:00:00+08:00'))).toBe('peak')
    expect(peakPeriodAt(rule, bj('2026-01-05T23:59:00+08:00'))).toBe('off_peak')
  })

  describe('nextPeakSwitch', () => {
    it('规则为空/非法返回 null', () => {
      expect(nextPeakSwitch('', new Date())).toBeNull()
      expect(nextPeakSwitch('bogus', new Date())).toBeNull()
    })

    it('高峰中给出下一次「转空闲」', () => {
      const s = nextPeakSwitch(deepseek, bj('2026-01-05T10:30:00+08:00'))
      expect(s).not.toBeNull()
      expect(s!.to).toBe('off_peak')
      // 10:30 -> 12:00 = 90 分钟
      expect(s!.ms).toBe(90 * 60_000)
      expect(s!.at.toISOString()).toBe(bj('2026-01-05T12:00:00+08:00').toISOString())
    })

    it('空闲中给出下一次「转高峰」（跳过午休到下午场）', () => {
      const s = nextPeakSwitch(deepseek, bj('2026-01-05T12:30:00+08:00'))
      expect(s!.to).toBe('peak')
      expect(s!.ms).toBe(90 * 60_000) // 12:30 -> 14:00
    })

    it('收盘后跨到次日上午高峰', () => {
      const s = nextPeakSwitch(deepseek, bj('2026-01-05T20:00:00+08:00'))
      expect(s!.to).toBe('peak')
      expect(s!.at.toISOString()).toBe(bj('2026-01-06T09:00:00+08:00').toISOString())
    })

    it('周五收盘后跨过周末到下周一高峰', () => {
      const s = nextPeakSwitch(deepseek, bj('2026-01-09T20:00:00+08:00')) // 周五
      expect(s!.to).toBe('peak')
      expect(s!.at.toISOString()).toBe(bj('2026-01-12T09:00:00+08:00').toISOString()) // 周一
    })

    it('切换点严格晚于 from（恰好在边界时不返回当前这一刻）', () => {
      const s = nextPeakSwitch(deepseek, bj('2026-01-05T12:00:00+08:00'))
      expect(s!.to).toBe('peak')
      expect(s!.ms).toBe(120 * 60_000) // 12:00 -> 14:00
    })
  })

  describe('peakPeriodBounds', () => {
    it('规则为空/非法返回 null', () => {
      expect(peakPeriodBounds('', new Date())).toBeNull()
      expect(peakPeriodBounds('bogus', new Date())).toBeNull()
    })

    it('高峰中：区间为窗口起止', () => {
      const b = peakPeriodBounds(deepseek, bj('2026-01-05T10:30:00+08:00'))
      expect(b!.start.toISOString()).toBe(bj('2026-01-05T09:00:00+08:00').toISOString())
      expect(b!.end.toISOString()).toBe(bj('2026-01-05T12:00:00+08:00').toISOString())
    })

    it('午休空闲：区间为上午窗口结束到下午窗口开始', () => {
      const b = peakPeriodBounds(deepseek, bj('2026-01-05T13:00:00+08:00'))
      expect(b!.start.toISOString()).toBe(bj('2026-01-05T12:00:00+08:00').toISOString())
      expect(b!.end.toISOString()).toBe(bj('2026-01-05T14:00:00+08:00').toISOString())
    })

    it('周末空闲：区间跨越整个周末到周一高峰', () => {
      const b = peakPeriodBounds(deepseek, bj('2026-01-10T12:00:00+08:00')) // 周六
      expect(b!.start.toISOString()).toBe(bj('2026-01-09T18:00:00+08:00').toISOString()) // 周五收盘
      expect(b!.end.toISOString()).toBe(bj('2026-01-12T09:00:00+08:00').toISOString()) // 周一开盘
    })

    it('首尾相接的窗口边界不算切换（区间为整个连续段）', () => {
      // 09:00-12:00 与 12:00-14:00 相接：12:00 仍是高峰，区间应直到 14:00
      const b = peakPeriodBounds('1-5;09:00-12:00,12:00-14:00', bj('2026-01-05T10:00:00+08:00'))
      expect(b!.start.toISOString()).toBe(bj('2026-01-05T09:00:00+08:00').toISOString())
      expect(b!.end.toISOString()).toBe(bj('2026-01-05T14:00:00+08:00').toISOString())
    })
  })

  describe('节假日记号（!H / W）', () => {
    // 年历口径同 Go 峰谷测试：2026-10-01 周四（国庆节，kind=holiday）、
    // 2026-10-10 周六（国庆后补班，kind=workday）、2026-10-13 周二（普通工作日）、
    // 2026-10-17 周六（普通周末）。
    it('记号解析：合法组合接受，非法记号返回 null', () => {
      for (const ok of [
        '1-5,!H;09:00-12:00,14:00-18:00', // DeepSeek 口径：节假日全天空闲
        '1-5,W;09:00-12:00,14:00-18:00', // 补班日按工作日
        '1-5,!H,W;09:00-12:00', // 两个记号组合
        '!H;00:00-23:59', // 只有记号、无星期
        '1-5,!h;09:00-12:00', // 小写容忍
        '1-5,w;09:00-12:00',
      ]) {
        expect(parsePeakWindow(ok)).not.toBeNull()
      }
      for (const bad of [
        '1-5,!X;09:00-12:00', // 未知排除记号
        '1-5,H;09:00-12:00', // 少了 ! 也拒绝（提示用 !H）
        '1-5,!!H;09:00-12:00',
        '1-5,W-6;09:00-12:00', // 记号不能参与星期区间
      ]) {
        expect(parsePeakWindow(bad)).toBeNull()
      }
      expect(hasHolidayTokens('1-5,!H;09:00-12:00')).toBe(true)
      expect(hasHolidayTokens('1-5,w;09:00-12:00')).toBe(true)
      expect(hasHolidayTokens(deepseek)).toBe(false)
    })

    it('!H：放假日（含调休连休）全天空闲，优先于星期判定', () => {
      setHolidayCalendar([{ date: '2026-10-01', kind: 'holiday' }])
      const rule = '1-5,!H;09:00-12:00,14:00-18:00'
      expect(peakPeriodAt(rule, bj('2026-10-01T10:00:00+08:00'))).toBe('off_peak') // 周四白天本是高峰
      expect(peakPeriodAt(rule, bj('2026-10-01T20:00:00+08:00'))).toBe('off_peak') // 全天不止单个窗口
      // 未配 !H：不受年历影响，维持原口径
      expect(peakPeriodAt(deepseek, bj('2026-10-01T10:00:00+08:00'))).toBe('peak')
      // 普通工作日不受影响
      expect(peakPeriodAt(rule, bj('2026-10-13T10:00:00+08:00'))).toBe('peak')
      // 年历按北京日期匹配：写成 UTC 时刻（北京 10-01 10:00）判定一致
      expect(peakPeriodAt(rule, bj('2026-10-01T02:00:00Z'))).toBe('off_peak')
    })

    it('W：调休补班日（周末上班）按工作日计入高峰', () => {
      setHolidayCalendar([{ date: '2026-10-10', kind: 'workday' }])
      const rule = '1-5,!H,W;09:00-12:00,14:00-18:00'
      expect(peakPeriodAt(rule, bj('2026-10-10T10:00:00+08:00'))).toBe('peak') // 补班周六白天
      expect(peakPeriodAt(rule, bj('2026-10-10T13:00:00+08:00'))).toBe('off_peak') // 午休照旧不计峰
      // 未配 W：周末一律空闲
      expect(peakPeriodAt('1-5,!H;09:00-12:00,14:00-18:00', bj('2026-10-10T10:00:00+08:00'))).toBe('off_peak')
      // 普通周六即使配了 W 也照常空闲
      expect(peakPeriodAt(rule, bj('2026-10-17T10:00:00+08:00'))).toBe('off_peak')
    })

    it('无年历数据时退化为按星期判定（不误判）', () => {
      clearHolidayCalendar()
      const rule = '1-5,!H,W;09:00-12:00,14:00-18:00'
      expect(peakPeriodAt(rule, bj('2026-10-01T10:00:00+08:00'))).toBe('peak') // 放假日当普通周四
      expect(peakPeriodAt(rule, bj('2026-10-10T10:00:00+08:00'))).toBe('off_peak') // 补班周六当普通周六
    })

    it('记号同样作用于切换推算与区间', () => {
      setHolidayCalendar([
        { date: '2026-10-01', kind: 'holiday' },
        { date: '2026-10-10', kind: 'workday' },
      ])
      const rule = '1-5,!H,W;09:00-12:00,14:00-18:00'
      // 周三收盘后：周四国庆全天空闲，下一次转高峰是周五 10-02 09:00
      const s = nextPeakSwitch(rule, bj('2026-09-30T19:00:00+08:00'))
      expect(s!.to).toBe('peak')
      expect(s!.at.toISOString()).toBe(bj('2026-10-02T09:00:00+08:00').toISOString())
      // 补班周六 10-10 08:00：当日 09:00 转高峰
      const m = nextPeakSwitch(rule, bj('2026-10-10T08:00:00+08:00'))
      expect(m!.to).toBe('peak')
      expect(m!.at.toISOString()).toBe(bj('2026-10-10T09:00:00+08:00').toISOString())
      // 补班周六在高峰中：区间为当日窗口起止
      const b = peakPeriodBounds(rule, bj('2026-10-10T10:00:00+08:00'))
      expect(b!.start.toISOString()).toBe(bj('2026-10-10T09:00:00+08:00').toISOString())
      expect(b!.end.toISOString()).toBe(bj('2026-10-10T12:00:00+08:00').toISOString())
    })

    it('年历整体替换、按北京日期查询', () => {
      setHolidayCalendar([{ date: '2026-10-01', kind: 'holiday' }])
      setHolidayCalendar([{ date: '2026-10-10', kind: 'workday' }])
      expect(holidayKindOf('2026-10-01')).toBeNull() // 第二次注入整体替换
      expect(holidayKindOf('2026-10-10')).toBe('workday')
    })
  })
})

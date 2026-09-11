import { describe, expect, it } from 'vitest'
import { compactCN, compactNumber, duration, durationLong, fullTime, hitRateColor, modeShort, percent, protocolShort, timeOf, tokenSpeed } from './format'
import { toModelRank, toStatCards, toTrendSeries } from './transform'
import { parseCSV, nativeProtocolsOf, type Channel } from '../api/channel'

describe('format', () => {
  it('compactNumber 缩写', () => {
    expect(compactNumber(0)).toBe('0')
    expect(compactNumber(999)).toBe('999')
    expect(compactNumber(1500)).toBe('1.5K')
    expect(compactNumber(2_340_000)).toBe('2.34M')
    expect(compactNumber(5_600_000_000)).toBe('5.60B')
    expect(compactNumber(NaN)).toBe('0')
  })

  it('compactCN 中文单位', () => {
    expect(compactCN(69)).toBe('69')          // <1万 原数
    expect(compactCN(9999)).toBe('9999')
    expect(compactCN(10000)).toBe('1万')
    expect(compactCN(19_080_000)).toBe('1908万') // 19.08M
    expect(compactCN(2_340_000)).toBe('234万')
    expect(compactCN(120_000_000)).toBe('1.2亿')
    expect(compactCN(560_000_000)).toBe('5.6亿')
    expect(compactCN(1_000_000_000)).toBe('10亿')
    expect(compactCN(NaN)).toBe('0')
  })

  it('durationLong 长时长', () => {
    expect(durationLong(500)).toBe('500ms')
    expect(durationLong(30_000)).toBe('30.00s')
    expect(durationLong(60_000)).toBe('1分0秒')
    expect(durationLong(65_000)).toBe('1分5秒')
    expect(durationLong(3_600_000)).toBe('1小时0分钟')
    expect(durationLong(10_740_000)).toBe('2小时59分钟')
    expect(durationLong(NaN)).toBe('NaNs')
  })

  it('percent', () => {
    expect(percent(0)).toBe('0.0%')
    expect(percent(0.234)).toBe('23.4%')
    expect(percent(1)).toBe('100.0%')
    expect(percent(0.6667, 2)).toBe('66.67%')
  })

  it('duration', () => {
    expect(duration(50)).toBe('50ms')
    expect(duration(999)).toBe('999ms')
    expect(duration(1500)).toBe('1.50s')
  })

  it('tokenSpeed / tokensPerSec', () => {
    expect(tokenSpeed(0, 1000)).toBe('—')
    expect(tokenSpeed(100, 0)).toBe('—')
    expect(tokenSpeed(NaN, 1000)).toBe('—')
    // 500 tok / 1s = 500 tok/s
    expect(tokenSpeed(500, 1000)).toBe('500 tok/s')
    // 1200 tok / 400ms = 3000 tok/s，<1万 保持原数（中文不喜 K 单位）
    expect(tokenSpeed(1200, 400)).toBe('3000 tok/s')
    // 50_000 tok / 1s = 5万 tok/s
    expect(tokenSpeed(50_000, 1000)).toBe('5万 tok/s')
  })

  it('protocolShort / modeShort', () => {
    expect(protocolShort('chat_completions')).toBe('chat')
    expect(protocolShort('responses')).toBe('responses')
    expect(protocolShort('messages')).toBe('messages')
    expect(modeShort('native_passthrough')).toBe('透传')
    expect(modeShort('converted')).toBe('转换')
  })

  it('timeOf / fullTime 解析', () => {
    const d = new Date(2026, 8, 6, 14, 5, 9)
    expect(timeOf(d.toISOString())).toBe('14:05:09')
    expect(fullTime(d.toISOString())).toBe('09-06 14:05:09')
  })

  it('hitRateColor 分档', () => {
    expect(hitRateColor(0.8)).toBe('var(--accent)')
    expect(hitRateColor(0.3)).toBe('var(--cyan)')
    expect(hitRateColor(0.1)).toBe('var(--amber)')
    expect(hitRateColor(0)).toBe('var(--text-faint)')
  })
})

describe('transform', () => {
  it('toTrendSeries 映射错误与 token', () => {
    const now = new Date('2026-09-05T10:00:00')
    const series = toTrendSeries(
      [{ date: '2026-09-05', requests: 10, total_tokens: 500, error_requests: 2, cost_usd: 0 }],
      1,
      now,
    )
    expect(series[0]).toEqual({ date: '2026-09-05', requests: 10, errors: 2, tokens: 500 })
  })

  it('toModelRank 截断 TopN 并计算缓存百分比', () => {
    const rank = toModelRank(
      [
        { name: 'gpt-4o', requests: 10, total_tokens: 100, cached_tokens: 50, cache_rate: 0.5, avg_ms: 1, cost_usd: 0 },
        { name: 'claude', requests: 5, total_tokens: 40, cached_tokens: 0, cache_rate: 0, avg_ms: 1, cost_usd: 0 },
      ],
      1,
    )
    expect(rank).toHaveLength(1)
    expect(rank[0].cachePercent).toBe(50)
  })

  it('toStatCards', () => {
    const cards = toStatCards({
      total_requests: 100,
      success_requests: 90,
      error_requests: 10,
      prompt_tokens: 400,
      completion_tokens: 100,
      total_tokens: 500,
      cached_tokens: 100,
      cache_hit_rate: 0.25,
      avg_duration_ms: 233.6,
      native_ratio: 0.5,
      cost_usd: 1.23,
    })
    expect(cards.requests).toBe(100)
    expect(cards.hitPercent).toBe('25.0%')
    expect(cards.avgMs).toBe(234)
    expect(cards.nativePercent).toBe('50.0%')
  })
})

describe('channel utils', () => {
  it('parseCSV', () => {
    expect(parseCSV('')).toEqual([])
    expect(parseCSV('a, b,,c')).toEqual(['a', 'b', 'c'])
  })

  it('nativeProtocolsOf', () => {
    const ch = { native_protocols: 'chat_completions,messages' } as Channel
    expect(nativeProtocolsOf(ch)).toEqual(['chat_completions', 'messages'])
  })
})

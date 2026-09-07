import { describe, expect, it } from 'vitest'
import { toBucketSeries, toModelRank, toStatCards, toTrendSeries } from './transform'
import type { GroupStat, Summary, TrendPoint } from '../api/stats'

describe('toTrendSeries', () => {
  const NOW = new Date('2026-09-05T10:00:00')

  it('窗口内日期对位转换', () => {
    const points: TrendPoint[] = [
      { date: '2026-09-04', requests: 10, error_requests: 1, total_tokens: 500 },
      { date: '2026-09-05', requests: 3, error_requests: 0, total_tokens: 120 },
    ]
    expect(toTrendSeries(points, 2, NOW)).toEqual([
      { date: '2026-09-04', requests: 10, errors: 1, tokens: 500 },
      { date: '2026-09-05', requests: 3, errors: 0, tokens: 120 },
    ])
  })

  it('后端只返回有数据的日子，空缺日期补 0', () => {
    const points: TrendPoint[] = [{ date: '2026-09-04', requests: 14, error_requests: 0, total_tokens: 900 }]
    const out = toTrendSeries(points, 5, NOW)
    expect(out.map((p) => p.date)).toEqual(['2026-09-01', '2026-09-02', '2026-09-03', '2026-09-04', '2026-09-05'])
    expect(out[3]).toEqual({ date: '2026-09-04', requests: 14, errors: 0, tokens: 900 })
    expect(out[0]).toEqual({ date: '2026-09-01', requests: 0, errors: 0, tokens: 0 })
  })

  it('跨月补齐正确', () => {
    const now = new Date('2026-09-02T08:00:00')
    const out = toTrendSeries([], 3, now)
    expect(out.map((p) => p.date)).toEqual(['2026-08-31', '2026-09-01', '2026-09-02'])
  })

  it('空库 null / undefined 补齐为全 0 序列', () => {
    expect(toTrendSeries(null, 3, NOW)).toHaveLength(3)
    const out = toTrendSeries(undefined, 3, NOW)
    expect(out.every((p) => p.requests === 0 && p.errors === 0 && p.tokens === 0)).toBe(true)
  })
})

describe('toBucketSeries', () => {
  it('分钟桶按本地 HH:mm 生成标签并补零', () => {
    const now = new Date('2026-09-05T10:03:30')
    const out = toBucketSeries(
      [{ date: '', ts: Math.floor(new Date('2026-09-05T10:02:00').getTime() / 1000), requests: 5, error_requests: 1, total_tokens: 90 }],
      60,
      4,
      now,
    )
    expect(out.map((p) => p.date)).toEqual(['10:00', '10:01', '10:02', '10:03'])
    expect(out[2]).toEqual({ date: '10:02', requests: 5, errors: 1, tokens: 90 })
    expect(out[3].requests).toBe(0)
  })

  it('小时桶标签为 MM-DD HH:00', () => {
    const now = new Date('2026-09-05T10:00:00')
    const out = toBucketSeries([], 3600, 2, now)
    expect(out.map((p) => p.date)).toEqual(['09-05 09:00', '09-05 10:00'])
  })
})

describe('toModelRank', () => {
  it('按 Top N 截断并计算缓存百分比', () => {
    const rows: GroupStat[] = [
      { name: 'gpt-4o', requests: 8, total_tokens: 900, cached_tokens: 400, cache_rate: 0.432, avg_ms: 120 },
      { name: '', requests: 2, total_tokens: 10, cached_tokens: 0, cache_rate: 0, avg_ms: 30 },
    ]
    const rank = toModelRank(rows, 1)
    expect(rank).toHaveLength(1)
    expect(rank[0]).toEqual({ name: 'gpt-4o', requests: 8, tokens: 900, cachePercent: 43.2 })
  })

  it('空模型名归为 unknown；null 安全', () => {
    expect(toModelRank([{ name: '', requests: 1, total_tokens: 2, cached_tokens: 0, cache_rate: 0, avg_ms: 5 }])[0].name).toBe('unknown')
    expect(toModelRank(null)).toEqual([])
    expect(toModelRank(undefined)).toEqual([])
  })
})

describe('toStatCards', () => {
  it('汇总字段转卡片数据', () => {
    const s: Summary = {
      total_requests: 12,
      success_requests: 10,
      error_requests: 2,
      prompt_tokens: 1000,
      completion_tokens: 500,
      total_tokens: 1500,
      cached_tokens: 400,
      cache_hit_rate: 0.4,
      avg_duration_ms: 123.6,
      native_ratio: 0.75,
    }
    expect(toStatCards(s)).toEqual({
      requests: 12,
      errors: 2,
      tokens: '1500',
      hitPercent: '40.0%',
      avgMs: 124,
      nativePercent: '75.0%',
    })
  })
})

// ==================== 热力图 / 多模型趋势 / 环图 ====================
import { toDonut, toHeatmap, toModelTrend } from './transform'

describe('toModelTrend', () => {
  const now = new Date('2026-09-07T12:00:00')

  it('按日对齐并补零', () => {
    const r = toModelTrend(
      [
        { date: '2026-09-06', model: 'a', total_tokens: 100 },
        { date: '2026-09-07', model: 'b', total_tokens: 50 },
      ],
      7,
      6,
      now,
    )
    expect(r.dates).toHaveLength(7)
    expect(r.dates[6]).toBe('2026-09-07')
    const a = r.series.find((s) => s.name === 'a')!
    expect(a.values).toHaveLength(7)
    expect(a.values[5]).toBe(100)
    expect(a.values[6]).toBe(0)
  })

  it('超出 top 的模型合并为「其他」', () => {
    const pts = Array.from({ length: 8 }, (_, i) => ({
      date: '2026-09-07',
      model: `m${i}`,
      total_tokens: 100 - i,
    }))
    const r = toModelTrend(pts, 1, 6, now)
    expect(r.series).toHaveLength(7) // 6 + 其他
    expect(r.series[6].name).toBe('其他')
    expect(r.series[6].values[0]).toBe(100 - 6 + (100 - 7)) // m6 + m7
  })

  it('空数据安全', () => {
    const r = toModelTrend(null, 3, 6, now)
    expect(r.series).toHaveLength(0)
    expect(r.dates).toHaveLength(3)
  })
})

describe('toHeatmap', () => {
  const now = new Date('2026-09-07T12:00:00') // 周一

  it('生成 53 列（首列可能不满一周）且行数不超过 7', () => {
    const r = toHeatmap([{ date: '2026-09-06', total_tokens: 500 }], 'daily', now)
    expect(r.columns).toHaveLength(53)
    for (const col of r.columns) expect(col.cells.length).toBeLessThanOrEqual(7)
  })

  it('daily 模式级别按占比分档', () => {
    const r = toHeatmap(
      [
        { date: '2026-09-06', total_tokens: 1000 }, // 最大 -> level 4
        { date: '2026-09-05', total_tokens: 300 }, // 30% -> level 2
      ],
      'daily',
      now,
    )
    const flat = r.columns.flatMap((c) => c.cells)
    expect(flat.find((c) => c.date === '2026-09-06')!.level).toBe(4)
    expect(flat.find((c) => c.date === '2026-09-05')!.level).toBe(2)
    expect(flat.find((c) => c.date === '2026-09-01')!.level).toBe(0)
  })

  it('weekly 模式每周一格横向排开且带月份标签', () => {
    const r = toHeatmap(
      [
        { date: '2026-09-06', total_tokens: 400 },
        { date: '2026-09-07', total_tokens: 600 },
      ],
      'weekly',
      now,
    )
    expect(r.columns.length).toBeGreaterThan(50) // 列数 = 周数
    for (const col of r.columns) expect(col.cells).toHaveLength(1) // 每列一格
    expect(r.monthLabels.length).toBeGreaterThan(8)
    expect(r.max).toBeGreaterThanOrEqual(1000)
  })

  it('cumulative 模式单调递增且末格为总量', () => {
    const r = toHeatmap(
      [
        { date: '2026-09-05', total_tokens: 300 },
        { date: '2026-09-06', total_tokens: 700 },
      ],
      'cumulative',
      now,
    )
    const flat = r.columns.flatMap((c) => c.cells)
    const v5 = flat.find((c) => c.date === '2026-09-05')!.value
    const v6 = flat.find((c) => c.date === '2026-09-06')!.value
    expect(v6).toBe(1000)
    expect(v6).toBeGreaterThanOrEqual(v5)
  })

  it('weeks 参数控制窗口长度', () => {
    const r = toHeatmap([{ date: '2026-09-06', total_tokens: 500 }], 'daily', now, 26)
    expect(r.columns).toHaveLength(27) // 26 周 + 首列补齐
    for (const col of r.columns) expect(col.cells.length).toBeLessThanOrEqual(7)
  })

  it('月份标签跨月出现', () => {
    const r = toHeatmap([], 'daily', now)
    const labels = r.monthLabels.map((m) => m.label)
    expect(labels.length).toBeGreaterThan(8)
    expect(labels[0]).toMatch(/月$/)
  })
})

describe('toDonut', () => {
  it('取前 top 并合并其他，百分比总和为 100', () => {
    const series = Array.from({ length: 7 }, (_, i) => ({
      name: `m${i}`,
      values: [100 - i],
    }))
    const { slices, total } = toDonut(series, 5)
    expect(total).toBe(100 + 99 + 98 + 97 + 96 + 95 + 94)
    expect(slices).toHaveLength(6)
    expect(slices[0].name).toBe('m0')
    expect(slices[5].name).toBe('其他')
    expect(Math.round(slices.reduce((a, s) => a + s.percent, 0))).toBe(100)
  })

  it('全零安全', () => {
    const { slices, total } = toDonut([{ name: 'a', values: [0, 0] }], 5)
    expect(total).toBe(0)
    expect(slices[0].percent).toBe(0)
  })
})

import { groupBySession } from './transform'
import type { RequestLog } from '../api/log'

/** 构造请求日志行的最小工厂。 */
function mkLog(over: Partial<RequestLog>): RequestLog {
  return {
    id: 0,
    created_at: '2026-09-07T10:00:00+08:00',
    protocol: 'chat_completions',
    forward_mode: 'native_passthrough',
    channel_id: 1,
    channel_name: '渠道A',
    key_id: 0,
    key_name: '',
    model: 'gpt-4o',
    stream: false,
    prompt_tokens: 0,
    completion_tokens: 0,
    total_tokens: 0,
    cached_tokens: 0,
    cache_write_tokens: 0,
    cache_hit_rate: 0,
    duration_ms: 0,
    upstream_status: 200,
    client_ip: '',
    user_agent: '',
    error: '',
    ...over,
  }
}

describe('groupBySession', () => {
  it('同一 session_id 合并为一组，无标识行各自成组', () => {
    const rows = [
      mkLog({ id: 3, session_id: 'sess-a', created_at: '2026-09-07T10:03:00+08:00' }),
      mkLog({ id: 2, created_at: '2026-09-07T10:02:00+08:00' }),
      mkLog({ id: 1, session_id: 'sess-a', created_at: '2026-09-07T10:01:00+08:00' }),
    ]
    const gs = groupBySession(rows)
    expect(gs.map((g) => g.key)).toEqual(['sess:sess-a', 'req:2'])
    expect(gs[0].requests.map((r) => r.id)).toEqual([3, 1])
    expect(gs[0].firstAt).toBe('2026-09-07T10:01:00+08:00')
    expect(gs[0].lastAt).toBe('2026-09-07T10:03:00+08:00')
    expect(gs[1].requests).toHaveLength(1)
    expect(gs[1].sessionId).toBe('')
  })

  it('聚合统计：token/耗时求和、模型去重保持顺序', () => {
    const rows = [
      mkLog({ id: 2, session_id: 's', model: 'claude-x', total_tokens: 300, prompt_tokens: 200, completion_tokens: 100, cached_tokens: 50, duration_ms: 1200 }),
      mkLog({ id: 1, session_id: 's', model: 'gpt-4o', total_tokens: 100, prompt_tokens: 60, completion_tokens: 40, cached_tokens: 30, duration_ms: 800 }),
    ]
    const [g] = groupBySession(rows)
    expect(g.totalTokens).toBe(400)
    expect(g.promptTokens).toBe(260)
    expect(g.completionTokens).toBe(140)
    expect(g.cachedTokens).toBe(80)
    expect(g.totalMs).toBe(2000)
    expect(g.errors).toBe(0)
    expect(g.models).toEqual(['claude-x', 'gpt-4o'])
  })

  it('实时推送未落库行计为运行中，error 行计为错误', () => {
    const rows = [
      mkLog({ id: 0, req_id: 9, session_id: 's' }),
      mkLog({ id: 1, session_id: 's', error: 'boom', upstream_status: 502 }),
      mkLog({ id: 2, session_id: 's' }),
    ]
    const [g] = groupBySession(rows)
    expect(g.running).toBe(1)
    expect(g.errors).toBe(1)
  })

  it('组序 = 组内最新请求次序（新→旧），与输入一致', () => {
    const rows = [
      mkLog({ id: 3, session_id: 'sess-old', created_at: '2026-09-07T10:03:00+08:00' }),
      mkLog({ id: 2, session_id: 'sess-new', created_at: '2026-09-07T10:02:00+08:00' }),
      mkLog({ id: 1, session_id: 'sess-old', created_at: '2026-09-07T09:00:00+08:00' }),
    ]
    expect(groupBySession(rows).map((g) => g.sessionId)).toEqual(['sess-old', 'sess-new'])
  })

  it('客户端与调用方取首个非空值', () => {
    const rows = [
      mkLog({ id: 2, session_id: 's', user_agent: '', key_name: 'k2' }),
      mkLog({ id: 1, session_id: 's', user_agent: 'ZCode/3.11.2', key_name: 'k1' }),
    ]
    const [g] = groupBySession(rows)
    expect(g.userAgent).toBe('ZCode/3.11.2')
    expect(g.keyName).toBe('k2')
  })
})

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
import type { LiveSession } from '../api/stats'

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

  it('协议与转发模式去重保持出现顺序', () => {
    const rows = [
      mkLog({ id: 3, session_id: 's', protocol: 'responses', forward_mode: 'converted' }),
      mkLog({ id: 2, session_id: 's', protocol: 'chat_completions', forward_mode: 'native_passthrough' }),
      mkLog({ id: 1, session_id: 's', protocol: 'responses', forward_mode: 'converted' }),
    ]
    const [g] = groupBySession(rows)
    expect(g.protocols).toEqual(['responses', 'chat_completions'])
    expect(g.modes).toEqual(['converted', 'native_passthrough'])
  })
})

import { mergeLiveEvent, mergeLiveEvents, mergeSessionViews, trimLiveRows } from './transform'

describe('trimLiveRows', () => {
  it('不超容量时原样返回', () => {
    const rows = [mkLog({ id: 1, session_id: 's1' }), mkLog({ id: 2 })]
    expect(trimLiveRows(rows, 30)).toBe(rows)
    expect(trimLiveRows(rows, 0)).toEqual([])
  })

  it('超容量时从最旧端整组移除会话，不把会话切成两半', () => {
    // 新→旧：s1 有 3 行（最新），散行 2 行，s2 有 4 行（最旧）
    const rows = [
      mkLog({ id: 1, session_id: 's1' }),
      mkLog({ id: 2, session_id: 's1' }),
      mkLog({ id: 3, session_id: 's1' }),
      mkLog({ id: 4 }),
      mkLog({ id: 5 }),
      mkLog({ id: 6, session_id: 's2' }),
      mkLog({ id: 7, session_id: 's2' }),
      mkLog({ id: 8, session_id: 's2' }),
      mkLog({ id: 9, session_id: 's2' }),
    ]
    // 容量 6：需移除 3 行——最旧的 s2 整组 4 行被移除，即使超摘 1 行
    const out = trimLiveRows(rows, 6)
    expect(out.map((r) => r.id)).toEqual([1, 2, 3, 4, 5])
    // 聚合校验：s1 组仍包含全部 3 行
    const groups = groupBySession(out)
    const s1 = groups.find((g) => g.sessionId === 's1')
    expect(s1?.requests).toHaveLength(3)
  })

  it('移除旧组后恰好不超容量时不误伤更多组', () => {
    const rows = [
      mkLog({ id: 1, session_id: 'a' }),
      mkLog({ id: 2, session_id: 'b' }),
      mkLog({ id: 3, session_id: 'c' }),
      mkLog({ id: 4, session_id: 'c' }),
    ]
    // 容量 3：移除 1 行即可 → 最旧的 c 组整组移除（2 行），a/b 保留
    expect(trimLiveRows(rows, 3).map((r) => r.id)).toEqual([1, 2])
  })

  it('单一会话自身超容量时退化为保留最新 max 行', () => {
    const rows = Array.from({ length: 12 }, (_, i) => mkLog({ id: i + 1, session_id: 'big' }))
    const out = trimLiveRows(rows, 10)
    expect(out).toHaveLength(10)
    expect(out.map((r) => r.id)).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9, 10])
  })

  it('无 session_id 的行按 req_id 作为散行独立裁剪', () => {
    const rows = [
      mkLog({ id: 0, req_id: 101, session_id: '' }),
      mkLog({ id: 0, req_id: 102, session_id: '' }),
      mkLog({ id: 3, session_id: '' }),
    ]
    expect(trimLiveRows(rows, 2).map((r) => r.req_id ?? r.id)).toEqual([101, 102])
  })
})

describe('mergeLiveEvent', () => {
  it('started：新请求插到最前，重复 req_id 不重复插入', () => {
    const prev = [mkLog({ id: 1, req_id: 1 })]
    const next = mergeLiveEvent(prev, mkLog({ id: 0, req_id: 2, model: 'new' }), 'started')
    expect(next.map((r) => r.req_id)).toEqual([2, 1])
    expect(next[0].model).toBe('new')
    // 重复 started（同一 req_id）原样返回同一引用
    expect(mergeLiveEvent(next, mkLog({ id: 0, req_id: 2 }), 'started')).toBe(next)
  })

  it('started：不修改传入行（拷贝后插入）', () => {
    const row = mkLog({ id: 0, req_id: 7 })
    const next = mergeLiveEvent([], row, 'started')
    expect(next[0]).not.toBe(row)
    expect(next[0]).toEqual(row)
  })

  it('completed：按 req_id 原位替换进行中行，不改变顺序', () => {
    const prev = [mkLog({ id: 0, req_id: 5, model: 'running' }), mkLog({ id: 4, req_id: 4 })]
    const next = mergeLiveEvent(prev, mkLog({ id: 99, req_id: 5, model: 'done' }), 'completed')
    expect(next.map((r) => r.id)).toEqual([99, 4])
    expect(next[0].model).toBe('done')
  })

  it('completed：已落库的同一 req_id 重复推送直接忽略', () => {
    const prev = [mkLog({ id: 9, req_id: 9 })]
    expect(mergeLiveEvent(prev, mkLog({ id: 9, req_id: 9 }), 'completed')).toBe(prev)
  })

  it('completed：无匹配进行中行时作为新行插入最前（started 丢失兜底）', () => {
    const prev = [mkLog({ id: 1, req_id: 1 })]
    const next = mergeLiveEvent(prev, mkLog({ id: 2, req_id: 2 }), 'completed')
    expect(next.map((r) => r.req_id)).toEqual([2, 1])
  })

  it('completed 先于 started 到达时不产生重复行', () => {
    let rows: RequestLog[] = []
    rows = mergeLiveEvent(rows, mkLog({ id: 3, req_id: 3, model: 'final' }), 'completed')
    rows = mergeLiveEvent(rows, mkLog({ id: 0, req_id: 3, model: 'stale' }), 'started')
    expect(rows).toHaveLength(1)
    expect(rows[0].model).toBe('final')
  })
})

describe('mergeLiveEvents', () => {
  it('空事件返回原引用', () => {
    const prev = [mkLog({ id: 1 })]
    expect(mergeLiveEvents(prev, [], 200)).toBe(prev)
  })

  it('按到达顺序合并并统一裁剪容量', () => {
    // 到达顺序（旧→新）：old 会话先来，随后同会话的 2、3，最后 3 完成
    const events = [
      { row: mkLog({ id: 1, req_id: 1, session_id: 'old' }), phase: 'started' as const },
      { row: mkLog({ id: 0, req_id: 2, session_id: 's1' }), phase: 'started' as const },
      { row: mkLog({ id: 0, req_id: 3, session_id: 's1' }), phase: 'started' as const },
      { row: mkLog({ id: 33, req_id: 3, session_id: 's1', model: 'done' }), phase: 'completed' as const },
    ]
    // 合并后新→旧为 [3(done), 2, 1]；容量 2 时最旧的 old 整组移除
    const out = mergeLiveEvents([], events, 2)
    expect(out.map((r) => r.req_id)).toEqual([3, 2])
    expect(out[0].model).toBe('done')
  })

  it('批量合并结果与逐条 mergeLiveEvent 一致', () => {
    const events = [
      { row: mkLog({ id: 0, req_id: 1 }), phase: 'started' as const },
      { row: mkLog({ id: 0, req_id: 2 }), phase: 'started' as const },
      { row: mkLog({ id: 11, req_id: 1 }), phase: 'completed' as const },
      { row: mkLog({ id: 12, req_id: 2 }), phase: 'completed' as const },
    ]
    let seq: RequestLog[] = []
    for (const e of events) seq = mergeLiveEvent(seq, e.row, e.phase)
    expect(mergeLiveEvents([], events, 200)).toEqual(seq)
  })
})

/** 构造后端会话聚合视图的最小工厂。 */
function mkView(over: Partial<LiveSession>): LiveSession {
  return {
    key: 'sess:s1',
    session_id: 's1',
    requests: 1,
    prompt_tokens: 0,
    completion_tokens: 0,
    total_tokens: 0,
    cached_tokens: 0,
    total_ms: 0,
    errors: 0,
    first_at: '2026-09-07T10:00:00+08:00',
    last_at: '2026-09-07T10:00:00+08:00',
    user_agent: '',
    key_name: '',
    models: [],
    channels: [],
    protocols: [],
    modes: [],
    ...over,
  }
}

describe('mergeSessionViews', () => {
  it('后端合计原样透传，count 取全量口径而非已加载行数', () => {
    const views = [mkView({ requests: 92, total_tokens: 7_334_300, completion_tokens: 147_900, prompt_tokens: 7_186_400, total_ms: 743_450, errors: 3 })]
    const [g] = mergeSessionViews(views, [])
    expect(g.count).toBe(92)
    expect(g.totalTokens).toBe(7_334_300)
    expect(g.totalMs).toBe(743_450)
    expect(g.errors).toBe(3)
    // 明细未加载，展开时才拉取
    expect(g.loaded).toBe(false)
    expect(g.requests).toHaveLength(0)
  })

  it('叠加未落库的进行中行，但不重复计入已完成行', () => {
    const views = [mkView({ requests: 10, total_tokens: 1000 })]
    const pending = [
      mkLog({ id: 0, req_id: 50, session_id: 's1', total_tokens: 100, completion_tokens: 40 }), // 未落库 → 叠加
      mkLog({ id: 11, req_id: 49, session_id: 's1', total_tokens: 999 }), // 已落库 → 后端已计入，跳过
    ]
    const [g] = mergeSessionViews(views, pending)
    expect(g.count).toBe(11)
    expect(g.totalTokens).toBe(1100)
    expect(g.running).toBe(1)
    expect(g.requests).toHaveLength(1)
    expect(g.requests[0].req_id).toBe(50)
  })

  it('多个未落库进行中行保持新→旧顺序且排在已加载明细之前', () => {
    const persisted = [mkLog({ id: 1, session_id: 's1' }), mkLog({ id: 2, session_id: 's1' })]
    const views = [mkView({ requests: 2 })]
    // pending 为实时流的「新→旧」顺序
    const pending = [
      mkLog({ id: 0, req_id: 32, session_id: 's1' }),
      mkLog({ id: 0, req_id: 31, session_id: 's1' }),
    ]
    const [g] = mergeSessionViews(views, pending, { 'sess:s1': persisted })
    expect(g.requests.map((r) => r.req_id)).toEqual([32, 31, undefined, undefined])
    expect(g.requests.map((r) => r.id)).toEqual([0, 0, 1, 2])
    expect(g.running).toBe(2)
  })

  it('散行：后端带回整行时直接可展示', () => {
    const row = mkLog({ id: 7, total_tokens: 15, upstream_status: 200 })
    const views = [mkView({ key: 'req:7', session_id: '', requests: 1, total_tokens: 15, last_request: row })]
    const [g] = mergeSessionViews(views, [])
    expect(g.loaded).toBe(true)
    expect(g.requests).toEqual([row])
    expect(g.count).toBe(1)
  })

  it('协议/模式透传后端聚合，并叠加未落库行的模式', () => {
    const views = [mkView({ protocols: ['responses'], modes: ['converted'] })]
    const pending = [mkLog({ id: 0, req_id: 5, session_id: 's1', protocol: 'responses', forward_mode: 'native_passthrough' })]
    const [g] = mergeSessionViews(views, pending)
    // 未落库行与后端已聚合的 protocol 相同，去重后仍只有一项
    expect(g.protocols).toEqual(['responses'])
    expect(g.modes).toEqual(['converted', 'native_passthrough'])
  })

  it('新会话（后端尚无记录）由实时行置顶', () => {
    const views = [mkView({ key: 'sess:old', session_id: 'old', requests: 3 })]
    const pending = [mkLog({ id: 0, req_id: 99, session_id: 'fresh', total_tokens: 50 })]
    const out = mergeSessionViews(views, pending)
    expect(out.map((g) => g.sessionId)).toEqual(['fresh', 'old'])
    expect(out[0].count).toBe(1)
    expect(out[0].running).toBe(1)
  })

  it('已加载明细优先于后端 last_request，且不丢全量 count', () => {
    const rows = [mkLog({ id: 1, session_id: 's1' }), mkLog({ id: 2, session_id: 's1' })]
    const views = [mkView({ requests: 2 })]
    const [g] = mergeSessionViews(views, [], { 'sess:s1': rows })
    expect(g.loaded).toBe(true)
    expect(g.requests).toEqual(rows)
    expect(g.count).toBe(2)
  })

  it('maxGroups 限制展示组数（新会话不撑破上限）', () => {
    const views = [mkView({ key: 'sess:a', session_id: 'a' }), mkView({ key: 'sess:b', session_id: 'b' })]
    const pending = [mkLog({ id: 0, req_id: 1, session_id: 'fresh' })]
    expect(mergeSessionViews(views, pending, {}, 2)).toHaveLength(2)
    // 新会话置顶，故被截掉的是最旧的 b
    expect(mergeSessionViews(views, pending, {}, 2).map((g) => g.sessionId)).toEqual(['fresh', 'a'])
  })
})

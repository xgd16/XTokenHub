import { useMemo } from 'react'
import { compactCN } from '../utils/format'
import type { SeriesPoint } from '../utils/transform'
import { useChartPalette } from '../theme'
import EChart from '../charts/EChart'
import {
  echarts,
  escapeHtml,
  MONO_FONT,
  tooltipSkin,
  withAlpha,
  type EChartsCoreOption,
} from '../charts/echarts'

/** y 轴上限取「好看」的整数（1/2/2.5/5/10 × 10^k），避免顶到图边。 */
function niceCeil(v: number): number {
  const pow = Math.pow(10, Math.floor(Math.log10(v)))
  for (const m of [1, 2, 2.5, 5, 10]) {
    if (m * pow >= v - 1e-9) return m * pow
  }
  return 10 * pow
}

/** x 轴标签采样：首尾必含，中间按步长最多再取 3 个。 */
function labelIndices(n: number): Set<number> {
  const idx = new Set<number>([0, n - 1])
  if (n > 2) {
    const step = Math.max(1, Math.ceil((n - 1) / 4))
    for (let i = step; i < n - 1; i += step) idx.add(i)
  }
  return idx
}

function buildOption(data: SeriesPoint[], pal: ReturnType<typeof useChartPalette>): EChartsCoreOption {
  const yMax = niceCeil(Math.max(...data.map((d) => d.requests), 1))
  const labels = labelIndices(data.length)
  const topIdx = data.reduce((best, d, i) => (d.requests > data[best].requests ? i : best), 0)
  const peak = data[topIdx]?.requests > 0 ? data[topIdx] : null

  return {
    // 折线数据随 WS 高频刷新，动画会让曲线一直“追”，保持原来的静态观感。
    animation: false,
    grid: { left: 46, right: 20, top: 22, bottom: 28, containLabel: false },
    xAxis: {
      type: 'category',
      data: data.map((d) => d.date),
      boundaryGap: false,
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: {
        interval: (index: number) => labels.has(index),
        formatter: (value: string) => (value.length >= 10 ? value.slice(5) : value),
        color: pal.textFaint,
        fontSize: 10,
        fontFamily: MONO_FONT,
        margin: 10,
      },
      splitLine: { show: false },
    },
    yAxis: {
      type: 'value',
      min: 0,
      max: yMax,
      interval: yMax / 2,
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: {
        formatter: (value: number) => compactCN(value),
        color: pal.textFaint,
        fontSize: 10,
        fontFamily: MONO_FONT,
        margin: 6,
      },
      splitLine: {
        lineStyle: { color: pal.grid, type: [3, 4] },
      },
    },
    tooltip: {
      trigger: 'axis',
      ...tooltipSkin(pal),
      confine: true,
      // 还原原来的位置：贴图表顶部、以鼠标 x 居中，左右留 8px。
      position: (point: number[], _params: any, _dom: any, _rect: any, size: any) => {
        const [w] = size.contentSize
        const maxX = Math.max(size.viewSize[0] - w - 8, 8)
        return [Math.min(Math.max(point[0] - w / 2, 8), maxX), 26]
      },
      axisPointer: {
        type: 'line',
        lineStyle: { color: pal.cross, type: [3, 3], width: 1 },
      },
      formatter: (params: any) => {
        const row = Array.isArray(params) ? params[0] : params
        const point = row ? data[row.dataIndex] : null
        if (!point) return ''
        const lines = [
          `<div style="color:${pal.textFaint};margin-bottom:3px">${escapeHtml(point.date)}</div>`,
          `<div>请求数 ${compactCN(point.requests)}</div>`,
        ]
        if (point.errors > 0) {
          lines.push(`<div style="color:${pal.coral}">错误 ${compactCN(point.errors)}</div>`)
        }
        if (point.tokens > 0) {
          lines.push(`<div style="color:${pal.textFaint}">tokens ${compactCN(point.tokens)}</div>`)
        }
        return lines.join('')
      },
    },
    series: [
      {
        type: 'line',
        name: '请求数',
        data: data.map((d) => d.requests),
        showSymbol: true,
        symbol: 'circle',
        symbolSize: 5.6,
        itemStyle: { color: pal.accent, borderColor: pal.halo, borderWidth: 1.5 },
        lineStyle: { color: pal.accent, width: 2, cap: 'round', join: 'round' },
        areaStyle: {
          color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
            { offset: 0, color: withAlpha(pal.accent, 0.2) },
            { offset: 1, color: withAlpha(pal.accent, 0.02) },
          ]),
        },
        emphasis: { scale: true },
        markPoint: peak
          ? {
              silent: true,
              symbol: 'circle',
              symbolSize: 1,
              itemStyle: { color: 'transparent' },
              label: {
                show: true,
                position: 'top',
                distance: 6,
                formatter: String(peak.requests),
                color: pal.textSecondary,
                fontSize: 11,
                fontFamily: MONO_FONT,
              },
              data: [{ coord: [topIdx, peak.requests], value: peak.requests }],
            }
          : undefined,
      },
    ],
  }
}

/** 请求趋势折线图（ECharts）：空数据/单点/全 0 均安全渲染。 */
export default function TrendChart({
  data,
  height = 230,
  ariaLabel = '请求趋势',
  loading = false,
}: {
  data: SeriesPoint[]
  height?: number
  ariaLabel?: string
  loading?: boolean
}) {
  const pal = useChartPalette()
  const option = useMemo(() => buildOption(data, pal), [data, pal])

  if (data.length === 0) {
    if (loading) {
      const PAD = { l: 46, r: 20, t: 22, b: 28 }
      const W = 800
      const iw = W - PAD.l - PAD.r
      const ih = Math.max(height - PAD.t - PAD.b, 10)
      // 模拟一条微波动的骨架折线（骨架不是图表本体，保留原 SVG 动画）
      const pts = Array.from({ length: 12 }, (_, i) => {
        const x = PAD.l + (i / 11) * iw
        const y = PAD.t + ih * (0.35 + 0.3 * Math.sin(i * 0.8))
        return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)} ${y.toFixed(1)}`
      }).join(' ')
      return (
        <svg viewBox={`0 0 ${W} ${height}`} width="100%" style={{ display: 'block' }}>
          {[0, 0.5, 1].map((f) => (
            <line key={f} x1={PAD.l} x2={W - PAD.r} y1={PAD.t + ih * f} y2={PAD.t + ih * f} stroke="var(--track-bg)" strokeDasharray="3 4" />
          ))}
          <path d={pts} fill="none" stroke="var(--track-bg)" strokeWidth="2" strokeLinecap="round" className="skeleton-chart-wave" />
        </svg>
      )
    }
    return (
      <div style={{ height, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-faint)', fontSize: 12 }}>
        暂无数据
      </div>
    )
  }

  const allZero = data.every((d) => d.requests === 0)

  return (
    <div style={{ position: 'relative' }}>
      <EChart option={option} height={height} ariaLabel={ariaLabel} />
      {allZero && (
        <div
          style={{
            position: 'absolute',
            inset: 0,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: 'var(--text-faint)',
            fontSize: 12,
            pointerEvents: 'none',
          }}
        >
          暂无数据 · 网关请求产生后实时出现
        </div>
      )}
    </div>
  )
}

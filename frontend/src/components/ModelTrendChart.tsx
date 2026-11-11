import { useMemo } from 'react'
import { compactCN } from '../utils/format'
import { MODEL_COLORS, type ModelTrendData } from '../utils/transform'
import { useChartPalette, type ChartPalette } from '../theme'
import EChart from '../charts/EChart'
import {
  escapeHtml,
  MONO_FONT,
  tooltipSkin,
  type EChartsCoreOption,
} from '../charts/echarts'

/** y 轴上限取「好看」的整数（1/2/2.5/5/10 × 10^k）。 */
function niceCeil(v: number): number {
  if (v <= 0) return 1
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

function dateLabel(value: string): string {
  if (value.length < 10) return value
  return `${Number(value.slice(5, 7))}月${Number(value.slice(8, 10))}日`
}

/** 日期 -> 系列配色：与 toDonut 一致按数组序号取 MODEL_COLORS。 */
function seriesColor(index: number): string {
  return MODEL_COLORS[index % MODEL_COLORS.length]
}

function buildOption(data: ModelTrendData, pal: ChartPalette): EChartsCoreOption {
  const maxVal = Math.max(...data.series.flatMap((s) => s.values), 1)
  const yMax = niceCeil(maxVal)
  const labels = labelIndices(data.dates.length)
  const rows = data.series.map((s, si) => ({
    name: s.name,
    color: seriesColor(si),
    values: s.values,
  }))

  return {
    animation: false,
    // 图例是自绘 HTML（8px 圆点 + 等宽字体），不启用 ECharts 自带 legend。
    grid: { left: 52, right: 20, top: 18, bottom: 28, containLabel: false },
    xAxis: {
      type: 'category',
      data: data.dates,
      boundaryGap: false,
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: {
        interval: (index: number) => labels.has(index),
        formatter: (value: string) => dateLabel(value),
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
        return [Math.min(Math.max(point[0] - w / 2, 8), maxX), 28]
      },
      axisPointer: {
        type: 'line',
        lineStyle: { color: pal.cross, type: [3, 3], width: 1 },
      },
      formatter: (params: any) => {
        const list = Array.isArray(params) ? params : [params]
        const index = list[0]?.dataIndex
        if (index == null || !data.dates[index]) return ''
        const hoverRows = rows
          .map((r) => ({ name: r.name, color: r.color, value: r.values[index] }))
          .filter((r) => r.value > 0)
          .sort((a, b) => b.value - a.value)
        if (hoverRows.length === 0) return ''
        const head = `<div style="color:${pal.textFaint};margin-bottom:3px">${escapeHtml(dateLabel(data.dates[index]))}</div>`
        const body = hoverRows
          .map(
            (r) =>
              `<div style="display:flex;align-items:center;gap:6px">` +
              `<span style="width:7px;height:7px;border-radius:50%;background:${r.color};flex-shrink:0"></span>` +
              `<span style="max-width:150px;overflow:hidden;text-overflow:ellipsis">${escapeHtml(r.name)}</span>` +
              `<span style="margin-left:auto;padding-left:10px">${compactCN(r.value)}</span>` +
              `</div>`,
          )
          .join('')
        return head + body
      },
    },
    series: rows.map((r) => ({
      type: 'line',
      name: r.name,
      data: r.values,
      smooth: 0.4,
      showSymbol: false,
      lineStyle: { color: r.color, width: 2, cap: 'round', join: 'round' },
      itemStyle: { color: r.color },
      emphasis: { focus: 'series' },
    })),
  }
}

interface Props {
  data: ModelTrendData
  height?: number
  ariaLabel?: string
  loading?: boolean
}

/** 多模型每日 token 趋势图（ECharts）：平滑曲线 + 自绘 HTML 图例 + 悬停明细。 */
export default function ModelTrendChart({
  data,
  height = 260,
  ariaLabel = '每日 Token 趋势图',
  loading = false,
}: Props) {
  const pal = useChartPalette()
  const option = useMemo(() => buildOption(data, pal), [data, pal])
  const maxVal = Math.max(...data.series.flatMap((s) => s.values), 1)
  const empty = data.series.length === 0 || maxVal <= 1

  // 加载中且无数据时显示骨架
  if (loading && empty) {
    const W = 800
    const PAD = { l: 52, r: 20, t: 18, b: 28 }
    const iw = W - PAD.l - PAD.r
    const ih = Math.max(height - PAD.t - PAD.b, 10)
    const waveColors = ['var(--track-bg)', 'rgba(94,128,148,0.08)']
    return (
      <div>
        <div style={{ display: 'flex', gap: 12, marginBottom: 8 }}>
          {[56, 72, 48].map((w, i) => (
            <span key={i} className="skeleton-bar" style={{ width: w, height: 12, animationDelay: `${i * 0.1}s` }} />
          ))}
        </div>
        <svg viewBox={`0 0 ${W} ${height}`} width="100%" style={{ display: 'block' }}>
          {[0, 0.5, 1].map((f) => (
            <line key={f} x1={PAD.l} x2={W - PAD.r} y1={PAD.t + ih * f} y2={PAD.t + ih * f} stroke="var(--track-bg)" strokeDasharray="3 4" />
          ))}
          {[0.3, 0.55, 0.75].map((base, si) => {
            const pts = Array.from({ length: 10 }, (_, i) => {
              const x = PAD.l + (i / 9) * iw
              const y = PAD.t + ih * (base + 0.12 * Math.sin(i * 0.9 + si * 2))
              return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)} ${y.toFixed(1)}`
            }).join(' ')
            return <path key={si} d={pts} fill="none" stroke={waveColors[si % 2]} strokeWidth="2" strokeLinecap="round" className="skeleton-chart-wave" style={{ animationDelay: `${si * 0.2}s` }} />
          })}
        </svg>
      </div>
    )
  }

  return (
    <div>
      {/* 图例 */}
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px 16px', marginBottom: 8 }}>
        {data.series.map((s, si) => (
          <span key={s.name} style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 12 }}>
            <span style={{ width: 8, height: 8, borderRadius: '50%', background: seriesColor(si), flexShrink: 0 }} />
            <span className="mono" style={{ color: 'var(--text-secondary)' }}>{s.name}</span>
          </span>
        ))}
      </div>

      <div style={{ position: 'relative' }}>
        <EChart option={option} height={height} ariaLabel={ariaLabel} />
        {empty && (
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
    </div>
  )
}

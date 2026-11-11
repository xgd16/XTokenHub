import { useEffect, useMemo, useRef, useState } from 'react'
import { compactCN } from '../utils/format'
import type { HeatmapData, HeatMode } from '../utils/transform'
import { useChartPalette, type ChartPalette } from '../theme'
import EChart from '../charts/EChart'
import {
  escapeHtml,
  MONO_FONT,
  tooltipSkin,
  type EChartsCoreOption,
} from '../charts/echarts'
import {
  computeHeatmapLayout,
  HEATMAP_LABEL_GAP,
  HEATMAP_PAD,
} from '../charts/heatmapLayout'

interface Props {
  data: HeatmapData
  /** weekly 模式悬浮文案带「当周」。 */
  mode?: HeatMode
  ariaLabel?: string
  loading?: boolean
}

function cellText(date: string, value: number, mode: HeatMode): string {
  const m = Number(date.slice(5, 7))
  const d = Number(date.slice(8, 10))
  const suffix = mode === 'weekly' ? ' 当周' : ''
  return `${m}月${d}日${suffix} · ${compactCN(value)} tokens`
}

function buildOption(
  data: HeatmapData,
  mode: HeatMode,
  pal: ChartPalette,
  geo: ReturnType<typeof computeHeatmapLayout>,
): EChartsCoreOption {
  const cols = data.columns.length
  const rows = Math.max(...data.columns.map((c) => c.cells.length), 1)
  const monthByIndex = new Map(data.monthLabels.map((m) => [m.index, m.label]))
  // 类目带里放不下元素本身：band = 元素 + 间隙，反解出元素尺寸才能保证像素级间隙。
  const symbolW = Math.max(geo.cell - geo.gap / cols, 2)
  const symbolH = Math.max(geo.cell - geo.gap / rows, 2)
  const points = data.columns.flatMap((col, ci) =>
    col.cells.map((c, ri) => ({
      value: [ci, ri],
      date: c.date,
      count: c.value,
      level: c.level,
    })),
  )

  return {
    animation: false,
    grid: {
      left: HEATMAP_PAD.l,
      right: HEATMAP_PAD.r,
      top: geo.padT,
      bottom: HEATMAP_PAD.b,
      containLabel: false,
    },
    xAxis: {
      type: 'category',
      data: data.columns.map((_, i) => i),
      boundaryGap: true,
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: {
        interval: 0,
        hideOverlap: false,
        margin: HEATMAP_LABEL_GAP / 2,
        align: 'left',
        formatter: (value: number) => monthByIndex.get(Number(value)) ?? '',
        color: pal.textFaint,
        fontSize: geo.labelFs,
        fontFamily: MONO_FONT,
      },
      splitLine: { show: false },
    },
    yAxis: {
      type: 'category',
      data: Array.from({ length: rows }, (_, i) => i),
      boundaryGap: true,
      show: false,
      splitLine: { show: false },
    },
    tooltip: {
      trigger: 'item',
      ...tooltipSkin(pal),
      confine: false,
      appendTo: () => document.body,
      // 还原原来的位置：浮层底边在光标上方 10px、水平居中。
      position: (point: number[], _params: any, _dom: any, _rect: any, size: any) => {
        const [w, h] = size.contentSize
        return [point[0] - w / 2, point[1] - h - 10]
      },
      formatter: (params: any) => {
        const item = params?.data
        if (!item) return ''
        return escapeHtml(cellText(item.date, item.count, mode))
      },
    },
    series: [
      {
        type: 'scatter',
        coordinateSystem: 'cartesian2d',
        symbol: 'roundRect',
        symbolSize: [symbolW, symbolH],
        data: points.map((p) => ({
          ...p,
          itemStyle: { color: pal.heat[p.level] },
        })),
        emphasis: {
          scale: false,
          itemStyle: {
            borderColor: pal.hoverStroke,
            borderWidth: geo.cell > 14 ? 2 : 1,
          },
        },
      },
    ],
  }
}

/** GitHub 风格 Token 活动热力图（ECharts）：格子随容器宽高自适应，悬浮显示明细。 */
export default function TokenHeatmap({
  data,
  mode = 'daily',
  ariaLabel = 'Token 活动热力图',
  loading = false,
}: Props) {
  const pal = useChartPalette()
  const boxRef = useRef<HTMLDivElement | null>(null)
  const [box, setBox] = useState<{ w: number; h: number } | null>(null)

  useEffect(() => {
    const el = boxRef.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver((entries) => {
      const r = entries[0].contentRect
      setBox({ w: r.width, h: r.height })
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const geo = useMemo(() => {
    const cols = data.columns.length
    const rows = Math.max(...data.columns.map((c) => c.cells.length), 1)
    return computeHeatmapLayout(cols, rows, box?.w ?? null, box?.h ?? null)
  }, [data, box])

  const option = useMemo(() => buildOption(data, mode, pal, geo), [data, mode, pal, geo])

  if (data.columns.length === 0) {
    if (loading) {
      // 7 列 × 5 行骨架网格
      const cell = 10
      const gap = 2
      const cols = 20
      const rows = 7
      const w = cols * (cell + gap) - gap
      const h = rows * (cell + gap) - gap
      return (
        <div style={{ height: 160, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: `${gap}px`, width: w, height: h }}>
            {Array.from({ length: cols * rows }, (_, i) => (
              <span
                key={i}
                className="skeleton-heatmap-cell"
                style={{ width: cell, height: cell, animationDelay: `${(i % cols) * 0.03}s` }}
              />
            ))}
          </div>
        </div>
      )
    }
    return (
      <div style={{ height: 160, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-faint)', fontSize: 12 }}>
        暂无数据
      </div>
    )
  }

  return (
    <div
      ref={boxRef}
      style={{ flex: 1, minHeight: geo.boxMinH, overflowX: 'auto', display: 'flex', alignItems: 'center' }}
    >
      <EChart
        option={option}
        width={geo.W}
        height={geo.H}
        ariaLabel={ariaLabel}
        style={{ margin: '0 auto', flexShrink: 0 }}
      />
    </div>
  )
}

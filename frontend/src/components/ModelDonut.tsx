import { useCallback, useMemo, useRef, useState } from 'react'
import { compactCN } from '../utils/format'
import type { DonutSlice } from '../utils/transform'
import { useChartPalette, type ChartPalette } from '../theme'
import EChart, { type EChartEventHandler } from '../charts/EChart'
import { type EChartsCoreOption, type EChartsType } from '../charts/echarts'

interface Props {
  slices: DonutSlice[]
  total: number
  size?: number
  loading?: boolean
}

/** 环图内/外径占半径的比例：对应原 SVG r=70 / stroke=26（57~83）。 */
const RADIUS: [string, string] = ['57%', '83%']
const MAIN_SERIES = 1

function buildOption(slices: DonutSlice[], pal: ChartPalette): EChartsCoreOption {
  return {
    tooltip: { show: false },
    // 首次不播放入场动画（原实现是静态环），数据更新仍保留 500ms 过渡。
    animationDuration: 0,
    animationDurationUpdate: 500,
    series: [
      {
        type: 'pie',
        radius: RADIUS,
        center: ['50%', '50%'],
        silent: true,
        label: { show: false },
        labelLine: { show: false },
        emphasis: { disabled: true },
        data: [{ value: 1, itemStyle: { color: pal.track } }],
      },
      {
        type: 'pie',
        radius: RADIUS,
        center: ['50%', '50%'],
        startAngle: 90,
        clockwise: true,
        avoidLabelOverlap: false,
        selectedMode: false,
        label: { show: false },
        labelLine: { show: false },
        itemStyle: { borderWidth: 0 },
        emphasis: { scale: true, scaleSize: 6, focus: 'self' },
        blur: { itemStyle: { opacity: 0.35 } },
        data: slices.map((s) => ({
          name: s.name,
          value: s.value,
          itemStyle: { color: s.color },
        })),
      },
    ],
  }
}

/** 环图（ECharts pie）+ 右侧图例：悬浮扇区加粗、中心切换占比、图例同步高亮。 */
export default function ModelDonut({ slices, total, size = 220, loading = false }: Props) {
  const pal = useChartPalette()
  const chartRef = useRef<EChartsType | null>(null)
  const [hover, setHover] = useState<string | null>(null)
  const option = useMemo(() => buildOption(slices, pal), [slices, pal])
  const hovered = slices.find((s) => s.name === hover)

  const onReady = useCallback((chart: EChartsType) => {
    chartRef.current = chart
  }, [])

  const events = useMemo<Record<string, EChartEventHandler>>(
    () => ({
      mouseover: (params: any) => {
        if (params.seriesIndex === MAIN_SERIES && typeof params.name === 'string') {
          setHover(params.name)
        }
      },
      globalout: () => setHover(null),
    }),
    [],
  )

  /** 图例悬停：同步中心读数 + 用 dispatchAction 让扇区高亮/其余变淡。 */
  const highlight = (name: string | null) => {
    setHover(name)
    const chart = chartRef.current
    if (!chart) return
    chart.dispatchAction({ type: 'downplay', seriesIndex: MAIN_SERIES })
    if (!name) return
    const dataIndex = slices.findIndex((s) => s.name === name)
    if (dataIndex >= 0) {
      chart.dispatchAction({ type: 'highlight', seriesIndex: MAIN_SERIES, dataIndex })
    }
  }

  // 加载中且无数据时显示骨架
  if (loading && slices.length === 0) {
    const R = 70
    const C = 2 * Math.PI * R
    return (
      <div style={{ display: 'flex', alignItems: 'center', gap: 20, flexWrap: 'wrap' }}>
        <div style={{ position: 'relative', width: size, height: size, flexShrink: 0, margin: '0 auto' }}>
          <svg viewBox="0 0 200 200" width={size} height={size}>
            <circle cx="100" cy="100" r={R} fill="none" stroke="var(--track-bg)" strokeWidth="26" className="skeleton-donut-ring" />
            <circle cx="100" cy="100" r={R} fill="none" stroke="var(--track-bg)" strokeWidth="26" strokeDasharray={`${C * 0.35} ${C * 0.65}`} strokeDashoffset={0} className="skeleton-donut-ring" style={{ animationDelay: '0.2s' }} opacity={0.5} />
          </svg>
        </div>
        <div style={{ flex: 1, minWidth: 240 }}>
          {/* 5 行 = 实际最多渲染的扇区数（top 5），行数与真实一致，加载完成时下方不跳位 */}
          {[0, 1, 2, 3, 4].map((i) => (
            <div key={i} style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '6px', borderBottom: '1px solid var(--border-faint)' }}>
              <span className="skeleton-bar" style={{ width: 9, height: 9, borderRadius: '50%', animationDelay: `${i * 0.1}s` }} />
              <span className="skeleton-bar" style={{ width: `${50 + i * 8}%`, height: 13, animationDelay: `${i * 0.1 + 0.05}s` }} />
              <span className="skeleton-bar" style={{ width: 36, height: 13, marginLeft: 'auto', animationDelay: `${i * 0.1 + 0.1}s` }} />
            </div>
          ))}
        </div>
      </div>
    )
  }

  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 20, flexWrap: 'wrap' }}>
      <div style={{ position: 'relative', width: size, height: size, flexShrink: 0, margin: '0 auto' }}>
        <EChart
          option={option}
          width={size}
          height={size}
          ariaLabel="模型用量占比"
          events={events}
          onReady={onReady}
        />
        <div
          style={{
            position: 'absolute',
            inset: 0,
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            pointerEvents: 'none',
            transition: 'opacity 0.15s ease',
          }}
        >
          {hovered ? (
            <>
              <span className="mono" style={{ fontSize: 12, color: 'var(--text-faint)', maxWidth: size * 0.7, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {hovered.name}
              </span>
              <span className="mono" style={{ fontSize: 22, fontWeight: 600, color: hovered.color }}>{hovered.percent}%</span>
              <span className="mono" style={{ fontSize: 12, color: 'var(--text-faint)' }}>{compactCN(hovered.value)} tokens</span>
            </>
          ) : (
            <>
              <span className="mono" style={{ fontSize: 22, fontWeight: 600 }}>{compactCN(total)}</span>
              <span style={{ fontSize: 12, color: 'var(--text-faint)' }}>tokens</span>
            </>
          )}
        </div>
      </div>

      <div style={{ flex: 1, minWidth: 240 }}>
        {slices.map((s) => (
          <div
            key={s.name}
            onMouseEnter={() => highlight(s.name)}
            onMouseLeave={() => highlight(null)}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 10,
              padding: '6px 6px',
              borderRadius: 6,
              background: hover === s.name ? pal.hoverBg : 'transparent',
              borderBottom: hover === s.name ? 'none' : '1px solid var(--border-faint)',
              cursor: 'default',
              transition: 'background 0.15s ease',
            }}
          >
            <span style={{ width: 9, height: 9, borderRadius: '50%', background: s.color, flexShrink: 0 }} />
            <span className="mono" style={{ fontSize: 13, color: 'var(--text-primary)', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {s.name}
            </span>
            <span className="mono" style={{ fontSize: 13, width: 52, textAlign: 'right' }}>{s.percent}%</span>
            <span className="mono" style={{ fontSize: 12, color: 'var(--text-faint)', width: 84, textAlign: 'right' }}>
              {compactCN(s.value)} tokens
            </span>
          </div>
        ))}
        {slices.length === 0 && (
          <div style={{ color: 'var(--text-faint)', padding: '24px 0', textAlign: 'center', fontSize: 12 }}>
            暂无数据 · 网关请求产生后实时出现
          </div>
        )}
      </div>
    </div>
  )
}

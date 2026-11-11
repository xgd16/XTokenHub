import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { CSSProperties } from 'react'
import {
  echarts,
  prefersReducedMotion,
  type EChartsCoreOption,
  type EChartsType,
} from './echarts'

export type EChartEventHandler = (params: any) => void

interface Props {
  option: EChartsCoreOption
  /** CSS 像素高度（不再是 800 宽 viewBox 的缩放高度）。 */
  height: number
  /** 固定像素宽度：热力图横向滚动时使用；不传则跟随容器宽度。 */
  width?: number
  ariaLabel: string
  className?: string
  style?: CSSProperties
  /** ECharts 事件（mouseover / globalout / ...），对象身份变化时重新绑定。 */
  events?: Record<string, EChartEventHandler>
  /** 实例就绪回调（需要 dispatchAction 的组件用，例如环图图例联动）。 */
  onReady?: (chart: EChartsType) => void
}

/**
 * ECharts 的 React 轻封装：负责 init / resize / dispose 与主题 option 更新。
 * 不引 echarts-for-react，避免额外依赖与 React 19 peer 兼容风险。
 */
export default function EChart({
  option,
  height,
  width,
  ariaLabel,
  className,
  style,
  events,
  onReady,
}: Props) {
  const hostRef = useRef<HTMLDivElement | null>(null)
  const chartRef = useRef<EChartsType | null>(null)
  const [measuredWidth, setMeasuredWidth] = useState(0)
  const onReadyRef = useRef(onReady)
  useLayoutEffect(() => {
    onReadyRef.current = onReady
  })

  // 只 init 一次；宽高变化走 resize，主题/数据变化走 setOption。
  useLayoutEffect(() => {
    const host = hostRef.current
    if (!host) return
    const chart = echarts.init(host, undefined, {
      renderer: 'canvas',
      width: width ?? (host.clientWidth || 800),
      height,
    })
    chartRef.current = chart
    onReadyRef.current?.(chart)
    return () => {
      chart.dispose()
      chartRef.current = null
    }
    // width/height 的后续变化由下方 effect 处理，这里刻意只在挂载时执行。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    const chart = chartRef.current
    if (!chart) return
    const resolved = prefersReducedMotion() ? { ...option, animation: false } : option
    chart.setOption(resolved, { notMerge: true })
  }, [option])

  useEffect(() => {
    const chart = chartRef.current
    if (!chart) return
    chart.resize({ width: width ?? (measuredWidth || 800), height })
  }, [width, height, measuredWidth])

  useLayoutEffect(() => {
    if (width != null) return
    const host = hostRef.current
    if (!host) return
    const readWidth = () => host.clientWidth || 800
    setMeasuredWidth(readWidth())
    if (typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(() => setMeasuredWidth(readWidth()))
    ro.observe(host)
    return () => ro.disconnect()
  }, [width])

  useEffect(() => {
    const chart = chartRef.current
    if (!chart || !events) return
    const entries = Object.entries(events)
    for (const [name, handler] of entries) chart.on(name, handler)
    return () => {
      if (chart.isDisposed()) return
      for (const [name, handler] of entries) chart.off(name, handler)
    }
  }, [events])

  return (
    <div
      ref={hostRef}
      role="img"
      aria-label={ariaLabel}
      data-echart=""
      className={className}
      style={{ width: width ?? '100%', height, ...style }}
    />
  )
}

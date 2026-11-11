/**
 * ECharts 按需注册：只打包 Dashboard 用到的折线 / 环图 / 散点（日历热力图）
 * 与网格、tooltip、坐标轴指示器、峰值标注，避免整包进入前端产物。
 */
import * as echarts from 'echarts/core'
import { LineChart, PieChart, ScatterChart } from 'echarts/charts'
import {
  AxisPointerComponent,
  GridComponent,
  MarkPointComponent,
  TooltipComponent,
} from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import type { ChartPalette } from '../theme'

echarts.use([
  LineChart,
  PieChart,
  ScatterChart,
  GridComponent,
  TooltipComponent,
  AxisPointerComponent,
  MarkPointComponent,
  CanvasRenderer,
])

export { echarts }
export type { EChartsCoreOption, EChartsType } from 'echarts/core'

/** 图表里的等宽字体：ECharts 画布读不到 CSS 变量，必须给出具体字体栈。 */
export const MONO_FONT = "'IBM Plex Mono', 'SF Mono', 'PingFang SC', monospace"

/** 是否应关闭动画（系统「减弱动态效果」/ 测试环境）。 */
export function prefersReducedMotion(): boolean {
  if (import.meta.env.MODE === 'test') return true
  if (typeof matchMedia === 'undefined') return false
  try {
    return matchMedia('(prefers-reduced-motion: reduce)').matches
  } catch {
    return false
  }
}

/** 给十六进制色值追加透明度（palette 里 rgba 原样返回）。 */
export function withAlpha(color: string, alpha: number): string {
  const hex = color.trim()
  if (!hex.startsWith('#')) return color
  const raw = hex.slice(1)
  const full = raw.length === 3 ? raw.split('').map((c) => c + c).join('') : raw
  if (full.length !== 6) return color
  const r = Number.parseInt(full.slice(0, 2), 16)
  const g = Number.parseInt(full.slice(2, 4), 16)
  const b = Number.parseInt(full.slice(4, 6), 16)
  return `rgba(${r}, ${g}, ${b}, ${alpha})`
}

/** tooltip / 图例里拼 HTML 前转义，避免模型名等数据注入标签。 */
export function escapeHtml(value: unknown): string {
  return String(value).replace(/[&<>"']/g, (ch) => {
    switch (ch) {
      case '&':
        return '&amp;'
      case '<':
        return '&lt;'
      case '>':
        return '&gt;'
      case '"':
        return '&quot;'
      default:
        return '&#39;'
    }
  })
}

/** 统一的 HTML tooltip 皮肤，与原来的 ChartTooltip 观感一致。 */
export function tooltipSkin(pal: ChartPalette) {
  return {
    backgroundColor: pal.tooltipBg,
    borderColor: pal.tooltipBorder,
    borderWidth: 1,
    padding: [6, 10] as [number, number],
    textStyle: {
      color: pal.textPrimary,
      fontSize: 12,
      fontFamily: MONO_FONT,
    },
    extraCssText: `border-radius:6px;box-shadow:${pal.tooltipShadow};white-space:nowrap;`,
    transitionDuration: 0,
  }
}

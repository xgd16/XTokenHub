import { useSyncExternalStore } from 'react'

export type ThemeMode = 'dark' | 'light'

const STORAGE_KEY = 'xtokenhub-theme'
const META_THEME_COLOR: Record<ThemeMode, string> = { dark: '#070b10', light: '#f3f5f7' }

function detectInitial(): ThemeMode {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved === 'dark' || saved === 'light') return saved
  } catch {
    // 隐私模式等存取失败，退回跟随系统
  }
  try {
    if (typeof matchMedia !== 'undefined' && matchMedia('(prefers-color-scheme: light)').matches) {
      return 'light'
    }
  } catch {
    // matchMedia 不可用时默认深色
  }
  return 'dark'
}

let mode: ThemeMode = detectInitial()
const listeners = new Set<() => void>()

/** 把当前主题写到 <html data-theme>、color-scheme 与 <meta name=theme-color>（切换即时生效）。 */
export function applyTheme(m: ThemeMode): void {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  root.dataset.theme = m
  root.style.colorScheme = m
  let meta = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')
  if (!meta) {
    meta = document.createElement('meta')
    meta.name = 'theme-color'
    document.head.appendChild(meta)
  }
  meta.content = META_THEME_COLOR[m]
}

/** 当前主题模式。 */
export function getThemeMode(): ThemeMode {
  return mode
}

/** 切换主题：持久化到 localStorage，并同步 <html> 属性。 */
export function setThemeMode(m: ThemeMode): void {
  if (m === mode) return
  mode = m
  try {
    localStorage.setItem(STORAGE_KEY, m)
  } catch {
    // 忽略持久化失败
  }
  applyTheme(m)
  listeners.forEach((l) => l())
}

/** 深浅一键互换。 */
export function toggleThemeMode(): void {
  setThemeMode(mode === 'dark' ? 'light' : 'dark')
}

function subscribe(cb: () => void): () => void {
  listeners.add(cb)
  return () => {
    listeners.delete(cb)
  }
}

/** 主题模式 hook：切换时自动重渲染（图表/组件据此取色）。 */
export function useThemeMode(): ThemeMode {
  return useSyncExternalStore(subscribe, getThemeMode, getThemeMode)
}

// 用户未手动选择时跟随系统切换
if (typeof matchMedia !== 'undefined') {
  try {
    matchMedia('(prefers-color-scheme: light)').addEventListener?.('change', (e) => {
      try {
        if (localStorage.getItem(STORAGE_KEY)) return
      } catch {
        // 忽略
      }
      setThemeMode(e.matches ? 'light' : 'dark')
    })
  } catch {
    // 老浏览器无 addEventListener
  }
}

// ============ 图表色板 ============
// SVG 展示属性（fill/stroke/stop-color）对 CSS 变量支持参差，图表取色走 React 色板对象。

export interface ChartPalette {
  accent: string
  coral: string
  textSecondary: string
  textFaint: string
  grid: string
  cross: string
  /** 数据点描边（与面板底色一致的光晕）。 */
  halo: string
  /** 热力图悬浮描边。 */
  hoverStroke: string
  /** 环形图底环 / 进度条底槽。 */
  track: string
  hoverBg: string
  tooltipBg: string
  tooltipBorder: string
  tooltipShadow: string
  /** 热力图 0~4 档色阶。 */
  heat: [string, string, string, string, string]
}

const DARK_PALETTE: ChartPalette = {
  accent: '#2fe0a4',
  coral: '#ef6b6b',
  textSecondary: '#8b98a9',
  textFaint: '#5a6675',
  grid: 'rgba(94, 128, 148, 0.16)',
  cross: 'rgba(139, 152, 169, 0.45)',
  halo: 'rgba(13, 18, 24, 0.9)',
  hoverStroke: '#c9d7e2',
  track: 'rgba(94, 128, 148, 0.14)',
  hoverBg: 'rgba(94, 128, 148, 0.12)',
  tooltipBg: 'rgba(13, 18, 24, 0.96)',
  tooltipBorder: 'rgba(94, 128, 148, 0.35)',
  tooltipShadow: '0 4px 16px rgba(0, 0, 0, 0.35)',
  heat: ['rgba(94, 128, 148, 0.14)', 'rgba(47, 224, 164, 0.28)', 'rgba(47, 224, 164, 0.5)', 'rgba(47, 224, 164, 0.72)', '#2fe0a4'],
}

const LIGHT_PALETTE: ChartPalette = {
  accent: '#0c9d74',
  coral: '#d9504f',
  textSecondary: '#5a6a7a',
  textFaint: '#8b98a6',
  grid: 'rgba(45, 74, 96, 0.14)',
  cross: 'rgba(90, 106, 122, 0.45)',
  halo: '#ffffff',
  hoverStroke: '#1a2530',
  track: 'rgba(45, 74, 96, 0.14)',
  hoverBg: 'rgba(45, 74, 96, 0.08)',
  tooltipBg: 'rgba(255, 255, 255, 0.97)',
  tooltipBorder: 'rgba(45, 74, 96, 0.2)',
  tooltipShadow: '0 4px 16px rgba(23, 46, 66, 0.16)',
  heat: ['rgba(45, 74, 96, 0.12)', 'rgba(12, 157, 116, 0.25)', 'rgba(12, 157, 116, 0.48)', 'rgba(12, 157, 116, 0.72)', '#0c9d74'],
}

/** 指定模式的图表色板。 */
export function chartPalette(m: ThemeMode = mode): ChartPalette {
  return m === 'dark' ? DARK_PALETTE : LIGHT_PALETTE
}

/** 图表色板 hook：主题切换时返回新色板并触发重渲染。 */
export function useChartPalette(): ChartPalette {
  return chartPalette(useThemeMode())
}

// 模块加载即应用（配合 index.html 内联脚本，双保险避免首帧错主题）
if (typeof document !== 'undefined') {
  applyTheme(mode)
}

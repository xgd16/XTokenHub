import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import {
  applyTheme,
  chartPalette,
  getThemeMode,
  setThemeMode,
  toggleThemeMode,
  useChartPalette,
  useThemeMode,
} from './theme'

function reset() {
  // 包 act：可能通知仍未卸载的 hook 订阅者，避免 React act 告警
  act(() => {
    localStorage.removeItem('xtokenhub-theme')
    setThemeMode('dark')
    localStorage.removeItem('xtokenhub-theme')
  })
}

describe('theme 模式存储', () => {
  beforeEach(() => {
    reset()
  })
  afterEach(() => {
    reset()
  })

  it('默认深色（无存储且系统非浅色）', () => {
    expect(getThemeMode()).toBe('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
  })

  it('setThemeMode 同步 <html data-theme>、color-scheme 与 meta theme-color', () => {
    setThemeMode('light')
    expect(getThemeMode()).toBe('light')
    expect(localStorage.getItem('xtokenhub-theme')).toBe('light')
    expect(document.documentElement.dataset.theme).toBe('light')
    expect(document.documentElement.style.colorScheme).toBe('light')
    expect(document.querySelector('meta[name="theme-color"]')?.getAttribute('content')).toBe('#f3f5f7')

    setThemeMode('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(document.querySelector('meta[name="theme-color"]')?.getAttribute('content')).toBe('#070b10')
  })

  it('setThemeMode 相同值时状态与存储保持不变', () => {
    setThemeMode('light')
    setThemeMode('light') // 重复设置无副作用
    expect(getThemeMode()).toBe('light')
    expect(document.documentElement.dataset.theme).toBe('light')
  })

  it('toggleThemeMode 深浅互换并持久化', () => {
    setThemeMode('dark')
    toggleThemeMode()
    expect(getThemeMode()).toBe('light')
    expect(localStorage.getItem('xtokenhub-theme')).toBe('light')
    toggleThemeMode()
    expect(getThemeMode()).toBe('dark')
    expect(localStorage.getItem('xtokenhub-theme')).toBe('dark')
  })

  it('applyTheme 只改 DOM 属性，不写 localStorage', () => {
    localStorage.removeItem('xtokenhub-theme')
    applyTheme('light')
    expect(document.documentElement.dataset.theme).toBe('light')
    expect(localStorage.getItem('xtokenhub-theme')).toBeNull()
  })
})

describe('图表色板', () => {
  beforeEach(() => {
    reset()
  })
  afterEach(() => {
    reset()
  })

  it('深浅两套色板关键字段不同且完整', () => {
    const dark = chartPalette('dark')
    const light = chartPalette('light')
    expect(dark.accent).not.toBe(light.accent)
    expect(dark.tooltipBg).not.toBe(light.tooltipBg)
    for (const key of ['accent', 'grid', 'halo', 'tooltipBg', 'hoverStroke'] as const) {
      expect(dark[key]).toBeTruthy()
      expect(light[key]).toBeTruthy()
    }
    expect(dark.heat).toHaveLength(5)
    expect(light.heat).toHaveLength(5)
  })

  it('useChartPalette 跟随主题切换重渲染', () => {
    const { result } = renderHook(() => useChartPalette())
    expect(result.current.accent).toBe(chartPalette('dark').accent)
    act(() => {
      setThemeMode('light')
    })
    expect(result.current.accent).toBe(chartPalette('light').accent)
  })

  it('useThemeMode 跟随切换', () => {
    const { result } = renderHook(() => useThemeMode())
    expect(result.current).toBe('dark')
    act(() => {
      toggleThemeMode()
    })
    expect(result.current).toBe('light')
  })
})

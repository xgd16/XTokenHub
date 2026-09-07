import { fireEvent, render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import TrendChart from './TrendChart'
import type { SeriesPoint } from '../utils/transform'

const pts: SeriesPoint[] = [
  { date: '2026-09-01', requests: 0, errors: 0, tokens: 0 },
  { date: '2026-09-02', requests: 14, errors: 0, tokens: 0 },
  { date: '2026-09-03', requests: 3, errors: 1, tokens: 0 },
]

describe('TrendChart', () => {
  it('渲染折线、数据点与峰值标注', () => {
    const { container } = render(<TrendChart data={pts} />)
    expect(container.querySelector('path[stroke]')).toBeTruthy()
    expect(container.querySelectorAll('circle').length).toBe(pts.length * 2)
    expect(container.textContent).toContain('14')
    expect(container.textContent).toContain('09-02')
  })

  it('全 0 数据仍渲染网格并显示空态提示', () => {
    const zeros = pts.map((p) => ({ ...p, requests: 0 }))
    const { container } = render(<TrendChart data={zeros} />)
    expect(container.querySelector('svg')).toBeTruthy()
    expect(container.textContent).toContain('暂无数据')
  })

  it('空数组渲染占位文案', () => {
    const { container } = render(<TrendChart data={[]} />)
    expect(container.querySelector('svg')).toBeNull()
    expect(container.textContent).toContain('暂无数据')
  })
})

describe('TrendChart 悬浮交互', () => {
  it('mouseMove 出现十字准线与明细提示，离开后消失', () => {
    const { container } = render(<TrendChart data={pts} />)
    const svg = container.querySelector('svg')!
    svg.getBoundingClientRect = () =>
      ({ width: 800, height: 230, top: 0, left: 0, right: 800, bottom: 230, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect

    fireEvent.mouseMove(svg, { clientX: 400, clientY: 100 })
    expect(container.querySelector('line[stroke="rgba(139, 152, 169, 0.45)"]')).toBeTruthy()
    expect(container.textContent).toContain('请求数 14')

    fireEvent.mouseLeave(svg)
    expect(container.querySelector('line[stroke="rgba(139, 152, 169, 0.45)"]')).toBeNull()
    expect(container.textContent).not.toContain('请求数 14')
  })
})

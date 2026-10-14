import { act, fireEvent, render } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import MemoryGraph, { namespaceColor } from './MemoryGraph'
import type { MemoryGraph as GraphData } from '../api/memory'

/** 捕获组件注册的 ResizeObserver 回调，由用例手动喂入容器尺寸。 */
let roCallback: ResizeObserverCallback | null = null

beforeEach(() => {
  roCallback = null
  vi.stubGlobal(
    'ResizeObserver',
    class {
      constructor(cb: ResizeObserverCallback) {
        roCallback = cb
      }
      observe() {}
      disconnect() {}
    },
  )
})

const graph: GraphData = {
  nodes: [
    { id: 1, title: '支付回调幂等', namespace: '支付', kind: 'convention', importance: 4, pinned: true, access_count: 3, degree: 1, updated_at: '' },
    { id: 2, title: '数据库连接池', namespace: '配置', kind: 'note', importance: 3, pinned: false, access_count: 0, degree: 1, updated_at: '' },
  ],
  edges: [
    { id: 10, from_id: 1, to_id: 2, relation: 'related', note: '' },
  ],
  truncated: false,
}

describe('namespaceColor', () => {
  it('同名稳定取色', () => {
    expect(namespaceColor('支付')).toBe(namespaceColor('支付'))
    expect(namespaceColor('支付')).not.toBe(namespaceColor('配置'))
  })

  it('未分类用中性灰', () => {
    expect(namespaceColor('')).toBe('#8b98a9')
  })
})

describe('MemoryGraph', () => {
  it('按容器尺寸渲染节点与边', () => {
    const { container } = render(<MemoryGraph graph={graph} height={400} />)
    act(() => {
      roCallback?.([{ contentRect: { width: 800, height: 400 } } as unknown as ResizeObserverEntry], {} as ResizeObserver)
    })
    const svg = container.querySelector('svg')!
    expect(svg.getAttribute('width')).toBe('800')
    expect(svg.getAttribute('height')).toBe('400')
    expect(container.querySelectorAll('circle').length).toBe(2)
    expect(container.querySelectorAll('line').length).toBe(1)
  })

  it('点击节点触发 onSelect', () => {
    const onSelect = vi.fn()
    const { container } = render(<MemoryGraph graph={graph} onSelect={onSelect} height={400} />)
    act(() => {
      roCallback?.([{ contentRect: { width: 800, height: 400 } } as unknown as ResizeObserverEntry], {} as ResizeObserver)
    })
    const circle = container.querySelector('circle')!
    fireEvent.pointerDown(circle)
    fireEvent.pointerUp(circle)
    expect(onSelect).toHaveBeenCalledWith(1)
  })

  it('裁剪提示在 truncated 时出现', () => {
    const { container } = render(<MemoryGraph graph={{ ...graph, truncated: true }} height={400} />)
    expect(container.textContent).toContain('裁剪')
  })
})

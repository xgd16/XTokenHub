import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Core } from 'cytoscape'
import MemoryGraph, { buildStyle, namespaceColor, prepareGraph } from './MemoryGraph'
import type { MemoryGraph as GraphData } from '../api/memory'
import { chartPalette } from '../theme'

beforeEach(() => {
  // 组件与 antd 都用 ResizeObserver（jsdom 没有）。桩要实现全 unobserve，
  // 否则 @rc-component/resize-observer 在卸载时会报错。
  vi.stubGlobal('ResizeObserver', class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
})

/**
 * 拿回组件内部建的 cytoscape 实例。
 * cytoscape 3.x 不支持 `cytoscape(domEl)` 取回实例（那是当年 jQuery 插件的能力），
 * 实例固定挂在容器的 `_cyreg.cy` 上。测试只读这个字段，不改生产代码接口。
 */
const cyOf = (host: HTMLElement): Core =>
  (host as unknown as { _cyreg?: { cy?: Core } })._cyreg?.cy as Core

/** 等动态 import 的 promise 落地并让 React 提交。 */
async function flush(): Promise<void> {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0))
  })
}

const node = (id: number, over: Partial<GraphData['nodes'][number]> = {}): GraphData['nodes'][number] => ({
  id,
  title: `记忆 ${id}`,
  namespace: '支付',
  kind: 'convention',
  importance: 3,
  pinned: false,
  access_count: 0,
  degree: 1,
  updated_at: '',
  ...over,
})

const graph: GraphData = {
  nodes: [
    node(1, { title: '支付回调幂等', pinned: true, degree: 1 }),
    node(2, { title: '数据库连接池', namespace: '配置', degree: 1 }),
  ],
  edges: [{ id: 10, from_id: 1, to_id: 2, relation: 'related', note: '' }],
  truncated: false,
}

/** 渲染组件、等 cytoscape 就绪，返回它的 core 实例。 */
async function mountCy(
  props: Partial<Parameters<typeof MemoryGraph>[0]> = {},
): Promise<{ cy: Core; container: HTMLElement }> {
  const { container } = render(<MemoryGraph graph={graph} height={400} {...props} />)
  const host = container.querySelector<HTMLElement>('.mg-host')!
  await flush()
  await waitFor(() => expect(host.querySelector('canvas')).not.toBeNull())
  return { cy: cyOf(host), container }
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

describe('prepareGraph', () => {
  it('把节点转成 cytoscape 元素并带上业务主键', () => {
    const prep = prepareGraph(graph)
    const nodes = prep.elements.filter((e) => e.group === 'nodes')
    expect(nodes).toHaveLength(2)
    expect(nodes[0].data).toMatchObject({ id: 'n1', key: 1, title: '支付回调幂等', pinned: true, orphan: false })
  })

  it('边引用不存在的节点时被丢弃（否则 cytoscape 抛错）', () => {
    const prep = prepareGraph({
      ...graph,
      edges: [...graph.edges, { id: 11, from_id: 1, to_id: 999, relation: 'related', note: '' }],
    })
    expect(prep.elements.filter((e) => e.group === 'edges')).toHaveLength(1)
  })

  it('命名空间取色只取决于名字集合，与出现顺序/数量无关', () => {
    const a = prepareGraph({ ...graph, nodes: [node(1, { namespace: '乙' }), node(2, { namespace: '甲' })] })
    const b = prepareGraph({
      ...graph,
      nodes: [node(1, { namespace: '甲' }), node(2, { namespace: '乙' }), node(3, { namespace: '乙' })],
    })
    expect(new Set(a.namespaces.map((n) => n.color)).size).toBe(2)
    expect(b.namespaces.find((n) => n.name === '甲')!.color).toBe(a.namespaces.find((n) => n.name === '甲')!.color)
  })

  it('权重归一化到 0~100，孤立标记取自后端给的 degree', () => {
    const prep = prepareGraph({
      ...graph,
      nodes: [node(1, { importance: 5 }), node(2, { importance: 1, degree: 0 }), node(3, { importance: 3 })],
      edges: [],
    })
    const nodes = prep.elements.filter((e) => e.group === 'nodes')
    const weights = nodes.map((e) => e.data!.weight as number)
    expect(Math.min(...weights)).toBe(0)
    expect(Math.max(...weights)).toBe(100)
    expect(nodes.find((e) => e.data!.key === 2)!.data!.orphan).toBe(true)
    expect(prep.orphanCount).toBe(1)
  })

  it('统计关系类型与数量（供图例）', () => {
    const prep = prepareGraph({
      ...graph,
      edges: [
        ...graph.edges,
        { id: 11, from_id: 1, to_id: 2, relation: 'conflicts', note: '' },
        { id: 12, from_id: 1, to_id: 2, relation: 'conflicts', note: '' },
      ],
    })
    expect(prep.relCounts).toEqual([
      { rel: 'related', count: 1 },
      { rel: 'conflicts', count: 2 },
    ])
  })
})

describe('buildStyle', () => {
  const pal = chartPalette('dark')
  type Block = ReturnType<typeof buildStyle>[number]
  /** StylesheetJsonBlock 是 union，测试只关心带 style 的那种。 */
  const pick = (style: ReturnType<typeof buildStyle>, sel: string) =>
    style.find((r) => r.selector === sel) as Extract<Block, { style: unknown }> | undefined

  it('按命名空间索引给节点上色', () => {
    const style = buildStyle(pal, ['#111111', '#222222'])
    expect(pick(style, 'node[nsIdx = "0"]')).toMatchObject({ style: { 'background-color': '#111111' } })
    expect(pick(style, 'node[nsIdx = "1"]')).toMatchObject({ style: { 'background-color': '#222222' } })
  })

  it('关系样式区分虚实与箭头方向', () => {
    const style = buildStyle(pal, [])
    expect(pick(style, 'edge[rel = "related"]')!.style).toMatchObject({
      'line-style': 'solid', 'target-arrow-shape': 'none',
    })
    expect(pick(style, 'edge[rel = "supersedes"]')!.style).toMatchObject({
      'line-style': 'dashed', 'target-arrow-shape': 'triangle',
    })
  })

  it('关系配色取自色板（深浅主题都能区分）', () => {
    const style = buildStyle(pal, [])
    expect(pick(style, 'edge[rel = "conflicts"]')!.style).toMatchObject({ 'line-color': pal.coral })
    expect(pick(style, 'edge[rel = "derived_from"]')!.style).toMatchObject({ 'line-color': pal.violet })
    expect(pick(style, 'edge[rel = "supersedes"]')!.style).toMatchObject({ 'line-color': pal.amber })
  })

  it('置顶/选中/悬停用描边区分，不改半径', () => {
    const style = buildStyle(pal, [])
    expect(pick(style, 'node.pinned')!.style).toMatchObject({ 'border-color': pal.amber })
    expect(pick(style, 'node.selected')!.style).toMatchObject({ 'border-color': pal.accent })
    expect(pick(style, 'node.pinned')!.style).not.toHaveProperty('width')
  })
})

describe('MemoryGraph', () => {
  it('把节点与边灌进 cytoscape', async () => {
    const { cy } = await mountCy()
    expect(cy.nodes().length).toBe(2)
    expect(cy.edges().length).toBe(1)
    expect(cy.getElementById('n1').data('title')).toBe('支付回调幂等')
    expect(cy.getElementById('e10').data('rel')).toBe('related')
  })

  it('点击节点触发 onSelect，且回传业务主键而非 DOM id', async () => {
    const onSelect = vi.fn()
    const { cy } = await mountCy({ onSelect })
    act(() => {
      cy.getElementById('n2').emit('tap')
    })
    expect(onSelect).toHaveBeenCalledWith(2)
  })

  it('选中态同步到图上（只保留一个 selected）', async () => {
    const { rerender, container } = render(<MemoryGraph graph={graph} height={400} selectedId={null} />)
    const host = container.querySelector<HTMLElement>('.mg-host')!
    await flush()
    await waitFor(() => expect(host.querySelector('canvas')).not.toBeNull())
    const cy = cyOf(host)
    await act(async () => {
      rerender(<MemoryGraph graph={graph} height={400} selectedId={1} />)
    })
    await waitFor(() => expect(cy.nodes('.selected').length).toBe(1))
    expect(cy.getElementById('n1').hasClass('selected')).toBe(true)
    await act(async () => {
      rerender(<MemoryGraph graph={graph} height={400} selectedId={2} />)
    })
    await waitFor(() => expect(cy.nodes('.selected').length).toBe(1))
    expect(cy.getElementById('n2').hasClass('selected')).toBe(true)
  })

  it('图数据变化时增量更新：删掉的节点消失、新的节点进来', async () => {
    const { container, rerender } = render(<MemoryGraph graph={graph} height={400} />)
    const host = container.querySelector<HTMLElement>('.mg-host')!
    await flush()
    await waitFor(() => expect(host.querySelector('canvas')).not.toBeNull())
    const cy = cyOf(host)

    await act(async () => {
      rerender(
        <MemoryGraph
          graph={{ nodes: [graph.nodes[0], node(3, { title: '新增记忆' })], edges: [], truncated: false }}
          height={400}
        />,
      )
    })

    await waitFor(() => expect(cy.nodes().length).toBe(2))
    expect(cy.getElementById('n2').empty()).toBe(true)
    expect(cy.getElementById('n3').data('title')).toBe('新增记忆')
    expect(cy.edges().length).toBe(0)
  })

  it('只改关系（节点集合不变）时不重排，保留用户已有的坐标', async () => {
    const { container, rerender } = render(<MemoryGraph graph={graph} height={400} />)
    const host = container.querySelector<HTMLElement>('.mg-host')!
    await flush()
    await waitFor(() => expect(host.querySelector('canvas')).not.toBeNull())
    const cy = cyOf(host)
    cy.getElementById('n1').position({ x: 111, y: 222 })
    cy.getElementById('n2').position({ x: -77, y: 55 })

    await act(async () => {
      rerender(
        <MemoryGraph
          graph={{ ...graph, edges: [{ id: 10, from_id: 1, to_id: 2, relation: 'conflicts', note: '' }] }}
          height={400}
        />,
      )
    })

    await waitFor(() => expect(cy.getElementById('e10').data('rel')).toBe('conflicts'))
    expect(cy.getElementById('n1').position()).toEqual({ x: 111, y: 222 })
    expect(cy.getElementById('n2').position()).toEqual({ x: -77, y: 55 })
  })

  it('图例渲染命名空间与关系类型', async () => {
    await mountCy()
    expect(screen.getByRole('button', { name: /支付\(1\)/ })).toBeTruthy()
    expect(screen.getByRole('button', { name: /配置\(1\)/ })).toBeTruthy()
    expect(screen.getByText('相关(1)')).toBeTruthy()
    expect(screen.getByText(/2 节点 \/ 1 关系/)).toBeTruthy()
  })

  it('图例点击可切换命名空间聚焦', async () => {
    await mountCy()
    const btn = screen.getByRole('button', { name: /支付\(1\)/ })
    expect(btn.getAttribute('aria-pressed')).toBe('false')
    await act(async () => {
      fireEvent.click(btn)
    })
    await waitFor(() => expect(btn.getAttribute('aria-pressed')).toBe('true'))
    await act(async () => {
      fireEvent.click(btn)
    })
    await waitFor(() => expect(btn.getAttribute('aria-pressed')).toBe('false'))
  })

  it('图例聚焦会把其它命名空间淡出', async () => {
    const { cy } = await mountCy()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /支付\(1\)/ }))
    })
    await waitFor(() => expect(cy.getElementById('n2').hasClass('faded')).toBe(true))
    expect(cy.getElementById('n1').hasClass('faded')).toBe(false)
  })

  it('悬停只点亮邻接，移出后高亮与淡出都清干净（不能卡在淡出态）', async () => {
    // 三节点：n1—n2 相连，n3 无关，悬停 n2 时 n3 必须淡出
    const { cy } = await mountCy({
      graph: {
        nodes: [...graph.nodes, node(3, { title: '无关记忆', degree: 0 })],
        edges: graph.edges,
        truncated: false,
      },
    })
    const n2 = cy.getElementById('n2')
    act(() => {
      n2.emit('mouseover')
    })
    await waitFor(() => expect(cy.getElementById('n3').hasClass('faded')).toBe(true))
    expect(cy.getElementById('n1').hasClass('faded')).toBe(false)
    expect(cy.getElementById('e10').hasClass('highlighted')).toBe(true)

    // 移出后 setHover(null) 是异步的，但高亮必须立刻跟着清掉
    act(() => {
      n2.emit('mouseout')
    })
    await waitFor(() => expect(cy.elements('.faded').length).toBe(0))
    expect(cy.elements('.highlighted').length).toBe(0)
  })

  it('关系图例按语义着色并区分箭头/虚线', async () => {
    const pal = chartPalette('dark')
    const { container } = await mountCy({
      graph: {
        ...graph,
        edges: [
          { id: 10, from_id: 1, to_id: 2, relation: 'supersedes', note: '' },
          { id: 11, from_id: 1, to_id: 2, relation: 'conflicts', note: '' },
        ],
      },
    })
    // 只看关系图例自己的 svg，别把 antd 图标的 path 算进来
    const svgs = Array.from(container.querySelectorAll('.mg-rel-legend svg'))
    expect(svgs.map((s) => s.querySelector('line')!.getAttribute('stroke-dasharray'))).toEqual(['4 3', '4 3'])
    expect(svgs.map((s) => s.querySelector('path')!.getAttribute('fill'))).toEqual([pal.amber, pal.coral])
    expect(screen.getByText('替代(1)')).toBeTruthy()
    expect(screen.getByText('冲突(1)')).toBeTruthy()
  })

  it('孤立节点在统计里提示已单列（孤立取自后端给的 degree=0）', async () => {
    await mountCy({
      graph: {
        nodes: [node(1, { degree: 0 }), node(2, { namespace: '配置', degree: 0 })],
        edges: [],
        truncated: false,
      },
    })
    expect(screen.getByText(/2 条无关联已单列/)).toBeTruthy()
  })

  it('孤立节点被排到主连通块下方而不是堆在圆心', async () => {
    const { cy } = await mountCy({
      graph: {
        nodes: [node(1), node(2), node(3, { degree: 0 }), node(4, { degree: 0 })],
        edges: [{ id: 10, from_id: 1, to_id: 2, relation: 'related', note: '' }],
        truncated: false,
      },
    })
    const linkedBottom = Math.max(cy.getElementById('n1').position('y'), cy.getElementById('n2').position('y'))
    const orphanTop = Math.min(cy.getElementById('n3').position('y'), cy.getElementById('n4').position('y'))
    expect(orphanTop).toBeGreaterThan(linkedBottom)
  })

  it('提供缩放/适应/重排控制，且带无障碍名称', async () => {
    const { cy } = await mountCy()
    for (const name of ['放大', '缩小', '适应画布', '重新布局']) {
      expect(screen.getByLabelText(name)).toBeTruthy()
    }
    const before = cy.zoom()
    await act(async () => {
      fireEvent.click(screen.getByLabelText('放大'))
    })
    await waitFor(() => expect(cy.zoom()).toBeGreaterThan(before))
  })

  it('裁剪提示在 truncated 时出现', async () => {
    await mountCy({ graph: { ...graph, truncated: true } })
    expect(screen.getByText(/节点过多已裁剪/)).toBeTruthy()
  })

  it('画布带描述性 aria-label（列表视图是等价可读形式）', async () => {
    const { container } = await mountCy()
    const el = container.querySelector('[role="img"]')!
    expect(el.getAttribute('aria-label')).toContain('2 条记忆、1 条关系')
    expect(el.getAttribute('aria-label')).toContain('列表')
  })

  it('卸载时销毁 cytoscape 实例', async () => {
    const { unmount, container } = render(<MemoryGraph graph={graph} height={400} />)
    const host = container.querySelector<HTMLElement>('.mg-host')!
    await flush()
    await waitFor(() => expect(host.querySelector('canvas')).not.toBeNull())
    const cy = cyOf(host)
    expect(cy.destroyed()).toBe(false)
    unmount()
    expect(cy.destroyed()).toBe(true)
  })
})

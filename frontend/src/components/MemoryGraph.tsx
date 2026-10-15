import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
// cytoscape 的类型用 `export =` 声明，默认类型导入可同时拿到命名空间（StylesheetJson / Css.Node 等）。
import type cytoscape from 'cytoscape'
import type {
  Core, CoseLayoutOptions, ElementDefinition, EventObject, Layouts, NodeSingular,
} from 'cytoscape'
import { Button, Tooltip } from 'antd'
import { CompressOutlined, MinusOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import type {
  MemoryGraph as GraphData, MemoryGraphNode, MemoryKind, MemoryLinkRelation,
} from '../api/memory'
import { kindLabel, namespaceColor } from '../utils/memoryMeta'
import { useChartPalette, type ChartPalette } from '../theme'

// 节点/图例着色与星图卡片同源（utils/memoryMeta），从此处再导出以保持既有引用。
export { namespaceColor }

/** cytoscape 工厂函数签名。 */
type CytoscapeFactory = typeof import('cytoscape')

/** 动态载入的模块解析成工厂：ESM 走 `.default`，CJS（vitest/node）直接就是函数本体。 */
function resolveFactory(mod: unknown): CytoscapeFactory {
  const m = mod as { default?: CytoscapeFactory } & CytoscapeFactory
  return m.default ?? m
}

/** 缩放到此倍数以下时只给重要节点留标签，避免密图上文字糊成一片。 */
const LABEL_FAR_ZOOM = 0.65

/** 关系边语义样式：颜色 + 虚实 + 是否有向箭头。 */
interface RelationStyle {
  label: string
  color: (pal: ChartPalette) => string
  dash: boolean
  directed: boolean
}

const RELATION_STYLE: Record<MemoryLinkRelation, RelationStyle> = {
  related: { label: '相关', color: (p) => p.accent, dash: false, directed: false },
  supersedes: { label: '替代', color: (p) => p.amber, dash: true, directed: true },
  conflicts: { label: '冲突', color: (p) => p.coral, dash: true, directed: true },
  derived_from: { label: '派生自', color: (p) => p.violet, dash: true, directed: true },
}
const RELATIONS = Object.keys(RELATION_STYLE) as MemoryLinkRelation[]

/** 后端新增的关系类型兜底为「相关」样式。 */
function relationStyle(rel: string): RelationStyle {
  return RELATION_STYLE[rel as MemoryLinkRelation] ?? RELATION_STYLE.related
}

// ---- 数据准备 ----

/** 节点视觉权重：重要度为主、关联度为辅，置顶略微加权（只影响相对大小）。 */
function rawWeight(n: MemoryGraphNode): number {
  return n.importance * 2 + Math.min(n.degree, 8) * 1.2 + (n.pinned ? 3 : 0)
}

export interface PreparedGraph {
  elements: ElementDefinition[]
  /** 按名称排序的命名空间（顺序稳定 → 颜色跨刷新不跳），附出现次数。 */
  namespaces: { name: string; color: string; count: number }[]
  /** 图例上展示的关系类型与数量。 */
  relCounts: { rel: MemoryLinkRelation; count: number }[]
  /** 无任何关系的节点数（布局时单列到主连通块下方）。 */
  orphanCount: number
}

/** 图数据 → cytoscape 元素集 + 图例统计（纯函数，便于测试）。 */
export function prepareGraph(graph: GraphData): PreparedGraph {
  // 取色按名字排序而不是按数量，否则节点增减会让颜色整片跳变。
  const nsCount = new Map<string, number>()
  graph.nodes.forEach((n) => nsCount.set(n.namespace, (nsCount.get(n.namespace) ?? 0) + 1))
  const namespaces = Array.from(nsCount, ([name, count]) => ({ name, color: namespaceColor(name), count }))
    .sort((a, b) => a.name.localeCompare(b.name, 'zh-Hans-CN'))
  const nsIdx = new Map(namespaces.map((s, i) => [s.name, i]))

  const raws = graph.nodes.map(rawWeight)
  const lo = raws.length ? Math.min(...raws) : 0
  const hi = raws.length ? Math.max(...raws) : 0
  const span = hi - lo || 1

  const ids = new Set<number>()
  const elements: ElementDefinition[] = graph.nodes.map((n, i) => {
    ids.add(n.id)
    return {
      group: 'nodes',
      data: {
        id: `n${n.id}`,
        // 业务主键单独存一份，回调/tooltip 不用再从 DOM id 剥前缀
        key: n.id,
        title: n.title,
        ns: n.namespace,
        nsIdx: String(nsIdx.get(n.namespace) ?? 0),
        kind: n.kind,
        importance: n.importance,
        pinned: n.pinned,
        degree: n.degree,
        weight: Math.round(((raws[i] - lo) / span) * 100),
        orphan: n.degree === 0,
      },
    }
  })

  // 只保留两端节点都在图内的边，否则 cytoscape 会抛错。
  const relCount = new Map<MemoryLinkRelation, number>()
  graph.edges.forEach((e) => {
    if (!ids.has(e.from_id) || !ids.has(e.to_id)) return
    relCount.set(e.relation, (relCount.get(e.relation) ?? 0) + 1)
    elements.push({
      group: 'edges',
      data: {
        id: `e${e.id}`,
        source: `n${e.from_id}`,
        target: `n${e.to_id}`,
        rel: e.relation,
        relLabel: relationStyle(e.relation).label,
      },
    })
  })

  return {
    elements,
    namespaces,
    relCounts: RELATIONS.map((rel) => ({ rel, count: relCount.get(rel) ?? 0 })).filter((r) => r.count > 0),
    orphanCount: graph.nodes.filter((n) => n.degree === 0).length,
  }
}

// ---- 样式表 ----

/** 由色板 + 命名空间取色生成 cytoscape 样式表（canvas 读不到 CSS 变量，必须给具体色值）。 */
export function buildStyle(pal: ChartPalette, nsColors: string[]): cytoscape.StylesheetJson {
  const node: cytoscape.Css.Node = {
    'background-color': '#8b98a9',
    width: 'mapData(weight, 0, 100, 24, 48)',
    height: 'mapData(weight, 0, 100, 24, 48)',
    'border-width': 2,
    'border-color': pal.track,
    label: 'data(title)',
    color: pal.textSecondary,
    'font-size': 11,
    'font-family': 'IBM Plex Sans, PingFang SC, Microsoft YaHei, system-ui, sans-serif',
    // 标签放圆下方：圆本身是视觉锚点，标题不压圆。
    'text-valign': 'bottom',
    'text-halign': 'center',
    'text-margin-y': 6,
    // 单行 + 省略号：标题全貌在悬浮卡里，图上不铺开多行，节点不会被文字撑变形。
    'text-wrap': 'ellipsis',
    'text-max-width': '124px',
    'text-background-color': pal.tooltipBg,
    'text-background-opacity': 0.6,
    'text-background-padding': '2px',
    'text-background-shape': 'roundrectangle',
    // 透明 overlay：让带 padding 的整个命中区都可点，小节点也好点中。
    'overlay-opacity': 0,
    'transition-property': 'opacity, border-width, border-color, text-opacity',
    'transition-duration': 150,
  }

  const style: cytoscape.StylesheetJson = [{ selector: 'node', style: node }]

  nsColors.forEach((color, i) => {
    style.push({ selector: `node[nsIdx = "${i}"]`, style: { 'background-color': color } })
  })

  style.push(
    // 置顶/选中/悬停靠 border 宽度与颜色区分，不动半径，视觉更稳。
    { selector: 'node.pinned', style: { 'border-color': pal.amber, 'border-width': 3 } },
    { selector: 'node.selected', style: { 'border-color': pal.accent, 'border-width': 4, 'z-index': 20 } },
    { selector: 'node.hovered', style: { 'border-color': pal.hoverStroke, 'border-width': 3, 'z-index': 30 } },

    { selector: 'edge', style: {
      'curve-style': 'bezier',
      width: 1.5,
      'line-color': pal.grid,
      opacity: 0.75,
      'arrow-scale': 0.85,
      'target-arrow-shape': 'none',
      label: 'data(relLabel)',
      color: pal.textSecondary,
      'font-size': 10,
      'text-rotation': 'autorotate',
      'text-margin-y': -4,
      'text-background-color': pal.tooltipBg,
      'text-background-opacity': 0.75,
      'text-background-padding': '2px',
      // 关系名默认隐藏，只有邻接高亮时显示，否则密图上全是字。
      'text-opacity': 0,
      'transition-property': 'opacity, line-color, width, text-opacity',
      'transition-duration': 150,
    } },
  )

  RELATIONS.forEach((rel) => {
    const r = RELATION_STYLE[rel]
    const color = r.color(pal)
    style.push({
      selector: `edge[rel = "${rel}"]`,
      style: {
        'line-color': color,
        'line-style': r.dash ? 'dashed' : 'solid',
        'target-arrow-shape': r.directed ? 'triangle' : 'none',
        'target-arrow-color': color,
      },
    })
  })

  style.push(
    { selector: 'edge.highlighted', style: { width: 2.4, opacity: 1, 'text-opacity': 1, 'z-index': 20 } },
    { selector: 'node.label-off', style: { 'text-opacity': 0 } },
    { selector: 'node.label-off.hovered, node.label-off.selected', style: { 'text-opacity': 1 } },
    { selector: 'edge.label-off.highlighted', style: { 'text-opacity': 1 } },
    // 淡出：悬停聚焦邻接 / 图例按命名空间筛除时用。
    { selector: '.faded', style: { opacity: 0.12 } },
    { selector: 'edge.faded', style: { opacity: 0.04 } },
  )

  return style
}

// ---- 布局 ----

/** cose 参数：斥力/边长/向心力调到「圆之间留白、簇之间不散」的观感。 */
const LAYOUT_OPTIONS: CoseLayoutOptions = {
  name: 'cose',
  animate: false,
  randomize: true,
  fit: false, // 孤立节点排完再统一 fit
  padding: 40,
  nodeRepulsion: 9000,
  idealEdgeLength: 130,
  edgeElasticity: 90,
  nestingFactor: 1.2,
  gravity: 0.4,
  componentSpacing: 90,
  numIter: 1200,
  initialTemp: 220,
  coolingFactor: 0.94,
  minTemp: 1,
  nodeDimensionsIncludeLabels: true,
}

/**
 * 孤立节点（无任何关系）不交给力导向：cose 会把它们全堆到圆心。
 * 布局结束后按网格排在主连通块下方，再整体 fit。
 */
function placeOrphans(cy: Core): void {
  // 按 data 判定而不是 `.orphan` 类：类名会被 selected/faded 等样式类一起改写。
  const orphans = cy.nodes().filter((n) => n.data('orphan') === true)
  if (orphans.length === 0) return
  const linked = cy.nodes().filter((n) => n.data('orphan') !== true)
  const bb = linked.nonempty() ? linked.boundingBox() : { x1: -200, y1: -200, x2: 200, y2: 200 }
  const cols = Math.max(1, Math.ceil(Math.sqrt(orphans.length * 1.7)))
  const gap = 64
  const top = bb.y2 + gap
  const startX = (bb.x1 + bb.x2) / 2 - ((cols - 1) * gap) / 2
  cy.batch(() => {
    orphans.forEach((n, i) => {
      n.position({ x: startX + (i % cols) * gap, y: top + Math.floor(i / cols) * gap })
    })
  })
}

// ---- 悬浮卡 ----

interface Neighbor {
  id: number
  title: string
  rel: MemoryLinkRelation
}

interface HoverInfo {
  id: number
  /** 节点在画布内的渲染坐标（相对容器左上角）。 */
  x: number
  y: number
  title: string
  ns: string
  kind: MemoryKind
  importance: number
  degree: number
  pinned: boolean
  orphan: boolean
  neighbors: Neighbor[]
}

interface Props {
  graph: GraphData
  /** 点击节点回调（打开详情）。 */
  onSelect?: (id: number) => void
  /** 当前选中节点（高亮描边）。 */
  selectedId?: number | null
  /** 画布高度（容器宽度自适应填充）。 */
  height?: number
}

/**
 * 记忆关系图：基于 cytoscape.js 的力导向图。
 * 布局/缩放/平移/邻接高亮全部交给 cytoscape，本组件只做数据转换、主题取色与浮层。
 */
export default function MemoryGraph({ graph, onSelect, selectedId, height = 560 }: Props) {
  const pal = useChartPalette()
  const hostRef = useRef<HTMLDivElement | null>(null)
  const cyRef = useRef<Core | null>(null)
  const layoutRef = useRef<Layouts | null>(null)
  // 工厂函数本身是函数，放 state 会被 React 当成 updater 调用，所以只放 ref，用 ready 触发渲染。
  const factoryRef = useRef<CytoscapeFactory | null>(null)
  const [ready, setReady] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [hover, setHover] = useState<HoverInfo | null>(null)
  const [focusNs, setFocusNs] = useState<string | null>(null)

  // 回调/状态进 ref：事件监听只在实例化时绑一次，避免随渲染反复重绑。
  const onSelectRef = useRef(onSelect)
  onSelectRef.current = onSelect
  // 统一收敛成 number | null，省得每处判断 undefined。
  const selectedIdRef = useRef<number | null>(selectedId ?? null)
  selectedIdRef.current = selectedId ?? null
  const focusNsRef = useRef<string | null>(focusNs)
  focusNsRef.current = focusNs
  const hoverIdRef = useRef<number | null>(null)
  hoverIdRef.current = hover?.id ?? null

  const prep = useMemo(() => prepareGraph(graph), [graph])
  const nsColors = useMemo(() => prep.namespaces.map((n) => n.color), [prep])

  useEffect(() => {
    let alive = true
    void import('cytoscape')
      .then((m) => {
        if (!alive) return
        factoryRef.current = resolveFactory(m)
        setReady(true)
      })
      .catch((e: unknown) => {
        if (alive) setLoadError((e as Error)?.message ?? '关系图引擎载入失败')
      })
    return () => {
      alive = false
    }
  }, [])

  /**
   * 高亮策略：悬停聚焦邻接、选中保持可见、图例按命名空间筛除，其余淡出。
   * hoveredId 由调用方显式传入：事件回调里 setHover 是异步的，此刻 hoverIdRef 还是旧值，
   * 读 ref 会让「移出节点后整张图卡在淡出态」。
   */
  const applyHighlight = useCallback((hoveredId: number | null) => {
    const cy = cyRef.current
    if (!cy) return
    const sel = selectedIdRef.current
    const focus = focusNsRef.current
    // 没有任何激活项时 keep 为空，此时若还按「不在 keep 里就淡出」算，会把整张图都淡掉，
    // 所以要用 active 短路——但不能提前 return，否则上一次留下的 faded 清不掉。
    const active = hoveredId != null || sel != null || focus != null

    const keep = new Set<string>()
    const spread = (id: number | null) => {
      if (id == null) return
      const n = cy.getElementById(`n${id}`)
      if (n.empty()) return
      keep.add(n.id())
      n.connectedEdges().forEach((e) => {
        keep.add(e.id())
        keep.add(e.source().id())
        keep.add(e.target().id())
      })
    }
    spread(hoveredId)
    spread(sel)
    if (focus != null) {
      cy.nodes().forEach((n) => {
        if (n.data('ns') === focus) keep.add(n.id())
      })
      cy.edges().forEach((e) => {
        if (keep.has(e.source().id()) && keep.has(e.target().id())) keep.add(e.id())
      })
    }

    cy.batch(() => {
      cy.elements().forEach((el) => {
        el.toggleClass('faded', active && !keep.has(el.id()))
        if (el.isEdge()) el.toggleClass('highlighted', active && keep.has(el.id()))
      })
    })
  }, [])

  /** 远景（缩放较小）时只给重要节点留标签。 */
  const applyZoomLabels = useCallback(() => {
    const cy = cyRef.current
    if (!cy || cy.nodes().empty()) return
    const far = cy.zoom() < LABEL_FAR_ZOOM
    const weights = cy.nodes().map((n) => n.data('weight') as number).sort((a, b) => b - a)
    // 远景保留权重最高的 30%
    const cut = weights[Math.floor(weights.length * 0.3)] ?? 0
    cy.batch(() => {
      cy.nodes().forEach((n) => {
        n.toggleClass('label-off', far && (n.data('weight') as number) < cut)
      })
    })
  }, [])

  // ---- 实例化（只做一次；数据/主题/图例变化走下面的增量 effect）----
  useEffect(() => {
    const host = hostRef.current
    const factory = factoryRef.current
    if (!ready || !host || !factory) return

    const cy = factory({
      container: host,
      style: buildStyle(pal, nsColors),
      layout: { name: 'preset' },
      minZoom: 0.15,
      maxZoom: 3,
      boxSelectionEnabled: false,
      // 清晰度相关：四项都显式设置，否则 HiDPI 屏上整张图发虚。
      // 注意 pixelRatio 的默认值是 1（不是 'auto'），等于按 CSS 像素渲染再交给浏览器
      // 放大 2 倍 —— 圆和文字必糊。必须显式给 'auto' 才跟随 devicePixelRatio。
      pixelRatio: 'auto',
      // textureOnViewport / hideEdgesOnViewport 会把视口外的元素缓存成贴图，平移时
      // 用低清贴图顶替原生分辨率；节点量级不大，全量重绘换清晰度更划算。
      textureOnViewport: false,
      hideEdgesOnViewport: false,
      // 平移/缩放动画的残影同样糊边
      motionBlur: false,
    })
    cyRef.current = cy

    cy.on('tap', 'node', (evt: EventObject) => {
      const id = (evt.target as NodeSingular).data('key') as number
      onSelectRef.current?.(id)
    })
    cy.on('mouseover', 'node', (evt: EventObject) => {
      const n = evt.target as NodeSingular
      if (!n || n.empty()) return
      const key = n.data('key') as number
      n.addClass('hovered')
      const neighbors = n.connectedEdges().map<Neighbor>((e) => {
        const other = e.source().id() === n.id() ? e.target() : e.source()
        return { id: other.data('key') as number, title: other.data('title') as string, rel: e.data('rel') as MemoryLinkRelation }
      })
      neighbors.sort((a, b) => a.rel.localeCompare(b.rel))
      // 事件里理论上总带 renderedPosition，但解引用失败会炸掉整页，这里兜底成节点自身位置
      const rp = evt.renderedPosition ?? n.renderedPosition()
      setHover({
        id: key,
        x: rp.x,
        y: rp.y,
        title: n.data('title') as string,
        ns: n.data('ns') as string,
        kind: n.data('kind') as MemoryKind,
        importance: n.data('importance') as number,
        degree: n.data('degree') as number,
        pinned: n.data('pinned') as boolean,
        orphan: Boolean(n.data('orphan')),
        neighbors,
      })
      applyHighlight(key)
    })
    cy.on('mouseout', 'node', (evt: EventObject) => {
      const n = evt.target as NodeSingular
      if (!n || n.empty()) return
      n.removeClass('hovered')
      setHover(null)
      // 必须显式传 null：此刻 hoverIdRef 还没被新渲染刷新
      applyHighlight(null)
    })
    cy.on('zoom', applyZoomLabels)

    return () => {
      layoutRef.current?.stop()
      layoutRef.current = null
      cyRef.current = null
      cy.destroy()
    }
    // 实例只建一次；数据/主题/图例变化分别由下面的 effect 增量更新。
  }, [ready])

  // 主题切换：cytoscape 样式只能整体替换
  useEffect(() => {
    cyRef.current?.style(buildStyle(pal, nsColors))
  }, [pal, nsColors, ready])

  // ---- 数据更新：diff 节点（保留旧坐标）→ 重建边 → 节点集合变了才重排 ----
  const nodeSetRef = useRef('')
  useEffect(() => {
    const cy = cyRef.current
    if (!cy || !ready) return
    layoutRef.current?.stop()

    const prevPos = new Map<string, { x: number; y: number }>()
    cy.nodes().forEach((n) => {
      prevPos.set(n.id(), { ...n.position() })
    })

    const nodeEls = prep.elements.filter((e) => e.group === 'nodes')
    const edgeEls = prep.elements.filter((e) => e.group === 'edges')
    const nextIds = new Set(nodeEls.map((e) => String(e.data?.id)))
    const signature = Array.from(nextIds).sort().join(',')
    const changed = signature !== nodeSetRef.current

    cy.batch(() => {
      cy.nodes().forEach((n) => {
        if (!nextIds.has(n.id())) n.remove()
      })
      nodeEls.forEach((el) => {
        const id = String(el.data?.id)
        const exist = cy.getElementById(id)
        const pos = prevPos.get(id)
        if (exist.nonempty()) {
          exist.data(el.data ?? {})
          if (pos) exist.position(pos)
        } else {
          cy.add(pos ? { ...el, position: pos } : el)
        }
      })
      // 边整体重建：cytoscape 不支持就地改端点，重建远便宜于绕开这个限制。
      cy.edges().remove()
      cy.add(edgeEls)
    })
    nodeSetRef.current = signature

    if (changed) {
      const layout = cy.layout({ ...LAYOUT_OPTIONS, randomize: prevPos.size === 0 })
      layout.one('layoutstop', () => {
        layoutRef.current = null
        placeOrphans(cy)
        cy.fit(undefined, 40)
        applyZoomLabels()
      })
      layoutRef.current = layout
      layout.run()
    } else {
      cy.fit(undefined, 40)
      applyZoomLabels()
    }
    applyHighlight(hoverIdRef.current)
  }, [prep, ready, applyHighlight, applyZoomLabels])

  // 选中态同步（外部抽屉打开/关闭时）
  useEffect(() => {
    const cy = cyRef.current
    if (!cy || !ready) return
    cy.batch(() => {
      cy.nodes('.selected').removeClass('selected')
      if (selectedId != null) cy.getElementById(`n${selectedId}`).addClass('selected')
    })
    applyHighlight(hoverIdRef.current)
  }, [selectedId, ready, applyHighlight])

  // 切图例命名空间时先清 hover：悬浮卡指向的节点可能已被淡出。
  useEffect(() => {
    setHover(null)
    applyHighlight(null)
  }, [focusNs, applyHighlight])

  // 容器尺寸变化（侧栏折叠/窗口缩放）→ 重排画布
  useEffect(() => {
    const host = hostRef.current
    const cy = cyRef.current
    if (!host || !cy || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(() => {
      cy.resize()
      cy.fit(undefined, 40)
    })
    ro.observe(host)
    return () => ro.disconnect()
  }, [ready])

  const zoomBy = (k: number) => {
    const cy = cyRef.current
    if (!cy) return
    const z = Math.min(cy.maxZoom(), Math.max(cy.minZoom(), cy.zoom() * k))
    cy.animate({ zoom: { level: z, position: { x: cy.width() / 2, y: cy.height() / 2 } }, duration: 160 })
  }
  const fit = () => {
    const cy = cyRef.current
    if (!cy) return
    cy.animate({ fit: { eles: cy.elements(), padding: 40 }, duration: 240 })
  }
  const relayout = () => {
    const cy = cyRef.current
    if (!cy) return
    layoutRef.current?.stop()
    const layout = cy.layout({ ...LAYOUT_OPTIONS, randomize: true })
    layout.one('layoutstop', () => {
      layoutRef.current = null
      placeOrphans(cy)
      fit()
    })
    layoutRef.current = layout
    layout.run()
  }

  const a11yLabel = `记忆关系图：${graph.nodes.length} 条记忆、${graph.edges.length} 条关系。`
    + '可拖拽节点、滚轮缩放、点击查看详情；同样的数据可在「列表」视图逐条浏览。'

  return (
    <div
      style={{ position: 'relative', width: '100%', height, overflow: 'hidden' }}
      role="img"
      aria-label={a11yLabel}
    >
      <div ref={hostRef} className="mg-host" style={{ position: 'absolute', inset: 0, cursor: 'grab' }} />

      {loadError && (
        <div style={{ position: 'absolute', inset: 0, display: 'grid', placeItems: 'center', fontSize: 13, color: 'var(--coral)' }}>
          {loadError}
        </div>
      )}

      {/* 图例：点击只看某个命名空间 */}
      {prep.namespaces.length > 0 && (
        <div
          style={{
            position: 'absolute', left: 12, top: 12, display: 'flex', gap: 8, flexWrap: 'wrap',
            maxWidth: '58%', pointerEvents: 'auto',
          }}
        >
          {prep.namespaces.map((s) => {
            const active = focusNs === s.name
            return (
              <button
                key={s.name || '未分类'}
                type="button"
                onClick={() => setFocusNs(active ? null : s.name)}
                aria-pressed={active}
                style={{
                  display: 'inline-flex', alignItems: 'center', gap: 5, padding: '2px 8px',
                  borderRadius: 999, font: 'inherit', fontSize: 12, cursor: 'pointer',
                  color: active ? 'var(--text-primary)' : 'var(--text-secondary)',
                  background: active ? 'var(--accent-dim)' : 'transparent',
                  border: `1px solid ${active ? 'var(--accent)' : 'var(--border-faint)'}`,
                }}
              >
                <span style={{ width: 9, height: 9, borderRadius: '50%', background: s.color, flex: 'none' }} />
                {s.name || '未分类'}({s.count})
              </button>
            )
          })}
        </div>
      )}

      {/* 悬浮卡：锚在节点位置；邻居可点击跳转 */}
      {hover && (
        <Tooltip
          open
          placement="top"
          mouseEnterDelay={0}
          styles={{ root: { pointerEvents: 'auto' } }}
          title={
            <div style={{ maxWidth: 280, fontSize: 12 }}>
              <div style={{ fontWeight: 600, marginBottom: 4 }}>{hover.title}</div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
                <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
                  <span style={{ width: 8, height: 8, borderRadius: '50%', background: namespaceColor(hover.ns) }} />
                  {hover.ns || '未分类'}
                </span>
                <span>{kindLabel(hover.kind)}</span>
                <span>重要度 {hover.importance}</span>
                {hover.pinned && <span style={{ color: 'var(--amber)' }}>置顶</span>}
                <span>{hover.orphan ? '暂无关联' : `${hover.degree} 条关系`}</span>
              </div>
              {hover.neighbors.length > 0 && (
                <div style={{ borderTop: '1px solid var(--border-faint)', paddingTop: 4, marginTop: 4, maxHeight: 180, overflowY: 'auto' }}>
                  {hover.neighbors.map((nb) => (
                    <button
                      key={`${nb.rel}-${nb.id}`}
                      type="button"
                      onClick={() => onSelectRef.current?.(nb.id)}
                      style={{
                        display: 'block', width: '100%', textAlign: 'left', background: 'none',
                        border: 0, padding: '2px 0', color: 'inherit', font: 'inherit', cursor: 'pointer',
                      }}
                    >
                      <span style={{ opacity: 0.65, marginRight: 6 }}>[{relationStyle(nb.rel).label}]</span>
                      {nb.title}
                    </button>
                  ))}
                </div>
              )}
            </div>
          }
        >
          <span
            style={{
              position: 'absolute', left: hover.x, top: hover.y, width: 0, height: 0, pointerEvents: 'none',
            }}
          />
        </Tooltip>
      )}

      {/* 视图控制 */}
      <div style={{ position: 'absolute', right: 12, bottom: 12, display: 'flex', flexDirection: 'column', gap: 6 }}>
        <Button size="small" icon={<PlusOutlined />} onClick={() => zoomBy(1.25)} aria-label="放大" />
        <Button size="small" icon={<MinusOutlined />} onClick={() => zoomBy(0.8)} aria-label="缩小" />
        <Button size="small" icon={<CompressOutlined />} onClick={fit} aria-label="适应画布" />
        <Button size="small" icon={<ReloadOutlined />} onClick={relayout} aria-label="重新布局" />
      </div>

      {/* 关系图例 + 统计 */}
      <div
        className="mg-rel-legend"
        style={{
          position: 'absolute', left: 12, bottom: 10, fontSize: 11, color: 'var(--text-secondary)',
          display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap', maxWidth: '70%',
        }}
      >
        {prep.relCounts.map(({ rel, count }) => {
          const r = relationStyle(rel)
          return (
            <span key={rel} style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
              <svg width="26" height="8" aria-hidden="true">
                <line
                  x1="1" y1="4" x2="25" y2="4"
                  stroke={r.color(pal)} strokeWidth="1.6"
                  strokeDasharray={r.dash ? '4 3' : undefined}
                />
                {r.directed && <path d="M25 1 L20 4 L25 7 Z" fill={r.color(pal)} />}
              </svg>
              {r.label}({count})
            </span>
          )
        })}
        <span style={{ color: 'var(--text-faint)' }}>
          {graph.nodes.length} 节点 / {graph.edges.length} 关系
          {prep.orphanCount > 0 && ` · ${prep.orphanCount} 条无关联已单列`}
        </span>
      </div>

      {graph.truncated && (
        <div style={{ position: 'absolute', right: 12, top: 12, fontSize: 12, color: 'var(--text-faint)' }}>
          节点过多已裁剪，可按命名空间过滤查看
        </div>
      )}
    </div>
  )
}

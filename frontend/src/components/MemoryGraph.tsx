import { useEffect, useMemo, useRef, useState } from 'react'
import type { PointerEvent as ReactPointerEvent, WheelEvent as ReactWheelEvent } from 'react'
import type { MemoryGraph as GraphData, MemoryGraphEdge, MemoryGraphNode } from '../api/memory'
import { namespaceColor } from '../utils/memoryMeta'

// 节点/图例着色与星图卡片同源（utils/memoryMeta），从此处再导出以保持既有引用。
export { namespaceColor }

/** 关系边样式：替代/冲突/派生用虚线区分。 */
const RELATION_STYLE: Record<string, { color: string; dash: string }> = {
  related: { color: 'var(--accent)', dash: '' },
  supersedes: { color: '#e8a33d', dash: '6 4' },
  conflicts: { color: '#e8664f', dash: '2 3' },
  derived_from: { color: '#9c7be8', dash: '6 4' },
}

interface SimNode {
  id: number
  x: number
  y: number
  vx: number
  vy: number
  fixed: boolean
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
 * 记忆关系图：SVG 力导向图（自研弹簧-斥力模型，零第三方依赖）。
 * 节点按命名空间着色、大小按重要度+度数；支持拖拽/平移/缩放/点击。
 */
export default function MemoryGraph({ graph, onSelect, selectedId, height = 560 }: Props) {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const [size, setSize] = useState({ w: 960, h: height })
  const [hover, setHover] = useState<{ id: number; x: number; y: number } | null>(null)

  // 仿真状态（不进 React 状态，避免每帧重渲染）
  const simRef = useRef<Map<number, SimNode>>(new Map())
  const alphaRef = useRef(1)
  const rafRef = useRef<number | null>(null)
  const dragRef = useRef<{ id: number | null; panX: number; panY: number; moved: boolean }>({
    id: null, panX: 0, panY: 0, moved: false,
  })
  const viewRef = useRef({ scale: 1, ox: 0, oy: 0 })
  const nodeEls = useRef<Map<number, SVGCircleElement>>(new Map())
  const edgeEls = useRef<Map<number, SVGLineElement>>(new Map())
  const labelEls = useRef<Map<number, SVGTextElement>>(new Map())
  const rootGRef = useRef<SVGGElement | null>(null)

  const nodeIndex = useMemo(() => {
    const map = new Map<number, MemoryGraphNode>()
    graph.nodes.forEach((n) => map.set(n.id, n))
    return map
  }, [graph.nodes])

  // 容器尺寸自适应（图表填充卡片，不留固定小画布）
  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const ro = new ResizeObserver((entries) => {
      const rect = entries[0]?.contentRect
      if (rect && rect.width > 0) {
        setSize({ w: Math.round(rect.width), h: rect.height > 0 ? Math.round(rect.height) : height })
      }
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [height])

  // 图数据变化：重建仿真节点（保留既有坐标，新节点绕中心铺开）
  useEffect(() => {
    const sim = simRef.current
    const cx = size.w / 2
    const cy = size.h / 2
    const radius = Math.min(size.w, size.h) / 3
    const golden = Math.PI * (3 - Math.sqrt(5))
    graph.nodes.forEach((n, i) => {
      const exist = sim.get(n.id)
      if (exist) return
      const angle = i * golden
      sim.set(n.id, {
        id: n.id,
        x: cx + radius * Math.cos(angle),
        y: cy + radius * Math.sin(angle),
        vx: 0, vy: 0, fixed: false,
      })
    })
    // 移除不再显示的节点
    const alive = new Set(graph.nodes.map((n) => n.id))
    for (const id of Array.from(sim.keys())) {
      if (!alive.has(id)) sim.delete(id)
    }
    alphaRef.current = 1
    startLoop()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [graph, size.w, size.h])

  const tick = () => {
    const sim = simRef.current
    const nodes = Array.from(sim.values())
    const alpha = alphaRef.current
    const cx = size.w / 2
    const cy = size.h / 2

    // 斥力（n 较小，直接两两计算）
    for (let i = 0; i < nodes.length; i++) {
      for (let j = i + 1; j < nodes.length; j++) {
        const a = nodes[i]
        const b = nodes[j]
        let dx = b.x - a.x
        let dy = b.y - a.y
        let dist2 = dx * dx + dy * dy
        if (dist2 < 1) {
          dx = (i - j) * 0.7 + 0.5
          dy = (j - i) * 0.7 + 0.5
          dist2 = dx * dx + dy * dy
        }
        const force = (2600 * alpha) / dist2
        const dist = Math.sqrt(dist2)
        const fx = (dx / dist) * force
        const fy = (dy / dist) * force
        a.vx -= fx
        a.vy -= fy
        b.vx += fx
        b.vy += fy
      }
    }
    // 弹簧（边）
    for (const e of graph.edges) {
      const a = sim.get(e.from_id)
      const b = sim.get(e.to_id)
      if (!a || !b) continue
      const dx = b.x - a.x
      const dy = b.y - a.y
      const dist = Math.sqrt(dx * dx + dy * dy) || 1
      const target = 96
      const force = (dist - target) * 0.022 * alpha
      const fx = (dx / dist) * force
      const fy = (dy / dist) * force
      a.vx += fx
      a.vy += fy
      b.vx -= fx
      b.vy -= fy
    }
    // 向心 + 阻尼 + 位置积分
    for (const n of nodes) {
      if (n.fixed) {
        n.vx = 0
        n.vy = 0
        continue
      }
      n.vx += (cx - n.x) * 0.008 * alpha
      n.vy += (cy - n.y) * 0.008 * alpha
      n.vx *= 0.82
      n.vy *= 0.82
      n.x += n.vx
      n.y += n.vy
    }
    alphaRef.current = alpha * 0.97
  }

  const applyPositions = () => {
    const { scale, ox, oy } = viewRef.current
    if (rootGRef.current) {
      rootGRef.current.setAttribute('transform', `translate(${ox} ${oy}) scale(${scale})`)
    }
    for (const [id, el] of nodeEls.current) {
      const n = simRef.current.get(id)
      if (!n) continue
      el.setAttribute('cx', String(n.x))
      el.setAttribute('cy', String(n.y))
    }
    for (const [id, el] of labelEls.current) {
      const n = simRef.current.get(id)
      if (!n) continue
      el.setAttribute('x', String(n.x))
      el.setAttribute('y', String(n.y + 15))
    }
    for (const [id, el] of edgeEls.current) {
      const a = simRef.current.get(id)
      if (!a) continue
      const e = edgeById.get(id)
      if (!e) continue
      const b = simRef.current.get(e.to_id)
      if (!b) continue
      el.setAttribute('x1', String(a.x))
      el.setAttribute('y1', String(a.y))
      el.setAttribute('x2', String(b.x))
      el.setAttribute('y2', String(b.y))
    }
  }

  const edgeById = useMemo(() => {
    const map = new Map<number, MemoryGraphEdge>()
    // 边元素以 from_id 为键不可靠（同一节点多条边），用「边 id」存；见 render 处。
    graph.edges.forEach((e) => map.set(e.id, e))
    return map
  }, [graph.edges])

  const startLoop = () => {
    if (rafRef.current != null) return
    const step = () => {
      rafRef.current = null
      const dragging = dragRef.current.id != null
      if (alphaRef.current > 0.006 || dragging) {
        tick()
        applyPositions()
        rafRef.current = requestAnimationFrame(step)
      } else {
        applyPositions()
      }
    }
    rafRef.current = requestAnimationFrame(step)
  }

  useEffect(() => () => {
    if (rafRef.current != null) cancelAnimationFrame(rafRef.current)
  }, [])

  // ---- 指针交互 ----

  const toWorld = (clientX: number, clientY: number) => {
    const rect = containerRef.current?.getBoundingClientRect()
    const { scale, ox, oy } = viewRef.current
    const x = ((clientX - (rect?.left ?? 0)) - ox) / scale
    const y = ((clientY - (rect?.top ?? 0)) - oy) / scale
    return { x, y }
  }

  const onNodePointerDown = (id: number, e: ReactPointerEvent) => {
    e.stopPropagation()
    const w = toWorld(e.clientX, e.clientY)
    const n = simRef.current.get(id)
    if (n) {
      n.fixed = true
      n.x = w.x
      n.y = w.y
    }
    dragRef.current = { id, panX: e.clientX, panY: e.clientY, moved: false }
    alphaRef.current = Math.max(alphaRef.current, 0.35)
    startLoop()
  }

  const onSvgPointerDown = (e: ReactPointerEvent) => {
    dragRef.current = { id: null, panX: e.clientX, panY: e.clientY, moved: false }
  }

  const onPointerMove = (e: ReactPointerEvent) => {
    const drag = dragRef.current
    if (drag.id != null) {
      const w = toWorld(e.clientX, e.clientY)
      const n = simRef.current.get(drag.id)
      if (n) {
        n.x = w.x
        n.y = w.y
      }
      drag.moved = true
      startLoop()
      return
    }
    if (e.buttons === 1) {
      const dx = e.clientX - drag.panX
      const dy = e.clientY - drag.panY
      if (Math.abs(dx) + Math.abs(dy) > 2) {
        drag.moved = true
        viewRef.current.ox += dx
        viewRef.current.oy += dy
        drag.panX = e.clientX
        drag.panY = e.clientY
        applyPositions()
      }
    }
  }

  const onPointerUp = (id?: number) => {
    const drag = dragRef.current
    if (drag.id != null) {
      const n = simRef.current.get(drag.id)
      if (n) n.fixed = false
      if (!drag.moved && id != null) onSelect?.(id)
    }
    dragRef.current = { id: null, panX: 0, panY: 0, moved: false }
  }

  const onWheel = (e: ReactWheelEvent) => {
    const rect = containerRef.current?.getBoundingClientRect()
    const mx = e.clientX - (rect?.left ?? 0)
    const my = e.clientY - (rect?.top ?? 0)
    const view = viewRef.current
    const next = Math.min(3, Math.max(0.4, view.scale * (e.deltaY > 0 ? 0.9 : 1.1)))
    // 以光标为锚点缩放
    view.ox = mx - ((mx - view.ox) / view.scale) * next
    view.oy = my - ((my - view.oy) / view.scale) * next
    view.scale = next
    applyPositions()
  }

  // 图例（按出现的命名空间）
  const legend = useMemo(() => {
    const seen = new Map<string, number>()
    graph.nodes.forEach((n) => seen.set(n.namespace, (seen.get(n.namespace) ?? 0) + 1))
    return Array.from(seen.entries()).sort((a, b) => b[1] - a[1])
  }, [graph.nodes])

  return (
    <div ref={containerRef} style={{ position: 'relative', width: '100%', height }}>
      <svg
        width={size.w}
        height={size.h}
        style={{ display: 'block', cursor: 'grab', touchAction: 'none' }}
        onPointerDown={onSvgPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={() => onPointerUp()}
        onPointerLeave={() => onPointerUp()}
        onWheel={onWheel}
      >
        <g ref={rootGRef}>
          {graph.edges.map((e) => {
            const style = RELATION_STYLE[e.relation] ?? RELATION_STYLE.related
            return (
              <line
                key={e.id}
                ref={(el) => {
                  if (el) edgeEls.current.set(e.id, el)
                  else edgeEls.current.delete(e.id)
                }}
                stroke={style.color}
                strokeDasharray={style.dash}
                strokeWidth={1.6}
                opacity={0.55}
              />
            )
          })}
          {graph.nodes.map((n) => {
            const r = 7 + n.importance * 1.6 + Math.min(n.degree, 5) * 1.4
            const selected = selectedId === n.id
            return (
              <g key={n.id}>
                <circle
                  ref={(el) => {
                    if (el) nodeEls.current.set(n.id, el)
                    else nodeEls.current.delete(n.id)
                  }}
                  r={r}
                  fill={namespaceColor(n.namespace)}
                  stroke={selected ? 'var(--accent)' : n.pinned ? '#e8c33d' : 'transparent'}
                  strokeWidth={selected ? 3 : n.pinned ? 2 : 1}
                  style={{ cursor: 'pointer' }}
                  onPointerDown={(e) => onNodePointerDown(n.id, e)}
                  onPointerUp={() => onPointerUp(n.id)}
                  onPointerEnter={() => setHover({ id: n.id, x: 0, y: 0 })}
                  onPointerLeave={() => setHover(null)}
                />
                {(r >= 11 || selected || hover?.id === n.id) && (
                  <text
                    ref={(el) => {
                      if (el) labelEls.current.set(n.id, el)
                      else labelEls.current.delete(n.id)
                    }}
                    textAnchor="middle"
                    fontSize={11}
                    fill="var(--text-secondary)"
                    style={{ pointerEvents: 'none', userSelect: 'none' }}
                  >
                    {n.title.length > 12 ? `${n.title.slice(0, 12)}…` : n.title}
                  </text>
                )}
              </g>
            )
          })}
        </g>
      </svg>

      {hover && nodeIndex.get(hover.id) && (
        <div
          style={{
            position: 'absolute', left: 12, top: 12, pointerEvents: 'none',
            background: 'var(--bg-elevated, rgba(20,28,39,0.92))', borderRadius: 8,
            padding: '8px 12px', fontSize: 12, maxWidth: 280,
            border: '1px solid rgba(128,148,168,0.25)',
          }}
        >
          <div style={{ fontWeight: 600 }}>{nodeIndex.get(hover.id)!.title}</div>
          <div style={{ opacity: 0.75, marginTop: 2 }}>
            {nodeIndex.get(hover.id)!.namespace || '未分类'} · 关联 {nodeIndex.get(hover.id)!.degree} 条
          </div>
        </div>
      )}

      {legend.length > 0 && (
        <div style={{ position: 'absolute', right: 12, top: 12, display: 'flex', gap: 10, flexWrap: 'wrap', maxWidth: '60%' }}>
          {legend.map(([ns, count]) => (
            <span key={ns || '未分类'} style={{ display: 'inline-flex', alignItems: 'center', gap: 5, fontSize: 12, opacity: 0.85 }}>
              <span style={{ width: 9, height: 9, borderRadius: '50%', background: namespaceColor(ns) }} />
              {ns || '未分类'}({count})
            </span>
          ))}
        </div>
      )}

      {graph.truncated && (
        <div style={{ position: 'absolute', left: 12, bottom: 10, fontSize: 12, opacity: 0.7 }}>
          节点过多已裁剪，可按命名空间过滤查看
        </div>
      )}
    </div>
  )
}

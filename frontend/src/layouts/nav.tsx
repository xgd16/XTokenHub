import type { ReactNode } from 'react'
import {
  AppstoreOutlined,
  BulbOutlined,
  ClusterOutlined,
  DashboardOutlined,
  FileTextOutlined,
  KeyOutlined,
  NodeIndexOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import type { MemoryNamespaceView } from '../api/memory'
import { NS_ALL, NS_NONE, nsPath } from '../utils/memoryMeta'
import { bestMenuPath } from '../utils/menuPath'

/** 导航叶子（可点击路由）。code 为仪表编号，侧栏行尾与顶栏铭牌共用。 */
export interface NavLeaf {
  path: string
  label: string
  icon: ReactNode
  code: string
  /** 快速跳转的匹配关键词（别名/英文），空格分隔。 */
  keywords: string
  /** 行尾计数（记忆库命名空间条数）。 */
  count?: number
}

/** 导航节点：叶子，或带子项的分组（如「记忆库」）。 */
export interface NavNode extends NavLeaf {
  children?: NavLeaf[]
}

/** 侧栏分组：微标题（code + 中文名）+ 若干节点。 */
export interface NavGroup {
  code: string
  label: string
  nodes: NavNode[]
}

function leaf(path: string, label: string, icon: ReactNode, code: string, keywords: string): NavLeaf {
  return { path, label, icon, code, keywords }
}

/** 子项（记忆库下级）统一用菱形标记，与导航行的图标位对齐。 */
const NS_GLYPH = <span className="nav-glyph-dot" aria-hidden="true" />

/** 记忆库节点：全部/未分类 + 各命名空间 + 命名空间管理。 */
function memoryNode(namespaces: MemoryNamespaceView[]): NavNode {
  const children: NavLeaf[] = [
    leaf(nsPath(NS_ALL), '全部记忆', NS_GLYPH, '', 'all 全部'),
    leaf(nsPath(NS_NONE), '未分类', NS_GLYPH, '', 'none 未分类 inbox'),
  ]
  for (const n of namespaces) {
    children.push({
      path: nsPath(n.name),
      label: n.name,
      icon: NS_GLYPH,
      code: '',
      keywords: `namespace ${n.name}`,
      count: n.count,
    })
  }
  children.push(leaf('/memories', '命名空间管理', NS_GLYPH, '', 'manage 管理'))
  return {
    ...leaf('/memories', '记忆库', <NodeIndexOutlined />, '08', 'memory 记忆 长期记忆'),
    children,
  }
}

/** 导航骨架：按「观测 / 网关 / 模型 / 记忆」分组，编号即仪表位号。 */
export function buildNav(namespaces: MemoryNamespaceView[] = []): NavGroup[] {
  return [
    {
      code: 'OBS',
      label: '观测',
      nodes: [
        leaf('/', '仪表盘', <DashboardOutlined />, '01', 'dashboard 概览 首页 home'),
        leaf('/logs', '请求日志', <FileTextOutlined />, '02', 'log logs 请求 流水'),
        leaf('/models', '模型用量', <ThunderboltOutlined />, '03', 'model usage 用量 token'),
      ],
    },
    {
      code: 'GATE',
      label: '网关',
      nodes: [
        leaf('/channels', '渠道管理', <ClusterOutlined />, '04', 'channel 渠道 上游 upstream'),
        leaf('/keys', 'API Keys', <KeyOutlined />, '05', 'key keys 密钥 apikey'),
      ],
    },
    {
      code: 'MOD',
      label: '模型',
      nodes: [
        leaf('/custom-models', '自定义模型', <AppstoreOutlined />, '06', 'custom model 自定义模型'),
        leaf('/reasoning-rewrites', '思维改写', <BulbOutlined />, '07', 'reasoning 思维 改写'),
      ],
    },
    {
      code: 'MEM',
      label: '记忆',
      nodes: [memoryNode(namespaces)],
    },
  ]
}

/**
 * 展平所有可点击路由（仅叶子；带子项的父节点是展开开关，不参与高亮匹配）。
 */
export function navLeaves(groups: NavGroup[]): NavLeaf[] {
  const out: NavLeaf[] = []
  for (const g of groups) {
    for (const n of g.nodes) out.push(...(n.children ?? [n]))
  }
  return out
}

/** 路径是否为某导航项的当前位置（按段边界匹配，`/memories` 不匹配 `/memories-foo`）。 */
export function isActive(pathname: string, path: string): boolean {
  return pathname === path || (path !== '/' && pathname.startsWith(`${path}/`))
}

/** 当前应高亮的导航路径（叶子中最长前缀，未命中回退 `/`）。 */
export function activePath(pathname: string, leaves: NavLeaf[]): string {
  return bestMenuPath(pathname, leaves.map((l) => l.path))
}

/** 顶栏铭牌：位号 + 标题 + 面包屑尾迹（记忆库下的命名空间）。 */
export interface Place {
  code: string
  title: string
  trail: string[]
}

function safeDecode(seg: string): string {
  try {
    return decodeURIComponent(seg)
  } catch {
    return seg
  }
}

/** 由当前 pathname 推导顶栏铭牌。 */
export function placeFor(pathname: string, groups: NavGroup[]): Place {
  const leaves = navLeaves(groups)
  const path = activePath(pathname, leaves)
  const current = leaves.find((l) => l.path === path)
  let code = current?.code || '--'
  let title = current?.label ?? '仪表盘'
  const trail: string[] = []

  if (pathname === '/memories' || pathname.startsWith('/memories/')) {
    // 记忆库页统一以「记忆库」为标题，命名空间下沉为面包屑尾迹
    for (const g of groups) {
      for (const n of g.nodes) {
        if (n.children?.some((c) => c.path === path)) {
          code = n.code
          title = n.label
        }
      }
    }
    const seg = safeDecode(pathname.slice('/memories'.length).replace(/^\//, ''))
    if (seg) trail.push(seg === NS_ALL ? '全部记忆' : seg === NS_NONE ? '未分类' : seg)
  }

  return { code, title, trail }
}

/** 快速跳转命中项。 */
export interface NavHit {
  leaf: NavLeaf
  group: string
  groupCode: string
  score: number
}

function scoreLeaf(l: NavLeaf, q: string): number {
  const label = l.label.toLowerCase()
  if (label.startsWith(q)) return 100 - Math.min(label.length, 40) * 0.1
  if (label.includes(q)) return 80
  if (l.keywords.toLowerCase().includes(q)) return 60
  if (l.path.toLowerCase().includes(q)) return 50
  return 0
}

/** 按关键词过滤导航（空查询返回全量，保持骨架顺序）。 */
export function searchNav(groups: NavGroup[], query: string): NavHit[] {
  const q = query.trim().toLowerCase()
  const hits: NavHit[] = []
  for (const g of groups) {
    for (const n of g.nodes) {
      const candidates: NavLeaf[] = n.children ? n.children : [n]
      // 父项命中时，其子项一并召回（如搜「记忆」带出「全部记忆」）
      const parentHit = q && n.label.toLowerCase().includes(q) ? 10 : 0
      for (const c of candidates) {
        // 空查询时全部保留（得分 1，仅用于维持骨架顺序）
        if (!q) {
          hits.push({ leaf: c, group: g.label, groupCode: g.code, score: 1 })
          continue
        }
        const own = scoreLeaf(c, q)
        // 自身不命中时，只有父项命中才召回（搜「记忆」带出「未分类」）
        if (own <= 0 && parentHit <= 0) continue
        hits.push({
          leaf: c,
          group: g.label,
          groupCode: g.code,
          score: own + parentHit + (own > 0 && c === n ? 6 : 0),
        })
      }
    }
  }
  if (q) hits.sort((a, b) => b.score - a.score)
  return hits
}
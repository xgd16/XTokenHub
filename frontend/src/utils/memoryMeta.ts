import { MEMORY_KINDS, type MemoryKind } from '../api/memory'
import { hashColor } from './format'

/** 虚拟命名空间保留值：全部记忆（后端同为保留名，禁止用作命名空间名）。 */
export const NS_ALL = 'all'
/** 虚拟命名空间保留值：未分类（后端过滤保留值）。 */
export const NS_NONE = 'none'

export const kindLabel = (k: MemoryKind) => MEMORY_KINDS.find((x) => x.value === k)?.label ?? k

export const kindColor: Record<string, string> = {
  note: 'default', fact: 'blue', decision: 'purple', convention: 'geekblue',
  preference: 'cyan', task: 'orange', lesson: 'magenta',
}

export const statusLabel: Record<string, string> = {
  active: '生效', superseded: '被替代', archived: '归档', deleted: '已删除',
}

export const statusColor: Record<string, string> = {
  active: 'green', superseded: 'gold', archived: 'default', deleted: 'red',
}

export const nsSourceLabel: Record<string, string> = {
  explicit: '接入端指定', llm: 'LLM 分类', manual: '人工', '': '未分类',
}

/** 命名空间在 URL 中的单段路径（`none`/`all` 保留值原样使用）。 */
export function nsPath(ns: string): string {
  return `/memories/${encodeURIComponent(ns)}`
}

/** `:ns` 路由参数 → 命名空间展示名。 */
export function nsTitle(ns: string): string {
  if (ns === NS_ALL) return '全部记忆'
  if (ns === NS_NONE) return '未分类'
  return ns
}

/** `:ns` 路由参数 → 查询过滤值（undefined = 跨全部命名空间）。 */
export function nsFilter(ns: string): string | undefined {
  if (ns === NS_ALL) return undefined
  return ns
}

/** 命名空间身份色：按名字哈希取稳定色相（与关系图节点同源，全局可辨识）；未分类用中性灰。 */
export function namespaceColor(ns: string): string {
  return hashColor(ns)
}

/** 一句话摘要（表格列用）。 */
export function excerpt(m: { content: string }): string {
  const line = m.content.split('\n').find((s) => s.trim()) ?? ''
  return line.length > 60 ? `${line.slice(0, 60)}…` : line
}

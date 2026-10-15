import { request, type PageData } from './http'

/** 记忆类型（受控枚举）。 */
export type MemoryKind = 'note' | 'fact' | 'decision' | 'convention' | 'preference' | 'task' | 'lesson'

export const MEMORY_KINDS: { value: MemoryKind; label: string }[] = [
  { value: 'note', label: '一般记录' },
  { value: 'fact', label: '事实' },
  { value: 'decision', label: '决策' },
  { value: 'convention', label: '约定' },
  { value: 'preference', label: '偏好' },
  { value: 'task', label: '任务进展' },
  { value: 'lesson', label: '经验教训' },
]

export type MemoryStatus = 'active' | 'superseded' | 'archived' | 'deleted'
export type MemoryNamespaceSource = '' | 'explicit' | 'llm' | 'manual'

/** 一条记忆（与后端 model.Memory 对齐）。 */
export interface MemoryItem {
  id: number
  title: string
  content: string
  namespace: string
  namespace_source: MemoryNamespaceSource
  kind: MemoryKind
  tags: string
  importance: number
  pinned: boolean
  status: MemoryStatus
  superseded_by: number
  expires_at?: string | null
  source: string
  created_by: string
  source_session?: string
  source_ref?: string
  access_count: number
  last_accessed_at?: string | null
  created_at: string
  updated_at: string
}

/** 检索命中（含综合分）。 */
export type MemoryHit = MemoryItem & { score: number }

/** 命名空间命中分布。 */
export interface MemoryFacet {
  namespace: string
  count: number
}

export type SearchMode = 'fts' | 'fts_loose' | 'like' | 'list'

export interface MemorySearchResult {
  items: MemoryHit[]
  count: number
  namespaces: MemoryFacet[]
  mode: SearchMode
}

export type MemoryLinkRelation = 'related' | 'supersedes' | 'conflicts' | 'derived_from'

export const MEMORY_LINK_RELATIONS: { value: MemoryLinkRelation; label: string }[] = [
  { value: 'related', label: '相关' },
  { value: 'supersedes', label: '替代' },
  { value: 'conflicts', label: '冲突' },
  { value: 'derived_from', label: '派生自' },
]

export interface MemoryLink {
  id: number
  from_id: number
  to_id: number
  relation: MemoryLinkRelation
  note: string
  created_by: string
  created_at: string
}

export interface MemoryGraphNode {
  id: number
  title: string
  namespace: string
  kind: MemoryKind
  importance: number
  pinned: boolean
  access_count: number
  degree: number
  updated_at: string
}

export interface MemoryGraphEdge {
  id: number
  from_id: number
  to_id: number
  relation: MemoryLinkRelation
  note: string
}

export interface MemoryGraph {
  nodes: MemoryGraphNode[]
  edges: MemoryGraphEdge[]
  truncated: boolean
}

export interface MemoryNamespaceView {
  id: number
  name: string
  description: string
  created_by: string
  count: number
}

export interface MemoryStats {
  /** 生效记忆总数；口径 = active，必须等于 uncategorized + Σ by_namespace。 */
  total: number
  active: number
  uncategorized: number
  /** 被替代数（不在 total 内）。 */
  superseded: number
  /** 归档数（不在 total 内）。 */
  archived: number
  /** 软删除数（回收站，不在 total 内，也不被「全部状态」筛出）。 */
  deleted: number
  links: number
  by_kind: Record<string, number>
  by_namespace: Record<string, number>
}

/** 分类任务状态（GET /api/v1/memories/classify）。 */
export interface ClassifyStatus {
  status: '' | 'running' | 'done' | 'failed'
  total: number
  done: number
  assigned: number
  new_namespaces: number
  message: string
  started_at?: string | null
  finished_at?: string | null
}

/** 分类配置 + 最近任务状态（GET /api/v1/memories/classify-config）。 */
export interface ClassifyConfig extends ClassifyStatus {
  channel_id: number
  model: string
}

export interface MemoryInput {
  title?: string
  content: string
  namespace?: string
  kind?: MemoryKind
  tags?: string[]
  importance?: number
  pinned?: boolean
  expires_at?: string | null
  source_ref?: string
  supersedes?: number[]
  links?: { to_id: number; relation?: MemoryLinkRelation; note?: string }[]
}

/** 新建记忆时「先攒着、随提交一起发」的一条关系。 */
export interface PendingMemoryLink {
  to_id: number
  relation: MemoryLinkRelation
  /** 目标记忆标题，仅用于提交前展示，不发给后端。 */
  title: string
}

/**
 * 待建关系 → create 入参。
 * 「替代」必须走 supersedes 而不是普通 link：它除了建边（new → old，supersedes）
 * 还会把旧记忆置为 superseded 并回填 superseded_by，只发 links 只会多一条边、
 * 状态不变。派生自/相关/冲突才是普通有向边。
 */
export function pendingLinksToInput(
  pending: PendingMemoryLink[],
): { links: { to_id: number; relation: MemoryLinkRelation }[]; supersedes: number[] } {
  const supersedes: number[] = []
  const links: { to_id: number; relation: MemoryLinkRelation }[] = []
  const seen = new Set<number>()
  for (const p of pending) {
    // 同一目标只认一条（重复提交后端虽幂等，但列表上会出现重复行）
    if (seen.has(p.to_id)) continue
    seen.add(p.to_id)
    if (p.relation === 'supersedes') supersedes.push(p.to_id)
    else links.push({ to_id: p.to_id, relation: p.relation })
  }
  return { links, supersedes }
}

export interface MemoryUpdate {
  title?: string
  content?: string
  namespace?: string
  kind?: MemoryKind
  tags?: string[]
  importance?: number
  pinned?: boolean
  status?: 'active' | 'archived'
  expires_at?: string | null
  clear_expires?: boolean
}

export interface MemoryQuery {
  q?: string
  namespace?: string
  kind?: MemoryKind | ''
  tags?: string[]
  status?: string
  pinned?: boolean
  limit?: number
}

/** 标签以逗号分隔存库，解析成数组展示。 */
export function tagsOf(m: { tags: string }): string[] {
  return m.tags
    .split(',')
    .map((t) => t.trim())
    .filter(Boolean)
}

export const memoryApi = {
  list: (params: MemoryQuery & { page?: number; per_page?: number; sort?: string }) =>
    request<PageData<MemoryItem>>({ url: '/api/v1/memories', method: 'GET', params }),
  search: (params: MemoryQuery) =>
    request<MemorySearchResult>({ url: '/api/v1/memories/search', method: 'GET', params }),
  get: (id: number) =>
    request<{ memory: MemoryItem; links: MemoryLink[] }>({ url: `/api/v1/memories/${id}`, method: 'GET' }),
  create: (input: MemoryInput) =>
    request<{ memory: MemoryItem; duplicated: boolean; superseded: number[]; links: MemoryLink[]; auto_links?: MemoryLink[] }>({
      url: '/api/v1/memories', method: 'POST', data: input,
    }),
  update: (id: number, input: MemoryUpdate) =>
    request<MemoryItem>({ url: `/api/v1/memories/${id}`, method: 'PATCH', data: input }),
  forget: (id: number) =>
    request<{ id: number; status: string }>({ url: `/api/v1/memories/${id}`, method: 'DELETE' }),
  supersede: (id: number, successorId: number) =>
    request<{ id: number; superseded_by: number }>({
      url: `/api/v1/memories/${id}/supersede`, method: 'POST', data: { successor_id: successorId },
    }),
  addLink: (id: number, input: { to_id: number; relation?: MemoryLinkRelation; note?: string }) =>
    request<MemoryLink>({ url: `/api/v1/memories/${id}/links`, method: 'POST', data: input }),
  removeLink: (id: number, linkId: number) =>
    request<{ id: number }>({ url: `/api/v1/memories/${id}/links/${linkId}`, method: 'DELETE' }),
  graph: (params: MemoryQuery & { limit?: number }) =>
    request<MemoryGraph>({ url: '/api/v1/memories/graph', method: 'GET', params }),
  /** 全库重算自动关系（只回收/重建 system 自动边，手动关系不受影响）。 */
  rebuildAutoLinks: () =>
    request<{ processed: number; created: number }>({ url: '/api/v1/memories/auto-link', method: 'POST' }),
  stats: () => request<MemoryStats>({ url: '/api/v1/memories/stats', method: 'GET' }),

  namespaces: () =>
    request<{ items: MemoryNamespaceView[] }>({ url: '/api/v1/memory-namespaces', method: 'GET' }),
  createNamespace: (input: { name: string; description?: string }) =>
    request<MemoryNamespaceView>({ url: '/api/v1/memory-namespaces', method: 'POST', data: input }),
  updateNamespace: (id: number, input: { description: string }) =>
    request<MemoryNamespaceView>({ url: `/api/v1/memory-namespaces/${id}`, method: 'PATCH', data: input }),
  renameNamespace: (from: string, to: string) =>
    request<{ from: string; to: string; moved: number }>({
      url: '/api/v1/memory-namespaces/rename', method: 'POST', data: { from, to },
    }),
  mergeNamespaces: (from: string, to: string) =>
    request<{ from: string; to: string; moved: number }>({
      url: '/api/v1/memory-namespaces/merge', method: 'POST', data: { from, to },
    }),
  deleteNamespace: (id: number) =>
    request<{ id: number; moved: number }>({ url: `/api/v1/memory-namespaces/${id}`, method: 'DELETE' }),
  batchMove: (ids: number[], namespace: string) =>
    request<{ moved: number }>({ url: '/api/v1/memories/batch-move', method: 'POST', data: { ids, namespace } }),

  classifyConfig: () =>
    request<ClassifyConfig>({ url: '/api/v1/memories/classify-config', method: 'GET' }),
  updateClassifyConfig: (input: { channel_id: number; model: string }) =>
    request<{ channel_id: number; model: string }>({
      url: '/api/v1/memories/classify-config', method: 'PUT', data: input,
    }),
  triggerClassify: () =>
    request<ClassifyStatus>({ url: '/api/v1/memories/classify', method: 'POST' }),
  classifyStatus: () =>
    request<ClassifyStatus>({ url: '/api/v1/memories/classify', method: 'GET' }),
}

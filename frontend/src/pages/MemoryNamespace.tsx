import { useCallback, useEffect, useState } from 'react'
import {
  App, Button, Empty, Form, Input, Popconfirm, Rate, Segmented, Select, Space, Table, Tag,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  ArrowLeftOutlined, DeleteOutlined, EditOutlined, PlusOutlined, ReloadOutlined,
} from '@ant-design/icons'
import dayjs from 'dayjs'
import { useNavigate, useParams } from 'react-router'
import {
  MEMORY_KINDS, memoryApi, tagsOf,
  type MemoryGraph as GraphData, type MemoryHit, type MemoryItem, type MemoryKind, type MemoryLink,
} from '../api/memory'
import MemoryDrawer from '../components/MemoryDrawer'
import MemoryGraph, { namespaceColor } from '../components/MemoryGraph'
import { obsColorStyle } from '../utils/format'
import {
  NS_ALL, NS_NONE, excerpt, kindColor, kindLabel, nsFilter, nsTitle, statusColor, statusLabel,
} from '../utils/memoryMeta'
import { refreshMemoryNamespaces, useMemoryNamespaces } from '../utils/useMemoryNamespaces'

/**
 * 二级页面：某个命名空间下的具体记忆（/memories/:ns）。
 * ns 为保留值 all=全部记忆、none=未分类，其余为命名空间名。
 */
export default function MemoryNamespace() {
  const { ns = NS_ALL } = useParams<{ ns: string }>()
  const navigate = useNavigate()
  const { message } = App.useApp()
  const namespaces = useMemoryNamespaces()

  const isAll = ns === NS_ALL
  const isNone = ns === NS_NONE
  const current = !isAll && !isNone ? namespaces.find((n) => n.name === ns) : undefined
  /** 头部身份色：与总览卡片/关系图同源。 */
  const nsColor = isAll ? 'var(--accent)' : isNone ? 'var(--amber)' : namespaceColor(ns)

  // ---- 列表/检索 ----
  const [q, setQ] = useState('')
  const [debouncedQ, setDebouncedQ] = useState('')
  const [kind, setKind] = useState<MemoryKind | '' | undefined>()
  const [status, setStatus] = useState('active')
  /** 「全部记忆」页上的进一步收窄（undefined=全部；none=未分类；其余=命名空间名）。 */
  const [nsRefine, setNsRefine] = useState<string | undefined>()
  const [view, setView] = useState<'list' | 'graph'>('list')
  const [items, setItems] = useState<MemoryHit[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [loading, setLoading] = useState(false)
  const [searchMode, setSearchMode] = useState<string>('list')
  const [selected, setSelected] = useState<number[]>([])

  // ---- 关系图 ----
  const [graph, setGraph] = useState<GraphData | null>(null)
  const [graphLoading, setGraphLoading] = useState(false)

  // ---- 抽屉（详情/编辑） ----
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [editing, setEditing] = useState<MemoryItem | null>(null)
  const [links, setLinks] = useState<MemoryLink[]>([])
  const [form] = Form.useForm()

  // 切换命名空间（同一路由组件实例复用）时重置检索与选择
  useEffect(() => {
    setQ(''); setDebouncedQ(''); setPage(1); setSelected([]); setNsRefine(undefined)
  }, [ns])

  // 防抖搜索词
  useEffect(() => {
    const t = setTimeout(() => setDebouncedQ(q), 300)
    return () => clearTimeout(t)
  }, [q])

  const filter = isAll ? nsRefine : nsFilter(ns)

  const loadList = useCallback(async () => {
    setLoading(true)
    try {
      const params = {
        q: debouncedQ || undefined,
        namespace: filter,
        kind: kind || undefined,
        status,
        page,
        per_page: pageSize,
      }
      if (debouncedQ) {
        const res = await memoryApi.search(params)
        setItems(res.items)
        setTotal(res.count)
        setSearchMode(res.mode)
      } else {
        const res = await memoryApi.list(params)
        setItems(res.items.map((m) => ({ ...m, score: 0 })))
        setTotal(res.total)
        setSearchMode('list')
      }
    } catch (e) {
      message.error((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [debouncedQ, filter, kind, status, page, pageSize, message])

  useEffect(() => {
    loadList()
  }, [loadList])

  const loadGraph = useCallback(async () => {
    setGraphLoading(true)
    try {
      const res = await memoryApi.graph({ namespace: filter, kind: kind || undefined, status, limit: 150 })
      setGraph(res)
    } catch (e) {
      message.error((e as Error).message)
    } finally {
      setGraphLoading(false)
    }
  }, [filter, kind, status, message])

  useEffect(() => {
    if (view === 'graph') loadGraph()
  }, [view, loadGraph])

  // ---- 操作 ----

  const openCreate = () => {
    setEditing(null)
    setLinks([])
    form.resetFields()
    form.setFieldsValue({
      kind: 'note', importance: 3, pinned: false, tags: [],
      namespace: !isAll && !isNone ? ns : undefined,
    })
    setDrawerOpen(true)
  }

  const openDetail = async (id: number) => {
    try {
      const res = await memoryApi.get(id)
      setEditing(res.memory)
      setLinks(res.links)
      form.setFieldsValue({
        title: res.memory.title,
        content: res.memory.content,
        namespace: res.memory.namespace || undefined,
        kind: res.memory.kind,
        tags: tagsOf(res.memory),
        importance: res.memory.importance,
        pinned: res.memory.pinned,
      })
      setDrawerOpen(true)
    } catch (e) {
      message.error((e as Error).message)
    }
  }

  const save = async () => {
    const values = await form.validateFields()
    try {
      if (editing) {
        await memoryApi.update(editing.id, {
          title: values.title, content: values.content,
          namespace: values.namespace ?? '', kind: values.kind,
          tags: values.tags ?? [], importance: values.importance, pinned: values.pinned,
        })
        message.success('已保存')
      } else {
        const res = await memoryApi.create({
          title: values.title, content: values.content,
          namespace: values.namespace ?? '', kind: values.kind,
          tags: values.tags ?? [], importance: values.importance, pinned: values.pinned,
        })
        message.success(res.duplicated
          ? '同内容记忆已存在，未重复写入'
          : `已写入${res.auto_links?.length ? `，自动关联 ${res.auto_links.length} 条相似记忆` : ''}`)
      }
      setDrawerOpen(false)
      await refreshMemoryNamespaces()
      await loadList()
    } catch (e) {
      message.error((e as Error).message)
    }
  }

  const forget = async (id: number) => {
    try {
      await memoryApi.forget(id)
      message.success('已删除（软删除）')
      await refreshMemoryNamespaces()
      await loadList()
    } catch (e) {
      message.error((e as Error).message)
    }
  }

  const batchMove = async (target: string) => {
    try {
      const res = await memoryApi.batchMove(selected, target)
      message.success(`已移动 ${res.moved} 条`)
      setSelected([])
      await refreshMemoryNamespaces()
      await loadList()
    } catch (e) {
      message.error((e as Error).message)
    }
  }

  // 抽屉里的命名空间选项（含未分类）
  const drawerNsOptions = [
    ...namespaces.map((n) => ({ value: n.name, label: `${n.name}（${n.count}）` })),
    { value: '', label: '未分类' },
  ]
  // 批量移动目标：排除当前空间（含未分类页排除「未分类」本身）
  const currentValue = isAll ? undefined : isNone ? '' : ns
  const moveOptions = drawerNsOptions.filter((o) => o.value !== currentValue)

  const columns: ColumnsType<MemoryHit> = [
    {
      title: '记忆', dataIndex: 'title', key: 'title', ellipsis: true,
      render: (_, m) => (
        <Space direction="vertical" size={0}>
          <a onClick={() => openDetail(m.id)} style={{ fontWeight: 500 }}>
            {m.pinned && <Tag color="gold" style={{ marginRight: 4 }}>置顶</Tag>}
            {m.title || '（无标题）'}
            {m.score > 0 && <Tag style={{ marginLeft: 6 }}>分 {m.score.toFixed(2)}</Tag>}
          </a>
          <span style={{ fontSize: 12, opacity: 0.65 }}>{excerpt(m)}</span>
        </Space>
      ),
    },
    ...(isAll
      ? [{
          title: '命名空间', dataIndex: 'namespace', key: 'namespace', width: 130,
          render: (v: string) => (v ? <Tag color="processing">{v}</Tag> : <Tag>未分类</Tag>),
        } as ColumnsType<MemoryHit>[number]]
      : []),
    { title: '类型', dataIndex: 'kind', key: 'kind', width: 90, render: (k: MemoryKind) => <Tag color={kindColor[k]}>{kindLabel(k)}</Tag> },
    {
      title: '标签', dataIndex: 'tags', key: 'tags', width: 150,
      render: (tags: string) => (
        <Space size={2} wrap>
          {tagsOf({ tags }).map((t) => <Tag key={t} style={{ marginInlineEnd: 0 }}>{t}</Tag>)}
        </Space>
      ),
    },
    { title: '重要度', dataIndex: 'importance', key: 'importance', width: 110, render: (v: number) => <Rate disabled value={v} /> },
    { title: '访问', dataIndex: 'access_count', key: 'access_count', width: 70 },
    {
      title: '状态', dataIndex: 'status', key: 'status', width: 90,
      render: (s: string) => <Tag color={statusColor[s]}>{statusLabel[s] ?? s}</Tag>,
    },
    {
      title: '更新', dataIndex: 'updated_at', key: 'updated_at', width: 130,
      render: (v: string) => dayjs(v).format('MM-DD HH:mm'),
    },
    {
      title: '操作', key: 'action', width: 90, fixed: 'right',
      render: (_, m) => (
        <Space>
          <Button type="text" size="small" icon={<EditOutlined />} onClick={() => openDetail(m.id)} />
          <Popconfirm title="删除这条记忆？（软删除，可追溯）" onConfirm={() => forget(m.id)}>
            <Button type="text" size="small" danger icon={<DeleteOutlined />} />
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <div className="obs-page">
      {/* —— 观测台：空间标题 + 全局操作 —— */}
      <section className="obs-hero slim">
        <div className="obs-hero-inner">
          <div>
            <div className="obs-detail-title">
              <Button
                type="text"
                size="small"
                icon={<ArrowLeftOutlined />}
                onClick={() => navigate('/memories')}
                aria-label="返回命名空间总览"
              />
              <span className="obs-card-dot" aria-hidden="true" style={obsColorStyle(nsColor)} />
              <span>{nsTitle(ns)}</span>
              {current && <span className="obs-count-chip">{current.count} 条</span>}
            </div>
            {current?.description && <div className="obs-detail-desc">{current.description}</div>}
          </div>
          <div className="obs-hero-actions">
            <Button
              icon={<ReloadOutlined />}
              aria-label="刷新"
              onClick={() => { void loadList(); if (view === 'graph') void loadGraph() }}
            />
            <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>写入记忆</Button>
          </div>
        </div>
      </section>

      <section className="obs-panel">
        <Space wrap style={{ marginBottom: 12 }}>
          <Input.Search
            allowClear
            placeholder={isAll ? '全局搜索记忆（中文/英文子串）' : `在「${nsTitle(ns)}」内搜索`}
            style={{ width: 280 }}
            value={q}
            onChange={(e) => { setQ(e.target.value); setPage(1) }}
            onSearch={(v) => { setQ(v); setPage(1) }}
          />
          {isAll && (
            <Select
              allowClear
              placeholder="限定命名空间"
              style={{ width: 180 }}
              value={nsRefine}
              onChange={(v) => { setNsRefine(v); setPage(1) }}
              options={[
                ...namespaces.map((n) => ({ value: n.name, label: `${n.name}（${n.count}）` })),
                { value: NS_NONE, label: '未分类' },
              ]}
            />
          )}
          <Select
            allowClear
            placeholder="类型"
            style={{ width: 130 }}
            options={MEMORY_KINDS}
            value={kind}
            onChange={(v) => { setKind(v); setPage(1) }}
          />
          <Select
            style={{ width: 120 }}
            value={status}
            onChange={(v) => { setStatus(v); setPage(1) }}
            options={[
              { value: 'active', label: '生效' },
              { value: 'all', label: '全部状态' },
              { value: 'superseded', label: '被替代' },
              { value: 'archived', label: '归档' },
              { value: 'deleted', label: '已删除' },
            ]}
          />
          <Segmented
            value={view}
            onChange={(v) => setView(v as typeof view)}
            options={[
              { value: 'list', label: '列表' },
              { value: 'graph', label: '关系图' },
            ]}
          />
        </Space>

        {view === 'list' && (
          <>
            {selected.length > 0 && (
              <Space style={{ marginBottom: 12 }}>
                <span>已选 {selected.length} 条</span>
                <Select
                  placeholder="批量移动到命名空间"
                  style={{ width: 200 }}
                  options={moveOptions}
                  onChange={(v) => batchMove(v)}
                />
                <Popconfirm title={`删除选中 ${selected.length} 条？`} onConfirm={async () => {
                  await Promise.all(selected.map((id) => memoryApi.forget(id)))
                  setSelected([])
                  message.success('已删除')
                  await refreshMemoryNamespaces()
                  await loadList()
                }}>
                  <Button danger>批量删除</Button>
                </Popconfirm>
              </Space>
            )}
            <Table<MemoryHit>
              rowKey="id"
              size="small"
              loading={loading}
              columns={columns}
              dataSource={items}
              scroll={{ x: 980 }}
              rowSelection={{
                selectedRowKeys: selected,
                onChange: (keys) => setSelected(keys as number[]),
              }}
              pagination={{
                current: page, pageSize, total, showSizeChanger: true,
                showTotal: (t) => `共 ${t} 条${searchMode !== 'list' ? '（检索模式不分页）' : ''}`,
                onChange: (p, ps) => { setPage(p); setPageSize(ps) },
              }}
              locale={{
                emptyText: debouncedQ
                  ? <Empty description={`没有匹配「${debouncedQ}」的记忆`} />
                  : <Empty description={isAll ? '还没有记忆，点右上「写入记忆」开始' : `「${nsTitle(ns)}」下还没有记忆`} />,
              }}
            />
          </>
        )}

        {view === 'graph' && (
          <div style={{ height: 560 }}>
            {graphLoading ? (
              <div style={{ padding: 40, textAlign: 'center', opacity: 0.6 }}>加载关系图…</div>
            ) : graph && graph.nodes.length > 0 ? (
              <MemoryGraph graph={graph} onSelect={openDetail} height={560} />
            ) : (
              <Empty description="暂无节点（可先写入记忆并建立关系）" style={{ paddingTop: 120 }} />
            )}
          </div>
        )}
      </section>

      <MemoryDrawer
        open={drawerOpen}
        editing={editing}
        links={links}
        form={form}
        nsOptions={drawerNsOptions}
        onClose={() => setDrawerOpen(false)}
        onSave={save}
        onDelete={async () => {
          if (editing) {
            await forget(editing.id)
            setDrawerOpen(false)
          }
        }}
        onLinksChanged={async () => {
          if (editing) {
            const res = await memoryApi.get(editing.id)
            setLinks(res.links)
          }
        }}
        onOpenMemory={(id) => { setDrawerOpen(false); openDetail(id) }}
      />
    </div>
  )
}

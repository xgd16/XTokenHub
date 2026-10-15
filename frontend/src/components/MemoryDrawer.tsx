import { useEffect, useRef, useState } from 'react'
import {
  App, AutoComplete, Button, Drawer, Form, Input, InputNumber, Segmented, Select, Space, Switch, Tag,
} from 'antd'
import type { FormInstance } from 'antd'
import { DeleteOutlined } from '@ant-design/icons'
import dayjs from 'dayjs'
import {
  MEMORY_KINDS, MEMORY_LINK_RELATIONS, memoryApi,
  type MemoryItem, type MemoryLink, type MemoryLinkRelation, type PendingMemoryLink,
} from '../api/memory'
import { nsSourceLabel, statusColor, statusLabel } from '../utils/memoryMeta'
import { MemoryMarkdown } from './MemoryMarkdown'

interface Props {
  open: boolean
  editing: MemoryItem | null
  links: MemoryLink[]
  /** 新建模式下待建立的关系（还没落库，提交时随 create 一起发）。 */
  pendingLinks: PendingMemoryLink[]
  onPendingLinksChange: (next: PendingMemoryLink[]) => void
  form: FormInstance
  nsOptions: { value: string; label: string }[]
  onClose: () => void
  onSave: () => Promise<void> | void
  onDelete: () => Promise<void> | void
  onLinksChanged: () => Promise<void> | void
  onOpenMemory: (id: number) => void
}

/** 记忆详情/编辑抽屉（详情页与写入共用）。 */
export default function MemoryDrawer({
  open, editing, links, pendingLinks, onPendingLinksChange,
  form, nsOptions, onClose, onSave, onDelete, onLinksChanged, onOpenMemory,
}: Props) {
  const { message } = App.useApp()
  const [linkTarget, setLinkTarget] = useState<number | null>(null)
  const [linkTargetTitle, setLinkTargetTitle] = useState('')
  const [linkRelation, setLinkRelation] = useState<MemoryLinkRelation>('related')
  const [candidates, setCandidates] = useState<{ value: string; label: string }[]>([])
  const searchTimer = useRef<number | null>(null)
  // 正文视图模式：已有记忆默认渲染预览，新建默认编辑
  const [mode, setMode] = useState<'preview' | 'edit'>('edit')
  useEffect(() => {
    if (open) setMode(editing ? 'preview' : 'edit')
  }, [open, editing])
  // 预览实时跟随表单内容（含未保存的修改）
  const watchedContent = Form.useWatch('content', form)

  const searchCandidates = (kw: string) => {
    if (searchTimer.current) window.clearTimeout(searchTimer.current)
    searchTimer.current = window.setTimeout(async () => {
      if (!kw.trim()) return
      try {
        const res = await memoryApi.search({ q: kw, limit: 10 })
        setCandidates(res.items.map((m) => ({ value: String(m.id), label: `${m.id} · ${m.title}` })))
      } catch {
        /* 搜索失败忽略 */
      }
    }, 300)
  }

  const resetLinkDraft = () => {
    setLinkTarget(null)
    setLinkTargetTitle('')
    setLinkRelation('related')
  }

  const relationLabel = (r: MemoryLinkRelation) =>
    MEMORY_LINK_RELATIONS.find((x) => x.value === r)?.label ?? r

  const submitLink = async () => {
    if (!linkTarget) {
      message.warning('请先选择目标记忆')
      return
    }
    // 新建：先攒进待建列表，随 create 一次提交（后端建边时新记忆才有 id）
    if (!editing) {
      if (pendingLinks.some((p) => p.to_id === linkTarget)) {
        message.warning('该记忆已在待建关系里')
        return
      }
      onPendingLinksChange([...pendingLinks, { to_id: linkTarget, relation: linkRelation, title: linkTargetTitle }])
      resetLinkDraft()
      return
    }
    try {
      await memoryApi.addLink(editing.id, { to_id: linkTarget, relation: linkRelation })
      message.success('已建立关系')
      resetLinkDraft()
      await onLinksChanged()
    } catch (e) {
      message.error((e as Error).message)
    }
  }

  return (
    <Drawer
      title={editing ? `记忆 #${editing.id}` : '写入记忆'}
      width={640}
      open={open}
      onClose={onClose}
      extra={
        <Space>
          {editing && <Button danger icon={<DeleteOutlined />} onClick={onDelete}>删除</Button>}
          <Button type="primary" onClick={onSave}>{editing ? '保存' : '写入'}</Button>
        </Space>
      }
    >
      <Form form={form} layout="vertical">
        <Form.Item name="title" label="标题（缺省取正文首行）">
          <Input placeholder="简短标题" />
        </Form.Item>
        <Form.Item
          name="content"
          label={
            <Space size={10}>
              <span>正文</span>
              <Segmented
                size="small"
                value={mode}
                onChange={(v) => setMode(v as 'preview' | 'edit')}
                options={[
                  { value: 'preview', label: '预览' },
                  { value: 'edit', label: '编辑' },
                ]}
              />
            </Space>
          }
          rules={[{ required: true, message: '请填写记忆正文' }]}
          style={mode === 'preview' ? { marginBottom: 8 } : undefined}
        >
          <Input.TextArea
            rows={9}
            style={mode === 'preview' ? { display: 'none' } : undefined}
            placeholder="用 Markdown 记录：## 小节、- 列表、`代码`、表格，控制台会渲染展示"
          />
        </Form.Item>
        {mode === 'preview' && (
          <div className="md-pane" style={{ marginBottom: 16 }}>
            {watchedContent ? (
              <MemoryMarkdown content={String(watchedContent)} />
            ) : (
              <span style={{ color: 'var(--text-faint)', fontSize: 12 }}>（空正文）</span>
            )}
          </div>
        )}
        <Space size={16} wrap>
          <Form.Item name="namespace" label="命名空间">
            <Select
              allowClear
              showSearch
              style={{ width: 220 }}
              placeholder="不选 = 未分类"
              options={nsOptions}
              optionFilterProp="label"
            />
          </Form.Item>
          <Form.Item name="kind" label="类型">
            <Select style={{ width: 140 }} options={MEMORY_KINDS} />
          </Form.Item>
          <Form.Item name="importance" label="重要度">
            <InputNumber min={1} max={5} style={{ width: 100 }} />
          </Form.Item>
          <Form.Item name="pinned" label="置顶" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Space>
        <Form.Item name="tags" label="标签">
          <Select mode="tags" placeholder="回车添加标签" style={{ width: '100%' }} />
        </Form.Item>
      </Form>

      {editing && (
        <div style={{ margin: '8px 0 12px', fontSize: 12, opacity: 0.75 }}>
          状态：<Tag color={statusColor[editing.status]}>{statusLabel[editing.status]}</Tag>
          归属方式：{nsSourceLabel[editing.namespace_source] ?? editing.namespace_source} ·
          来源：{editing.source}（{editing.created_by || '—'}）·
          访问 {editing.access_count} 次 ·
          创建 {dayjs(editing.created_at).format('YYYY-MM-DD HH:mm')}
          {editing.superseded_by > 0 && <> · 被 #{editing.superseded_by} 替代</>}
        </div>
      )}

      {/* 关系：新建时先攒「待建」，随 create 一起提交；编辑时直接落库 */}
      <div style={{ fontWeight: 600, margin: '12px 0 8px' }}>
        {editing ? `关系（共 ${links.length} 条）` : '建立关系（可留空，保存后仍可再补）'}
      </div>

      {editing
        ? links.map((l) => {
            const other = l.from_id === editing.id ? l.to_id : l.from_id
            return (
              <div key={l.id} style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
                <Tag>{relationLabel(l.relation)}</Tag>
                <a onClick={() => onOpenMemory(other)}>#{other}</a>
                <span style={{ fontSize: 12, opacity: 0.7, flex: 1 }}>{l.note}</span>
                <Button
                  type="text"
                  size="small"
                  danger
                  icon={<DeleteOutlined />}
                  onClick={async () => {
                    try {
                      await memoryApi.removeLink(editing.id, l.id)
                      await onLinksChanged()
                    } catch (e) {
                      message.error((e as Error).message)
                    }
                  }}
                />
              </div>
            )
          })
        : pendingLinks.map((p) => (
            <div key={`${p.relation}-${p.to_id}`} style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
              <Tag>{relationLabel(p.relation)}</Tag>
              <a onClick={() => onOpenMemory(p.to_id)}>#{p.to_id}</a>
              <span style={{ fontSize: 12, opacity: 0.7, flex: 1 }}>{p.title}</span>
              <Button
                type="text"
                size="small"
                danger
                icon={<DeleteOutlined />}
                onClick={() => onPendingLinksChange(pendingLinks.filter((x) => x.to_id !== p.to_id))}
              />
            </div>
          ))}

      {(!editing || links.length === 0) && (
        <div style={{ fontSize: 12, color: 'var(--text-faint)', marginBottom: 4 }}>
          {editing ? '还没有关系' : '未选择关系时仅按内容相似度自动关联'}
        </div>
      )}

      <Space.Compact style={{ width: '100%', marginTop: 8 }}>
        <AutoComplete
          style={{ width: '60%' }}
          options={candidates}
          onSearch={searchCandidates}
          onChange={(v) => {
            const id = Number(v) || null
            setLinkTarget(id)
            // 选中项的标题用于待建列表展示；回退成 #id
            const hit = candidates.find((c) => c.value === v)
            setLinkTargetTitle(hit ? hit.label.slice(hit.label.indexOf('·') + 1).trim() : '')
          }}
          placeholder="搜索目标记忆（关键词）"
        />
        <Select
          style={{ width: 130 }}
          value={linkRelation}
          onChange={setLinkRelation}
          options={MEMORY_LINK_RELATIONS}
        />
        <Button onClick={submitLink}>{editing ? '加关系' : '加入待建'}</Button>
      </Space.Compact>
    </Drawer>
  )
}

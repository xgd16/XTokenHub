import { useCallback, useEffect, useRef, useState } from 'react'
import type { TableColumnsType } from 'antd'
import { App, Button, Drawer, Form, Input, Popconfirm, Space, Switch, Table, Tag, Tooltip, Typography } from 'antd'
import { CopyOutlined, KeyOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { keyApi, maskKey, type APIKey, type KeyInput } from '../api/key'
import { statsApi, type GroupStat } from '../api/stats'
import { WS_EVENTS, useWsEvent, useWsReconnected } from '../api/ws'
import { compactCN, fullTime } from '../utils/format'
import { DetailRows } from '../components/LogDetail'
import { useIsMobile } from '../utils/useIsMobile'
import { copyText } from '../utils/clipboard'

const { Text } = Typography

/** 移动端保留的列 key，其余列收进展开行。 */
const MOBILE_KEY_KEYS = new Set(['name', 'key', 'actions'])

/** 每个密钥最近 24h 的用量（按 key 名聚合）。 */
interface KeyUsage {
  requests: number
  tokens: number
  cachePercent: number
}

/** API Keys 管理页：生成密钥给 agent 使用，按 key 统计用量。 */
export default function Keys() {
  const { message } = App.useApp()
  const isMobile = useIsMobile()
  const [items, setItems] = useState<APIKey[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [perPage] = useState(20)
  const [loading, setLoading] = useState(false)
  const [usage, setUsage] = useState<Record<string, KeyUsage>>({})
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [editing, setEditing] = useState<APIKey | null>(null)
  const [form] = Form.useForm()

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [data, byKey] = await Promise.all([
        keyApi.list({ page, per_page: perPage }),
        statsApi.byKey(24).catch(() => [] as GroupStat[]),
      ])
      setItems(data.items ?? [])
      setTotal(data.total)
      const map: Record<string, KeyUsage> = {}
      for (const g of byKey ?? []) {
        if (g.name === '(匿名)') continue
        map[g.name] = {
          requests: g.requests,
          tokens: g.total_tokens,
          cachePercent: Math.round((g.cache_rate || 0) * 1000) / 10,
        }
      }
      setUsage(map)
    } catch (e) {
      message.error((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [page, perPage, message])

  useEffect(() => {
    void load()
  }, [load])

  // 新请求完成 -> 用量变化 -> 节流刷新（5s）。stats.updated 每个请求完成都推送，
  // 不节流时突发流量会对 /keys + /stats/by-key 各打一次，且整表反复重渲染。
  const lastRefreshRef = useRef(0)
  useWsEvent(WS_EVENTS.statsUpdated, () => {
    const now = Date.now()
    if (now - lastRefreshRef.current > 5000) {
      lastRefreshRef.current = now
      void load()
    }
  })

  // 断线重连后补拉一次，避免断连窗口内事件丢失
  useWsReconnected(() => void load())

  const openCreate = () => {
    setEditing(null)
    form.resetFields()
    form.setFieldsValue({ status: true })
    setDrawerOpen(true)
  }

  const openEdit = (k: APIKey) => {
    setEditing(k)
    form.resetFields()
    form.setFieldsValue({ name: k.name, remark: k.remark, status: k.status === 1 })
    setDrawerOpen(true)
  }

  const submit = async () => {
    try {
      const values = await form.validateFields()
      const input: KeyInput = {
        name: values.name,
        status: values.status ? 1 : 0,
        remark: values.remark ?? '',
      }
      if (editing) {
        await keyApi.update(editing.id, input)
        message.success('密钥已更新')
      } else {
        const created = await keyApi.create(input)
        void copyText(created.key).then((ok) =>
          message.success(ok ? `密钥已创建并复制：${created.key}` : `密钥已创建（自动复制失败）：${created.key}`, 6),
        )
      }
      setDrawerOpen(false)
      void load()
    } catch (e) {
      // validateFields 失败时抛 { errorFields }（错误已在表单内展示）；
      // 其余为保存失败（重名 / 网络异常），必须提示，否则抽屉静默不动。
      if (!(e as { errorFields?: unknown })?.errorFields) message.error((e as Error).message)
    }
  }

  const toggleStatus = async (k: APIKey) => {
    try {
      await keyApi.update(k.id, { name: k.name, status: k.status === 1 ? 0 : 1, remark: k.remark })
      void load()
    } catch (e) {
      message.error((e as Error).message)
    }
  }

  const copyKey = async (k: APIKey) => {
    const ok = await copyText(k.key)
    if (ok) message.success('密钥已复制')
    else message.error('复制失败，请手动选择复制')
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div className="panel" style={{ padding: 16 }}>
        <Table<APIKey>
          rowKey="id"
          loading={loading}
          dataSource={items}
          scroll={{ x: isMobile ? 580 : 1250 }}
          pagination={{
            current: page,
            pageSize: perPage,
            total,
            onChange: (p) => setPage(p),
            showTotal: (t) => <span className="mono" style={{ color: 'var(--text-faint)' }}>共 {t} 个密钥</span>,
          }}
          title={() => (
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <Text style={{ color: 'var(--text-secondary)', fontSize: 13, letterSpacing: '0.06em' }}>
                API Keys · 调用方鉴权与用量统计
              </Text>
              <Space>
                <Button icon={<ReloadOutlined />} onClick={() => void load()} size="small" />
                <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
                  新建密钥
                </Button>
              </Space>
            </div>
          )}
          expandable={isMobile ? {
            expandedRowRender: (k) => (
              <DetailRows
                rows={[
                  ['24h 请求', usage[k.name]?.requests ?? '—'],
                  ['24h Tokens', usage[k.name] ? compactCN(usage[k.name].tokens) : '—'],
                  ['缓存命中', usage[k.name] ? `${usage[k.name].cachePercent}%` : '—'],
                  ['备注', k.remark || '—'],
                  ['创建时间', fullTime(k.created_at)],
                ]}
              />
            ),
          } : undefined}
          columns={([
            {
              key: 'name',
              title: '名称',
              dataIndex: 'name',
              width: 170,
              render: (v, k) => (
                <Space size={6}>
                  <KeyOutlined style={{ color: 'var(--accent)' }} />
                  <span className="mono" style={{ fontWeight: 600 }}>{v}</span>
                  {k.status === 1 ? (
                    <Tag color="success" style={{ background: 'transparent' }}>启用</Tag>
                  ) : (
                    <Tag style={{ background: 'transparent', color: 'var(--text-faint)' }}>停用</Tag>
                  )}
                </Space>
              ),
            },
            {
              key: 'key',
              title: 'Key',
              dataIndex: 'key',
              width: 240,
              render: (v, k) => (
                <Space size={4}>
                  <Tooltip title="点击复制完整密钥">
                    <span
                      className="mono"
                      style={{ fontSize: 12, color: 'var(--text-secondary)', cursor: 'pointer' }}
                      onClick={() => void copyKey(k)}
                    >
                      {maskKey(v)}
                    </span>
                  </Tooltip>
                  <Button
                    size="small"
                    type="text"
                    icon={<CopyOutlined />}
                    onClick={() => copyKey(k)}
                  />
                </Space>
              ),
            },
            {
              key: 'req24',
              title: '24h 请求',
              width: 100,
              render: (_, k) => (
                <span className="mono" style={{ fontSize: 12 }}>{usage[k.name]?.requests ?? '—'}</span>
              ),
            },
            {
              key: 'tok24',
              title: '24h Tokens',
              width: 110,
              render: (_, k) => (
                <span className="mono" style={{ fontSize: 12 }}>{usage[k.name] ? compactCN(usage[k.name].tokens) : '—'}</span>
              ),
            },
            {
              key: 'cache',
              title: '缓存命中',
              width: 90,
              render: (_, k) => (
                <span className="mono" style={{ fontSize: 12, color: 'var(--text-faint)' }}>
                  {usage[k.name] ? `${usage[k.name].cachePercent}%` : '—'}
                </span>
              ),
            },
            {
              key: 'remark',
              title: '备注',
              dataIndex: 'remark',
              ellipsis: true,
              render: (v) => <span style={{ color: 'var(--text-secondary)' }}>{v || '—'}</span>,
            },
            {
              key: 'created',
              title: '创建时间',
              dataIndex: 'created_at',
              width: 150,
              render: (v) => <span className="mono" style={{ fontSize: 12, color: 'var(--text-faint)' }}>{fullTime(v)}</span>,
            },
            {
              key: 'status',
              title: '状态',
              dataIndex: 'status',
              width: 70,
              render: (_, k) => <Switch size="small" checked={k.status === 1} onChange={() => void toggleStatus(k)} />,
            },
            {
              key: 'actions',
              title: '操作',
              width: 120,
              render: (_, k) => (
                <Space size={4}>
                  <Button size="small" type="text" onClick={() => openEdit(k)}>
                    编辑
                  </Button>
                  <Popconfirm
                    title="确认删除该密钥？"
                    description="删除后使用此 key 的调用将立即 401"
                    onConfirm={async () => {
                      await keyApi.remove(k.id)
                      message.success('已删除')
                      void load()
                    }}
                  >
                    <Button size="small" type="text" danger>
                      删除
                    </Button>
                  </Popconfirm>
                </Space>
              ),
            },
          ] as TableColumnsType<APIKey>).filter((c) => !isMobile || MOBILE_KEY_KEYS.has(String(c.key)))}
        />
      </div>

      <div className="panel mono" style={{ padding: '12px 16px', fontSize: 12, color: 'var(--text-secondary)' }}>
        <div style={{ marginBottom: 6, color: 'var(--text-faint)', letterSpacing: '0.06em' }}>调用方式</div>
        <div style={{ wordBreak: 'break-all' }}>OpenAI 兼容：base_url = <span style={{ color: 'var(--accent)' }}>http://&lt;host&gt;:8080/v1</span>，api_key = 本页密钥（Authorization: Bearer）</div>
        <div style={{ wordBreak: 'break-all' }}>Anthropic 兼容：base_url = <span style={{ color: 'var(--accent)' }}>http://&lt;host&gt;:8080</span>，密钥经 x-api-key 头传递同样有效</div>
      </div>

      <Drawer
        title={editing ? `编辑密钥 · ${editing.name}` : '新建密钥'}
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        width={isMobile ? '100%' : 460}
        destroyOnHidden
        extra={
          <Space>
            <Button onClick={() => setDrawerOpen(false)}>取消</Button>
            <Button type="primary" onClick={() => void submit()}>
              保存
            </Button>
          </Space>
        }
      >
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入密钥名称' }]}>
            <Input placeholder="如 claude-code、cursor-office" />
          </Form.Item>
          {editing && (
            <Form.Item label="密钥">
              <Space size={4}>
                <span
                  className="mono"
                  style={{ fontSize: 12, cursor: 'pointer' }}
                  onClick={() => void copyKey(editing)}
                >
                  {maskKey(editing.key)}
                </span>
                <Button size="small" type="text" icon={<CopyOutlined />} onClick={() => copyKey(editing)} />
              </Space>
              <div style={{ fontSize: 12, color: 'var(--text-faint)', marginTop: 2 }}>密钥本体创建后不可修改</div>
            </Form.Item>
          )}
          <Form.Item name="status" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item name="remark" label="备注">
            <Input.TextArea rows={2} placeholder="选填：用途 / 归属" />
          </Form.Item>
        </Form>
      </Drawer>
    </div>
  )
}

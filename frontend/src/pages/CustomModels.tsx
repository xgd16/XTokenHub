import { useCallback, useEffect, useState } from 'react'
import type { TableColumnsType } from 'antd'
import { App, Button, Drawer, Form, Input, InputNumber, Popconfirm, Space, Switch, Table, Tag, Tooltip } from 'antd'
import { DeleteOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { customModelApi, type CustomModel, type CustomModelInput, type ModelMember } from '../api/customModel'
import { PageHero } from '../components/PageHero'
import { fullTime } from '../utils/format'
import { Reveal } from '../utils/motion'
import { DetailRows } from '../components/LogDetail'
import { useIsMobile } from '../utils/useIsMobile'

/** 移动端保留的列 key，其余列收进展开行。 */
const MOBILE_CUSTOM_KEYS = new Set(['name', 'status', 'actions'])

/** 自定义模型页：把一组真实模型聚合到同一个对外模型 ID 下按优先级路由。 */
export default function CustomModels() {
  const { message } = App.useApp()
  const isMobile = useIsMobile()
  const [items, setItems] = useState<CustomModel[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(20)
  const [loading, setLoading] = useState(false)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [editing, setEditing] = useState<CustomModel | null>(null)
  const [form] = Form.useForm()

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const data = await customModelApi.list({ page, per_page: perPage })
      setItems(data.items)
      setTotal(data.total)
    } catch (e) {
      message.error((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [page, perPage, message])

  useEffect(() => {
    void load()
  }, [load])

  const openCreate = () => {
    setEditing(null)
    form.setFieldsValue({ name: '', members: [{ model: '', priority: 0 }], status: true, remark: '' })
    setDrawerOpen(true)
  }

  const openEdit = (cm: CustomModel) => {
    setEditing(cm)
    form.setFieldsValue({
      name: cm.name,
      members: cm.members.length > 0 ? cm.members : [{ model: '', priority: 0 }],
      status: cm.status === 1,
      remark: cm.remark,
    })
    setDrawerOpen(true)
  }

  const submit = async () => {
    try {
      const values = await form.validateFields()
      const members: ModelMember[] = (values.members ?? [])
        .map((m: ModelMember) => ({ model: (m.model ?? '').trim(), priority: m.priority ?? 0 }))
        .filter((m: ModelMember) => m.model !== '')
      if (members.length === 0) {
        message.error('至少需要配置一个成员模型')
        return
      }
      const input: CustomModelInput = {
        name: values.name.trim(),
        members,
        status: values.status ? 1 : 0,
        remark: values.remark ?? '',
      }
      if (editing) {
        await customModelApi.update(editing.id, input)
        message.success('自定义模型已更新')
      } else {
        await customModelApi.create(input)
        message.success('自定义模型已创建，客户端即可使用该模型 ID')
      }
      setDrawerOpen(false)
      void load()
    } catch (e) {
      if ((e as Error)?.message) message.error((e as Error).message)
    }
  }

  const toggleStatus = async (cm: CustomModel) => {
    try {
      await customModelApi.update(cm.id, {
        name: cm.name,
        members: cm.members,
        status: cm.status === 1 ? 0 : 1,
        remark: cm.remark,
      })
      void load()
    } catch (e) {
      message.error((e as Error).message)
    }
  }

  const memberTags = (cm: CustomModel, max: number) => (
    <Tooltip
      title={
        <div style={{ fontSize: 12 }}>
          {cm.members.map((m) => (
            <div key={m.model} className="mono">
              {m.model}
              {m.priority ? <span style={{ color: 'var(--text-faint)' }}> · P{m.priority}</span> : ''}
            </div>
          ))}
        </div>
      }
    >
      <Space size={4} wrap>
        {cm.members.slice(0, max).map((m) => (
          <Tag key={m.model} className="mono" style={{ background: 'transparent', fontSize: 12 }}>
            {m.model}
          </Tag>
        ))}
        {cm.members.length > max && <Tag style={{ background: 'transparent' }}>+{cm.members.length - max}</Tag>}
      </Space>
    </Tooltip>
  )

  const columns: TableColumnsType<CustomModel> = [
    {
      key: 'name',
      title: '模型 ID',
      dataIndex: 'name',
      width: 170,
      render: (v, cm) => (
        <Space size={6}>
          <span className="mono" style={{ fontWeight: 600 }}>{v}</span>
          {cm.status === 1 ? (
            <Tag color="success" style={{ background: 'transparent' }}>启用</Tag>
          ) : (
            <Tag style={{ background: 'transparent', color: 'var(--text-faint)' }}>停用</Tag>
          )}
        </Space>
      ),
    },
    {
      key: 'members',
      title: '成员模型',
      width: 380,
      render: (_, cm) => memberTags(cm, isMobile ? 1 : 4),
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
      render: (_, cm) => <Switch size="small" checked={cm.status === 1} onChange={() => void toggleStatus(cm)} />,
    },
    {
      key: 'actions',
      title: '操作',
      width: 120,
      render: (_, cm) => (
        <Space size={4}>
          <Button size="small" type="text" onClick={() => openEdit(cm)}>
            编辑
          </Button>
          <Popconfirm
            title="确认删除该自定义模型？"
            description="删除后客户端再用此模型 ID 请求将返回无可用渠道"
            onConfirm={async () => {
              await customModelApi.remove(cm.id)
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
  ]

  return (
    <Reveal className="obs-page">
      <PageHero
        eyebrow="custom models · 聚合路由"
        title="自定义模型"
        desc="把一组真实模型聚合成一个对外模型 ID，按「成员优先级 → 渠道优先级 → 原生透传」依次展开候选。"
        readouts={[
          { label: '自定义模型', value: total, tone: 'accent' },
          { label: '本页启用', value: items.filter((cm) => cm.status === 1).length },
        ]}
        actions={
          <>
            <Button icon={<ReloadOutlined />} onClick={() => void load()} aria-label="刷新" />
            <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
              新建自定义模型
            </Button>
          </>
        }
      />

      <div className="obs-panel">
        <Table<CustomModel>
          rowKey="id"
          loading={loading}
          dataSource={items}
          scroll={{ x: isMobile ? 460 : 1000 }}
          pagination={{
            current: page,
            pageSize: perPage,
            total,
            onChange: (p, ps) => {
              setPage(p)
              setPerPage(ps)
            },
            showTotal: (t) => <span className="mono" style={{ color: 'var(--text-faint)' }}>共 {t} 个自定义模型</span>,
          }}
          expandable={isMobile ? {
            expandedRowRender: (cm) => (
              <DetailRows
                rows={[
                  ['成员模型', <div key="m" style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                    {cm.members.map((m) => (
                      <span key={m.model} className="mono" style={{ fontSize: 12 }}>
                        {m.model}
                        {m.priority ? <span style={{ color: 'var(--text-faint)' }}> · P{m.priority}</span> : ''}
                      </span>
                    ))}
                  </div>],
                  ['备注', cm.remark || '—'],
                  ['创建时间', fullTime(cm.created_at)],
                ]}
              />
            ),
          } : undefined}
          columns={([...columns] as TableColumnsType<CustomModel>).filter((c) => !isMobile || MOBILE_CUSTOM_KEYS.has(String(c.key)))}
        />
      </div>

      <div className="obs-hint">
        <div className="obs-hint-label">路由规则</div>
        <div>客户端请求自定义模型 ID 时，网关按「成员优先级（数值小者优先）→ 渠道优先级 → 原生透传优先」展开候选并逐个尝试；</div>
        <div>请求日志与统计记录实际使用的成员模型；模型 ID 也会出现在 GET /v1/models 能力清单中。</div>
      </div>

      <Drawer
        title={editing ? `编辑自定义模型 · ${editing.name}` : '新建自定义模型'}
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        width={isMobile ? '100%' : 520}
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
        <Form form={form} layout="vertical" initialValues={{ status: true, members: [{ model: '', priority: 0 }] }}>
          <Form.Item name="name" label="模型 ID" rules={[{ required: true, message: '请输入对外模型 ID' }]}>
            <Input placeholder="如 free-1M（客户端请求时使用，需唯一）" />
          </Form.Item>
          <Form.Item label="成员模型（优先级数值越小越优先）" required>
            <Form.List name="members">
              {(fields, { add, remove }) => (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                  {fields.map((field) => (
                    <Space key={field.key} size={6} align="baseline" style={{ display: 'flex' }}>
                      <Form.Item
                        name={[field.name, 'model']}
                        rules={[{ required: true, message: '请输入真实模型名' }]}
                        noStyle
                      >
                        <Input placeholder="真实模型名，如 deepseek-chat" style={{ flex: 1, minWidth: 0 }} />
                      </Form.Item>
                      <Form.Item name={[field.name, 'priority']} noStyle>
                        <InputNumber placeholder="优先级" min={0} style={{ width: 88 }} />
                      </Form.Item>
                      <Button
                        type="text"
                        danger
                        icon={<DeleteOutlined />}
                        disabled={fields.length <= 1}
                        onClick={() => remove(field.name)}
                      />
                    </Space>
                  ))}
                  <Button type="dashed" icon={<PlusOutlined />} onClick={() => add({ model: '', priority: 0 })} style={{ width: 'fit-content' }}>
                    添加成员
                  </Button>
                </div>
              )}
            </Form.List>
          </Form.Item>
          <Form.Item name="status" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item name="remark" label="备注">
            <Input.TextArea rows={2} placeholder="如：全部免费的 1M 上下文模型" />
          </Form.Item>
        </Form>
      </Drawer>
    </Reveal>
  )
}

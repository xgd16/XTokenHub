import { useCallback, useEffect, useState } from 'react'
import type { TableColumnsType } from 'antd'
import { App, AutoComplete, Button, Drawer, Form, Input, Popconfirm, Select, Space, Switch, Table, Tag, Typography } from 'antd'
import { DeleteOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import {
  REASONING_TIERS,
  reasoningRewriteApi,
  type ReasoningMapping,
  type ReasoningRewrite,
  type ReasoningRewriteInput,
} from '../api/reasoningRewrite'
import { fullTime } from '../utils/format'
import { DetailRows } from '../components/LogDetail'
import { useIsMobile } from '../utils/useIsMobile'

const { Text } = Typography

/** 档位目标选项。 */
const TARGET_OPTIONS = [
  { value: 'disabled', label: '关思维（disabled）' },
  { value: 'enabled', label: '开思维（enabled）' },
]

const MISSING_OPTIONS = [
  { value: '', label: '不注入（保留原样）' },
  { value: 'enabled', label: '缺省开思维' },
  { value: 'disabled', label: '缺省关思维' },
]

function targetLabel(v: string): string | null {
  if (v === 'enabled') return '开'
  if (v === 'disabled') return '关'
  return null
}

const MOBILE_REWRITE_KEYS = new Set(['model', 'status', 'actions'])

/** 思维档位改写页：把 reasoning_effort 等档位串翻译成 thinking 开关（MiMo 类只认开/关的上游）。 */
export default function ReasoningRewrites() {
  const { message } = App.useApp()
  const isMobile = useIsMobile()
  const [items, setItems] = useState<ReasoningRewrite[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(20)
  const [loading, setLoading] = useState(false)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [editing, setEditing] = useState<ReasoningRewrite | null>(null)
  const [form] = Form.useForm()

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const data = await reasoningRewriteApi.list({ page, per_page: perPage })
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
    form.setFieldsValue({ model: '', mapping: [{ level: 'low', target: 'disabled' }], missing: '', remark: '' })
    setDrawerOpen(true)
  }

  const openEdit = (rule: ReasoningRewrite) => {
    setEditing(rule)
    form.setFieldsValue({
      model: rule.model,
      mapping: rule.mapping.length > 0 ? rule.mapping : [{ level: '', target: 'disabled' }],
      missing: rule.missing,
      remark: rule.remark,
    })
    setDrawerOpen(true)
  }

  const submit = async () => {
    try {
      const values = await form.validateFields()
      const mapping: ReasoningMapping[] = (values.mapping ?? [])
        .map((m: ReasoningMapping) => ({ level: (m.level ?? '').trim().toLowerCase(), target: m.target }))
        .filter((m: ReasoningMapping) => m.level !== '')
      const levels = new Set<string>()
      for (const m of mapping) {
        if (levels.has(m.level)) {
          message.error(`档位重复: ${m.level}`)
          return
        }
        levels.add(m.level)
      }
      const input: ReasoningRewriteInput = {
        model: values.model.trim(),
        mapping,
        missing: values.missing ?? '',
        remark: values.remark ?? '',
      }
      if (editing) {
        await reasoningRewriteApi.update(editing.id, input)
        message.success('改写规则已更新')
      } else {
        await reasoningRewriteApi.create(input)
        message.success('改写规则已创建')
      }
      setDrawerOpen(false)
      void load()
    } catch (e) {
      if ((e as Error)?.message) message.error((e as Error).message)
    }
  }

  const toggleStatus = async (rule: ReasoningRewrite) => {
    try {
      await reasoningRewriteApi.update(rule.id, {
        model: rule.model,
        mapping: rule.mapping,
        missing: rule.missing,
        status: rule.status === 1 ? 0 : 1,
        remark: rule.remark,
      })
      void load()
    } catch (e) {
      message.error((e as Error).message)
    }
  }

  const mappingTags = (rule: ReasoningRewrite, max: number) => {
    const tags = rule.mapping.slice(0, max).map((m) => {
      const target = targetLabel(m.target)
      if (!target) return null
      return (
        <Tag key={m.level} color={target === '开' ? 'success' : 'default'} className="mono" style={{ background: 'transparent', fontSize: 12 }}>
          {m.level}→{target}
        </Tag>
      )
    })
    if (rule.missing) {
      const target = targetLabel(rule.missing)
      if (target) {
        tags.push(
          <Tag key="missing" color="processing" className="mono" style={{ background: 'transparent', fontSize: 12 }}>
            未带档位→{target}
          </Tag>,
        )
      }
    }
    if (tags.every((t) => t === null)) {
      return <span style={{ color: 'var(--text-faint)' }}>未配置映射</span>
    }
    const shown = tags.filter(Boolean)
    if (rule.mapping.length > max) {
      shown.push(<Tag key="more" style={{ background: 'transparent' }}>+{rule.mapping.length - max}</Tag>)
    }
    return <Space size={4} wrap>{shown}</Space>
  }

  const columns: TableColumnsType<ReasoningRewrite> = [
    {
      key: 'model',
      title: '上游模型',
      dataIndex: 'model',
      width: 200,
      render: (v) => <span className="mono" style={{ fontWeight: 600 }}>{v}</span>,
    },
    {
      key: 'mapping',
      title: '档位映射',
      width: 400,
      render: (_, rule) => mappingTags(rule, isMobile ? 2 : 6),
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
      render: (_, rule) => <Switch size="small" checked={rule.status === 1} onChange={() => void toggleStatus(rule)} />,
    },
    {
      key: 'actions',
      title: '操作',
      width: 120,
      render: (_, rule) => (
        <Space size={4}>
          <Button size="small" type="text" onClick={() => openEdit(rule)}>
            编辑
          </Button>
          <Popconfirm
            title="确认删除该改写规则？"
            description="删除后该模型的档位参数将原样透传给上游"
            onConfirm={async () => {
              await reasoningRewriteApi.remove(rule.id)
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
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div className="panel" style={{ padding: 16 }}>
        <Table<ReasoningRewrite>
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
            showTotal: (t) => <span className="mono" style={{ color: 'var(--text-faint)' }}>共 {t} 条改写规则</span>,
          }}
          title={() => (
            <div style={{ display: 'flex', flexWrap: 'wrap', rowGap: 8, justifyContent: 'space-between', alignItems: 'center' }}>
              <Text style={{ color: 'var(--text-secondary)', fontSize: 13, letterSpacing: '0.06em' }}>
                思维档位改写 · 按上游模型把 reasoning_effort 档位翻译成 thinking 开/关
              </Text>
              <Space>
                <Button icon={<ReloadOutlined />} onClick={() => void load()} size="small" />
                <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
                  新建改写规则
                </Button>
              </Space>
            </div>
          )}
          expandable={isMobile ? {
            expandedRowRender: (rule) => (
              <DetailRows
                rows={[
                  ['档位映射', mappingTags(rule, 8)],
                  ['备注', rule.remark || '—'],
                  ['创建时间', fullTime(rule.created_at)],
                ]}
              />
            ),
          } : undefined}
          columns={([...columns] as TableColumnsType<ReasoningRewrite>).filter((c) => !isMobile || MOBILE_REWRITE_KEYS.has(String(c.key)))}
        />
      </div>

      <div className="panel mono" style={{ padding: '12px 16px', fontSize: 12, color: 'var(--text-secondary)' }}>
        <div style={{ marginBottom: 6, color: 'var(--text-faint)', letterSpacing: '0.06em' }}>改写规则</div>
        <div>仅对命中的上游模型名生效（自定义模型组按成员模型匹配）；未命中的档位保留原样透传，未列出的模型完全不受影响；</div>
        <div>请求体已有 thinking 配置时只删除档位字段、不覆盖；客户端未带档位时按「缺省注入」补 thinking（不配置则不注入）。</div>
      </div>

      <Drawer
        title={editing ? `编辑改写规则 · ${editing.model}` : '新建改写规则'}
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
        <Form form={form} layout="vertical">
          <Form.Item name="model" label="上游模型名" rules={[{ required: true, message: '请输入上游真实模型名' }]}>
            <Input placeholder="如 mimo-v2.6-flash（与渠道里配置的模型名一致）" />
          </Form.Item>
          <Form.Item label="档位映射（客户端档位 → thinking 开关）" required>
            <Form.List name="mapping">
              {(fields, { add, remove }) => (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                  {fields.map((field) => (
                    <Space key={field.key} size={6} align="baseline" style={{ display: 'flex' }}>
                      <Form.Item
                        name={[field.name, 'level']}
                        rules={[{ required: true, message: '请输入档位' }]}
                        noStyle
                      >
                        <AutoComplete
                          options={REASONING_TIERS.map((t) => ({ value: t }))}
                          placeholder="档位，如 low"
                          style={{ width: 140 }}
                        />
                      </Form.Item>
                      <Form.Item
                        name={[field.name, 'target']}
                        rules={[{ required: true, message: '请选择开关' }]}
                        noStyle
                      >
                        <Select options={TARGET_OPTIONS} style={{ width: 190 }} />
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
                  <Button type="dashed" icon={<PlusOutlined />} onClick={() => add({ level: '', target: 'disabled' })} style={{ width: 'fit-content' }}>
                    添加档位
                  </Button>
                </div>
              )}
            </Form.List>
          </Form.Item>
          <Form.Item name="missing" label="未带档位时注入">
            <Select options={MISSING_OPTIONS} />
          </Form.Item>
          <Form.Item name="remark" label="备注">
            <Input.TextArea rows={2} placeholder="如：MiMo 只认 thinking 开/关，档位需翻译" />
          </Form.Item>
        </Form>
      </Drawer>
    </div>
  )
}

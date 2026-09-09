import { memo, useCallback, useEffect, useMemo, useState } from 'react'
import {
  App,
  Button,
  Checkbox,
  Col,
  Drawer,
  Empty,
  Form,
  Input,
  InputNumber,
  Pagination,
  Popconfirm,
  Row,
  Select,
  Space,
  Spin,
  Switch,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import { CloudDownloadOutlined, CopyOutlined, ExperimentOutlined, PlusOutlined, ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import {
  channelApi,
  nativeProtocolsOf,
  parseCSV,
  PROVIDER_AUTO,
  PROTOCOLS,
  PROVIDERS,
  type BalanceInfo,
  type Channel,
  type ChannelBalance,
  type ChannelInput,
  type ProbeReport,
  type Protocol,
  type ProviderType,
} from '../api/channel'
import { WS_EVENTS, useWsEvent, useWsReconnected } from '../api/ws'
import { fullTime, protocolShort } from '../utils/format'
import { copyText } from '../utils/clipboard'
import { useIsMobile } from '../utils/useIsMobile'

const { Text } = Typography
/** 协议探测徽标组：✓ 原生 / ✗ / 未探测。 */
const ProtocolBadges = memo(function ProtocolBadges({ channel }: { channel: Channel }) {
  const native = nativeProtocolsOf(channel)
  const probed = channel.last_probe_at != null
  return (
    <Space size={4} wrap>
      {PROTOCOLS.map((p) => {
        const isNative = native.includes(p.value)
        const color = isNative ? 'var(--accent)' : probed ? 'var(--coral)' : 'var(--text-faint)'
        const title = isNative
          ? `${p.label}：原生支持（透传）`
          : probed
            ? `${p.label}：探测未通过（${p.path}）`
            : `${p.label}：未探测`
        return (
          <Tooltip key={p.value} title={title}>
            <Tag
              className="mono"
              style={{
                background: 'transparent',
                color,
                borderColor: color,
                opacity: isNative || probed ? 1 : 0.6,
              }}
            >
              {isNative ? '✓ ' : probed ? '✗ ' : '· '}
              {protocolShort(p.value)}
            </Tag>
          </Tooltip>
        )
      })}
    </Space>
  )
})

/** 模型标签：单击复制该模型名；maxWidth 用于表格列内防止单个超长模型名撑爆布局。 */
function modelTagNode(model: string, copy: (text: string, tip: string) => void, maxWidth?: number) {
  return (
    <Tooltip key={model} title={`点击复制 ${model}`}>
      <Tag
        className="mono"
        style={{
          marginInlineEnd: 0,
          cursor: 'pointer',
          display: 'inline-block',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
          ...(maxWidth ? { maxWidth } : {}),
        }}
        onClick={() => copy(model, `已复制 ${model}`)}
      >
        {model}
      </Tag>
    </Tooltip>
  )
}

/** 渠道模型面板：完整模型列表，支持关键字筛选；单击标签复制单个，按钮复制筛选结果（未筛选时复制全部）。 */
function ModelPanel({ models }: { models: string[] }) {
  const { message } = App.useApp()
  const [keyword, setKeyword] = useState('')
  const kw = keyword.trim().toLowerCase()
  const filtered = kw ? models.filter((m) => m.toLowerCase().includes(kw)) : models
  const copy = async (text: string, tip: string) => {
    const ok = await copyText(text)
    if (ok) message.success(tip)
    else message.error('复制失败，请手动选择复制')
  }
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <Input
        allowClear
        placeholder="输入关键字筛选模型"
        prefix={<SearchOutlined style={{ color: 'var(--text-faint)' }} />}
        value={keyword}
        onChange={(e) => setKeyword(e.target.value)}
      />
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}>
        <span className="mono" style={{ fontSize: 12, color: 'var(--text-faint)' }}>
          {kw ? `匹配 ${filtered.length} / ${models.length} 个` : `共 ${models.length} 个 · 单击标签复制`}
        </span>
        <Button
          size="small"
          icon={<CopyOutlined />}
          disabled={filtered.length === 0}
          onClick={() => void copy(filtered.join(','), `已复制 ${filtered.length} 个模型`)}
        >
          {kw ? `复制 ${filtered.length} 个` : '复制全部'}
        </Button>
      </div>
      {filtered.length > 0 ? (
        <Space size={[4, 4]} wrap>
          {filtered.map((m) => modelTagNode(m, copy))}
        </Space>
      ) : (
        <span style={{ fontSize: 12, color: 'var(--text-faint)' }}>无匹配模型</span>
      )}
    </div>
  )
}

/** 余额符号：按上游币种映射，未识别币种原样展示。 */
const CURRENCY_SYMBOLS: Record<string, string> = { CNY: '¥', USD: '$' }

/** 余额单元格：金额 + 拆分 tooltip；不支持 / 未加载 / 失败分别展示。 */
function BalanceCell({ entry }: { entry?: ChannelBalance }) {
  if (!entry || !entry.supported) {
    return <span style={{ color: 'var(--text-faint)' }}>—</span>
  }
  if (!entry.ok) {
    return (
      <Tooltip title={entry.error || '查询失败'}>
        <span style={{ color: 'var(--coral)', fontSize: 12 }}>查询失败</span>
      </Tooltip>
    )
  }
  const b: BalanceInfo = entry.balance!
  const symbol = CURRENCY_SYMBOLS[b.currency] ?? `${b.currency} `
  const color = b.is_available ? 'var(--text-primary)' : 'var(--coral)'
  const title = (
    <div style={{ fontSize: 12 }}>
      <div>
        总额 {symbol}
        {b.total.toFixed(2)}（赠金 {symbol}
        {b.granted.toFixed(2)} · 充值 {symbol}
        {b.topped_up.toFixed(2)}）
      </div>
      <div style={{ opacity: 0.7 }}>拉取于 {entry.fetched_at ? fullTime(entry.fetched_at) : '—'}</div>
      {!b.is_available && <div style={{ color: 'var(--coral)' }}>账户当前无可用于 API 调用的余额</div>}
    </div>
  )
  return (
    <Tooltip title={title}>
      <span className="mono" style={{ color, fontSize: 12 }}>
        {symbol}
        {b.total.toFixed(2)}
      </span>
    </Tooltip>
  )
}

/** 键值行：卡片内信息行（与 DetailRows 同节奏）。 */
function CardRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 12, minHeight: 22 }}>
      <span style={{ color: 'var(--text-faint)', fontSize: 12, flexShrink: 0, width: 56 }}>{label}</span>
      <span style={{ minWidth: 0, flex: 1, display: 'flex', alignItems: 'center', gap: 8 }}>{children}</span>
    </div>
  )
}

/** BaseURL 展示：等宽字体截断，悬浮完整地址，点击复制。 */
function BaseUrlCell({ url }: { url: string }) {
  const { message } = App.useApp()
  return (
    <Tooltip title={`点击复制 ${url}`}>
      <span
        className="mono"
        style={{
          fontSize: 12,
          color: 'var(--text-secondary)',
          cursor: 'pointer',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
          display: 'block',
        }}
        onClick={async () => {
          const ok = await copyText(url)
          if (ok) message.success('已复制 BaseURL')
          else message.error('复制失败，请手动选择复制')
        }}
      >
        {url}
      </span>
    </Tooltip>
  )
}

/** 渠道卡片：头部名称 + 启用开关，中部键值信息，底部操作；网格内等高。
 *  回调按渠道传参（而非闭包捕获），父级可传稳定引用，配合 memo 避免任一余额/探测更新牵动全部卡片。 */
const ChannelCard = memo(function ChannelCard(props: {
  ch: Channel
  balance?: ChannelBalance
  probing: boolean
  onProbe: (ch: Channel) => void
  onEdit: (ch: Channel) => void
  onToggle: (ch: Channel) => void
  onModels: (ch: Channel) => void
  onDelete: (ch: Channel) => Promise<void>
}) {
  const { ch, balance, probing } = props
  const enabled = ch.status === 1
  const modelCount = ch.models ? parseCSV(ch.models).length : 0
  return (
    <div
      style={{
        flex: 1,
        display: 'flex',
        flexDirection: 'column',
        gap: 10,
        padding: '14px 16px',
        border: '1px solid var(--border-faint)',
        borderRadius: 10,
        background: 'var(--card-tint)',
      }}
    >
      {/* 头部：名称 + 启用开关 */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}>
        <Space size={8} style={{ minWidth: 0 }}>
          <span className="mono" style={{ fontWeight: 600, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {ch.name}
          </span>
          <Tag style={{ background: 'transparent', fontSize: 11, lineHeight: '16px', padding: '0 6px', flexShrink: 0 }}>
            {PROVIDERS.find((p) => p.value === ch.provider)?.label ?? ch.provider}
          </Tag>
        </Space>
        <Tooltip title={enabled ? '启用中 · 点击停用' : '已停用 · 点击启用'}>
          <Switch size="small" checked={enabled} onChange={() => props.onToggle(ch)} />
        </Tooltip>
      </div>

      {/* BaseURL：点击复制 */}
      <BaseUrlCell url={ch.base_url} />

      <div style={{ borderTop: '1px dashed var(--border-faint)' }} />

      {/* 信息行 */}
      <CardRow label="原生协议">
        <ProtocolBadges channel={ch} />
      </CardRow>
      <CardRow label="模型">
        {ch.models ? (
          <>
            <span className="mono" style={{ fontSize: 12 }}>{modelCount} 个</span>
            <Button size="small" type="text" style={{ padding: '0 6px', height: 20 }} onClick={() => props.onModels(ch)}>
              查看
            </Button>
          </>
        ) : (
          <span style={{ fontSize: 12, color: 'var(--text-faint)' }}>全部（未限定）</span>
        )}
      </CardRow>
      <CardRow label="余额">
        <BalanceCell entry={balance} />
      </CardRow>
      <CardRow label="路由">
        <span className="mono" style={{ fontSize: 12 }}>
          P{ch.priority} · W{ch.weight}
        </span>
        <span style={{ color: 'var(--text-faint)', fontSize: 12 }}>优先级小者先</span>
      </CardRow>
      <CardRow label="最后探测">
        <span className="mono" style={{ fontSize: 12, color: 'var(--text-faint)' }}>
          {ch.last_probe_at ? fullTime(ch.last_probe_at) : '未探测'}
        </span>
      </CardRow>

      {/* 底部操作 */}
      <div style={{ display: 'flex', justifyContent: 'flex-end', alignItems: 'center', gap: 4, borderTop: '1px dashed var(--border-faint)', paddingTop: 10, marginTop: 'auto' }}>
        <Button size="small" icon={<ExperimentOutlined />} loading={probing} onClick={() => props.onProbe(ch)}>
          探测
        </Button>
        <Button size="small" type="text" onClick={() => props.onEdit(ch)}>
          编辑
        </Button>
        <Popconfirm title="确认删除该渠道？" onConfirm={() => props.onDelete(ch)}>
          <Button size="small" type="text" danger>
            删除
          </Button>
        </Popconfirm>
      </div>
    </div>
  )
})

/** 渠道管理页。 */
export default function Channels() {
  const { message, notification } = App.useApp()
  const isMobile = useIsMobile()
  const [items, setItems] = useState<Channel[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [perPage] = useState(20)
  const [loading, setLoading] = useState(false)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [editing, setEditing] = useState<Channel | null>(null)
  const [probing, setProbing] = useState<number | null>(null)
  // 余额查询结果（channel_id -> entry），由批量接口 + WS 推送维护
  const [balances, setBalances] = useState<Record<number, ChannelBalance>>({})
  // 模型面板：当前查看模型列表的渠道
  const [modelsChannel, setModelsChannel] = useState<Channel | null>(null)
  const [form] = Form.useForm()
  // 从上游拉取的模型候选列表（抽屉内共享）
  const [modelOptions, setModelOptions] = useState<{ label: string; value: string }[] | undefined>()
  const [fetchingModels, setFetchingModels] = useState(false)
  // 厂家类型驱动 BaseURL 提示（子路径挂载点 vs 裸域名）
  const providerWatch = Form.useWatch('provider', form)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const data = await channelApi.list({ page, per_page: perPage })
      setItems(data.items ?? [])
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

  const loadBalances = useCallback(async () => {
    try {
      const { items } = await channelApi.balances()
      const map: Record<number, ChannelBalance> = {}
      for (const it of items) map[it.channel_id] = it
      setBalances(map)
    } catch {
      // 余额列加载失败不打扰主列表
    }
  }, [])

  useEffect(() => {
    void loadBalances()
  }, [loadBalances])

  // 探测结果经 WS 实时刷新行数据
  useWsEvent(WS_EVENTS.channelProbeResult, () => void load())

  // 余额更新经 WS 实时合并（后端拉取成功后推送）
  useWsEvent(WS_EVENTS.channelBalanceUpdated, (msg) => {
    const p = msg.payload as { channel_id: number; balance: BalanceInfo }
    setBalances((prev) => {
      const old = prev[p.channel_id]
      if (!old) return prev // 不在当前页的渠道忽略
      return {
        ...prev,
        [p.channel_id]: { ...old, ok: true, balance: p.balance, error: undefined, fetched_at: p.balance.fetched_at },
      }
    })
  })

  // 断线重连后补拉一次：断连窗口内的探测结果/余额推送已丢失
  useWsReconnected(() => {
    void load()
    void loadBalances()
  })

  const openCreate = () => {
    setEditing(null)
    form.resetFields()
    setModelOptions(undefined)
    form.setFieldsValue({ provider: PROVIDER_AUTO, priority: 100, weight: 1, status: true })
    setDrawerOpen(true)
  }

  const submit = async () => {
    try {
      const values = await form.validateFields()
      const input: ChannelInput = {
        name: values.name,
        // 「自动识别」不下发，由后端按 BaseURL 推断
        provider: values.provider === PROVIDER_AUTO ? undefined : values.provider,
        base_url: values.base_url,
        api_key: values.api_key,
        models: values.models ?? [],
        native_protocols: (values.native_protocols ?? []) as Protocol[],
        priority: values.priority,
        weight: values.weight,
        status: values.status ? 1 : 0,
        remark: values.remark ?? '',
      }
      if (editing) {
        await channelApi.update(editing.id, input)
        message.success('渠道已更新')
      } else {
        await channelApi.create(input)
        message.success('渠道已创建，建议先执行「探测」识别原生协议')
      }
      setDrawerOpen(false)
      void load()
    } catch (e) {
      // validateFields 失败时抛 { errorFields }（错误已在表单内展示）；
      // 其余为保存失败（重名 / 参数非法 / 网络异常），必须提示，否则抽屉静默不动。
      if (!(e as { errorFields?: unknown })?.errorFields) message.error((e as Error).message)
    }
  }

  // 用表单内凭据拉取上游模型列表（渠道可尚未创建）
  const fetchModels = async () => {
    try {
      await form.validateFields(['provider', 'base_url', 'api_key'])
    } catch {
      return // 校验错误已在表单内展示
    }
    setFetchingModels(true)
    try {
      const v = form.getFieldsValue(true) as { provider: ProviderType | typeof PROVIDER_AUTO; base_url: string; api_key: string }
      const { models } = await channelApi.lookupModels({
        provider: v.provider === PROVIDER_AUTO ? undefined : v.provider,
        base_url: v.base_url,
        api_key: v.api_key,
      })
      setModelOptions(models.map((m) => ({ label: m, value: m })))
      const current: string[] = form.getFieldValue('models') ?? []
      if (models.length > 0 && current.length === 0) {
        form.setFieldsValue({ models }) // 全选便于直接保存；清空则视为路由全部
      }
      message.success(`拉取到 ${models.length} 个模型`)
    } catch (e) {
      message.error((e as Error).message)
    } finally {
      setFetchingModels(false)
    }
  }

  // 复制表单中已填的模型列表（CSV，可直接粘贴进其他渠道）
  const copyFormModels = async () => {
    const models: string[] = form.getFieldValue('models') ?? []
    if (models.length === 0) {
      message.info('尚未填写模型')
      return
    }
    const ok = await copyText(models.join(','))
    if (ok) message.success(`已复制 ${models.length} 个模型`)
    else message.error('复制失败，请手动选择复制')
  }

  const doProbe = useCallback(async (ch: Channel) => {
    setProbing(ch.id)
    try {
      const report: ProbeReport = await channelApi.probe(ch.id)
      const okList = report.items.filter((i) => i.ok).map((i) => protocolShort(i.protocol))
      notification.success({
        message: '探测完成',
        description: (
          <div className="mono" style={{ fontSize: 12 }}>
            <div>探测模型：{report.probe_model || '—'}</div>
            <div>原生协议：{okList.length > 0 ? okList.join(' / ') : '无'}</div>
            {report.items
              .filter((i) => !i.ok)
              .map((i) => (
                <div key={i.protocol} style={{ color: 'var(--coral)', marginTop: 2 }}>
                  ✗ {protocolShort(i.protocol)} — {i.detail}
                </div>
              ))}
          </div>
        ),
      })
      void load()
    } catch (e) {
      message.error((e as Error).message)
    } finally {
      setProbing(null)
    }
  }, [load, message, notification])

  const toggleStatus = useCallback(async (ch: Channel) => {
    try {
      await channelApi.update(ch.id, {
        name: ch.name,
        provider: ch.provider,
        base_url: ch.base_url,
        api_key: ch.api_key,
        models: parseCSV(ch.models),
        priority: ch.priority,
        weight: ch.weight,
        status: ch.status === 1 ? 0 : 1,
        remark: ch.remark,
      })
      void load()
    } catch (e) {
      message.error((e as Error).message)
    }
  }, [load, message])

  const openEdit = useCallback((ch: Channel) => {
    setEditing(ch)
    form.resetFields()
    setModelOptions(undefined)
    form.setFieldsValue({
      name: ch.name,
      provider: ch.provider,
      base_url: ch.base_url,
      api_key: ch.api_key,
      models: parseCSV(ch.models),
      native_protocols: nativeProtocolsOf(ch),
      priority: ch.priority,
      weight: ch.weight,
      status: ch.status === 1,
      remark: ch.remark,
    })
    setDrawerOpen(true)
  }, [form])

  const showModels = useCallback((ch: Channel) => setModelsChannel(ch), [])

  const removeChannel = useCallback(async (ch: Channel) => {
    await channelApi.remove(ch.id)
    message.success('已删除')
    void load()
  }, [load, message])

  // 客户端过滤（当前页内）：名称 / BaseURL / 模型
  const [keyword, setKeyword] = useState('')
  const kw = keyword.trim().toLowerCase()
  const filtered = useMemo(() => {
    if (!kw) return items
    return items.filter(
      (ch) =>
        ch.name.toLowerCase().includes(kw) ||
        ch.base_url.toLowerCase().includes(kw) ||
        ch.models.toLowerCase().includes(kw),
    )
  }, [items, kw])

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div className="panel" style={{ padding: 16, display: 'flex', flexDirection: 'column', gap: 14 }}>
        {/* 工具栏 */}
        <div style={{ display: 'flex', flexWrap: 'wrap', rowGap: 8, justifyContent: 'space-between', alignItems: 'center', gap: 12 }}>
          <Text style={{ color: 'var(--text-secondary)', fontSize: 13, letterSpacing: '0.06em' }}>
            上游渠道 · 原生协议自动识别
            <span className="mono" style={{ marginLeft: 10, fontSize: 12, color: 'var(--text-faint)' }}>
              {total} 个
            </span>
          </Text>
          <Space wrap>
            <Input
              allowClear
              size="small"
              style={{ width: 220 }}
              placeholder="搜索名称 / BaseURL / 模型"
              prefix={<SearchOutlined style={{ color: 'var(--text-faint)' }} />}
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
            />
            <Button icon={<ReloadOutlined />} onClick={() => { void load(); void loadBalances() }} size="small" />
            <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
              新建渠道
            </Button>
          </Space>
        </div>

        {/* 渠道卡片网格 */}
        <Spin spinning={loading}>
          {filtered.length === 0 ? (
            <Empty
              style={{ padding: '40px 0' }}
              description={kw ? '无匹配渠道' : '还没有渠道 · 新建后即可统一接入上游 API'}
            >
              {!kw && (
                <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
                  新建渠道
                </Button>
              )}
            </Empty>
          ) : (
            <Row gutter={[12, 12]} align="stretch">
              {filtered.map((ch) => (
                <Col key={ch.id} xs={24} sm={12} xl={8} style={{ display: 'flex' }}>
                  <ChannelCard
                    ch={ch}
                    balance={balances[ch.id]}
                    probing={probing === ch.id}
                    onProbe={doProbe}
                    onEdit={openEdit}
                    onToggle={toggleStatus}
                    onModels={showModels}
                    onDelete={removeChannel}
                  />
                </Col>
              ))}
            </Row>
          )}
        </Spin>

        {/* 分页 */}
        <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
          <Pagination
            size="small"
            current={page}
            pageSize={perPage}
            total={total}
            onChange={(p) => setPage(p)}
            showTotal={(t) => <span className="mono" style={{ color: 'var(--text-faint)' }}>共 {t} 个渠道</span>}
          />
        </div>
      </div>


      <Drawer
        title={editing ? `编辑渠道 · ${editing.name}` : '新建渠道'}
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
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入渠道名' }]}>
            <Input placeholder="如 openai-main" />
          </Form.Item>
          <Form.Item
            name="provider"
            label="接口风格"
            tooltip="按 BaseURL 自动识别（挂载点含 anthropic 即 Anthropic 风格）；同一厂家的不同协议端点各建一条渠道"
            rules={[{ required: true }]}
          >
            <Select options={[{ value: PROVIDER_AUTO, label: '自动识别（按 BaseURL）' }, ...PROVIDERS]} />
          </Form.Item>
          <Form.Item
            name="base_url"
            label="BaseURL"
            rules={[
              { required: true },
              { type: 'url', message: '须为合法 URL（兼容带/不带 /v1 结尾）' },
            ]}
          >
            <Input
              placeholder={
                providerWatch === 'anthropic'
                  ? 'https://api.anthropic.com；DeepSeek 填 https://api.deepseek.com/anthropic'
                  : providerWatch === PROVIDER_AUTO
                    ? 'https://api.openai.com；Anthropic 挂载点如 https://api.deepseek.com/anthropic'
                    : 'https://api.openai.com；版本化挂载如智谱 https://open.bigmodel.cn/api/paas/v4'
              }
            />
          </Form.Item>
          <Form.Item name="api_key" label="API Key" rules={[{ required: true }]}>
            <Input.Password placeholder="sk-..." autoComplete="new-password" />
          </Form.Item>
          <Form.Item
            name="models"
            label={
              <Space size={8}>
                <span>支持的模型（留空 = 全部）</span>
                <Button
                  size="small"
                  icon={<CloudDownloadOutlined />}
                  loading={fetchingModels}
                  onClick={(e) => {
                    e.stopPropagation()
                    void fetchModels()
                  }}
                >
                  从上游拉取
                </Button>
                <Button
                  size="small"
                  icon={<CopyOutlined />}
                  onClick={(e) => {
                    e.stopPropagation()
                    void copyFormModels()
                  }}
                >
                  复制
                </Button>
              </Space>
            }
          >
            <Select
              mode="tags"
              options={modelOptions}
              placeholder="可先「从上游拉取」列表后勾选，或手动输入"
              tokenSeparators={[',']}
            />
          </Form.Item>
          <Form.Item
            name="native_protocols"
            label="原生协议（可手工修正，通常用「探测」自动识别）"
          >
            <Checkbox.Group
              options={PROTOCOLS.map((p) => ({ value: p.value, label: `${p.label}（${p.path}）` }))}
            />
          </Form.Item>
          <Space size={16}>
            <Form.Item name="priority" label="优先级（小者优先）">
              <InputNumber min={0} max={9999} style={{ width: 130 }} />
            </Form.Item>
            <Form.Item name="weight" label="权重">
              <InputNumber min={0} max={100} style={{ width: 130 }} />
            </Form.Item>
          </Space>
          <Form.Item name="status" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item name="remark" label="备注">
            <Input.TextArea rows={2} placeholder="选填" />
          </Form.Item>
        </Form>
      </Drawer>

      <Drawer
        title={modelsChannel ? `渠道模型 · ${modelsChannel.name}` : '渠道模型'}
        open={modelsChannel != null}
        onClose={() => setModelsChannel(null)}
        width={isMobile ? '100%' : 560}
        destroyOnHidden
      >
        {modelsChannel && <ModelPanel models={parseCSV(modelsChannel.models)} />}
      </Drawer>
    </div>
  )
}

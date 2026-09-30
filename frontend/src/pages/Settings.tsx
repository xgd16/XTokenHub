import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Segmented,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
  message,
} from 'antd'
import type { TableColumnsType } from 'antd'
import { PlusOutlined, DeleteOutlined, ReloadOutlined, InfoCircleOutlined, SyncOutlined, EditOutlined, WarningOutlined, DatabaseOutlined, KeyOutlined } from '@ant-design/icons'
import { request } from '../api/http'
import {
  costApi,
  pricingApi,
  DEEPSEEK_PEAK_WINDOW,
  type BillingSettings,
  type ModelPrice,
  type SyncStatus,
  type UnpricedModel,
} from '../api/pricing'
import { dataSettingsApi, type AggregateStatus, type UpdateDataSettingsInput } from '../api/dataSettings'
import { mimoSettingsApi, type MimoVerifyResult } from '../api/mimoSettings'
import {
  formatPricePerMillion,
  fromPerMillion,
  toPerMillion,
  currencySymbol,
  type Currency,
} from '../utils/money'
import { setMoneyOptions } from '../utils/useMoneyFormat'
import { Reveal } from '../utils/motion'
import { useNow } from '../utils/useNow'
import { peakPeriodAt } from '../utils/peakWindow'

const { Text } = Typography

/** 会话标识配置行（后端 model.SessionHeaderConfig）。 */
interface SessionHeaderConfig {
  id: number
  key: string
  header_name: string
  description: string
  enabled: boolean
  created_at: string
  updated_at: string
}

/** 价格编辑表单：单价按其币种、以「每百万 token」录入，避免在表单里填 0.000003 这种数字。 */
interface PriceForm {
  id?: number
  model: string
  /** 费率币种：USD | CNY。 */
  currency: 'USD' | 'CNY'
  input: number
  output: number
  cacheRead: number
  cacheWrite: number
  /** 高峰时段规则（北京时间），空 = 不启用时段价。 */
  peakWindow: string
  offPeakInput: number
  offPeakOutput: number
  offPeakCacheRead: number
  offPeakCacheWrite: number
  threshold: number
  inputAbove: number
  outputAbove: number
  cacheReadAbove: number
  cacheWriteAbove: number
}

const emptyPriceForm: PriceForm = {
  model: '',
  currency: 'USD',
  input: 0,
  output: 0,
  cacheRead: 0,
  cacheWrite: 0,
  peakWindow: '',
  offPeakInput: 0,
  offPeakOutput: 0,
  offPeakCacheRead: 0,
  offPeakCacheWrite: 0,
  threshold: 0,
  inputAbove: 0,
  outputAbove: 0,
  cacheReadAbove: 0,
  cacheWriteAbove: 0,
}

/** 价格行 -> 编辑表单（单 token 单价换算成每百万）。 */
function toForm(p: ModelPrice): PriceForm {
  return {
    id: p.id,
    model: p.model,
    currency: p.currency === 'CNY' ? 'CNY' : 'USD',
    input: toPerMillion(p.input_cost_per_token),
    output: toPerMillion(p.output_cost_per_token),
    cacheRead: toPerMillion(p.cache_read_cost_per_token),
    cacheWrite: toPerMillion(p.cache_write_cost_per_token),
    peakWindow: p.peak_window || '',
    offPeakInput: toPerMillion(p.off_peak_input_cost_per_token),
    offPeakOutput: toPerMillion(p.off_peak_output_cost_per_token),
    offPeakCacheRead: toPerMillion(p.off_peak_cache_read_cost_per_token),
    offPeakCacheWrite: toPerMillion(p.off_peak_cache_write_cost_per_token),
    threshold: p.threshold_tokens || 0,
    inputAbove: toPerMillion(p.input_cost_above_per_token),
    outputAbove: toPerMillion(p.output_cost_above_per_token),
    cacheReadAbove: toPerMillion(p.cache_read_cost_above_per_token),
    cacheWriteAbove: toPerMillion(p.cache_write_cost_above_per_token),
  }
}

/** 编辑表单 -> 接口入参（每百万换算回单 token）。 */
function toPayload(f: PriceForm) {
  return {
    model: f.model.trim(),
    currency: f.currency,
    input_cost_per_token: fromPerMillion(f.input),
    output_cost_per_token: fromPerMillion(f.output),
    cache_read_cost_per_token: fromPerMillion(f.cacheRead),
    cache_write_cost_per_token: fromPerMillion(f.cacheWrite),
    peak_window: f.peakWindow.trim(),
    off_peak_input_cost_per_token: fromPerMillion(f.offPeakInput),
    off_peak_output_cost_per_token: fromPerMillion(f.offPeakOutput),
    off_peak_cache_read_cost_per_token: fromPerMillion(f.offPeakCacheRead),
    off_peak_cache_write_cost_per_token: fromPerMillion(f.offPeakCacheWrite),
    threshold_tokens: Math.round(f.threshold || 0),
    input_cost_above_per_token: fromPerMillion(f.inputAbove),
    output_cost_above_per_token: fromPerMillion(f.outputAbove),
    cache_read_cost_above_per_token: fromPerMillion(f.cacheReadAbove),
    cache_write_cost_above_per_token: fromPerMillion(f.cacheWriteAbove),
  }
}

/** 会话标识配置页面：会话标识 + 计费与价格表配置。 */
export default function Settings() {
  const [configs, setConfigs] = useState<SessionHeaderConfig[]>([])
  const [newHeader, setNewHeader] = useState('')
  const [newDescription, setNewDescription] = useState('')
  const [loading, setLoading] = useState(false)
  const [adding, setAdding] = useState(false)

  // 计费与价格表
  const [billing, setBilling] = useState<BillingSettings>({ display_currency: 'USD', usd_cny_rate: 0, monthly_budget_usd: 0 })
  const [billingSaving, setBillingSaving] = useState(false)
  const [prices, setPrices] = useState<ModelPrice[]>([])
  const [priceTotal, setPriceTotal] = useState(0)
  const [pricePage, setPricePage] = useState(1)
  const [priceLoading, setPriceLoading] = useState(false)
  const [priceQuery, setPriceQuery] = useState('')
  const [usedOnly, setUsedOnly] = useState(true)
  const [status, setStatus] = useState<SyncStatus | null>(null)
  const [unpriced, setUnpriced] = useState<UnpricedModel[]>([])
  const [syncing, setSyncing] = useState(false)
  const [recomputing, setRecomputing] = useState(false)
  const [editing, setEditing] = useState<PriceForm | null>(null)
  const [savingPrice, setSavingPrice] = useState(false)

  // 数据管理：清理配置 + 聚合状态
  const [dataForm, setDataForm] = useState<UpdateDataSettingsInput>({
    enabled: true, mode: 'daily', run_hour: 4, interval_hours: 24,
    max_days: 90, batch_size: 1000, vacuum: false, trim_headers: true, trim_headers_after_days: 7,
  })
  const [dataSaving, setDataSaving] = useState(false)
  const [rebuilding, setRebuilding] = useState(false)
  const [aggStatus, setAggStatus] = useState<AggregateStatus | null>(null)

  // MiMo 网页余额凭证：保存后立即重拉渠道余额验证
  const [mimoCookie, setMimoCookie] = useState('')
  const [mimoUpdatedAt, setMimoUpdatedAt] = useState('')
  const [mimoSaving, setMimoSaving] = useState(false)
  const [mimoVerify, setMimoVerify] = useState<MimoVerifyResult | null>(null)

  const PRICE_PER_PAGE = 20

  // 加载会话标识配置：后端信封 {code, message, data} 已由 request() 解包
  const loadConfigs = useCallback(async () => {
    setLoading(true)
    try {
      const data = await request<{ configs: SessionHeaderConfig[] }>({
        url: '/api/v1/settings/session-headers',
        method: 'GET',
      })
      setConfigs(data.configs ?? [])
    } catch (error) {
      message.error(error instanceof Error ? error.message : '加载配置失败')
    } finally {
      setLoading(false)
    }
  }, [])

  // 加载价格表：同时拿回同步状态、未定价模型与计费设置，省掉额外往返
  const loadPrices = useCallback(
    async (page = 1, q = priceQuery, onlyUsed = usedOnly) => {
      setPriceLoading(true)
      try {
        const data = await pricingApi.listPrices({
          page,
          per_page: PRICE_PER_PAGE,
          q: q.trim() || undefined,
          used_only: onlyUsed,
        })
        setPrices(data.items ?? [])
        setPriceTotal(data.total ?? 0)
        setPricePage(data.page ?? page)
        setStatus(data.status ?? null)
        setUnpriced(data.unpriced ?? [])
        if (data.billing) setBilling(data.billing)
      } catch (error) {
        message.error(error instanceof Error ? error.message : '加载价格表失败')
      } finally {
        setPriceLoading(false)
      }
    },
    [priceQuery, usedOnly],
  )

  useEffect(() => {
    void loadConfigs()
  }, [loadConfigs])

  useEffect(() => {
    void loadPrices(1)
    // 仅在首次挂载时拉取；后续由搜索/翻页/同步显式触发
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // 加载数据管理设置与聚合状态
  const loadDataSettings = useCallback(async () => {
    try {
      const view = await dataSettingsApi.get()
      const c = view.cleanup
      setDataForm({
        enabled: c.cleanup_enabled,
        mode: c.cleanup_mode,
        run_hour: c.cleanup_run_hour,
        interval_hours: c.cleanup_interval_hours,
        max_days: c.cleanup_max_days,
        batch_size: c.cleanup_batch_size,
        vacuum: c.cleanup_vacuum,
        trim_headers: c.trim_headers,
        trim_headers_after_days: c.trim_headers_after_days,
      })
      setAggStatus(view.aggregate ?? null)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '加载数据管理设置失败')
    }
  }, [])

  // 保存清理配置：后台任务每轮执行前重读配置，改完即生效
  const saveDataSettings = useCallback(async () => {
    setDataSaving(true)
    try {
      await dataSettingsApi.update(dataForm)
      message.success('数据管理设置已保存，后台任务下一轮生效')
      await loadDataSettings()
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    } finally {
      setDataSaving(false)
    }
  }, [dataForm, loadDataSettings])

  // 重建聚合：days>0 近 N 天，0 全量（以现存明细为准，累计统计不缩水）
  const rebuildAggregate = useCallback(async (days: number) => {
    setRebuilding(true)
    try {
      const st = await dataSettingsApi.rebuildAggregate(days)
      setAggStatus(st)
      message.success(days > 0 ? `已重建近 ${days} 天聚合` : '聚合已按现存明细全量重建')
    } catch (error) {
      message.error(error instanceof Error ? error.message : '重建失败')
    } finally {
      setRebuilding(false)
    }
  }, [])

  useEffect(() => {
    void loadDataSettings()
  }, [loadDataSettings])

  // 加载 MiMo 凭证（明文回显，便于核对/更新）
  const loadMimoSettings = useCallback(async () => {
    try {
      const row = await mimoSettingsApi.get()
      setMimoCookie(row.cookie ?? '')
      setMimoUpdatedAt(row.updated_at ?? '')
    } catch (error) {
      message.error(error instanceof Error ? error.message : '加载 MiMo 凭证失败')
    }
  }, [])

  useEffect(() => {
    void loadMimoSettings()
  }, [loadMimoSettings])

  // 保存 MiMo 凭证并立即验证：服务端保存后已失效余额缓存，重拉即得新结果
  const saveMimoCookie = useCallback(async () => {
    setMimoSaving(true)
    try {
      const saved = await mimoSettingsApi.update(mimoCookie.trim())
      setMimoUpdatedAt(saved.updated_at)
      message.success('MiMo 凭证已保存')
      const { items } = await mimoSettingsApi.balances()
      const entry = (items ?? []).find((it) => it.provider === 'mimo' && it.supported)
      if (!entry) {
        setMimoVerify({ ok: false, text: '未找到可查余额的 MiMo 渠道（渠道 BaseURL 需为 xiaomimimo.com 域名）' })
      } else if (entry.ok && entry.balance) {
        const parts = [`余额 ¥${entry.balance.total.toFixed(2)}`]
        if (entry.balance.granted > 0) parts.push(`含赠金 ¥${entry.balance.granted.toFixed(2)}`)
        setMimoVerify({ ok: true, text: `验证成功：${parts.join('，')}` })
      } else {
        setMimoVerify({ ok: false, text: `验证失败：${entry.error || '未知错误'}` })
      }
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    } finally {
      setMimoSaving(false)
    }
  }, [mimoCookie])

  // 添加新配置：即时落库，成功后重拉列表
  const addConfig = useCallback(async () => {
    const name = newHeader.trim()
    if (!name) {
      message.warning('请输入 Header 名称')
      return
    }
    const exists = configs.some((c) => c.header_name.toLowerCase() === name.toLowerCase())
    if (exists) {
      message.warning('该 Header 已存在')
      return
    }
    setAdding(true)
    try {
      await request<SessionHeaderConfig>({
        url: '/api/v1/settings/session-headers',
        method: 'POST',
        data: { header_name: name, description: newDescription.trim() },
      })
      message.success('添加成功')
      setNewHeader('')
      setNewDescription('')
      await loadConfigs()
    } catch (error) {
      message.error(error instanceof Error ? error.message : '添加失败')
    } finally {
      setAdding(false)
    }
  }, [newHeader, newDescription, configs, loadConfigs])

  // 删除配置
  const removeConfig = useCallback(
    async (id: number) => {
      try {
        await request({ url: `/api/v1/settings/session-headers/${id}`, method: 'DELETE' })
        message.success('删除成功')
        await loadConfigs()
      } catch (error) {
        message.error(error instanceof Error ? error.message : '删除失败')
      }
    },
    [loadConfigs],
  )

  // 切换启用状态（按配置 key）
  const toggleEnabled = useCallback(
    async (key: string) => {
      try {
        await request({ url: `/api/v1/settings/session-headers/${key}/toggle`, method: 'PUT' })
        await loadConfigs()
      } catch (error) {
        message.error(error instanceof Error ? error.message : '更新失败')
      }
    },
    [loadConfigs],
  )

  // 保存计费设置：成功后把新的币种/汇率推给所有展示处（无需刷新页面）
  const saveBilling = useCallback(async () => {
    setBillingSaving(true)
    try {
      const saved = await pricingApi.updateBilling(billing)
      setBilling(saved)
      setMoneyOptions({
        currency: (saved.display_currency === 'CNY' ? 'CNY' : 'USD') as Currency,
        rate: Number(saved.usd_cny_rate) || 0,
      })
      message.success('计费设置已保存')
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    } finally {
      setBillingSaving(false)
    }
  }, [billing])

  // 立即同步公开价格表
  const syncPrices = useCallback(async () => {
    setSyncing(true)
    try {
      const res = await pricingApi.sync()
      message.success(`同步完成：写入 ${res.written} 个模型${res.skipped_manual > 0 ? `，保留 ${res.skipped_manual} 个手工配置` : ''}`)
      await loadPrices(1)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '同步失败')
    } finally {
      setSyncing(false)
    }
  }, [loadPrices])

  // 按当前价格重算历史费用（覆盖已有值）
  const recompute = useCallback(async () => {
    setRecomputing(true)
    try {
      const res = await costApi.recompute({ only_missing: false })
      message.success(`重算完成：扫描 ${res.scanned} 条，更新 ${res.updated} 条`)
      await loadPrices(pricePage)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '重算失败')
    } finally {
      setRecomputing(false)
    }
  }, [loadPrices, pricePage])

  // 保存价格（新增或编辑）
  const savePrice = useCallback(async () => {
    if (!editing) return
    if (!editing.id && !editing.model.trim()) {
      message.warning('请输入模型名')
      return
    }
    setSavingPrice(true)
    try {
      const payload = toPayload(editing)
      if (editing.id) {
        await pricingApi.updatePrice(editing.id, payload)
      } else {
        await pricingApi.createPrice(payload)
      }
      message.success(editing.id ? '价格已更新（标记为手工配置，同步不再覆盖）' : '价格已添加')
      setEditing(null)
      await loadPrices(editing.id ? pricePage : 1)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    } finally {
      setSavingPrice(false)
    }
  }, [editing, loadPrices, pricePage])

  const removePrice = useCallback(
    async (p: ModelPrice) => {
      try {
        await pricingApi.deletePrice(p.id)
        message.success(p.source === 'manual' ? '已删除' : '已删除，下次同步会重新写回')
        await loadPrices(pricePage)
      } catch (error) {
        message.error(error instanceof Error ? error.message : '删除失败')
      }
    },
    [loadPrices, pricePage],
  )

  const columns = useMemo(
    () => [
      {
        title: 'Header 名称',
        dataIndex: 'header_name',
        key: 'header_name',
        render: (text: string) => (
          <Text code style={{ fontSize: 13 }}>
            {text}
          </Text>
        ),
      },
      {
        title: '描述',
        dataIndex: 'description',
        key: 'description',
        render: (text: string) => (
          <Text type="secondary" style={{ fontSize: 13 }}>
            {text || '—'}
          </Text>
        ),
      },
      {
        title: '状态',
        dataIndex: 'enabled',
        key: 'enabled',
        width: 100,
        render: (enabled: boolean, record: SessionHeaderConfig) => (
          <Tag
            color={enabled ? 'success' : 'default'}
            style={{ cursor: 'pointer' }}
            onClick={() => void toggleEnabled(record.key)}
          >
            {enabled ? '启用' : '禁用'}
          </Tag>
        ),
      },
      {
        title: '操作',
        key: 'actions',
        width: 80,
        render: (_: unknown, record: SessionHeaderConfig) => (
          <Popconfirm
            title="确定删除此配置？"
            description="删除后将不再识别该 Header 中的会话标识"
            onConfirm={() => void removeConfig(record.id)}
            okText="删除"
            cancelText="取消"
          >
            <Button type="text" danger icon={<DeleteOutlined />} size="small" />
          </Popconfirm>
        ),
      },
    ],
    [removeConfig, toggleEnabled],
  )

  // 时段列的「当前峰/谷」标记随真实时间变化，每 30s 重算一次
  const now = useNow()

  const priceColumns: TableColumnsType<ModelPrice> = useMemo(
    () => [
      {
        title: '模型',
        dataIndex: 'model',
        key: 'model',
        ellipsis: true,
        render: (v: string) => (
          <Tooltip title={v}>
            <span className="mono" style={{ fontSize: 12 }}>{v}</span>
          </Tooltip>
        ),
      },
      {
        title: '币种',
        dataIndex: 'currency',
        key: 'currency',
        width: 72,
        render: (v: string, p) => (
          <Tag color={p.currency === 'CNY' ? 'red' : undefined} style={{ background: 'transparent' }}>
            {currencySymbol(v)} {p.currency === 'CNY' ? 'CNY' : 'USD'}
          </Tag>
        ),
      },
      {
        title: '时段',
        key: 'peakWindow',
        width: 96,
        render: (_, p) => {
          if (!p.peak_window) {
            return <Text type="secondary" style={{ fontSize: 12 }}>—</Text>
          }
          const period = peakPeriodAt(p.peak_window, new Date(now))
          const tooltip = (
            <>
              高峰时段（北京时间）：{p.peak_window}
              <br />
              {period === 'peak'
                ? '当前处于高峰时段，按高峰费率计价'
                : period === 'off_peak'
                  ? '当前处于空闲时段，按 off-peak 费率计价'
                  : '空闲时段按其半价左右的 off-peak 费率计价'}
            </>
          )
          // 规则解析失败时退回静态标记，不臆造当前时段
          if (period === null) {
            return (
              <Tooltip title={tooltip}>
                <Tag color="orange" style={{ background: 'transparent' }}>错峰</Tag>
              </Tooltip>
            )
          }
          return (
            <Tooltip title={tooltip}>
              <Tag color={period === 'peak' ? 'orange' : 'green'} style={{ background: 'transparent' }}>
                {period === 'peak' ? '高峰价' : '空闲价'}
              </Tag>
            </Tooltip>
          )
        },
      },
      {
        title: '输入',
        key: 'input',
        width: 100,
        align: 'right',
        render: (_, p) => <span className="mono" style={{ fontSize: 12 }}>{formatPricePerMillion(p.input_cost_per_token, p.currency)}</span>,
      },
      {
        title: '输出',
        key: 'output',
        width: 100,
        align: 'right',
        render: (_, p) => <span className="mono" style={{ fontSize: 12 }}>{formatPricePerMillion(p.output_cost_per_token, p.currency)}</span>,
      },
      {
        title: '缓存命中',
        key: 'cacheRead',
        width: 100,
        align: 'right',
        render: (_, p) => <span className="mono" style={{ fontSize: 12 }}>{formatPricePerMillion(p.cache_read_cost_per_token, p.currency)}</span>,
      },
      {
        title: '缓存写',
        key: 'cacheWrite',
        width: 100,
        align: 'right',
        render: (_, p) => <span className="mono" style={{ fontSize: 12 }}>{formatPricePerMillion(p.cache_write_cost_per_token, p.currency)}</span>,
      },
      {
        title: '来源',
        dataIndex: 'source',
        key: 'source',
        width: 92,
        render: (v: string) =>
          v === 'manual' ? (
            <Tag color="blue" style={{ background: 'transparent' }}>手工</Tag>
          ) : (
            <Tag style={{ background: 'transparent' }}>同步</Tag>
          ),
      },
      {
        title: '操作',
        key: 'actions',
        width: 90,
        render: (_, p) => (
          <Space size={0}>
            <Button type="text" size="small" icon={<EditOutlined />} onClick={() => setEditing(toForm(p))} />
            <Popconfirm
              title="确定删除该价格？"
              description={p.source === 'manual' ? '删除后该模型将按未定价处理（费用记 0）' : '同步来源的行会在下次同步时重新写回'}
              onConfirm={() => void removePrice(p)}
              okText="删除"
              cancelText="取消"
            >
              <Button type="text" danger size="small" icon={<DeleteOutlined />} />
            </Popconfirm>
          </Space>
        ),
      },
    ],
    [removePrice, now],
  )

  const lastSyncText = status?.last_sync_at ? new Date(status.last_sync_at).toLocaleString() : '尚未同步'

  return (
    <Reveal style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <Card
        className="panel"
        styles={{
          body: { padding: '16px 20px' },
          header: { borderBottom: '1px solid var(--border-faint)' },
        }}
        title={
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <InfoCircleOutlined style={{ color: 'var(--accent)' }} />
            <span style={{ fontSize: 14, fontWeight: 500 }}>会话标识配置</span>
          </div>
        }
      >
        <div style={{ marginBottom: 16 }}>
          <Text type="secondary" style={{ fontSize: 13 }}>
            配置可识别的 HTTP Header 中的会话标识。网关会按这里启用的 Header
            依次读取（取第一个非空值）写入请求日志的 session_id，仪表盘据此对同一会话的请求进行聚合。
          </Text>
        </div>

        <Table
          dataSource={configs}
          columns={columns}
          rowKey="id"
          pagination={false}
          size="small"
          loading={loading}
          style={{ marginBottom: 16 }}
        />

        <div
          style={{
            display: 'flex',
            gap: 12,
            alignItems: 'flex-end',
            padding: '12px 0 0',
            borderTop: '1px solid var(--border-faint)',
          }}
        >
          <div style={{ flex: 1 }}>
            <Text type="secondary" style={{ fontSize: 12, marginBottom: 4, display: 'block' }}>
              Header 名称
            </Text>
            <Input
              placeholder="例如: X-Conversation-Id"
              value={newHeader}
              onChange={(e) => setNewHeader(e.target.value)}
              onPressEnter={() => void addConfig()}
              style={{ width: '100%' }}
            />
          </div>
          <div style={{ flex: 1 }}>
            <Text type="secondary" style={{ fontSize: 12, marginBottom: 4, display: 'block' }}>
              描述（可选）
            </Text>
            <Input
              placeholder="例如: 自定义会话标识"
              value={newDescription}
              onChange={(e) => setNewDescription(e.target.value)}
              onPressEnter={() => void addConfig()}
              style={{ width: '100%' }}
            />
          </div>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => void addConfig()} loading={adding}>
            添加
          </Button>
        </div>
      </Card>

      <Card
        className="panel"
        styles={{
          body: { padding: '16px 20px' },
          header: { borderBottom: '1px solid var(--border-faint)' },
        }}
        title={
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <InfoCircleOutlined style={{ color: 'var(--accent)' }} />
            <span style={{ fontSize: 14, fontWeight: 500 }}>会话聚合说明</span>
          </div>
        }
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <div>
            <Text strong style={{ fontSize: 13 }}>会话聚合规则</Text>
            <ul style={{ margin: '8px 0 0 20px', padding: 0 }}>
              <li>
                <Text type="secondary" style={{ fontSize: 13 }}>
                  网关按启用的 Header 依次读取，取第一个非空值作为会话标识
                </Text>
              </li>
              <li>
                <Text type="secondary" style={{ fontSize: 13 }}>
                  同一会话的所有请求会在仪表盘中合并显示
                </Text>
              </li>
              <li>
                <Text type="secondary" style={{ fontSize: 13 }}>
                  配置即时生效（含短缓存），无需重启服务
                </Text>
              </li>
            </ul>
          </div>
          <div>
            <Text strong style={{ fontSize: 13 }}>示例</Text>
            <div
              style={{
                background: 'var(--bg-elevated)',
                borderRadius: 6,
                padding: '12px 16px',
                marginTop: 8,
                fontFamily: 'monospace',
                fontSize: 13,
              }}
            >
              <div style={{ color: 'var(--text-faint)' }}># 请求示例</div>
              <div>POST /v1/chat/completions</div>
              <div style={{ color: 'var(--accent)' }}>X-Session-Id: sess_abc123</div>
              <div style={{ color: 'var(--accent)' }}>X-Conversation-Id: conv_xyz789</div>
            </div>
          </div>
        </div>
      </Card>

      <Card
        className="panel"
        styles={{
          body: { padding: '16px 20px' },
          header: { borderBottom: '1px solid var(--border-faint)' },
        }}
        title={
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <InfoCircleOutlined style={{ color: 'var(--accent)' }} />
            <span style={{ fontSize: 14, fontWeight: 500 }}>计费与展示</span>
          </div>
        }
      >
        <Text type="secondary" style={{ fontSize: 13, display: 'block', marginBottom: 16 }}>
          费用一律以美元存储与计算，这里只影响展示。汇率需手工维护（不接入外部汇率接口）。
          它同时用于把人民币计价的价格行折算成美元记账。
        </Text>
        {billing.usd_cny_rate <= 0 && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 16 }}
            message="未配置 USD → CNY 汇率"
            description={
              <>
                人民币计价的价格行需要汇率才能折算成美元记账。<b>汇率为 0 时这类模型的费用会记为 0</b>
                （按未定价处理），且界面会回退为美元显示。
                填好汇率并保存后，可在下方「重算历史费用」一键补全（已有费用默认不重算）。
              </>
            }
          />
        )}
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 20, alignItems: 'flex-end' }}>
          <div>
            <Text type="secondary" style={{ fontSize: 12, marginBottom: 6, display: 'block' }}>
              默认展示币种
            </Text>
            <Segmented
              value={billing.display_currency === 'CNY' ? 'CNY' : 'USD'}
              onChange={(v) => setBilling({ ...billing, display_currency: String(v) })}
              options={[
                { value: 'USD', label: '美元 $' },
                { value: 'CNY', label: '人民币 ¥' },
              ]}
            />
          </div>
          <div>
            <Text type="secondary" style={{ fontSize: 12, marginBottom: 6, display: 'block' }}>
              USD → CNY 汇率
            </Text>
            <InputNumber
              min={0}
              step={0.01}
              precision={4}
              value={billing.usd_cny_rate}
              onChange={(v) => setBilling({ ...billing, usd_cny_rate: Number(v) || 0 })}
              style={{ width: 160 }}
              placeholder="如 7.2000"
            />
          </div>
          <div>
            <Text type="secondary" style={{ fontSize: 12, marginBottom: 6, display: 'block' }}>
              月度预算（USD，0 = 不设）
            </Text>
            <InputNumber
              min={0}
              step={10}
              precision={2}
              value={billing.monthly_budget_usd}
              onChange={(v) => setBilling({ ...billing, monthly_budget_usd: Number(v) || 0 })}
              style={{ width: 160 }}
              placeholder="如 100"
            />
          </div>
          <Button type="primary" onClick={() => void saveBilling()} loading={billingSaving}>
            保存
          </Button>
        </div>
        <Text type="secondary" style={{ fontSize: 12, display: 'block', marginTop: 12 }}>
          月度预算仅用于仪表盘的「本月预计花费」超支提示，不会拦截或限流请求。
        </Text>
      </Card>

      <Card
        className="panel"
        styles={{
          body: { padding: '16px 20px' },
          header: { borderBottom: '1px solid var(--border-faint)' },
        }}
        title={
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <DatabaseOutlined style={{ color: 'var(--accent)' }} />
            <span style={{ fontSize: 14, fontWeight: 500 }}>数据管理</span>
          </div>
        }
      >
        <Text type="secondary" style={{ fontSize: 13, display: 'block', marginBottom: 16 }}>
          累计统计（总 Token、连续天数、花费预测、模型用量）按本地日自动聚合落库：
          完整历史日读聚合表、仅当天实时计算，数据量增大后查询成本只与天数相关。
          清理过期明细不影响累计数据；请求头只有日志详情页使用，超过瘦身天数自动置空以减小库体积。
        </Text>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 20, alignItems: 'flex-end' }}>
          <div>
            <Text type="secondary" style={{ fontSize: 12, marginBottom: 6, display: 'block' }}>
              自动清理
            </Text>
            <Switch checked={dataForm.enabled} onChange={(v) => setDataForm({ ...dataForm, enabled: v })} />
          </div>
          <div>
            <Text type="secondary" style={{ fontSize: 12, marginBottom: 6, display: 'block' }}>
              触发方式
            </Text>
            <Segmented
              value={dataForm.mode}
              onChange={(v) => setDataForm({ ...dataForm, mode: v as 'daily' | 'interval' })}
              options={[
                { value: 'daily', label: '每天固定时刻' },
                { value: 'interval', label: '每 N 小时' },
              ]}
            />
          </div>
          {dataForm.mode === 'daily' ? (
            <div>
              <Text type="secondary" style={{ fontSize: 12, marginBottom: 6, display: 'block' }}>
                执行时刻
              </Text>
              <Select
                value={dataForm.run_hour}
                onChange={(v) => setDataForm({ ...dataForm, run_hour: Number(v) })}
                options={Array.from({ length: 24 }, (_, h) => ({ value: h, label: `${String(h).padStart(2, '0')}:00` }))}
                style={{ width: 110 }}
              />
            </div>
          ) : (
            <div>
              <Text type="secondary" style={{ fontSize: 12, marginBottom: 6, display: 'block' }}>
                间隔（小时）
              </Text>
              <InputNumber
                min={1}
                max={720}
                value={dataForm.interval_hours}
                onChange={(v) => setDataForm({ ...dataForm, interval_hours: Number(v) || 24 })}
                style={{ width: 110 }}
              />
            </div>
          )}
          <div>
            <Text type="secondary" style={{ fontSize: 12, marginBottom: 6, display: 'block' }}>
              保留天数（建议 ≥30，覆盖仪表盘窗口）
            </Text>
            <InputNumber
              min={1}
              max={3650}
              value={dataForm.max_days}
              onChange={(v) => setDataForm({ ...dataForm, max_days: Number(v) || 90 })}
              style={{ width: 130 }}
            />
          </div>
          <div>
            <Text type="secondary" style={{ fontSize: 12, marginBottom: 6, display: 'block' }}>
              请求头瘦身
            </Text>
            <Space size={8}>
              <Switch size="small" checked={dataForm.trim_headers} onChange={(v) => setDataForm({ ...dataForm, trim_headers: v })} />
              <InputNumber
                size="small"
                min={1}
                max={365}
                value={dataForm.trim_headers_after_days}
                onChange={(v) => setDataForm({ ...dataForm, trim_headers_after_days: Number(v) || 7 })}
                style={{ width: 100 }}
                addonAfter="天"
              />
            </Space>
          </div>
          <div>
            <Text type="secondary" style={{ fontSize: 12, marginBottom: 6, display: 'block' }}>
              清理后 VACUUM
            </Text>
            <Switch checked={dataForm.vacuum} onChange={(v) => setDataForm({ ...dataForm, vacuum: v })} />
          </div>
          <Button type="primary" onClick={() => void saveDataSettings()} loading={dataSaving}>
            保存
          </Button>
          <Popconfirm
            title="重建聚合数据？"
            description="全量重建以现存明细为准：已被清理的日期不会恢复，累计统计不受影响；如需修正近期口径可选近 7 天。"
            okText="重建"
            cancelText="取消"
            onConfirm={() => void rebuildAggregate(0)}
          >
            <Button icon={<ReloadOutlined />} loading={rebuilding}>
              重建聚合
            </Button>
          </Popconfirm>
        </div>
        {aggStatus?.watermark_date && (
          <Text type="secondary" style={{ fontSize: 12, display: 'block', marginTop: 12 }}>
            聚合覆盖 {aggStatus.earliest_day || '—'} ~ {aggStatus.latest_day || '—'}，完整日已封存至 {aggStatus.watermark_date}；
            删除明细后累计统计保持不变。
          </Text>
        )}
      </Card>

      <Card
        className="panel"
        styles={{
          body: { padding: '16px 20px' },
          header: { borderBottom: '1px solid var(--border-faint)' },
        }}
        title={
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <KeyOutlined style={{ color: 'var(--accent)' }} />
            <span style={{ fontSize: 14, fontWeight: 500 }}>MiMo 余额凭证</span>
          </div>
        }
      >
        <Text type="secondary" style={{ fontSize: 13, display: 'block', marginBottom: 12 }}>
          小米 MiMo 官方未开放余额接口，这里保存网页控制台的会话 Cookie，渠道页用它查询平台余额。
          凭证由小米服务端管理：过期后渠道页余额显示「查询失败（凭证已过期）」，回本页粘贴最新 Cookie 即可。
        </Text>
        <div
          style={{
            background: 'var(--bg-elevated)',
            borderRadius: 6,
            padding: '10px 14px',
            marginBottom: 12,
          }}
        >
          <Text type="secondary" style={{ fontSize: 12 }}>
            获取方式：浏览器登录 platform.xiaomimimo.com → F12 打开 DevTools → Network → 刷新后点任一 api/v1 请求 →
            Request Headers 里复制 <Text code style={{ fontSize: 12 }}>Cookie</Text> 整行。
            最小需要 <Text code style={{ fontSize: 12 }}>userId</Text> 与{' '}
            <Text code style={{ fontSize: 12 }}>api-platform_serviceToken</Text> 两个值。
          </Text>
        </div>
        <Input.TextArea
          rows={4}
          value={mimoCookie}
          onChange={(e) => setMimoCookie(e.target.value)}
          placeholder='userId=266075060; api-platform_serviceToken="…"; …'
          style={{ fontFamily: 'monospace', fontSize: 12, marginBottom: 12 }}
        />
        <Space size={12} wrap>
          <Button type="primary" onClick={() => void saveMimoCookie()} loading={mimoSaving}>
            保存并验证
          </Button>
          {mimoVerify && (
            <Text type={mimoVerify.ok ? 'success' : 'danger'} style={{ fontSize: 13 }}>
              {mimoVerify.text}
            </Text>
          )}
        </Space>
        <Text type="secondary" style={{ fontSize: 12, display: 'block', marginTop: 12 }}>
          {mimoUpdatedAt ? `凭证更新于 ${new Date(mimoUpdatedAt).toLocaleString()}。` : '尚未配置凭证。'}
          与渠道 api_key 同一安全姿态：明文存储，仅本机管理后台可见。
        </Text>
      </Card>

      <Card
        className="panel"
        styles={{
          body: { padding: '16px 20px' },
          header: { borderBottom: '1px solid var(--border-faint)' },
        }}
        title={
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <InfoCircleOutlined style={{ color: 'var(--accent)' }} />
            <span style={{ fontSize: 14, fontWeight: 500 }}>模型价格表</span>
          </div>
        }
        extra={
          <Space>
            <Button size="small" icon={<SyncOutlined />} onClick={() => void syncPrices()} loading={syncing}>
              立即同步
            </Button>
            <Popconfirm
              title="按当前价格重算全部历史费用？"
              description="会覆盖已有费用值；保留期内（默认 90 天）的请求都会重新计价"
              onConfirm={() => void recompute()}
              okText="重算"
              cancelText="取消"
            >
              <Button size="small" icon={<ReloadOutlined />} loading={recomputing}>
                重算历史费用
              </Button>
            </Popconfirm>
          </Space>
        }
      >
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 16, alignItems: 'center', marginBottom: 12 }}>
          <Text type="secondary" style={{ fontSize: 12 }}>
            单价单位：<Text code style={{ fontSize: 12 }}>该行币种 / 百万 token</Text>（同步来源恒为 USD）
          </Text>
          <Text type="secondary" style={{ fontSize: 12 }}>
            上次同步：{lastSyncText} · 共 {status?.price_count ?? 0} 个模型
          </Text>
          {status?.last_error && (
            <Text type="danger" style={{ fontSize: 12 }}>
              上次同步失败：{status.last_error}
            </Text>
          )}
          {!status?.enabled && (
            <Text type="warning" style={{ fontSize: 12 }}>
              计价功能未启用（configs/config.yaml 的 pricing.enabled）
            </Text>
          )}
        </div>

        {unpriced.length > 0 && (
          <Alert
            type="warning"
            showIcon
            icon={<WarningOutlined />}
            style={{ marginBottom: 12 }}
            message={`有 ${unpriced.length} 个模型有用量但没有价格，费用按 0 计`}
            description={
              <span style={{ fontSize: 12 }}>
                {unpriced.slice(0, 10).map((u) => u.model).join('、')}
                {unpriced.length > 10 ? ` 等 ${unpriced.length} 个` : ''}。可点「新增手工价格」
                为它们补上单价，然后点「重算历史费用」补齐历史花费。
              </span>
            }
          />
        )}

        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 12, alignItems: 'center', marginBottom: 12 }}>
          <Input.Search
            allowClear
            placeholder="按模型名搜索（如 gpt-4o）"
            value={priceQuery}
            onChange={(e) => setPriceQuery(e.target.value)}
            onSearch={(v) => void loadPrices(1, v, usedOnly)}
            style={{ width: 260 }}
          />
          <Space size={6}>
            <Switch
              size="small"
              checked={usedOnly}
              onChange={(v) => {
                setUsedOnly(v)
                void loadPrices(1, priceQuery, v)
              }}
            />
            <Text type="secondary" style={{ fontSize: 12 }}>
              只看有用量的模型
            </Text>
          </Space>
          <Button
            size="small"
            icon={<PlusOutlined />}
            onClick={() => setEditing({ ...emptyPriceForm })}
          >
            新增手工价格
          </Button>
          <Text type="secondary" style={{ fontSize: 12 }}>
            搜索可查全量价格表（共 {status?.price_count ?? 0} 条）
          </Text>
        </div>

        <Table
          dataSource={prices}
          columns={priceColumns}
          rowKey="id"
          size="small"
          loading={priceLoading}
          scroll={{ x: 720 }}
          pagination={{
            current: pricePage,
            pageSize: PRICE_PER_PAGE,
            total: priceTotal,
            showSizeChanger: false,
            onChange: (p) => void loadPrices(p),
          }}
        />
      </Card>

      <Card
        className="panel"
        styles={{
          body: { padding: '16px 20px' },
          header: { borderBottom: '1px solid var(--border-faint)' },
        }}
        title={
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <InfoCircleOutlined style={{ color: 'var(--accent)' }} />
            <span style={{ fontSize: 14, fontWeight: 500 }}>计价规则</span>
          </div>
        }
      >
        <ul style={{ margin: 0, padding: '0 0 0 20px' }}>
          <li>
            <Text type="secondary" style={{ fontSize: 13 }}>
              费用在网关收到上游响应后即时计算并随请求日志落库，历史记录不会因价格变动而改变（可手工「重算历史费用」）
            </Text>
          </li>
          <li>
            <Text type="secondary" style={{ fontSize: 13 }}>
              输入侧按上游口径拆分：OpenAI 系 <Text code style={{ fontSize: 12 }}>prompt_tokens</Text> 已含缓存命中，
              未命中部分 = prompt − cached；Anthropic 系 <Text code style={{ fontSize: 12 }}>input_tokens</Text> 不含缓存读写，
              缓存读/写各自单独计费
            </Text>
          </li>
          <li>
            <Text type="secondary" style={{ fontSize: 13 }}>
              缓存费率留空时按输入费率计（与公开价格表的口径一致）
            </Text>
          </li>
          <li>
            <Text type="secondary" style={{ fontSize: 13 }}>
              长上下文只支持单档：填写阈值后，请求 prompt 超过该阈值时整单改用「超阈值」费率；上游多档阶梯价按第一档近似
            </Text>
          </li>
          <li>
            <Text type="secondary" style={{ fontSize: 13 }}>
              上游未报告 usage、由本地估算 token 的请求，其费用同样是估算值
            </Text>
          </li>
        </ul>
      </Card>

      <Modal
        open={!!editing}
        title={editing?.id ? `编辑价格 · ${editing.model}` : '新增手工价格'}
        onCancel={() => setEditing(null)}
        onOk={() => void savePrice()}
        confirmLoading={savingPrice}
        okText="保存"
        cancelText="取消"
        destroyOnHidden
      >
        {editing && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <div>
              <Text type="secondary" style={{ fontSize: 12, marginBottom: 4, display: 'block' }}>
                模型名（与请求日志中的模型名一致）
              </Text>
              <Input
                value={editing.model}
                disabled={!!editing.id}
                onChange={(e) => setEditing({ ...editing, model: e.target.value })}
                placeholder="例如: deepseek-flash"
              />
            </div>
            <div>
              <Text type="secondary" style={{ fontSize: 12, marginBottom: 4, display: 'block' }}>
                计价币种（下游按此币种标价；人民币行按设置页汇率折成美元记账）
              </Text>
              <Segmented
                value={editing.currency}
                onChange={(v) => setEditing({ ...editing, currency: v as 'USD' | 'CNY' })}
                options={[
                  { label: '人民币 ¥', value: 'CNY' },
                  { label: '美元 $', value: 'USD' },
                ]}
              />
            </div>
            <div style={{ display: 'flex', gap: 12 }}>
              <div style={{ flex: 1 }}>
                <Text type="secondary" style={{ fontSize: 12, marginBottom: 4, display: 'block' }}>
                  输入 {currencySymbol(editing.currency)} / 百万
                </Text>
                <InputNumber
                  min={0}
                  step={0.1}
                  precision={6}
                  value={editing.input}
                  onChange={(v) => setEditing({ ...editing, input: Number(v) || 0 })}
                  style={{ width: '100%' }}
                />
              </div>
              <div style={{ flex: 1 }}>
                <Text type="secondary" style={{ fontSize: 12, marginBottom: 4, display: 'block' }}>
                  输出 {currencySymbol(editing.currency)} / 百万
                </Text>
                <InputNumber
                  min={0}
                  step={0.1}
                  precision={6}
                  value={editing.output}
                  onChange={(v) => setEditing({ ...editing, output: Number(v) || 0 })}
                  style={{ width: '100%' }}
                />
              </div>
            </div>
            <div style={{ display: 'flex', gap: 12 }}>
              <div style={{ flex: 1 }}>
                <Text type="secondary" style={{ fontSize: 12, marginBottom: 4, display: 'block' }}>
                  缓存读＝缓存命中 {currencySymbol(editing.currency)} / 百万（留 0 = 按输入价计）
                </Text>
                <InputNumber
                  min={0}
                  step={0.01}
                  precision={6}
                  value={editing.cacheRead}
                  onChange={(v) => setEditing({ ...editing, cacheRead: Number(v) || 0 })}
                  style={{ width: '100%' }}
                />
              </div>
              <div style={{ flex: 1 }}>
                <Text type="secondary" style={{ fontSize: 12, marginBottom: 4, display: 'block' }}>
                  缓存写 {currencySymbol(editing.currency)} / 百万（可空）
                </Text>
                <InputNumber
                  min={0}
                  step={0.01}
                  precision={6}
                  value={editing.cacheWrite}
                  onChange={(v) => setEditing({ ...editing, cacheWrite: Number(v) || 0 })}
                  style={{ width: '100%' }}
                />
              </div>
            </div>
            <Alert
              type="info"
              showIcon
              style={{ padding: '6px 10px' }}
              message={
                <Text style={{ fontSize: 12 }}>
                  国内模型多为「缓存命中」单独计价（常远低于输入价）。<b>若此处的缓存读留 0</b>，
                  命中的输入 token 会按输入价计费——缓存命中率高的场景会显著<b>高估</b>费用。
                </Text>
              }
            />
            {/* 时段价：高峰时段按上面的基础费率，其余时段按下面的空闲费率 */}
            <div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4 }}>
                <Text type="secondary" style={{ fontSize: 12 }}>
                  高峰时段（北京时间，1=周一..7=周日；留空 = 不分时段）
                </Text>
                <Button
                  type="link"
                  size="small"
                  style={{ padding: 0, height: 'auto', fontSize: 12 }}
                  onClick={() => setEditing({ ...editing, peakWindow: DEEPSEEK_PEAK_WINDOW })}
                >
                  套用 DeepSeek 模板
                </Button>
              </div>
              <Input
                value={editing.peakWindow}
                onChange={(e) => setEditing({ ...editing, peakWindow: e.target.value })}
                placeholder={DEEPSEEK_PEAK_WINDOW}
                className="mono"
              />
            </div>
            {editing.peakWindow.trim() !== '' && (
              <div>
                <Text type="secondary" style={{ fontSize: 12, marginBottom: 4, display: 'block' }}>
                  空闲时段费率 {currencySymbol(editing.currency)} / 百万（留 0 = 回退高峰费率）
                </Text>
                <div style={{ display: 'flex', gap: 12 }}>
                  <InputNumber
                    min={0}
                    step={0.1}
                    precision={6}
                    addonBefore="输入"
                    value={editing.offPeakInput}
                    onChange={(v) => setEditing({ ...editing, offPeakInput: Number(v) || 0 })}
                    style={{ width: '100%' }}
                  />
                  <InputNumber
                    min={0}
                    step={0.1}
                    precision={6}
                    addonBefore="输出"
                    value={editing.offPeakOutput}
                    onChange={(v) => setEditing({ ...editing, offPeakOutput: Number(v) || 0 })}
                    style={{ width: '100%' }}
                  />
                </div>
                <div style={{ display: 'flex', gap: 12, marginTop: 12 }}>
                  <InputNumber
                    min={0}
                    step={0.01}
                    precision={6}
                    addonBefore="命中"
                    value={editing.offPeakCacheRead}
                    onChange={(v) => setEditing({ ...editing, offPeakCacheRead: Number(v) || 0 })}
                    style={{ width: '100%' }}
                  />
                  <InputNumber
                    min={0}
                    step={0.01}
                    precision={6}
                    addonBefore="缓存写"
                    value={editing.offPeakCacheWrite}
                    onChange={(v) => setEditing({ ...editing, offPeakCacheWrite: Number(v) || 0 })}
                    style={{ width: '100%' }}
                  />
                </div>
              </div>
            )}
            <div>
              <Text type="secondary" style={{ fontSize: 12, marginBottom: 4, display: 'block' }}>
                长上下文阈值（token，0 = 不分档）
              </Text>
              <InputNumber
                min={0}
                step={1000}
                value={editing.threshold}
                onChange={(v) => setEditing({ ...editing, threshold: Number(v) || 0 })}
                style={{ width: '100%' }}
                placeholder="例如: 200000"
              />
            </div>
            {editing.threshold > 0 && (
              <div style={{ display: 'flex', gap: 12 }}>
                <div style={{ flex: 1 }}>
                  <Text type="secondary" style={{ fontSize: 12, marginBottom: 4, display: 'block' }}>
                    超阈值输入
                  </Text>
                  <InputNumber
                    min={0}
                    step={0.1}
                    precision={6}
                    value={editing.inputAbove}
                    onChange={(v) => setEditing({ ...editing, inputAbove: Number(v) || 0 })}
                    style={{ width: '100%' }}
                  />
                </div>
                <div style={{ flex: 1 }}>
                  <Text type="secondary" style={{ fontSize: 12, marginBottom: 4, display: 'block' }}>
                    超阈值输出
                  </Text>
                  <InputNumber
                    min={0}
                    step={0.1}
                    precision={6}
                    value={editing.outputAbove}
                    onChange={(v) => setEditing({ ...editing, outputAbove: Number(v) || 0 })}
                    style={{ width: '100%' }}
                  />
                </div>
              </div>
            )}
            <Text type="secondary" style={{ fontSize: 12 }}>
              编辑后该行会标记为「手工」，自动同步不再覆盖它。
            </Text>
          </div>
        )}
      </Modal>

      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 12 }}>
        <Button
          icon={<ReloadOutlined />}
          onClick={() => {
            void loadConfigs()
            void loadPrices(pricePage)
          }}
          loading={loading || priceLoading}
        >
          重新加载
        </Button>
      </div>
    </Reveal>
  )
}

import { useCallback, useEffect, useState } from 'react'
import { App, Card, Input, Select, Space, Switch, Table, Tag, Tooltip } from 'antd'
import type { TableColumnsType } from 'antd'
import { LoadingOutlined, ReloadOutlined } from '@ant-design/icons'
import LiveDuration from '../components/LiveDuration'
import LogDetail from '../components/LogDetail'
import { channelApi, type Channel } from '../api/channel'
import { keyApi, type APIKey } from '../api/key'
import { logApi, type LogQuery, type RequestLog } from '../api/log'
import { WS_EVENTS, useWsEvent, useWsReconnected } from '../api/ws'
import { agentShort, compactCN, duration, fullTime, hitRateColor, modeShort, percent, protocolShort, timeOf } from '../utils/format'
import { useIsMobile } from '../utils/useIsMobile'

const PROTOCOL_OPTIONS = [
  { value: 'chat_completions', label: 'Chat Completions' },
  { value: 'responses', label: 'Responses' },
  { value: 'messages', label: 'Messages' },
]
const MODE_OPTIONS = [
  { value: 'native_passthrough', label: '原生透传' },
  { value: 'converted', label: '协议转换' },
]
const HOUR_OPTIONS = [
  { value: 1, label: '近 1 小时' },
  { value: 24, label: '近 24 小时' },
  { value: 24 * 7, label: '近 7 天' },
  { value: 0, label: '全部' },
]

/** 进行中请求：实时推送来的行尚未落库（无 id），完成事件按 req_id 原位替换。 */
const isPending = (r: RequestLog) => !r.id && !!r.req_id

/** 桌面端列。 */
const LOG_COLUMNS: TableColumnsType<RequestLog> = [
  {
    key: 'time',
    title: '时间',
    width: 150,
    fixed: 'left',
    render: (_, r) => <span className="mono" style={{ color: 'var(--text-faint)' }}>{fullTime(r.created_at)}</span>,
  },
  {
    key: 'protocol',
    title: '协议',
    dataIndex: 'protocol',
    width: 110,
    render: (v) => <Tag color="cyan" style={{ background: 'transparent' }}>{protocolShort(v)}</Tag>,
  },
  {
    key: 'mode',
    title: '模式',
    dataIndex: 'forward_mode',
    width: 90,
    render: (v, r) => {
      if (isPending(r) || !v) return <span style={{ color: 'var(--text-faint)' }}>—</span>
      return v === 'native_passthrough' ? (
        <Tag style={{ background: 'transparent', color: 'var(--accent)', borderColor: 'var(--accent)' }}>{modeShort(v)}</Tag>
      ) : (
        <Tag style={{ background: 'transparent', color: 'var(--amber)', borderColor: 'var(--amber)' }}>{modeShort(v)}</Tag>
      )
    },
  },
  { key: 'model', title: '模型', dataIndex: 'model', width: 170, ellipsis: true, render: (v) => <Tooltip title={v}><span className="mono">{v}</span></Tooltip> },
  { key: 'channel', title: '渠道', dataIndex: 'channel_name', width: 130, ellipsis: true },
  {
    key: 'caller',
    title: '调用方',
    dataIndex: 'key_name',
    width: 120,
    ellipsis: true,
    render: (v) => (v ? <span className="mono" style={{ fontSize: 12 }}>{v}</span> : <span style={{ color: 'var(--text-faint)' }}>—</span>),
  },
  {
    key: 'client',
    title: '客户端',
    width: 130,
    ellipsis: true,
    render: (_, r) => (
      <Tooltip
        title={
          <div style={{ fontSize: 12 }}>
            <div className="mono">{r.user_agent || '未提供 User-Agent'}</div>
            {r.client_ip && <div className="mono" style={{ color: 'var(--text-faint)', marginTop: 2 }}>IP {r.client_ip}</div>}
          </div>
        }
      >
        <span className="mono" style={{ fontSize: 12 }}>{agentShort(r.user_agent)}</span>
      </Tooltip>
    ),
  },
  { key: 'stream', title: '流式', dataIndex: 'stream', width: 60, render: (v) => (v ? <span className="mono">SSE</span> : <span style={{ color: 'var(--text-faint)' }}>—</span>) },
  {
    key: 'prompt',
    title: 'Prompt',
    dataIndex: 'prompt_tokens',
    width: 90,
    render: (v, r) => (isPending(r) ? <span style={{ color: 'var(--text-faint)' }}>—</span> : <span className="mono">{compactCN(v)}</span>),
  },
  {
    key: 'completion',
    title: 'Completion',
    dataIndex: 'completion_tokens',
    width: 110,
    render: (v, r) => (isPending(r) ? <span style={{ color: 'var(--text-faint)' }}>—</span> : <span className="mono">{compactCN(v)}</span>),
  },
  {
    key: 'cache',
    title: '缓存命中',
    width: 130,
    render: (_, r) => {
      if (isPending(r)) return <span style={{ color: 'var(--text-faint)' }}>—</span>
      return (
        <Tooltip title={`cached ${r.cached_tokens} / write ${r.cache_write_tokens}`}>
          <span className="mono" style={{ color: hitRateColor(r.cache_hit_rate) }}>
            {r.cached_tokens > 0 ? `${compactCN(r.cached_tokens)} (${percent(r.cache_hit_rate, 0)})` : '—'}
          </span>
        </Tooltip>
      )
    },
  },
  {
    key: 'duration',
    title: '耗时',
    dataIndex: 'duration_ms',
    width: 90,
    sorter: (a, b) => a.duration_ms - b.duration_ms,
    render: (_, r) => (isPending(r) ? <LiveDuration from={r.created_at} /> : <span className="mono">{duration(r.duration_ms)}</span>),
  },
  {
    key: 'status',
    title: '状态',
    dataIndex: 'upstream_status',
    width: 200,
    render: (v, r) =>
      isPending(r) ? (
        <Tag icon={<LoadingOutlined spin />} color="processing" style={{ background: 'transparent' }}>运行中</Tag>
      ) : r.error ? (
        <Tooltip title={r.error} placement="left">
          <Tag color="error" style={{ background: 'transparent', maxWidth: 170, overflow: 'hidden', textOverflow: 'ellipsis' }}>
            {v || 'ERR'} · {r.error}
          </Tag>
        </Tooltip>
      ) : (
        <Tag color="success" style={{ background: 'transparent' }}>{v}</Tag>
      ),
  },
]

/** 移动端列：时间/模型/Token 合计/状态，其余收进展开行。 */
const MOBILE_LOG_COLUMNS: TableColumnsType<RequestLog> = [
  {
    key: 'time',
    title: '时间',
    width: 76,
    render: (_, r) => <span className="mono" style={{ color: 'var(--text-faint)' }}>{r.created_at ? timeOf(r.created_at) : ''}</span>,
  },
  {
    key: 'model',
    title: '模型',
    dataIndex: 'model',
    width: 150,
    ellipsis: true,
    render: (v) => <span className="mono">{v}</span>,
  },
  {
    key: 'tokens',
    title: 'Tokens',
    width: 110,
    render: (_, r) =>
      isPending(r) ? (
        <span style={{ color: 'var(--text-faint)' }}>—</span>
      ) : (
        <Tooltip title={`入 ${compactCN(r.prompt_tokens)} · 出 ${compactCN(r.completion_tokens)}`}>
          <span className="mono">
            {compactCN(r.total_tokens)}
            <span style={{ fontSize: 11, color: 'var(--text-faint)', marginLeft: 4 }}>出{compactCN(r.completion_tokens)}</span>
          </span>
        </Tooltip>
      ),
  },
  {
    key: 'status',
    title: '状态',
    width: 96,
    render: (v, r) =>
      isPending(r) ? (
        <Tag icon={<LoadingOutlined spin />} color="processing" style={{ background: 'transparent' }}>运行中</Tag>
      ) : r.error ? (
        <Tooltip title={r.error} placement="left">
          <Tag color="error" style={{ background: 'transparent' }}>{v || 'ERR'}</Tag>
        </Tooltip>
      ) : (
        <Tag color="success" style={{ background: 'transparent' }}>{v}</Tag>
      ),
  },
]

/** 请求日志页。 */
export default function RequestLogs() {
  const { message } = App.useApp()
  const isMobile = useIsMobile()
  const [items, setItems] = useState<RequestLog[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(20)
  const [loading, setLoading] = useState(false)
  const [channels, setChannels] = useState<Channel[]>([])
  const [keys, setKeys] = useState<APIKey[]>([])
  const [filters, setFilters] = useState<LogQuery>({})

  const load = useCallback(
    async (p = page) => {
      setLoading(true)
      try {
        const data = await logApi.list({ page: p, per_page: perPage, ...filters })
        setItems(data.items ?? [])
        setTotal(data.total)
      } catch (e) {
        message.error((e as Error).message)
      } finally {
        setLoading(false)
      }
    },
    [page, perPage, filters, message],
  )

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    void channelApi.list({ per_page: 200 }).then((d) => setChannels(d.items ?? [])).catch(() => undefined)
    void keyApi.list({ per_page: 200 }).then((d) => setKeys(d.items ?? [])).catch(() => undefined)
  }, [])

  // 新请求受理即插入"运行中"行（仅第 1 页且无筛选时）；完成事件原位替换并计数。
  // 幂等保护：completed 偶发先于 started 到达（或重复推送）时，以先到者为准。
  useWsEvent(WS_EVENTS.requestStarted, (msg) => {
    if (page === 1 && Object.keys(filters).length === 0) {
      const l = msg.payload as RequestLog
      setItems((prev) =>
        l.req_id && prev.some((x) => x.req_id === l.req_id)
          ? prev
          : [l, ...prev].slice(0, perPage),
      )
    }
  })
  useWsEvent(WS_EVENTS.requestCompleted, (msg) => {
    if (page === 1 && Object.keys(filters).length === 0) {
      const l = msg.payload as RequestLog
      setItems((prev) => {
        if (l.req_id && prev.some((x) => x.req_id === l.req_id && !!x.id)) return prev
        const idx = l.req_id ? prev.findIndex((x) => x.req_id === l.req_id && !x.id) : -1
        if (idx >= 0) {
          const next = [...prev]
          next[idx] = l
          return next
        }
        return [l, ...prev].slice(0, perPage)
      })
      setTotal((t) => t + 1)
    }
  })

  // 断线重连后补拉一次：断连窗口内 started/completed 事件已丢失
  useWsReconnected(() => void load())

  // 兜底清理：WS 断连窗口内 completed 丢失的"运行中"行，超过 5 分钟自动移除
  useEffect(() => {
    const t = setInterval(() => {
      setItems((prev) => {
        const cutoff = Date.now() - 5 * 60 * 1000
        const next = prev.filter((r) => !isPending(r) || new Date(r.created_at).getTime() > cutoff)
        return next.length === prev.length ? prev : next
      })
    }, 10_000)
    return () => clearInterval(t)
  }, [])

  const setFilter = (patch: Partial<LogQuery>) => {
    setPage(1)
    setFilters((f) => ({ ...f, ...patch }))
  }

  return (
    <Card
      className="panel"
      styles={{ body: { padding: 0 }, header: { borderBottom: "1px solid var(--border-faint)", paddingInline: 16 } }}
      title={
        <span style={{ fontSize: 13, color: 'var(--text-secondary)', letterSpacing: '0.06em' }}>
          请求日志 · Token 计量与缓存命中
        </span>
      }
      extra={
        <Space wrap size={8}>
          <Select
            placeholder="协议"
            allowClear
            style={{ minWidth: 150 }}
            options={PROTOCOL_OPTIONS}
            onChange={(v) => setFilter({ protocol: v })}
          />
          <Select
            placeholder="模式"
            allowClear
            style={{ minWidth: 110 }}
            options={MODE_OPTIONS}
            onChange={(v) => setFilter({ forward_mode: v })}
          />
          <Select
            placeholder="渠道"
            allowClear
            showSearch
            optionFilterProp="label"
            style={{ minWidth: 140 }}
            options={channels.map((c) => ({ value: c.id, label: c.name }))}
            onChange={(v) => setFilter({ channel_id: v })}
          />
          <Select
            placeholder="调用方"
            allowClear
            showSearch
            optionFilterProp="label"
            style={{ minWidth: 130 }}
            options={keys.map((k) => ({ value: k.id, label: k.name }))}
            onChange={(v) => setFilter({ key_id: v })}
          />
          <Input.Search
            placeholder="模型名"
            allowClear
            style={{ width: 160 }}
            onSearch={(v) => setFilter({ model: v || undefined })}
          />
          <Select style={{ width: 110 }} defaultValue={24} options={HOUR_OPTIONS} onChange={(v) => setFilter({ hours: v })} />
          <Tooltip title="仅看失败请求">
            <Space size={4}>
              <span style={{ color: 'var(--text-secondary)', fontSize: 12 }}>仅错误</span>
              <Switch size="small" onChange={(v) => setFilter({ error_only: v })} />
            </Space>
          </Tooltip>
          <button
            className="ant-btn ant-btn-default"
            style={{ height: 32, borderRadius: 6, color: 'var(--text-secondary)', background: 'transparent', border: '1px solid var(--border-faint)', cursor: 'pointer' }}
            onClick={() => void load()}
          >
            <ReloadOutlined />
          </button>
        </Space>
      }
    >
      <Table<RequestLog>
        rowKey={(r) => (r.req_id && !r.id ? `live-${r.req_id}` : `${r.id}`)}
        loading={loading}
        dataSource={items}
        size="small"
        scroll={{ x: isMobile ? 480 : 1580 }}
        onChange={(pager) => {
          setPage(pager.current ?? 1)
          setPerPage(pager.pageSize ?? 20)
        }}
        pagination={{
          current: page,
          pageSize: perPage,
          total,
          showSizeChanger: true,
          pageSizeOptions: [10, 20, 50, 100],
          showTotal: (t) => <span className="mono" style={{ color: 'var(--text-faint)' }}>共 {t} 条</span>,
        }}
        columns={isMobile ? MOBILE_LOG_COLUMNS : LOG_COLUMNS}
        expandable={isMobile ? { expandedRowRender: (r) => <LogDetail r={r} pending={isPending(r)} /> } : undefined}
      />
    </Card>
  )
}

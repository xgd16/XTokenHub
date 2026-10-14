import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { App, Button, Input, Spin, Switch, Tooltip } from 'antd'
import { CopyOutlined, ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import { modelsApi, type ModelUsage, type ModelWindow } from '../api/stats'
import { WS_EVENTS, useWsEvent, useWsReconnected } from '../api/ws'
import { PageHero } from '../components/PageHero'
import { compactCN, compactNumber, obsColorStyle } from '../utils/format'
import { Reveal } from '../utils/motion'
import { copyText } from '../utils/clipboard'

/** 单张模型用量卡片：模型名（点击复制）+ 1h/24h/7d/30d 四个窗口的请求与 token。 */
const ModelCard = memo(function ModelCard({ m }: { m: ModelUsage }) {
  const { message } = App.useApp()
  const copyName = async () => {
    const ok = await copyText(m.model)
    if (ok) message.success(`已复制 ${m.model}`)
    else message.error('复制失败，请手动选择复制')
  }
  const windows: [string, ModelWindow][] = [
    ['1 小时', m.w_1h],
    ['24 小时', m.w_24h],
    ['7 天', m.w_7d],
    ['30 天', m.w_30d],
  ]
  // 30 天有用量的模型亮起身份点，长期未用的模型转灰
  const hasUsage = m.w_30d.requests > 0
  return (
    <div className="obs-card" style={{ ...obsColorStyle(hasUsage ? 'var(--accent)' : 'var(--text-faint)'), gap: 10, padding: 14 }}>
      <span className="obs-card-glow" aria-hidden="true" />
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
        <span className="obs-card-dot" aria-hidden="true" />
        <Tooltip title="点击复制模型名">
          <span
            className="mono"
            style={{
              flex: 1,
              minWidth: 0,
              fontWeight: 600,
              fontSize: 13,
              cursor: 'pointer',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
            onClick={() => void copyName()}
          >
            {m.model}
          </span>
        </Tooltip>
        <Button size="small" type="text" icon={<CopyOutlined />} onClick={() => void copyName()} />
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 6 }}>
        {windows.map(([label, w]) => (
          <div key={label} style={{ background: 'var(--bg-elevated)', borderRadius: 6, padding: '6px 10px' }}>
            <div style={{ fontSize: 11, color: 'var(--text-faint)', letterSpacing: '0.04em' }}>{label}</div>
            <div className="mono" style={{ fontSize: 13, marginTop: 2 }}>
              {compactNumber(w.requests)} <span style={{ color: 'var(--text-faint)', fontSize: 11 }}>次</span>
            </div>
            <div className="mono" style={{ fontSize: 12, marginTop: 1, color: w.total_tokens > 0 ? 'var(--accent)' : 'var(--text-faint)' }}>
              {compactCN(w.total_tokens)} tok
            </div>
          </div>
        ))}
      </div>
    </div>
  )
})

/** 模型用量页：全部模型卡片 + 1h/24h/7d/30d 使用量。 */
export default function Models() {
  const { message } = App.useApp()
  const [items, setItems] = useState<ModelUsage[]>([])
  const [loading, setLoading] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [onlyUsed, setOnlyUsed] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setItems(await modelsApi.usage())
    } catch (e) {
      message.error((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [message])

  useEffect(() => {
    void load()
  }, [load])

  // 完成事件节流刷新（10s），用量准实时且不随请求频次打接口
  const lastRefreshRef = useRef(0)
  useWsEvent(WS_EVENTS.requestCompleted, () => {
    const now = Date.now()
    if (now - lastRefreshRef.current > 10_000) {
      lastRefreshRef.current = now
      void load()
    }
  })

  // 断线重连后补拉一次，避免断连窗口内事件丢失
  useWsReconnected(() => void load())

  const filtered = useMemo(() => {
    const kw = keyword.trim().toLowerCase()
    return items.filter((m) => (!onlyUsed || m.w_30d.requests > 0) && (!kw || m.model.toLowerCase().includes(kw)))
  }, [items, keyword, onlyUsed])

  const used30 = useMemo(() => items.filter((m) => m.w_30d.requests > 0).length, [items])

  return (
    <Reveal className="obs-page">
      <PageHero
        eyebrow="models · 用量审查"
        title="模型用量"
        desc="全部已知模型的 1 小时 / 24 小时 / 7 天 / 30 天窗口用量；网关请求产生后实时汇总。"
        readouts={[
          { label: '模型总数', value: items.length, tone: 'accent' },
          { label: '30 天有用量', value: used30 },
          { label: '当前展示', value: filtered.length },
        ]}
        actions={
          <>
            <Input
              allowClear
              prefix={<SearchOutlined style={{ color: 'var(--text-faint)' }} />}
              placeholder="筛选模型名"
              style={{ width: 200 }}
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
            />
            <span style={{ color: 'var(--text-secondary)', fontSize: 12, display: 'inline-flex', alignItems: 'center', gap: 6 }}>
              仅看有用量
              <Switch size="small" checked={onlyUsed} onChange={setOnlyUsed} />
            </span>
            <Button icon={<ReloadOutlined />} onClick={() => void load()} aria-label="刷新" />
          </>
        }
      />
      <section className="obs-section">
        <div className="obs-section-head">
          <span>模型清单</span>
          <span className="obs-count-chip">{filtered.length}</span>
        </div>
        <Spin spinning={loading}>
          {filtered.length === 0 ? (
            <div className="obs-empty">
              <div className="obs-empty-title">{items.length === 0 ? '暂无模型' : '无匹配模型'}</div>
              <div className="obs-empty-hint">模型首次被请求后就会出现在这里</div>
            </div>
          ) : (
            <div className="obs-grid">
              {filtered.map((m, i) => (
                <Reveal key={m.model} className="obs-cell" delay={Math.min(i * 0.03, 0.35)}>
                  <ModelCard m={m} />
                </Reveal>
              ))}
            </div>
          )}
        </Spin>
      </section>
    </Reveal>
  )
}

import { memo, useEffect, useState } from 'react'
import { Tooltip, Typography } from 'antd'
import { pricingApi, type ModelPrice } from '../api/pricing'
import { formatPricePerMillion } from '../utils/money'
import { useNow } from '../utils/useNow'
import { nextPeakSwitch, peakPeriodAt, peakPeriodBounds, hasHolidayTokens, type PricePeriod } from '../utils/peakWindow'
import { useHolidayCalendar } from '../utils/useHolidayCalendar'

const { Text } = Typography

/** 配置了时段价的模型数上限（手工配置，实际远小于此）。 */
const PEAK_PRICE_PAGE_SIZE = 200

/** 时段主色：空闲取主题绿、高峰取琥珀，与仪表盘调色板同源。 */
const PERIOD_COLOR: Record<PricePeriod, string> = { peak: 'var(--amber)', off_peak: 'var(--accent)' }
const PERIOD_LABEL: Record<PricePeriod, string> = { peak: '高峰价', off_peak: '空闲价' }

/** 生效费率（原币种 / token），口径同后端 ModelPrice.Rates 的时段部分。 */
function effectiveRates(p: ModelPrice, offPeak: boolean) {
  let input = p.input_cost_per_token
  let output = p.output_cost_per_token
  let read = p.cache_read_cost_per_token
  let write = p.cache_write_cost_per_token
  if (offPeak) {
    if (p.off_peak_input_cost_per_token > 0) input = p.off_peak_input_cost_per_token
    if (p.off_peak_output_cost_per_token > 0) output = p.off_peak_output_cost_per_token
    read = p.off_peak_cache_read_cost_per_token
    write = p.off_peak_cache_write_cost_per_token
  }
  // 缓存费率缺失时回退为本次生效的输入费率（与后端一致）
  if (read <= 0) read = input
  if (write <= 0) write = input
  return { input, output, read, write }
}

/** 空闲相对高峰的输入侧折扣；无优惠返回 null。 */
function offPeakDiscount(p: ModelPrice): number | null {
  const peak = p.input_cost_per_token
  const off = p.off_peak_input_cost_per_token > 0 ? p.off_peak_input_cost_per_token : peak
  if (peak <= 0 || off >= peak) return null
  return Math.round((1 - off / peak) * 100)
}

/** 倒计时文案：1h35m / 47m / 2d3h。 */
function countdown(ms: number): string {
  const totalMin = Math.max(0, Math.round(ms / 60_000))
  const d = Math.floor(totalMin / 1440)
  const h = Math.floor((totalMin % 1440) / 60)
  const m = totalMin % 60
  if (d > 0) return `${d}d${h}h`
  if (h > 0) return `${h}h${m}m`
  return `${m}m`
}

/** 加载骨架：与 StatCard 的占位一致，避免该格出现时整行跳动。 */
function CardSkeleton() {
  return (
    <div className="panel" style={{ display: 'flex', flexDirection: 'column', height: '100%', padding: '16px 18px', overflow: 'hidden' }}>
      <Text style={{ color: 'var(--text-secondary)', fontSize: 12, letterSpacing: '0.06em' }}>峰谷价目</Text>
      <div style={{ marginTop: 6 }}>
        <div className="skeleton-stat-number" />
      </div>
      <div className="skeleton-stat-sub" />
    </div>
  )
}

/** 悬停明细：一个模型的时段规则与两档四类费率。 */
function ModelDetail({ p }: { p: ModelPrice }) {
  const peak = effectiveRates(p, false)
  const off = effectiveRates(p, true)
  const discount = offPeakDiscount(p)
  return (
    <div style={{ fontSize: 12 }}>
      <div className="mono">{p.model}</div>
      <div className="mono" style={{ marginTop: 2 }}>
        高峰 输入 {formatPricePerMillion(peak.input, p.currency)} / 输出 {formatPricePerMillion(peak.output, p.currency)}
      </div>
      <div className="mono" style={{ color: PERIOD_COLOR.off_peak }}>
        空闲 输入 {formatPricePerMillion(off.input, p.currency)} / 输出 {formatPricePerMillion(off.output, p.currency)}
        {discount != null && `（省 ${discount}%）`}
      </div>
      <div className="mono" style={{ color: 'var(--text-faint)' }}>
        缓存 高峰 {formatPricePerMillion(peak.read, p.currency)}/{formatPricePerMillion(peak.write, p.currency)} · 空闲{' '}
        {formatPricePerMillion(off.read, p.currency)}/{formatPricePerMillion(off.write, p.currency)}（读/写）
      </div>
      <div className="mono" style={{ marginTop: 4, color: 'var(--text-faint)' }}>
        高峰时段（北京时间）{p.peak_window}
      </div>
      {hasHolidayTokens(p.peak_window) && (
        <div style={{ marginTop: 2, color: 'var(--text-faint)' }}>
          记号 !H = 放假日（含调休连休）全天空闲价、W = 调休补班日按工作日（依节假日年历判定）
        </div>
      )}
      <div style={{ marginTop: 2, color: 'var(--text-faint)' }}>费率均为每百万 token，不含长上下文分档</div>
    </div>
  )
}

/**
 * 峰谷价目卡片：与统计卡同格同族，显示当前处于高峰价还是空闲价、生效费率、
 * 距下次切换的时间，以及本时段的进度。
 *
 * 多模型时聚焦「最近一次切换」的那个（最可操作），其余放进悬停明细；
 * 未配置时段价时整卡隐藏，把格子留给后续统计卡。
 */
export default memo(function PeakPriceCard() {
  const [prices, setPrices] = useState<ModelPrice[] | null>(null)
  const now = useNow()
  // 拉取节假日年历：`!H` / `W` 记号的时段判定依赖它
  useHolidayCalendar()

  useEffect(() => {
    let alive = true
    void (async () => {
      try {
        const res = await pricingApi.listPrices({ peak_only: true, page: 1, per_page: PEAK_PRICE_PAGE_SIZE })
        if (alive) setPrices(res.items ?? [])
      } catch {
        // 计价未启用或接口不可用时隐藏该卡，不干扰仪表盘其余内容
        if (alive) setPrices([])
      }
    })()
    return () => {
      alive = false
    }
  }, [])

  if (prices === null) return <CardSkeleton />
  if (prices.length === 0) return null

  const at = new Date(now)
  // 以「距下次切换最近」的模型为卡片主角
  const rows = prices.map((p) => ({
    p,
    period: peakPeriodAt(p.peak_window, at),
    next: nextPeakSwitch(p.peak_window, at),
    bounds: peakPeriodBounds(p.peak_window, at),
  }))
  const lead = rows.reduce((a, b) => ((b.next?.ms ?? Infinity) < (a.next?.ms ?? Infinity) ? b : a))
  const period = lead.period
  const color = period ? PERIOD_COLOR[period] : 'var(--text-secondary)'
  const rates = effectiveRates(lead.p, period === 'off_peak')
  const progress =
    lead.bounds && lead.next
      ? Math.min(1, Math.max(0, (now - lead.bounds.start.getTime()) / (lead.next.at.getTime() - lead.bounds.start.getTime())))
      : null

  return (
    <Tooltip
      title={
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {rows.map(({ p }) => (
            <ModelDetail key={p.id} p={p} />
          ))}
        </div>
      }
    >
      <div
        className="panel"
        style={{ display: 'flex', flexDirection: 'column', height: '100%', padding: '16px 18px', overflow: 'hidden' }}
      >
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
          <Text style={{ color: 'var(--text-secondary)', fontSize: 12, letterSpacing: '0.06em', whiteSpace: 'nowrap' }}>
            峰谷价目
          </Text>
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, flexShrink: 0 }}>
            <span className={`live-dot${period === 'peak' ? ' warn' : ''}`} style={period ? { background: color, boxShadow: `0 0 8px ${color}` } : undefined} />
            <span className="mono" style={{ fontSize: 12, color }}>
              {period ? PERIOD_LABEL[period] : '错峰'}
            </span>
          </span>
        </div>

        <div className="mono" style={{ fontSize: 26, fontWeight: 600, marginTop: 6, lineHeight: 1.2, whiteSpace: 'nowrap' }}>
          {formatPricePerMillion(rates.input, lead.p.currency)}
          <span style={{ color: 'var(--text-faint)' }}>/</span>
          {formatPricePerMillion(rates.output, lead.p.currency)}
          <span style={{ fontSize: 11, fontWeight: 400, color: 'var(--text-faint)', marginLeft: 6 }}>输入/输出 ·M</span>
        </div>

        <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, marginTop: 4, fontSize: 12, minWidth: 0 }}>
          <span
            className="mono"
            style={{ color: 'var(--text-secondary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
          >
            {lead.p.model}
          </span>
          {prices.length > 1 && (
            <span className="mono" style={{ color: 'var(--text-faint)', flexShrink: 0 }}>+{prices.length - 1}</span>
          )}
          {lead.next && (
            <span style={{ color: 'var(--text-faint)', flexShrink: 0, marginLeft: 'auto', whiteSpace: 'nowrap' }}>
              {countdown(lead.next.ms)} 后转{lead.next.to === 'peak' ? '高峰' : '空闲'}
            </span>
          )}
        </div>

        {progress != null && (
          <div style={{ marginTop: 'auto', paddingTop: 10 }}>
            <div style={{ height: 3, borderRadius: 2, background: 'var(--track-bg)', overflow: 'hidden' }}>
              <div
                style={{
                  height: '100%',
                  width: `${progress * 100}%`,
                  borderRadius: 2,
                  background: color,
                  boxShadow: `0 0 6px ${color}`,
                  transition: 'width 0.5s ease',
                }}
              />
            </div>
          </div>
        )}
      </div>
    </Tooltip>
  )
})

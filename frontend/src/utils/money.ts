/**
 * 金额展示工具。
 *
 * 全链路以 USD 存储与计算（后端 cost_usd），这里只负责展示换汇与格式化：
 * 汇率由设置页手工配置，不引入外部汇率接口。
 */

export type Currency = 'USD' | 'CNY'

export interface MoneyOptions {
  currency: Currency
  /** USD -> CNY 汇率（currency 为 CNY 时生效）。 */
  rate: number
}

export const DEFAULT_MONEY: MoneyOptions = { currency: 'USD', rate: 0 }

export const CURRENCY_SYMBOL: Record<Currency, string> = { USD: '$', CNY: '¥' }

/**
 * 归一化展示选项：选了 CNY 但汇率未配置时回退 USD，
 * 避免把美元数字直接套上 ¥ 符号（比不换算更误导）。
 */
export function normalizeMoney(opts: MoneyOptions): MoneyOptions {
  if (opts.currency === 'CNY' && !(opts.rate > 0)) {
    return { currency: 'USD', rate: 0 }
  }
  return opts
}

/** USD 金额换算为目标币种数值。 */
export function convertUSD(usd: number, opts: MoneyOptions): number {
  if (!Number.isFinite(usd)) return 0
  const o = normalizeMoney(opts)
  return o.currency === 'CNY' ? usd * o.rate : usd
}

/** 金额小数位：金额越大越省位，小额保留 4 位以便看清单次请求成本。 */
export function moneyDigits(value: number): number {
  const abs = Math.abs(value)
  if (abs >= 100) return 2
  if (abs >= 1) return 3
  return 4
}

/**
 * 金额格式化（USD 入参，按 opts 换算后展示）。零值显示为符号 + 0，
 * 无效值显示为符号 + —，便于表格里一眼区分「没有花费」与「数据缺失」。
 */
export function formatMoney(usd: number, opts: MoneyOptions = DEFAULT_MONEY): string {
  const o = normalizeMoney(opts)
  const symbol = CURRENCY_SYMBOL[o.currency] ?? '$'
  if (!Number.isFinite(usd)) return `${symbol}—`
  const v = convertUSD(usd, o)
  if (v === 0) return `${symbol}0`
  const digits = moneyDigits(v)
  return (
    symbol +
    v.toLocaleString('en-US', { minimumFractionDigits: digits, maximumFractionDigits: digits })
  )
}

/** 每百万 token 的换算基数：上游报价都按百万 token 计，后端按单 token 存储。 */
export const PER_MILLION = 1_000_000

/** 单 token 单价 -> 每百万 token 单价。 */
export function toPerMillion(perToken: number): number {
  if (!Number.isFinite(perToken)) return 0
  return perToken * PER_MILLION
}

/** 每百万 token 单价 -> 单 token 单价（入库用）。 */
export function fromPerMillion(perMillion: number): number {
  if (!Number.isFinite(perMillion)) return 0
  return perMillion / PER_MILLION
}

/**
 * 单 token 单价展示为「每百万 token」数值（不带符号，单位由列头说明）。
 * 单价可能小到 1e-8 USD，直接展示单 token 值无法阅读。
 */
export function formatPerMillion(perToken: number): string {
  const v = toPerMillion(perToken)
  if (v === 0) return '—'
  return v.toLocaleString('en-US', { maximumFractionDigits: 4 })
}

/** 币种符号：未知/空值按 USD 处理。 */
export function currencySymbol(currency?: string): string {
  return currency === 'CNY' ? CURRENCY_SYMBOL.CNY : CURRENCY_SYMBOL.USD
}

/**
 * 价格表用的「每百万 token」格式化，带上该行**自身**的币种符号。
 *
 * 与 formatMoney 不同：这里不做汇率换算——价格行存的就是该币种的原价
 * （人民币行就是人民币报价），换算反而会失真。
 */
export function formatPricePerMillion(perToken: number, currency?: string): string {
  const v = toPerMillion(perToken)
  if (!Number.isFinite(v) || v === 0) return '—'
  return (
    currencySymbol(currency) +
    v.toLocaleString('en-US', { maximumFractionDigits: 4 })
  )
}

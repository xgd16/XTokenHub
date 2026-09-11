import { describe, expect, it } from 'vitest'
import {
  CURRENCY_SYMBOL,
  convertUSD,
  currencySymbol,
  formatMoney,
  formatPerMillion,
  formatPricePerMillion,
  fromPerMillion,
  moneyDigits,
  normalizeMoney,
  toPerMillion,
} from './money'

describe('money', () => {
  it('convertUSD 只在币种为 CNY 且有汇率时换算', () => {
    expect(convertUSD(10, { currency: 'USD', rate: 7.2 })).toBe(10)
    expect(convertUSD(10, { currency: 'CNY', rate: 7.2 })).toBeCloseTo(72, 10)
    // 汇率缺失时回退 USD，避免把美元数字套上 ¥ 符号
    expect(convertUSD(10, { currency: 'CNY', rate: 0 })).toBe(10)
    expect(convertUSD(NaN, { currency: 'USD', rate: 0 })).toBe(0)
  })

  it('normalizeMoney 在 CNY 缺汇率时回退 USD', () => {
    expect(normalizeMoney({ currency: 'CNY', rate: 0 })).toEqual({ currency: 'USD', rate: 0 })
    expect(normalizeMoney({ currency: 'CNY', rate: 7.2 })).toEqual({ currency: 'CNY', rate: 7.2 })
    expect(normalizeMoney({ currency: 'USD', rate: 0 })).toEqual({ currency: 'USD', rate: 0 })
  })

  it('moneyDigits 金额越大保留位越少', () => {
    expect(moneyDigits(0.0123)).toBe(4)
    expect(moneyDigits(0.5)).toBe(4)
    expect(moneyDigits(1.5)).toBe(3)
    expect(moneyDigits(123.456)).toBe(2)
  })

  it('formatMoney 按币种与量级格式化', () => {
    const usd = { currency: 'USD' as const, rate: 0 }
    expect(formatMoney(0, usd)).toBe('$0')
    expect(formatMoney(0.0123, usd)).toBe('$0.0123')
    expect(formatMoney(1.5, usd)).toBe('$1.500')
    expect(formatMoney(1234.5678, usd)).toBe('$1,234.57')
    // NaN -> 占位符，与「零花费」区分开
    expect(formatMoney(NaN, usd)).toBe('$—')

    expect(formatMoney(10, { currency: 'CNY', rate: 7.2 })).toBe('¥72.000')
    // CNY 缺汇率 -> 回退 USD 符号
    expect(formatMoney(10, { currency: 'CNY', rate: 0 })).toBe('$10.000')
    expect(CURRENCY_SYMBOL.CNY).toBe('¥')
  })

  it('每百万 token 单价换算与展示', () => {
    expect(toPerMillion(0.000003)).toBeCloseTo(3, 10)
    expect(fromPerMillion(15)).toBeCloseTo(0.000015, 12)
    expect(formatPerMillion(0.0000025)).toBe('2.5')
    expect(formatPerMillion(0.000015)).toBe('15')
    expect(formatPerMillion(0)).toBe('—')
    // 往返不失真
    expect(fromPerMillion(toPerMillion(0.00000375))).toBeCloseTo(0.00000375, 15)
  })

  it('价格表按行币种加符号，且不做汇率换算', () => {
    // 人民币行显示人民币原价（2 元/百万），不参与汇率换算
    expect(formatPricePerMillion(0.000002, 'CNY')).toBe('¥2')
    expect(formatPricePerMillion(0.000008, 'CNY')).toBe('¥8')
    // 美元行加 $ 符号；空/未知币种按 USD 处理
    expect(formatPricePerMillion(0.000002, 'USD')).toBe('$2')
    expect(formatPricePerMillion(0.000002)).toBe('$2')
    expect(formatPricePerMillion(0.000002, '')).toBe('$2')
    // 零值与其不可读性保留占位符
    expect(formatPricePerMillion(0, 'CNY')).toBe('—')
    expect(currencySymbol('CNY')).toBe('¥')
    expect(currencySymbol('USD')).toBe('$')
    expect(currencySymbol(undefined)).toBe('$')
  })
})

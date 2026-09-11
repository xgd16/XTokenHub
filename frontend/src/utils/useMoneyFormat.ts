import { useEffect, useMemo, useState } from 'react'
import { pricingApi } from '../api/pricing'
import { DEFAULT_MONEY, formatMoney, type MoneyOptions } from './money'

/**
 * 展示币种/汇率的进程内缓存：仪表盘、请求日志、设置页共用一次请求。
 * 设置页保存后调用 setMoneyOptions()，所有已挂载组件立即同步。
 */
let cachedMoney: MoneyOptions = DEFAULT_MONEY
let inflight: Promise<MoneyOptions> | null = null
const listeners = new Set<(m: MoneyOptions) => void>()

function publish(m: MoneyOptions) {
  cachedMoney = m
  for (const l of listeners) l(m)
}

/** 拉取计费设置（并发去重）。失败时保留当前值，不阻塞页面。 */
export function loadMoneyOptions(force = false): Promise<MoneyOptions> {
  if (inflight && !force) return inflight
  inflight = pricingApi
    .getBilling()
    .then((b) => {
      const m: MoneyOptions = {
        currency: b?.display_currency === 'CNY' ? 'CNY' : 'USD',
        rate: Number(b?.usd_cny_rate) || 0,
      }
      publish(m)
      return m
    })
    .catch(() => cachedMoney)
    .finally(() => {
      inflight = null
    })
  return inflight
}

/** 设置页保存后调用：立刻把新的币种/汇率推给所有展示处。 */
export function setMoneyOptions(m: MoneyOptions) {
  publish(m)
}

/** 当前金额展示格式；返回的 format 已绑定币种与汇率。 */
export function useMoneyFormat(): { money: MoneyOptions; format: (usd: number) => string } {
  const [opts, setOpts] = useState<MoneyOptions>(cachedMoney)
  useEffect(() => {
    listeners.add(setOpts)
    // 首次挂载且尚未取到设置时拉一次；CNY 无汇率会在 formatMoney 内回退 USD
    if (cachedMoney === DEFAULT_MONEY) void loadMoneyOptions()
    return () => {
      listeners.delete(setOpts)
    }
  }, [])

  return useMemo(
    () => ({ money: opts, format: (usd: number) => formatMoney(usd, opts) }),
    [opts],
  )
}

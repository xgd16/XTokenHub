import { useEffect, useState } from 'react'
import { ensureHolidayCalendar } from '../api/holiday'

/**
 * 挂载时拉取（缓存注入）节假日年历，使 peakWindow 的 `!H` / `W` 记号判定生效。
 * 年历到位后触发一次重渲染，让当前时段标记立即反映节假日；
 * 拉取失败静默降级为按星期判定，不打扰页面。
 */
export function useHolidayCalendar(): void {
  const [, setTick] = useState(0)
  useEffect(() => {
    let alive = true
    void ensureHolidayCalendar()
      .catch(() => {})
      .finally(() => {
        if (alive) setTick((n) => n + 1)
      })
    return () => {
      alive = false
    }
  }, [])
}

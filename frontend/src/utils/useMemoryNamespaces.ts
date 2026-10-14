import { useEffect, useState } from 'react'
import { memoryApi, type MemoryNamespaceView } from '../api/memory'

// 模块级共享缓存：侧边栏子菜单与控制台页面共读一份命名空间清单，
// 命名空间增删改后调用 refreshMemoryNamespaces() 广播，避免各自轮询。

let cache: MemoryNamespaceView[] = []
let loaded = false
const subscribers = new Set<(items: MemoryNamespaceView[]) => void>()

function emit() {
  for (const fn of subscribers) fn(cache)
}

/** 拉取命名空间清单并广播；失败静默返回旧缓存（菜单不因接口异常中断）。 */
export async function refreshMemoryNamespaces(): Promise<MemoryNamespaceView[]> {
  try {
    const res = await memoryApi.namespaces()
    cache = res.items
    loaded = true
    emit()
  } catch {
    /* 保持旧缓存 */
  }
  return cache
}

/** 侧边栏/页面共用：命名空间清单（模块级缓存 + 订阅）。 */
export function useMemoryNamespaces(): MemoryNamespaceView[] {
  const [items, setItems] = useState(cache)
  useEffect(() => {
    subscribers.add(setItems)
    setItems(cache)
    if (!loaded) void refreshMemoryNamespaces()
    return () => {
      subscribers.delete(setItems)
    }
  }, [])
  return items
}

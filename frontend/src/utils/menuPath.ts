/**
 * 在菜单叶子路径集合中取与当前 pathname 匹配的最长前缀。
 * 规则：`/memories/all` 优先于 `/memories`（最长优先）；`/memories-foo` 不匹配 `/memories`
 * （按段边界匹配）；无匹配回退到根路径 `/`。
 */
export function bestMenuPath(pathname: string, paths: string[]): string {
  let best = '/'
  for (const key of paths) {
    if (key === '/' || !key.startsWith('/')) continue
    if ((pathname === key || pathname.startsWith(`${key}/`)) && key.length > best.length) {
      best = key
    }
  }
  return best
}

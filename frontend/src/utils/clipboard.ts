/**
 * 复制文本到剪贴板。
 * Clipboard API 仅在安全上下文（HTTPS / localhost）可用，管理台常经
 * http://局域网IP 访问时 navigator.clipboard 为 undefined，需回退到
 * execCommand（依赖用户点击手势，在点击回调里调用即可生效）。
 */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // 权限被拒或上下文受限，走下方回退
  }
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    ta.style.position = 'fixed'
    ta.style.top = '0'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    if (/ipad|iphone|ipod/i.test(navigator.userAgent)) {
      // iOS Safari 需手动构造选区
      const range = document.createRange()
      range.selectNodeContents(ta)
      const sel = window.getSelection()
      sel?.removeAllRanges()
      sel?.addRange(range)
      ta.setSelectionRange(0, text.length)
    } else {
      ta.select()
    }
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  } catch {
    return false
  }
}

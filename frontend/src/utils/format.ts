/** 数字与时间格式化工具。 */

/** 大数缩写：1234567 -> 1.23M */
export function compactNumber(n: number): string {
  if (!Number.isFinite(n)) return '0'
  const abs = Math.abs(n)
  if (abs >= 1e9) return (n / 1e9).toFixed(2) + 'B'
  if (abs >= 1e6) return (n / 1e6).toFixed(2) + 'M'
  if (abs >= 1e3) return (n / 1e3).toFixed(1) + 'K'
  return String(Math.round(n))
}

/** 中文单位大数：>=1亿 -> X亿；>=1万 -> X万；否则原数。最多保留 2 位有效小数。 */
export function compactCN(n: number): string {
  if (!Number.isFinite(n)) return '0'
  const sign = n < 0 ? '-' : ''
  const abs = Math.abs(n)
  const trim = (x: number) => {
    const s = x.toFixed(2)
    return String(parseFloat(s))
  }
  if (abs >= 1e8) return sign + trim(abs / 1e8) + '亿'
  if (abs >= 1e4) return sign + trim(abs / 1e4) + '万'
  return sign + String(Math.round(abs))
}

/** 百分数：0.234 -> 23.4% */
export function percent(ratio: number, digits = 1): string {
  if (!Number.isFinite(ratio)) return '0%'
  return (ratio * 100).toFixed(digits) + '%'
}

/** 耗时：<1000ms 显示 ms，否则秒。 */
export function duration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

/** 长时长格式化：1h+ 显示 X小时Y分钟，1min+ 显示 X分Y秒，其余沿用 duration()。 */
export function durationLong(ms: number): string {
  if (!Number.isFinite(ms) || ms < 60_000) return duration(ms)
  const totalSec = Math.round(ms / 1000)
  const h = Math.floor(totalSec / 3600)
  const m = Math.floor((totalSec % 3600) / 60)
  const sec = totalSec % 60
  if (h > 0) return `${h}小时${m}分钟`
  return m > 0 ? `${m}分${sec}秒` : `${sec}秒`
}

/** token 速度（吞吐）：tokens/second。tokens 或 durationMs 无效时返回 '—'。 */
export function tokenSpeed(tokens: number, durationMs: number): string {
  if (!Number.isFinite(tokens) || !Number.isFinite(durationMs)) return '—'
  if (tokens <= 0 || durationMs <= 0) return '—'
  return `${compactCN(Math.round((tokens / durationMs) * 1000))} tok/s`
}

/** token 速度数值（tokens/second），供图表/卡片计算用；无效时返回 0。 */
export function tokensPerSec(tokens: number, durationMs: number): number {
  if (!Number.isFinite(tokens) || !Number.isFinite(durationMs)) return 0
  if (tokens <= 0 || durationMs <= 0) return 0
  return (tokens / durationMs) * 1000
}

/** 时间显示：HH:mm:ss */
export function timeOf(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const p = (x: number) => String(x).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

/** 完整时间：MM-DD HH:mm:ss */
export function fullTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const p = (x: number) => String(x).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

/** token 计数（带缓存命中高亮比例计算）。 */
export function hitRateColor(rate: number): string {
  if (rate >= 0.5) return 'var(--accent)'
  if (rate >= 0.2) return 'var(--cyan)'
  if (rate > 0) return 'var(--amber)'
  return 'var(--text-faint)'
}

/** 协议短名（表格/徽标用）。 */
export function protocolShort(p: string): string {
  switch (p) {
    case 'chat_completions':
      return 'chat'
    case 'responses':
      return 'responses'
    case 'messages':
      return 'messages'
    default:
      return p
  }
}

/** 转发模式短名。 */
export function modeShort(m: string): string {
  return m === 'native_passthrough' ? '透传' : m === 'converted' ? '转换' : m
}

/** 浏览器 UA（Mozilla 前缀）的产品识别规则。 */
const BROWSER_PATTERNS: [RegExp, string][] = [
  [/Edg\//, 'Edge'],
  [/OPR\//, 'Opera'],
  [/Chrome\//, 'Chrome'],
  [/Firefox\//, 'Firefox'],
  [/Safari\//, 'Safari'],
]

/** 认得出具体 agent 工具/SDK 时映射成友好名（保留版本号）。 */
const TOOL_PATTERNS: [RegExp, string][] = [
  [/claude-cli|claude-code/i, 'Claude Code'],
  [/codex/i, 'Codex CLI'],
  [/^hermes/i, 'Hermes Agent'],
  [/^deepseek/i, 'DeepSeek Harness'],
  [/gemini-cli/i, 'Gemini CLI'],
  [/\bgoose\b/i, 'Goose'],
  [/\bcline\b/i, 'Cline'],
  [/roo-?code/i, 'Roo Code'],
  [/cursor/i, 'Cursor'],
  [/cherrystudio/i, 'Cherry Studio'],
  [/lobehub|lobe-chat/i, 'LobeChat'],
  [/nextchat/i, 'NextChat'],
  [/chatbox/i, 'ChatBox'],
  [/dify/i, 'Dify'],
  [/openai/i, 'OpenAI SDK'],
  [/anthropic/i, 'Anthropic SDK'],
]

/**
 * 从 User-Agent 提取调用方工具短名，优先「产品名/版本」（如 "ZCode/3.11.2 ai-sdk/..." ->
 * ZCode/3.11.2）。只对认得出的具体工具换友好名；不把 python-httpx、go-http-client 之类
 * 折叠成实现语言。浏览器 UA 单独识别产品。
 */
export function agentShort(ua: string): string {
  if (!ua) return '—'
  const first = ua.trim().split(/[\s(]/)[0] ?? ''
  const slash = first.indexOf('/')
  const name = slash > 0 ? first.slice(0, slash) : first
  if (/^mozilla$/i.test(name)) {
    for (const [re, label] of BROWSER_PATTERNS) {
      if (re.test(ua)) return label
    }
  }
  // 版本号形如 3.11.2 才带上，避免 "OpenAI/NodeJS/5.1" 这类多段产品串一起显示
  const ver = slash > 0 ? first.slice(slash + 1) : ''
  const suffix = /^\d+(\.\d+)*$/.test(ver) ? `/${ver}` : ''
  for (const [re, label] of TOOL_PATTERNS) {
    if (re.test(name)) return `${label}${suffix}`
  }
  return first ? first.slice(0, 32) : '—'
}

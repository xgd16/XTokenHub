import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router'
import { App, Button, Select, Tooltip } from 'antd'
import { CheckOutlined, CopyOutlined, KeyOutlined, WarningOutlined } from '@ant-design/icons'
import { keyApi, maskKey, type APIKey } from '../api/key'
import { copyText } from '../utils/clipboard'
import { Reveal } from '../utils/motion'

/** 挂载后的 MCP server 名（两端一致，便于 memory_add 等工具名统一）。 */
const SERVER_NAME = 'xtokenhub-memory'

/**
 * 两端 CLI 的 --header 语法**不一样**，这是最容易踩的坑：
 *   opencode 收 `name=value`（等号）
 *   claude   收 `Name: Value`（冒号）
 * 写错不会报错，只是鉴权头没带上，表现为「连上了但每个工具都 401 缺少 API Key」。
 */
function buildCommands(baseUrl: string, key: string) {
  const url = `${baseUrl.replace(/\/+$/, '')}/mcp`
  return [
    {
      id: 'opencode',
      label: 'OpenCode',
      note: '写入全局配置，所有项目可用',
      cmd: `opencode mcp add ${SERVER_NAME} --global --url ${url} --header "Authorization=Bearer ${key}"`,
    },
    {
      id: 'claude',
      label: 'Claude Code',
      note: '写入用户级配置，所有项目可用',
      cmd: `claude mcp add --transport http --scope user ${SERVER_NAME} ${url} --header "Authorization: Bearer ${key}"`,
    },
  ]
}

/**
 * 记忆库「快速接入」：展示可直接复制到终端的 MCP 挂载命令。
 *
 * 地址取 `location.origin`——控制台就由这台服务提供，用户用什么地址打开控制台，
 * 粘贴出来的命令就用同一个地址，不用手改 IP。
 */
export default function McpQuickConnect() {
  const { message } = App.useApp()
  const navigate = useNavigate()
  const [keys, setKeys] = useState<APIKey[]>([])
  const [loaded, setLoaded] = useState(false)
  const [picked, setPicked] = useState<number | null>(null)
  const [copied, setCopied] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const data = await keyApi.list({ page: 1, per_page: 100 })
      setKeys(data.items ?? [])
    } catch {
      // 拉不到密钥不阻塞页面：下方会退化成「去创建密钥」提示
    } finally {
      setLoaded(true)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  // 只展示启用的密钥；默认取最近更新的一条（最可能是当前在用的那个）
  const usable = useMemo(
    () => keys.filter((k) => k.status === 1).sort((a, b) => b.updated_at.localeCompare(a.updated_at)),
    [keys],
  )
  const active = useMemo(
    () => usable.find((k) => k.id === picked) ?? usable[0] ?? null,
    [usable, picked],
  )

  const baseUrl = typeof window === 'undefined' ? '' : window.location.origin
  const commands = useMemo(() => (active ? buildCommands(baseUrl, active.key) : []), [baseUrl, active])

  const copy = async (id: string, cmd: string) => {
    const ok = await copyText(cmd)
    if (ok) {
      setCopied(id)
      setTimeout(() => setCopied((c) => (c === id ? null : c)), 1600)
      message.success('命令已复制，粘贴到终端执行即可')
    } else {
      message.error('复制失败，请手动选择文本复制')
    }
  }

  return (
    <section className="obs-section">
      <div className="obs-section-head">
        <span>快速接入</span>
        <span className="obs-count-chip">MCP</span>
      </div>

      <div className="panel" style={{ padding: '14px 16px 16px' }}>
        <Reveal>
          <div className="obs-connect-intro">
            复制下面任意一条命令到终端执行，agent 就能直接读写本记忆库（<code className="obs-code">memory_add</code> /{' '}
            <code className="obs-code">memory_search</code> 等 7 个工具）。
          </div>

          {!loaded ? (
            <div className="obs-connect-skeleton" />
          ) : !active ? (
            <div className="obs-connect-empty">
              <WarningOutlined style={{ color: 'var(--amber)', marginRight: 8 }} />
              <span>还没有启用的 API Key。先创建一个，命令里才能填入鉴权头。</span>
              <Button size="small" icon={<KeyOutlined />} onClick={() => navigate('/keys')}>
                去创建
              </Button>
            </div>
          ) : (
            <>
              {usable.length > 1 && (
                <div className="obs-connect-keypick">
                  <span className="obs-connect-keypick-label">使用密钥</span>
                  <Select
                    size="small"
                    style={{ width: 220 }}
                    value={active.id}
                    onChange={setPicked}
                    options={usable.map((k) => ({
                      value: k.id,
                      label: `${k.name}（${maskKey(k.key)}）`,
                    }))}
                  />
                </div>
              )}

              <div className="obs-connect-grid">
                {commands.map((c) => (
                  <div className="obs-connect-item" key={c.id}>
                    <div className="obs-connect-item-head">
                      <span className="obs-panel-title">{c.label}</span>
                      <span className="obs-connect-item-note">{c.note}</span>
                      <Tooltip title="复制命令">
                        <Button
                          size="small"
                          type="text"
                          className="obs-connect-copy"
                          icon={copied === c.id ? <CheckOutlined style={{ color: 'var(--accent)' }} /> : <CopyOutlined />}
                          onClick={() => void copy(c.id, c.cmd)}
                        >
                          {copied === c.id ? '已复制' : '复制'}
                        </Button>
                      </Tooltip>
                    </div>
                    {/* 命令整块可点复制：省得拖选长串，含密钥的串手选很痛苦 */}
                    <code
                      className="obs-connect-cmd mono"
                      role="button"
                      tabIndex={0}
                      title="点击复制整条命令"
                      onClick={() => void copy(c.id, c.cmd)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter' || e.key === ' ') {
                          e.preventDefault()
                          void copy(c.id, c.cmd)
                        }
                      }}
                    >
                      {c.cmd}
                    </code>
                  </div>
                ))}
              </div>

              <div className="obs-connect-foot">
                两端 --header 语法不同（OpenCode 用 <code className="obs-code">=</code>，Claude Code 用{' '}
                <code className="obs-code">:</code>），命令已按各自格式生成，直接复制即可。
                接入后在 agent 里说「记一下 …」即可写入。
              </div>
            </>
          )}
        </Reveal>
      </div>
    </section>
  )
}
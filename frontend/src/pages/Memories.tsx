import { useCallback, useEffect, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent } from 'react'
import {
  App, AutoComplete, Button, Dropdown, Form, Input, Modal, Progress, Select, Tooltip,
} from 'antd'
import {
  DeleteOutlined, EditOutlined, MergeCellsOutlined, MoreOutlined, PlusOutlined,
  ReloadOutlined, SettingOutlined, ShareAltOutlined, ThunderboltOutlined,
} from '@ant-design/icons'
import { useNavigate } from 'react-router'
import { channelApi, parseCSV, type Channel } from '../api/channel'
import {
  memoryApi, type ClassifyConfig, type ClassifyStatus, type MemoryNamespaceView, type MemoryStats,
} from '../api/memory'
import { AnimatedNumber } from '../components/AnimatedNumber'
import { PageHero } from '../components/PageHero'
import { obsColorStyle } from '../utils/format'
import { NS_ALL, NS_NONE, namespaceColor, nsPath } from '../utils/memoryMeta'
import { Reveal } from '../utils/motion'
import { refreshMemoryNamespaces, useMemoryNamespaces } from '../utils/useMemoryNamespaces'

/** 计数格式化（AnimatedNumber 滚动中间值需取整）。 */
const intText = (n: number) => String(Math.round(n))

/**
 * 记忆库总览（/memories）：命名空间星图 + 分类设置。
 * 每个命名空间以稳定哈希色（与关系图同源）标识身份；点卡片进入 /memories/:ns 看具体记忆。
 */
export default function Memories() {
  const { message, modal } = App.useApp()
  const navigate = useNavigate()
  const namespaces = useMemoryNamespaces()

  const [stats, setStats] = useState<MemoryStats | null>(null)

  // 分类设置 + 任务进度
  const [classifyCfg, setClassifyCfg] = useState<ClassifyConfig | null>(null)
  const [classifySt, setClassifySt] = useState<ClassifyStatus | null>(null)
  const [classifyModal, setClassifyModal] = useState(false)
  const [channels, setChannels] = useState<Channel[]>([])
  const [cfgChannelId, setCfgChannelId] = useState<number | undefined>()
  const [cfgModel, setCfgModel] = useState('')
  const [relinking, setRelinking] = useState(false)

  // 命名空间操作弹窗
  const [creating, setCreating] = useState(false)
  const [newName, setNewName] = useState('')
  const [renameTarget, setRenameTarget] = useState<MemoryNamespaceView | null>(null)
  const [renameTo, setRenameTo] = useState('')
  const [mergeTarget, setMergeTarget] = useState<MemoryNamespaceView | null>(null)
  const [mergeTo, setMergeTo] = useState<string | undefined>()
  const [descTarget, setDescTarget] = useState<MemoryNamespaceView | null>(null)
  const [desc, setDesc] = useState('')

  const loadMeta = useCallback(async () => {
    try {
      const [st, cfg] = await Promise.all([memoryApi.stats(), memoryApi.classifyConfig()])
      setStats(st)
      setClassifyCfg(cfg)
      setClassifySt(cfg)
    } catch (e) {
      message.error((e as Error).message)
    }
  }, [message])

  useEffect(() => {
    void loadMeta()
    void refreshMemoryNamespaces()
  }, [loadMeta])

  // 分类任务进行中则轮询进度；结束后刷新统计与命名空间计数
  useEffect(() => {
    if (classifySt?.status !== 'running') return
    const timer = setInterval(async () => {
      try {
        const st = await memoryApi.classifyStatus()
        setClassifySt(st)
        if (st.status !== 'running') {
          await loadMeta()
          await refreshMemoryNamespaces()
        }
      } catch {
        /* 轮询失败静默，下轮再试 */
      }
    }, 1500)
    return () => clearInterval(timer)
  }, [classifySt?.status, loadMeta])

  const afterNsChanged = async () => {
    await refreshMemoryNamespaces()
    await loadMeta()
  }

  const confirmDeleteNs = (n: MemoryNamespaceView) => {
    modal.confirm({
      title: `删除命名空间「${n.name}」？`,
      content: `其 ${n.count} 条记忆会回到未分类（可再次一键分类）`,
      okText: '删除',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await memoryApi.deleteNamespace(n.id)
          message.success('已删除')
          await afterNsChanged()
        } catch (e) {
          message.error((e as Error).message)
          throw e
        }
      },
    })
  }

  const openClassifyModal = async () => {
    try {
      const res = await channelApi.list({ page: 1, per_page: 200 })
      setChannels(res.items)
      setCfgChannelId(classifyCfg?.channel_id || undefined)
      setCfgModel(classifyCfg?.model || '')
      setClassifyModal(true)
    } catch (e) {
      message.error((e as Error).message)
    }
  }

  const saveClassifyConfig = async () => {
    try {
      await memoryApi.updateClassifyConfig({ channel_id: cfgChannelId ?? 0, model: cfgModel })
      message.success('分类设置已保存')
      setClassifyModal(false)
      await loadMeta()
    } catch (e) {
      message.error((e as Error).message)
    }
  }

  const triggerClassify = async () => {
    try {
      const st = await memoryApi.triggerClassify()
      setClassifySt(st)
      message.success('分类任务已启动')
    } catch (e) {
      message.error((e as Error).message)
    }
  }

  // 全库重算自动关系（只动 system 自动边，手动关系不受影响）
  const rebuildLinks = async () => {
    setRelinking(true)
    try {
      const res = await memoryApi.rebuildAutoLinks()
      message.success(`重算完成：处理 ${res.processed} 条记忆，维护自动关系 ${res.created} 条`)
      void loadMeta()
    } catch (e) {
      message.error((e as Error).message)
    } finally {
      setRelinking(false)
    }
  }

  const classifyRunning = classifySt?.status === 'running'
  const classifyConfigured = !!classifyCfg?.model
  const showFoot = classifyRunning || !!classifySt?.message || !classifyConfigured

  const uncategorized = stats?.uncategorized ?? 0

  /** 卡片键盘可达：仅当事件源为卡片本身时响应（避免吞掉内部按钮/菜单的回车）。 */
  const enterOnKey = (fn: () => void) => (e: ReactKeyboardEvent) => {
    if (e.target !== e.currentTarget) return
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      fn()
    }
  }

  return (
    <div className="obs-page">
      {/* —— 观测台：读数 + 全局操作 —— */}
      <PageHero
        eyebrow="memory bank · 记忆星图"
        title="记忆库"
        backdrop={<ConstellationBackdrop />}
        readouts={[
          { label: '记忆总量', value: stats ? <AnimatedNumber value={stats.total} format={intText} /> : '—', tone: 'accent' },
          { label: '生效', value: stats ? stats.active : '—' },
          { label: '未分类', value: stats ? stats.uncategorized : '—', tone: uncategorized > 0 ? 'warn' : undefined },
          { label: '关系边', value: stats ? stats.links : '—' },
        ]}
        actions={
          <>
            <Button icon={<PlusOutlined />} onClick={() => { setCreating(true); setNewName('') }}>新建命名空间</Button>
            <Button icon={<SettingOutlined />} onClick={openClassifyModal}>分类设置</Button>
            <Tooltip title="按内容相似度重算全库自动关系（只动系统自动边，手动关系不受影响）">
              <Button icon={<ShareAltOutlined />} loading={relinking} onClick={rebuildLinks}>重算关系</Button>
            </Tooltip>
            <Tooltip title={classifyCfg?.model ? `模型：${classifyCfg.model}` : '尚未配置分类模型'}>
              <Button
                type="primary"
                icon={<ThunderboltOutlined />}
                loading={classifyRunning}
                onClick={triggerClassify}
              >
                {classifyRunning ? `分类中 ${classifySt?.done ?? 0}/${classifySt?.total ?? 0}` : '一键分类未分类'}
              </Button>
            </Tooltip>
            <Button
              icon={<ReloadOutlined />}
              aria-label="刷新"
              onClick={() => { void loadMeta(); void refreshMemoryNamespaces() }}
            />
          </>
        }
        foot={showFoot ? (
          <>
            {classifyRunning ? (
              <>
                <span className="live-dot" aria-hidden="true" />
                <span>正在分类未分类记忆 {classifySt?.done ?? 0}/{classifySt?.total ?? 0}</span>
                <Progress
                  percent={classifySt && classifySt.total > 0 ? Math.round((classifySt.done / classifySt.total) * 100) : 100}
                  showInfo={false}
                  size="small"
                />
              </>
            ) : !classifyConfigured ? (
              <>
                <span className="obs-flag warn" aria-hidden="true" />
                <span>未配置分类模型：一键分类前先在「分类设置」中选择渠道与模型</span>
                <Button type="link" size="small" style={{ padding: 0, height: 'auto' }} onClick={openClassifyModal}>
                  去配置
                </Button>
              </>
            ) : (
              <>
                <span className="obs-flag" aria-hidden="true" />
                <span>上次分类：{classifySt?.message}</span>
              </>
            )}
          </>
        ) : undefined}
      />

      {/* —— 星域：命名空间卡片 —— */}
      <section className="obs-section">
        <div className="obs-section-head">
          <span>命名空间</span>
          <span className="obs-count-chip">{namespaces.length}</span>
        </div>

        <div className="obs-grid">
          <Reveal className="obs-cell" delay={0}>
            <div
              className="obs-card clickable"
              role="button"
              tabIndex={0}
              aria-label={`进入全部记忆（${stats?.total ?? 0} 条）`}
              style={obsColorStyle('var(--accent)')}
              onClick={() => navigate(nsPath(NS_ALL))}
              onKeyDown={enterOnKey(() => navigate(nsPath(NS_ALL)))}
            >
              <span className="obs-card-dots" aria-hidden="true" />
              <span className="obs-card-glow" aria-hidden="true" />
              <div className="obs-card-top">
                <span className="obs-card-dot" aria-hidden="true" />
                <span className="obs-card-name">全部记忆</span>
              </div>
              <div className="obs-card-num">
                <AnimatedNumber value={stats?.total ?? 0} format={intText} /><small>条</small>
              </div>
              <div className="obs-card-desc">跨命名空间浏览与全局搜索</div>
              <div className="obs-card-foot">
                <span className="obs-came-from">all spaces</span>
                <span className="obs-enter">进入 →</span>
              </div>
            </div>
          </Reveal>

          <Reveal className="obs-cell" delay={0.05}>
            <div
              className="obs-card dashed clickable"
              role="button"
              tabIndex={0}
              aria-label={`进入未分类（${uncategorized} 条）`}
              style={obsColorStyle(uncategorized > 0 ? 'var(--amber)' : 'var(--text-faint)')}
              onClick={() => navigate(nsPath(NS_NONE))}
              onKeyDown={enterOnKey(() => navigate(nsPath(NS_NONE)))}
            >
              <span className="obs-card-glow" aria-hidden="true" />
              <div className="obs-card-top">
                <span className="obs-card-dot" aria-hidden="true" />
                <span className="obs-card-name">未分类</span>
              </div>
              <div className="obs-card-num">
                <AnimatedNumber value={uncategorized} format={intText} /><small>条</small>
              </div>
              <div className="obs-card-desc">等待归类（一键分类或手动移动）</div>
              <div className="obs-card-foot">
                <span className="obs-came-from">inbox</span>
                <span className="obs-enter">进入 →</span>
              </div>
            </div>
          </Reveal>

          {namespaces.map((n, i) => (
            <Reveal key={n.id} className="obs-cell" delay={Math.min(0.1 + i * 0.05, 0.55)}>
              <div
                className="obs-card clickable"
                role="button"
                tabIndex={0}
                aria-label={`进入命名空间 ${n.name}（${n.count} 条记忆）`}
                style={obsColorStyle(namespaceColor(n.name))}
                onClick={() => navigate(nsPath(n.name))}
                onKeyDown={enterOnKey(() => navigate(nsPath(n.name)))}
              >
                <span className="obs-card-glow" aria-hidden="true" />
                <div className="obs-card-top">
                  <span className="obs-card-dot" aria-hidden="true" />
                  <span className="obs-card-name">{n.name}</span>
                  <span className="obs-card-menu">
                    <Dropdown
                      trigger={['click']}
                      menu={{
                        items: [
                          { key: 'desc', icon: <EditOutlined />, label: '编辑描述' },
                          { key: 'rename', icon: <EditOutlined />, label: '改名' },
                          { key: 'merge', icon: <MergeCellsOutlined />, label: '合并到…' },
                          { type: 'divider' },
                          { key: 'delete', icon: <DeleteOutlined />, label: '删除', danger: true },
                        ],
                        onClick: ({ key, domEvent }) => {
                          domEvent.stopPropagation()
                          if (key === 'desc') { setDescTarget(n); setDesc(n.description) }
                          if (key === 'rename') { setRenameTarget(n); setRenameTo(n.name) }
                          if (key === 'merge') { setMergeTarget(n); setMergeTo(undefined) }
                          if (key === 'delete') confirmDeleteNs(n)
                        },
                      }}
                    >
                      <Button
                        type="text"
                        size="small"
                        icon={<MoreOutlined />}
                        aria-label={`管理命名空间 ${n.name}`}
                        onClick={(e) => e.stopPropagation()}
                      />
                    </Dropdown>
                  </span>
                </div>
                <div className="obs-card-num">{n.count}<small>条</small></div>
                <div className={`obs-card-desc${n.description ? '' : ' placeholder'}`}>
                  {n.description || '未填写描述，可在 ⋯ 中补充'}
                </div>
                <div className="obs-card-foot">
                  <span className="obs-came-from">{n.created_by || 'manual'}</span>
                  <span className="obs-enter">进入 →</span>
                </div>
              </div>
            </Reveal>
          ))}
        </div>

        {namespaces.length === 0 && (
          <div className="obs-empty">
            <div className="obs-empty-title">还没有命名空间</div>
            <div className="obs-empty-hint">
              写入记忆时用 <code className="obs-code">namespace</code> 指定归属会自动创建；
              agent 侧也可以直接调 <code className="obs-code">memory_add</code>（MCP）。
            </div>
          </div>
        )}
      </section>

      <Modal
        title="新建命名空间"
        open={creating}
        onCancel={() => setCreating(false)}
        onOk={async () => {
          try {
            await memoryApi.createNamespace({ name: newName })
            message.success('已创建')
            setCreating(false)
            await afterNsChanged()
          } catch (e) {
            message.error((e as Error).message)
          }
        }}
      >
        <Input placeholder="命名空间名（2~8 字主题词为佳）" value={newName} onChange={(e) => setNewName(e.target.value)} />
      </Modal>

      <Modal
        title={`改名「${renameTarget?.name}」`}
        open={!!renameTarget}
        onCancel={() => setRenameTarget(null)}
        onOk={async () => {
          try {
            const res = await memoryApi.renameNamespace(renameTarget!.name, renameTo)
            message.success(`已改名，迁移 ${res.moved} 条记忆`)
            setRenameTarget(null)
            await afterNsChanged()
          } catch (e) {
            message.error((e as Error).message)
          }
        }}
      >
        <Input value={renameTo} onChange={(e) => setRenameTo(e.target.value)} placeholder="新名称" />
      </Modal>

      <Modal
        title={`合并「${mergeTarget?.name}」到…`}
        open={!!mergeTarget}
        onCancel={() => setMergeTarget(null)}
        onOk={async () => {
          if (!mergeTo) {
            message.warning('请选择目标命名空间')
            return
          }
          try {
            const res = await memoryApi.mergeNamespaces(mergeTarget!.name, mergeTo)
            message.success(`已合并 ${res.moved} 条记忆`)
            setMergeTarget(null)
            await afterNsChanged()
          } catch (e) {
            message.error((e as Error).message)
          }
        }}
      >
        <Select
          style={{ width: '100%' }}
          placeholder="目标命名空间"
          value={mergeTo}
          onChange={setMergeTo}
          options={namespaces.filter((n) => n.id !== mergeTarget?.id).map((n) => ({ value: n.name, label: n.name }))}
        />
      </Modal>

      <Modal
        title={`描述「${descTarget?.name}」`}
        open={!!descTarget}
        onCancel={() => setDescTarget(null)}
        onOk={async () => {
          try {
            await memoryApi.updateNamespace(descTarget!.id, { description: desc })
            message.success('已保存')
            setDescTarget(null)
            await afterNsChanged()
          } catch (e) {
            message.error((e as Error).message)
          }
        }}
      >
        <Input.TextArea rows={3} value={desc} onChange={(e) => setDesc(e.target.value)} placeholder="一句话说明这个命名空间装什么（供 LLM 分类参考）" />
      </Modal>

      <Modal
        title="分类设置（一键 LLM 分类用）"
        open={classifyModal}
        onCancel={() => setClassifyModal(false)}
        onOk={saveClassifyConfig}
        okText="保存"
      >
        <Form layout="vertical">
          <Form.Item label="渠道" required>
            <Select
              allowClear
              placeholder="选择调用模型的渠道"
              value={cfgChannelId}
              onChange={(v) => {
                setCfgChannelId(v)
                const ch = channels.find((c) => c.id === v)
                const models = ch ? parseCSV(ch.models) : []
                if (models.length > 0) setCfgModel(models[0])
                else setCfgModel('')
              }}
              options={channels.map((c) => ({ value: c.id, label: `${c.name}${c.status === 0 ? '（已停用）' : ''}` }))}
            />
          </Form.Item>
          <Form.Item label="模型" required>
            <AutoComplete
              placeholder="分类模型名（可手输）"
              value={cfgModel}
              onChange={(v) => setCfgModel(v)}
              options={(cfgChannelId ? parseCSV(channels.find((c) => c.id === cfgChannelId)?.models ?? '') : []).map((m) => ({ value: m }))}
            />
          </Form.Item>
          <div style={{ fontSize: 12, opacity: 0.7 }}>
            触发后对「未分类」记忆批量归类到命名空间（可新建），只改归属不动正文；
            只支持 chat_completions / messages 协议的渠道。
          </div>
        </Form>
      </Modal>
    </div>
  )
}

/** 星座底纹：右侧星点连线，低透明度装饰（呼应记忆关系图）。 */
function ConstellationBackdrop() {
  return (
    <svg className="obs-hero-backdrop" viewBox="0 0 560 180" preserveAspectRatio="xMaxYMid slice" aria-hidden="true">
      <g stroke="var(--accent)" strokeWidth="1" strokeOpacity="0.35" fill="none">
        <path d="M96 132 L188 74 L268 118 L352 58 L436 96" />
        <path d="M188 74 L226 30 M352 58 L322 122 L268 118 M436 96 L500 52" />
      </g>
      <g fill="var(--accent)">
        <circle cx="96" cy="132" r="2.4" />
        <circle cx="188" cy="74" r="3" />
        <circle cx="268" cy="118" r="2.2" />
        <circle className="obs-node-pulse" cx="352" cy="58" r="3.4" />
        <circle cx="436" cy="96" r="2.4" />
      </g>
      <g fill="var(--cyan)">
        <circle cx="226" cy="30" r="2" />
        <circle cx="322" cy="122" r="2.2" />
        <circle className="obs-node-pulse" cx="500" cy="52" r="2.8" />
      </g>
      <circle cx="352" cy="58" r="7.5" fill="none" stroke="var(--accent)" strokeOpacity="0.4" />
    </svg>
  )
}

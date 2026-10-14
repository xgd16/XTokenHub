import { Suspense, useCallback, useEffect, useMemo, useState } from 'react'
import type { CSSProperties } from 'react'
import { Drawer, Layout, Spin } from 'antd'
import { MenuOutlined } from '@ant-design/icons'
import { useLocation, useNavigate, useOutlet } from 'react-router'
import { AnimatePresence, motion } from 'motion/react'
import { useWsStatus } from '../api/ws'
import { useIsMobile } from '../utils/useIsMobile'
import { easeOutExpo, useEntranceOffset } from '../utils/motion'
import NavRail from '../components/NavRail'
import HubMark from '../components/HubMark'
import TopTicker from '../components/TopTicker'
import { PlacePlate, TopActions } from '../components/TopBar'
import { QuickJumpHost } from '../components/QuickJump'
import { buildNav, placeFor } from './nav'
import { refreshMemoryNamespaces, useMemoryNamespaces } from '../utils/useMemoryNamespaces'

const { Sider, Header, Content } = Layout

const SIDER_WIDTH = 236
const SIDER_COLLAPSED_WIDTH = 62

const WS_LABEL = {
  open: '实时推送已连接',
  connecting: '正在连接实时推送…',
  closed: '实时推送已断开，自动重连中',
} as const

/** 背景极光：缓慢漂移的柔和色块，营造深邃空间感（pointer-events:none，不干扰交互）。 */
function AuroraBg() {
  return (
    <div className="bg-aurora" aria-hidden="true">
      <div className="aurora-blob blob-1" />
      <div className="aurora-blob blob-2" />
    </div>
  )
}

/** 页面切换：路由变化时旧页淡出、新页淡入（keyed by pathname）。
 *  移动端不做位移（见 useEntranceOffset）：窄屏位移会被读成抖动，
 *  且仪表盘自身还有一层入场动画，叠加会加倍。
 *  Suspense 放在内容区内部：懒加载页面下载期间侧栏/顶栏保持可见，只让内容区显示兜底。 */
function PageTransition() {
  const location = useLocation()
  const outlet = useOutlet()
  const offset = useEntranceOffset(10)
  return (
    <AnimatePresence mode="wait">
      <motion.div
        key={location.pathname}
        initial={{ opacity: 0, y: offset }}
        animate={{ opacity: 1, y: 0 }}
        exit={{ opacity: 0, y: 0 }}
        transition={{ duration: offset === 0 ? 0.26 : 0.3, ease: easeOutExpo }}
      >
        <Suspense
          fallback={
            <div style={{ display: 'flex', justifyContent: 'center', padding: '80px 0' }}>
              <Spin />
            </div>
          }
        >
          {outlet}
        </Suspense>
      </motion.div>
    </AnimatePresence>
  )
}

/** 侧栏容器：折叠时宽度收窄，位号/标题逐项淡出。 */
function RailShell({
  collapsed,
  style,
  children,
}: {
  collapsed: boolean
  style?: CSSProperties
  children: React.ReactNode
}) {
  return (
    <div
      className={`rail-shell${collapsed ? ' is-collapsed' : ''}`}
      style={{ width: collapsed ? SIDER_COLLAPSED_WIDTH : SIDER_WIDTH, ...style }}
    >
      {children}
    </div>
  )
}

/** 页面级顶栏：左侧位号铭牌，右侧动作区；滚动时保持吸顶。 */
function TopBar({
  code,
  title,
  trail,
  onOpenJump,
}: {
  code: string
  title: string
  trail: string[]
  onOpenJump: () => void
}) {
  const status = useWsStatus()
  return (
    <Header className="bar">
      <PlacePlate code={code} title={title} trail={trail} />
      <TopTicker />
      <TopActions onOpenJump={onOpenJump} status={status} statusLabel={WS_LABEL[status]} />
    </Header>
  )
}

/** 控制台布局：桌面为 导航轨+顶栏+内容区；移动端为 顶栏(汉堡)+内容区+抽屉导航。 */
export default function ConsoleLayout() {
  const navigate = useNavigate()
  const location = useLocation()
  const [collapsed, setCollapsed] = useState(false)
  const [navOpen, setNavOpen] = useState(false)
  const [jumpOpen, setJumpOpen] = useState(false)
  const isMobile = useIsMobile()
  const status = useWsStatus()

  const namespaces = useMemoryNamespaces()
  const groups = useMemo(() => buildNav(namespaces), [namespaces])
  const place = useMemo(() => placeFor(location.pathname, groups), [location.pathname, groups])

  // 每次导航刷新一次命名空间清单（其他 agent / MCP 侧写入后，侧边栏计数不至长期陈旧）
  useEffect(() => {
    void refreshMemoryNamespaces()
  }, [location.pathname])

  // ⌘K / Ctrl K 唤起快速跳转
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setJumpOpen(true)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const go = useCallback(
    (path: string) => {
      setJumpOpen(false)
      setNavOpen(false)
      if (path !== location.pathname) navigate(path)
    },
    [navigate, location.pathname],
  )

  if (isMobile) {
    return (
      <div className="console-root">
        <AuroraBg />
        <Header className="bar bar-mobile">
          <button
            type="button"
            className="bar-icon"
            onClick={() => setNavOpen(true)}
            aria-label="打开导航菜单"
          >
            <MenuOutlined />
          </button>
          <button type="button" className="bar-mobile-brand" onClick={() => go('/')} aria-label="回到仪表盘">
            <HubMark size={18} />
            <span>XTokenHub</span>
          </button>
          <TopActions
            onOpenJump={() => setJumpOpen(true)}
            status={status}
            statusLabel={WS_LABEL[status]}
            compact
          />
        </Header>

        <Content className="console-content console-content-mobile">
          <PageTransition />
        </Content>

        <Drawer
          placement="left"
          open={navOpen}
          onClose={() => setNavOpen(false)}
          closable={false}
          size={SIDER_WIDTH}
          styles={{
            body: { padding: 0, background: 'var(--bg-base)' },
            wrapper: { background: 'var(--bg-base)' },
          }}
        >
          <NavRail
            groups={groups}
            pathname={location.pathname}
            collapsed={false}
            onNavigate={go}
            onToggleCollapse={() => setNavOpen(false)}
            toggleLabel="关闭导航"
          />
        </Drawer>

        <QuickJumpHost open={jumpOpen} groups={groups} onClose={() => setJumpOpen(false)} onNavigate={go} />
      </div>
    )
  }

  return (
    <Layout className="console-root">
      <AuroraBg />
      <Sider
        width={SIDER_WIDTH}
        collapsedWidth={SIDER_COLLAPSED_WIDTH}
        collapsible
        collapsed={collapsed}
        trigger={null}
        onCollapse={setCollapsed}
        className="console-sider"
        style={{ background: 'transparent', height: '100vh', overflow: 'hidden' }}
      >
        <RailShell collapsed={collapsed}>
          <NavRail
            groups={groups}
            pathname={location.pathname}
            collapsed={collapsed}
            onNavigate={go}
            onToggleCollapse={() => setCollapsed((v) => !v)}
          />
        </RailShell>
      </Sider>

      <Layout style={{ background: 'transparent', height: '100vh', overflowY: 'auto' }}>
        <TopBar
          code={place.code}
          title={place.title}
          trail={place.trail}
          onOpenJump={() => setJumpOpen(true)}
        />
        <Content className="console-content">
          <PageTransition />
        </Content>
      </Layout>

      <QuickJumpHost open={jumpOpen} groups={groups} onClose={() => setJumpOpen(false)} onNavigate={go} />
    </Layout>
  )
}
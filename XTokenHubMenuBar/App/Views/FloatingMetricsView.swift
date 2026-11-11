import AppKit
import SwiftUI

/// 置顶悬浮窗:把一部分指标从菜单栏分流出来,给刘海屏腾出顶部宽度。
///
/// 两种形态共用浮窗指标与窗口位置:
/// - `.card` 两列迷你统计卡(默认);
/// - `.bar` 单行条状,高度约 32pt,尽量少占竖向空间。
///
/// 窗口本身透明、后面没有第二层系统玻璃,所以两种形态都用 `.regular` 档位保证壁纸上的可读性。
/// 单击切换完整面板(展开/收起),拖动移动位置;右键菜单提供样式切换与设置等操作。
struct FloatingMetricsView: View {
    @Environment(HubStore.self) private var store
    @Environment(AppSettings.self) private var settings

    /// 单击卡片/条状/右上角按钮:打开或收起完整面板。
    let onToggleDashboard: () -> Void
    /// 右键菜单「打开完整面板」:总是打开/置前(不因已打开而收起)。
    let onOpenDashboard: () -> Void
    /// 条状内容宽度随数据变化(吞吐/花费位数)时,请控制器重新量窗口尺寸。
    let onNeedsRefit: () -> Void

    var body: some View {
        if settings.floatingMetrics.contains(.peakStatus) {
            // 峰谷状态随时间翻转,但 Observation 不会因时间流逝刷新:30s 局部重算一次。
            TimelineView(.periodic(from: .now, by: 30)) { context in
                content(now: context.date)
            }
        } else {
            content(now: Date())
        }
    }

    @ViewBuilder
    private func content(now: Date) -> some View {
        let snapshot = SurfaceMetricSnapshot(store: store, settings: settings, now: now)
        switch settings.floatingStyle {
        case .card:
            FloatingCardContent(
                snapshot: snapshot,
                items: SurfaceMetrics.items(snapshot, metrics: settings.floatingMetrics),
                style: settings.floatingStyle,
                onToggleDashboard: onToggleDashboard,
                onOpenDashboard: onOpenDashboard,
                onToggleStyle: toggleStyle,
                onOpenSettings: { AppSettingsWindow.open() },
                onResetPosition: { settings.onResetFloatingPosition?() },
                onHide: { settings.setFloatingWindowVisible(false) }
            )
        case .bar:
            let parts = SurfaceMetrics.parts(snapshot, metrics: settings.floatingMetrics)
            FloatingBarContent(
                snapshot: snapshot,
                parts: parts,
                style: settings.floatingStyle,
                onToggleDashboard: onToggleDashboard,
                onOpenDashboard: onOpenDashboard,
                onToggleStyle: toggleStyle,
                onOpenSettings: { AppSettingsWindow.open() },
                onResetPosition: { settings.onResetFloatingPosition?() },
                onHide: { settings.setFloatingWindowVisible(false) }
            )
            // 条状窗口宽度由内容决定:数字位数一变(吞吐 9→1234、花费 $1.2→$12.345、
            // 峰谷/活跃流标签出现消失)就要重新量窗口,否则更长的末位指标会被裁掉。
            .onChange(of: parts) { _, _ in onNeedsRefit() }
            .onChange(of: snapshot.activeStreams) { _, _ in onNeedsRefit() }
        }
    }

    /// 右键菜单:在统计卡与条状之间切换(设置页也有同一个选项)。
    private func toggleStyle() {
        let next: AppSettings.FloatingStyle = settings.floatingStyle == .card ? .bar : .card
        // 菜单还在跟踪时整棵视图树会被替换,旧菜单的锚点跟着消失,macOS 上会留下一个
        // 内容不再重绘的空窗口。等菜单收起后的下一轮 runloop 再切换。
        DispatchQueue.main.async {
            settings.floatingStyle = next
        }
    }
}

/// 统计卡纯内容视图:只依赖快照与闭包,不碰 Store/UserDefaults,
/// 便于离屏 `fittingSize` 验证两列排版预算。
struct FloatingCardContent: View {
    let snapshot: SurfaceMetricSnapshot
    let items: [StatItem]
    let style: AppSettings.FloatingStyle
    let onToggleDashboard: () -> Void
    let onOpenDashboard: () -> Void
    let onToggleStyle: () -> Void
    let onOpenSettings: () -> Void
    let onResetPosition: () -> Void
    let onHide: () -> Void

    /// 固定宽度:数字位数变化只会让内容缩放,不会让窗口左右跳。
    static let contentWidth: CGFloat = 252
    /// 玻璃外的透明留白,给镜面边缘一点呼吸空间(窗口本身无色)。
    static let outerPadding: CGFloat = 6

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            header
            if items.isEmpty {
                Text("在设置中选择浮窗指标")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 14)
            } else {
                StatGrid(cells: items, columns: 2, variant: .regular, appliesSurface: false)
            }
        }
        .padding(12)
        .frame(width: Self.contentWidth)
        .panelSurface(cornerRadius: 22, variant: .regular)
        .padding(Self.outerPadding)
        // `NSHostingView` 不会把鼠标事件交给 `isMovableByWindowBackground`,
        // 必须用 SwiftUI 的窗口拖拽手势显式接管;单击(无位移)则切换完整面板。
        .gesture(WindowDragGesture())
        .onTapGesture { onToggleDashboard() }
        .contextMenu {
            Button("打开完整面板", systemImage: "rectangle.expand.vertical") { onOpenDashboard() }
            Button(style.switchActionTitle, systemImage: style.switchActionSymbol) { onToggleStyle() }
            Button("设置…", systemImage: "gearshape") { onOpenSettings() }
            Divider()
            Button("恢复浮窗默认位置", systemImage: "arrow.uturn.backward") { onResetPosition() }
            Button("隐藏悬浮窗", systemImage: "eye.slash") { onHide() }
        }
    }

    private var header: some View {
        HStack(spacing: 6) {
            HStack(spacing: 6) {
                Image(systemName: "line.3.horizontal")
                    .font(.system(size: 8, weight: .bold))
                    .foregroundStyle(.tertiary)
                Circle()
                    .fill(snapshot.isOnline ? Color.green : Color.red)
                    .frame(width: 7, height: 7)
                Text(snapshot.sourceName.isEmpty ? "XTokenHub" : snapshot.sourceName)
                    .font(.caption2.weight(.medium))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                if snapshot.activeStreams > 0 {
                    Label("\(snapshot.activeStreams)", systemImage: "waveform")
                        .font(.caption2)
                        .foregroundStyle(.orange)
                }
                Spacer(minLength: 0)
            }
            // 确定可用的拖动手柄:NSHostingView 默认不把鼠标事件交给窗口背景拖拽。
            // 拖动结束若位置没变,按单击处理(切换完整面板)。
            .padding(.vertical, 2)
            .overlay {
                WindowDragHandle(onTap: onToggleDashboard)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }

            Button(action: onToggleDashboard) {
                Image(systemName: "rectangle.expand.vertical")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .help("打开 / 收起完整面板")
        }
    }
}

/// 标题栏/条状内容区拖拽条:直接调用 `NSWindow.performDrag`,不依赖 SwiftUI 的窗口背景拖拽。
struct WindowDragHandle: NSViewRepresentable {
    let onTap: () -> Void

    func makeNSView(context: Context) -> NSView {
        let view = DragView()
        view.onTap = onTap
        return view
    }

    func updateNSView(_ nsView: NSView, context: Context) {
        (nsView as? DragView)?.onTap = onTap
    }

    private final class DragView: NSView {
        var onTap: (() -> Void)?

        /// 非激活态下的第一下点击也要能拖动/切换面板(菜单栏 App 几乎总是非激活态)。
        override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }

        override func mouseDown(with event: NSEvent) {
            let before = window?.frame.origin
            window?.performDrag(with: event)
            // performDrag 在拖动结束后返回:位置没变就是一次单击。
            guard let before, let after = window?.frame.origin else { return }
            if hypot(after.x - before.x, after.y - before.y) < 1 {
                onTap?()
            }
        }

        override var mouseDownCanMoveWindow: Bool { true }
    }
}

import AppKit
import SwiftUI

/// 悬浮卡片的窗口管理:置顶、拖拽、位置记忆,以及菜单栏项隐藏后的「完整面板」窗口。
///
/// 卡片是 `NSPanel` 而不是 SwiftUI `Window` 场景:需要无标题栏、不抢焦点、
/// 所有桌面/全屏空间可见,并且能被设置开关直接 `orderFront/orderOut`。
@MainActor
final class FloatingPanelController: NSObject, NSWindowDelegate {
    private let store: HubStore
    private let settings: AppSettings

    private var panel: FloatingPanel!
    private var hostingView: NSView!
    private var dashboardPanel: NSPanel?
    /// 上一次「完整面板因失焦自动收起」的时刻(见 toggleDashboard)。
    private var lastAutoHideAt: Date?
    private var screenObserver: NSObjectProtocol?
    /// 上一次量尺寸时的形态;切换形态后 SwiftUI 的布局要跨轮次才落地,需要多量一次。
    private var lastFittedStyle: AppSettings.FloatingStyle?
    /// 条状内容宽度变化后的重新量尺寸任务(80ms 去抖,吞吐 2Hz 更新时不至于每帧都量)。
    private var refitWorkItem: DispatchWorkItem?

    /// AppKit 的窗口位置记忆键(实际 defaults 键为 `NSWindow Frame <name>`)。
    static let frameAutosaveName = "XTokenHubFloatingWindow"

    init(store: HubStore, settings: AppSettings) {
        self.store = store
        self.settings = settings
        super.init()

        buildPanel()
        settings.onFloatingChange = { [weak self] in self?.applySettings() }
        settings.onResetFloatingPosition = { [weak self] in self?.resetPosition() }
        // 换显示器/改分辨率后,浮窗可能落在新屏幕布局之外 —— 重新量尺寸并夹回可见区。
        screenObserver = NotificationCenter.default.addObserver(
            forName: NSApplication.didChangeScreenParametersNotification,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            Task { @MainActor in
                guard let self, self.settings.showFloatingWindow else { return }
                self.resizeToFit()
                self.clampToVisibleScreen()
                self.panel.orderFrontRegardless()
            }
        }
        applySettings()
    }

    // MARK: - 构建与布局

    private func buildPanel() {
        let root = FloatingMetricsView(
            onToggleDashboard: { [weak self] in self?.toggleDashboard() },
            onOpenDashboard: { [weak self] in self?.showDashboard() },
            onNeedsRefit: { [weak self] in self?.scheduleRefit() }
        )
        .environment(store)
        .environment(settings)
        let hosting = FirstMouseHostingView(rootView: root)
        hosting.frame = NSRect(x: 0, y: 0, width: 264, height: 200)
        hosting.wantsLayer = true
        hosting.layer?.backgroundColor = NSColor.clear.cgColor
        hostingView = hosting

        let panel = FloatingPanel(
            contentRect: hosting.frame,
            styleMask: [.nonactivatingPanel, .borderless, .fullSizeContentView],
            backing: .buffered,
            defer: false
        )
        panel.contentView = hosting
        panel.isFloatingPanel = true
        // 比完整面板(.floating)高一级:面板展开时卡片仍可见、可点,点一下就收起面板。
        panel.level = NSWindow.Level(rawValue: NSWindow.Level.floating.rawValue + 1)
        // 所有桌面与全屏空间都可见;Mission Control 里不随窗口移动;Cmd-` 不参与循环。
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .stationary, .ignoresCycle]
        panel.hidesOnDeactivate = false
        panel.isOpaque = false
        panel.backgroundColor = .clear
        // 透明无边框窗口的系统阴影会按**窗口矩形**画出一圈黑框(不是玻璃轮廓),必须关掉。
        panel.hasShadow = false
        panel.isMovableByWindowBackground = true
        panel.isReleasedWhenClosed = false
        panel.becomesKeyOnlyIfNeeded = false
        panel.animationBehavior = .utilityWindow
        self.panel = panel

        resizeToFit()
        restorePosition()
    }

    /// 指标增减/形态切换时重算窗口尺寸,保持左上角不动(浮窗通常贴在屏幕上方)。
    private func resizeToFit() {
        let size = fittingSize()
        panel.setFrame(
            FloatingPlacement.resizedFramePreservingTopLeft(panel.frame, size: size),
            display: true
        )
        // contentView 是手动 setFrame 过的 NSHostingView:量尺寸阶段会临时放宽到探测宽度,
        // 必须显式同步回窗口内容尺寸。否则 SwiftUI 把固定宽度的卡片居中到探测宽度里,
        // 窗口只截到左侧空白 —— 表现就是「窗口还在,但什么都显示不出来」。
        hostingView.frame = NSRect(
            origin: .zero,
            size: panel.contentRect(forFrameRect: panel.frame).size
        )
        hostingView.needsLayout = true
        hostingView.needsDisplay = true
    }

    private func fittingSize() -> NSSize {
        let size = FloatingSizing.fittingSize(of: hostingView)
        if size.width >= 1, size.height >= 1 {
            // 上限兜底:异常布局值不应把窗口撑到屏幕外;条状高度约 32pt,下限放到 24。
            return NSSize(
                width: min(max(size.width, 80), 560),
                height: min(max(size.height, 24), 600)
            )
        }
        return settings.floatingStyle == .bar
            ? NSSize(width: 260, height: 40)
            : NSSize(
                width: FloatingCardContent.contentWidth + FloatingCardContent.outerPadding * 2,
                height: 120
            )
    }

    /// 首次显示放在主屏右上角、菜单栏下方(自然避开刘海);之后沿用用户拖到的位置。
    private func restorePosition() {
        let saved = UserDefaults.standard.string(
            forKey: "NSWindow Frame \(Self.frameAutosaveName)"
        ) != nil
        panel.setFrameAutosaveName(Self.frameAutosaveName)
        if saved {
            clampToVisibleScreen()
        } else {
            placeDefault()
        }
    }

    private func placeDefault() {
        guard let screen = NSScreen.screens.first ?? NSScreen.main else { return }
        let size = panel.frame.size
        let origin = FloatingPlacement.defaultOrigin(visibleFrame: screen.visibleFrame, size: size)
        panel.setFrame(NSRect(origin: origin, size: size), display: true)
    }

    /// 换显示器/缩放后,已保存的位置可能落在屏幕外。
    private func clampToVisibleScreen() {
        let frames = NSScreen.screens.map(\.visibleFrame)
        panel.setFrame(
            FloatingPlacement.clampedFrame(panel.frame, visibleFrames: frames),
            display: true
        )
    }

    // MARK: - 显隐

    private func applySettings() {
        if settings.showFloatingWindow {
            let styleChanged = lastFittedStyle != settings.floatingStyle
            lastFittedStyle = settings.floatingStyle
            resizeToFit()
            clampToVisibleScreen()
            panel.orderFrontRegardless()
            // 指标变化时 SwiftUI 的重新布局在下一轮 runloop 才落地,届时有量再量一次。
            DispatchQueue.main.async { [weak self] in
                guard let self else { return }
                self.resizeToFit()
                self.clampToVisibleScreen()
            }
            // 形态切换是整棵视图树替换(SwiftUI 的布局可能跨两轮),再兜底量一次,
            // 不然窗口会停在旧形态的尺寸上:条状变统计卡时高度不够,内容会被裁掉。
            if styleChanged {
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.1) { [weak self] in
                    guard let self else { return }
                    self.resizeToFit()
                    self.clampToVisibleScreen()
                }
            }
        } else {
            panel.orderOut(nil)
            hideDashboard()
        }
    }

    /// 条状内容(数字位数/活跃流标签)变化后重新量窗口宽度。
    ///
    /// 窗口宽度只在设置变化时量一次是不够的:吞吐每 2 秒推一次,`9/s → 1234/s`、
    /// 花费 `$1.2 → $12.345` 都会让文本变长,停在旧宽度上就会把末位指标裁掉。
    /// 去抖后再量,且尺寸没变就不动窗口。
    private func scheduleRefit() {
        guard settings.showFloatingWindow, settings.floatingStyle == .bar else { return }
        refitWorkItem?.cancel()
        let item = DispatchWorkItem { [weak self] in
            guard let self else { return }
            self.refitWorkItem = nil
            self.refitIfNeeded()
        }
        refitWorkItem = item
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.08, execute: item)
    }

    private func refitIfNeeded() {
        guard settings.showFloatingWindow else { return }
        let size = fittingSize()
        let current = panel.contentRect(forFrameRect: panel.frame).size
        guard size != current else { return }
        panel.setFrame(
            FloatingPlacement.resizedFramePreservingTopLeft(panel.frame, size: size),
            display: true
        )
        hostingView.frame = NSRect(origin: .zero, size: size)
        hostingView.needsLayout = true
        hostingView.needsDisplay = true
        clampToVisibleScreen()
    }

    /// 设置页「恢复浮窗默认位置」。
    func resetPosition() {
        UserDefaults.standard.removeObject(forKey: "NSWindow Frame \(Self.frameAutosaveName)")
        resizeToFit()
        placeDefault()
    }

    // MARK: - 完整面板(菜单栏项隐藏时的入口)

    /// 卡片单击/右上角按钮:打开或收起完整面板。
    func toggleDashboard() {
        // 点卡片会让面板失焦自动收起;如果这次点击的语义是「收起」,
        // 不要在自动收起之后又立刻打开(否则永远关不上)。
        if let lastAutoHideAt, Date().timeIntervalSince(lastAutoHideAt) < 0.5,
           dashboardPanel?.isVisible != true {
            self.lastAutoHideAt = nil
            return
        }
        if let dashboardPanel, dashboardPanel.isVisible {
            hideDashboard()
            return
        }
        showDashboard()
    }

    private func showDashboard() {
        lastAutoHideAt = nil
        if dashboardPanel == nil {
            dashboardPanel = makeDashboardPanel()
        }
        guard let dashboardPanel else { return }
        positionDashboard(dashboardPanel)
        dashboardPanel.makeKeyAndOrderFront(nil)
    }

    private func hideDashboard() {
        dashboardPanel?.orderOut(nil)
    }

    private func makeDashboardPanel() -> NSPanel {
        let root = MenuBarContentView()
            .environment(store)
            .environment(settings)
        let hosting = FirstMouseHostingView(rootView: root)
        // 先给一个目标宽度再量理想高度(与 WidgetLayoutTests 的离屏量法一致)。
        hosting.setFrameSize(NSSize(width: 400, height: 0))
        hosting.layoutSubtreeIfNeeded()
        var size = hosting.fittingSize
        if size.width < 1 || size.height < 1 {
            size = NSSize(width: 400, height: 640)
        }

        let panel = DashboardPanel(
            contentRect: NSRect(origin: .zero, size: size),
            styleMask: [.nonactivatingPanel, .borderless, .fullSizeContentView],
            backing: .buffered,
            defer: false
        )
        panel.onEscape = { [weak self] in self?.hideDashboard() }
        panel.level = .floating
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .stationary, .ignoresCycle]
        panel.hidesOnDeactivate = false
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = false
        panel.isReleasedWhenClosed = false
        panel.delegate = self

        // 菜单栏面板本身有一层系统玻璃;这里用 popover 材质补上这一层,
        // 面板内各分区的 `.clear` 玻璃才有可读底衬(亮/暗模式自动跟随)。
        let effect = NSVisualEffectView()
        effect.material = .popover
        effect.blendingMode = .behindWindow
        effect.state = .active
        effect.wantsLayer = true
        effect.layer?.cornerRadius = 26
        effect.layer?.masksToBounds = true
        hosting.translatesAutoresizingMaskIntoConstraints = false
        effect.addSubview(hosting)
        NSLayoutConstraint.activate([
            hosting.leadingAnchor.constraint(equalTo: effect.leadingAnchor),
            hosting.trailingAnchor.constraint(equalTo: effect.trailingAnchor),
            hosting.topAnchor.constraint(equalTo: effect.topAnchor),
            hosting.bottomAnchor.constraint(equalTo: effect.bottomAnchor),
        ])
        panel.contentView = effect
        return panel
    }

    /// 优先贴在卡片**左侧**、顶边对齐(卡片始终完整可见、可点来收起);
    /// 左侧放不下再依次尝试下方/上方,最后夹取在屏幕内。
    private func positionDashboard(_ panel: NSPanel) {
        let size = panel.frame.size
        let card = self.panel.frame
        let screen = NSScreen.screens.first { $0.frame.intersects(card) } ?? NSScreen.main
        guard let visible = screen?.visibleFrame else { return }

        let origin = FloatingPlacement.dashboardOrigin(
            cardFrame: card,
            size: size,
            visibleFrame: visible
        )
        let frames = NSScreen.screens.map(\.visibleFrame)
        let frame = FloatingPlacement.clampedFrame(
            NSRect(origin: origin, size: size),
            visibleFrames: frames
        )
        panel.setFrame(frame, display: true)
    }

    func windowDidResignKey(_ notification: Notification) {
        guard let window = notification.object as? NSWindow, window === dashboardPanel else { return }
        // 焦点转到悬浮卡时不要先自动收起:卡片上的单击/按钮要能切换(收起)面板,
        // 否则会变成「先收起、再被点击动作打开」永远关不上。
        if NSApp.keyWindow === panel { return }
        lastAutoHideAt = Date()
        hideDashboard()
    }
}

/// 悬浮窗/完整面板的 SwiftUI 宿主:接受「首次点击」。
///
/// 菜单栏 App 常处非激活态,`NSView` 默认 `acceptsFirstMouse == false` —— 第一下点击
/// 只会让窗口成为 key,不会交给内容视图,表现就是「点浮窗没反应,得点第二下」。
/// 浮窗是常驻的被动面板,必须一次点中。
private final class FirstMouseHostingView<Content: View>: NSHostingView<Content> {
    override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
}

/// 悬浮窗口:点击不激活宿主 App(配合 `.nonactivatingPanel`),但不抢主窗口身份。
private final class FloatingPanel: NSPanel {
    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { false }
}

/// 完整面板:Esc 收起;点卡片同样能收(由控制器的 key 判断 + 卡片点击动作完成)。
private final class DashboardPanel: NSPanel {
    var onEscape: (() -> Void)?

    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { false }

    override func keyDown(with event: NSEvent) {
        if event.keyCode == 53, let onEscape {
            onEscape()
            return
        }
        super.keyDown(with: event)
    }
}

/// 浮窗尺寸测量:条状宽度由内容决定。若直接按当前(可能还是卡片的)窄宽度量,
/// SwiftUI 会把单行文本压缩并回报截断后的宽度,窗口就永远长不大;
/// 先放宽到探测宽度再取 `fittingSize`,固定宽度的统计卡不受影响。
enum FloatingSizing {
    /// 量内容的理想尺寸。
    ///
    /// 探测期间会把宿主放宽到 `probeWidth`,否则宿主还停在旧窗口(可能只有卡片宽度)时,
    /// 单行文本会被压缩并回报截断宽度,条状窗口就会越量越窄。量完**必须还原原始尺寸**:
    /// 这个宿主就是窗口的 contentView,留在探测宽度上会让 SwiftUI 把固定宽度的内容
    /// 水平居中到 1200pt 的中间,窗口只截到左侧空白(看起来什么都没有)。
    static func fittingSize(of view: NSView, probeWidth: CGFloat = 1200) -> NSSize {
        let original = view.frame
        view.setFrameSize(NSSize(width: probeWidth, height: max(original.height, 1)))
        view.layoutSubtreeIfNeeded()
        let size = view.fittingSize
        view.setFrameSize(original.size)
        view.layoutSubtreeIfNeeded()
        return size
    }
}

/// 悬浮卡片的纯几何计算(单测覆盖)。
enum FloatingPlacement {    /// 默认位置:屏幕可用区右上角内缩,落在菜单栏下方,不会压到刘海。
    static func defaultOrigin(visibleFrame: NSRect, size: NSSize, inset: CGFloat = 12) -> NSPoint {
        NSPoint(
            x: visibleFrame.maxX - size.width - inset,
            y: visibleFrame.maxY - size.height - inset
        )
    }

    /// 尺寸变化时保持左上角:卡片在上方时向下长,不会顶出屏幕。
    static func resizedFramePreservingTopLeft(_ frame: NSRect, size: NSSize) -> NSRect {
        NSRect(x: frame.origin.x, y: frame.maxY - size.height, width: size.width, height: size.height)
    }

    /// 完整面板相对卡片的落点:优先左侧、顶边对齐(卡片保持完整可见);
    /// 左侧不够宽再下方,最后上方。最终由调用方夹取到屏幕内。
    static func dashboardOrigin(
        cardFrame: NSRect,
        size: NSSize,
        visibleFrame: NSRect,
        gap: CGFloat = 8
    ) -> NSPoint {
        let left = NSPoint(x: cardFrame.minX - size.width - gap, y: cardFrame.maxY - size.height)
        if left.x >= visibleFrame.minX,
           left.y >= visibleFrame.minY,
           left.y + size.height <= visibleFrame.maxY {
            return left
        }

        let below = NSPoint(x: cardFrame.maxX - size.width, y: cardFrame.minY - size.height - gap)
        if below.y >= visibleFrame.minY {
            return below
        }

        return NSPoint(x: cardFrame.maxX - size.width, y: cardFrame.maxY + gap)
    }

    /// 夹取到与窗口重叠面积最大的那块屏幕的可用区。
    static func clampedFrame(_ frame: NSRect, visibleFrames: [NSRect]) -> NSRect {
        guard !visibleFrames.isEmpty else { return frame }
        let screen = visibleFrames.max {
            overlapArea($0, frame) < overlapArea($1, frame)
        } ?? visibleFrames[0]
        var result = frame
        result.origin.x = max(screen.minX, min(frame.origin.x, screen.maxX - frame.width))
        result.origin.y = max(screen.minY, min(frame.origin.y, screen.maxY - frame.height))
        return result
    }

    private static func overlapArea(_ a: NSRect, _ b: NSRect) -> CGFloat {
        let overlap = a.intersection(b)
        guard !overlap.isNull, !overlap.isEmpty else { return 0 }
        return overlap.width * overlap.height
    }
}

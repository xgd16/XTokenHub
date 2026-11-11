import AppKit

/// 打开设置窗口。
///
/// 不走 SwiftUI 的 `Settings` 场景:菜单栏 App 常处非激活态,`showSettingsWindow:`
/// 依赖场景与激活时序,经常被吞掉(表现为「点设置没反应」)。这里用 AppKit 自建
/// 普通 `NSWindow` 承载 `SettingsView`,直接 `makeKeyAndOrderFront` 就能出现。
@MainActor
enum AppSettingsWindow {
    private static let windowAutosaveName = "XTokenHubSettingsWindow"

    /// 由 AppDelegate 注入:用当前 store/settings 构建设置页。
    static var makeContent: (() -> NSView)?

    private static var settingsWindow: NSWindow?

    static func open() {
        NSApp.activate(ignoringOtherApps: true)
        if let settingsWindow {
            settingsWindow.makeKeyAndOrderFront(nil)
            return
        }
        guard let makeContent else { return }
        show(makeContent())
    }

    private static func show(_ content: NSView) {
        // 先在 440pt 宽下量理想高度(与系统设置窗口的默认宽度一致)。
        content.setFrameSize(NSSize(width: 440, height: 0))
        content.layoutSubtreeIfNeeded()
        var size = content.fittingSize
        if size.width < 1 || size.height < 1 {
            size = NSSize(width: 440, height: 640)
        }
        // 内容比屏幕高时窗口夹到可用区,Form 自身可滚动。
        let maxHeight = (NSScreen.main?.visibleFrame.height ?? 900) - 80
        size.height = min(max(size.height, 240), maxHeight)
        size.width = max(size.width, 440)

        let window = NSWindow(
            contentRect: NSRect(origin: .zero, size: size),
            styleMask: [.titled, .closable, .miniaturizable],
            backing: .buffered,
            defer: false
        )
        window.title = "设置"
        window.contentView = content
        window.isReleasedWhenClosed = false
        window.setFrameAutosaveName(windowAutosaveName)
        // `setFrameAutosaveName` 只注册名字(返回值是「名字是否设置成功」,不是「是否有旧位置」),
        // 必须显式 `setFrameUsingName` 才真正恢复;没有历史位置时居中,别停在 (0,0)
        // 被屏幕夹到左下角。恢复的位置也要夹回可见区(换显示器后可能出屏)。
        if window.setFrameUsingName(windowAutosaveName) {
            let frames = NSScreen.screens.map(\.visibleFrame)
            window.setFrame(
                FloatingPlacement.clampedFrame(window.frame, visibleFrames: frames),
                display: false
            )
        } else {
            window.center()
        }
        window.makeKeyAndOrderFront(nil)
        settingsWindow = window
    }
}

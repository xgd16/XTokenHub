import AppKit

/// 可靠地打开 SwiftUI 的 Settings 场景。
///
/// `MenuBarExtra` 面板是临时窗口:点击时 App 处于非激活态,面板又随焦点转移关闭,
/// 直接调用 `openSettings` 可能被吞掉,或窗口打开后未置前(表现为「点不开」)。
///
/// 浮窗里的按钮不在 SwiftUI 场景内(`NSHostingView`),拿不到 `openSettings` action,
/// 改走 AppKit 的 `showSettingsWindow:` selector(macOS 14+ SwiftUI 实现,
/// 旧的 `showPreferencesWindow:` 已不存在);菜单栏面板内仍优先用环境 action。
@MainActor
enum AppSettingsWindow {
    private static let windowIdentifier = "com_apple_SwiftUI_Settings_window"

    private static func find() -> NSWindow? {
        NSApp.windows.first {
            $0.identifier?.rawValue == windowIdentifier
                || $0.frameAutosaveName == windowIdentifier
        }
    }

    /// - Parameter fallback: SwiftUI 环境里的 `openSettings`(仅菜单栏面板内可用)。
    static func open(fallback: (() -> Void)? = nil) {
        NSApp.activate(ignoringOtherApps: true)
        if let existing = find() {
            existing.makeKeyAndOrderFront(nil)
            return
        }

        if let fallback {
            fallback()
        } else {
            sendShowSettings()
        }

        DispatchQueue.main.asyncAfter(deadline: .now() + 0.1) {
            NSApp.activate(ignoringOtherApps: true)
            if let window = find() {
                window.makeKeyAndOrderFront(nil)
            } else {
                // 时序兜底:action 没有落地时再走一次 AppKit selector。
                sendShowSettings()
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.1) {
                    NSApp.activate(ignoringOtherApps: true)
                    find()?.makeKeyAndOrderFront(nil)
                }
            }
        }
    }

    private static func sendShowSettings() {
        NSApp.sendAction(Selector(("showSettingsWindow:")), to: nil, from: nil)
    }
}

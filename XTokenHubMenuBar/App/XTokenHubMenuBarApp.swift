import SwiftUI
import UserNotifications

@main
struct XTokenHubMenuBarApp: App {
    @NSApplicationDelegateAdaptor private var appDelegate: AppDelegate

    var body: some Scene {
        MenuBarExtra {
            MenuBarContentView()
                .environment(appDelegate.store)
                .environment(appDelegate.settings)
        } label: {
            MenuBarLabel(store: appDelegate.store, settings: appDelegate.settings)
        }
        .menuBarExtraStyle(.window)

        Settings {
            SettingsView()
                .environment(appDelegate.store)
                .environment(appDelegate.settings)
        }
    }
}

/// 状态与设置的唯一属主,随应用生命周期常驻。
@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    let store = HubStore()
    let settings = AppSettings()

    func applicationDidFinishLaunching(_ notification: Notification) {
        // 峰谷通知:菜单栏 App 常处非激活态,挂上代理保证面板打开时也弹横幅
        UNUserNotificationCenter.current().delegate = self
        settings.onPeakNotifyChange = { [weak self] enabled in
            Task { @MainActor in await self?.store.handlePeakNotifyChange(enabled) }
        }
        store.start(with: settings)
    }
}

extension AppDelegate: UNUserNotificationCenterDelegate {
    /// App 处于激活(面板打开)时也照常弹横幅,避免正好在看面板时错过切换提醒。
    /// nonisolated:代理参数非 Sendable,不能跨进主 actor 隔离域(结果也不依赖任何状态)。
    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification
    ) async -> UNNotificationPresentationOptions {
        [.banner]
    }
}

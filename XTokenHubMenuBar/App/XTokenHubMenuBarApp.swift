import SwiftUI
import UserNotifications

@main
struct XTokenHubMenuBarApp: App {
    @NSApplicationDelegateAdaptor private var appDelegate: AppDelegate

    var body: some Scene {
        MenuBarExtra(isInserted: menuBarInserted) {
            MenuBarContentView()
                .environment(appDelegate.store)
                .environment(appDelegate.settings)
        } label: {
            MenuBarLabel(store: appDelegate.store, settings: appDelegate.settings)
        }
        .menuBarExtraStyle(.window)
        // 不再声明 SwiftUI `Settings` 场景:它的窗口依赖 App 激活态,菜单栏 App 里
        // `showSettingsWindow:` 常被吞掉;统一走 `AppSettingsWindow` 的自建窗口。
    }

    /// 菜单栏项显隐:设置页与浮窗右键菜单都走同一对守卫方法,
    /// 保证「菜单栏 + 浮窗」至少保留一个入口。
    private var menuBarInserted: Binding<Bool> {
        Binding(
            get: { appDelegate.settings.showMenuBarItem },
            set: { appDelegate.settings.setMenuBarItemVisible($0) }
        )
    }
}

/// 状态与设置的唯一属主,随应用生命周期常驻。
@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    let store = HubStore()
    let settings = AppSettings()
    /// 置顶悬浮卡片(含菜单栏项隐藏后的完整面板入口)。
    private var floatingController: FloatingPanelController?

    func applicationDidFinishLaunching(_ notification: Notification) {
        // 峰谷通知:菜单栏 App 常处非激活态,挂上代理保证面板打开时也弹横幅
        UNUserNotificationCenter.current().delegate = self
        settings.onPeakNotifyChange = { [weak self] enabled in
            Task { @MainActor in await self?.store.handlePeakNotifyChange(enabled) }
        }
        // 设置页用自建窗口承载:浮窗/面板都跑在普通 NSHostingView 里,SwiftUI 的
        // Settings 场景在非激活态下唤不起来(点「设置…」没反应),自建窗口不依赖激活时序。
        let store = store
        let settings = settings
        AppSettingsWindow.makeContent = {
            NSHostingView(
                rootView: SettingsView()
                    .environment(store)
                    .environment(settings)
            )
        }
        floatingController = FloatingPanelController(store: store, settings: settings)
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

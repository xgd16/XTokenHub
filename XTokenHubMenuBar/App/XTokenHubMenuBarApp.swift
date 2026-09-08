import SwiftUI

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
        store.start(with: settings)
    }
}

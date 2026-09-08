import Foundation
import Observation
import ServiceManagement

/// 用户设置:UserDefaults 持久化,地址变更通过 onChange 通知 HubStore 重连。
@MainActor
@Observable
final class AppSettings {
    /// 菜单栏可显示的指标,用户在设置中自由勾选组合。
    enum MenuBarMetric: String, CaseIterable, Identifiable {
        case icon
        case requests
        case tokens
        case cacheHit
        case throughput

        var id: String { rawValue }

        var label: String {
            switch self {
            case .icon: "图标"
            case .requests: "今日请求数"
            case .tokens: "今日 Token"
            case .cacheHit: "缓存命中率"
            case .throughput: "实时速度"
            }
        }
    }

    private enum Key {
        static let baseURL = "baseURLString"
        static let metrics = "menuBarMetrics"
        static let legacyMode = "menuBarMode" // 旧版单选模式,迁移后废弃
        static let launchAtLogin = "launchAtLogin"
    }

    var baseURLString: String {
        didSet {
            guard oldValue != baseURLString else { return }
            UserDefaults.standard.set(baseURLString, forKey: Key.baseURL)
            onChange?()
        }
    }

    /// 菜单栏显示的指标组合(顺序固定:图标、请求数、Token、命中率、速度)。
    var menuBarMetrics: Set<MenuBarMetric> {
        didSet {
            guard oldValue != menuBarMetrics else { return }
            let raw = menuBarMetrics.map(\.rawValue).sorted().joined(separator: ",")
            UserDefaults.standard.set(raw, forKey: Key.metrics)
        }
    }

    var launchAtLogin: Bool {
        didSet {
            guard oldValue != launchAtLogin else { return }
            UserDefaults.standard.set(launchAtLogin, forKey: Key.launchAtLogin)
            applyLaunchAtLogin()
        }
    }

    private(set) var launchAtLoginError: String?

    /// 设置变更回调(当前只有服务地址需要触发重连)。
    var onChange: (() -> Void)?

    init() {
        let d = UserDefaults.standard
        baseURLString = d.string(forKey: Key.baseURL) ?? "http://127.0.0.1:9192"

        // 迁移旧版单选模式 → 组合;无旧值时默认 数量 + 实时速度
        if let legacy = d.string(forKey: Key.legacyMode) {
            menuBarMetrics = AppSettings.legacyMapping(legacy)
            d.removeObject(forKey: Key.legacyMode)
        } else if let raw = d.string(forKey: Key.metrics), !raw.isEmpty {
            menuBarMetrics = Set(raw.split(separator: ",").compactMap { MenuBarMetric(rawValue: String($0)) })
        } else {
            menuBarMetrics = [.tokens, .throughput]
        }

        launchAtLogin = d.bool(forKey: Key.launchAtLogin) && SMAppService.mainApp.status == .enabled
    }

    private static func legacyMapping(_ mode: String) -> Set<MenuBarMetric> {
        switch mode {
        case "tokensToday": [.tokens]
        case "throughput": [.throughput]
        case "iconAndText": [.icon, .tokens]
        case "icon": [.icon]
        default: [.tokens, .throughput]
        }
    }

    var httpBaseURL: URL { HubURL.httpBase(baseURLString) }

    private func applyLaunchAtLogin() {
        let service = SMAppService.mainApp
        do {
            if launchAtLogin {
                if service.status != .enabled { try service.register() }
            } else if service.status == .enabled {
                try service.unregister()
            }
            launchAtLoginError = nil
        } catch {
            // 常见于未把 .app 拷入 /Applications 时注册失败,展示原因即可
            launchAtLoginError = error.localizedDescription
        }
    }
}

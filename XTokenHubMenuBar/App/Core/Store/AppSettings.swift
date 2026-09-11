import Foundation
import Observation
import ServiceManagement

/// 一个数据来源:XTokenHub 服务实例(名称 + 地址)。
struct DataSource: Codable, Identifiable, Equatable, Sendable {
    var id: UUID = UUID()
    var name: String
    var urlString: String
}

/// 用户设置:UserDefaults 持久化,数据来源增删与地址变更通过 onChange 通知 HubStore 重连。
@MainActor
@Observable
final class AppSettings {
    /// 菜单栏可显示的指标,用户在设置中自由勾选组合。
    enum MenuBarMetric: String, CaseIterable, Identifiable {
        case icon
        case requests
        case tokens
        case cacheHit
        case costToday
        case throughput

        var id: String { rawValue }

        var label: String {
            switch self {
            case .icon: "图标"
            case .requests: "今日请求数"
            case .tokens: "今日 Token"
            case .cacheHit: "缓存命中率"
            case .costToday: "今日花费"
            case .throughput: "实时速度"
            }
        }
    }

    private enum Key {
        static let sources = "dataSources"
        static let selectedSourceID = "selectedSourceID"
        static let legacyBaseURL = "baseURLString" // 旧版单地址,迁移后移除
        static let metrics = "menuBarMetrics"
        static let legacyMode = "menuBarMode" // 旧版单选模式,迁移后废弃
        static let launchAtLogin = "launchAtLogin"
    }

    /// 数据来源列表,至少保留一个(删空时回退默认)。
    private(set) var dataSources: [DataSource]

    /// 当前展示的数据来源。
    private(set) var selectedSourceID: UUID

    var selectedSource: DataSource {
        dataSources.first { $0.id == selectedSourceID } ?? dataSources[0]
    }

    /// 菜单栏显示的指标组合(顺序固定:图标、请求数、Token、命中率、花费、速度)。
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

    /// 设置变更回调(数据来源变化需触发 HubStore 重连)。
    var onChange: (() -> Void)?

    init() {
        let d = UserDefaults.standard

        // 迁移旧版单地址 → 来源列表
        var sources: [DataSource]
        if let raw = d.string(forKey: Key.sources),
           let data = raw.data(using: .utf8),
           let list = try? JSONDecoder().decode([DataSource].self, from: data), !list.isEmpty {
            sources = list
        } else {
            let legacy = d.string(forKey: Key.legacyBaseURL) ?? "http://127.0.0.1:9192"
            sources = [DataSource(name: "默认", urlString: legacy)]
        }
        dataSources = sources

        if let raw = d.string(forKey: Key.selectedSourceID),
           let id = UUID(uuidString: raw),
           sources.contains(where: { $0.id == id }) {
            selectedSourceID = id
        } else {
            selectedSourceID = sources[0].id
        }

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

    // MARK: - 数据来源操作

    /// 切换当前来源(面板快速切换与设置页共用)。
    func selectSource(_ id: UUID) {
        guard dataSources.contains(where: { $0.id == id }), selectedSourceID != id else { return }
        selectedSourceID = id
        persistSources()
        onChange?()
    }

    func addSource() {
        dataSources.append(DataSource(name: "来源 \(dataSources.count + 1)", urlString: ""))
        persistSources()
        onChange?()
    }

    /// 新增一个已填好信息的来源(设置页"添加"表单或导入场景)。
    func addSource(name: String, urlString: String) {
        dataSources.append(DataSource(name: name, urlString: urlString))
        persistSources()
        onChange?()
    }

    func removeSource(_ id: UUID) {
        guard let idx = dataSources.firstIndex(where: { $0.id == id }) else { return }
        dataSources.remove(at: idx)
        if dataSources.isEmpty {
            dataSources = [DataSource(name: "默认", urlString: HubURL.fallback.absoluteString)]
        }
        if selectedSourceID == id {
            selectedSourceID = dataSources[max(0, idx - 1)].id
        }
        persistSources()
        onChange?()
    }

    /// 编辑来源的名称与地址;地址变化由 HubStore 防抖后重连。
    func updateSource(_ id: UUID, name: String, urlString: String) {
        guard let idx = dataSources.firstIndex(where: { $0.id == id }) else { return }
        let trimmedName = name.trimmingCharacters(in: .whitespacesAndNewlines)
        let trimmedURL = urlString.trimmingCharacters(in: .whitespacesAndNewlines)
        guard dataSources[idx].name != trimmedName || dataSources[idx].urlString != trimmedURL else { return }
        dataSources[idx].name = trimmedName.isEmpty ? "未命名" : trimmedName
        dataSources[idx].urlString = trimmedURL
        persistSources()
        onChange?()
    }

    private func persistSources() {
        if let data = try? JSONEncoder().encode(dataSources) {
            UserDefaults.standard.set(String(data: data, encoding: .utf8), forKey: Key.sources)
        }
        UserDefaults.standard.set(selectedSourceID.uuidString, forKey: Key.selectedSourceID)
        UserDefaults.standard.removeObject(forKey: Key.legacyBaseURL)
    }

    var httpBaseURL: URL { HubURL.httpBase(selectedSource.urlString) }

    private static func legacyMapping(_ mode: String) -> Set<MenuBarMetric> {
        switch mode {
        case "tokensToday": [.tokens]
        case "throughput": [.throughput]
        case "iconAndText": [.icon, .tokens]
        case "icon": [.icon]
        default: [.tokens, .throughput]
        }
    }

    var httpBaseURLLegacy: URL { HubURL.httpBase(selectedSource.urlString) }

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

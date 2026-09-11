import Foundation
import WidgetKit

/// Widget 时间线提供器。
///
/// 刷新策略：
/// - 主 App 数据变化时调用 `WidgetCenter.shared.reloadTimelines` 立即重建；
/// - 同时 15 分钟兜底刷新，保证主 App 未运行时数据也能更新。
struct XTokenHubProvider: AppIntentTimelineProvider {
    typealias Entry = XTokenHubEntry
    typealias Intent = XTokenHubConfigurationIntent

    func placeholder(in context: Context) -> XTokenHubEntry {
        .placeholder()
    }

    func snapshot(for configuration: XTokenHubConfigurationIntent, in context: Context) async -> XTokenHubEntry {
        if context.isPreview { return .placeholder() }
        return await makeEntry(for: configuration)
    }

    func timeline(for configuration: XTokenHubConfigurationIntent, in context: Context) async -> Timeline<XTokenHubEntry> {
        let entry = await makeEntry(for: configuration)
        let next = Calendar.current.date(byAdding: .minute, value: 15, to: Date()) ?? Date().addingTimeInterval(900)
        return Timeline(entries: [entry], policy: .after(next))
    }

    private func makeEntry(for configuration: XTokenHubConfigurationIntent) async -> XTokenHubEntry {
        let snapshot = await WidgetDataProvider.loadSnapshot(configuredURL: configuration.sourceURL)
        return XTokenHubEntry(date: Date(), snapshot: snapshot)
    }
}

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

        // 在峰谷切换时刻追加条目：Widget 到点自动翻状态，不必等 15 分钟兜底刷新。
        // 只看主角模型（距下次切换最近），追加其后 2 个切换点，覆盖一个工作日的午休/收盘。
        var entries = [entry]
        if let lead = entry.peakLead(at: entry.date) {
            var cursor = entry.date
            for _ in 0..<2 {
                guard let next = lead.rule.nextSwitch(after: cursor, calendar: entry.holidayCalendar) else { break }
                entries.append(XTokenHubEntry(date: next.at, snapshot: entry.snapshot))
                cursor = next.at
            }
        }

        let last = entries.last?.date ?? Date()
        let next = Calendar.current.date(byAdding: .minute, value: 15, to: last) ?? last.addingTimeInterval(900)
        return Timeline(entries: entries, policy: .after(next))
    }

    private func makeEntry(for configuration: XTokenHubConfigurationIntent) async -> XTokenHubEntry {
        let snapshot = await WidgetDataProvider.loadSnapshot(configuredURL: configuration.sourceURL)
        return XTokenHubEntry(date: Date(), snapshot: snapshot)
    }
}

import WidgetKit
import SwiftUI

/// XTokenHub Widget — macOS 桌面小组件入口。
///
/// 支持三种尺寸：
/// - Small (170×170): 今日速览
/// - Medium (358×170): 今日统计 + 趋势图
/// - Large (358×358): 完整仪表盘
@main
struct XTokenHubWidget: Widget {
    let kind = WidgetDataProvider.widgetKind

    var body: some WidgetConfiguration {
        AppIntentConfiguration(kind: kind, intent: XTokenHubConfigurationIntent.self, provider: XTokenHubProvider()) { entry in
            XTokenHubWidgetEntryView(entry: entry)
                .containerBackground(.fill.tertiary, for: .widget)
        }
        .configurationDisplayName("XTokenHub 概览")
        .description("展示 XTokenHub 服务的实时 Token 使用统计")
        .supportedFamilies([.systemSmall, .systemMedium, .systemLarge])
    }
}

/// Widget 入口视图 — 根据尺寸分发到对应的子视图。
struct XTokenHubWidgetEntryView: View {
    var entry: XTokenHubEntry
    @Environment(\.widgetFamily) var family

    var body: some View {
        switch family {
        case .systemSmall:
            SmallWidgetView(entry: entry)
        case .systemMedium:
            MediumWidgetView(entry: entry)
        case .systemLarge:
            LargeWidgetView(entry: entry)
        default:
            SmallWidgetView(entry: entry)
        }
    }
}

// MARK: - Widget 预览

#Preview("Small", as: .systemSmall) {
    XTokenHubWidget()
} timeline: {
    XTokenHubEntry.placeholder()
}

#Preview("Medium", as: .systemMedium) {
    XTokenHubWidget()
} timeline: {
    XTokenHubEntry.placeholder()
}

#Preview("Large", as: .systemLarge) {
    XTokenHubWidget()
} timeline: {
    XTokenHubEntry.placeholder()
}

import SwiftUI
import WidgetKit

/// Small (170×170，约 138pt 可用高) — 今日速览。
///
/// 垂直预算：标题 13 + 主数字 38 + 迷你趋势 22 + 双指标 26 = 99pt，
/// 余下约 39pt 由三个等分间隙吸收，避免出现两处大空洞。
struct SmallWidgetView: View {
    let entry: XTokenHubEntry

    var body: some View {
        if let summary = entry.summary {
            VStack(spacing: 0) {
                SourceHeader(
                    sourceName: entry.sourceName,
                    isConnected: entry.isConnected,
                    trailing: entry.snapshot.timestamp.formatted(date: .omitted, time: .shortened)
                )

                Spacer(minLength: 6)

                // 主视觉：今日 Token
                VStack(alignment: .leading, spacing: 1) {
                    CompactNumber(
                        value: summary.totalTokens,
                        font: WD.heroFont(28),
                        unitStyle: .system(size: 12, weight: .medium)
                    )
                    Text("今日 Token")
                        .font(WD.captionFont)
                        .foregroundStyle(.tertiary)
                }
                .frame(maxWidth: .infinity, alignment: .leading)

                Spacer(minLength: 6)

                MiniTrendChart(values: entry.trendValues)
                    .frame(height: 22)

                Spacer(minLength: 6)

                HStack(spacing: 0) {
                    MetricStack(
                        icon: "arrow.up.arrow.down",
                        value: "\(summary.totalRequests)",
                        label: "请求"
                    )
                    MetricStack(
                        icon: "percent",
                        value: TokenFormatter.percent(summary.cacheHitRate, digits: 0),
                        label: "缓存命中"
                    )
                }
            }
            .padding(WD.inset)
        } else {
            NoDataView()
                .padding(WD.inset)
        }
    }
}

// MARK: - 预览

#Preview("Small Widget", as: .systemSmall) {
    XTokenHubWidget()
} timeline: {
    XTokenHubEntry.placeholder()
}

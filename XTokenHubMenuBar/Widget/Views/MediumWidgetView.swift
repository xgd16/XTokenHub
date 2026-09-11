import SwiftUI
import WidgetKit

/// Medium (358×170，约 326×138 可用) — 今日统计 + 趋势。
///
/// 左栏是主视觉（今日 Token + 面积图），右栏是四项明细；
/// 两栏之间用发丝线分隔，明确结构而不靠留白猜测。
struct MediumWidgetView: View {
    let entry: XTokenHubEntry

    var body: some View {
        if let summary = entry.summary {
            HStack(spacing: 10) {
                heroColumn(summary)
                    .frame(maxWidth: .infinity)

                WidgetHairline(vertical: true)

                metricsColumn(summary)
                    .frame(width: 132)
            }
            .padding(WD.inset)
        } else {
            NoDataView()
                .padding(WD.inset)
        }
    }

    // MARK: 左栏

    private func heroColumn(_ summary: Summary) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            SourceHeader(
                sourceName: entry.sourceName,
                isConnected: entry.isConnected,
                trailing: entry.snapshot.timestamp.formatted(date: .omitted, time: .shortened)
            )

            Spacer(minLength: 4)

            Text("今日 Token")
                .font(WD.captionFont)
                .foregroundStyle(.tertiary)

            CompactNumber(
                value: summary.totalTokens,
                font: WD.heroFont(26),
                unitStyle: .system(size: 12, weight: .medium)
            )

            Spacer(minLength: 4)

            MiniTrendChart(values: entry.trendValues)
                .frame(minHeight: 34, maxHeight: .infinity)
        }
    }

    // MARK: 右栏

    private func metricsColumn(_ summary: Summary) -> some View {
        let rows: [(String, String)] = [
            ("请求数", "\(summary.totalRequests)"),
            ("缓存命中", TokenFormatter.percent(summary.cacheHitRate)),
            ("今日花费", entry.costToday.map {
                MoneyFormatter.format(usd: $0.spentUSD, options: entry.snapshot.moneyOptions)
            } ?? "—"),
            ("平均耗时", TokenFormatter.duration(Int64(summary.avgDurationMS))),
        ]

        return VStack(spacing: 0) {
            Spacer(minLength: 0)
            ForEach(Array(rows.enumerated()), id: \.offset) { index, row in
                StatRow(label: row.0, value: row.1)
                if index < rows.count - 1 {
                    Spacer(minLength: 0)
                }
            }
            Spacer(minLength: 0)
        }
    }
}

// MARK: - 预览

#Preview("Medium Widget", as: .systemMedium) {
    XTokenHubWidget()
} timeline: {
    XTokenHubEntry.placeholder()
}

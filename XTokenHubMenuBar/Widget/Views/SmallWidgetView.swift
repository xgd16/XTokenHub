import SwiftUI
import WidgetKit

/// Small (170×170，约 138pt 可用高) — 今日速览。
///
/// 垂直预算（离屏实测，见 Tests/Widget/WidgetLayoutTests）：
/// 固定内容 115pt（标题 13 + 主数字 38 + 迷你趋势 22 + 双指标 26，含行距）；
/// 配置了峰谷时段价的模型再多一行 13pt（合计 128pt）。
/// 四处间隙是弹性的，下限取 2pt 而非 6pt —— 只在内容撑高时让位，
/// 有富余时照常均分吸收余量，这样带峰谷行也不会被挤出边界（无峰谷时 131pt / 有峰谷时 136pt）。
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

                Spacer(minLength: 2)

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

                Spacer(minLength: 2)

                MiniTrendChart(values: entry.trendValues)
                    .frame(height: 22)

                Spacer(minLength: 2)

                // 峰谷状态：有配置时段价的模型才出现（固定 13pt，见文件头垂直预算）
                if let lead = entry.peakLead(at: entry.date) {
                    PeakStatusRow(lead: lead, date: entry.date)
                }

                Spacer(minLength: 2)

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

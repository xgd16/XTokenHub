import SwiftUI
import WidgetKit

/// Medium (358×170，约 326×138 可用) — 今日统计 + 趋势。
///
/// 左栏是主视觉（今日 Token + 面积图），右栏是明细行（有余额渠道时多出「余额·渠道名」行，最多 3 行）；
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

            // 峰谷状态：有配置时段价的模型才出现（固定 13pt，趋势图最低 34pt 仍放得下）
            if let lead = entry.peakLead(at: entry.date) {
                PeakStatusRow(lead: lead, date: entry.date)
                    .padding(.top, 2)
            }

            Spacer(minLength: 4)

            MiniTrendChart(values: entry.trendValues)
                .frame(minHeight: 34, maxHeight: .infinity)
        }
    }

    // MARK: 右栏

    private func metricsColumn(_ summary: Summary) -> some View {
        let metrics: [(String, String)] = [
            ("请求数", "\(summary.totalRequests)"),
            ("缓存命中", TokenFormatter.percent(summary.cacheHitRate)),
            ("今日花费", entry.costToday.map {
                MoneyFormatter.format(usd: $0.spentUSD, options: entry.snapshot.moneyOptions)
            } ?? "—"),
            ("平均耗时", TokenFormatter.duration(Int64(summary.avgDurationMS))),
        ]
        // 勾选渠道的余额(设置页多选;未设置时=全部):右栏还有富余,最多 3 行也不撑高
        let candidates = entry.balanceCandidates
        let balances = Array(candidates.prefix(3))
        let overflow = candidates.count - balances.count

        return VStack(spacing: 0) {
            Spacer(minLength: 0)
            ForEach(Array(metrics.enumerated()), id: \.offset) { _, row in
                StatRow(label: row.0, value: row.1)
                Spacer(minLength: 0)
            }
            ForEach(balances) { balance in
                // 余额行:状态点 + 渠道名 + 原币种金额(与 Large 的 chip 同一套视觉)
                BalanceStatRow(balance: balance)
                Spacer(minLength: 0)
            }
            if overflow > 0 {
                Text("还有 \(overflow) 个勾选渠道未显示")
                    .font(WD.captionFont)
                    .foregroundStyle(.tertiary)
                    .lineLimit(1)
                Spacer(minLength: 0)
            }
            Spacer(minLength: 0)
        }
    }
}

/// 余额行：状态点 + 渠道名 + 上游原币种金额。
///
/// 宽度分配与 `StatRow` 相反：**渠道名优先**（用户要认出「哪一家」，截断了就没意义），
/// 金额靠 `minimumScaleFactor` 缩字号让位 —— 数字仍完整，最多缩到 0.6 倍。
private struct BalanceStatRow: View {
    let balance: ChannelBalance

    var body: some View {
        HStack(spacing: 5) {
            Circle()
                .fill(balance.ok ? Color.green : Color.secondary.opacity(0.4))
                .frame(width: 5, height: 5)

            Text(balance.channelName)
                .font(WD.captionFont)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .truncationMode(.tail)
                .layoutPriority(1)

            Spacer(minLength: 4)

            Text(WD.balanceText(balance))
                .font(WD.valueFont)
                .monospacedDigit()
                .lineLimit(1)
                .minimumScaleFactor(0.6)
        }
    }
}

// MARK: - 预览

#Preview("Medium Widget", as: .systemMedium) {
    XTokenHubWidget()
} timeline: {
    XTokenHubEntry.placeholder()
}

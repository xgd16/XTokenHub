import SwiftUI
import WidgetKit

/// Large (358×358，约 326pt 可用高) — 完整概览。
///
/// 垂直预算：标题 13 + 指标宫格 78 + 趋势标题 13 + 趋势图（弹性，至少 70）
/// + 排行标题 13 + 排行 39 + 余额 13，加间隙共约 204pt；
/// 余量全部给趋势图，因此不会再出现内容被挤出边界的情况。
struct LargeWidgetView: View {
    let entry: XTokenHubEntry

    var body: some View {
        if let summary = entry.summary {
            VStack(alignment: .leading, spacing: 0) {
                SourceHeader(
                    sourceName: entry.sourceName,
                    isConnected: entry.isConnected,
                    trailing: entry.snapshot.timestamp.formatted(date: .omitted, time: .shortened)
                )

                gap()

                MetricGrid(summary: summary, costToday: entry.costToday, options: entry.snapshot.moneyOptions)

                gap()

                SectionLabel(text: "Token 用量趋势（24 小时）")
                Spacer().frame(height: WD.labelGap)
                // 弹性填充剩余空间，但设上限：模型/余额都为空时不会被拉成一条巨幅色块
                MiniTrendChart(values: entry.trendValues)
                    .frame(minHeight: 70, maxHeight: 150)

                if !entry.modelTop.isEmpty {
                    gap()
                    SectionLabel(text: "模型使用排行")
                    Spacer().frame(height: WD.labelGap)
                    ModelRankingSection(
                        models: entry.modelTop,
                        totalRequests: summary.totalRequests
                    )
                }

                if !entry.channelBalances.isEmpty {
                    gap()
                    ChannelBalanceRow(balances: entry.channelBalances)
                }

                // 内容不足一屏时（如无模型/余额数据），余量留在底部而非拉伸图表
                Spacer(minLength: 0)
            }
            .padding(WD.inset)
        } else {
            NoDataView()
                .padding(WD.inset)
        }
    }

    private func gap() -> some View {
        Spacer().frame(height: WD.sectionGap)
    }
}

// MARK: - 指标宫格（2×3）

private struct MetricGrid: View {
    let summary: Summary
    let costToday: CostForecast?
    let options: MoneyFormatter.Options

    private let columns = Array(repeating: GridItem(.flexible(), spacing: 6), count: 3)

    var body: some View {
        LazyVGrid(columns: columns, spacing: 6) {
            MetricPill(
                icon: "arrow.up.arrow.down",
                value: "\(summary.totalRequests)",
                label: "请求",
                color: .blue
            )
            MetricPill(
                icon: "token",
                value: TokenFormatter.compact(summary.totalTokens),
                label: "Token",
                color: .green
            )
            MetricPill(
                icon: "percent",
                value: TokenFormatter.percent(summary.cacheHitRate),
                label: "缓存",
                color: .orange
            )

            if let cost = costToday {
                MetricPill(
                    // 图标跟展示币种走，避免出现「¥」配美元符号的错位
                    icon: options.currency == "CNY" ? "yensign.circle" : "dollarsign.circle",
                    value: MoneyFormatter.format(usd: cost.spentUSD, options: options),
                    label: "花费",
                    color: .red
                )
            } else {
                MetricPill(
                    icon: "arrow.triangle.branch",
                    value: TokenFormatter.percent(summary.nativeRatio, digits: 0),
                    label: "透传",
                    color: .teal
                )
            }

            MetricPill(
                icon: "clock",
                value: TokenFormatter.duration(Int64(summary.avgDurationMS)),
                label: "均耗",
                color: .purple
            )
            MetricPill(
                icon: "exclamationmark.triangle",
                value: "\(summary.errorRequests)",
                label: "错误",
                color: summary.errorRequests > 0 ? .red : .secondary
            )
        }
    }
}

// MARK: - 模型排行

private struct ModelRankingSection: View {
    let models: [GroupStat]
    /// 用全量请求数作分母，占比才是「占今天所有请求的比例」，而非仅占排行榜内的比例。
    let totalRequests: Int64

    var body: some View {
        VStack(spacing: 2) {
            ForEach(models.prefix(3)) { model in
                ModelBar(name: model.name, percentage: percentage(of: model))
            }
        }
    }

    private func percentage(of model: GroupStat) -> Double {
        guard totalRequests > 0 else { return 0 }
        return Double(model.requests) / Double(totalRequests)
    }
}

// MARK: - 渠道余额（单行）

private struct ChannelBalanceRow: View {
    let balances: [ChannelBalance]

    /// 只列真的查到余额的渠道：查不到余额的项只有一个灰点加名字，
    /// 既占宽度又不带信息，反而把有效项挤到被截断。
    private var meaningful: [ChannelBalance] {
        balances.filter { $0.balance != nil }
    }

    var body: some View {
        if !meaningful.isEmpty {
            HStack(spacing: 10) {
                Text("余额")
                    .font(WD.captionFont)
                    .foregroundStyle(.tertiary)
                    .layoutPriority(2)

                ForEach(meaningful.prefix(3)) { balance in
                    ChannelChip(balance: balance)
                }

                Spacer(minLength: 0)
            }
            .frame(height: 13)
        }
    }
}

// MARK: - 预览

#Preview("Large Widget", as: .systemLarge) {
    XTokenHubWidget()
} timeline: {
    XTokenHubEntry.placeholder()
}

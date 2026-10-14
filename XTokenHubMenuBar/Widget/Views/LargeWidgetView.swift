import SwiftUI
import WidgetKit

/// Large (358×358，约 326pt 可用高) — 完整概览。
///
/// 垂直预算：标题 13 + 指标宫格 78 + 趋势标题 13 + 趋势图（弹性 40～150）
/// + 排行标题 13 + 排行 39 + 余额 13（勾选超过 2 个渠道时两行 44），加间隙共约 196～273pt；
/// 余量全部给趋势图，内容多时趋势图让位（见下方 minHeight 注释），因此不会被挤出边界。
///
/// 余额另有**水平**预算：单行 326pt 只放得下 2 个长名渠道，所以按每行 2 个换行，
/// 溢出的用 `+N` 标注；chip 不加 `layoutPriority`，宽度不够时先截渠道名、金额保完整。
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

                // 峰谷状态：有配置时段价的模型才出现（单行 13pt + 间隙 9pt，趋势图弹性吸收）
                if let lead = entry.peakLead(at: entry.date) {
                    gap()
                    PeakStatusRow(
                        lead: lead,
                        date: entry.date,
                        showsModel: true,
                        extraCount: entry.peakPrices.count - 1
                    )
                }

                gap()

                SectionLabel(text: "Token 用量趋势（24 小时）")
                Spacer().frame(height: WD.labelGap)
                // 弹性填充剩余空间(上限 150 防止无模型/无余额时拉成巨幅色块)。
                // 下限 40 而非 70:勾选渠道多时余额占两行,趋势图要能把高度让出来,
                // 否则固定内容 273 + 70 = 343pt > 326pt,底部会被静默裁掉
                MiniTrendChart(values: entry.trendValues)
                    .frame(minHeight: 40, maxHeight: 150)

                if !entry.modelTop.isEmpty {
                    gap()
                    SectionLabel(text: "模型使用排行")
                    Spacer().frame(height: WD.labelGap)
                    ModelRankingSection(
                        models: entry.modelTop,
                        totalRequests: summary.totalRequests
                    )
                }

                // 余额：只显示设置里勾选的渠道，按 id 排序；每行 2 个、最多 2 行，溢出数并进标签
                let (balanceRows, balanceOverflow) = entry.balanceRows()
                if !balanceRows.isEmpty {
                    gap()
                    ForEach(Array(balanceRows.enumerated()), id: \.offset) { index, balances in
                        ChannelBalanceRow(
                            balances: balances,
                            extraCount: index == balanceRows.count - 1 ? balanceOverflow : 0
                        )
                        if index < balanceRows.count - 1 {
                            gap()
                        }
                    }
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

/// 余额单行：标签 + 最多几个 chip + 溢出数。
///
/// `internal` 而非 `private`：Tests/Widget 会拿它做**横向不溢出**断言
/// （macOS 上越界是静默裁掉，高度测试发现不了）。
struct ChannelBalanceRow: View {
    /// 已由 `entry.balanceCandidates` 过滤排序：未勾选的渠道不会进来。
    let balances: [ChannelBalance]
    /// 本行之外还有多少个勾选渠道没显示（单行宽度有限），并进标签显示为 `余额+N`。
    var extraCount = 0

    var body: some View {
        if !balances.isEmpty {
            HStack(spacing: 10) {
                Text(extraCount > 0 ? "余额+\(extraCount)" : "余额")
                    .font(WD.captionFont)
                    .foregroundStyle(.tertiary)
                    .lineLimit(1)
                    .layoutPriority(2)

                ForEach(balances) { balance in
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

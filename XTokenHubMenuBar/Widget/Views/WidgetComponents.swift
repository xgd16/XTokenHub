import SwiftUI
import WidgetKit

/// Widget 设计系统。
///
/// macOS 上 `.caption` 与 `.caption2` 实际都是 10pt，字号档位比 iOS 稀疏，
/// 所以这里用显式尺寸而不是语义字号，保证 358×358 / 358×170 / 170×170
/// 三种尺寸下的垂直预算都可预算、不溢出。
enum WD {
    // MARK: 间距

    /// 分区间距。
    static let sectionGap: CGFloat = 9
    /// 区块内标签与内容的间距。
    static let labelGap: CGFloat = 4
    /// 内容区统一内边距（Widget 自带约 16pt 系统内边距，这里再留一点呼吸）。
    static let inset: CGFloat = 2

    // MARK: 字号

    /// 分区小标题。
    static let labelFont = Font.system(size: 10, weight: .medium)
    /// 次要说明文字。
    static let captionFont = Font.system(size: 10)
    /// 正文数值。
    static let valueFont = Font.system(size: 12, weight: .medium)
    /// 磁贴数值。
    static let tileValueFont = Font.system(size: 12, weight: .semibold)
    /// 主视觉大数字。
    static func heroFont(_ size: CGFloat = 30) -> Font {
        .system(size: size, weight: .bold)
    }

    static let hairline = Color.primary.opacity(0.08)
    static let tileFill = Color.primary.opacity(0.05)
}

// MARK: - 发丝分隔线

struct WidgetHairline: View {
    var vertical = false

    var body: some View {
        Rectangle()
            .fill(WD.hairline)
            .frame(width: vertical ? 0.5 : nil, height: vertical ? nil : 0.5)
    }
}

// MARK: - 连接状态指示器

struct ConnectionIndicator: View {
    let isConnected: Bool

    var body: some View {
        Circle()
            .fill(isConnected ? Color.green : Color.secondary.opacity(0.5))
            .frame(width: 6, height: 6)
    }
}

// MARK: - 数据来源标题

/// 一行标题：状态点 + 来源名（可带右侧附着信息）。
struct SourceHeader: View {
    let sourceName: String
    let isConnected: Bool
    var trailing: String?

    var body: some View {
        HStack(spacing: 5) {
            ConnectionIndicator(isConnected: isConnected)

            Text(sourceName)
                .font(WD.labelFont)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .truncationMode(.middle)

            Spacer(minLength: 4)

            if let trailing {
                Text(trailing)
                    .font(WD.captionFont)
                    .foregroundStyle(.tertiary)
                    .lineLimit(1)
            }
        }
    }
}

// MARK: - 大数字（数量级单位排成次级字号）

/// `1.43亿` 拆成 `1.43` + `亿`，单位用更小字号，避免整串数字抢走视觉重心。
struct CompactNumber: View {
    let value: Int64
    var font: Font
    var unitStyle: Font

    init(value: Int64, font: Font, unitStyle: Font) {
        self.value = value
        self.font = font
        self.unitStyle = unitStyle
    }

    var body: some View {
        let parts = TokenFormatter.splitCompact(value)
        HStack(alignment: .firstTextBaseline, spacing: 1) {
            Text(parts.number)
                .font(font)
                .monospacedDigit()
                .lineLimit(1)
                .minimumScaleFactor(0.6)
            if !parts.unit.isEmpty {
                Text(parts.unit)
                    .font(unitStyle)
                    .foregroundStyle(.secondary)
            }
        }
    }
}

// MARK: - 迷你趋势图

/// 0 基线面积图。基线取 0 而不是 min-max：量级差异才能如实呈现，
/// 否则一天的平稳波动会被拉伸成剧烈起伏。
struct MiniTrendChart: View {
    let values: [Double]

    var body: some View {
        GeometryReader { geometry in
            if values.count > 1 {
                let maxValue = max(values.max() ?? 0, 1)
                let stepX = geometry.size.width / CGFloat(values.count - 1)
                let height = geometry.size.height

                let points: [CGPoint] = values.enumerated().map { index, value in
                    let ratio = max(0, min(1, value / maxValue))
                    return CGPoint(x: CGFloat(index) * stepX, y: height * (1 - ratio))
                }

                ZStack {
                    Path { path in
                        guard let first = points.first, let last = points.last else { return }
                        path.move(to: CGPoint(x: first.x, y: height))
                        path.addLine(to: first)
                        points.dropFirst().forEach { path.addLine(to: $0) }
                        path.addLine(to: CGPoint(x: last.x, y: height))
                        path.closeSubpath()
                    }
                    .fill(
                        LinearGradient(
                            colors: [Color.accentColor.opacity(0.30), Color.accentColor.opacity(0.02)],
                            startPoint: .top,
                            endPoint: .bottom
                        )
                    )

                    Path { path in
                        guard let first = points.first else { return }
                        path.move(to: first)
                        points.dropFirst().forEach { path.addLine(to: $0) }
                    }
                    .stroke(Color.accentColor, style: StrokeStyle(lineWidth: 1.3, lineJoin: .round))
                }
            }
        }
    }
}

// MARK: - 分区标题

struct SectionLabel: View {
    let text: String

    var body: some View {
        Text(text)
            .font(WD.labelFont)
            .foregroundStyle(.secondary)
            .lineLimit(1)
    }
}

// MARK: - 标签 / 数值 一行

struct StatRow: View {
    let label: String
    let value: String

    var body: some View {
        HStack(spacing: 6) {
            Text(label)
                .font(WD.captionFont)
                .foregroundStyle(.secondary)
                .lineLimit(1)

            Spacer(minLength: 4)

            Text(value)
                .font(WD.valueFont)
                .monospacedDigit()
                .lineLimit(1)
                .minimumScaleFactor(0.7)
        }
    }
}

// MARK: - 紧凑指标（图标 + 数值 / 标签）

struct MetricPill: View {
    let icon: String
    let value: String
    let label: String
    let color: Color

    var body: some View {
        VStack(spacing: 2) {
            HStack(spacing: 4) {
                Image(systemName: icon)
                    .font(.system(size: 10))
                    .foregroundStyle(color)
                Text(value)
                    .font(WD.tileValueFont)
                    .monospacedDigit()
                    .lineLimit(1)
                    .minimumScaleFactor(0.7)
            }
            Text(label)
                .font(WD.captionFont)
                .foregroundStyle(.tertiary)
                .lineLimit(1)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 5)
        .background(WD.tileFill, in: RoundedRectangle(cornerRadius: 6))
    }
}

// MARK: - 图标 + 数值 / 标签（竖排，小尺寸用）

struct MetricStack: View {
    let icon: String
    let value: String
    let label: String

    var body: some View {
        VStack(spacing: 1) {
            HStack(spacing: 3) {
                Image(systemName: icon)
                    .font(.system(size: 9))
                    .foregroundStyle(Color.accentColor)
                Text(value)
                    .font(WD.valueFont)
                    .monospacedDigit()
                    .lineLimit(1)
                    .minimumScaleFactor(0.7)
            }
            Text(label)
                .font(WD.captionFont)
                .foregroundStyle(.tertiary)
                .lineLimit(1)
        }
        .frame(maxWidth: .infinity)
    }
}

// MARK: - 模型排行条

/// 排行条。名称列随容器比例分配，避免固定宽度把长模型名截成无信息的前缀。
struct ModelBar: View {
    let name: String
    let percentage: Double
    let color: Color

    init(name: String, percentage: Double, color: Color = .accentColor) {
        self.name = name
        self.percentage = percentage
        self.color = color
    }

    var body: some View {
        HStack(spacing: 6) {
            Text(name)
                .font(WD.captionFont)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .truncationMode(.middle)
                .layoutPriority(1)

            GeometryReader { geometry in
                ZStack(alignment: .leading) {
                    Capsule()
                        .fill(color.opacity(0.16))
                    Capsule()
                        .fill(color)
                        .frame(width: max(2, geometry.size.width * min(1, percentage)))
                }
            }
            .frame(height: 5)
            .frame(minWidth: 40)

            Text(String(format: "%.0f%%", percentage * 100))
                .font(WD.captionFont)
                .foregroundStyle(.tertiary)
                .monospacedDigit()
                .lineLimit(1)
        }
        .frame(height: 13)
    }
}

// MARK: - 渠道余额（单行 chip，尽量少占垂直空间）

struct ChannelChip: View {
    let balance: ChannelBalance

    var body: some View {
        HStack(spacing: 3) {
            Circle()
                .fill(balance.ok ? Color.green : Color.secondary.opacity(0.4))
                .frame(width: 5, height: 5)

            Text(balance.channelName)
                .font(WD.captionFont)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                // 尾部截断：渠道名多以后缀区分（…-Claude / …-Go），中间截断会只剩前后各三个字母
                .truncationMode(.tail)

            if let info = balance.balance {
                // 余额是上游账户的真实币种，按原币种展示、不做汇率折算（与主 App 面板同口径）
                Text(MoneyFormatter.formatBalance(info.total, currency: info.currency))
                    .font(WD.captionFont)
                    .fontWeight(.medium)
                    .monospacedDigit()
                    .lineLimit(1)
            }
        }
        .layoutPriority(1)
    }
}

// MARK: - 更新时间

struct TimestampView: View {
    let date: Date

    var body: some View {
        Text(date.formatted(date: .omitted, time: .shortened))
            .font(WD.captionFont)
            .foregroundStyle(.tertiary)
            .monospacedDigit()
    }
}

// MARK: - 空状态

/// 连不上服务时的引导。
///
/// Widget 与主 App 无法共享配置容器（ad-hoc 签名下 App Groups 不可用），
/// 正常情况下由主 App 在本机回环端点供数，取不到时才需要手填地址。
struct NoDataView: View {
    var body: some View {
        VStack(spacing: 5) {
            Image(systemName: "antenna.radiowaves.left.and.right.slash")
                .font(.system(size: 18))
                .foregroundStyle(.tertiary)

            Text("无法连接 XTokenHub")
                .font(WD.labelFont)
                .fontWeight(.medium)

            Text("请确认主 App 正在运行")
                .font(WD.captionFont)
                .foregroundStyle(.tertiary)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

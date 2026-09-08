import SwiftUI
import Charts

/// 通用条形行(模型 TOP / 调用方 TOP 共用)。
struct UsageBarRow: View {
    let name: String
    let tokens: Int64
    let requests: Int64
    let fraction: Double

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(name)
                    .font(.callout.weight(.medium))
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer(minLength: 8)
                Text("\(TokenFormatter.compact(tokens)) · \(requests) 次")
                    .font(.caption2.monospacedDigit())
                    .foregroundStyle(.secondary)
            }
            GeometryReader { geo in
                ZStack(alignment: .leading) {
                    Capsule().fill(Color.primary.opacity(0.08))
                    Capsule()
                        .fill(LinearGradient(colors: [.accentColor, .cyan], startPoint: .leading, endPoint: .trailing))
                        .frame(width: max(4, geo.size.width * fraction))
                }
            }
            .frame(height: 5)
        }
    }
}

/// Token 趋势迷你图(可选高度)。
struct TrendSparkline: View {
    let points: [TrendPoint]
    var height: CGFloat = 88

    var body: some View {
        Chart(points) { point in
            AreaMark(
                x: .value("时间", point.pointDate),
                y: .value("Tokens", point.totalTokens)
            )
            .interpolationMethod(.catmullRom)
            .foregroundStyle(
                .linearGradient(
                    colors: [Color.accentColor.opacity(0.45), Color.accentColor.opacity(0.02)],
                    startPoint: .top,
                    endPoint: .bottom
                )
            )
            LineMark(
                x: .value("时间", point.pointDate),
                y: .value("Tokens", point.totalTokens)
            )
            .interpolationMethod(.catmullRom)
            .foregroundStyle(Color.accentColor)
            .lineStyle(StrokeStyle(lineWidth: 1.5))
        }
        .chartXAxis(.hidden)
        .chartYAxis {
            AxisMarks(position: .trailing, values: .automatic(desiredCount: 3)) { value in
                AxisValueLabel {
                    if let v = value.as(Int64.self) {
                        Text(TokenFormatter.compact(v))
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        }
        .frame(height: height)
    }
}

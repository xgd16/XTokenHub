import SwiftUI

/// 统计磁贴:图标 + 数值 + 标题,交互式玻璃材质;help 为悬停说明。
struct StatTile: View {
    let title: String
    let value: String
    let icon: String
    var tint: Color = .accentColor
    var help: String = ""

    var body: some View {
        VStack(spacing: 5) {
            Image(systemName: icon)
                .font(.footnote.weight(.semibold))
                .foregroundStyle(tint)
            Text(value)
                .font(.title3.weight(.semibold).monospacedDigit())
                .lineLimit(1)
                .minimumScaleFactor(0.5)
                .contentTransition(.numericText())
            Text(title)
                .font(.caption2)
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 8)
        .frame(maxWidth: .infinity)
        .glassEffect(.regular.interactive(), in: .rect(cornerRadius: 10))
        .help(help)
    }
}

import SwiftUI

/// 单个统计单元的展示数据。数值与数量级后缀(万/亿)分开,便于把单位排成次级字号。
struct StatItem {
    var title: String
    var value: String
    var unit: String = ""
    var icon: String
    var tint: Color = .accentColor
    var help: String = ""
}

/// 统计九宫格:同一块液态玻璃内的网格,格间以发丝线分隔。
///
/// 关键取舍:九宫格是**一块**玻璃 + 内部发丝线,不是九个各自套玻璃的小磁贴。
/// 九个玻璃形状密排在 10pt 间距里时,各自那圈镜面硬边会互相挤压,亮色背景下
/// 呈现为硬边与脏灰缝 —— 这正是之前「阴影很奇怪」的根因。合并成一块大面后,
/// 玻璃的折射与高光落在整片上(与 macOS 控制中心的模块一致),数字成为主角。
struct StatGrid: View {
    let cells: [StatItem]
    var columns: Int = 3
    /// 玻璃档位:菜单栏面板内用默认 `.clear`(下面已有系统玻璃);
    /// 悬浮卡片自身就是唯一一层玻璃,传 `.regular` 保证壁纸上的可读性。
    var variant: Glass = .clear
    /// 悬浮卡片把标题栏和网格包在**同一块**玻璃里,网格自身不再套第二层玻璃
    /// (两层玻璃各自提亮一次会把卡片糊成毛玻璃)。
    var appliesSurface: Bool = true

    private var rows: [[StatItem]] {
        stride(from: 0, to: cells.count, by: columns).map {
            Array(cells[$0 ..< min($0 + columns, cells.count)])
        }
    }

    @ViewBuilder
    var body: some View {
        if appliesSurface {
            grid.panelSurface(variant: variant)
        } else {
            grid
        }
    }

    private var grid: some View {
        VStack(spacing: 0) {
            ForEach(Array(rows.enumerated()), id: \.offset) { rowIndex, row in
                if rowIndex > 0 {
                    PanelHairline()
                        .padding(.horizontal, 12)
                }
                HStack(spacing: 0) {
                    ForEach(Array(row.enumerated()), id: \.offset) { columnIndex, item in
                        if columnIndex > 0 {
                            PanelHairline(axis: .vertical)
                                .padding(.vertical, 10)
                        }
                        StatCell(item: item)
                    }
                }
            }
        }
    }
}

/// 统计单元:数值(主)+ 数量级(次)+ 图标与标题(次)。
struct StatCell: View {
    let item: StatItem

    var body: some View {
        VStack(spacing: 5) {
            HStack(alignment: .firstTextBaseline, spacing: 1) {
                Text(item.value)
                    .font(.title3.weight(.semibold).monospacedDigit())
                if !item.unit.isEmpty {
                    Text(item.unit)
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(.secondary)
                }
            }
            .lineLimit(1)
            .minimumScaleFactor(0.6)
            .contentTransition(.numericText())

            HStack(spacing: 3) {
                Image(systemName: item.icon)
                    .font(.system(size: 9, weight: .semibold))
                    .foregroundStyle(item.tint)
                Text(item.title)
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
        }
        .padding(.vertical, 10)
        .padding(.horizontal, 6)
        .frame(maxWidth: .infinity)
        .help(item.help)
    }
}

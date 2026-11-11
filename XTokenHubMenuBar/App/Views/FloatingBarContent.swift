import AppKit
import SwiftUI

/// 单行条状浮窗:状态点 + 指标串 + 活跃流 + 展开按钮,高度约 32pt。
///
/// 指标沿用浮窗勾选,格式化与菜单栏完全一致(`SurfaceMetrics.parts`),
/// 只是把同一串数字包成一条贴合菜单栏的胶囊,尽量少占竖向空间。
struct FloatingBarContent: View {
    let snapshot: SurfaceMetricSnapshot
    let parts: [String]
    let style: AppSettings.FloatingStyle
    let onToggleDashboard: () -> Void
    let onOpenDashboard: () -> Void
    let onToggleStyle: () -> Void
    let onOpenSettings: () -> Void
    let onResetPosition: () -> Void
    let onHide: () -> Void

    /// 玻璃外的透明留白。
    static let outerPadding: CGFloat = 4
    static let cornerRadius: CGFloat = 14

    var body: some View {
        HStack(spacing: 6) {
            leading
            Button(action: onToggleDashboard) {
                Image(systemName: "rectangle.expand.vertical")
                    .font(.system(size: 9, weight: .semibold))
                    .foregroundStyle(.secondary)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .help("打开 / 收起完整面板")
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
        .panelSurface(cornerRadius: Self.cornerRadius, variant: .regular)
        .padding(Self.outerPadding)
        .gesture(WindowDragGesture())
        .onTapGesture { onToggleDashboard() }
        .contextMenu {
            Button("打开完整面板", systemImage: "rectangle.expand.vertical") { onOpenDashboard() }
            Button(style.switchActionTitle, systemImage: style.switchActionSymbol) { onToggleStyle() }
            Button("设置…", systemImage: "gearshape") { onOpenSettings() }
            Divider()
            Button("恢复浮窗默认位置", systemImage: "arrow.uturn.backward") { onResetPosition() }
            Button("隐藏悬浮窗", systemImage: "eye.slash") { onHide() }
        }
    }

    /// 状态点 + 指标串:整块叠 `WindowDragHandle`,拖动移动、单击切换面板。
    private var leading: some View {
        HStack(spacing: 6) {
            Circle()
                .fill(snapshot.isOnline ? Color.green : Color.red)
                .frame(width: 6, height: 6)

            if parts.isEmpty {
                Text("未选择指标")
                    .font(.system(size: 11, weight: .medium))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .fixedSize(horizontal: true, vertical: false)
            } else {
                Text(parts.joined(separator: " · "))
                    .font(.system(size: 11, weight: .medium, design: .monospaced))
                    .lineLimit(1)
                    // 条状窗口宽度由内容决定:不加 fixedSize 时,宿主还是旧宽度会把 Text
                    // 压缩并回报截断后的宽度,窗口就会越量越窄、最后一个指标永远被省略。
                    .fixedSize(horizontal: true, vertical: false)
            }

            if snapshot.activeStreams > 0 {
                Label("\(snapshot.activeStreams)", systemImage: "waveform")
                    .font(.system(size: 9, weight: .medium))
                    .foregroundStyle(.orange)
                    .lineLimit(1)
            }
        }
        .padding(.vertical, 1)
        // 确定可用的拖动手柄:NSHostingView 默认不把鼠标事件交给窗口背景拖拽。
        // 拖动结束若位置没变,按单击处理(切换完整面板)。
        .overlay {
            WindowDragHandle(onTap: onToggleDashboard)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }
}

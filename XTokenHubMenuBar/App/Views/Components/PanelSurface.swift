import SwiftUI

/// 面板分区的统一材质:**系统 Liquid Glass**(`glassEffect`),对齐 macOS/iOS 控制中心那种
/// 「模块彼此独立、间隔留出背景、玻璃透出下层」的观感。
///
/// 之前「阴影很奇怪」的根因不是玻璃本身,而是九个统计磁贴**各自**套玻璃、只隔 10pt 排成网格:
/// 每个玻璃形状都会画自己那圈镜面硬边,窄缝里相邻硬边互相挤压,浅色背景上就变成一圈脏灰边;
/// 而 `GlassEffectContainer(spacing: 10)` 的融合阈值恰好等于 10pt 网格间距,导致边缘半融合、更不可控。
///
/// 因此约定三条,材质仍是液态玻璃:
/// 1. **小区块合并成一张大面**:九宫格 = 一块玻璃 + 内部发丝线分格(见 `StatGrid`)。
///    就像控制中心里「显示器 / 声音」是单个宽胶囊内含多行,而不是每行一块玻璃。
/// 2. **分区间距(16)> `GlassEffectContainer` 的 spacing(8)**,相邻玻璃不半融合,各留完整玻璃边,
///    间隔里透出下层 —— 这是控制中心的节奏。
/// 3. **面板内不加任何投影、不加不透明底色**:玻璃的折射和镜面高光自己就是层次,
///    再加填充或 `.shadow` 只会把玻璃糊成毛玻璃。
///
/// 想更透:把 `variant` 换成 `.clear`(需要自行处理文字可读性);想更实:.regular 已是系统默认档。
struct PanelSurfaceStyle: ViewModifier {
    var cornerRadius: CGFloat = 20

    /// 玻璃档位。`.clear` 是文档里更透的一档(代价是 Apple 要求自行保证文字可读性);
    /// `.regular` 是默认可读性优先档,会主动加底衬,所以天生偏实。
    /// 注意:面板自身已是一层系统玻璃,这里再叠一层 —— 两层各自提亮一次,是「看着像毛玻璃」的主因。
    /// 系统设置 → 外观 → Liquid Glass 的「清透 / 着色」也会整体调节这两档的透明度。
    private var variant: Glass { .clear }

    func body(content: Content) -> some View {
        content.glassEffect(variant, in: .rect(cornerRadius: cornerRadius))
    }
}

extension View {
    /// 套用面板统一的液态玻璃材质。
    func panelSurface(cornerRadius: CGFloat = 20) -> some View {
        modifier(PanelSurfaceStyle(cornerRadius: cornerRadius))
    }
}

/// 面板分区:标题 + 内容,整块液态玻璃。
struct PanelSection<Content: View>: View {
    var title: String?
    var content: Content

    init(title: String? = nil, @ViewBuilder content: () -> Content) {
        self.title = title
        self.content = content()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            if let title {
                Text(title)
                    .font(.caption.weight(.semibold))
                    .tracking(0.3)
                    .foregroundStyle(.secondary)
                    .textCase(.uppercase)
            }
            content
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 11)
        .frame(maxWidth: .infinity, alignment: .leading)
        .panelSurface()
    }
}

/// 玻璃面内部的发丝分隔线,用于网格与分组列表。
///
/// 用 `Color.primary` 而不是写死黑白:面板玻璃浮在任意壁纸上,亮色模式下玻璃偏亮、
/// `primary` 即黑;暗色模式下玻璃偏暗、`primary` 即白,方向天然正确。
struct PanelHairline: View {
    var axis: Axis = .horizontal

    var body: some View {
        Rectangle()
            .fill(Color.primary.opacity(0.10))
            .frame(
                width: axis == .vertical ? 0.5 : nil,
                height: axis == .horizontal ? 0.5 : nil
            )
    }
}

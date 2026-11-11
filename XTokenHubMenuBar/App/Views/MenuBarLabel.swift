import SwiftUI

/// 菜单栏标签:按设置的指标组合渲染,格式如 `482 · 3214.45万 · 96.0% · 12/s`。
/// 数字只用「数字 + 万/亿」单位,不带 tok 后缀;全部取消勾选时回退为图标。
/// 数值读取发生在本视图 body 中,@Observable 依赖跟踪会驱动标签自动刷新。
struct MenuBarLabel: View {
    let store: HubStore
    let settings: AppSettings

    var body: some View {
        let snapshot = SurfaceMetricSnapshot(store: store, settings: settings)
        let parts = SurfaceMetrics.parts(snapshot, metrics: settings.menuBarMetrics)
        let symbol = SurfaceMetrics.symbol(snapshot)
        if parts.isEmpty {
            Image(systemName: symbol)
        } else if settings.menuBarMetrics.contains(.icon) {
            Label(parts.joined(separator: " · "), systemImage: symbol)
        } else {
            Text(parts.joined(separator: " · "))
        }
    }
}

import Foundation

/// 主 App 通过本机回环桥接服务交给 Widget 的数据载荷。
///
/// 因为无开发者账号、无法使用 App Groups（见 Shared/WidgetDataProvider.swift 说明），
/// 主 App 在 127.0.0.1 上暴露一个只读端点，Widget 从那里取数：
/// 既能拿到当前选中的数据来源地址，也能直接复用主 App 已经算好的统计结果。
struct WidgetBridgePayload: Codable, Sendable {
    /// 主 App 当前选中的数据来源地址（Widget 用它兜底自行请求趋势等数据）。
    var baseURL: String
    /// 数据来源显示名。
    var sourceName: String
    var summary: Summary?
    var costToday: CostForecast?
    var modelTop: [GroupStat]
    var channelBalances: [ChannelBalance]
    /// 计费展示设置：花费按这个币种/汇率换算后展示（与主 App 面板同口径）。
    var billingCurrency: String
    var billingRate: Double
    /// 主 App 生成该载荷的时间。
    var generatedAt: Date
}

/// 桥接服务的监听端口（仅绑定回环地址）。
enum WidgetBridge {
    static let port: UInt16 = 9193
    static let snapshotPath = "/snapshot"
    static var url: URL { URL(string: "http://127.0.0.1:\(port)\(snapshotPath)")! }
}

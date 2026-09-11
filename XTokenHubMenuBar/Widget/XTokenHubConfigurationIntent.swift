import AppIntents
import Foundation

/// Widget 配置。
///
/// 正常情况下**无需配置**：主 App 运行时会在本机暴露桥接端点，
/// Widget 自动取到主 App 当前选中的数据来源。
///
/// 这个字段只在主 App 未运行时兜底使用（或想固定监控某个特定服务实例时覆盖）。
///
/// 注意：`WidgetConfigurationIntent` 要求所有参数必须是可选类型，不能给非可选默认值。
struct XTokenHubConfigurationIntent: WidgetConfigurationIntent {
    static let title: LocalizedStringResource = "XTokenHub"
    static let description = IntentDescription("留空即可跟随主 App 当前的数据来源；主 App 未运行时才需要填写服务地址。")

    @Parameter(title: "备选服务地址", description: "例如 http://192.168.1.110:9192，留空则只用主 App 的数据")
    var sourceURL: String?

    static var parameterSummary: some ParameterSummary {
        Summary("备选服务地址 \(\.$sourceURL)")
    }
}

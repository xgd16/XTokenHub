import Foundation
import UserNotifications

/// 峰谷切换通知调度器:为每个配置了时段价的模型预排未来 2 次切换的本地通知。
///
/// - 双向都提醒(转高峰 / 转空闲),每模型一条,正文带生效费率,转空闲附折扣;
/// - 每次价目刷新后整批重排:先撤销上一轮(已调度标识符清单持久化,跨启动也能撤销),
///   再按「模型 + 序号」的固定标识符添加;
/// - 本地通知只能由主 App 发(Widget 无 App Groups,不能代发),主 App 常驻时生效。
@MainActor
final class PeakNotifier {
    /// 通知标识前缀与已调度清单的存储键(系统不提供枚举待发通知,撤销依赖自己的清单)。
    private static let idPrefix = "peak."
    private static let idsKey = "peakNotifyScheduledIDs"

    private let center = UNUserNotificationCenter.current()

    /// 请求通知授权;返回错误文案(nil = 成功或此前已授权),设置页展示。
    func requestAuthorizationIfNeeded() async -> String? {
        do {
            let granted = try await center.requestAuthorization(options: [.alert])
            if granted { return nil }
            let status = await center.notificationSettings().authorizationStatus
            if status == .authorized || status == .provisional { return nil }
            return "系统通知已被拒绝,请在 系统设置 → 通知 中开启本应用"
        } catch {
            return error.localizedDescription
        }
    }

    /// 撤销全部峰谷待发通知(开关关闭/价目为空)。
    func cancelAll() async {
        let ids = UserDefaults.standard.stringArray(forKey: Self.idsKey) ?? []
        if !ids.isEmpty {
            center.removePendingNotificationRequests(withIdentifiers: ids)
        }
        UserDefaults.standard.set([String](), forKey: Self.idsKey)
    }

    /// 按价目重排通知(幂等)。开关开启时由 HubStore 在每次价目刷新后调用。
    func reschedule(_ prices: [ModelPrice]) async {
        await cancelAll()
        guard !prices.isEmpty else { return }

        var ids: [String] = []
        for price in prices {
            guard let rule = price.peakRule else { continue }
            // 未来 2 次切换:足以覆盖一个工作日的午休/收盘两跳,重排前不会耗尽
            var cursor = Date()
            for index in 0..<2 {
                guard let next = rule.nextSwitch(after: cursor) else { break }
                let content = UNMutableNotificationContent()
                let text = Self.notificationContent(price: price, switchingTo: next.to, at: next.at)
                content.title = text.title
                content.body = text.body
                content.threadIdentifier = Self.idPrefix + price.model

                let id = "\(Self.idPrefix)\(price.model).\(index)"
                let trigger = UNTimeIntervalNotificationTrigger(
                    timeInterval: max(1, next.at.timeIntervalSinceNow),
                    repeats: false
                )
                do {
                    try await center.add(UNNotificationRequest(identifier: id, content: content, trigger: trigger))
                    ids.append(id)
                } catch {
                    // 单条失败不阻断其余模型
                }
                cursor = next.at
            }
        }
        UserDefaults.standard.set(ids, forKey: Self.idsKey)
    }

    /// 通知文案(纯函数,单测覆盖)。费率按切换时刻的生效口径计算(含空闲回退)。
    nonisolated static func notificationContent(price: ModelPrice, switchingTo period: PricePeriod, at date: Date) -> (title: String, body: String) {
        let title = "\(price.model) 转入\(period.label)"
        let rates = price.effectiveRates(at: date)
        var body = "输入 \(MoneyFormatter.formatPerMillion(perToken: rates.input, currency: price.displayCurrency))/M"
            + " · 输出 \(MoneyFormatter.formatPerMillion(perToken: rates.output, currency: price.displayCurrency))/M"
        if period == .offPeak, let discount = price.offPeakDiscountPercent {
            body += "(省 \(discount)%)"
        }
        return (title, body)
    }
}

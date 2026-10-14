import Foundation
import Testing

/// 小组件余额渠道**多选**：只有勾选的渠道才会出现在小组件上。
struct BalanceSelectionTests {
    // MARK: 夹具

    private static func balance(_ id: Int64, name: String, hasValue: Bool) -> ChannelBalance {
        ChannelBalance(
            channelID: id,
            channelName: name,
            provider: "deepseek",
            supported: hasValue,
            ok: hasValue,
            balance: hasValue
                ? BalanceInfo(
                    provider: "deepseek",
                    isAvailable: true,
                    currency: "CNY",
                    total: Double(id) * 100,
                    granted: 0,
                    toppedUp: 100,
                    fetchedAt: ""
                )
                : nil,
            error: hasValue ? nil : "unsupported",
            fetchedAt: nil
        )
    }

    /// 故意乱序 + 混入无余额渠道，验证过滤与排序。
    private static let balances: [ChannelBalance] = [
        balance(9, name: "Claude-转发", hasValue: true),
        balance(2, name: "OpenAI-转发", hasValue: false),
        balance(5, name: "DeepSeek-官方", hasValue: true),
    ]

    private func entry(checked: [Int64]?, balances: [ChannelBalance]) -> XTokenHubEntry {
        var snapshot = WidgetSnapshotFactory.sample
        snapshot.channelBalances = balances
        snapshot.balanceChannelIDs = checked
        return XTokenHubEntry(date: Date(), snapshot: snapshot)
    }

    // MARK: 未设置（默认：全部显示）

    @Test func unsetShowsAllChannelsWithBalanceInStableOrder() {
        let entry = self.entry(checked: nil, balances: Self.balances)
        // 来自字典顺序不稳:按 channel_id 排序,且只留真的查到余额的
        #expect(entry.balanceCandidates.map(\.channelID) == [5, 9])
        #expect(entry.featuredBalance?.channelID == 5)
    }

    // MARK: 勾选 = 白名单

    @Test func onlyCheckedChannelsAreShown() {
        let entry = self.entry(checked: [9], balances: Self.balances)
        // 5 号也有余额,但没勾 —— 不能出现
        #expect(entry.balanceCandidates.map(\.channelID) == [9])
    }

    @Test func checkedChannelsOrderedByID() {
        let entry = self.entry(checked: [9, 5], balances: Self.balances)
        #expect(entry.balanceCandidates.map(\.channelID) == [5, 9])
        #expect(entry.featuredBalance?.channelID == 5)
    }

    @Test func uncheckedChannelsWithoutBalanceNeverAppear() {
        let entry = self.entry(checked: [5], balances: Self.balances)
        #expect(entry.balanceCandidates.map(\.channelID) == [5])
    }

    @Test func checkingNothingShowsNoBalance() {
        let entry = self.entry(checked: [], balances: Self.balances)
        #expect(entry.balanceCandidates.isEmpty)
        #expect(entry.featuredBalance == nil)
    }

    /// 勾了却查不到余额：仍要出现（名字 + `—`），否则用户以为没勾上；但排在有余额的后面。
    @Test func checkedChannelWithoutBalanceIsKeptLast() {
        let entry = self.entry(checked: [2, 5], balances: Self.balances)
        #expect(entry.balanceCandidates.map(\.channelID) == [5, 2])
        #expect(entry.balanceCandidates.last?.balance == nil)
    }

    @Test func checkedChannelMissingFromPayloadIsSkipped() {
        // 勾选集合里有、但当前数据来源没有的渠道:直接略过,不占展示位
        let entry = self.entry(checked: [404, 5], balances: Self.balances)
        #expect(entry.balanceCandidates.map(\.channelID) == [5])
    }
}

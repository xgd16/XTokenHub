import Foundation
import Observation
import WidgetKit

/// 面板核心状态:REST 拉取 + WS 实时事件,全部在主线程。
@MainActor
@Observable
final class HubStore {
    enum ConnectionState: Equatable {
        case connecting
        case connected
        case disconnected(String)

        var isOnline: Bool { self == .connected }
    }

    /// 趋势图时间范围(与 Web 仪表盘切换档位一致)。
    enum TrendRange: String, CaseIterable, Identifiable {
        case live
        case day
        case week
        case month

        var id: String { rawValue }

        var label: String {
            switch self {
            case .live: "实时"
            case .day: "24h"
            case .week: "7天"
            case .month: "30天"
            }
        }
    }

    // MARK: 观察状态

    private(set) var connection: ConnectionState = .connecting
    private(set) var summary: Summary?
    private(set) var lifetime: Lifetime?
    private(set) var trend: [TrendPoint] = []
    private(set) var trendRange: TrendRange = .live
    private(set) var modelTop: [GroupStat] = []
    private(set) var callersTop: [GroupStat] = []
    /// 接入渠道(用于展示启停状态与余额)。
    private(set) var channels: [Channel] = []
    /// 渠道余额,按 channel_id 索引;WS 推送直接合并。
    private(set) var balances: [Int64: ChannelBalance] = [:]
    /// 今日花费与预测(随快速刷新走)。
    private(set) var costToday: CostForecast?
    /// 本月花费与预测(随慢速刷新走)。
    private(set) var costMonth: CostForecast?
    /// 计费展示设置(币种/汇率/月预算),金额格式化依赖它。
    private(set) var billing: BillingSettings?
    /// 实时请求流:WS started/completed 增量维护,首屏由 /logs 填充。
    private(set) var recentRequests: [RequestLog] = []
    private(set) var tokensPerSec: Double = 0
    private(set) var activeStreams = 0
    private(set) var lastErrorMessage: String?

    var todayTokens: Int64 { summary?.totalTokens ?? 0 }
    var lifetimeTokens: Int64 { lifetime?.totalTokens ?? 0 }

    // MARK: 内部

    private var api: APIClient?
    private var socket: HubSocket?
    private var settings: AppSettings?
    private var activeBase: URL?
    private var started = false
    private var rebuildTask: Task<Void, Never>?
    private var statsRefreshTask: Task<Void, Never>?
    private var channelsRefreshTask: Task<Void, Never>?
    private var periodicTask: Task<Void, Never>?
    /// 把当前数据交给 Widget 的本机回环端点。
    private let widgetBridge = WidgetBridgeServer()
    private let maxLiveRows = 50

    func start(with settings: AppSettings) {
        guard !started else { return }
        started = true
        self.settings = settings
        settings.onChange = { [weak self] in self?.scheduleRebuild() }
        widgetBridge.start()
        rebuildIfNeeded()
        periodicTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(60))
                guard let self, case .connected = self.connection else { continue }
                // 让趋势窗口随时间滑动(无流量时 WS 不产生 stats.updated);慢速数据同步刷新
                await self.refreshQuick()
                await self.fetchCallers()
                // 余额服务端有 5 分钟 TTL,轮询基本命中缓存
                await self.fetchBalances()
            }
        }
    }

    /// 手动刷新(设置页"立即刷新"按钮)。
    func refreshNow() async {
        await refreshAll()
    }

    /// 切换趋势范围并立即拉取对应数据。
    func setTrendRange(_ range: TrendRange) {
        guard range != trendRange else { return }
        trendRange = range
        trend = []
        Task { await fetchTrend() }
    }

    /// 设置变更防抖:连续输入地址时避免逐字符重连;停顿后真正执行重建。
    private func scheduleRebuild() {
        rebuildTask?.cancel()
        rebuildTask = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(300))
            guard let self, !Task.isCancelled else { return }
            self.rebuildIfNeeded()
        }
    }

    /// 设置变更:仅当服务地址变化时才重建 API 客户端并重连。
    private func rebuildIfNeeded() {
        guard let settings else { return }
        let base = HubURL.httpBase(settings.selectedSource.urlString)
        guard base != activeBase else { return }
        activeBase = base
        api = APIClient(baseURL: base)

        // 切换来源:清空上一个服务的统计数据,避免串台
        summary = nil
        lifetime = nil
        trend = []
        modelTop = []
        callersTop = []
        recentRequests = []
        channels = []
        balances = [:]
        costToday = nil
        costMonth = nil
        billing = nil

        // 数据来源切换：让 Widget 重新取数
        WidgetDataProvider.reloadWidgets()

        socket?.disconnect()
        let sock = HubSocket(url: HubURL.wsBase(base))
        sock.onEvent = { [weak self] event in self?.handleEvent(event) }
        sock.onConnected = { [weak self] in
            self?.connection = .connected
            self?.lastErrorMessage = nil
            Task { await self?.refreshAll() }
        }
        socket = sock

        connection = .connecting
        tokensPerSec = 0
        activeStreams = 0
        sock.connect()
        Task { await refreshAll() }
    }

    // MARK: - WS 事件

    private func handleEvent(_ event: HubEvent) {
        switch event {
        case .throughput(let perSec, let streams):
            tokensPerSec = perSec
            activeStreams = streams
        case .requestStarted(let log):
            upsert(log)
        case .requestCompleted(let log):
            upsert(log)
        case .statsUpdated:
            scheduleStatsRefresh()
        case .channelBalanceUpdated(let payload):
            mergeBalance(payload)
        case .channelUpdated:
            scheduleChannelsRefresh()
        case .ignored:
            break
        }
    }

    /// started 行插入顶部,completed 行按 req_id 原位替换(与前端 Dashboard 逻辑一致)。
    private func upsert(_ log: RequestLog) {
        if log.reqID > 0, let idx = recentRequests.firstIndex(where: { $0.reqID == log.reqID }) {
            recentRequests[idx] = log
        } else {
            recentRequests.insert(log, at: 0)
        }
        if recentRequests.count > maxLiveRows {
            recentRequests.removeLast(recentRequests.count - maxLiveRows)
        }
    }

    /// stats.updated 节流 1s,避免请求风暴时高频拉取。
    private func scheduleStatsRefresh() {
        guard statsRefreshTask == nil else { return }
        statsRefreshTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(1))
            guard let self, !Task.isCancelled else { return }
            self.statsRefreshTask = nil
            await self.refreshQuick()
        }
    }

    /// 渠道启停/探测变化:防抖 1s 后重拉渠道列表(配置型数据,变化不频繁)。
    private func scheduleChannelsRefresh() {
        guard channelsRefreshTask == nil else { return }
        channelsRefreshTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(1))
            guard let self, !Task.isCancelled else { return }
            self.channelsRefreshTask = nil
            await self.fetchChannels()
        }
    }

    /// WS 余额推送:直接合并,避免为了一个渠道余额重拉整批。
    private func mergeBalance(_ payload: ChannelBalancePayload) {
        var entry = balances[payload.channelID] ?? ChannelBalance(
            channelID: payload.channelID,
            channelName: payload.channelName,
            provider: payload.balance.provider,
            supported: true,
            ok: true,
            balance: payload.balance,
            error: nil,
            fetchedAt: payload.balance.fetchedAt
        )
        entry.balance = payload.balance
        entry.ok = true
        entry.error = nil
        entry.fetchedAt = payload.balance.fetchedAt
        balances[payload.channelID] = entry
    }

    // MARK: - REST

    private func refreshQuick() async {
        guard let api else { return }
        let midnight = Calendar.current.startOfDay(for: Date())
        do {
            async let summaryTask = api.summary(since: midnight)
            async let modelTask = api.modelTop(since: midnight)
            let (s, m) = try await (summaryTask, modelTask)
            summary = s
            modelTop = Array(m.prefix(5))
            lastErrorMessage = nil
        } catch {
            lastErrorMessage = error.localizedDescription
        }
        // 花费是可选增强:接口缺失(旧后端)时不能拖垮核心汇总
        costToday = try? await api.costForecast(period: "today")
        await fetchTrend()

        // 同步数据到 Widget
        syncWidgetData()
    }

    /// 按当前选择的范围拉取趋势。
    private func fetchTrend() async {
        guard let api else { return }
        do {
            switch trendRange {
            case .live: trend = try await api.trend(hours: 1, bucket: "minute")
            case .day: trend = try await api.trend(hours: 24, bucket: "hour")
            case .week: trend = try await api.trend(days: 7)
            case .month: trend = try await api.trend(days: 30)
            }
        } catch {
            lastErrorMessage = error.localizedDescription
        }
    }

    /// 调用方 TOP · 30 天(慢速数据)。
    private func fetchCallers() async {
        guard let api else { return }
        do {
            callersTop = Array(try await api.keyTop(hours: 720).prefix(5))
        } catch {
            lastErrorMessage = error.localizedDescription
        }
    }

    /// 渠道列表:失败静默(旧后端无此端点时面板显示空态,不打扰其余数据)。
    private func fetchChannels() async {
        guard let api else { return }
        if let list = try? await api.channels() {
            channels = list
        }
    }

    /// 渠道余额:失败静默并保留上次已知值,不打扰主列表。
    private func fetchBalances() async {
        guard let api else { return }
        guard let list = try? await api.channelBalances() else { return }
        balances = Dictionary(uniqueKeysWithValues: list.items.map { ($0.channelID, $0) })
    }

    /// 计费设置:决定金额展示币种与汇率,失败时回退 USD。
    private func fetchBilling() async {
        guard let api else { return }
        billing = try? await api.billing()
    }

    /// 本月花费与预测(慢速数据,随重连/手动刷新走)。
    private func fetchCostMonth() async {
        guard let api else { return }
        costMonth = try? await api.costForecast(period: "month")
    }

    private func refreshAll() async {
        guard let api else { return }
        // 计费设置先行：花费的展示币种/汇率依赖它，也让随后的 Widget 同步带上正确币种
        await fetchBilling()
        await refreshQuick()
        do {
            async let lifetimeTask = api.lifetime()
            async let logsTask = api.logs(perPage: 30)
            let (lt, lg) = try await (lifetimeTask, logsTask)
            lifetime = lt
            recentRequests = lg.items
        } catch {
            lastErrorMessage = error.localizedDescription
        }
        await fetchCallers()
        await fetchChannels()
        await fetchCostMonth()
        await fetchBalances()
        // 慢速数据（余额等）齐了再同步一次，避免 Widget 拿到残缺载荷
        syncWidgetData()
    }

    // MARK: - Widget 刷新

    /// 把最新数据交给桥接服务，并通知系统重建 Widget 时间线。
    ///
    /// 每次快速刷新后调用。Widget 会先来这里取数（能拿到当前选中的数据来源），
    /// 拿不到时才退回到自己请求 XTokenHub 接口。
    private func syncWidgetData() {
        guard let settings else { return }

        widgetBridge.update(WidgetBridgePayload(
            baseURL: settings.selectedSource.urlString,
            sourceName: settings.selectedSource.name,
            summary: summary,
            costToday: costToday,
            modelTop: modelTop,
            channelBalances: Array(balances.values),
            billingCurrency: billing?.displayCurrency ?? "USD",
            billingRate: billing?.usdRate ?? 0,
            generatedAt: Date()
        ))

        WidgetDataProvider.reloadWidgets()
    }
}

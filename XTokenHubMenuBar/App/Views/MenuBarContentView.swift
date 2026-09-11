import AppKit
import Charts
import SwiftUI

/// 菜单栏下拉主面板:对齐 Web 仪表盘内容(液态玻璃卡片)。
/// 中部为固定高度滚动区:内容超出可滚动,同时不超过屏幕可用高度。
struct MenuBarContentView: View {
    @Environment(HubStore.self) private var store
    @Environment(AppSettings.self) private var settings
    @Environment(\.openSettings) private var openSettings

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            header
            Divider()
            ScrollView(.vertical) {
                // 分区间距 16 > 容器 spacing 8:相邻玻璃不半融合,间隔里透出下层(控制中心的节奏)。
                GlassEffectContainer(spacing: 8) {
                    VStack(alignment: .leading, spacing: 16) {
                        tiles
                        costCard
                        channelsCard
                        trendCard
                        modelCard
                        callersCard
                        liveCard
                    }
                    .padding(.horizontal, 2)
                    .padding(.vertical, 2)
                }
            }
            .frame(height: panelContentHeight)
            .scrollIndicators(.hidden)
            footer
        }
        .padding(14)
        .frame(width: 400)
    }

    /// 面板中部滚动区高度:不超过屏幕可用高度(留出头部/底部与菜单栏)。
    private var panelContentHeight: CGFloat {
        let usable = NSScreen.main?.visibleFrame.height ?? 900
        return min(760, max(320, usable - 200))
    }

    // MARK: - 头部:连接状态 + 实时速度

    private var header: some View {
        HStack(spacing: 8) {
            sourcePicker
            Circle()
                .fill(statusColor)
                .frame(width: 8, height: 8)
            Text(statusText)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            Spacer(minLength: 12)
            if store.activeStreams > 0 {
                Label("\(store.activeStreams)", systemImage: "waveform")
                    .font(.footnote.weight(.medium))
                    .foregroundStyle(.orange)
            }
            Text("\(TokenFormatter.compactSpeed(store.tokensPerSec))/s")
                .font(.callout.weight(.semibold).monospacedDigit())
                .contentTransition(.numericText())
        }
    }

    // MARK: - 数据来源快速切换

    /// 头部来源菜单:列出全部数据来源,点击即切换(当前项带勾选)。
    private var sourcePicker: some View {
        Menu {
            ForEach(settings.dataSources) { source in
                Button {
                    settings.selectSource(source.id)
                } label: {
                    if source.id == settings.selectedSourceID {
                        Label(source.name, systemImage: "checkmark")
                    } else {
                        Text(source.name)
                    }
                }
            }
            if settings.dataSources.count > 1 {
                Divider()
                Button {
                    openSettingsReliably()
                } label: {
                    Label("管理数据来源…", systemImage: "gearshape")
                }
            }
        } label: {
            HStack(spacing: 4) {
                Image(systemName: "server.rack")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                Text(settings.selectedSource.name)
                    .font(.footnote.weight(.semibold))
                    .lineLimit(1)
                Image(systemName: "chevron.up.chevron.down")
                    .font(.caption2.weight(.medium))
                    .foregroundStyle(.secondary)
            }
        }
        .menuStyle(.button)
        .menuIndicator(.hidden)
        .fixedSize()
        .help("切换数据来源")
    }

    // MARK: - 统计磁贴(对齐 Web 两排卡片)

    /// 九宫格:同为计数器但数量级差很大,故大数值的数量级(万/亿)降为次级字号。
    private var tiles: some View {
        StatGrid(cells: [
            // 第一排 · 今天(对应 Web "请求总数/错误请求/Token 用量/缓存命中率/平均耗时/原生透传占比")
            StatItem(title: "请求 · 今日", value: reqText, icon: "number", help: "当天请求总数"),
            StatItem(
                title: "错误 · 今日",
                value: errText,
                icon: "exclamationmark.triangle",
                tint: (store.summary?.errorRequests ?? 0) > 0 ? .red : .secondary,
                help: "当天失败请求"
            ),
            tokenItem(title: "Token · 今日", value: store.summary?.totalTokens, icon: "paperplane.fill", help: "今天 · prompt + completion"),
            StatItem(title: "缓存命中", value: cacheHitText, icon: "memorychip", help: "cached / prompt"),
            StatItem(title: "平均耗时", value: avgDurationText, icon: "clock", help: "今天成功请求均值"),
            StatItem(title: "透传占比", value: nativeRatioText, icon: "arrow.right.circle", help: "零转换直连上游"),
            // 第二排 · 全历史(对应 Web "累计/峰值/连续天数")
            tokenItem(title: "累计 Token", value: store.lifetime?.totalTokens, icon: "infinity", help: lifetimeHelp),
            tokenItem(title: "峰值 Token", value: store.lifetime?.peakDayTokens, icon: "arrow.up.forward.circle", help: peakHelp),
            StatItem(title: "连续天数", value: streakText, icon: "flame", tint: .orange, help: streakHelp),
        ])
    }

    /// 大数值单元:万/亿后缀拆成次级字号,避免「9290.47万」读成一个长数字。
    private func tokenItem(title: String, value: Int64?, icon: String, help: String) -> StatItem {
        guard let value else { return StatItem(title: title, value: "—", icon: icon, help: help) }
        let parts = TokenFormatter.splitCompact(value)
        return StatItem(title: title, value: parts.number, unit: parts.unit, icon: icon, help: help)
    }

    private var reqText: String { store.summary.map { String($0.totalRequests) } ?? "—" }
    private var errText: String { store.summary.map { String($0.errorRequests) } ?? "—" }

    private var cacheHitText: String {
        guard let rate = store.summary?.cacheHitRate else { return "—" }
        return String(format: "%.1f%%", rate * 100)
    }

    private var avgDurationText: String {
        guard let ms = store.summary?.avgDurationMS else { return "—" }
        return TokenFormatter.duration(Int64(ms.rounded()))
    }

    private var nativeRatioText: String {
        guard let ratio = store.summary?.nativeRatio else { return "—" }
        return String(format: "%.1f%%", ratio * 100)
    }

    private var lifetimeHelp: String { store.lifetime.map { "共 \($0.activeDays) 天有用量" } ?? "" }
    private var peakHelp: String { store.lifetime?.peakDay ?? "单日最高" }
    private var streakText: String { store.lifetime.map { "\($0.currentStreak)" } ?? "—" }
    private var streakHelp: String {
        guard let lt = store.lifetime else { return "" }
        return "当前 \(lt.currentStreak) 天 · 最长 \(lt.maxStreak) 天"
    }

    // MARK: - 花费(对齐 Web 仪表盘:今日 / 本月累计 / 本月预计)

    private var costCard: some View {
        PanelSection(title: "花费") {
            VStack(spacing: 10) {
                CostRow(
                    label: "今日花费",
                    value: money(store.costToday?.spentUSD),
                    sub: todayCostSub,
                    tint: .accentColor
                )
                CostRow(
                    label: "本月累计",
                    value: money(store.costMonth?.spentUSD),
                    sub: budgetSub,
                    tint: .primary
                )
                CostRow(
                    label: "本月预计",
                    value: money(store.costMonth?.projectedUSD),
                    sub: monthCostSub,
                    tint: .orange
                )
            }
        }
    }

    /// 金额展示选项:币种与汇率来自设置页,未加载时按 USD。
    private var moneyOptions: MoneyFormatter.Options {
        MoneyFormatter.Options(
            currency: store.billing?.displayCurrency ?? "USD",
            rate: store.billing?.usdRate ?? 0
        )
    }

    private func money(_ usd: Double?) -> String {
        guard let usd else { return "—" }
        return MoneyFormatter.format(usd: usd, options: moneyOptions)
    }

    private var todayCostSub: String {
        guard let cost = store.costToday else { return "" }
        if let projected = cost.projectedUSD {
            return "预计今日 " + MoneyFormatter.format(usd: projected, options: moneyOptions)
        }
        return cost.reason ?? "样本不足"
    }

    private var monthCostSub: String {
        guard let cost = store.costMonth else { return "" }
        guard cost.projectedUSD != nil else { return cost.reason ?? "样本不足" }
        return "\(basisLabel(cost.basis)) · 置信度\(confidenceLabel(cost.confidence))"
    }

    private var budgetSub: String {
        guard let cost = store.costMonth, cost.budgetUSD > 0 else { return "" }
        var text = "月度预算 " + MoneyFormatter.format(usd: cost.budgetUSD, options: moneyOptions)
        if let date = cost.projectedExceededDate, !date.isEmpty {
            text += " · 预计 \(date) 触及"
        }
        return text
    }

    private func basisLabel(_ basis: String) -> String {
        basis == "run_rate" ? "近 7 日均速" : "本周期线性"
    }

    private func confidenceLabel(_ confidence: String) -> String {
        switch confidence {
        case "high": "高"
        case "medium": "中"
        default: "低"
        }
    }

    // MARK: - 渠道与余额(余额为上游原币种,不做汇率折算)

    private var channelsCard: some View {
        PanelSection(title: channelsTitle) {
            if store.channels.isEmpty {
                emptyHint
            } else {
                VStack(spacing: 8) {
                    ForEach(store.channels) { channel in
                        ChannelBalanceRow(channel: channel, entry: store.balances[channel.id])
                    }
                }
            }
        }
    }

    private var channelsTitle: String {
        let enabled = store.channels.filter(\.isEnabled).count
        guard !store.channels.isEmpty else { return "渠道 · 余额" }
        return "渠道 · 余额 · 启用 \(enabled)/\(store.channels.count)"
    }

    // MARK: - 趋势(带范围切换,对应 Web 实时/小时/7天/30天)

    private var trendCard: some View {
        PanelSection {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Text("Token 趋势")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(.secondary)
                    Spacer(minLength: 8)
                    Picker("范围", selection: rangeBinding) {
                        ForEach(HubStore.TrendRange.allCases) { range in
                            Text(range.label).tag(range)
                        }
                    }
                    .pickerStyle(.segmented)
                    .controlSize(.mini)
                    .labelsHidden()
                    .frame(width: 172)
                }
                if store.trend.isEmpty {
                    emptyHint
                } else {
                    TrendSparkline(points: store.trend, height: 80)
                }
            }
        }
    }

    private var rangeBinding: Binding<HubStore.TrendRange> {
        Binding(
            get: { store.trendRange },
            set: { store.setTrendRange($0) }
        )
    }

    // MARK: - 模型 / 调用方排行

    private var modelCard: some View {
        PanelSection(title: "模型 TOP · 今日") {
            if store.modelTop.isEmpty {
                emptyHint
            } else {
                VStack(spacing: 8) {
                    let maxTokens = store.modelTop.map(\.totalTokens).max() ?? 1
                    ForEach(store.modelTop) { stat in
                        UsageBarRow(
                            name: stat.name,
                            tokens: stat.totalTokens,
                            requests: stat.requests,
                            fraction: maxTokens > 0 ? Double(stat.totalTokens) / Double(maxTokens) : 0
                        )
                    }
                }
            }
        }
    }

    private var callersCard: some View {
        PanelSection(title: "调用方 TOP · 30 天") {
            if store.callersTop.isEmpty {
                emptyHint
            } else {
                VStack(spacing: 8) {
                    let maxTokens = store.callersTop.map(\.totalTokens).max() ?? 1
                    ForEach(store.callersTop) { stat in
                        UsageBarRow(
                            name: stat.name,
                            tokens: stat.totalTokens,
                            requests: stat.requests,
                            fraction: maxTokens > 0 ? Double(stat.totalTokens) / Double(maxTokens) : 0
                        )
                    }
                }
            }
        }
    }

    // MARK: - 实时请求流

    private var liveCard: some View {
        PanelSection(title: "实时请求流") {
            if store.recentRequests.isEmpty {
                emptyHint
            } else {
                ScrollView {
                    LazyVStack(spacing: 5) {
                        ForEach(store.recentRequests) { log in
                            LiveRequestRow(log: log, money: moneyOptions)
                        }
                    }
                }
                .frame(height: 260)
            }
        }
    }

    private var emptyHint: some View {
        Text(store.connection.isOnline ? "暂无数据" : "等待连接…")
            .font(.caption)
            .foregroundStyle(.secondary)
            .frame(maxWidth: .infinity)
            .padding(.vertical, 10)
    }

    // MARK: - 底部操作

    private var footer: some View {
        // 相邻玻璃按钮同一容器,由系统决定融合与高光,避免各自成边。
        GlassEffectContainer(spacing: 6) {
            HStack(spacing: 8) {
                Button {
                    NSWorkspace.shared.open(settings.httpBaseURL)
                } label: {
                    Label("控制台", systemImage: "macwindow")
                }
                .buttonStyle(.glass)

                Button {
                    openSettingsReliably()
                } label: {
                    Label("设置", systemImage: "gearshape")
                }
                .buttonStyle(.glass)

                Spacer(minLength: 8)

                Button {
                    NSApp.terminate(nil)
                } label: {
                    Label("退出", systemImage: "power")
                }
                .buttonStyle(.glass)
            }
            // 显式占满宽度:容器不应替内部的 Spacer 决定可用宽度,否则「退出」会挤到中间。
            .frame(maxWidth: .infinity)
        }
    }

    // MARK: - 设置窗口可靠打开

    /// MenuBarExtra 面板是临时窗口:点击时 App 处于非激活态,面板又随焦点转移关闭,
    /// `openSettings` 在这种时序下可能被吞掉,或设置窗口打开后未置前(表现为"点不开")。
    /// 处理:先激活 App;窗口已存在直接置前;否则 openSettings 后兜底再激活并置前一次。
    private func openSettingsReliably() {
        let findSettingsWindow = {
            NSApp.windows.first {
                $0.identifier?.rawValue == "com_apple_SwiftUI_Settings_window"
                    || $0.frameAutosaveName == "com_apple_SwiftUI_Settings_window"
            }
        }

        NSApp.activate(ignoringOtherApps: true)
        if let existing = findSettingsWindow() {
            existing.makeKeyAndOrderFront(nil)
        } else {
            openSettings()
        }
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.1) {
            NSApp.activate(ignoringOtherApps: true)
            findSettingsWindow()?.makeKeyAndOrderFront(nil)
        }
    }

    // MARK: - 状态文案

    private var statusText: String {
        switch store.connection {
        case .connected: "已连接"
        case .connecting: "连接中…"
        case .disconnected(let reason): "离线 · \(reason)"
        }
    }

    private var statusColor: Color {
        switch store.connection {
        case .connected: .green
        case .connecting: .orange
        case .disconnected: .red
        }
    }
}

// MARK: - 花费行

/// 花费卡单行:左侧标题 + 右侧金额与副文案。
/// 用纯文本行而非嵌套玻璃磁贴,避免与卡片玻璃材质叠加。
private struct CostRow: View {
    let label: String
    let value: String
    var sub: String = ""
    var tint: Color = .primary

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
            Spacer(minLength: 8)
            VStack(alignment: .trailing, spacing: 1) {
                Text(value)
                    .font(.callout.weight(.semibold).monospacedDigit())
                    .foregroundStyle(tint)
                    .contentTransition(.numericText())
                if !sub.isEmpty {
                    Text(sub)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }
        }
    }
}

// MARK: - 渠道余额行

/// 渠道行:状态点 + 名称 + 接口风格 + 原生币种余额。
private struct ChannelBalanceRow: View {
    let channel: Channel
    let entry: ChannelBalance?

    var body: some View {
        HStack(spacing: 8) {
            Circle()
                .fill(channel.isEnabled ? Color.green : Color.secondary)
                .frame(width: 7, height: 7)
            VStack(alignment: .leading, spacing: 1) {
                Text(channel.name)
                    .font(.caption.weight(.medium))
                    .foregroundStyle(channel.isEnabled ? .primary : .secondary)
                    .lineLimit(1)
                Text(channel.provider == "anthropic" ? "Anthropic" : "OpenAI 兼容")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 8)
            Text(balanceText)
                .font(.caption.monospacedDigit())
                .foregroundStyle(balanceTint)
                .help(balanceHelp)
        }
        .opacity(channel.isEnabled ? 1 : 0.55)
    }

    private var balanceText: String {
        guard let entry, entry.supported else { return "—" }
        guard entry.ok, let info = entry.balance else { return "查询失败" }
        return MoneyFormatter.formatBalance(info.total, currency: info.currency)
    }

    private var balanceTint: Color {
        guard let entry, entry.supported else { return .secondary }
        guard entry.ok, let info = entry.balance else { return .red }
        return info.isAvailable ? .primary : .red
    }

    private var balanceHelp: String {
        guard let entry else { return "余额查询中…" }
        guard entry.supported else { return "该渠道不支持余额查询" }
        guard entry.ok, let info = entry.balance else { return entry.error ?? "查询失败" }

        var lines = [
            "总额 " + MoneyFormatter.formatBalance(info.total, currency: info.currency),
            "赠金 " + MoneyFormatter.formatBalance(info.granted, currency: info.currency),
            "充值 " + MoneyFormatter.formatBalance(info.toppedUp, currency: info.currency),
        ]
        // 后端零值时间(0001-01-01)不展示
        if let at = HubDate.parseRFC3339(entry.fetchedAt ?? ""), at.timeIntervalSince1970 > 0 {
            lines.append("拉取于 \(at.formatted(date: .abbreviated, time: .shortened))")
        }
        if !info.isAvailable {
            lines.append("账户当前无可用于 API 调用的余额")
        }
        return lines.joined(separator: "\n")
    }
}

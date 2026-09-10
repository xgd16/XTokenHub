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
                GlassEffectContainer(spacing: 10) {
                    VStack(alignment: .leading, spacing: 10) {
                        tiles
                        trendCard
                        modelCard
                        callersCard
                        liveCard
                    }
                    .padding(.horizontal, 2)
                    .padding(.vertical, 1)
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

    private var tiles: some View {
        LazyVGrid(
            columns: Array(repeating: GridItem(.flexible(), spacing: 10), count: 3),
            spacing: 10
        ) {
            // 第一排 · 今天(对应 Web "请求总数/错误请求/Token 用量/缓存命中率/平均耗时/原生透传占比")
            StatTile(title: "请求 · 今日", value: reqText, icon: "number", help: "当天请求总数")
            StatTile(
                title: "错误 · 今日",
                value: errText,
                icon: "exclamationmark.triangle",
                tint: (store.summary?.errorRequests ?? 0) > 0 ? .red : .secondary,
                help: "当天失败请求"
            )
            StatTile(title: "Token · 今日", value: TokenFormatter.compact(store.todayTokens), icon: "paperplane.fill", help: "今天 · prompt + completion")
            StatTile(title: "缓存命中", value: cacheHitText, icon: "memorychip", help: "cached / prompt")
            StatTile(title: "平均耗时", value: avgDurationText, icon: "clock", help: "今天成功请求均值")
            StatTile(title: "透传占比", value: nativeRatioText, icon: "arrow.right.circle", help: "零转换直连上游")
            // 第二排 · 全历史(对应 Web "累计/峰值/连续天数")
            StatTile(title: "累计 Token", value: lifetimeText, icon: "infinity", help: lifetimeHelp)
            StatTile(title: "峰值 Token", value: peakText, icon: "arrow.up.forward.circle", help: peakHelp)
            StatTile(title: "连续天数", value: streakText, icon: "flame", help: streakHelp)
        }
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

    private var lifetimeText: String { store.lifetime.map { TokenFormatter.compact($0.totalTokens) } ?? "—" }
    private var lifetimeHelp: String { store.lifetime.map { "共 \($0.activeDays) 天有用量" } ?? "" }
    private var peakText: String { store.lifetime.map { TokenFormatter.compact($0.peakDayTokens) } ?? "—" }
    private var peakHelp: String { store.lifetime?.peakDay ?? "单日最高" }
    private var streakText: String { store.lifetime.map { "\($0.currentStreak)" } ?? "—" }
    private var streakHelp: String {
        guard let lt = store.lifetime else { return "" }
        return "当前 \(lt.currentStreak) 天 · 最长 \(lt.maxStreak) 天"
    }

    // MARK: - 趋势(带范围切换,对应 Web 实时/小时/7天/30天)

    private var trendCard: some View {
        GlassCard {
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
        GlassCard(title: "模型 TOP · 今日") {
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
        GlassCard(title: "调用方 TOP · 30 天") {
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
        GlassCard(title: "实时请求流") {
            if store.recentRequests.isEmpty {
                emptyHint
            } else {
                ScrollView {
                    LazyVStack(spacing: 5) {
                        ForEach(store.recentRequests) { log in
                            LiveRequestRow(log: log)
                        }
                    }
                }
                .frame(height: 160)
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

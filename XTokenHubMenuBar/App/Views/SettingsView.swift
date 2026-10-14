import SwiftUI

/// 设置窗口:数据来源管理 / 菜单栏显示 / 小组件余额渠道 / 登录自启。
struct SettingsView: View {
    @Environment(AppSettings.self) private var settings
    @Environment(HubStore.self) private var store

    var body: some View {
        @Bindable var settings = settings
        Form {
            Section {
                ForEach(settings.dataSources) { source in
                    SourceRow(source: source, isSelected: source.id == settings.selectedSourceID)
                }
                Button {
                    settings.addSource()
                } label: {
                    Label("添加数据来源", systemImage: "plus")
                }
            } header: {
                Text("数据来源")
            } footer: {
                Text("可添加多个 XTokenHub 服务,在菜单栏面板顶部快速切换;名称与地址修改后自动保存并重连。")
            }

            Section {
                LabeledContent("当前状态") {
                    HStack(spacing: 6) {
                        Circle()
                            .fill(statusColor)
                            .frame(width: 8, height: 8)
                        Text(statusText)
                            .foregroundStyle(.secondary)
                    }
                }
                if let err = store.lastErrorMessage {
                    LabeledContent("最近错误") {
                        Text(err)
                            .font(.caption)
                            .foregroundStyle(.red)
                            .lineLimit(2)
                    }
                }
                Button("立即刷新") {
                    Task { await store.refreshNow() }
                }
            } header: {
                Text("Hub 连接 · \(settings.selectedSource.name)")
            } footer: {
                Text("默认 http://127.0.0.1:9192,支持远程部署地址。")
            }

            Section {
                ForEach(AppSettings.MenuBarMetric.allCases) { metric in
                    Toggle(metric.label, isOn: metricBinding(metric))
                }
                LabeledContent("效果预览") {
                    MenuBarLabel(store: store, settings: settings)
                        .font(.callout)
                }
            } header: {
                Text("菜单栏显示")
            } footer: {
                Text("自由勾选要显示的指标,顺序固定为:图标 · 请求数 · Token · 命中率 · 花费 · 速度 · 峰谷;全部取消时仅显示图标。峰谷取最近切换的模型,显示「峰」或「谷」。")
            }

            Section {
                if balanceChannelOptions.isEmpty {
                    Text("渠道列表加载后可在此勾选,当前默认全部显示。")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                } else {
                    ForEach(balanceChannelOptions) { option in
                        Toggle(option.name, isOn: channelBinding(option.id))
                    }
                    HStack(spacing: 8) {
                        Button("全选(含新增渠道)") { settings.widgetBalanceChannels = nil }
                        Button("清空(都不显示)") { settings.widgetBalanceChannels = [] }
                        Spacer(minLength: 0)
                    }
                    .buttonStyle(.borderless)
                    .controlSize(.small)
                }
            } header: {
                Text("小组件 · 余额渠道")
            } footer: {
                Text("多选:只有勾选的渠道才会显示在小组件上,没勾的一律不显示。各尺寸按空间取前几个(按渠道 id 排序):Small 1 个、Medium 最多 3 行(再多加一行「还有 N 个勾选渠道未显示」)、Large 每行 2 个最多 2 行(溢出并进「余额+N」)。勾选了但查不到余额的渠道显示「—」,排在有余额的后面。「全选」是默认状态,以后新增的渠道也会显示;「清空」则完全不显示余额,Small 右下角回到「缓存命中」。")
            }

            Section {
                Toggle("登录时自动启动", isOn: $settings.launchAtLogin)
                if let err = settings.launchAtLoginError {
                    Text(err)
                        .font(.caption)
                        .foregroundStyle(.red)
                }
                Toggle("峰谷切换通知", isOn: $settings.peakNotifyEnabled)
                if let err = settings.peakNotifyError {
                    Text(err)
                        .font(.caption)
                        .foregroundStyle(.red)
                }
                LabeledContent("版本") {
                    Text("1.2.0")
                        .foregroundStyle(.secondary)
                }
            } header: {
                Text("通用")
            } footer: {
                Text("峰谷通知按模型提醒转入高峰/空闲的时刻,需在系统弹窗中允许通知。")
            }
        }
        .formStyle(.grouped)
        .frame(width: 440, height: 500)
    }

    /// 小组件余额渠道的可选项:优先渠道列表;渠道接口还没返回时退回「已查到余额的渠道」。
    /// 勾选集合里存在、但当前列表没有的渠道(换了数据来源)时补一个占位项,
    /// 保证它在设置页可见、也能被取消勾选。
    private var balanceChannelOptions: [BalanceChannelOption] {
        var options = store.channels.map { BalanceChannelOption(id: $0.id, name: $0.name) }
        if options.isEmpty {
            options = store.balances.values
                .sorted { $0.channelID < $1.channelID }
                .map { BalanceChannelOption(id: $0.channelID, name: $0.channelName) }
        }
        // 勾选集合里有、但当前列表没有的渠道(换了数据来源):补占位项,便于看到并取消勾选
        let missing = (settings.widgetBalanceChannels ?? [])
            .filter { id in !options.contains(where: { $0.id == id }) }
            .sorted()
        for id in missing.reversed() {
            options.insert(BalanceChannelOption(id: id, name: "已勾选渠道 #\(id)"), at: 0)
        }
        return options
    }

    /// 单个渠道的勾选状态:未设置(nil)时默认全部显示;首次改动时先把当前列表物化成显式集合再改。
    private func channelBinding(_ id: Int64) -> Binding<Bool> {
        Binding(
            get: { settings.widgetBalanceChannels?.contains(id) ?? true },
            set: { on in
                var selected = settings.widgetBalanceChannels ?? Set(balanceChannelOptions.map(\.id))
                if on { selected.insert(id) } else { selected.remove(id) }
                settings.widgetBalanceChannels = selected
            }
        )
    }

    private func metricBinding(_ metric: AppSettings.MenuBarMetric) -> Binding<Bool> {
        Binding(
            get: { settings.menuBarMetrics.contains(metric) },
            set: { on in
                if on { settings.menuBarMetrics.insert(metric) }
                else { settings.menuBarMetrics.remove(metric) }
            }
        )
    }

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

/// 小组件余额渠道选择器的一项。
private struct BalanceChannelOption: Identifiable {
    let id: Int64
    let name: String
}

/// 单个数据来源行:左侧单选标记 + 名称/地址编辑 + 删除。
/// 编辑使用本地草稿,失焦或回车时提交,避免逐字符触发重连。
private struct SourceRow: View {
    let source: DataSource
    let isSelected: Bool

    @Environment(AppSettings.self) private var settings

    private enum Field { case name, url }

    @State private var name: String
    @State private var urlString: String
    @FocusState private var focused: Field?

    init(source: DataSource, isSelected: Bool) {
        self.source = source
        self.isSelected = isSelected
        _name = State(initialValue: source.name)
        _urlString = State(initialValue: source.urlString)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack(spacing: 8) {
                Button {
                    commit()
                    settings.selectSource(source.id)
                } label: {
                    Image(systemName: isSelected ? "checkmark.circle.fill" : "circle")
                        .foregroundStyle(isSelected ? Color.accentColor : .secondary)
                }
                .buttonStyle(.plain)
                .help(isSelected ? "当前数据来源" : "切换到此来源")

                TextField("", text: $name, prompt: Text("名称"))
                    .labelsHidden()
                    .focused($focused, equals: .name)
                    .onSubmit(commit)

                Spacer(minLength: 4)

                if settings.dataSources.count > 1 {
                    Button(role: .destructive) {
                        settings.removeSource(source.id)
                    } label: {
                        Image(systemName: "minus.circle")
                    }
                    .buttonStyle(.borderless)
                    .help("删除此来源")
                }
            }
            TextField("", text: $urlString, prompt: Text("http://127.0.0.1:9192"))
                .labelsHidden()
                .font(.caption.monospacedDigit())
                .foregroundStyle(.secondary)
                .textFieldStyle(.plain)
                .focused($focused, equals: .url)
                .onSubmit(commit)
        }
        .onChange(of: focused) { _, newValue in
            if newValue == nil { commit() }
        }
    }

    private func commit() {
        settings.updateSource(source.id, name: name, urlString: urlString)
    }
}

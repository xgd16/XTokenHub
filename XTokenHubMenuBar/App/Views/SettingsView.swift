import SwiftUI

/// 设置窗口:数据来源管理 / 菜单栏显示 / 登录自启。
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
                Text("自由勾选要显示的指标,按 勾选顺序固定为:图标 · 请求数 · Token · 命中率 · 速度;全部取消时仅显示图标。")
            }

            Section("通用") {
                Toggle("登录时自动启动", isOn: $settings.launchAtLogin)
                if let err = settings.launchAtLoginError {
                    Text(err)
                        .font(.caption)
                        .foregroundStyle(.red)
                }
                LabeledContent("版本") {
                    Text("1.1.0")
                        .foregroundStyle(.secondary)
                }
            }
        }
        .formStyle(.grouped)
        .frame(width: 440, height: 460)
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

                TextField("名称", text: $name)
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
            TextField("http://127.0.0.1:9192", text: $urlString)
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

import SwiftUI

/// 设置窗口:Hub 地址 / 菜单栏显示 / 登录自启。
struct SettingsView: View {
    @Environment(AppSettings.self) private var settings
    @Environment(HubStore.self) private var store

    var body: some View {
        @Bindable var settings = settings
        Form {
            Section {
                TextField("服务地址", text: $settings.baseURLString)
                    .textFieldStyle(.roundedBorder)
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
                Text("Hub 连接")
            } footer: {
                Text("默认 http://127.0.0.1:9192,支持远程部署地址。修改后自动重连。")
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
                    Text("1.0.0")
                        .foregroundStyle(.secondary)
                }
            }
        }
        .formStyle(.grouped)
        .frame(width: 440, height: 400)
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

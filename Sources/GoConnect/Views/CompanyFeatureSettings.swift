import SwiftUI
import GoConnectCore

struct CompanyFeatureSettings: View {
    @Bindable var store: AppStore
    @State private var configuring = false
    @State private var error: String?

    private var config: CompanyConnectionConfiguration {
        store.companyConfiguration
    }

    private var configuredLineName: String? {
        guard let id = config.companyProfileID else { return nil }
        return store.configuration.connections.first(where: { $0.id == id })?.displayName
    }

    private var hasSavedScope: Bool {
        config.enabled || config.companyProfileID != nil || !config.networks.isEmpty || !config.domains.isEmpty
    }

    private var dnsSummary: String {
        config.dnsServers.isEmpty ? "DNS 自动获取" : "DNS 覆盖 \(config.dnsServers.count) 个"
    }

    var body: some View {
        Section("公司副连接") {
            Toggle("启用公司副连接", isOn: Binding(
                get: { config.enabled },
                set: { enabled in
                    error = nil
                    if enabled {
                        configuring = true
                    } else {
                        do { try store.disableCompanyConnection() }
                        catch { self.error = error.localizedDescription }
                    }
                }
            ))
            .disabled(!store.canEditCompanyConnectionSettings)
            .accessibilityIdentifier("CompanyConnectionFeatureEnabled")

            if hasSavedScope {
                HStack(spacing: 12) {
                    VStack(alignment: .leading, spacing: 3) {
                        HStack(spacing: 6) {
                            Text(configuredLineName ?? "尚未选择公司线路")
                                .font(.callout.weight(.medium))
                                .lineLimit(1)
                            if config.enabled {
                                StatusPill(title: "已启用", symbol: "checkmark.circle.fill", color: AppTheme.success)
                            }
                        }
                        Text("\(config.networks.count) 个网段 · \(config.domains.count) 个域名 · \(dnsSummary)")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    Spacer(minLength: 8)
                    Button("配置…", systemImage: "slider.horizontal.3") { configuring = true }
                        .appActionStyle()
                        .disabled(!store.canEditCompanyConnectionSettings)
                        .fixedSize()
                }
            }

            if let reason = store.companyConnectionSettingsReadOnlyReason {
                Label(reason, systemImage: "lock")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            } else {
                Text("默认关闭。开启并保存后在连接页和菜单栏显示入口；关闭后保留已有配置。")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }

            if let error {
                InlineNotice(text: error)
            }
        }
        .sheet(isPresented: $configuring) {
            CompanyConnectionSettings(store: store, initiallyEnabled: true)
        }
    }
}

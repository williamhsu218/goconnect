import SwiftUI

struct CompanyFeatureSettings: View {
    @Bindable var store: AppStore
    @State private var configuring = false
    @State private var error: String?

    var body: some View {
        Section("高级功能") {
            Toggle("公司副连接", isOn: Binding(
                get: { store.companyConfiguration.enabled },
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
            Text("默认关闭。开启并保存公司配置后，在连接页和菜单栏显示入口；关闭后保留已有配置。")
                .font(.caption).foregroundStyle(.secondary)
            if store.companyConfiguration.enabled {
                Button("配置公司副连接…") { configuring = true }
                    .disabled(!store.canEditCompanyConnectionSettings)
            }
            if store.busy || store.company.isRunning || store.company.busy {
                Text("请先断开主、副连接，再调整此功能。")
                    .font(.caption).foregroundStyle(.secondary)
            }
            if let error { Text(error).font(.caption).foregroundStyle(.red) }
        }
        .sheet(isPresented: $configuring) {
            CompanyConnectionSettings(store: store, initiallyEnabled: true)
        }
    }
}

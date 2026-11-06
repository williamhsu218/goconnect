import SwiftUI

struct AppUpdateCommands: Commands {
    @ObservedObject var updater: AppUpdater
    var body: some Commands {
        CommandGroup(after: .appInfo) {
            Button("检查更新…") { updater.checkForUpdates() }
                .disabled(!updater.canCheckForUpdates)
        }
    }
}

struct AppUpdateSettings: View {
    @ObservedObject var updater: AppUpdater
    var body: some View {
        Section("应用更新") {
            HStack {
                Text("通过 GitHub 获取更新")
                Spacer()
                Button("检查更新…") { updater.checkForUpdates() }
                    .disabled(!updater.canCheckForUpdates)
            }
            Toggle("自动检查更新", isOn: Binding(get: { updater.automaticallyChecksForUpdates },
                set: { updater.automaticallyChecksForUpdates = $0 }))
                .disabled(!updater.isConfigured)
            Text(updater.statusDescription).font(.callout).foregroundStyle(.secondary)
            Text("安装前请手动断开 VPN，并保存正在编辑的线路。应用不会自动安装更新。")
                .font(.caption).foregroundStyle(.secondary)
            if updater.hasUnconfirmedInstallation {
                Text("上次安装尚待确认。连接功能可继续使用；退出前请手动断开 VPN。")
                    .font(.caption).foregroundStyle(.secondary)
            }
            if updater.hasDeferredInstallation {
                Button("继续安装已下载的更新") { updater.resumeDeferredInstallation() }
            }
        }
    }
}

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

    private var statusBadge: (title: String, symbol: String, color: Color)? {
        if updater.installationInProgress {
            return ("准备安装", "arrow.down.circle.fill", AppTheme.accent)
        }
        if updater.hasDeferredInstallation {
            return ("待安装", "arrow.down.circle.fill", AppTheme.accent)
        }
        if updater.hasUnconfirmedInstallation {
            return ("待确认", "exclamationmark.triangle.fill", AppTheme.warning)
        }
        if !updater.isConfigured {
            return ("未启用", "minus.circle", .secondary)
        }
        return nil
    }

    var body: some View {
        Section("应用更新") {
            HStack(spacing: 12) {
                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 8) {
                        Text("通过 GitHub 获取更新")
                            .font(.callout.weight(.medium))
                        if let badge = statusBadge {
                            StatusPill(title: badge.title, symbol: badge.symbol, color: badge.color)
                        }
                    }
                    Text(updater.statusDescription)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Spacer(minLength: 8)
                Button("检查更新…", systemImage: "arrow.clockwise") { updater.checkForUpdates() }
                    .appActionStyle()
                    .disabled(!updater.canCheckForUpdates)
                    .fixedSize()
            }

            Toggle("自动检查更新", isOn: Binding(
                get: { updater.automaticallyChecksForUpdates },
                set: { updater.automaticallyChecksForUpdates = $0 }
            ))
            .disabled(!updater.isConfigured)

            if updater.hasUnconfirmedInstallation {
                InlineNotice(text: "上次安装尚待确认。连接功能可继续使用；退出前请手动断开 VPN。")
            }

            if updater.hasDeferredInstallation {
                HStack {
                    Text("新版本已下载并校验完成。")
                        .font(.callout)
                        .foregroundStyle(.secondary)
                    Spacer()
                    Button("安装并重启", systemImage: "arrow.triangle.2.circlepath") {
                        updater.resumeDeferredInstallation()
                    }
                    .appActionStyle(primary: true)
                    .disabled(updater.installationInProgress)
                    .fixedSize()
                }
            }

            Text("检查、下载和验签可保持 VPN 连接，仅在确认安装重启时断开。应用不会自动下载安装。")
                .font(.caption)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
    }
}

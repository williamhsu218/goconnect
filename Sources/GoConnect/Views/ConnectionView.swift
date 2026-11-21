import SwiftUI
import GoConnectCore

struct ConnectionView: View {
    @Bindable var store: AppStore

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                HStack(spacing: 8) {
                    Image(nsImage: AppBranding.icon)
                        .resizable()
                        .interpolation(.high)
                        .scaledToFit()
                        .frame(width: 28, height: 28)
                        .accessibilityHidden(true)
                    Text(AppBranding.displayName).font(.headline)
                }
                .frame(maxWidth: .infinity)
                if Product.isNetworkTestApp { TestWorkspaceCard(store: store) }
                VPNConnectionCard(store: store)

                if store.recoveryRequired {
                    InlineNotice(text: "网络进程尚未完全退出，已暂停重连。请导出诊断，重启 macOS 后再试。", symbol: "exclamationmark.triangle")
                }

                if store.showsCompanyConnection {
                    CompanyConnectionCard(store: store)
                }

                Surface {
                    VStack(spacing: 0) {
                        ApplicationRoutingCard(store: store)
                        Divider().padding(.vertical, 8)
                        TrafficUsageCard(store: store)
                    }
                }
            }
            .frame(maxWidth: 632)
            .padding(.horizontal, AppTheme.pagePadding)
            .padding(.vertical, 22)
            .frame(maxWidth: .infinity)
        }
    }
}

struct ConnectionSummaryLink: View {
    let title: String
    let symbol: String
    let detail: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 10) {
                Label(title, systemImage: symbol)
                    .font(.callout.weight(.medium))
                    .fixedSize(horizontal: true, vertical: false)
                Spacer(minLength: 8)
                Text(detail)
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
                    .multilineTextAlignment(.trailing)
                Image(systemName: "chevron.right")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)
                    .accessibilityHidden(true)
            }
            .frame(minHeight: 24)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(title)
        .accessibilityValue(detail)
    }
}

private struct TestWorkspaceCard: View {
    let store: AppStore
    var body: some View {
        GroupBox {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Label("双 VPN 功能测试版", systemImage: "testtube.2").font(.headline)
                    Spacer()
                    Button("复制正式版线路…") { store.copyProductionProfilesForTesting() }
                        .disabled(store.busy || store.company.busy || store.company.isRunning || store.companyDraftOpen || store.profileDraft != nil)
                }
                Text("独立配置、钥匙串和测试网络服务。复制线路会包含订阅节点凭据；VPN 密码需重新输入。").font(.callout).foregroundStyle(.secondary)
                if let reason = store.networkService.testHostMessage {
                    Label(reason, systemImage: "pause.circle").font(.callout).foregroundStyle(.orange)
                } else {
                    Text("请在单独验收窗口连接；测试服务需单独授权。完成后先断开测试版，再恢复正式版连接。").font(.caption).foregroundStyle(.secondary)
                }
                Button("重新检测正式版状态") { store.networkService.refresh() }.font(.caption)
            }.padding(8).frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

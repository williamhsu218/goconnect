import SwiftUI
import GoConnectCore

struct ApplicationRoutingCard: View {
    @Bindable var store: AppStore
    @State private var showingHelp = false
    private var mode: RoutingMode { store.configuration.routingMode }

    var body: some View {
        Surface {
            HStack {
                Text("分流规则").font(.headline)
                Button { showingHelp.toggle() } label: { Image(systemName: "questionmark.circle") }
                    .buttonStyle(.borderless).foregroundStyle(.secondary)
                    .accessibilityLabel("查看分流说明").help("连接边界与分流优先级")
                    .popover(isPresented: $showingHelp) { ConnectionBoundaryHelpView(mode: mode) }
                Spacer()
                ModeBadge(mode: mode)
            }
            Text(store.accessExplanation).font(.callout).foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true).padding(.top, 8).padding(.bottom, 12)
            Divider()
            HStack {
                Label(mode == .global ? "直连 App / 服务" : "白名单 App / 服务", systemImage: mode == .global ? "arrow.triangle.branch" : "square.grid.2x2")
                Spacer()
                Text("\(store.configuration.routingApplications.count) 个").foregroundStyle(.secondary).monospacedDigit()
                Button("管理名单") {
                    store.navigate(to: mode == .global ? .exclusions : .applications)
                }.appInlineActionStyle()
            }.font(.callout).padding(.vertical, 12)
            Divider()
            HStack {
                Label("直连域名与 IP", systemImage: "globe")
                Spacer()
                Text("\(store.configuration.directDomains.count) 条").foregroundStyle(.secondary).monospacedDigit()
                Button("配置规则") {
                    if store.profileDraft == nil { store.beginEditingProfile(store.configuration.activeProfileID) }
                    else { store.navigate(to: .profiles) }
                }.appInlineActionStyle()
            }.font(.callout).padding(.vertical, 12)
            if store.usesCompanyRoutes {
                Divider()
                LabeledContent("公司内网网段", value: "\(store.configuration.activeConnection.remoteNetworks.count) 个")
                    .font(.callout).padding(.top, 12)
            }
        }
    }
}

struct ConnectionBoundaryHelpView: View {
    let mode: RoutingMode

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 8) {
                Image(systemName: "shield.lefthalf.filled")
                    .foregroundStyle(AppTheme.accent)
                Text("连接边界与分流说明")
                    .font(.headline)
            }
            Divider()
            CompactInfoRow(
                title: "未列入名单的 App",
                detail: mode == .global ? "通过当前线路" : "保持原网络",
                symbol: mode == .global ? "network" : "arrow.turn.down.right",
                color: mode == .global ? AppTheme.accent : .secondary
            )
            CompactInfoRow(
                title: "直连规则",
                detail: "始终优先于 App 模式",
                symbol: "arrow.triangle.branch"
            )
            CompactInfoRow(
                title: "本地访问",
                detail: "局域网与 Tailscale 保留原路径",
                symbol: "checkmark.shield",
                color: AppTheme.success
            )
            Divider()
            Text("GoConnect 不会启动或重启目标 App。")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(16)
        .frame(width: 290)
    }
}

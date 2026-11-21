import SwiftUI
import GoConnectCore

struct ApplicationRoutingCard: View {
    @Bindable var store: AppStore
    private var mode: RoutingMode { store.configuration.routingMode }

    var body: some View {
        ConnectionSummaryLink(title: "分流规则", symbol: "arrow.triangle.branch", detail: mode.title) {
            store.navigate(to: mode == .global ? .exclusions : .applications)
        }
        .help(store.accessExplanation)
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

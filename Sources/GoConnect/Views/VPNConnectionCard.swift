import SwiftUI
import Combine
import GoConnectCore

struct VPNConnectionCard: View {
    @Bindable var store: AppStore

    @State private var currentTime = Date()

    private let timer = Timer.publish(every: 1.0, on: .main, in: .common).autoconnect()

    private var state: VPNControlState {
        store.connectionControlState
    }

    private var canStart: Bool {
        store.localReady && store.configurationReadable && !store.recoveryRequired
    }

    private var metricsActive: Bool { state == .connected || (state == .diagnostic && store.phase == "testing") }
    private var hasSession: Bool { metricsActive || state == .reconnecting }
    private var trafficFresh: Bool {
        store.trafficAvailable && store.trafficUpdatedAt.map { currentTime.timeIntervalSince($0) < 5 } == true
    }
    private var isProxyLine: Bool { store.configuration.activeConnection.subscription != nil }

    private var statusColor: Color {
        if state.needsAttention { return AppTheme.warning }
        if state == .connected { return AppTheme.success }
        return state == .off ? .secondary : AppTheme.accent
    }

    private var durationString: String {
        guard hasSession, let connectedAt = store.connectedAt else {
            return "00:00:00"
        }
        let elapsed = max(0, currentTime.timeIntervalSince(connectedAt))
        return NetworkFormatters.formatDuration(elapsed)
    }

    private var addressSummary: String {
        if !hasSession {
            return "—"
        }
        if isProxyLine { return "不适用" }
        if store.addresses.isEmpty {
            return "未上报"
        }
        if let first = store.addresses.first {
            return store.addresses.count > 1 ? "\(first) (+\(store.addresses.count - 1))" : first
        }
        return "—"
    }

    var body: some View {
        Surface(emphasized: state == .connected) {
            VStack(alignment: .leading, spacing: 18) {
                // Status and connection control share a stable row.
                HStack(alignment: .center, spacing: 16) {
                    statusIcon

                    VStack(alignment: .leading, spacing: 4) {
                        HStack(spacing: 8) {
                            Text(state.title)
                                .font(.title2.weight(.bold))
                            if state.isWorking {
                                ProgressView()
                                    .controlSize(.small)
                            }
                        }
                        Text(state == .off || state == .connected ? "当前线路：\(store.configuration.activeConnection.displayName)" : store.connectionControlSubtitle)
                            .font(.callout)
                            .foregroundStyle(.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)

                    connectionToggle
                }

                // Real-time network metrics grid
                metricsGrid

                publicIPRow

                Divider()

                // Keep the label on its own row so the picker and action share a baseline.
                VStack(alignment: .leading, spacing: 6) {
                    Text("当前线路")
                        .font(.caption)
                        .foregroundStyle(.secondary)

                    HStack(alignment: .center, spacing: 10) {
                        if store.busy || state.isOn {
                            Text(store.configuration.activeConnection.displayName)
                                .font(.body).foregroundStyle(.primary)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        } else {
                            SavedConnectionsView(store: store)
                                .labelsHidden().controlSize(.regular)
                                .frame(maxWidth: .infinity).help("选择已保存的线路")
                        }

                        Button {
                            store.beginEditingProfile(store.configuration.activeProfileID)
                        } label: {
                            Label("线路设置", systemImage: "slider.horizontal.3")
                        }
                        .appActionStyle()
                        .fixedSize(horizontal: true, vertical: false)
                        .help("当前分流模式：\(store.accessTitle)")
                    }
                    if store.busy || state.isOn {
                        Text("结束连接或诊断后可切换线路").font(.caption).foregroundStyle(.secondary)
                    }
                }

                if state == .diagnostic {
                    Button("停止诊断") {
                        store.disconnect()
                    }
                    .controlSize(.regular)
                }
            }
        }
        .onAppear { currentTime = Date() }
        .onReceive(timer) { now in
            currentTime = now
        }
    }

    private var publicIPRow: some View {
        HStack(spacing: 12) {
            Label("公网出口 IP", systemImage: "globe")
                .foregroundStyle(.secondary)
            Text(!hasSession ? "连接后检测" : store.publicIPChecking ? "检测中…" : store.publicIP ?? (store.publicIPFailed ? "检测失败" : "待检测"))
                .font(.system(.callout, design: .monospaced))
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: 0)
            if let checked = store.publicIPCheckedAt, hasSession {
                Text(checked, style: .time).font(.caption).foregroundStyle(.secondary)
                    .help("上次出口 IP 检测时间")
            }
            Button("刷新出口 IP", systemImage: "arrow.clockwise") { store.refreshPublicIP() }
                .labelStyle(.iconOnly).buttonStyle(.borderless)
                .disabled(!metricsActive || store.publicIPChecking)
                .help("通过当前线路检测公网出口 IP")
        }
        .font(.callout)
        .help("通过所选线路的代理检测；这是该线路的公网出口 IP，不是隧道虚拟地址。")
    }

    private var statusIcon: some View {
        Image(systemName: state == .off ? "network" : state.symbol)
            .font(.system(size: 26, weight: .medium))
            .foregroundStyle(statusColor)
            .frame(width: 52, height: 52)
            .background(statusColor.opacity(0.08), in: RoundedRectangle(cornerRadius: 14))
            .accessibilityHidden(true)
    }

    private var connectionToggle: some View {
        HStack(spacing: 12) {
            Text("VPN 连接").font(.subheadline.weight(.medium)).foregroundStyle(.primary)

            Toggle("VPN 连接", isOn: Binding(
                get: { state.isOn },
                set: { enabled in
                    if enabled != state.isOn {
                        store.setConnectionEnabled(enabled)
                    }
                }
            ))
            .labelsHidden()
            .toggleStyle(.switch)
            .controlSize(.regular)
            .tint(AppTheme.accent)
            .disabled(!state.canToggle || (!canStart && !state.isOn))
            .accessibilityLabel("VPN 连接开关")
            .accessibilityHint(state.isOn ? "关闭以断开连接" : "开启所选线路")
            .help(state.isOn ? "关闭 VPN 连接" : "开启 VPN 连接")
        }
    }

    private var metricsGrid: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 12) {
                metricTiles
            }
            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 12) {
                metricTiles
            }
        }
    }

    @ViewBuilder
    private var metricTiles: some View {
        MetricTile(
            title: "下行速率",
            value: metricsActive && trafficFresh ? NetworkFormatters.formatRate(store.downloadRate) : "—",
            secondary: "\(metricsActive && !trafficFresh ? "统计待更新 · 已接收" : "已接收") \(NetworkFormatters.formatBytes(store.received))",
            symbol: "arrow.down"
        )
        MetricTile(
            title: "上行速率",
            value: metricsActive && trafficFresh ? NetworkFormatters.formatRate(store.uploadRate) : "—",
            secondary: "\(metricsActive && !trafficFresh ? "统计待更新 · 已发送" : "已发送") \(NetworkFormatters.formatBytes(store.sent))",
            symbol: "arrow.up"
        )
        MetricTile(
            title: "连接时长",
            value: hasSession ? durationString : "—",
            secondary: state == .reconnecting ? "会话保留，恢复中" : metricsActive ? "会话活跃中" : "未建立会话",
            symbol: "timer"
        )
        MetricTile(
            title: "虚拟地址",
            value: addressSummary,
            secondary: isProxyLine ? "代理线路不分配隧道地址" : store.addresses.isEmpty ? "尚未收到隧道地址" : "\(store.addresses.count) 个\(state == .reconnecting ? "已保留" : "活跃")地址",
            symbol: "network",
            tooltip: isProxyLine ? "HY2 等代理线路不分配远端隧道地址；公网出口见下方独立检测结果。" : "隧道分配的虚拟地址，非公网出口 IP。\n" + (store.addresses.isEmpty ? "尚未收到隧道地址" : store.addresses.joined(separator: "\n"))
        )
    }
}

private struct MetricTile: View {
    let title: String
    let value: String
    let secondary: String
    let symbol: String
    var tooltip: String? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Image(systemName: symbol)
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(.secondary)
                Text(title)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Text(value)
                .font(.system(size: 16, weight: .semibold, design: .monospaced))
                .foregroundStyle(.primary)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
            Text(secondary)
                .font(.caption2)
                .foregroundStyle(.secondary)
                .lineLimit(1)
        }
        .padding(12)
        .frame(minWidth: 130, maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(title)，\(value)，\(secondary)")
        .help(tooltip ?? "\(title): \(value)")
    }
}

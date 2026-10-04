import SwiftUI
import Combine
import GoConnectCore

struct VPNConnectionCard: View {
    @Bindable var store: AppStore

    @State private var lastReceived: UInt64 = 0
    @State private var lastSent: UInt64 = 0
    @State private var lastSampleTime: Date = Date()
    @State private var downRate: Double = 0
    @State private var upRate: Double = 0
    @State private var currentTime = Date()

    private let timer = Timer.publish(every: 1.0, on: .main, in: .common).autoconnect()

    private var state: VPNControlState {
        store.connectionControlState
    }

    private var canStart: Bool {
        store.localReady && store.configurationReadable && !store.recoveryRequired
    }

    private var statusColor: Color {
        if state.needsAttention { return AppTheme.warning }
        if state == .connected { return AppTheme.success }
        return state == .off ? .secondary : AppTheme.accent
    }

    private var durationString: String {
        guard state == .connected, let connectedAt = store.connectedAt else {
            return "00:00:00"
        }
        let elapsed = max(0, currentTime.timeIntervalSince(connectedAt))
        return NetworkFormatters.formatDuration(elapsed)
    }

    private var addressSummary: String {
        if state != .connected {
            return "—"
        }
        if store.addresses.isEmpty {
            return "分配中..."
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
        .onAppear {
            if state == .connected {
                lastReceived = store.received
                lastSent = store.sent
                lastSampleTime = Date()
                currentTime = Date()
            }
        }
        .onReceive(timer) { now in
            currentTime = now
            if state == .connected {
                let delta = now.timeIntervalSince(lastSampleTime)
                if delta >= 0.5 {
                    let rxDelta = store.received >= lastReceived ? store.received - lastReceived : 0
                    let txDelta = store.sent >= lastSent ? store.sent - lastSent : 0
                    downRate = Double(rxDelta) / delta
                    upRate = Double(txDelta) / delta
                    lastReceived = store.received
                    lastSent = store.sent
                    lastSampleTime = now
                }
            } else {
                downRate = 0
                upRate = 0
            }
        }
        .onChange(of: state) { _, newState in
            if newState == .connected {
                lastReceived = store.received
                lastSent = store.sent
                lastSampleTime = Date()
                currentTime = Date()
            } else if newState == .off {
                downRate = 0
                upRate = 0
                lastReceived = 0
                lastSent = 0
            }
        }
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
            value: state == .connected ? NetworkFormatters.formatRate(downRate) : "—",
            secondary: "\(state == .connected ? "已接收" : "上次接收") \(NetworkFormatters.formatBytes(store.received))",
            symbol: "arrow.down"
        )
        MetricTile(
            title: "上行速率",
            value: state == .connected ? NetworkFormatters.formatRate(upRate) : "—",
            secondary: "\(state == .connected ? "已发送" : "上次发送") \(NetworkFormatters.formatBytes(store.sent))",
            symbol: "arrow.up"
        )
        MetricTile(
            title: "连接时长",
            value: state == .connected ? durationString : "—",
            secondary: state == .connected ? "会话活跃中" : "未建立会话",
            symbol: "timer"
        )
        MetricTile(
            title: "虚拟地址",
            value: addressSummary,
            secondary: store.addresses.isEmpty ? "待分配虚拟地址" : "\(store.addresses.count) 个活跃地址",
            symbol: "network",
            tooltip: "隧道分配的虚拟地址，非公网出口 IP。\n" + (store.addresses.isEmpty ? "未分配虚拟地址" : store.addresses.joined(separator: "\n"))
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

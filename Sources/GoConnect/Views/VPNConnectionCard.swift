import SwiftUI
import Combine
import GoConnectCore

struct VPNConnectionCard: View {
    @Bindable var store: AppStore

    @State private var isPulsing = false
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
                // Top header: Status icon with breathing glow, state titles, and connection Toggle
                HStack(alignment: .center, spacing: 16) {
                    statusGlowIcon

                    VStack(alignment: .leading, spacing: 4) {
                        HStack(spacing: 8) {
                            Text(state.title)
                                .font(.title2.weight(.bold))
                            if state.isWorking {
                                ProgressView()
                                    .controlSize(.small)
                            }
                            if state == .connected {
                                StatusPill(title: "已保护", symbol: "checkmark.shield.fill", color: AppTheme.success)
                            }
                        }
                        Text(store.connectionControlSubtitle)
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
                        SavedConnectionsView(store: store)
                            .labelsHidden()
                            .controlSize(.regular)
                            .frame(maxWidth: .infinity)

                        Button {
                            store.beginEditingProfile(store.configuration.activeProfileID)
                        } label: {
                            Label("线路设置", systemImage: "slider.horizontal.3")
                        }
                        .appActionStyle()
                        .fixedSize(horizontal: true, vertical: false)
                        .help("当前分流模式：\(store.accessTitle)")
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

    private var statusGlowIcon: some View {
        ZStack {
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .fill(statusColor)
                .frame(width: 52, height: 52)
                .scaleEffect(isPulsing ? 1.25 : 0.95)
                .opacity((state == .connected || state.isWorking) ? (isPulsing ? 0.35 : 0.12) : 0)
                .blur(radius: isPulsing ? 8 : 3)
                .animation(.easeInOut(duration: 0.3), value: state == .connected || state.isWorking)

            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .fill(statusColor.opacity(state == .off ? 0.08 : 0.15))
                .frame(width: 50, height: 50)
                .overlay(
                    RoundedRectangle(cornerRadius: 14, style: .continuous)
                        .strokeBorder(statusColor.opacity(state == .off ? 0.12 : 0.35), lineWidth: 1)
                )

            Image(systemName: state.symbol)
                .font(.system(size: 22, weight: .semibold))
                .foregroundStyle(statusColor)
        }
        .frame(width: 58, height: 58)
        .onAppear {
            withAnimation(.easeInOut(duration: 1.6).repeatForever(autoreverses: true)) {
                isPulsing = true
            }
        }
    }

    private var connectionToggle: some View {
        HStack(spacing: 12) {
            VStack(alignment: .trailing, spacing: 2) {
                Text(state.isOn ? "开启" : "断开")
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(state.isOn ? statusColor : .secondary)
                Text(state.isOn ? "点击断开连接" : "点击开启线路")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }

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
            .controlSize(.large)
            .tint(state.needsAttention ? .orange : (state == .connected ? AppTheme.success : AppTheme.accent))
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
            value: NetworkFormatters.formatRate(downRate),
            secondary: "总接收 \(NetworkFormatters.formatBytes(store.received))",
            symbol: "arrow.down.circle.fill",
            symbolColor: .blue
        )
        MetricTile(
            title: "上行速率",
            value: NetworkFormatters.formatRate(upRate),
            secondary: "总发送 \(NetworkFormatters.formatBytes(store.sent))",
            symbol: "arrow.up.circle.fill",
            symbolColor: .purple
        )
        MetricTile(
            title: "连接时长",
            value: durationString,
            secondary: state == .connected ? "会话活跃中" : "未建立会话",
            symbol: "timer",
            symbolColor: .orange
        )
        MetricTile(
            title: "出口 / 虚拟 IP",
            value: addressSummary,
            secondary: store.addresses.isEmpty ? "待分配虚拟地址" : "\(store.addresses.count) 个活跃地址",
            symbol: "network",
            symbolColor: .teal,
            tooltip: store.addresses.isEmpty ? "未分配虚拟地址" : store.addresses.joined(separator: "\n")
        )
    }
}

private struct MetricTile: View {
    let title: String
    let value: String
    let secondary: String
    let symbol: String
    let symbolColor: Color
    var tooltip: String? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Image(systemName: symbol)
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(symbolColor)
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
                .foregroundStyle(.tertiary)
                .lineLimit(1)
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: AppTheme.itemRadius, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: AppTheme.itemRadius, style: .continuous)
                .strokeBorder(AppTheme.subtleBorder, lineWidth: 1)
        )
        .help(tooltip ?? "\(title): \(value)")
    }
}

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
    private var lineLocked: Bool { store.busy || state.isOn }
    private var lineName: String { store.configuration.activeConnection.displayName }

    private var statusColor: Color {
        if state.needsAttention { return AppTheme.warning }
        if state == .connected { return AppTheme.success }
        return state == .off ? .secondary : AppTheme.accent
    }

    /// The badge shape distinguishes each kind of state without relying on its color.
    private var statusBadge: String? {
        switch state {
        case .off: nil
        case .connected, .needsRestart, .interrupted, .failed: state.symbol
        case .reconnecting: "arrow.clockwise.circle.fill"
        case .disconnecting, .endingDiagnostic: "stop.circle.fill"
        case .diagnostic: "stethoscope.circle.fill"
        case .waitingForPassword, .preparingService, .maintainingService, .connecting, .authorizing: "ellipsis.circle.fill"
        }
    }

    /// Transitional and failed states keep the store's explanation. A healthy connection's
    /// routing explanation is shown by ApplicationRoutingCard on the same page.
    private var stateExplanation: String? {
        switch state {
        case .connected: nil
        case .off: canStart && !store.busy ? store.statusSubtitle : nil
        default: store.connectionControlSubtitle
        }
    }

    private var durationString: String {
        guard hasSession, let connectedAt = store.connectedAt else {
            return "—"
        }
        let elapsed = max(0, currentTime.timeIntervalSince(connectedAt))
        return NetworkFormatters.formatDuration(elapsed)
    }

    private var durationDetail: String {
        if metricsActive { return "会话活跃中" }
        switch state {
        case .reconnecting: return "会话保留，恢复中"
        case .authorizing: return "分流准备中"
        case .disconnecting, .endingDiagnostic: return "会话结束中"
        case .interrupted: return "会话已中断"
        case .needsRestart: return "时长暂不可用"
        default: return "未建立会话"
        }
    }

    /// Totals survive in the store until the next connect resets them, so anything shown
    /// outside a session is labelled as belonging to the previous one.
    private func trafficDetail(_ verb: String, bytes: UInt64) -> String {
        let total = "\(verb) \(NetworkFormatters.formatBytes(bytes))"
        let sampled = store.trafficUpdatedAt != nil
        if metricsActive {
            if trafficFresh { return total }
            return sampled ? "统计待更新 · \(total)" : "暂无统计数据"
        }
        switch state {
        case .reconnecting: return sampled ? "恢复中 · \(total)" : "暂无统计数据"
        case .authorizing, .interrupted, .needsRestart: return sampled ? total : "暂无统计数据"
        default: return store.received > 0 || store.sent > 0 ? "上次会话 · \(total)" : "连接后统计"
        }
    }

    private var addressSummary: String {
        guard hasSession else { return "连接后显示" }
        guard let first = store.addresses.first else { return "未上报" }
        return store.addresses.count > 1 ? "\(first) (+\(store.addresses.count - 1))" : first
    }

    private var addressDetail: String? {
        guard hasSession else { return nil }
        if store.addresses.isEmpty { return "尚未收到隧道地址" }
        return "\(store.addresses.count) 个\(state == .reconnecting ? "已保留" : "活跃")地址"
    }

    private var addressHelp: String {
        let note = "隧道分配的虚拟地址，非公网出口 IP。"
        guard hasSession else { return note }
        return note + "\n" + (store.addresses.isEmpty ? "尚未收到隧道地址" : store.addresses.joined(separator: "\n"))
    }

    private var addressAccessibilityValue: String {
        guard hasSession, !store.addresses.isEmpty else { return addressSummary }
        return store.addresses.joined(separator: "，") + "，" + (addressDetail ?? "")
    }

    var body: some View {
        Surface {
            VStack(alignment: .leading, spacing: 14) {
                // Status, the single line presentation and controls share one header.
                header

                metricsGrid

                Divider()

                publicIPRow

                if !isProxyLine {
                    Divider()
                    tunnelAddressRow
                }
            }
        }
        .onAppear { currentTime = Date() }
        .onReceive(timer) { now in
            currentTime = now
        }
    }

    private var header: some View {
        HStack(alignment: .top, spacing: 16) {
            statusIcon

            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 8) {
                    Text(state.title)
                        .font(.system(size: 28, weight: .semibold))
                        .fixedSize(horizontal: false, vertical: true)
                    if state.isWorking {
                        ProgressView()
                            .controlSize(.small)
                    }
                }
                lineSummary
                if let stateExplanation {
                    Text(stateExplanation)
                        .font(.callout)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)

            controls
        }
    }

    private var lineSummary: some View {
        HStack(spacing: 6) {
            if lineLocked {
                Text(lineName)
                    .font(.title3)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .help("当前线路：\(lineName)")
                    .accessibilityLabel("当前线路：\(lineName)")
            } else {
                SavedConnectionsView(store: store)
                    .labelsHidden().controlSize(.regular)
                    .frame(maxWidth: 280).help("选择已保存的线路")
            }
            Text(lineLocked ? "· \(store.accessTitle)" : store.accessTitle)
                .font(lineLocked ? .title3 : .callout)
                .foregroundStyle(.secondary)
                .fixedSize()
                .accessibilityLabel("分流模式：\(store.accessTitle)")
        }
    }

    private var controls: some View {
        VStack(alignment: .trailing, spacing: 10) {
            connectionToggle

            HStack(spacing: 8) {
                if state == .diagnostic {
                    Button("停止诊断") {
                        store.disconnect()
                    }
                    .appActionStyle()
                }

                Button {
                    store.beginEditingProfile(store.configuration.activeProfileID)
                } label: {
                    Label("线路设置", systemImage: "slider.horizontal.3")
                }
                .appActionStyle()
                .help("当前分流模式：\(store.accessTitle)")
            }

            if lineLocked {
                Text("结束连接或诊断后可切换线路").font(.subheadline).foregroundStyle(.secondary)
            }
        }
        .fixedSize(horizontal: true, vertical: false)
    }

    private var publicIPRow: some View {
        NetworkInfoRow(title: "公网出口 IP", symbol: "globe") {
            Text(!hasSession ? "连接后检测" : store.publicIPChecking ? "检测中…" : store.publicIP ?? (store.publicIPFailed ? "检测失败" : "待检测"))
                .font(.system(.callout, design: .monospaced))
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
        } trailing: {
            if let checked = store.publicIPCheckedAt, hasSession {
                Text("检测于 \(Text(checked, style: .time))").font(.subheadline).monospacedDigit().foregroundStyle(.secondary)
                    .help("上次出口 IP 检测时间")
            }
            Button("刷新出口 IP", systemImage: "arrow.clockwise") { store.refreshPublicIP() }
                .labelStyle(.iconOnly).buttonStyle(.borderless)
                .disabled(!metricsActive || store.publicIPChecking)
                .help("通过当前线路检测公网出口 IP")
        }
        .help("通过所选线路的代理检测；这是该线路的公网出口 IP，不是隧道虚拟地址。")
    }

    private var tunnelAddressRow: some View {
        NetworkInfoRow(title: "隧道虚拟地址", symbol: "network") {
            Text(addressSummary)
                .font(.system(.callout, design: .monospaced))
                .fixedSize(horizontal: false, vertical: true)
        } trailing: {
            if let addressDetail {
                Text(addressDetail).font(.subheadline).monospacedDigit().foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("隧道虚拟地址，非公网出口 IP")
        .accessibilityValue(addressAccessibilityValue)
        .help(addressHelp)
    }

    private var statusIcon: some View {
        Image(systemName: "network")
            .font(.system(size: 28, weight: .regular))
            .foregroundStyle(state == .off || state.needsAttention ? Color.secondary : AppTheme.accent)
            .frame(width: 56, height: 56)
            .background(statusColor.opacity(0.08), in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            .overlay(alignment: .bottomTrailing) {
                if let statusBadge {
                    Image(systemName: statusBadge)
                        .font(.system(size: 18, weight: .semibold))
                        .foregroundStyle(statusColor)
                        .background(AppTheme.controlBackground, in: Circle())
                        .offset(x: 4, y: 4)
                }
            }
            .accessibilityHidden(true)
    }

    private var connectionToggle: some View {
        HStack(spacing: 12) {
            Text("VPN 连接").font(.body.weight(.medium)).foregroundStyle(.primary)
                .accessibilityHidden(true)

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
            HStack(alignment: .top, spacing: 16) {
                downloadMetric
                Divider()
                uploadMetric
                Divider()
                durationMetric
            }
            .fixedSize(horizontal: false, vertical: true)

            VStack(alignment: .leading, spacing: 10) {
                downloadMetric
                Divider()
                uploadMetric
                Divider()
                durationMetric
            }
        }
    }

    private var downloadMetric: some View {
        MetricColumn(
            title: "下行速率",
            value: metricsActive && trafficFresh ? NetworkFormatters.formatRate(store.downloadRate) : "—",
            detail: trafficDetail("已接收", bytes: store.received),
            symbol: "arrow.down"
        )
    }

    private var uploadMetric: some View {
        MetricColumn(
            title: "上行速率",
            value: metricsActive && trafficFresh ? NetworkFormatters.formatRate(store.uploadRate) : "—",
            detail: trafficDetail("已发送", bytes: store.sent),
            symbol: "arrow.up"
        )
    }

    private var durationMetric: some View {
        MetricColumn(
            title: "连接时长",
            value: durationString,
            detail: durationDetail,
            symbol: "clock"
        )
    }
}

private struct MetricColumn: View {
    let title: String
    let value: String
    let detail: String
    let symbol: String

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Label(title, systemImage: symbol)
                .font(.callout)
                .foregroundStyle(.secondary)
            MetricValueText(value: value)
            Text(detail)
                .font(.subheadline)
                .monospacedDigit()
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(minWidth: 150, idealWidth: 160, maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(title)，\(value)，\(detail)")
        .help("\(title): \(value)")
    }
}

/// Keeps a long value such as an IPv6 address readable by moving it to its own line.
private struct NetworkInfoRow<Value: View, Trailing: View>: View {
    let title: String
    let symbol: String
    let value: Value
    let trailing: Trailing

    init(title: String, symbol: String, @ViewBuilder value: () -> Value, @ViewBuilder trailing: () -> Trailing) {
        self.title = title; self.symbol = symbol
        self.value = value(); self.trailing = trailing()
    }

    private var label: some View {
        Label(title, systemImage: symbol)
            .foregroundStyle(.secondary)
            .frame(minWidth: 104, alignment: .leading)
    }

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 12) {
                label
                value
                Spacer(minLength: 8)
                HStack(spacing: 8) { trailing }
            }

            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 12) {
                    label
                    Spacer(minLength: 8)
                    HStack(spacing: 8) { trailing }
                }
                value
            }
        }
        .font(.callout)
    }
}

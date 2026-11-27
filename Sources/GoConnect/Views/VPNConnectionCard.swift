import SwiftUI
import GoConnectCore

struct VPNConnectionCard: View {
    @Bindable var store: AppStore
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.appearsActive) private var appearsActive

    @State private var showingDetails = false
    @State private var currentTime = Date()

    private var state: VPNControlState {
        store.connectionControlState
    }

    private var canStart: Bool {
        store.localReady && store.configurationReadable && !store.recoveryRequired && (!Product.isNetworkTestApp || store.networkService.testHostAvailable)
    }

    private var metricsActive: Bool { state == .connected || (state == .diagnostic && store.phase == "testing") }
    private var hasSession: Bool { metricsActive || state == .reconnecting }
    private var connectionPageActive: Bool { appearsActive && store.page == .connection }
    private var updatesSessionClock: Bool { connectionPageActive && hasSession }
    private var trafficFresh: Bool {
        store.trafficAvailable && store.trafficUpdatedAt.map { currentTime.timeIntervalSince($0) < 5 } == true
    }
    private var isProxyLine: Bool { store.configuration.activeConnection.subscription != nil }
    private var lineLocked: Bool { store.busy || state.isOn }
    private var lineName: String { store.configuration.activeConnection.displayName }

    private var degraded: Bool { state == .connected && store.pathHealth?.state == "degraded" }
    private var isHealthyConnection: Bool { state == .connected && !degraded }

    private var statusColor: Color {
        if state.needsAttention || degraded { return AppTheme.warning }
        if state == .connected { return AppTheme.success }
        return state == .off ? .secondary : AppTheme.accent
    }

    /// The badge shape distinguishes each kind of state without relying on its color.
    private var statusBadge: String? {
        if degraded { return "exclamationmark.circle.fill" }
        return switch state {
        case .off: nil
        case .connected, .needsRestart, .interrupted, .failed: state.symbol
        case .reconnecting: "arrow.clockwise.circle.fill"
        case .disconnecting, .endingDiagnostic: "stop.circle.fill"
        case .diagnostic: "stethoscope.circle.fill"
        case .waitingForPassword, .preparingService, .maintainingService, .connecting, .authorizing: "ellipsis.circle.fill"
        }
    }

    private var stateExplanation: String? {
        switch state {
        case .connected: store.pathHealth?.statusDetail
        case .off: nil
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
        case .reconnecting: return "恢复中"
        case .authorizing: return "分流准备中"
        case .disconnecting, .endingDiagnostic: return "会话结束中"
        case .interrupted: return "会话已中断"
        case .needsRestart: return "时长暂不可用"
        default: return "未建立会话"
        }
    }

    /// Totals survive in the store until the next connect resets them, so anything shown
    /// outside a session is labelled as belonging to the previous one.
    private var trafficDetail: (showsTotals: Bool, note: String?) {
        let sampled = store.trafficUpdatedAt != nil
        if metricsActive {
            if trafficFresh { return (true, nil) }
            return sampled ? (true, "统计待更新") : (false, "暂无统计数据")
        }
        switch state {
        case .reconnecting: return sampled ? (true, "恢复中") : (false, "暂无统计数据")
        case .authorizing, .interrupted, .needsRestart: return sampled ? (true, nil) : (false, "暂无统计数据")
        default: return store.received > 0 || store.sent > 0 ? (true, "上次会话") : (false, "连接后统计")
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
        VStack(spacing: 12) {
            Surface {
                VStack(spacing: 12) {
                    powerControl
                        .background {
                            if isHealthyConnection && !reduceMotion {
                                breathingRing
                            }
                        }
                    VStack(spacing: 5) {
                        HStack(spacing: 7) {
                            if let statusBadge {
                                Image(systemName: statusBadge)
                                    .font(.callout.weight(.semibold))
                                    .foregroundStyle(statusColor)
                                    .accessibilityHidden(true)
                            }
                            Text(store.connectionDisplayTitle)
                                .font(.system(size: 28, weight: .semibold))
                            if state.isWorking {
                                ProgressView().controlSize(.small)
                            }
                        }
                        Text(store.accessTitle)
                            .font(.callout)
                            .foregroundStyle(.secondary)
                            .accessibilityLabel("分流模式：\(store.accessTitle)")
                        if let stateExplanation {
                            Text(stateExplanation)
                                .font(.callout)
                                .foregroundStyle(.secondary)
                        }
                    }
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)

                    if hasSession { sessionMetrics }
                    if state == .diagnostic {
                        Button("停止诊断", systemImage: "stop.circle") { store.disconnect(source: .diagnosticStop) }
                            .appActionStyle()
                    }
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, 10)
            }

            Surface {
                VStack(alignment: .leading, spacing: 10) {
                    lineRow
                    Divider()
                    connectionDetails
                }
            }
        }
        .task(id: updatesSessionClock) {
            guard updatesSessionClock else { return }
            while !Task.isCancelled {
                currentTime = Date()
                do { try await Task.sleep(for: .seconds(1)) }
                catch { break }
            }
        }
    }

    @ViewBuilder
    private var powerControl: some View {
        if #available(macOS 26.0, *) {
            powerButton
                .buttonStyle(.glassProminent)
                .buttonBorderShape(.circle)
        } else {
            powerButton
                .buttonStyle(.borderedProminent)
                .buttonBorderShape(.capsule)
        }
    }

    private var breathingRing: some View {
        TimelineView(.animation(minimumInterval: 1.0 / 60, paused: !connectionPageActive)) { timeline in
            // Only the phase changes each frame. Layout stays tied to the power button.
            let angle = timeline.date.timeIntervalSinceReferenceDate
                .truncatingRemainder(dividingBy: 3) * 2 * Double.pi / 3
            let expansion = (1 - cos(angle)) / 2
            Circle()
                .strokeBorder(AppTheme.success, lineWidth: 2)
                .scaleEffect(1.08 + 0.24 * expansion)
                .opacity(0.36 - 0.26 * expansion)
        }
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }

    private var powerButton: some View {
        let actionTitle = state.isOn ? "断开主线路" : "连接主线路"
        return Button {
            store.setConnectionEnabled(!state.isOn, source: .mainWindow)
        } label: {
            Image(systemName: "power")
                .font(.system(size: 25, weight: .medium))
                .frame(width: 48, height: 48)
        }
        .tint(isHealthyConnection ? AppTheme.success : AppTheme.accent)
        .disabled(!state.canToggle || (!canStart && !state.isOn))
        .accessibilityLabel(actionTitle)
        .accessibilityValue(store.connectionDisplayTitle)
        .help(actionTitle)
    }

    private var lineRow: some View {
        HStack(spacing: 12) {
            Image(systemName: "server.rack")
                .font(.body.weight(.medium))
                .foregroundStyle(AppTheme.accent)
                .frame(width: 34, height: 34)
                .background(AppTheme.selectedBackground, in: RoundedRectangle(cornerRadius: AppTheme.itemRadius))
                .accessibilityHidden(true)

            VStack(alignment: .leading, spacing: 4) {
                Text("当前线路").font(.caption).foregroundStyle(.secondary)
                if lineLocked {
                    Text(lineName)
                        .font(.headline)
                        .fixedSize(horizontal: false, vertical: true)
                        .help("结束连接或诊断后可切换线路")
                        .accessibilityLabel("当前线路：\(lineName)")
                } else {
                    SavedConnectionsView(store: store)
                        .labelsHidden()
                        .controlSize(.regular)
                }
                if let retained = store.activeRetainedSubscriptionNode {
                    Label("节点「\(retained.name)」已从订阅移除，正使用保留配置", systemImage: "info.circle")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)

            Button("线路设置", systemImage: "slider.horizontal.3") {
                store.beginEditingProfile(store.configuration.activeProfileID)
            }
            .appActionStyle()
            .fixedSize()
        }
    }

    private var sessionMetrics: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 16) {
                downloadMetric
                uploadMetric
                durationMetric
            }
            .fixedSize(horizontal: true, vertical: false)

            VStack(spacing: 4) {
                downloadMetric
                uploadMetric
                durationMetric
            }
        }
        .font(.callout)
        .monospacedDigit()
    }

    private var downloadMetric: some View {
        sessionMetric(
            "下行速率", icon: "arrow.down",
            value: metricsActive && trafficFresh ? NetworkFormatters.formatRate(store.downloadRate) : "—",
            color: downloadColor
        )
    }

    private var uploadMetric: some View {
        sessionMetric(
            "上行速率", icon: "arrow.up",
            value: metricsActive && trafficFresh ? NetworkFormatters.formatRate(store.uploadRate) : "—",
            color: uploadColor
        )
    }

    private var downloadColor: Color {
        AppTheme.downloadTint(for: colorScheme)
    }

    private var uploadColor: Color {
        AppTheme.uploadTint(for: colorScheme)
    }

    private var durationColor: Color {
        colorScheme == .dark ? Color(nsColor: .systemBlue) : Color(red: 0.08, green: 0.34, blue: 0.70)
    }

    private var durationMetric: some View {
        sessionMetric("连接时长", icon: "clock", value: durationString, color: durationColor)
    }

    private func sessionMetric(_ title: String, icon: String, value: String, color: Color) -> some View {
        Label(value, systemImage: icon)
            .foregroundStyle(value == "—" ? Color.secondary : color)
            .accessibilityLabel(title)
            .accessibilityValue(value)
            .help(title)
    }

    private var publicIPValue: String {
        guard hasSession else { return "连接后检测" }
        if store.publicIPChecking { return "检测中…" }
        return store.publicIP ?? (store.publicIPFailed ? "检测失败" : "待检测")
    }

    private var connectionDetails: some View {
        FullRowDisclosure("连接详情", isExpanded: $showingDetails, summary: hasSession ? "公网出口 IP：\(publicIPValue)" : nil) {
            detailsGrid
        } label: {
            HStack(spacing: 10) {
                Label("连接详情", systemImage: "info.circle")
                    .font(.callout.weight(.medium))
                Spacer(minLength: 8)
                if hasSession && !showingDetails {
                    Text(publicIPValue)
                        .font(.system(.caption, design: .monospaced))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
            }
        }
    }

    private var detailsGrid: some View {
        Grid(alignment: .leading, horizontalSpacing: 14, verticalSpacing: 0) {
            publicIPRow
            detailsDivider
            if !isProxyLine {
                tunnelAddressRow
                detailsDivider
            }
            trafficRow
            detailsDivider
            GridRow {
                detailLabel("连接时长", symbol: "clock")
                Text(durationString)
                    .foregroundStyle(durationString == "—" ? Color.secondary : durationColor)
                    .monospacedDigit()
                detailNote(durationDetail)
            }
        }
        .font(.callout)
        .padding(.horizontal, 10)
        .background(Color.primary.opacity(0.035), in: RoundedRectangle(cornerRadius: AppTheme.itemRadius))
    }

    private var detailsDivider: some View {
        Divider().gridCellUnsizedAxes(.horizontal)
    }

    private func detailLabel(_ title: String, symbol: String) -> some View {
        HStack(spacing: 7) {
            Image(systemName: symbol)
                .frame(width: 16)
                .accessibilityHidden(true)
            Text(title)
        }
        .foregroundStyle(.secondary)
        .fixedSize(horizontal: true, vertical: false)
        .frame(minHeight: 34)
    }

    private func detailNote(_ text: String?) -> some View {
        Text(text ?? "")
            .font(.caption)
            .foregroundStyle(.secondary)
            .fixedSize(horizontal: true, vertical: false)
    }

    private var publicIPRow: some View {
        GridRow {
            detailLabel("公网出口 IP", symbol: "globe")
            Text(publicIPValue)
                .font(.system(.callout, design: .monospaced))
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
                .help("通过所选线路的代理检测公网出口地址")
            HStack(spacing: 6) {
                if let checked = store.publicIPCheckedAt, hasSession {
                    Text("检测于 \(Text(checked, style: .time))")
                        .font(.caption)
                        .monospacedDigit()
                        .foregroundStyle(.secondary)
                        .fixedSize()
                        .help("上次出口 IP 检测时间")
                }
                Button {
                    store.refreshPublicIP()
                } label: {
                    Image(systemName: "arrow.clockwise")
                        .frame(width: 24, height: 24)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.borderless)
                .disabled(!metricsActive || store.publicIPChecking)
                .accessibilityLabel("刷新出口 IP")
                .help("通过当前线路检测公网出口 IP")
            }
            .gridColumnAlignment(.trailing)
        }
    }

    private var tunnelAddressRow: some View {
        GridRow {
            detailLabel("隧道虚拟地址", symbol: "network")
                .accessibilityHidden(true)
            Text(addressSummary)
                .font(.system(.callout, design: .monospaced))
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityLabel("隧道虚拟地址")
                .accessibilityValue(addressAccessibilityValue)
                .help(addressHelp)
            detailNote(addressDetail)
                .accessibilityHidden(true)
        }
    }

    private var trafficRow: some View {
        let detail = trafficDetail
        return GridRow {
            detailLabel("会话流量", symbol: "arrow.up.arrow.down")
            if detail.showsTotals {
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: 14) {
                        receivedTotal
                        sentTotal
                    }
                    .fixedSize(horizontal: true, vertical: false)
                    VStack(alignment: .leading, spacing: 3) {
                        receivedTotal
                        sentTotal
                    }
                    .padding(.vertical, 5)
                }
            } else {
                Text(detail.note ?? "—")
                    .foregroundStyle(.secondary)
            }
            detailNote(detail.showsTotals ? detail.note : nil)
        }
    }

    private var receivedTotal: some View {
        trafficTotal("已接收", bytes: store.received, color: downloadColor)
    }

    private var sentTotal: some View {
        trafficTotal("已发送", bytes: store.sent, color: uploadColor)
    }

    private func trafficTotal(_ title: String, bytes: UInt64, color: Color) -> some View {
        HStack(spacing: 5) {
            Text(title).foregroundStyle(.secondary)
            Text(NetworkFormatters.formatBytes(bytes))
                .foregroundStyle(color)
                .monospacedDigit()
        }
        .accessibilityElement(children: .combine)
    }
}

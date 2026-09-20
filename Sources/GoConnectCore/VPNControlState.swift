import Foundation

public enum VPNControlState: String, CaseIterable, Sendable {
    case off, waitingForPassword, preparingService, maintainingService, connecting, authorizing, connected, needsRestart
    case interrupted, disconnecting, diagnostic, endingDiagnostic, failed

    public init(phase: String, passwordRequested: Bool, testOnly: Bool, routingCheck: Bool, hasUnmanagedApps: Bool) {
        if phase == "stopping" {
            self = testOnly || routingCheck ? .endingDiagnostic : .disconnecting
        } else if (passwordRequested && testOnly) || phase == "testing" || phase == "checking" || routingCheck {
            self = .diagnostic
        } else if passwordRequested {
            self = .waitingForPassword
        } else {
            switch phase {
            case "preparingService": self = .preparingService
            case "serviceMaintenance": self = .maintainingService
            case "preparingCheckService": self = .diagnostic
            case "connecting": self = testOnly ? .diagnostic : .connecting
            case "authorizing": self = .authorizing
            case "connected": self = hasUnmanagedApps ? .needsRestart : .connected
            case "protected": self = .interrupted
            case "failed": self = .failed
            default: self = .off
            }
        }
    }

    /// The switch reflects the requested VPN session, not a diagnostic tunnel.
    public var isOn: Bool {
        [.waitingForPassword, .preparingService, .connecting, .authorizing, .connected, .needsRestart, .interrupted].contains(self)
    }
    public var canToggle: Bool { ![.preparingService, .maintainingService, .disconnecting, .diagnostic, .endingDiagnostic].contains(self) }
    public var isWorking: Bool { [.preparingService, .maintainingService, .connecting, .authorizing, .disconnecting, .diagnostic, .endingDiagnostic].contains(self) }
    public var needsAttention: Bool { [.interrupted, .needsRestart, .failed].contains(self) }
    public var title: String {
        switch self {
        case .off: "未连接"
        case .waitingForPassword: "等待输入密码"
        case .preparingService: "准备本机服务"
        case .maintainingService: "维护本机服务"
        case .connecting: "正在连接"
        case .authorizing: "准备透明分流"
        case .connected: "已连接"
        case .needsRestart: "部分应用需要重启"
        case .interrupted: "连接已中断"
        case .disconnecting: "正在断开"
        case .diagnostic: "诊断进行中"
        case .endingDiagnostic: "正在结束诊断"
        case .failed: "连接未完成"
        }
    }
    public var symbol: String {
        if needsAttention { return "exclamationmark.circle.fill" }
        return self == .connected ? "checkmark.circle.fill" : "circle.fill"
    }
}

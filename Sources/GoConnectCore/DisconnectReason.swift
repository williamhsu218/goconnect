import Foundation

/// Only classified core events become explanations. An exit status alone
/// cannot distinguish a server decision from network or TLS failure.
public enum DisconnectReason: Int, Sendable {
    case unknown = 0, server = 1, idleTimeout = 2, sessionExpired = 3, cookieRejected = 4
    case deadPeer = 5, tlsRead = 6, tlsWrite = 7, tls = 8, reconnect = 9

    public init?(diagnosticCode: String) {
        switch diagnosticCode {
        case "openconnect.server_disconnect": self = .server
        case "openconnect.idle_timeout": self = .idleTimeout
        case "openconnect.session_expired": self = .sessionExpired
        case "openconnect.cookie_rejected": self = .cookieRejected
        case "openconnect.dpd_timeout": self = .deadPeer
        case "openconnect.tls_read_failed": self = .tlsRead
        case "openconnect.tls_write_failed": self = .tlsWrite
        case "openconnect.tls_failed": self = .tls
        case "openconnect.reconnect_failed": self = .reconnect
        default: return nil
        }
    }

    public var explanation: String {
        switch self {
        case .unknown: "组件意外结束，尚未取得具体原因"
        case .server: "服务器发出了断开指令"
        case .idleTimeout: "服务器报告空闲超时"
        case .sessionExpired: "服务器报告会话到期"
        case .cookieRejected: "服务器拒绝了会话凭据"
        case .deadPeer: "对端存活检测超时"
        case .tlsRead: "TLS 读取失败"
        case .tlsWrite: "TLS 写入失败"
        case .tls: "TLS 连接失败"
        case .reconnect: "隧道重连失败"
        }
    }

    public func merging(_ next: Self) -> Self {
        // Preserve the specific first cause when a generic termination or
        // failed reconnect follows it. All observations remain in the log.
        if self == .unknown || (self == .server && [.idleTimeout, .sessionExpired, .cookieRejected].contains(next)) { return next }
        return self
    }

    public static func isCoreDiagnostic(_ event: FaultEvent) -> Bool {
        event.rawValue.hasPrefix("openconnect.")
    }
}

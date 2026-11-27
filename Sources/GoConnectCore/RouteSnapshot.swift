import Foundation

public struct AppRouteStatus: Codable, Sendable {
    public var path: String
    public var vpn: Int
    public var direct: Int
    public var managed: Int?
    public var unmanaged: Int?
}

public struct RouteCommandResult: Codable, Sendable {
    public var sequence: UInt64
    public var success: Bool
    public var message: String?
}

public struct NetworkPathCheck: Codable, Equatable, Sendable {
    public var result: String
    public var durationMs: Int64
    public var attempts: [NetworkProbeAttempt]?

    public var summary: String {
        let count = attempts?.count ?? 0
        guard count > 0 else {
            return result == "unavailable" ? "无法检测" : "\(result == "ok" ? "可用" : "探测失败") · 整轮 \(durationMs) ms"
        }
        let status = result == "ok" ? (count > 1 ? "备用目标可用" : "可用") : "探测失败"
        return "\(status) · \(count) 次共 \(durationMs) ms"
    }
}

public struct NetworkPathHealth: Codable, Equatable, Sendable {
    public var state: String
    public var issue: String?
    public var sampledAt: Int64
    public var proxyHTTPS: NetworkPathCheck
    public var proxyDoH: NetworkPathCheck
    public var directHTTPS: NetworkPathCheck

    public var warningTitle: String? {
        guard state == "degraded" else { return nil }
        return switch issue {
        case "proxy_doh": "代理 DNS 异常"
        case "proxy_https": "代理 HTTPS 异常"
        case "proxy_path": "代理线路异常"
        case "network": "网络探测异常"
        case "unstable": "网络波动"
        default: "线路降级"
        }
    }

    public var statusDetail: String? {
        if state == "checking" { return "检测到网络异常，正在复测。" }
        guard state == "degraded" else { return nil }
        return switch issue {
        case "proxy_doh": "代理 HTTPS 可用，代理 DoH 连续异常。"
        case "proxy_https": "代理 DoH 可用，代理 HTTPS 连续异常。"
        case "proxy_path": "原网络 HTTPS 可用，代理 HTTPS 和 DoH 连续异常。"
        case "network": "代理 HTTPS 和 DoH 连续异常，原网络可用性待确认。"
        case "unstable": "两次探测均有异常，异常路径发生了变化。"
        default: "代理探测连续失败，可在诊断中查看各路径结果。"
        }
    }

    /// Secondary diagnostic note shown only in DiagnosticsView; never alarms a healthy VPN tunnel.
    public var diagnosticDetail: String? {
        if let statusDetail { return statusDetail }
        if state == "reachable", !["", "ok", "unavailable"].contains(directHTTPS.result) {
            return "代理通道可用；原网络 HTTPS 探测异常，直连规则可能受限。"
        }
        return nil
    }
}

public struct RouteSnapshot: Codable, Sendable {
    public var failureCode: String?
    public var pathHealth: NetworkPathHealth?
    public var company: CompanyRouteState?
    public var state: String
    public var message: String?
    public var device: String?
    public var systemDevice: String?
    public var remoteNetworks: [String]?
    public var updatedAt: Int64
    public var vpn: Int
    public var direct: Int
    public var blocked: Int?
    public var unknown: Int
    public var vpnObserved: Bool
    public var apps: [AppRouteStatus]?
    public var captureMode: String?
    public var originalRoute: Bool?
    public var commandResult: RouteCommandResult?
    public var routingMode: RoutingMode?
    public var tailscaleActive: Bool?
    public var tailscaleRoutes: Int?
}

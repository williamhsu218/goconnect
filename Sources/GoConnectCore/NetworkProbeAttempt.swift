import Foundation

public struct NetworkProbeAttempt: Codable, Equatable, Sendable {
    public let probeTarget: String
    public let reason: String
    public let family: Int?
    public let durationMs: Int64?
    public let connectMs: Int64?
    public let tlsMs: Int64?
    public let responseMs: Int64?
    public let firstByteMs: Int64?
    public let httpStatus: Int?
    public let timedOut: Bool?

    public var targetLabel: String {
        let provider = ["cloudflare": "Cloudflare", "google": "Google", "alidns": "阿里云"][probeTarget] ?? probeTarget
        return family.map { "\(provider) · IPv\($0)" } ?? provider
    }

    public var resultLabel: String {
        switch reason {
        case "ok": "可用"
        case "connect": timedOut == true ? "连接超时" : "连接失败"
        case "tls": timedOut == true ? "TLS 握手超时" : "TLS 握手失败"
        case "response": timedOut == true ? "等待响应超时" : "未收到响应"
        case "http_status": "HTTP 响应异常"
        case "dns_response": "DNS 答复无效"
        default: "无法检测"
        }
    }
}

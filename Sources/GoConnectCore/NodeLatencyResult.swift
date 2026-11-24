import Foundation

public struct NodeLatencyResult: Codable, Equatable, Sendable {
    public enum Outcome: String, Codable, Sendable { case success, timeout, unavailable, cancelled, unmeasured }
    public var outcome: Outcome
    public var milliseconds: Int?
    public var detail: String?
    public var physicalInterface: String?
    public var recentSuccesses: [Bool]?
    public var checkedAt: Date
    public init(outcome: Outcome, milliseconds: Int? = nil, checkedAt: Date = Date(), detail: String? = nil) {
        self.outcome = outcome; self.milliseconds = milliseconds; self.checkedAt = checkedAt; self.detail = detail
    }
    public var label: String {
        switch outcome {
        case .success: milliseconds.map { "\($0) ms" } ?? "测试失败"
        case .timeout: "超时"
        case .unavailable: "不可用"
        case .cancelled: "已取消"
        case .unmeasured: "未测得"
        }
    }
    public func recordingHistory(after previous: Self?) -> Self {
        var result = self
        var history = previous?.recentSuccesses ?? []
        if physicalInterface != nil, [.success, .timeout, .unavailable].contains(outcome) {
            history.append(outcome == .success)
        }
        result.recentSuccesses = Array(history.suffix(5))
        return result
    }

    public var independentMilliseconds: Int? { physicalInterface == nil ? nil : milliseconds }

    public var historyLabel: String? {
        guard let recentSuccesses, !recentSuccesses.isEmpty else { return nil }
        let successes = recentSuccesses.filter { $0 }.count
        let percent = Int((100 * Double(successes) / Double(recentSuccesses.count)).rounded())
        return "最近 \(recentSuccesses.count) 次成功率 \(percent)%"
    }
    // curl's total time includes proxy negotiation, TLS and the HTTP response.
    public static func parse(curlExitCode: Int32, output: String, checkedAt: Date = Date()) -> Self {
        if curlExitCode == 28 { return Self(outcome: .timeout, checkedAt: checkedAt) }
        let parts = output.split(whereSeparator: \.isWhitespace)
        guard curlExitCode == 0, parts.count == 2, parts[0] == "204",
              let seconds = Double(parts[1]), seconds.isFinite, seconds >= 0, seconds <= 60 else {
            return Self(outcome: .unavailable, checkedAt: checkedAt, detail: curlExitCode == 35 || curlExitCode == 60 ? "HTTPS 握手或证书校验失败" : curlExitCode == 0 ? "测试站点未返回预期响应" : "节点连接失败")
        }
        return Self(outcome: .success, milliseconds: max(1, Int((seconds * 1000).rounded())), checkedAt: checkedAt)
    }
}

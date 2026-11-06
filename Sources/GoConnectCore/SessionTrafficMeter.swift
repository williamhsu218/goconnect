import Foundation

/// Sample at event arrival, independently of which window or page is visible.
public struct SessionTrafficMeter: Sendable {
    private var previous: (received: UInt64, sent: UInt64, time: TimeInterval)?
    public init() {}
    public mutating func reset() { previous = nil }

    public mutating func sample(received: UInt64, sent: UInt64, time: TimeInterval) -> (download: Double, upload: Double) {
        defer { previous = (received, sent, time) }
        guard let previous else { return (0, 0) }
        let elapsed = time - previous.time
        guard elapsed > 0, elapsed <= 5 else { return (0, 0) }
        let download = received >= previous.received ? received - previous.received : 0
        let upload = sent >= previous.sent ? sent - previous.sent : 0
        return (Double(download) / elapsed, Double(upload) / elapsed)
    }
}

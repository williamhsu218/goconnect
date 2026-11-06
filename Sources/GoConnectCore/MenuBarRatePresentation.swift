import Foundation

public enum MenuBarRateDirection: String, CaseIterable, Sendable {
    case both, download, upload

    public var title: String {
        switch self {
        case .both: "下行与上行"
        case .download: "仅下行"
        case .upload: "仅上行"
        }
    }
}

/// A display of the current GoConnect session, never a whole-device traffic meter.
public struct MenuBarRatePresentation: Equatable, Sendable {
    public static let freshnessInterval: TimeInterval = 5
    public let title: String
    public let lines: [String]
    public let accessibilityDescription: String?
    public let expiresAt: Date?

    public init(enabled: Bool, direction: MenuBarRateDirection = .both,
                state: VPNControlState, available: Bool, sampledAt: Date?,
                download: Double, upload: Double, now: Date = Date()) {
        guard enabled else {
            title = ""; lines = []; accessibilityDescription = nil; expiresAt = nil
            return
        }
        let age = sampledAt.map { now.timeIntervalSince($0) }
        let fresh = (state == .connected || state == .needsRestart) && available
            && age.map { $0.isFinite && $0 >= 0 && $0 < Self.freshnessInterval } == true
        let down = fresh ? Self.format(download) : "—"
        let up = fresh ? Self.format(upload) : "—"
        let compactDown = fresh ? Self.compact(download) : "—"
        let compactUp = fresh ? Self.compact(upload) : "—"
        let downDescription = "下载\(down == "—" ? "暂无数据" : down)"
        let upDescription = "上传\(up == "—" ? "暂无数据" : up)"
        let selectedAvailable: Bool
        switch direction {
        case .both:
            lines = ["↓\(compactDown)", "↑\(compactUp)"]
            title = "↓ \(down)  ↑ \(up)"
            accessibilityDescription = downDescription + "，" + upDescription
            selectedAvailable = down != "—" || up != "—"
        case .download:
            lines = ["↓\(compactDown)"]
            title = "↓ \(down)"
            accessibilityDescription = downDescription
            selectedAvailable = down != "—"
        case .upload:
            lines = ["↑\(compactUp)"]
            title = "↑ \(up)"
            accessibilityDescription = upDescription
            selectedAvailable = up != "—"
        }
        expiresAt = fresh && selectedAvailable
            ? sampledAt?.addingTimeInterval(Self.freshnessInterval) : nil
    }

    /// Compact spacing retains byte-per-second units. Tooltips and accessibility
    /// retain full precision. Never render four digits or a rounded "100.0".
    private static func compact(_ value: Double) -> String {
        guard value.isFinite, value >= 0 else { return "—" }
        let units = ["B/s", "KB/s", "MB/s", "GB/s"]
        var scaled = value
        var unit = 0
        while scaled >= 999.5, unit < units.count - 1 {
            scaled /= 1024
            unit += 1
        }
        if unit == units.count - 1, scaled >= 999 { return "≥999GB/s" }
        let decimal = unit > 0 && (scaled * 10).rounded() < 1000
        return String(format: decimal ? "%.1f%@" : "%.0f%@",
                      locale: Locale(identifier: "en_US_POSIX"), scaled, units[unit])
    }

    private static func format(_ value: Double) -> String {
        guard value.isFinite, value >= 0 else { return "—" }
        let units = ["B/s", "KB/s", "MB/s", "GB/s"]
        var scaled = value
        var unit = 0
        while scaled >= 1024, unit < units.count - 1 {
            scaled /= 1024
            unit += 1
        }
        // Bound the status-item width even for malformed or extreme samples.
        if unit == units.count - 1, scaled >= 999 { return "≥999 GB/s" }
        let pattern = unit > 0 && scaled < 100 ? "%.1f %@" : "%.0f %@"
        return String(format: pattern, locale: Locale(identifier: "en_US_POSIX"), scaled, units[unit])
    }
}

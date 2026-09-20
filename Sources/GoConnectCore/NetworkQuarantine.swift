import Foundation
import Darwin

/// Persist an unresolved child exit across GUI restarts during the same boot.
/// Sleep and wall-clock changes must never clear this guard.
public struct NetworkQuarantine: Codable, Sendable {
    public let bootID: String
    public init(bootID: String = NetworkQuarantine.currentBootID()) { self.bootID = bootID }
    public func applies(bootID current: String = NetworkQuarantine.currentBootID()) -> Bool {
        bootID == "unknown" || current == "unknown" || bootID == current
    }
    public static func currentBootID() -> String {
        var size = 0
        guard sysctlbyname("kern.bootsessionuuid", nil, &size, nil, 0) == 0, size > 1, size < 256 else { return "unknown" }
        var buffer = [CChar](repeating: 0, count: size)
        guard sysctlbyname("kern.bootsessionuuid", &buffer, &size, nil, 0) == 0 else { return "unknown" }
        return String(cString: buffer)
    }
}

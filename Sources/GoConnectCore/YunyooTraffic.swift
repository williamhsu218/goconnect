import Foundation

public enum YunyooTrafficError: LocalizedError, Equatable {
    case loginRequired, unavailable, invalidResponse, serverMismatch

    public var errorDescription: String? {
        switch self {
        case .loginRequired: "请登录云悠后同步套餐流量。"
        case .unavailable: "暂时无法同步云悠流量，已保留上次数据。"
        case .invalidResponse: "云悠未返回有效的套餐流量，已保留上次数据。"
        case .serverMismatch: "云悠产品地址与这组节点不一致，请检查产品 ID。"
        }
    }
}

/// Provider billing units are GB. Keep them as reported rather than applying a
/// guessed byte conversion or counting this Mac's proxy traffic as billed usage.
public struct YunyooTrafficSnapshot: Codable, Equatable, Sendable {
    public let serviceID: String
    public let server: String
    public let totalGB: Double
    public let usedGB: Double
    public let updatedAt: Date

    public init(serviceID: String, server: String, totalGB: Double, usedGB: Double, updatedAt: Date) {
        self.serviceID = serviceID; self.server = server
        self.totalGB = totalGB; self.usedGB = usedGB; self.updatedAt = updatedAt
    }

    public var isValid: Bool {
        Self.validServiceID(serviceID) && !server.isEmpty && totalGB.isFinite && usedGB.isFinite &&
        totalGB > 0 && totalGB <= 1_000_000_000 && usedGB >= 0 && usedGB <= 1_000_000_000 &&
        updatedAt.timeIntervalSince1970.isFinite
    }

    // Match the provider panel, which rounds used GB to one decimal before
    // subtracting it. Preserve the unrounded provider amount in the cache.
    public var displayedUsedGB: Double { (usedGB * 10).rounded(.toNearestOrAwayFromZero) / 10 }
    public var remainingGB: Double { max(0, totalGB - displayedUsedGB) }
    public var usedFraction: Double { min(1, max(0, usedGB / totalGB)) }
    public var exceeded: Bool { usedGB > totalGB }

    public static func validServiceID(_ value: String) -> Bool {
        value.range(of: "^[1-9][0-9]{0,11}$", options: .regularExpression) != nil
    }

    public static func formatGB(_ amount: Double) -> String {
        let formatter = NumberFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.numberStyle = .decimal; formatter.usesGroupingSeparator = false
        formatter.roundingMode = .halfUp
        formatter.minimumFractionDigits = amount > 1024 ? 2 : 1
        formatter.maximumFractionDigits = amount > 1024 ? 2 : 1
        return (formatter.string(from: NSNumber(value: amount > 1024 ? amount / 1024 : amount)) ?? "—") + (amount > 1024 ? " TB" : " GB")
    }

    public static func decode(_ data: Data, serviceID: String, expectedServer: String, now: Date = Date()) throws -> Self {
        guard data.count <= 2_097_152, validServiceID(serviceID) else { throw YunyooTrafficError.invalidResponse }
        struct Amount: Decodable {
            let value: Double
            init(from decoder: Decoder) throws {
                let c = try decoder.singleValueContainer()
                if let number = try? c.decode(Double.self) { value = number }
                else if let text = try? c.decode(String.self), let number = Double(text) { value = number }
                else { throw YunyooTrafficError.invalidResponse }
            }
        }
        struct Host: Decodable { let bwlimit: Amount; let bwusage: Amount; let dedicatedip: String }
        struct Payload: Decodable { let host_data: Host }
        struct Envelope: Decodable { let status: Int; let data: Payload? }
        let envelope: Envelope
        do { envelope = try JSONDecoder().decode(Envelope.self, from: data) }
        catch { throw YunyooTrafficError.invalidResponse }
        if [401, 403, 405].contains(envelope.status) { throw YunyooTrafficError.loginRequired }
        guard envelope.status == 200 else { throw YunyooTrafficError.unavailable }
        guard let host = envelope.data?.host_data else { throw YunyooTrafficError.invalidResponse }
        guard host.dedicatedip.lowercased() == expectedServer.lowercased() else { throw YunyooTrafficError.serverMismatch }
        let snapshot = Self(serviceID: serviceID, server: host.dedicatedip,
                            totalGB: host.bwlimit.value, usedGB: host.bwusage.value, updatedAt: now)
        guard snapshot.isValid else { throw YunyooTrafficError.invalidResponse }
        return snapshot
    }
}

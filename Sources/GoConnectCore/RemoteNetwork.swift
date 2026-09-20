import Foundation
import Darwin

public enum RemoteNetwork {
    /// Only whole RFC1918 / ULA subnets, never an accidental public/default route.
    public static func canonical(_ input: String) throws -> String {
        guard let canonical = DirectAddress.canonical(input.trimmingCharacters(in: .whitespacesAndNewlines)) else { throw ValidationError.invalid }
        let parts = canonical.split(separator: "/")
        let host = String(parts[0])
        if host.contains(":") {
            let bits = parts.count == 2 ? Int(parts[1]) ?? 0 : 128
            var bytes = [UInt8](repeating: 0, count: 16)
            guard inet_pton(AF_INET6, host, &bytes) == 1, bits >= 7, bytes[0] & 0xfe == 0xfc else { throw ValidationError.invalid }
        } else {
            let octets = host.split(separator: ".").compactMap { Int($0) }
            let bits = parts.count == 2 ? Int(parts[1]) ?? 0 : 32
            guard octets.count == 4,
                  (octets[0] == 10 && bits >= 8) || (octets[0] == 172 && (16...31).contains(octets[1]) && bits >= 12) || (octets[0] == 192 && octets[1] == 168 && bits >= 16) else { throw ValidationError.invalid }
        }
        return canonical
    }
    public static func validated(_ values: [String]) throws -> [String] {
        guard values.count <= 512 else { throw ValidationError.invalid }
        var result: [String] = []
        for value in values { let item = try canonical(value); if !result.contains(item) { result.append(item) } }
        guard result.count <= 256 else { throw ValidationError.invalid }
        return result
    }
    public enum ValidationError: LocalizedError {
        case invalid
        public var errorDescription: String? { "请输入私有 IP 或内网网段，例如 10.20.0.10、10.20.0.0/16；不支持公网或默认路由。" }
    }
}

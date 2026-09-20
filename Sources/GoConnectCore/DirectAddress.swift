import Foundation
import Darwin

/// A literal address or network, kept separate from hostname validation.
enum DirectAddress {
    static func canonical(_ input: String) -> String? {
        let parts = input.split(separator: "/", omittingEmptySubsequences: false)
        guard parts.count <= 2, let host = parts.first, !host.contains("%") else { return nil }
        let family = host.contains(":") ? AF_INET6 : AF_INET
        let size = family == AF_INET ? 4 : 16
        var bytes = [UInt8](repeating: 0, count: size)
        guard bytes.withUnsafeMutableBytes({ inet_pton(family, String(host), $0.baseAddress!) }) == 1 else { return nil }
        let bits: Int
        if parts.count == 2 {
            guard !parts[1].isEmpty, parts[1].allSatisfy({ $0.isASCII && $0.isNumber }), let n = Int(parts[1]), n > 0, n <= size * 8 else { return nil }
            bits = n
        } else { bits = size * 8 }
        for i in bytes.indices {
            let keep = max(0, min(8, bits - i * 8))
            bytes[i] &= keep == 0 ? 0 : UInt8(255 << (8 - keep) & 255)
        }
        guard bytes.contains(where: { $0 != 0 }) else { return nil }
        if family == AF_INET {
            guard bytes[0] != 127, bytes[0] < 224, bytes[0] != 0 else { return nil }
        } else {
            guard bytes[0] != 255,
                  !(bytes.dropLast().allSatisfy({ $0 == 0 }) && bytes.last == 1),
                  !(bytes.prefix(10).allSatisfy({ $0 == 0 }) && bytes[10] == 255 && bytes[11] == 255) else { return nil }
        }
        var result = [CChar](repeating: 0, count: Int(INET6_ADDRSTRLEN))
        guard bytes.withUnsafeBytes({ inet_ntop(family, $0.baseAddress!, &result, socklen_t(result.count)) }) != nil else { return nil }
        let address = String(cString: result)
        return parts.count == 2 ? "\(address)/\(bits)" : address
    }
}

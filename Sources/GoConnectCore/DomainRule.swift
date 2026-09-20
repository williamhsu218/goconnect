import Foundation

public struct DomainRule: Codable, Equatable, Identifiable, Sendable {
    public var domain: String
    public var includeSubdomains: Bool
    public var id: String { domain }
    public var isAddress: Bool { DirectAddress.canonical(domain) != nil }
    public init(_ input: String, includeSubdomains: Bool = true) throws {
        var value = input.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        if let address = DirectAddress.canonical(value) {
            domain = address; self.includeSubdomains = false; return
        }
        if value.hasPrefix("*.") { value.removeFirst(2) }
        if value.hasSuffix(".") { value.removeLast() }
        let labels = value.split(separator: ".", omittingEmptySubsequences: false)
        guard value.utf8.count <= 253, labels.count >= 2,
              labels.allSatisfy({ label in
                  !label.isEmpty && label.utf8.count <= 63 && label.first != "-" && label.last != "-" &&
                  label.utf8.allSatisfy { (97...122).contains($0) || (48...57).contains($0) || $0 == 45 }
              }), !labels.allSatisfy({ Int($0) != nil }) else { throw DomainRuleError.invalid }
        domain = value; self.includeSubdomains = includeSubdomains
    }
}
public enum DomainRuleError: LocalizedError {
    case invalid
    public var errorDescription: String? { "请输入域名、IP 或 CIDR，例如 example.com、192.168.2.10、10.211.55.0/24；不含协议和端口，不支持默认路由、回环或组播。" }
}

public struct SubscriptionSelection: Codable, Equatable, Sendable {
    public var subscriptionID: UUID
    public var nodeName: String
    public init(subscriptionID: UUID, nodeName: String) { self.subscriptionID = subscriptionID; self.nodeName = nodeName }
}

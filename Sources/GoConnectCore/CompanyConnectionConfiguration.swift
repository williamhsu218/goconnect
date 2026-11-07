import Foundation
import Darwin

/// Separate sidecar; schema-7 main configuration remains readable by 1.0.8.
public struct CompanyConnectionConfiguration: Codable, Equatable, Sendable {
    public var schemaVersion = 1
    public var enabled = false
    public var primaryProfileID: UUID?
    public var companyProfileID: UUID?
    public var networks: [String] = []
    public var domains: [String] = []
    /// Empty means use and validate DNS supplied by the authenticated company VPN.
    public var dnsServers: [String] = []
    public init() {}
    public func validated(in configuration: Configuration) throws -> CompanyConnectionConfiguration {
        guard schemaVersion == 1 else { throw CompanyConnectionError.invalid("不支持的公司连接配置版本。") }
        guard enabled else { return self }
        guard let primaryProfileID, let companyProfileID, primaryProfileID != companyProfileID,
              configuration.activeProfileID == primaryProfileID,
              let primary = configuration.connections.first(where: { $0.id == primaryProfileID }), primary.subscription != nil,
              let company = configuration.connections.first(where: { $0.id == companyProfileID }), company.subscription == nil else {
            throw CompanyConnectionError.invalid("请选择不同的 HY2 主线路和 AnyConnect 公司线路。")
        }
        _ = try company.profile.validatedServer()
        guard !networks.isEmpty, networks.count <= 32, domains.count <= 32, dnsServers.count <= 4 else {
            throw CompanyConnectionError.invalid("请填写 1–32 个明确公司网段；域名最多 32 个，DNS 最多 4 个。")
        }
        let checked = try RemoteNetwork.validated(networks)
        guard checked.allSatisfy({ !$0.contains(":") }), checked.count == networks.count else {
            throw CompanyConnectionError.invalid("首版只支持不重复的公司 IPv4 私网。")
        }
        let prefixes: [(UInt32, Int)] = checked.map { value in
            let parts = value.split(separator: "/")
            let ip = parts[0].split(separator: ".").reduce(UInt32(0)) { ($0 << 8) | UInt32($1)! }
            return (ip, parts.count == 2 ? Int(parts[1])! : 32)
        }
        for i in prefixes.indices {
            for j in prefixes.indices where j < i {
                let bits = min(prefixes[i].1, prefixes[j].1)
                let mask = UInt32.max << (32 - bits)
                guard prefixes[i].0 & mask != prefixes[j].0 & mask else {
                    throw CompanyConnectionError.invalid("公司网段不能重复或相互覆盖。")
                }
            }
        }
        for domain in domains {
            let rule = try DomainRule(domain)
            guard !rule.isAddress, rule.domain == domain, domain.contains("."), !domain.contains(":"),
                  let tld = domain.split(separator: ".").last,
                  !["local", "localhost", "localdomain", "arpa"].contains(String(tld)),
                  !tld.allSatisfy(\.isNumber) else {
                throw CompanyConnectionError.invalid("公司域名须为完整域名，不能使用 .local。")
            }
        }
        for i in domains.indices {
            for j in domains.indices where j < i {
                guard domains[i] != domains[j], !domains[i].hasSuffix("." + domains[j]), !domains[j].hasSuffix("." + domains[i]) else {
                    throw CompanyConnectionError.invalid("公司域名不能重复或相互覆盖。")
                }
            }
        }
        for dns in dnsServers {
            var address = in_addr()
            let octets = dns.split(separator: ".").compactMap { Int($0) }
            guard inet_pton(AF_INET, dns, &address) == 1, octets.count == 4,
                  (1...223).contains(octets[0]), octets[0] != 127, !(octets[0] == 169 && octets[1] == 254) else {
                throw CompanyConnectionError.invalid("公司 DNS 须为可用的 IPv4 地址。")
            }
        }
        guard Set(domains).count == domains.count, Set(dnsServers).count == dnsServers.count else {
            throw CompanyConnectionError.invalid("公司域名和 DNS 不能重复。")
        }
        var result = self
        result.networks = checked.map { $0.contains("/") ? $0 : $0 + "/32" }
        return result
    }
}
public enum CompanyConnectionError: LocalizedError {
    case invalid(String)
    public var errorDescription: String? { switch self { case .invalid(let message): message } }
}
public struct CompanyReservation: Codable, Equatable, Sendable {
    public var networks: [String]
    public var domains: [String]
    public var dnsServers: [String]
    public var gatewayHost: String
    public var gatewayPort: UInt16
    public var gatewayIPs: [String]
    public init(networks: [String], domains: [String], dnsServers: [String], gatewayHost: String, gatewayPort: UInt16, gatewayIPs: [String]) {
        self.networks = networks; self.domains = domains; self.dnsServers = dnsServers
        self.gatewayHost = gatewayHost; self.gatewayPort = gatewayPort; self.gatewayIPs = gatewayIPs
    }
}
/// No password or arbitrary route configuration is accepted by this IPC.
public struct CompanyBackendClaim: Codable, Equatable, Sendable {
    public var socketPath: String
    public var pid: Int32
    public var startTime: UInt64
    public var token: String
    public var remoteNetworks: [String]
    public var remoteExcluded: [String]
    public var dnsServers: [String]
    public init(socketPath: String, pid: Int32, startTime: UInt64, token: String, dnsServers: [String], remoteNetworks: [String] = [], remoteExcluded: [String] = []) {
        self.socketPath = socketPath; self.pid = pid; self.startTime = startTime; self.token = token; self.dnsServers = dnsServers; self.remoteNetworks = remoteNetworks; self.remoteExcluded = remoteExcluded
    }
}
public struct CompanyRouteState: Codable, Equatable, Sendable {
    public var revision: UInt64
    public var generation: String
    public var state: String
    public var message: String?
    public var proxyPort: UInt16?
    public var proxyToken: String?
    public var dnsServers: [String]?
    public init(revision: UInt64 = 0, generation: String = "", state: String = "blocked", message: String? = nil) {
        self.revision = revision; self.generation = generation; self.state = state; self.message = message
    }
}

public struct CompanyConnectionFile {
    public let url: URL
    public init(url: URL) { self.url = url }
    public func load() throws -> CompanyConnectionConfiguration {
        guard FileManager.default.fileExists(atPath: url.path) else { return .init() }
        let values = try url.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey, .fileSizeKey])
        guard values.isRegularFile == true, values.isSymbolicLink != true, (values.fileSize ?? 0) <= 65536 else {
            throw CompanyConnectionError.invalid("公司连接配置不是安全的普通文件，原文件已保留。")
        }
        let value = try JSONDecoder().decode(CompanyConnectionConfiguration.self, from: Data(contentsOf: url))
        guard value.schemaVersion == 1 else { throw CompanyConnectionError.invalid("不支持的公司连接配置版本。") }
        return value
    }
    public func save(_ value: CompanyConnectionConfiguration) throws {
        let directory = url.deletingLastPathComponent()
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        if FileManager.default.fileExists(atPath: url.path) {
            let attributes = try url.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey])
            guard attributes.isRegularFile == true, attributes.isSymbolicLink != true else { throw CompanyConnectionError.invalid("拒绝覆盖不安全的公司连接配置。") }
        }
        try JSONEncoder().encode(value).write(to: url, options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
    }
}

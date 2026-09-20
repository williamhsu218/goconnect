import Foundation

public enum Product {
    public static let bundleID = "com.willhsu.GoConnect"
    public static let version = "0.11.4"
}

public enum AccessMode: String, Codable, CaseIterable, Identifiable, Sendable {
    case applicationProxy, companyNetwork
    public var id: String { rawValue }
    public var title: String { self == .applicationProxy ? "应用代理" : "公司内网" }
    public var explanation: String {
        self == .applicationProxy ? "仅处理主动使用本机代理的请求；其他应用保持原网络。" : "仅将明确的公司网段送入 AnyConnect；不修改公网默认路由，不按 App 区分。"
    }
}

public enum RoutingMode: String, Codable, CaseIterable, Identifiable, Sendable {
    case whitelist, global
    public var id: String { rawValue }
    public var title: String {
        switch self { case .whitelist: "App 白名单"; case .global: "全局模式" }
    }
    public var explanation: String {
        switch self {
        case .whitelist: "白名单应用默认走线路，其它应用直连；直连域名和直连 App 优先。"
        case .global: "除直连 App 外，其余应用走线路；直连域名、本地与 Tailscale 路径优先。"
        }
    }
}

public struct VPNProfile: Codable, Equatable, Sendable {
    public var id = UUID()
    public var name = "工作网络"
    public var server = ""
    public var username = ""
    public var group = ""
    public var rememberPassword = false
    public init() {}

    public func validatedServer() throws -> String {
        let value = server.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !value.isEmpty, !value.contains(where: { $0.isWhitespace || $0.asciiValue.map { $0 < 32 } == true }) else {
            throw ConfigurationError.invalidServer
        }
        let normalized = value.contains("://") ? value : "https://" + value
        guard let url = URLComponents(string: normalized), url.scheme == "https",
              let host = url.host, !host.isEmpty, url.user == nil, url.password == nil,
              url.fragment == nil, url.query == nil, url.port.map({ (1...65535).contains($0) }) ?? true else {
            throw ConfigurationError.invalidServer
        }
        guard !username.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              !username.contains("\n"), !group.contains("\n") else {
            throw ConfigurationError.invalidUsername
        }
        return normalized
    }
}

public struct AllowedApplication: Codable, Equatable, Identifiable, Sendable {
    public var id: String { path }
    public var name: String
    public var bundleID: String
    public var path: String
    public var isExecutableService: Bool {
        URL(fileURLWithPath: path).pathExtension.lowercased() != "app"
    }
    public var targetTypeTitle: String { isExecutableService ? "可执行服务" : "App" }
    public init(name: String, bundleID: String, path: String) {
        self.name = name; self.bundleID = bundleID; self.path = path
    }
}

public struct SavedConnection: Codable, Equatable, Identifiable, Sendable {
    public var id: UUID { profile.id }
    public var profile: VPNProfile
    public var applications: [AllowedApplication]
    public var excludedApplications: [AllowedApplication]
    public var routingMode: RoutingMode
    public var directDomains: [DomainRule] = []
    public var accessMode: AccessMode = .applicationProxy
    public var companyRoutesEnabled = false
    public var usesCompanyRoutes: Bool { subscription == nil && companyRoutesEnabled }
    public var connectionSummary: String { routingMode.title }
    public var automaticRemoteNetworks = true
    public var remoteNetworks: [String] = []
    public var subscription: SubscriptionSelection?
    public var conflictingApplications: [AllowedApplication] {
        applications.filter { app in excludedApplications.contains { $0.path == app.path } }
    }
    public var effectiveVPNApplications: [AllowedApplication] {
        applications.filter { app in !excludedApplications.contains { $0.path == app.path } }
    }
    public var displayName: String { profile.name.isEmpty ? "未命名连接" : profile.name }
    public init(profile: VPNProfile = VPNProfile(), applications: [AllowedApplication] = [],
                excludedApplications: [AllowedApplication] = [], routingMode: RoutingMode = .whitelist) {
        self.profile = profile; self.applications = applications
        self.excludedApplications = excludedApplications; self.routingMode = routingMode
    }
    private enum CodingKeys: String, CodingKey { case profile, applications, excludedApplications, routingMode, directDomains, subscription, automaticRemoteNetworks, remoteNetworks, accessMode, companyRoutesEnabled }
    public init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        accessMode = try values.decodeIfPresent(AccessMode.self, forKey: .accessMode) ?? .applicationProxy
        profile = try values.decode(VPNProfile.self, forKey: .profile)
        applications = try values.decode([AllowedApplication].self, forKey: .applications)
        excludedApplications = try values.decodeIfPresent([AllowedApplication].self, forKey: .excludedApplications) ?? []
        routingMode = try values.decodeIfPresent(RoutingMode.self, forKey: .routingMode) ?? .whitelist
        directDomains = try values.decodeIfPresent([DomainRule].self, forKey: .directDomains) ?? []
        automaticRemoteNetworks = try values.decodeIfPresent(Bool.self, forKey: .automaticRemoteNetworks) ?? true
        remoteNetworks = try values.decodeIfPresent([String].self, forKey: .remoteNetworks) ?? []
        subscription = try values.decodeIfPresent(SubscriptionSelection.self, forKey: .subscription)
        companyRoutesEnabled = try values.decodeIfPresent(Bool.self, forKey: .companyRoutesEnabled) ?? (subscription == nil && (accessMode == .companyNetwork || !remoteNetworks.isEmpty))
    }
}

public struct Configuration: Codable, Equatable, Sendable {
    public var schemaVersion = 7
    public var connections: [SavedConnection]
    public var activeProfileID: UUID
    private var activeIndex: Int { connections.firstIndex { $0.id == activeProfileID } ?? 0 }
    public var profile: VPNProfile {
        get { connections[activeIndex].profile }
        set { connections[activeIndex].profile = newValue }
    }
    public var applications: [AllowedApplication] {
        get { connections[activeIndex].applications }
        set { connections[activeIndex].applications = newValue }
    }
    public var excludedApplications: [AllowedApplication] {
        get { connections[activeIndex].excludedApplications }
        set { connections[activeIndex].excludedApplications = newValue }
    }
    public var routingMode: RoutingMode {
        get { connections[activeIndex].routingMode }
        set { connections[activeIndex].routingMode = newValue }
    }
    public var accessMode: AccessMode {
        get { connections[activeIndex].accessMode }
        set { connections[activeIndex].accessMode = newValue }
    }
    public var activeConnection: SavedConnection { connections[activeIndex] }
    public var directDomains: [DomainRule] {
        get { connections[activeIndex].directDomains }
        set { connections[activeIndex].directDomains = newValue }
    }
    public var effectiveDirectDomains: [DomainRule] { directDomains }
    public var routingApplications: [AllowedApplication] {
        switch routingMode { case .whitelist: activeConnection.effectiveVPNApplications; case .global: excludedApplications }
    }
    public init() {
        let connection = SavedConnection(); connections = [connection]; activeProfileID = connection.id
    }
    public mutating func select(_ id: UUID) throws {
        guard connections.contains(where: { $0.id == id }) else { throw ConfigurationError.invalidProfiles }
        activeProfileID = id
    }
    public mutating func add(name: String, copyCurrent: Bool) throws {
        guard connections.count < 64, !name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { throw ConfigurationError.invalidProfiles }
        var item = copyCurrent ? connections[activeIndex] : SavedConnection()
        item.profile.id = UUID(); item.profile.name = name.trimmingCharacters(in: .whitespacesAndNewlines)
        item.profile.rememberPassword = false
        connections.append(item); activeProfileID = item.id
    }
    public mutating func remove(_ id: UUID) throws {
        guard connections.count > 1, connections.contains(where: { $0.id == id }) else { throw ConfigurationError.invalidProfiles }
        connections.removeAll { $0.id == id }
        if activeProfileID == id { activeProfileID = connections[0].id }
    }
    private enum CodingKeys: String, CodingKey { case schemaVersion, connections, activeProfileID, profile, applications }
    public init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        let version = try values.decode(Int.self, forKey: .schemaVersion)
        guard [1, 2, 3, 4, 5, 6, 7].contains(version) else { throw ConfigurationError.unsupportedVersion }
        if version == 1 {
            let profile = try values.decode(VPNProfile.self, forKey: .profile)
            connections = [SavedConnection(profile: profile, applications: try values.decode([AllowedApplication].self, forKey: .applications))]
            activeProfileID = profile.id
        } else {
            connections = try values.decode([SavedConnection].self, forKey: .connections)
            activeProfileID = try values.decode(UUID.self, forKey: .activeProfileID)
            guard !connections.isEmpty, connections.count <= 64,
                  Set(connections.map(\.id)).count == connections.count,
                  connections.contains(where: { $0.id == activeProfileID }) else { throw ConfigurationError.invalidProfiles }
        }
    }
    public func encode(to encoder: Encoder) throws {
        var values = encoder.container(keyedBy: CodingKeys.self)
        try values.encode(schemaVersion, forKey: .schemaVersion)
        try values.encode(connections, forKey: .connections)
        try values.encode(activeProfileID, forKey: .activeProfileID)
    }
}

public enum ConfigurationError: LocalizedError {
    case invalidCompanyMode
    case invalidServer, invalidUsername, invalidName, invalidDomainRules, profileChanged, unsupportedVersion, missingIdentity, invalidProfiles
    public var errorDescription: String? {
        switch self {
        case .invalidServer: return "请输入有效的 HTTPS 服务器地址，可包含端口和连接组路径，不要包含密码或查询参数。"
        case .invalidUsername: return "请填写用户名；用户名和连接组不能包含换行。"
        case .invalidDomainRules: return "每条线路最多保存 256 条有效且不重复的域名/IP规则。"
        case .invalidName: return "请填写线路名称。"
        case .profileChanged: return "这条线路已发生变化，请返回列表后重新编辑。"
        case .unsupportedVersion: return "配置来自更新版本，当前版本无法读取。原文件已保留。"
        case .missingIdentity: return "无法验证这个应用的身份，请重新选择有效的 .app。"
        case .invalidCompanyMode: return "公司内网模式仅支持 AnyConnect，且需要至少一个明确的公司 IP 或网段。"
        case .invalidProfiles: return "连接配置无效。请填写名称，最多保存 64 套连接，且至少保留一套。"
        }
    }
}

public struct ConfigurationFile {
    public let url: URL
    public init(url: URL) { self.url = url }
    public func load() throws -> Configuration {
        guard FileManager.default.fileExists(atPath: url.path) else { return Configuration() }
        let config = try JSONDecoder().decode(Configuration.self, from: Data(contentsOf: url))
        return config
    }
    public func save(_ configuration: Configuration) throws {
        let directory = url.deletingLastPathComponent()
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true,
                                                attributes: [.posixPermissions: 0o700])
        let encoder = JSONEncoder(); encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        if let existing = try? Data(contentsOf: url),
           let object = try? JSONSerialization.jsonObject(with: existing) as? [String: Any],
           let version = object["schemaVersion"] as? Int, version < configuration.schemaVersion {
            let backup = url.deletingPathExtension().appendingPathExtension("v\(version).backup.json")
            if !FileManager.default.fileExists(atPath: backup.path) {
                try existing.write(to: backup, options: .withoutOverwriting)
                try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: backup.path)
            }
        }
        try encoder.encode(configuration).write(to: url, options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
    }
}

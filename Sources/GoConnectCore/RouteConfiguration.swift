import Foundation

public struct RouteSession: Codable {
    public var transparentRouting = true
    public var remoteNetworks: [String] = []
    public var remoteExcluded: [String] = []
    public var liveRouting = true
    public var ownerPID: Int32
    public var controlDirectory: String
    public var token: String
    public var socksPort: UInt16
    public var appPaths: [String]
    public var gateway: String
    public var routingMode: RoutingMode
    public var directDomains: [DomainRule]
    public init(ownerPID: Int32, controlDirectory: String, token: String, socksPort: UInt16,
                appPaths: [String], gateway: String, routingMode: RoutingMode = .whitelist, directDomains: [DomainRule] = []) {
        self.ownerPID = ownerPID; self.controlDirectory = controlDirectory; self.token = token
        self.socksPort = socksPort
        self.appPaths = appPaths; self.gateway = gateway
        self.routingMode = routingMode; self.directDomains = directDomains
    }
}

public enum ShellQuote {
    public static func argument(_ value: String) -> String { "'" + value.replacingOccurrences(of: "'", with: "'\\''") + "'" }
    public static func appleScriptString(_ value: String) -> String {
        "\"" + value.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\"")
            .replacingOccurrences(of: "\n", with: "\\n")
            .replacingOccurrences(of: "\r", with: "\\r") + "\""
    }
}

import Foundation

public struct AppRouteStatus: Codable, Sendable {
    public var path: String
    public var vpn: Int
    public var direct: Int
    public var managed: Int?
    public var unmanaged: Int?
}

public struct RouteCommandResult: Codable, Sendable {
    public var sequence: UInt64
    public var success: Bool
    public var message: String?
}

public struct RouteSnapshot: Codable, Sendable {
    public var state: String
    public var message: String?
    public var device: String?
    public var systemDevice: String?
    public var remoteNetworks: [String]?
    public var updatedAt: Int64
    public var vpn: Int
    public var direct: Int
    public var blocked: Int?
    public var unknown: Int
    public var vpnObserved: Bool
    public var apps: [AppRouteStatus]?
    public var captureMode: String?
    public var originalRoute: Bool?
    public var commandResult: RouteCommandResult?
    public var routingMode: RoutingMode?
    public var tailscaleActive: Bool?
    public var tailscaleRoutes: Int?
}

import Foundation

public struct TransportEvent: Decodable, Sendable {
    public var event: String
    public var message: String?
    public var port: UInt16?
    public var addresses: [String]?
    public var dns: [String]?
    public var remoteNetworks: [String]?
    public var remoteExcluded: [String]?
    public var gateway: String?
    public var received: UInt64?
    public var sent: UInt64?
    public var passed: Bool?
    public var tun: Bool?
    public init(event: String, message: String? = nil) { self.event = event; self.message = message }
}

public struct TransportRequest: Encodable {
    public var server: String
    public var username: String
    public var group: String
    public var password: String
    public var token: String
    public var openconnectPath: String
    public init(server: String, username: String, group: String, password: String, token: String, openconnectPath: String) {
        self.server = server; self.username = username; self.group = group
        self.password = password; self.token = token; self.openconnectPath = openconnectPath
    }
}

/// Bounded, newline-framed messages. Only structured backend events reach the UI.
public struct EventDecoder {
    private var buffer = Data()
    public init() {}
    public mutating func feed(_ data: Data) -> [TransportEvent] {
        buffer.append(data)
        guard buffer.count <= 1_048_576 else { buffer.removeAll(); return [] }
        var events: [TransportEvent] = []
        while let newline = buffer.firstIndex(of: 10) {
            let line = buffer.prefix(upTo: newline)
            if let event = try? JSONDecoder().decode(TransportEvent.self, from: line) { events.append(event) }
            buffer.removeSubrange(...newline)
        }
        return events
    }
}

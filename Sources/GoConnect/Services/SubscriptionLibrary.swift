import Foundation
import GoConnectCore

indirect enum JSONValue: Codable, Equatable, Sendable {
    case string(String), number(Double), bool(Bool), array([JSONValue]), object([String: JSONValue]), null
    init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() { self = .null }
        else if let v = try? c.decode(Bool.self) { self = .bool(v) }
        else if let v = try? c.decode(Double.self) { self = .number(v) }
        else if let v = try? c.decode(String.self) { self = .string(v) }
        else if let v = try? c.decode([JSONValue].self) { self = .array(v) }
        else { self = .object(try c.decode([String: JSONValue].self)) }
    }
    func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        switch self {
        case .string(let v): try c.encode(v)
        case .number(let v): try c.encode(v)
        case .bool(let v): try c.encode(v)
        case .array(let v): try c.encode(v)
        case .object(let v): try c.encode(v)
        case .null: try c.encodeNil()
        }
    }
}
struct SubscriptionNode: Codable, Equatable, Identifiable, Sendable {
    var name: String
    var type: String
    var proxy: [String: JSONValue]
    var id: String { name }
}
struct SubscriptionPayload: Codable, Sendable {
    var name: String
    var url: String
    var nodes: [SubscriptionNode]
    var updated: Int64
    var userInfo: String
    var unsupportedCount: Int?
}
struct Subscription: Codable, Identifiable, Sendable {
    var id = UUID()
    var payload: SubscriptionPayload
    var quotaText: String? {
        var values: [String: Int64] = [:]
        for item in payload.userInfo.split(separator: ";") {
            let pair = item.split(separator: "=", maxSplits: 1)
            if pair.count == 2 { values[pair[0].trimmingCharacters(in: .whitespaces)] = Int64(pair[1].trimmingCharacters(in: .whitespaces)) }
        }
        guard let total = values["total"], total > 0 else { return nil }
        let upload = max(0, values["upload"] ?? 0), download = max(0, values["download"] ?? 0)
        let used = upload > Int64.max - download ? Int64.max : upload + download
        var text = "已用 \(ByteCountFormatter.string(fromByteCount: used, countStyle: .binary)) / \(ByteCountFormatter.string(fromByteCount: total, countStyle: .binary))"
        if let expiry = values["expire"], expiry > 0 { text += " · 到期 " + Date(timeIntervalSince1970: Double(expiry)).formatted(date: .abbreviated, time: .omitted) }
        return text
    }
}

struct SubscriptionLibrary {
    let url: URL
    func load() throws -> [Subscription] {
        guard FileManager.default.fileExists(atPath: url.path) else { return [] }
        return try JSONDecoder().decode([Subscription].self, from: Data(contentsOf: url))
    }
    func save(_ items: [Subscription]) throws {
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        try JSONEncoder().encode(items).write(to: url, options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
    }
    static func request(runtime: URL, importing: Bool, url: String = "") async throws -> [SubscriptionPayload] {
        try await Task.detached {
            let p = Process(), input = Pipe(), output = Pipe()
            p.executableURL = runtime.appendingPathComponent("bin/GoConnectTransport")
            p.arguments = [importing ? "subscription-import" : "subscription-fetch"]
            p.environment = ["PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "LANG": "en_US.UTF-8"]
            p.standardInput = input; p.standardOutput = output; p.standardError = FileHandle.nullDevice
            try p.run()
            if !importing { try input.fileHandleForWriting.write(contentsOf: JSONEncoder().encode(["url": url])) }
            try input.fileHandleForWriting.close()
            let data = output.fileHandleForReading.readDataToEndOfFile(); p.waitUntilExit()
            guard p.terminationStatus == 0 else {
                let event = try? JSONDecoder().decode(TransportEvent.self, from: data)
                throw NSError(domain: "Subscription", code: 1, userInfo: [NSLocalizedDescriptionKey: event?.message ?? "无法读取订阅，已保留现有内容。"])
            }
            return try JSONDecoder().decode([SubscriptionPayload].self, from: data)
        }.value
    }
}

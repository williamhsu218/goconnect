import Foundation
import Darwin
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

struct CatalogRefreshSummary: Equatable, Sendable {
    var retainedRemovedNames: [String] = []
    var updatedNodeNames: [String] = []
}

struct Subscription: Codable, Identifiable, Sendable {
    var id = UUID()
    var payload: SubscriptionPayload
    private(set) var retainedNodes: [SubscriptionNode]?

    func node(for selection: SubscriptionSelection) -> SubscriptionNode? {
        guard selection.subscriptionID == id else { return nil }
        return payload.nodes.first { $0.name == selection.nodeName } ?? retainedNode(for: selection)
    }

    func retainedNode(for selection: SubscriptionSelection) -> SubscriptionNode? {
        guard selection.subscriptionID == id,
              !payload.nodes.contains(where: { $0.name == selection.nodeName }) else { return nil }
        return retainedNodes?.first { $0.name == selection.nodeName }
    }

    /// Explicit refresh adopts updated parameters for surviving same-named nodes
    /// (active sessions keep their own in-flight snapshot), while retaining
    /// removed nodes so saved lines remain usable until the user switches.
    mutating func replaceCatalog(with updated: SubscriptionPayload, selections: [SubscriptionSelection]) -> CatalogRefreshSummary {
        var seen: Set<String> = []
        var retained: [SubscriptionNode] = []
        var summary = CatalogRefreshSummary()
        for selection in selections where selection.subscriptionID == id {
            guard seen.insert(selection.nodeName).inserted, let previous = node(for: selection) else { continue }
            if let replacement = updated.nodes.first(where: { $0.name == selection.nodeName }) {
                if previous != replacement {
                    summary.updatedNodeNames.append(selection.nodeName)
                }
            } else {
                retained.append(previous)
                summary.retainedRemovedNames.append(selection.nodeName)
            }
        }
        payload = updated
        retainedNodes = retained.isEmpty ? nil : retained
        return summary
    }

    /// Drops retained snapshots that are no longer referenced by any saved line
    /// or have reappeared in the live catalog.
    @discardableResult
    mutating func pruneRetainedNodes(keeping selections: [SubscriptionSelection]) -> Bool {
        guard let existing = retainedNodes, !existing.isEmpty else { return false }
        let activeNames = Set(selections.filter { $0.subscriptionID == id }.map(\.nodeName))
        let catalogNames = Set(payload.nodes.map(\.name))
        let kept = existing.filter { activeNames.contains($0.name) && !catalogNames.contains($0.name) }
        guard kept != existing else { return false }
        retainedNodes = kept.isEmpty ? nil : kept
        return true
    }

    var quotaValues: [String: Int64] {
        var result: [String: Int64] = [:]
        for field in payload.userInfo.split(separator: ";") {
            let pair = field.split(separator: "=", maxSplits: 1)
            if pair.count == 2 { result[pair[0].trimmingCharacters(in: .whitespaces)] = Int64(pair[1].trimmingCharacters(in: .whitespaces)) }
        }
        return result
    }
    var quotaText: String? {
        var values: [String: Int64] = [:]
        for item in payload.userInfo.split(separator: ";") {
            let pair = item.split(separator: "=", maxSplits: 1)
            if pair.count == 2 { values[pair[0].trimmingCharacters(in: .whitespaces)] = Int64(pair[1].trimmingCharacters(in: .whitespaces)) }
        }
        guard let total = values["total"], total > 0 else { return nil }
        let upload = max(0, values["upload"] ?? 0), download = max(0, values["download"] ?? 0)
        let used = upload > Int64.max - download ? Int64.max : upload + download
        let remaining = used >= total ? 0 : total - used
        var text = "剩余 \(ByteCountFormatter.string(fromByteCount: remaining, countStyle: .binary)) · 已用 \(ByteCountFormatter.string(fromByteCount: used, countStyle: .binary)) / \(ByteCountFormatter.string(fromByteCount: total, countStyle: .binary))"
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
        let data = try JSONEncoder().encode(items)
        let directory = url.deletingLastPathComponent()
        let manager = FileManager.default
        try manager.createDirectory(at: directory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        let staging = directory.appendingPathComponent(".subscriptions-" + UUID().uuidString)
        guard manager.createFile(atPath: staging.path, contents: nil, attributes: [.posixPermissions: 0o600]) else {
            throw CocoaError(.fileWriteUnknown)
        }
        defer { try? manager.removeItem(at: staging) }
        try data.write(to: staging)
        guard rename(staging.path, url.path) == 0 else { throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO) }
    }
    static func request(runtime: URL, importing: Bool, url: String = "", fileContent: String? = nil, name: String = "") async throws -> [SubscriptionPayload] {
        try await Task.detached {
            let p = Process(), input = Pipe(), output = Pipe()
            p.executableURL = runtime.appendingPathComponent("bin/GoConnectTransport")
            p.arguments = [fileContent != nil ? "subscription-file" : importing ? "subscription-import" : "subscription-fetch"]
            p.environment = ["PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "LANG": "en_US.UTF-8"]
            p.standardInput = input; p.standardOutput = output; p.standardError = FileHandle.nullDevice
            try p.run()
            if let fileContent { try input.fileHandleForWriting.write(contentsOf: JSONEncoder().encode(["content": fileContent, "name": name])) }
            else if !importing { try input.fileHandleForWriting.write(contentsOf: JSONEncoder().encode(["url": url])) }
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

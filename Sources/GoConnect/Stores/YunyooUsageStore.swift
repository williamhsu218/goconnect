import Foundation
import Observation
import GoConnectCore

struct YunyooUsageBinding: Codable, Equatable, Identifiable {
    var subscriptionID: UUID
    var serviceID: String
    var server: String
    var id: UUID { subscriptionID }
}

struct YunyooRelaySettings: Codable {
    let baseURL: String
    let writeToken: String
    let subscriptionURL: String
}

private final class RelayNoRedirect: NSObject, URLSessionTaskDelegate {
    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest, completionHandler: @escaping (URLRequest?) -> Void) { completionHandler(nil) }
}

@MainActor @Observable
final class YunyooUsageStore {
    enum State: Equatable { case cached, refreshing, current, loginRequired, failed(String) }
    private struct Saved: Codable {
        var schemaVersion = 1
        var bindings: [YunyooUsageBinding] = []
        var snapshots: [String: YunyooTrafficSnapshot] = [:]
    }
    private var saved = Saved()
    private(set) var states: [String: State] = [:]
    private(set) var storageError: String?
    private(set) var readable = true
    var loginBinding: YunyooUsageBinding?
    var relayMessage: String?
    var relayPublishing = false
    var relayNeedsLogin = false
    let relaySettings: YunyooRelaySettings?
    @ObservationIgnored let client = YunyooQuotaClient()
    @ObservationIgnored private let file: URL
    @ObservationIgnored private let fetchOverride: ((String, String) async throws -> YunyooTrafficSnapshot)?
    @ObservationIgnored private let authorizeOverride: ((YunyooUsageBinding, YunyooRelaySettings) async throws -> YunyooTrafficSnapshot)?

    init(file: URL, authorize: ((YunyooUsageBinding, YunyooRelaySettings) async throws -> YunyooTrafficSnapshot)? = nil,
         fetch: ((String, String) async throws -> YunyooTrafficSnapshot)? = nil) {
        self.file = file; self.fetchOverride = fetch; self.authorizeOverride = authorize
        relaySettings = (try? Data(contentsOf:file.deletingLastPathComponent().appendingPathComponent("yunyoo-relay-owner.json")))
            .flatMap { try? JSONDecoder().decode(YunyooRelaySettings.self, from:$0) }
        guard FileManager.default.fileExists(atPath: file.path) else { return }
        do {
            let data = try Data(contentsOf: file)
            guard data.count <= 1_048_576 else { throw YunyooTrafficError.invalidResponse }
            let value = try JSONDecoder().decode(Saved.self, from: data)
            guard value.schemaVersion == 1, value.bindings.count <= 64,
                  Set(value.bindings.map(\.subscriptionID)).count == value.bindings.count,
                  value.bindings.allSatisfy({ YunyooTrafficSnapshot.validServiceID($0.serviceID) && !$0.server.isEmpty }),
                  value.snapshots.allSatisfy({ $0.key == $0.value.serviceID && $0.value.isValid }) else { throw YunyooTrafficError.invalidResponse }
            saved = value
        } catch {
            readable = false; storageError = "云悠流量缓存无法读取，原文件已保留。"
        }
    }

    static func singleServer(_ subscription: Subscription) -> String? {
        let servers = subscription.payload.nodes.compactMap { node -> String? in
            if case .string(let value) = node.proxy["server"] { return value.lowercased() }
            return nil
        }
        let values = Set(servers)
        return servers.count == subscription.payload.nodes.count && values.count == 1 ? values.first : nil
    }
    func binding(for subscription: Subscription) -> YunyooUsageBinding? {
        saved.bindings.first { $0.subscriptionID == subscription.id && $0.server == Self.singleServer(subscription) }
    }
    func snapshot(for binding: YunyooUsageBinding) -> YunyooTrafficSnapshot? {
        guard let snapshot = saved.snapshots[binding.serviceID], snapshot.server.lowercased() == binding.server else { return nil }
        return snapshot
    }
    func state(for binding: YunyooUsageBinding) -> State { states[binding.serviceID] ?? .cached }

    func link(subscription: Subscription, serviceID: String) throws {
        guard readable else { throw YunyooTrafficError.invalidResponse }
        let id = serviceID.trimmingCharacters(in: .whitespacesAndNewlines)
        guard YunyooTrafficSnapshot.validServiceID(id), let server = Self.singleServer(subscription) else { throw YunyooTrafficError.invalidResponse }
        let binding = YunyooUsageBinding(subscriptionID: subscription.id, serviceID: id, server: server)
        var next = saved
        next.bindings.removeAll { $0.subscriptionID == subscription.id }
        next.bindings.append(binding)
        try write(next); saved = next
    }

    func refresh(_ binding: YunyooUsageBinding) async {
        guard readable, saved.bindings.contains(binding), state(for: binding) != .refreshing else { return }
        states[binding.serviceID] = .refreshing
        do {
            let value: YunyooTrafficSnapshot
            if let fetchOverride { value = try await fetchOverride(binding.serviceID, binding.server) }
            else { value = try await client.fetch(serviceID: binding.serviceID, server: binding.server) }
            try Task.checkCancellation()
            try saveSnapshot(value, for: binding)
        } catch is CancellationError {
            states[binding.serviceID] = .cached
        } catch YunyooTrafficError.loginRequired {
            states[binding.serviceID] = .loginRequired
        } catch {
            states[binding.serviceID] = .failed((error as? YunyooTrafficError)?.localizedDescription ?? "同步失败，已保留上次数据。")
        }
    }

    @discardableResult
    func publishRelay(_ binding: YunyooUsageBinding) async -> Bool {
        guard readable, saved.bindings.contains(binding), !relayPublishing, state(for: binding) != .refreshing, let settings = relaySettings,
              let base = URLComponents(string:settings.baseURL), base.scheme == "https", base.host == binding.server,
              base.user == nil, base.password == nil, base.query == nil, base.fragment == nil,
              base.path.isEmpty || base.path == "/", settings.writeToken.count >= 32,
              let endpoint = URL(string:settings.baseURL + "/admin/session") else { return false }
        relayPublishing = true; relayMessage = "正在授权并获取最新用量…"
        defer { relayPublishing = false }
        do {
            let value: YunyooTrafficSnapshot
            if let authorizeOverride { value = try await authorizeOverride(binding, settings) }
            else {
                let transfer = try await client.relaySession(serviceID:binding.serviceID,server:binding.server)
                var request = URLRequest(url:endpoint,timeoutInterval:45)
                request.httpMethod = "POST"; request.httpBody = transfer.body
                request.setValue("application/json",forHTTPHeaderField:"Content-Type")
                request.setValue("Bearer " + settings.writeToken,forHTTPHeaderField:"Authorization")
                let config = URLSessionConfiguration.ephemeral; config.httpCookieStorage = nil
                let session = URLSession(configuration:config,delegate:RelayNoRedirect(),delegateQueue:nil)
                defer { session.invalidateAndCancel() }
                let (data,response) = try await session.data(for:request)
                guard let response = response as? HTTPURLResponse else { throw YunyooTrafficError.unavailable }
                try Self.validateRelayResponse(data, statusCode: response.statusCode)
                value = transfer.snapshot
            }
            try Task.checkCancellation()
            try saveSnapshot(value, for: binding)
            relayNeedsLogin = false; relayMessage = "授权成功，最新用量已获取。"
            return true
        } catch is CancellationError {
            relayMessage = "授权已取消，保留上次数据。"
        } catch YunyooTrafficError.loginRequired {
            states[binding.serviceID] = .loginRequired
            relayNeedsLogin = true; loginBinding = binding; relayMessage = "授权已过期，请重新登录后点击“完成登录并刷新”。"
        } catch { relayMessage = "同步端授权失败，已保留原登录和节点配置。" }
        return false
    }

    func completeLogin(_ binding: YunyooUsageBinding) async -> Bool {
        await refresh(binding)
        guard state(for: binding) == .current else { return false }
        if relayNeedsLogin { return await publishRelay(binding) }
        return true
    }

    static func validateRelayResponse(_ data: Data, statusCode: Int) throws {
        if statusCode == 401 || statusCode == 403 { throw YunyooTrafficError.loginRequired }
        guard statusCode == 202, data.count <= 65536,
              let result = try JSONSerialization.jsonObject(with: data) as? [String:Any],
              result["accepted"] as? Bool == true else { throw YunyooTrafficError.unavailable }
        if result["status"] as? String == "auth_required" { throw YunyooTrafficError.loginRequired }
        guard result["status"] as? String == "ok" else { throw YunyooTrafficError.unavailable }
    }

    private func saveSnapshot(_ value: YunyooTrafficSnapshot, for binding: YunyooUsageBinding) throws {
        guard saved.bindings.contains(binding), value.isValid, value.serviceID == binding.serviceID,
              value.server.lowercased() == binding.server else { throw YunyooTrafficError.serverMismatch }
        var next = saved; next.snapshots[binding.serviceID] = value
        try write(next); saved = next; storageError = nil
        states[binding.serviceID] = .current
    }

    private func write(_ value: Saved) throws {
        try FileManager.default.createDirectory(at: file.deletingLastPathComponent(), withIntermediateDirectories: true,
                                               attributes: [.posixPermissions: 0o700])
        let data = try JSONEncoder().encode(value)
        try data.write(to: file, options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: file.path)
    }
}

import Foundation
import Observation
import CryptoKit
import GoConnectCore

@MainActor @Observable
final class NodeLatencyStore {
    enum Pending { case queued, testing }
    private(set) var results: [String: NodeLatencyResult] = [:]
    private(set) var pending: [String: Pending] = [:]
    private(set) var isRunning = false
    private(set) var subscriptionID: UUID?
    private(set) var total = 0
    private(set) var completed = 0
    private(set) var storageError: String?
    private var task: Task<Void, Never>?
    private var testers: [NodeLatencyTester] = []
    private let url: URL

    init(url: URL) {
        self.url = url
        if let data = try? Data(contentsOf: url), let saved = try? JSONDecoder().decode([String: NodeLatencyResult].self, from: data) { results = saved }
    }
    static func key(_ node: SubscriptionNode, subscriptionID: UUID) -> String {
        let encoder = JSONEncoder(); encoder.outputFormatting = .sortedKeys
        let digest = SHA256.hash(data: (try? encoder.encode(node)) ?? Data())
        return subscriptionID.uuidString + ":" + digest.map { String(format: "%02x", $0) }.joined()
    }
    func result(_ node: SubscriptionNode, in subscription: UUID) -> NodeLatencyResult? { results[Self.key(node, subscriptionID: subscription)] }
    func state(_ node: SubscriptionNode, in subscription: UUID) -> Pending? { pending[Self.key(node, subscriptionID: subscription)] }
    func stop() { task?.cancel(); testers.forEach { $0.stop() } }

    func start(_ nodes: [SubscriptionNode], in subscription: UUID, runtime: URL) {
        guard !isRunning, !nodes.isEmpty else { return }
        isRunning = true; subscriptionID = subscription; total = nodes.count; completed = 0; storageError = nil
        pending = Dictionary(nodes.map { (Self.key($0, subscriptionID: subscription), Pending.queued) }, uniquingKeysWith: { first, _ in first })
        task = Task {
            await withTaskGroup(of: Void.self) { group in
                // Three workers keep memory and connection load bounded.
                for worker in 0..<min(3, nodes.count) {
                    group.addTask { @MainActor in
                        let tester = NodeLatencyTester(); self.testers.append(tester)
                        var index = worker
                        while index < nodes.count && !Task.isCancelled {
                            let node = nodes[index], key = Self.key(nodes[index], subscriptionID: subscription)
                            self.pending[key] = .testing
                            let result = await tester.measure(node, runtime: runtime)
                            self.results[key] = result; self.pending.removeValue(forKey: key)
                            self.completed += 1
                            index += 3
                        }
                    }
                }
            }
            for key in pending.keys { results[key] = NodeLatencyResult(outcome: .cancelled) }
            pending = [:]; testers = []; isRunning = false; task = nil
            persist()
        }
    }
    private func persist() {
        results = Dictionary(uniqueKeysWithValues: results.sorted { $0.value.checkedAt > $1.value.checkedAt }.prefix(2048).map { ($0.key, $0.value) })
        do {
            try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
            try JSONEncoder().encode(results).write(to: url, options: .atomic)
            try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
        } catch { storageError = "测试结果暂未保存，重新打开后需要重测。" }
    }
}

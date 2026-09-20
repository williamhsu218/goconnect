import Foundation
import GoConnectCore

extension AppStore {
    var canTestNodes: Bool {
        localReady && !nodeLatency.isRunning && !subscriptionWorking && !routingCheckRunning &&
        (!busy || (phase == "connected" && !usesCompanyRoutes))
    }
    func testNodes(_ nodes: [SubscriptionNode], in subscription: Subscription) {
        guard canTestNodes else { return }
        nodeLatency.start(nodes, in: subscription.id, runtime: Bundle.main.resourceURL!.appendingPathComponent("Runtime"))
    }

    var subscriptionFile: SubscriptionLibrary { SubscriptionLibrary(url: file.url.deletingLastPathComponent().appendingPathComponent("subscriptions.json")) }
    var selectedSubscriptionNode: SubscriptionNode? {
        guard let selection = configuration.activeConnection.subscription else { return nil }
        return subscriptions.first { $0.id == selection.subscriptionID }?.payload.nodes.first { $0.name == selection.nodeName }
    }
    func importSubscriptions() { loadSubscriptions(importing: true) }
    func updateSubscription(_ item: Subscription) { loadSubscriptions(importing: false, url: item.payload.url, existingID: item.id) }
    func addSubscription(name: String, url: String) { loadSubscriptions(importing: false, url: url, name: name) }
    private func loadSubscriptions(importing: Bool, url: String = "", existingID: UUID? = nil, name: String? = nil) {
        guard !subscriptionWorking, configurationReadable, subscriptionsReadable else { return }
        nodeLatency.stop()
        subscriptionWorking = true
        Task {
            defer { subscriptionWorking = false }
            do {
                let payloads = try await SubscriptionLibrary.request(runtime: Bundle.main.resourceURL!.appendingPathComponent("Runtime"), importing: importing, url: url.trimmingCharacters(in: .whitespacesAndNewlines))
                var next = subscriptions
                for var payload in payloads {
                    if let index = next.firstIndex(where: { $0.id == existingID || $0.payload.url == payload.url }) {
                        payload.name = next[index].payload.name
                        next[index].payload = payload
                    } else {
                        if let name, !name.trimmingCharacters(in: .whitespaces).isEmpty { payload.name = name.trimmingCharacters(in: .whitespaces) }
                        next.append(Subscription(payload: payload))
                    }
                }
                try subscriptionFile.save(next); subscriptions = next
                show(payloads.isEmpty ? "没有找到可导入的远程订阅。" : "已\(importing ? "导入" : "更新") \(payloads.count) 个订阅，可进入选择节点。")
            } catch { show(error.localizedDescription, error: true) }
        }
    }
    func useNode(_ node: SubscriptionNode, in subscription: Subscription) {
        guard !busy, !subscriptionWorking, configurationReadable else { return }
        var next = configuration
        let selection = SubscriptionSelection(subscriptionID: subscription.id, nodeName: node.name)
        let current = next.connections.firstIndex { $0.id == next.activeProfileID && $0.subscription?.subscriptionID == subscription.id }
        if let index = current ?? next.connections.firstIndex(where: { $0.subscription?.subscriptionID == subscription.id }) {
            next.connections[index].subscription = selection
            next.activeProfileID = next.connections[index].id
        } else {
            guard next.connections.count < 64 else { show("最多保存 64 条线路。", error: true); return }
            var line = SavedConnection(applications: configuration.applications, excludedApplications: configuration.excludedApplications, routingMode: .global)
            line.profile.name = subscription.payload.name
            line.subscription = selection
            next.connections.append(line); next.activeProfileID = line.id
        }
        do { try file.save(next); configuration = next; password = ""; page = .connection; show("已选择 \(node.name)，开启连接开关即可连接。") }
        catch { show(error.localizedDescription, error: true) }
    }
    func removeSubscription(_ item: Subscription) {
        guard !subscriptionWorking, subscriptionsReadable else { return }
        guard !configuration.connections.contains(where: { $0.subscription?.subscriptionID == item.id }) else { show("请先在线路配置中删除使用此订阅的线路。", error: true); return }
        do { let next = subscriptions.filter { $0.id != item.id }; try subscriptionFile.save(next); subscriptions = next }
        catch { show(error.localizedDescription, error: true) }
    }
}

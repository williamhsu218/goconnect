import Foundation
import GoConnectCore
import XCTest
@testable import GoConnect

final class SubscriptionSnapshotTests: XCTestCase {
    private func node(_ host: String, name: String = "Tokyo") -> SubscriptionNode {
        SubscriptionNode(name: name, type: "hysteria2", proxy: ["server": .string(host)])
    }

    private func catalog(_ nodes: [SubscriptionNode]) -> SubscriptionPayload {
        SubscriptionPayload(name: "Fixture", url: "local://fixture", nodes: nodes, updated: 1, userInfo: "")
    }

    func testLegacySelectionKeepsRemovedNodeAfterSubscriptionSaveAndReload() throws {
        let original = node("original.example.test")
        let legacy = Subscription(payload: catalog([original]))
        let raw = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(legacy)) as? [String: Any])
        var subscription = try JSONDecoder().decode(Subscription.self, from: JSONSerialization.data(withJSONObject: raw))
        let selection = SubscriptionSelection(subscriptionID: legacy.id, nodeName: original.name)
        XCTAssertEqual(subscription.node(for: selection), original)
        let summary = subscription.replaceCatalog(with: catalog([node("new.example.test", name: "Renamed")]), selections: [selection])
        XCTAssertEqual(summary.retainedRemovedNames, [original.name])
        XCTAssertTrue(summary.updatedNodeNames.isEmpty)
        XCTAssertFalse(subscription.payload.nodes.contains(original))

        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let library = SubscriptionLibrary(url: directory.appendingPathComponent("subscriptions.json"))
        try library.save([subscription])
        let reloaded = try XCTUnwrap(library.load().first)
        XCTAssertEqual(reloaded.node(for: selection), original)
        XCTAssertEqual(reloaded.retainedNode(for: selection), original)
        let mode = try FileManager.default.attributesOfItem(atPath: library.url.path)[.posixPermissions] as? NSNumber
        XCTAssertEqual(mode?.intValue, 0o600)
    }

    func testSameNamedNodeRefreshAdoptsUpdatedDefinitionAndReappearingNodeClearsRetainedSnapshot() throws {
        let first = node("first.example.test")
        let second = node("second.example.test")
        var subscription = Subscription(payload: catalog([first]))
        let selection = SubscriptionSelection(subscriptionID: subscription.id, nodeName: first.name)

        // Same-named parameter rotation is adopted immediately for future connections.
        let rotated = subscription.replaceCatalog(with: catalog([second]), selections: [selection])
        XCTAssertEqual(rotated.updatedNodeNames, [first.name])
        XCTAssertTrue(rotated.retainedRemovedNames.isEmpty)
        XCTAssertEqual(subscription.node(for: selection), second)
        XCTAssertNil(subscription.retainedNode(for: selection))

        // Removal retains the last known definition.
        let removed = subscription.replaceCatalog(with: catalog([node("other.example.test", name: "Osaka")]), selections: [selection])
        XCTAssertEqual(removed.retainedRemovedNames, [first.name])
        XCTAssertEqual(subscription.node(for: selection), second)
        XCTAssertEqual(subscription.retainedNode(for: selection), second)

        // When the same node name returns in a later refresh, adopt the new catalog entry and drop the retained snapshot.
        let third = node("third.example.test")
        let returned = subscription.replaceCatalog(with: catalog([third]), selections: [selection])
        XCTAssertEqual(returned.updatedNodeNames, [first.name])
        XCTAssertTrue(returned.retainedRemovedNames.isEmpty)
        XCTAssertEqual(subscription.node(for: selection), third)
        XCTAssertNil(subscription.retainedNode(for: selection))
    }

    @MainActor
    func testSelectingAnotherNodeAndDeletingProfilePruneRetainedSnapshots() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let removed = node("removed.example.test", name: "Tokyo")
        let replacement = node("osaka.example.test", name: "Osaka")
        var subscription = Subscription(payload: catalog([removed]))
        let saved = SubscriptionSelection(subscriptionID: subscription.id, nodeName: removed.name)
        _ = subscription.replaceCatalog(with: catalog([replacement]), selections: [saved])
        let library = SubscriptionLibrary(url: directory.appendingPathComponent("subscriptions.json"))
        try library.save([subscription])

        var config = Configuration()
        config.connections[0].subscription = saved
        var secondary = SavedConnection()
        secondary.subscription = saved
        config.connections.append(secondary)
        try ConfigurationFile(url: directory.appendingPathComponent("configuration.json")).save(config)

        let store = AppStore(dataDirectory: directory, diagnostics: FaultDiagnostics(directory: directory), refreshRuntime: false)
        XCTAssertEqual(store.selectedSubscriptionNode, removed)
        XCTAssertEqual(store.activeRetainedSubscriptionNode, removed)

        // Switching the active line away still keeps the retained node because secondary references it.
        store.useNode(replacement, in: subscription)
        XCTAssertEqual(store.selectedSubscriptionNode, replacement)
        XCTAssertNil(store.activeRetainedSubscriptionNode)
        XCTAssertEqual(try library.load().first?.retainedNode(for: saved), removed)

        // Deleting the remaining line that referenced the removed node prunes the snapshot on disk.
        store.deleteProfile(secondary.id)
        XCTAssertNil(store.subscriptions.first?.retainedNode(for: saved))
        XCTAssertNil(try library.load().first?.retainedNode(for: saved))
    }
}

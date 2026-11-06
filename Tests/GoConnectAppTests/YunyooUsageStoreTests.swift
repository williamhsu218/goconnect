import AppKit
import XCTest
import GoConnectCore
@testable import GoConnect

@MainActor
final class YunyooUsageStoreTests: XCTestCase {
    private func fixture() throws -> (URL, Subscription) {
        _ = NSApplication.shared
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("GoConnect-quota-" + UUID().uuidString)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        addTeardownBlock { try? FileManager.default.removeItem(at: root) }
        let nodes = [443,45392].map { port in
            SubscriptionNode(name: "HY2-\(port)", type: "hysteria2", proxy: ["server":.string("192.0.2.10"), "port":.number(Double(port))])
        }
        return (root, Subscription(payload: SubscriptionPayload(name: "VPS", url: "local://vps", nodes: nodes, updated: 1, userInfo: "")))
    }
    func testSameServerPortsShareSnapshotAndFailuresKeepLastValue() async throws {
        let (root,sub) = try fixture(); let file = root.appendingPathComponent("yunyoo-usage.json")
        let sentinel = root.appendingPathComponent("configuration.json")
        let original = Data("original-config-must-stay-byte-identical".utf8); try original.write(to: sentinel)
        var calls = 0
        let store = YunyooUsageStore(file: file, fetch: { service,server in
            calls += 1
            if calls > 1 { throw YunyooTrafficError.loginRequired }
            return YunyooTrafficSnapshot(serviceID: service, server: server, totalGB: 1024, usedGB: 3.25, updatedAt: Date())
        })
        try store.link(subscription: sub, serviceID: "123456")
        let binding = try XCTUnwrap(store.binding(for: sub))
        await store.refresh(binding)
        let first = try XCTUnwrap(store.snapshot(for: binding))
        XCTAssertEqual(first.remainingGB, 1020.7, accuracy: 0.0001)
        await store.refresh(binding)
        XCTAssertEqual(store.state(for: binding), .loginRequired)
        XCTAssertEqual(store.snapshot(for: binding), first)
        XCTAssertEqual(try Data(contentsOf: sentinel), original)
        let attrs = try FileManager.default.attributesOfItem(atPath: file.path)
        XCTAssertEqual((attrs[.posixPermissions] as? NSNumber)?.intValue, 0o600)
        let restored = YunyooUsageStore(file: file)
        XCTAssertEqual(restored.snapshot(for: binding), first)
    }
    func testCorruptCacheIsRetainedAndWrongServerCannotOverwriteSnapshot() async throws {
        let (root,sub) = try fixture(); let file = root.appendingPathComponent("yunyoo-usage.json")
        let invalid = Data("not-json".utf8); try invalid.write(to: file)
        let corrupt = YunyooUsageStore(file: file)
        XCTAssertFalse(corrupt.readable)
        XCTAssertThrowsError(try corrupt.link(subscription: sub, serviceID: "123456"))
        XCTAssertEqual(try Data(contentsOf: file), invalid)
        try FileManager.default.removeItem(at: file)
        let store = YunyooUsageStore(file: file, fetch: { service,_ in
            YunyooTrafficSnapshot(serviceID: service, server: "192.0.2.1", totalGB: 1024, usedGB: 1, updatedAt: Date())
        })
        try store.link(subscription: sub, serviceID: "123456")
        let binding = try XCTUnwrap(store.binding(for: sub))
        await store.refresh(binding)
        XCTAssertNil(store.snapshot(for: binding))
        XCTAssertEqual(store.state(for: binding), .failed(YunyooTrafficError.serverMismatch.localizedDescription))
        var mixed = sub
        mixed.payload.nodes.append(SubscriptionNode(name: "other", type: "hysteria2", proxy: ["server":.string("192.0.2.2")]))
        XCTAssertNil(YunyooUsageStore.singleServer(mixed))
        XCTAssertThrowsError(try store.link(subscription: mixed, serviceID: "123456"))
    }

    func testOnlyUserActionsRefreshAndExpiredAuthorizationCanRecoverImmediately() async throws {
        let (root,sub) = try fixture()
        let settings = YunyooRelaySettings(baseURL: "https://192.0.2.10", writeToken: String(repeating: "x", count: 40), subscriptionURL: "https://192.0.2.10/s/synthetic")
        try JSONEncoder().encode(settings).write(to: root.appendingPathComponent("yunyoo-relay-owner.json"))
        var fetches = 0, authorizations = 0
        var expired = true
        let store = YunyooUsageStore(file: root.appendingPathComponent("yunyoo-usage.json"), authorize: { binding,_ in
            authorizations += 1
            if expired { throw YunyooTrafficError.loginRequired }
            return YunyooTrafficSnapshot(serviceID: binding.serviceID, server: binding.server, totalGB: 1024, usedGB: 42, updatedAt: Date())
        }, fetch: { service,server in
            fetches += 1
            return YunyooTrafficSnapshot(serviceID: service, server: server, totalGB: 1024, usedGB: 4, updatedAt: Date())
        })
        try store.link(subscription: sub, serviceID: "123456")
        let binding = try XCTUnwrap(store.binding(for: sub))
        await Task.yield()
        XCTAssertEqual(fetches, 0)
        XCTAssertEqual(authorizations, 0)
        await store.refresh(binding)
        let cached = try XCTUnwrap(store.snapshot(for: binding))
        let failed = await store.publishRelay(binding)
        XCTAssertFalse(failed)
        XCTAssertTrue(store.relayNeedsLogin)
        XCTAssertEqual(store.state(for: binding), .loginRequired)
        XCTAssertEqual(store.snapshot(for: binding), cached)
        expired = false
        let restored = await store.completeLogin(binding)
        XCTAssertTrue(restored)
        XCTAssertFalse(store.relayNeedsLogin)
        XCTAssertFalse(store.relayPublishing)
        XCTAssertEqual(store.state(for: binding), .current)
        XCTAssertEqual(store.snapshot(for: binding)?.usedGB, 42)
        XCTAssertEqual(fetches, 2)
        XCTAssertEqual(authorizations, 2)
        let reloaded = YunyooUsageStore(file: root.appendingPathComponent("yunyoo-usage.json"))
        XCTAssertEqual(reloaded.snapshot(for: binding)?.usedGB, 42)
    }

    func testRelayAcceptanceRequiresFreshUsageConfirmation() throws {
        XCTAssertNoThrow(try YunyooUsageStore.validateRelayResponse(Data(#"{"accepted":true,"status":"ok"}"#.utf8), statusCode: 202))
        for body in [#"{"accepted":true}"#, #"{"accepted":true,"status":"refreshing"}"#, #"{"accepted":true,"status":"unavailable"}"#, #"{"accepted":false,"status":"ok"}"#] {
            XCTAssertThrowsError(try YunyooUsageStore.validateRelayResponse(Data(body.utf8), statusCode: 202))
        }
        XCTAssertThrowsError(try YunyooUsageStore.validateRelayResponse(Data(#"{"accepted":true,"status":"auth_required"}"#.utf8), statusCode: 202)) { error in
            guard case YunyooTrafficError.loginRequired = error else { return XCTFail("Expected loginRequired") }
        }
        XCTAssertThrowsError(try YunyooUsageStore.validateRelayResponse(Data(), statusCode: 401)) { error in
            guard case YunyooTrafficError.loginRequired = error else { return XCTFail("Expected loginRequired") }
        }
    }
}

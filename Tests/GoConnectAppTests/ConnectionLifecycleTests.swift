import AppKit
import Foundation
import XCTest
import GoConnectCore
@testable import GoConnect

@MainActor
final class ConnectionLifecycleTests: XCTestCase {
    private func directory() throws -> URL {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("GoConnect-lifecycle-" + UUID().uuidString)
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        addTeardownBlock { try? FileManager.default.removeItem(at: url) }
        return url
    }
    private func runtime(in directory: URL, script: String) throws -> URL {
        let runtime = directory.appendingPathComponent("runtime")
        let bin = runtime.appendingPathComponent("bin")
        try FileManager.default.createDirectory(at: bin, withIntermediateDirectories: true)
        let helper = bin.appendingPathComponent("GoConnectTransport")
        // The independent address probe must not count as another VPN start.
        let probe = "if [ \"$1\" = public-ip ]; then cat >/dev/null; printf '%s\\n' '{\"event\":\"public_ip\"}'; exit 0; fi\n"
        try ("#!/bin/sh\n" + probe + script).write(to: helper, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: helper.path)
        return runtime
    }
    private func waitUntil(_ predicate: () -> Bool) async throws {
        for _ in 0..<200 {
            if predicate() { return }
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTFail("Isolated lifecycle did not reach its expected state")
    }

    func testCancelledQueuedConnectCannotStartInsideNewAttempt() async throws {
        let root = try directory(), marker = root.appendingPathComponent("starts")
        let runtime = try runtime(in: root, script: """
        IFS= read -r request
        printf x >> \(ShellQuote.argument(marker.path))
        printf '%s\\n' '{"event":"ready","port":12345,"gateway":"192.0.2.1","addresses":[]}'
        while IFS= read -r ignored; do :; done
        """
        )
        let diagnostics = FaultDiagnostics(directory: root.appendingPathComponent("logs"))
        let transport = TransportClient(runtimeURL: runtime, diagnostics: diagnostics)
        let store = AppStore(dataDirectory: root.appendingPathComponent("data"), diagnostics: diagnostics, refreshRuntime: false, transportClient: transport)
        store.localReady = true
        store.configuration.profile.server = "https://vpn.example.invalid"
        store.configuration.profile.username = "isolated-fixture"
        store.password = "synthetic-password"
        store.connect(test: true)
        store.disconnect()
        store.password = "synthetic-password"
        store.connect(test: true)
        try await waitUntil { store.phase == "testing" }
        XCTAssertEqual(try String(contentsOf: marker, encoding: .utf8), "x", "The cancelled attempt must never launch a helper")
        XCTAssertTrue(transport.isRunning)
        store.disconnect()
        try await waitUntil { store.phase == "idle" && !transport.isRunning }
        _ = await diagnostics.export()
    }

    func testFailedRouteCleanupDoesNotBecomeSuccessfulDisconnect() throws {
        let root = try directory()
        let runtime = try runtime(in: root, script: "exec /bin/sleep 60\n")
        let controls = root.appendingPathComponent("controls")
        try FileManager.default.createDirectory(at: controls, withIntermediateDirectories: true)
        let route = RouteClient(temporaryDirectory: controls)
        let store = AppStore(dataDirectory: root.appendingPathComponent("data"), diagnostics: FaultDiagnostics(directory: root.appendingPathComponent("logs")), refreshRuntime: false, routeClient: route)
        try route.start(port: 12345, token: String(repeating: "x", count: 40), apps: [], gateway: "192.0.2.1", runtime: runtime, mode: .whitelist)
        let session = try XCTUnwrap(FileManager.default.contentsOfDirectory(at: controls, includingPropertiesForKeys: nil).first)
        let status = """
        {"state":"failed","message":"合成路由清理失败，请导出诊断。","updatedAt":1,"vpn":0,"direct":0,"unknown":0,"vpnObserved":false}
        """
        try status.write(to: session.appendingPathComponent("status"), atomically: true, encoding: .utf8)
        store.phase = "connected"
        store.disconnect()
        XCTAssertEqual(store.phase, "failed")
        XCTAssertTrue(store.isError)
        XCTAssertEqual(store.message, "合成路由清理失败，请导出诊断。")
        XCTAssertFalse(route.isRunning)
        XCTAssertFalse(store.activity.contains { $0.message == "已断开。" })
    }

    func testCleanRouteStopStillCompletesDisconnect() throws {
        let root = try directory()
        let runtime = try runtime(in: root, script: "exec /bin/sleep 60\n")
        let controls = root.appendingPathComponent("controls")
        try FileManager.default.createDirectory(at: controls, withIntermediateDirectories: true)
        let route = RouteClient(temporaryDirectory: controls)
        let store = AppStore(dataDirectory: root.appendingPathComponent("data"), diagnostics: FaultDiagnostics(directory: root.appendingPathComponent("logs")), refreshRuntime: false, routeClient: route)
        try route.start(port: 12345, token: String(repeating: "x", count: 40), apps: [], gateway: "192.0.2.1", runtime: runtime, mode: .whitelist)
        let session = try XCTUnwrap(FileManager.default.contentsOfDirectory(at: controls, includingPropertiesForKeys: nil).first)
        try "{\"state\":\"stopped\",\"updatedAt\":1,\"vpn\":0,\"direct\":0,\"unknown\":0,\"vpnObserved\":false}".write(to: session.appendingPathComponent("status"), atomically: true, encoding: .utf8)
        store.phase = "connected"
        store.disconnect()
        XCTAssertEqual(store.phase, "idle")
        XCTAssertEqual(store.message, "已断开。")
    }
}

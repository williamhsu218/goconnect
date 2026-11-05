import AppKit
import SwiftUI
import XCTest
import GoConnectCore
@testable import GoConnect

@MainActor
final class TransportRecoveryTests: XCTestCase {
    private func fixture() throws -> (AppStore, URL, FaultDiagnostics) {
        _ = NSApplication.shared
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("GoConnect-recovery-" + UUID().uuidString)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        addTeardownBlock { try? FileManager.default.removeItem(at: root) }
        let diagnostics = FaultDiagnostics(directory: root.appendingPathComponent("logs"))
        let store = AppStore(dataDirectory: root.appendingPathComponent("data"), diagnostics: diagnostics, refreshRuntime: false)
        store.localReady = true
        store.phase = "connected"
        store.connectedAt = Date().addingTimeInterval(-120)
        store.addresses = ["192.0.2.1"]
        store.received = 5000
        return (store, root, diagnostics)
    }

    func testRepeatedRecoveryPreservesSessionAndClearsOldFailure() async throws {
        let (store, root, diagnostics) = try fixture()
        let identity = store.configuration.activeProfileID, began = store.connectedAt
        for _ in 0..<2 {
            var failure = TransportEvent(event: "diagnostic"); failure.code = "openconnect.tls_read_failed"
            store.handle(failure)
            store.handle(TransportEvent(event: "reconnecting"))
            XCTAssertEqual(store.phase, "reconnecting")
            XCTAssertTrue(store.connectionControlState.isOn)
            XCTAssertTrue(store.connectionControlState.canToggle)
            XCTAssertTrue(store.connectionControlState.isWorking)
            XCTAssertEqual(store.menuBarControlPolicy.actionTitle, "断开连接")
            XCTAssertFalse(store.menuBarControlPolicy.quickConnectEnabled)
            XCTAssertFalse(store.isError)
            store.handle(TransportEvent(event: "reconnected"))
            XCTAssertEqual(store.phase, "connected")
            XCTAssertEqual(store.connectedAt, began)
            XCTAssertEqual(store.configuration.activeProfileID, identity)
            XCTAssertEqual(store.addresses, ["192.0.2.1"])
            XCTAssertEqual(store.received, 5000)
        }
        // A later unrelated exit must not inherit a recovered TLS failure.
        store.handle(TransportEvent(event: "stopped"))
        XCTAssertEqual(store.phase, "failed")
        let report = await diagnostics.export()
        let records = report.split(separator: "\n").compactMap { line in
            (try? JSONSerialization.jsonObject(with: Data(line.utf8))) as? [String: Any]
        }
        let unexpected = records.filter { ($0["event"] as? String) == "connection.unexpected_disconnect" }
        XCTAssertEqual(unexpected.count, 1)
        XCTAssertEqual(unexpected.first?["value"] as? Int, 0, "Recovered TLS errors must not label a later unrelated exit")
        XCTAssertFalse(FileManager.default.fileExists(atPath: root.appendingPathComponent("data/configuration.json").path))
    }

    func testUserCancellationAndLateRecoveryCannotRestartSession() throws {
        let (store, _, _) = try fixture()
        store.handle(TransportEvent(event: "reconnecting"))
        store.setConnectionEnabled(false)
        XCTAssertEqual(store.phase, "idle")
        for event in ["reconnected", "reconnecting", "reconnected"] { store.handle(TransportEvent(event: event)) }
        XCTAssertEqual(store.phase, "idle")
        XCTAssertFalse(store.connectionControlState.isOn)
    }

    func testCookieRejectionAfterTLSFailureRequestsFreshAuthentication() async throws {
        let (store, root, diagnostics) = try fixture()
        store.handle(TransportEvent(event: "reconnecting"))
        for code in ["openconnect.tls_read_failed", "openconnect.tls_failed",
                     "openconnect.cookie_rejected", "openconnect.reconnect_failed"] {
            var event = TransportEvent(event: "diagnostic")
            event.code = code
            store.handle(event)
        }
        store.handle(TransportEvent(event: "error", message: "VPN 隧道未能建立或已中断。"))
        XCTAssertEqual(store.phase, "failed")
        XCTAssertTrue(store.message?.contains("服务器拒绝了会话凭据") == true)
        XCTAssertTrue(store.message?.contains("重新开启连接") == true)
        XCTAssertTrue(store.connectionControlSubtitle.contains("身份认证"))
        XCTAssertFalse(store.connectionControlState.isOn)
        XCTAssertTrue(store.connectionControlState.canToggle)
        XCTAssertFalse(store.passwordRequested, "A terminal rejection must not silently replay credentials")
        XCTAssertFalse(FileManager.default.fileExists(atPath: root.appendingPathComponent("data/configuration.json").path))

        // A late recovery notification cannot revive this rejected session.
        store.handle(TransportEvent(event: "reconnected"))
        store.handle(TransportEvent(event: "stopped"))
        XCTAssertEqual(store.phase, "failed")
        let report = await diagnostics.export()
        let records = report.split(separator: "\n").compactMap { line in
            (try? JSONSerialization.jsonObject(with: Data(line.utf8))) as? [String: Any]
        }
        let unexpected = records.filter { ($0["event"] as? String) == "connection.unexpected_disconnect" }
        XCTAssertEqual(unexpected.count, 1)
        XCTAssertEqual(unexpected.first?["value"] as? Int, DisconnectReason.cookieRejected.rawValue)
        try renderCard(store, root: root, name: "cookie-rejected")
    }

    func testRecoveryWhileAuthorizingDoesNotClaimRoutingReady() throws {
        let (store, _, _) = try fixture()
        store.phase = "authorizing"
        store.handle(TransportEvent(event: "reconnecting"))
        store.handle(TransportEvent(event: "reconnected"))
        XCTAssertEqual(store.phase, "authorizing")
        store.phase = "testing"
        store.testOnly = true
        store.handle(TransportEvent(event: "reconnecting"))
        XCTAssertFalse(store.connectionControlState.isOn)
        store.handle(TransportEvent(event: "reconnected"))
        XCTAssertEqual(store.phase, "testing")
    }

    func testRecoveryCardRendersWithCancelableEnabledSwitch() throws {
        let (store, root, _) = try fixture()
        store.handle(TransportEvent(event: "reconnecting"))
        try renderCard(store, root: root, name: "reconnecting")
        XCTAssertEqual(store.connectionControlState.title, "正在恢复连接")
        XCTAssertTrue(store.connectionControlState.isOn && store.connectionControlState.canToggle)
    }

    private func renderCard(_ store: AppStore, root: URL, name: String) throws {
        let host = NSHostingView(rootView: VPNConnectionCard(store: store).padding(24).frame(width: 760))
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 760, height: 460), styleMask: [.borderless], backing: .buffered, defer: false)
        window.contentView = host
        host.frame = window.contentView!.bounds
        host.layoutSubtreeIfNeeded()
        let bitmap = try XCTUnwrap(host.bitmapImageRepForCachingDisplay(in: host.bounds))
        host.cacheDisplay(in: host.bounds, to: bitmap)
        let data = try XCTUnwrap(bitmap.representation(using: .png, properties: [:]))
        let output = ProcessInfo.processInfo.environment["GOCONNECT_TEST_RENDER_DIR"].map { URL(fileURLWithPath: $0) } ?? root
        try data.write(to: output.appendingPathComponent(name + ".png"))
        window.contentView = nil
    }
}

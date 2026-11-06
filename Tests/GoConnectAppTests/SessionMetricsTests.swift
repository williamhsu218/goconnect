import AppKit
import XCTest
import GoConnectCore
@testable import GoConnect

@MainActor
final class SessionMetricsTests: XCTestCase {
    private func fixture() throws -> (AppStore, PublicIPChecker, URL) {
        _ = NSApplication.shared
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("GoConnect-metrics-" + UUID().uuidString)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        addTeardownBlock { try? FileManager.default.removeItem(at: root) }
        let checker = PublicIPChecker()
        let store = AppStore(dataDirectory: root, diagnostics: FaultDiagnostics(directory: root.appendingPathComponent("logs")), refreshRuntime: false, publicIPChecker: checker)
        return (store, checker, root)
    }

    func testStatisticsFailurePreservesTotalsAndLateSamplesAreIgnored() throws {
        let (store,_,_) = try fixture()
        store.phase = "connected"; store.connectedAt = Date()
        var event = TransportEvent(event: "stats"); event.received = 8192; event.sent = 123
        store.handle(event)
        XCTAssertTrue(store.trafficAvailable)
        XCTAssertNotNil(store.trafficUpdatedAt)
        store.handle(TransportEvent(event: "stats_unavailable"))
        XCTAssertFalse(store.trafficAvailable)
        XCTAssertEqual(store.received,8192); XCTAssertEqual(store.sent,123)
        event.received = 0; event.sent = 0
        store.handle(event)
        XCTAssertTrue(store.trafficAvailable, "A successful idle sample is distinct from an unavailable sample")
        store.disconnect()
        event.received = 999
        store.handle(event)
        XCTAssertFalse(store.trafficAvailable)
        XCTAssertEqual(store.received,0, "Old statistics cannot enter an ended session")
    }

    func testPublicIPFailureAndDisconnectDoNotKeepAnOldAddress() throws {
        let (store,checker,_) = try fixture()
        store.phase = "connected"; store.connectedAt = Date()
        checker.onResult?("2606:4700:4700::1111")
        XCTAssertEqual(store.publicIP,"2606:4700:4700::1111")
        XCTAssertNotNil(store.publicIPCheckedAt)
        checker.onResult?(nil)
        XCTAssertTrue(store.publicIPFailed); XCTAssertNil(store.publicIP)
        checker.onResult?("1.1.1.1")
        store.disconnect()
        checker.onResult?("8.8.8.8")
        XCTAssertNil(store.publicIP); XCTAssertNil(store.publicIPCheckedAt)
    }

    func testPublicIPCheckerFramesOutputAndCancelsOldGeneration() async throws {
        let (_,checker,root) = try fixture()
        let bin = root.appendingPathComponent("bin")
        try FileManager.default.createDirectory(at: bin, withIntermediateDirectories:true)
        let program = bin.appendingPathComponent("GoConnectTransport")
        try "#!/bin/sh\ncat >/dev/null\nprintf '{\"event\":\"public_ip\",'\nsleep 0.1\nprintf '\"publicIP\":\"1.1.1.1\"}\\n'\n".write(to:program,atomically:true,encoding:.utf8)
        try FileManager.default.setAttributes([.posixPermissions:0o700],ofItemAtPath:program.path)
        var results: [String?] = []
        let returned = expectation(description: "current helper returns its complete frame")
        returned.assertForOverFulfill = true
        checker.onResult = { results.append($0); returned.fulfill() }
        try checker.start(runtime:root,port:12345,token:String(repeating:"x",count:40))
        checker.stop()
        try checker.start(runtime:root,port:12345,token:String(repeating:"x",count:40))
        await fulfillment(of: [returned], timeout: 5)
        XCTAssertEqual(results.count,1)
        XCTAssertEqual(results.compactMap { $0 },["1.1.1.1"])
        checker.stop()
    }
}

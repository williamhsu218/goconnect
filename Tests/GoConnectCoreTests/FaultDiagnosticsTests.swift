import XCTest
@testable import GoConnectCore

final class FaultDiagnosticsTests: XCTestCase {
    func testBridgeDiagnosticsRemainTypedPrivateAndSeparate() async throws {
        let base = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: base) }
        let text = #"{"stage":"udp_write","outcome":"io_error","transaction":42,"flow":1,"packetKind":1,"bytes":0,"errno":32,"timeMicros":123456,"sequence":1,"dropped":0,"pid":123,"password":"PRIVATE-CANARY"}"#
        let decoder = JSONDecoder()
        let observation = try decoder.decode(BridgeDiagnostic.self, from: Data(text.utf8))
        let logger = FaultDiagnostics(directory: base)
        logger.recordBridge(observation)
        logger.recordBridge(try decoder.decode(BridgeDiagnostic.self, from: Data(text.replacingOccurrences(of: "udp_write", with: "udp_queue").utf8)))
        let output = await logger.export()
        XCTAssertTrue(output.contains("bridge.trace"))
        XCTAssertTrue(output.contains("udp_write"))
        XCTAssertTrue(output.contains("udp_queue"))
        XCTAssertFalse(output.contains("PRIVATE-CANARY"))
        XCTAssertFalse(FileManager.default.fileExists(atPath: base.appendingPathComponent("app.jsonl").path))
        let attrs = try FileManager.default.attributesOfItem(atPath: base.appendingPathComponent("bridge.jsonl").path)
        XCTAssertEqual((attrs[.posixPermissions] as! NSNumber).intValue, 0o600)
        XCTAssertThrowsError(try decoder.decode(BridgeDiagnostic.self, from: Data(text.replacingOccurrences(of: "udp_write", with: "PRIVATE-CANARY").utf8)))
        let invalid = try decoder.decode(BridgeDiagnostic.self, from: Data(text.replacingOccurrences(of: #""bytes":0"#, with: #""bytes":-1"#).utf8))
        XCTAssertFalse(invalid.isValid)
    }
    func testProbeDoesNotAccumulateAndSeparatesSchedulerGap() {
        var state = ResponsivenessState()
        XCTAssertTrue(state.tick(now: 0).enqueue)
        for t in 1...4 { XCTAssertFalse(state.tick(now: Double(t)).enqueue) }
        XCTAssertEqual(state.tick(now: 5).event, .mainStalled)
        XCTAssertNil(state.tick(now: 6).event)
        XCTAssertEqual(state.acknowledge(now: 7), 7)
        XCTAssertTrue(state.tick(now: 8).enqueue)
        XCTAssertEqual(state.tick(now: 100).event, .schedulerGap)
        XCTAssertFalse(state.tick(now: 101).enqueue)
        XCTAssertNil(state.acknowledge(now: 101))
        XCTAssertTrue(state.tick(now: 102).enqueue)
        state.resume(now: 500)
        XCTAssertNil(state.tick(now: 501).event)
        XCTAssertFalse(state.tick(now: 501).enqueue)
        _ = state.acknowledge(now: 502)
        let gapWithoutProbe = state.tick(now: 900)
        XCTAssertEqual(gapWithoutProbe.event, .schedulerGap)
        XCTAssertTrue(gapWithoutProbe.enqueue)
    }
    func testPersistenceRotationAndExport() async throws {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: dir) }
        let logger = FaultDiagnostics(directory: dir, limit: 400)
        for _ in 0..<20 { logger.record(.mainStalled, value: 6) }
        _ = await logger.export()
        let reopened = FaultDiagnostics(directory: dir, limit: 400)
        let output = await reopened.export()
        XCTAssertTrue(output.contains("main.stalled"))
        let files = try FileManager.default.contentsOfDirectory(at: dir, includingPropertiesForKeys: nil)
        XCTAssertEqual(files.count, 3)
        for file in files {
            let attrs = try FileManager.default.attributesOfItem(atPath: file.path)
            XCTAssertLessThanOrEqual((attrs[.size] as! NSNumber).intValue, 400)
            XCTAssertEqual((attrs[.posixPermissions] as! NSNumber).intValue, 0o600)
        }
    }
    func testRejectSymlinkAndKeepTargetUntouched() async throws {
        let base = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: base) }
        try FileManager.default.createDirectory(at: base, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        let target = base.appendingPathComponent("target")
        try "SECRET".write(to: target, atomically: true, encoding: .utf8)
        try FileManager.default.createSymbolicLink(at: base.appendingPathComponent("app.jsonl"), withDestinationURL: target)
        let logger = FaultDiagnostics(directory: base)
        logger.record(.mainStalled)
        let export = await logger.export()
        XCTAssertFalse(export.contains("SECRET"))
        XCTAssertEqual(try String(contentsOf: target), "SECRET")
    }
}

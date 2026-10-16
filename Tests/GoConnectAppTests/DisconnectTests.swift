import AppKit
import XCTest
import GoConnectCore
@testable import GoConnect

@MainActor
final class DisconnectTests: XCTestCase {
    func testCleanUnexpectedExitIsVisibleAndPersistedOnce() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        _ = NSApplication.shared
        let diagnostics = FaultDiagnostics(directory: directory.appendingPathComponent("Logs"))
        let store = AppStore(dataDirectory: directory, diagnostics: diagnostics, refreshRuntime: false)
        store.phase = "connected"; store.connectedAt = Date().addingTimeInterval(-300)
        store.handle(TransportEvent(event: "stopped"))
        store.handle(TransportEvent(event: "stopped"))
        XCTAssertEqual(store.phase, "failed")
        XCTAssertTrue(store.isError)
        XCTAssertTrue(store.message?.contains("连接意外断开") == true)
        XCTAssertEqual(store.activity.filter { $0.message.contains("连接意外断开") }.count, 1)
        let output = await diagnostics.export()
        XCTAssertEqual(output.components(separatedBy: "connection.unexpected_disconnect").count - 1, 1)
        XCTAssertTrue(output.contains("connection.duration_seconds"))
        XCTAssertTrue(output.contains("connection.disconnect_source"))
    }

    func testCoreReasonSurvivesGenericTermination() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        _ = NSApplication.shared
        let diagnostics = FaultDiagnostics(directory: directory.appendingPathComponent("Logs"))
        let store = AppStore(dataDirectory: directory, diagnostics: diagnostics, refreshRuntime: false)
        store.phase = "connected"; store.connectedAt = Date()
        for code in ["openconnect.idle_timeout", "openconnect.server_disconnect"] {
            var event = TransportEvent(event: "diagnostic"); event.code = code
            store.handle(event)
        }
        store.handle(TransportEvent(event: "error", message: "generic failure"))
        XCTAssertEqual(store.phase, "failed")
        XCTAssertTrue(store.message?.contains("空闲超时") == true)
        XCTAssertFalse(store.message?.contains("generic failure") == true)
        _ = await diagnostics.export()
    }

    func testUserDisconnectDoesNotBecomeUnexpected() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let diagnostics = FaultDiagnostics(directory: directory.appendingPathComponent("Logs"))
        let store = AppStore(dataDirectory: directory, diagnostics: diagnostics, refreshRuntime: false)
        store.phase = "connected"; store.connectedAt = Date()
        store.disconnect()
        store.handle(TransportEvent(event: "stopped"))
        XCTAssertEqual(store.phase, "idle")
        XCTAssertEqual(store.message, "已断开。")
        let output = await diagnostics.export()
        XCTAssertTrue(output.contains("connection.disconnect_requested"))
        XCTAssertFalse(output.contains("connection.unexpected_disconnect"))
    }
}

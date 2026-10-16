import XCTest
import GoConnectCore
@testable import GoConnect

@MainActor
final class TransportDiagnosticsTests: XCTestCase {
    func testFinalDiagnosticsPersistAfterCleanupAndRejectUnknownCodes() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let bin = directory.appendingPathComponent("bin")
        try FileManager.default.createDirectory(at: bin, withIntermediateDirectories: true)
        let executable = bin.appendingPathComponent("GoConnectTransport")
        try """
        #!/bin/sh
        IFS= read -r request
        echo '{"event":"diagnostic","code":"openconnect.idle_timeout","value":0}'
        echo '{"event":"error","message":"fixture error"}'
        echo '{"event":"diagnostic","code":"openconnect.exit_code","value":1}'
        echo '{"event":"diagnostic","code":"PRIVATE_COOKIE_SECRET","value":0}'
        echo '{"event":"diagnostic","code":"connection.disconnect_requested","value":0}'
        echo '{"event":"stopped"}'
        """.write(to: executable, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: executable.path)
        let diagnostics = FaultDiagnostics(directory: directory.appendingPathComponent("Logs"))
        let client = TransportClient(runtimeURL: directory, diagnostics: diagnostics)
        let ended = expectation(description: "process exited after final diagnostics")
        var received: [String] = []
        client.onEvent = { event in
            if let code = event.code { received.append(code) }
            if event.event == "error" { client.stop() }
            if event.event == "stopped" { ended.fulfill() }
        }
        var profile = VPNProfile(); profile.server = "https://example.invalid"; profile.username = "private-fixture-user"
        try client.start(profile: profile, password: "private-fixture-password", token: String(repeating: "t", count: 32))
        await fulfillment(of: [ended], timeout: 5)
        XCTAssertFalse(client.isRunning)
        XCTAssertEqual(received, ["openconnect.idle_timeout", "openconnect.exit_code"])
        let output = await diagnostics.export()
        XCTAssertTrue(output.contains("openconnect.idle_timeout"))
        XCTAssertTrue(output.contains("openconnect.exit_code"))
        XCTAssertTrue(output.contains("transport.stop_requested"))
        XCTAssertFalse(output.contains("PRIVATE_COOKIE_SECRET"))
        XCTAssertFalse(output.contains("private-fixture"))
        XCTAssertFalse(output.contains("connection.disconnect_requested"))
    }
}

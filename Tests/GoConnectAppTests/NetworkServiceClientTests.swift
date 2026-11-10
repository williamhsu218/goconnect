import XCTest
import Darwin
import GoConnectCore
@testable import GoConnect

/// In-memory replies only. No helper, installed service, credentials or VPN access.
actor ServiceStatusScript {
    enum Step: Sendable {
        case value(NetworkServiceStatus)
        case failure
        case suspended
    }
    private var steps: [Step]
    private let fallback: NetworkServiceStatus
    private var waiters: [CheckedContinuation<NetworkServiceStatus, Error>] = []
    private(set) var reads = 0

    init(_ steps: [Step] = [], fallback: NetworkServiceStatus = NetworkServiceStatus(state: "ready")) {
        self.steps = steps; self.fallback = fallback
    }
    func read() async throws -> NetworkServiceStatus {
        reads += 1
        switch steps.isEmpty ? .value(fallback) : steps.removeFirst() {
        case .value(let value): return value
        case .failure: throw CocoaError(.fileReadUnknown)
        case .suspended:
            return try await withCheckedThrowingContinuation { waiters.append($0) }
        }
    }
    func resumeNext(_ value: NetworkServiceStatus) {
        precondition(!waiters.isEmpty)
        waiters.removeFirst().resume(returning: value)
    }
}

@MainActor
func waitForServiceReads(_ count: Int, script: ServiceStatusScript,
                         file: StaticString = #filePath, line: UInt = #line) async throws {
    for _ in 0..<500 {
        if await script.reads >= count { return }
        try await Task.sleep(for: .milliseconds(2))
    }
    XCTFail("Isolated status reader did not start", file: file, line: line)
}

@MainActor
final class NetworkServiceClientTests: XCTestCase {
    func testOnlyReadyOrMissingInstallationCanConfirmIdle() {
        for state in ["ready", "notInstalled"] {
            XCTAssertTrue(NetworkServiceStatus(state: state).confirmsIdleForUpdate)
            XCTAssertFalse(NetworkServiceStatus(state: state, busy: true).confirmsIdleForUpdate)
        }
        for state in ["checking", "needsUpdate", "requiresAuthorization", "unavailable", "unknown"] {
            XCTAssertFalse(NetworkServiceStatus(state: state).confirmsIdleForUpdate)
        }
    }

    func testLateRefreshCannotOverwriteMaintenanceStatus() async throws {
        let script = ServiceStatusScript([.suspended, .suspended])
        let client = NetworkServiceClient(statusReader: { _ in try await script.read() })
        let refresh = Task { await client.refreshStatus() }
        try await waitForServiceReads(1, script: script)
        let maintenance = Task { try await client.ensureReady() }
        try await waitForServiceReads(2, script: script)
        XCTAssertTrue(client.working)
        await script.resumeNext(NetworkServiceStatus(state: "ready"))
        let obsolete = await refresh.value
        XCTAssertNil(obsolete)
        XCTAssertEqual(client.status.state, "checking", "Old diagnostic read must not publish during maintenance")
        await script.resumeNext(NetworkServiceStatus(state: "ready", busy: true))
        try await maintenance.value
        XCTAssertTrue(client.status.busy)
        XCTAssertFalse(client.working)
    }

    func testTimedOutReadCannotPublishALateIdleReply() async throws {
        let script = ServiceStatusScript([.suspended])
        let client = NetworkServiceClient(statusReader: { _ in try await script.read() })
        client.requireIdleConfirmation()
        let result = await client.refreshStatus(timeout: .milliseconds(30))
        XCTAssertEqual(result?.state, "unavailable")
        XCTAssertTrue(client.blocksUpdateInstallation)
        await script.resumeNext(NetworkServiceStatus(state: "ready"))
        await Task.yield()
        XCTAssertEqual(client.status.state, "unavailable")
        XCTAssertTrue(client.blocksUpdateInstallation)
    }

    func testCancelledReadPreservesGateUntilANewExplicitRead() async throws {
        let script = ServiceStatusScript([.suspended, .value(NetworkServiceStatus(state: "ready"))])
        let client = NetworkServiceClient(statusReader: { _ in try await script.read() })
        client.requireIdleConfirmation()
        let read = Task { await client.refreshStatus() }
        try await waitForServiceReads(1, script: script)
        read.cancel()
        let cancelled = await read.value
        XCTAssertNil(cancelled)
        XCTAssertTrue(client.blocksUpdateInstallation)
        await script.resumeNext(NetworkServiceStatus(state: "ready"))
        await Task.yield()
        XCTAssertTrue(client.blocksUpdateInstallation)
        _ = await client.refreshStatus()
        XCTAssertFalse(client.blocksUpdateInstallation)
    }

    private func stalledHelper() throws -> (helper: URL, marker: URL) {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("GoConnect-StatusProcess-" + UUID().uuidString)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: false)
        let helper = root.appendingPathComponent("status-helper"), marker = root.appendingPathComponent("pid")
        // exec keeps exactly one owned child; ignore TERM to exercise the bounded
        // kill fallback, without grandchildren retaining the output pipe.
        let script = "#!/bin/sh\nprintf '%s' \"$$\" > \(ShellQuote.argument(marker.path))\ntrap '' TERM\nexec /bin/sleep 60\n"
        try script.write(to: helper, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: helper.path)
        addTeardownBlock {
            if let text = try? String(contentsOf: marker, encoding: .utf8), let pid = Int32(text) {
                // Only kill a still-owned child; never act on a recycled PID.
                var status: Int32 = 0
                if waitpid(pid, &status, WNOHANG) == 0 { _ = kill(pid, SIGKILL) }
            }
            try? FileManager.default.removeItem(at: root)
        }
        return (helper, marker)
    }
    private func helperPID(_ marker: URL) async throws -> Int32 {
        for _ in 0..<500 {
            if let text = try? String(contentsOf: marker, encoding: .utf8), let pid = Int32(text) { return pid }
            try await Task.sleep(for: .milliseconds(2))
        }
        throw CocoaError(.fileReadUnknown)
    }
    private func assertHelperExited(_ pid: Int32, file: StaticString = #filePath, line: UInt = #line) async throws {
        for _ in 0..<500 {
            if kill(pid, 0) != 0 && errno == ESRCH { return }
            try await Task.sleep(for: .milliseconds(2))
        }
        XCTFail("Status-only child survived cancellation/deadline", file: file, line: line)
    }

    func testProductionStatusReaderTerminatesTimedOutChild() async throws {
        let (helper, marker) = try stalledHelper()
        let client = NetworkServiceClient(statusReader: { _ in try await NetworkServiceClient.readStatus(helper) })
        client.requireIdleConfirmation()
        let read = Task { await client.refreshStatus(timeout: .milliseconds(300)) }
        let pid = try await helperPID(marker)
        let status = await read.value
        XCTAssertEqual(status?.state, "unavailable")
        XCTAssertTrue(client.blocksUpdateInstallation)
        try await assertHelperExited(pid)
    }

    func testProductionStatusReaderTerminatesCancelledChild() async throws {
        let (helper, marker) = try stalledHelper()
        let client = NetworkServiceClient(statusReader: { _ in try await NetworkServiceClient.readStatus(helper) })
        client.requireIdleConfirmation()
        let read = Task { await client.refreshStatus() }
        let pid = try await helperPID(marker)
        read.cancel()
        let status = await read.value
        XCTAssertNil(status)
        XCTAssertTrue(client.blocksUpdateInstallation)
        try await assertHelperExited(pid)
    }
}

import Foundation
import XCTest
import GoConnectCore
@testable import GoConnect

@MainActor
final class RouteHeartbeatTests: XCTestCase {
    func testQueuedOldOwnerConfirmationCannotExtendWatchdog() throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("GoConnect-delayed-confirmation-" + UUID().uuidString)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: root) }
        let lease = root.appendingPathComponent("lease")
        try Data("synthetic-token".utf8).write(to: lease)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: lease.path)
        let queue = DispatchQueue(label: "GoConnect.delayed-renewal-fixture")
        let heartbeat = try RouteLeaseHeartbeat(lease: lease, interval: 0.1, ownerTimeout: 0.3, renewalQueue: queue)
        defer { heartbeat.stop() }
        Thread.sleep(forTimeInterval: 0.03)
        let entered = DispatchSemaphore(value: 0), release = DispatchSemaphore(value: 0)
        queue.async { entered.signal(); release.wait() }
        XCTAssertEqual(entered.wait(timeout: .now() + 2), .success)
        heartbeat.ownerResponded()
        Thread.sleep(forTimeInterval: 0.5)
        release.signal()
        Thread.sleep(forTimeInterval: 0.1)
        XCTAssertFalse(FileManager.default.fileExists(atPath: lease.path), "Queued old confirmation is not a new owner response")
    }

    func testLeaseRenewalCannotFollowReplacementOrOpenSymlink() throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("GoConnect-lease-inode-" + UUID().uuidString)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: root) }
        let lease = root.appendingPathComponent("lease"), target = root.appendingPathComponent("untouched")
        try Data("synthetic-token".utf8).write(to: lease)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: lease.path)
        let heartbeat = try RouteLeaseHeartbeat(lease: lease, interval: 0.02, ownerTimeout: 0.2)
        defer { heartbeat.stop() }
        try FileManager.default.removeItem(at: lease)
        try Data("untouched".utf8).write(to: target)
        let stamp = Date(timeIntervalSince1970: 1000)
        try FileManager.default.setAttributes([.modificationDate: stamp], ofItemAtPath: target.path)
        try FileManager.default.createSymbolicLink(at: lease, withDestinationURL: target)
        Thread.sleep(forTimeInterval: 1.2)
        XCTAssertEqual(try FileManager.default.attributesOfItem(atPath: target.path)[.modificationDate] as? Date, stamp)
        XCTAssertThrowsError(try RouteLeaseHeartbeat(lease: lease))
        XCTAssertEqual(try FileManager.default.destinationOfSymbolicLink(atPath: lease.path), target.path)
        heartbeat.stop(); heartbeat.stop()
    }

    func testPermanentlyUnresponsiveOwnerStillRevokesLease() throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("GoConnect-hung-owner-" + UUID().uuidString)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: root) }
        let lease = root.appendingPathComponent("lease")
        try Data("synthetic-token".utf8).write(to: lease)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: lease.path)
        let heartbeat = try RouteLeaseHeartbeat(lease: lease, interval: 0.02, ownerTimeout: 0.3)
        defer { heartbeat.stop() }
        // A responsive owner extends the deadline; sustained silence revokes it.
        for _ in 0..<5 {
            Thread.sleep(forTimeInterval: 0.1)
            heartbeat.ownerResponded()
            XCTAssertTrue(FileManager.default.fileExists(atPath: lease.path))
        }
        Thread.sleep(forTimeInterval: 0.5)
        XCTAssertFalse(FileManager.default.fileExists(atPath: lease.path))
        heartbeat.ownerResponded()
        Thread.sleep(forTimeInterval: 0.1)
        XCTAssertFalse(FileManager.default.fileExists(atPath: lease.path))
    }

    func testLeaseRenewsWhileMainActorIsBlockedAndCannotReturnAfterStop() throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("GoConnect-heartbeat-" + UUID().uuidString)
        try FileManager.default.createDirectory(at: root.appendingPathComponent("runtime/bin"), withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: root) }
        let helper = root.appendingPathComponent("runtime/bin/GoConnectTransport")
        try "#!/bin/sh\nexec /bin/sleep 60\n".write(to: helper, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: helper.path)
        let controls = root.appendingPathComponent("controls")
        try FileManager.default.createDirectory(at: controls, withIntermediateDirectories: true)
        let route = RouteClient(temporaryDirectory: controls)
        try route.start(port: 12345, token: String(repeating: "x", count: 40), apps: [], gateway: "192.0.2.1", runtime: root.appendingPathComponent("runtime"), mode: .whitelist)
        defer { route.stop() }
        let session = try XCTUnwrap(FileManager.default.contentsOfDirectory(at: controls, includingPropertiesForKeys: nil).first)
        let lease = session.appendingPathComponent("lease")
        // Reproduce the incident's >5 s scheduling gap without saturating the
        // host or touching the installed app/service/network.
        Thread.sleep(forTimeInterval: 6.5)
        let stamp = try XCTUnwrap(FileManager.default.attributesOfItem(atPath: lease.path)[.modificationDate] as? Date)
        XCTAssertLessThan(Date().timeIntervalSince(stamp), 2.5, "UI starvation must not starve the supervisor lease")
        route.stop()
        XCTAssertFalse(FileManager.default.fileExists(atPath: lease.path))
        Thread.sleep(forTimeInterval: 1.2)
        XCTAssertFalse(FileManager.default.fileExists(atPath: lease.path), "A queued renewal must never resurrect a revoked lease")
    }
}

import AppKit
import SQLite3
import XCTest
import GoConnectCore
@testable import GoConnect

@MainActor
final class ProfileTrafficStoreTests: XCTestCase {
    private func file() throws -> URL {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("GoConnect-traffic-tests-" + UUID().uuidString)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        addTeardownBlock { try? FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: root.path); try? FileManager.default.removeItem(at: root) }
        return root.appendingPathComponent("traffic.sqlite")
    }

    func testCumulativeSamplesDuplicatesAndCoreResetPreserveOnlyDeltas() throws {
        let store = ProfileTrafficStore(file: try file()), id = UUID()
        store.beginSession(profileID: id, name: "A")
        store.consume(received: 100, sent: 10)
        store.consume(received: 140, sent: 15)
        store.consume(received: 140, sent: 15)
        store.consume(received: 5, sent: 2)
        XCTAssertEqual(store.totals(profileID: id).received, 145)
        XCTAssertEqual(store.totals(profileID: id).sent, 17)
        store.endSession(); store.consume(received: 999, sent: 999)
        XCTAssertEqual(store.totals(profileID: id).received, 145)
    }

    func testReconnectAndSwitchProfileCannotMixCounters() throws {
        let store = ProfileTrafficStore(file: try file()), a = UUID(), b = UUID()
        store.beginSession(profileID: a, name: "A"); store.consume(received: 100, sent: 10)
        store.beginSession(profileID: a, name: "Renamed"); store.consume(received: 20, sent: 2)
        store.beginSession(profileID: b, name: "B"); store.consume(received: 7, sent: 1)
        XCTAssertEqual(store.totals(profileID: a).total, 132)
        XCTAssertEqual(store.totals(profileID: b).total, 8)
        XCTAssertEqual(store.profiles.first(where: { $0.id == a })?.name, "Renamed")
    }

    func testCoreResetKeepsBothDirectionsWhenOnlyOneFallsBelowPreviousSample() throws {
        for (beforeRX, beforeTX, afterRX, afterTX) in [(1000, 50, 200, 80), (50, 1000, 80, 200)] {
            let url = try file(), id = UUID()
            let store = ProfileTrafficStore(file: url)
            store.beginSession(profileID: id, name: "A")
            store.consume(received: UInt64(beforeRX), sent: UInt64(beforeTX))
            // Both core counters restarted, but one already exceeded its prior value
            // before the first new sample arrived.
            store.consume(received: UInt64(afterRX), sent: UInt64(afterTX))
            store.consume(received: UInt64(afterRX), sent: UInt64(afterTX))
            XCTAssertEqual(store.totals(profileID: id).received, UInt64(beforeRX + afterRX))
            XCTAssertEqual(store.totals(profileID: id).sent, UInt64(beforeTX + afterTX))
            XCTAssertTrue(store.flush())
            let loaded = ProfileTrafficStore(file: url)
            XCTAssertEqual(loaded.totals(profileID: id).total, UInt64(beforeRX + beforeTX + afterRX + afterTX))
        }
    }

    func testFlushAndReloadAreIdempotentAndTimezoneIsPersisted() throws {
        let url = try file(), id = UUID()
        let store = ProfileTrafficStore(file: url, timeZone: TimeZone(identifier: "Asia/Shanghai")!)
        store.beginSession(profileID: id, name: "A"); store.consume(received: 120, sent: 30)
        XCTAssertTrue(store.flush()); XCTAssertTrue(store.flush())
        let loaded = ProfileTrafficStore(file: url, timeZone: TimeZone(identifier: "America/New_York")!)
        XCTAssertEqual(loaded.timeZone.identifier, "Asia/Shanghai")
        XCTAssertEqual(loaded.totals(profileID: id).total, 150)
        XCTAssertTrue(loaded.storageError == nil)
        let mode = try FileManager.default.attributesOfItem(atPath: url.path)[.posixPermissions] as? Int
        XCTAssertEqual(mode, 0o600)
    }

    func testFullUnsignedCounterRangeSurvivesSQLiteRoundTrip() throws {
        let url = try file(), id = UUID(), large = UInt64(Int64.max) + 100
        let store = ProfileTrafficStore(file: url)
        store.beginSession(profileID: id, name: "A"); store.consume(received: large, sent: 5); store.endSession()
        let loaded = ProfileTrafficStore(file: url)
        XCTAssertEqual(loaded.totals(profileID: id).received, large)
        XCTAssertEqual(loaded.totals(profileID: id).sent, 5)
    }

    func testCorruptOrFutureDatabaseIsPreservedWithoutOverwrite() throws {
        let url = try file(), original = Data("not a sqlite database".utf8)
        try original.write(to: url)
        let corrupt = ProfileTrafficStore(file: url)
        XCTAssertFalse(corrupt.readable)
        corrupt.beginSession(profileID: UUID(), name: "A"); corrupt.consume(received: 20, sent: 10)
        XCTAssertFalse(corrupt.flush()); XCTAssertEqual(try Data(contentsOf: url), original)
        let futureURL = try file()
        var database: OpaquePointer?
        XCTAssertEqual(sqlite3_open(futureURL.path, &database), SQLITE_OK)
        XCTAssertEqual(sqlite3_exec(database, "PRAGMA user_version=99", nil, nil, nil), SQLITE_OK)
        sqlite3_close(database)
        let before = try Data(contentsOf: futureURL)
        XCTAssertFalse(ProfileTrafficStore(file: futureURL).readable)
        XCTAssertEqual(try Data(contentsOf: futureURL), before)
    }

    func testSymlinkTargetCannotBeReadOrModified() throws {
        let url = try file(), target = url.deletingLastPathComponent().appendingPathComponent("protected")
        let original = Data("protected bytes".utf8); try original.write(to: target)
        try FileManager.default.createSymbolicLink(at: url, withDestinationURL: target)
        let store = ProfileTrafficStore(file: url)
        XCTAssertFalse(store.readable); XCTAssertFalse(store.flush())
        XCTAssertEqual(try Data(contentsOf: target), original)
    }

    func testSaveFailureRetainsPendingDataForRetry() throws {
        if getuid() == 0 { throw XCTSkip("permission failure requires ordinary user") }
        let url = try file(), root = url.deletingLastPathComponent(), id = UUID()
        let store = ProfileTrafficStore(file: url)
        store.beginSession(profileID: id, name: "A"); store.consume(received: 10, sent: 2)
        try FileManager.default.setAttributes([.posixPermissions: 0o500], ofItemAtPath: root.path)
        defer { try? FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: root.path) }
        XCTAssertFalse(store.flush()); XCTAssertNotNil(store.storageError)
        XCTAssertEqual(store.totals(profileID: id).total, 12)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: root.path)
        XCTAssertTrue(store.flush()); XCTAssertNil(store.storageError)
        XCTAssertEqual(ProfileTrafficStore(file: url).totals(profileID: id).total, 12)
    }

    func testAppStoreExcludesDiagnosticAndLateSamplesAndPinsProfileIdentity() throws {
        _ = NSApplication.shared
        let url = try file(), root = url.deletingLastPathComponent()
        let store = AppStore(dataDirectory: root, diagnostics: FaultDiagnostics(directory: root.appendingPathComponent("logs")), refreshRuntime: false)
        let original = store.configuration.activeProfileID
        store.trafficHistory.beginSession(profileID: original, name: "A")
        store.phase = "connected"; store.connectedAt = Date()
        try store.configuration.add(name: "B", copyCurrent: false)
        let other = store.configuration.activeProfileID
        var event = TransportEvent(event: "stats"); event.received = 100; event.sent = 20
        store.handle(event)
        XCTAssertEqual(store.trafficHistory.totals(profileID: original).total, 120)
        XCTAssertFalse(store.trafficHistory.totals(profileID: other).hasData)
        store.testOnly = true; store.phase = "testing"; event.received = 500; store.handle(event)
        XCTAssertEqual(store.trafficHistory.totals(profileID: original).total, 120)
        store.disconnect(); event.received = 999; store.handle(event)
        XCTAssertEqual(store.trafficHistory.totals(profileID: original).total, 120)
        XCTAssertEqual(ProfileTrafficStore(file: root.appendingPathComponent("traffic-history.sqlite")).totals(profileID: original).total, 120)
    }

    func testUnavailableStatisticsDoNotResetHistoricalDeltaBaseline() throws {
        _ = NSApplication.shared
        let root = try file().deletingLastPathComponent()
        let store = AppStore(dataDirectory: root, diagnostics: FaultDiagnostics(directory: root.appendingPathComponent("logs")), refreshRuntime: false)
        let id = store.configuration.activeProfileID
        store.trafficHistory.beginSession(profileID: id, name: "A")
        store.phase = "connected"; store.connectedAt = Date()
        var event = TransportEvent(event: "stats"); event.received = 100; event.sent = 10
        store.handle(event); store.handle(TransportEvent(event: "stats_unavailable"))
        event.received = 160; event.sent = 15; store.handle(event)
        XCTAssertEqual(store.trafficHistory.totals(profileID: id).total, 175)
        store.disconnect()
    }

    func testBrowsingHistoryNeverSwitchesConnectionOrBypassesDraftConfirmation() throws {
        _ = NSApplication.shared
        let root = try file().deletingLastPathComponent()
        let store = AppStore(dataDirectory: root, diagnostics: FaultDiagnostics(directory: root.appendingPathComponent("logs")), refreshRuntime: false)
        let original = store.configuration.activeProfileID
        try store.configuration.add(name: "B", copyCurrent: false)
        let active = store.configuration.activeProfileID
        store.showTraffic(for: original)
        XCTAssertEqual(store.page, .traffic); XCTAssertEqual(store.configuration.activeProfileID, active)
        XCTAssertFalse(FileManager.default.fileExists(atPath: store.file.url.path))
        store.beginEditingProfile(original)
        store.profileDraft?.connection.profile.name = "Unsaved"
        store.showTraffic(for: active)
        XCTAssertEqual(store.page, .profiles); XCTAssertTrue(store.discardProfileChangesRequested)
        XCTAssertEqual(store.profileDraft?.connection.profile.name, "Unsaved")
        store.discardProfileChanges()
        XCTAssertEqual(store.page, .traffic); XCTAssertEqual(store.configuration.activeProfileID, active)
    }

    func testPendingSamplesAreSavedEvenWithoutAnotherStatisticsEvent() async throws {
        let url = try file(), id = UUID()
        let store = ProfileTrafficStore(file: url, flushInterval: 0.05)
        store.beginSession(profileID: id, name: "A"); store.consume(received: 100, sent: 20)
        try await Task.sleep(for: .milliseconds(200))
        XCTAssertEqual(ProfileTrafficStore(file: url).totals(profileID: id).total, 120)
        store.endSession()
    }
}

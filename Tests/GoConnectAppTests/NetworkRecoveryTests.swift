import AppKit
import XCTest
@testable import GoConnect
import GoConnectCore

@MainActor
final class NetworkRecoveryTests: XCTestCase {
    func testWaitingUsesStableDefaultGatewayAndAcceptsSelectedBridge() async {
        let old = RecoveryNetworkPath(interface: "bridge0", gateway: "192.0.2.1")
        let new = RecoveryNetworkPath(interface: "bridge0", gateway: "192.0.2.2")
        let paths: [RecoveryNetworkPath?] = [nil, old, new, new]
        var reads = 0
        let reached = await NetworkRecovery.waitForStablePath(until: .now.advanced(by: .seconds(2)),
                                                              stableFor: .milliseconds(5), interval: .milliseconds(15)) { _ in
            defer { reads += 1 }
            return paths[min(reads, paths.count - 1)]
        }
        XCTAssertTrue(reached)
        XCTAssertEqual(reads, paths.count)
    }

    func testWaitingResetsWhenDefaultRouteDisappears() async {
        let wifi = RecoveryNetworkPath(interface: "en0", gateway: "192.0.2.1")
        let paths: [RecoveryNetworkPath?] = [wifi, nil, wifi, wifi]
        var reads = 0
        let reached = await NetworkRecovery.waitForStablePath(until: .now.advanced(by: .seconds(2)),
                                                              stableFor: .milliseconds(5), interval: .milliseconds(15)) { _ in
            defer { reads += 1 }
            return paths[min(reads, paths.count - 1)]
        }
        XCTAssertTrue(reached)
        XCTAssertEqual(reads, paths.count)
    }

    func testRecoveryWaitsForPathOnEveryAttemptAndStopsOnSuccess() async {
        let deadline = ContinuousClock.now.advanced(by: .seconds(2))
        var actions: [String] = []
        let restored = await NetworkRecovery.retryOriginalLine(until: deadline, delays: [.zero, .zero, .zero], waitForPath: { _ in
            actions.append("wait")
            return true
        }, attempt: {
            actions.append("connect")
            return actions.count == 2 ? .retry : .connected
        })
        XCTAssertTrue(restored)
        XCTAssertEqual(actions, ["wait", "connect", "wait", "connect"])
    }

    func testRecoveryStopsAtLimitOrWhenNoNetworkArrives() async {
        let deadline = ContinuousClock.now.advanced(by: .seconds(2))
        var attempts = 0
        let restored = await NetworkRecovery.retryOriginalLine(until: deadline, delays: [.zero, .zero], waitForPath: { _ in true }, attempt: {
            attempts += 1
            return .retry
        })
        XCTAssertFalse(restored)
        XCTAssertEqual(attempts, 2)
        let offline = await NetworkRecovery.retryOriginalLine(until: deadline, waitForPath: { _ in false }, attempt: {
            XCTFail("reconnected before the network was available")
            return .connected
        })
        XCTAssertFalse(offline)
    }

    func testCancellationOrExpiredDeadlineNeverStartsAnAttempt() async {
        let deadline = ContinuousClock.now.advanced(by: .seconds(2))
        let task = Task {
            await NetworkRecovery.retryOriginalLine(until: deadline, delays: [.seconds(1)], waitForPath: { _ in
                XCTFail("cancelled recovery entered path monitoring")
                return true
            }, attempt: {
                XCTFail("cancelled recovery started a connection")
                return .connected
            })
        }
        task.cancel()
        let cancelled = await task.value
        XCTAssertFalse(cancelled)
        let timedOut = await NetworkRecovery.retryOriginalLine(until: .now, waitForPath: { _ in
            XCTFail("expired recovery entered path monitoring")
            return true
        }, attempt: { .connected })
        XCTAssertFalse(timedOut)
    }

    func testStoreOnlyRecoversConfirmedNetworkChangeAndCanStopWhileWaiting() throws {
        _ = NSApplication.shared
        let base = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: base) }
        let route = RouteClient()
        let store = AppStore(dataDirectory: base, diagnostics: FaultDiagnostics(directory: base), refreshRuntime: false, routeClient: route)
        store.configuration.profile.rememberPassword = true
        let failed = #"{ "updatedAt":0, "vpn":0, "direct":0, "unknown":0, "vpnObserved":false, "state":"failed", "failureCode":"physical_network_changed" }"#
        let snapshot = try JSONDecoder().decode(RouteSnapshot.self, from: Data(failed.utf8))

        store.phase = "connected"
        store.updateOperationBlocked = true
        route.onSnapshot?(snapshot)
        route.onState?("failed")
        XCTAssertFalse(store.networkRecoveryActive)

        store.phase = "connected"
        store.updateOperationBlocked = false
        route.onSnapshot?(snapshot)
        route.onState?("failed")
        XCTAssertTrue(store.networkRecoveryActive)
        XCTAssertEqual(store.connectionControlState, .reconnecting)
        XCTAssertTrue(store.updateSafetyState.connectionOrServiceActive)
        store.disconnect()
        XCTAssertFalse(store.networkRecoveryActive)
        XCTAssertEqual(store.phase, "idle")
    }

    func testPathHealthDegradationLeavesExistingSessionControlsActive() throws {
        let base = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: base) }
        let route = RouteClient()
        let store = AppStore(dataDirectory: base, diagnostics: FaultDiagnostics(directory: base), refreshRuntime: false, routeClient: route)
        store.phase = "connected"
        let payload = #"""
        {
          "updatedAt":0,"vpn":0,"direct":0,"unknown":0,"vpnObserved":false,"state":"ready",
          "pathHealth":{
            "state":"degraded","sampledAt":0,
            "proxyHTTPS":{"result":"connect","durationMs":2000},
            "proxyDoH":{"result":"connect","durationMs":2000},
            "directHTTPS":{"result":"ok","durationMs":48}
          }
        }
        """#
        let snapshot = try JSONDecoder().decode(RouteSnapshot.self, from: Data(payload.utf8))
        route.onSnapshot?(snapshot)
        XCTAssertEqual(store.connectionDisplayTitle, "线路降级")
        XCTAssertEqual(store.connectionControlState, .connected)
        XCTAssertTrue(store.menuBarControlPolicy.actionEnabled)
        XCTAssertTrue(store.updateSafetyState.connectionOrServiceActive)
    }
}

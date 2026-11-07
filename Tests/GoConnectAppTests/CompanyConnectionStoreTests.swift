import XCTest
import GoConnectCore
@testable import GoConnect

@MainActor
final class CompanyConnectionStoreTests: XCTestCase {
    private struct FixtureError: LocalizedError {
        var errorDescription: String? { "fixture failure" }
    }

    @MainActor
    private final class Recorder {
        var log: [String] = []
        var passwords: [String] = []
        var tokens: [String] = []
        var startGenerations: [UUID] = []
        var claims: [CompanyBackendClaim] = []
        var blockGenerations: [UUID] = []
        var startError: Error?
        var prepareError: Error?
        var blockError: Error?
        var time: TimeInterval = 100
    }

    private func fixture(ackTimeoutTicks: Int = 3) -> (CompanyConnectionStore, Recorder) {
        let recorder = Recorder()
        let actions = CompanyConnectionActions(
            start: { _, password, token, generation in
                recorder.log.append("start"); recorder.passwords.append(password)
                recorder.tokens.append(token); recorder.startGenerations.append(generation)
                if let error = recorder.startError { throw error }
            },
            prepare: { claim, _ in
                recorder.log.append("prepare"); recorder.claims.append(claim)
                if let error = recorder.prepareError { throw error }
            },
            block: { generation in
                recorder.log.append("block"); recorder.blockGenerations.append(generation)
                if let error = recorder.blockError { throw error }
            },
            stop: { recorder.log.append("stop") }
        )
        return (CompanyConnectionStore(actions: actions, ackTimeoutTicks: ackTimeoutTicks, now: { recorder.time }), recorder)
    }

    private func connect(_ store: CompanyConnectionStore, secret: String? = "secret") {
        store.connect(profile: VPNProfile(), primaryReady: true, controllerReserved: true, updateBlocked: false, secret: secret)
    }

    private func readyEvent() -> TransportEvent {
        var event = TransportEvent(event: "ready")
        event.companySocket = "/private/tmp/company.sock"; event.companyPID = 42
        event.companyStartTime = 7; event.dns = ["10.0.0.53"]
        return event
    }

    private func stats(received: UInt64, sent: UInt64) -> TransportEvent {
        var event = TransportEvent(event: "stats")
        event.received = received; event.sent = sent
        return event
    }

    private func route(_ state: String, _ generation: UUID, revision: UInt64 = 1) -> CompanyRouteState {
        CompanyRouteState(revision: revision, generation: generation.uuidString, state: state)
    }

    private func connected(_ store: CompanyConnectionStore) -> UUID {
        connect(store)
        let generation = store.generation
        store.handle(readyEvent(), generation: generation)
        store.routeState(route("active", generation))
        return generation
    }

    func testConnectRequiresPrimaryReservationAndNoUpdate() {
        let (store, recorder) = fixture()
        store.connect(profile: VPNProfile(), primaryReady: false, controllerReserved: true, updateBlocked: false, secret: "secret")
        XCTAssertNotNil(store.message)
        store.connect(profile: VPNProfile(), primaryReady: true, controllerReserved: false, updateBlocked: false, secret: "secret")
        store.connect(profile: VPNProfile(), primaryReady: true, controllerReserved: true, updateBlocked: true, secret: "secret")
        store.connect(profile: VPNProfile(), primaryReady: false, controllerReserved: false, updateBlocked: true)
        XCTAssertEqual(store.phase, "idle")
        XCTAssertFalse(store.passwordRequested)
        XCTAssertEqual(recorder.log, [])
    }

    func testEmptySecretRequestsOwnPasswordAndClearsItAfterDispatch() {
        let (store, recorder) = fixture()
        connect(store, secret: "")
        XCTAssertTrue(store.passwordRequested)
        XCTAssertTrue(store.busy)
        XCTAssertEqual(recorder.log, [])
        store.submitPassword()
        XCTAssertTrue(store.passwordRequested, "An empty password cannot start a transport")
        XCTAssertEqual(recorder.log, [])
        store.password = "typed"
        store.submitPassword()
        XCTAssertEqual(recorder.passwords, ["typed"])
        XCTAssertEqual(recorder.startGenerations, [store.generation])
        XCTAssertEqual(store.password, "")
        XCTAssertFalse(store.passwordRequested)
        XCTAssertEqual(store.phase, "authenticating")
    }

    func testCancelledPasswordIsClearedAndNeverReused() {
        let (store, recorder) = fixture()
        connect(store, secret: nil)
        store.password = "abandoned"
        store.cancelAuthentication()
        XCTAssertEqual(store.password, "")
        XCTAssertEqual(store.phase, "idle")
        XCTAssertFalse(store.isRunning)
        connect(store, secret: nil)
        XCTAssertTrue(store.passwordRequested)
        XCTAssertEqual(store.password, "")
        XCTAssertEqual(recorder.log, [])
    }

    func testRepeatedConnectIsRejectedInEveryActivePhase() {
        let (store, recorder) = fixture()
        connect(store)
        let generation = store.generation
        connect(store)
        store.handle(readyEvent(), generation: generation)
        connect(store)
        store.routeState(route("active", generation))
        connect(store)
        XCTAssertEqual(store.generation, generation)
        XCTAssertEqual(store.phase, "connected")
        XCTAssertEqual(recorder.log, ["start", "prepare"])
    }

    func testTokenIsPerAttemptAndClaimCarriesReadyIdentity() {
        let (store, recorder) = fixture()
        let generation = connected(store)
        store.disconnect()
        store.routeState(route("blocked", generation, revision: 2))
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        connect(store)
        XCTAssertEqual(recorder.tokens.count, 2)
        XCTAssertNotEqual(recorder.tokens[0], recorder.tokens[1])
        XCTAssertEqual(recorder.tokens[0].count, 72)
        XCTAssertEqual(recorder.claims, [CompanyBackendClaim(socketPath: "/private/tmp/company.sock", pid: 42, startTime: 7, token: recorder.tokens[0], dnsServers: ["10.0.0.53"])])
    }

    func testOnlyMatchingActiveRouteMarksConnected() {
        let (store, recorder) = fixture()
        connect(store)
        let generation = store.generation
        store.routeState(route("active", generation))
        XCTAssertEqual(store.phase, "authenticating", "A route cannot become active before the backend is claimed")
        store.handle(readyEvent(), generation: generation)
        XCTAssertEqual(store.phase, "preparing")
        store.routeState(route("active", UUID()))
        store.routeState(CompanyRouteState(revision: 0, generation: "", state: "blocked"))
        XCTAssertEqual(store.phase, "preparing", "The reservation's initial blocked state is not a failure")
        store.routeState(CompanyRouteState(revision: 2, generation: generation.uuidString.lowercased(), state: "active"))
        XCTAssertEqual(store.phase, "connected")
        store.handle(readyEvent(), generation: generation)
        XCTAssertEqual(recorder.claims.count, 1, "A repeated ready cannot claim twice")
        XCTAssertEqual(store.phase, "connected", "An identical ready is not a replacement backend")
    }

    func testMatchingBlockedWhilePreparingFailsClosedWithoutActivation() {
        let (store, recorder) = fixture()
        connect(store)
        let generation = store.generation
        store.handle(readyEvent(), generation: generation)
        store.routeState(route("blocked", generation))
        XCTAssertEqual(store.phase, "stopping")
        XCTAssertEqual(store.message, "公司路由已被阻断。")
        XCTAssertEqual(recorder.log, ["start", "prepare", "stop"], "The root already blocked this generation, so only the transport remains")
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(store.phase, "failed")
        XCTAssertFalse(store.isRunning)
    }

    func testStaleAndDuplicateRouteRevisionsAreIgnored() {
        let (store, recorder) = fixture()
        connect(store)
        let generation = store.generation
        store.handle(readyEvent(), generation: generation)
        store.routeState(route("active", generation, revision: 5))
        store.routeState(route("failed", generation, revision: 4))
        store.routeState(route("blocked", generation, revision: 4))
        store.routeState(route("active", generation, revision: 5))
        XCTAssertEqual(store.phase, "connected")
        XCTAssertEqual(recorder.log, ["start", "prepare"])
        store.routeState(route("failed", generation, revision: 6))
        store.routeState(route("failed", generation, revision: 6))
        XCTAssertEqual(recorder.log.filter { $0 == "block" }.count, 1)
        XCTAssertEqual(store.phase, "stopping")
    }

    func testRevisionCanFailClosedInPlaceButNeverReactivate() {
        let (store, recorder) = fixture()
        connect(store)
        let generation = store.generation
        store.handle(readyEvent(), generation: generation)
        store.routeState(route("prepared", generation, revision: 1))
        store.routeState(route("active", generation, revision: 1))
        XCTAssertEqual(store.phase, "preparing", "A root commit always takes a new revision")
        store.routeState(route("active", generation, revision: 2))
        XCTAssertEqual(store.phase, "connected")
        store.routeState(route("blocked", generation, revision: 2))
        XCTAssertEqual(store.phase, "stopping", "Closing the root gate blocks without a revision bump")
        XCTAssertEqual(recorder.log, ["start", "prepare", "stop"])
        store.routeState(route("active", generation, revision: 2))
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(store.phase, "failed", "The same revision cannot reopen a blocked route")
    }

    func testActiveAfterConfirmedBlockQuarantinesUntilBlockedAgain() {
        let (store, recorder) = fixture()
        let generation = connected(store)
        store.disconnect()
        store.routeState(route("blocked", generation, revision: 2))
        store.routeState(route("active", generation, revision: 3))
        XCTAssertEqual(store.phase, "quarantined")
        XCTAssertEqual(recorder.log, ["start", "prepare", "block", "stop", "block"])
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(store.phase, "quarantined", "The earlier acknowledgement no longer proves the route is blocked")
        XCTAssertTrue(store.busy)
        store.routeState(route("blocked", generation, revision: 4))
        XCTAssertEqual(store.phase, "failed")
        XCTAssertEqual(recorder.log.filter { $0 == "stop" }.count, 1)
    }

    func testActiveDNSIsVisibleOnlyWhileRouteIsInService() {
        let (store, _) = fixture()
        connect(store)
        let generation = store.generation
        store.handle(readyEvent(), generation: generation)
        XCTAssertEqual(store.activeDNSServers, [])
        var active = route("active", generation)
        active.dnsServers = ["10.0.0.53"]
        store.routeState(active)
        XCTAssertEqual(store.activeDNSServers, ["10.0.0.53"])
        store.handle(TransportEvent(event: "reconnecting"), generation: generation)
        XCTAssertEqual(store.activeDNSServers, ["10.0.0.53"])
        store.disconnect()
        XCTAssertEqual(store.activeDNSServers, [])
        store.routeState(route("blocked", generation, revision: 2))
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(store.activeDNSServers, [])
        _ = connected(store)
        XCTAssertEqual(store.activeDNSServers, [], "Resolvers never carry into another attempt")
    }

    func testReconnectKeepsGenerationAndBackendThroughRecovery() {
        let (store, recorder) = fixture(ackTimeoutTicks: 2)
        let generation = connected(store)
        let profile = store.activeProfileID
        store.handle(stats(received: 1_000, sent: 500), generation: generation)
        store.handle(TransportEvent(event: "reconnected"), generation: generation)
        XCTAssertEqual(store.phase, "connected", "Recovery cannot end before it starts")
        store.handle(TransportEvent(event: "reconnecting"), generation: generation)
        XCTAssertEqual(store.phase, "reconnecting")
        XCTAssertNotNil(store.message)
        XCTAssertTrue(store.isRunning)
        XCTAssertFalse(store.busy)
        store.handle(TransportEvent(event: "reconnecting"), generation: generation)
        for _ in 0..<5 { store.tickTimeout() }
        XCTAssertEqual(store.phase, "reconnecting", "Transport recovery has its own limit and is not a root acknowledgement")
        store.routeState(route("active", generation, revision: 2))
        store.handle(readyEvent(), generation: generation)
        recorder.time += 2
        store.handle(stats(received: 3_000, sent: 700), generation: generation)
        XCTAssertEqual(store.received, 3_000)
        XCTAssertEqual(store.phase, "reconnecting")
        store.handle(TransportEvent(event: "reconnected"), generation: generation)
        XCTAssertEqual(store.phase, "connected")
        XCTAssertNil(store.message)
        store.handle(TransportEvent(event: "reconnected"), generation: generation)
        XCTAssertEqual(store.phase, "connected")
        XCTAssertEqual(store.generation, generation)
        XCTAssertEqual(store.activeProfileID, profile)
        XCTAssertEqual(store.received, 3_000)
        XCTAssertEqual(recorder.log, ["start", "prepare"], "Recovery neither restarts, claims again, blocks nor stops")
    }

    func testStaleReconnectEventsCannotAffectCurrentAttempt() {
        let (store, _) = fixture()
        let generation = connected(store)
        store.handle(TransportEvent(event: "reconnecting"), generation: UUID())
        XCTAssertEqual(store.phase, "connected")
        store.handle(TransportEvent(event: "reconnecting"), generation: generation)
        store.handle(TransportEvent(event: "reconnected"), generation: UUID())
        XCTAssertEqual(store.phase, "reconnecting")
    }

    func testRecoveryBeforeActivationKeepsTimeoutAndReportsReconnectingOnActive() {
        let (store, recorder) = fixture(ackTimeoutTicks: 2)
        connect(store)
        let generation = store.generation
        store.handle(TransportEvent(event: "reconnecting"), generation: generation)
        XCTAssertEqual(store.phase, "authenticating")
        store.handle(readyEvent(), generation: generation)
        XCTAssertEqual(store.phase, "preparing")
        store.tickTimeout()
        store.routeState(route("active", generation))
        XCTAssertEqual(store.phase, "reconnecting", "An active route does not mean the tunnel has recovered")
        store.handle(TransportEvent(event: "reconnected"), generation: generation)
        XCTAssertEqual(store.phase, "connected")
        XCTAssertEqual(recorder.log, ["start", "prepare"])

        let (timed, timedRecorder) = fixture(ackTimeoutTicks: 2)
        connect(timed)
        timed.handle(readyEvent(), generation: timed.generation)
        timed.handle(TransportEvent(event: "reconnecting"), generation: timed.generation)
        timed.tickTimeout(); timed.tickTimeout()
        XCTAssertEqual(timed.phase, "quarantined", "Recovery does not extend the activation bound")
        XCTAssertEqual(timedRecorder.log, ["start", "prepare", "block", "stop"])
    }

    func testDisconnectDuringRecoveryStillBlocksFirstAndLateRecoveryCannotRevive() {
        let (store, recorder) = fixture()
        let generation = connected(store)
        store.handle(TransportEvent(event: "reconnecting"), generation: generation)
        store.disconnect()
        XCTAssertEqual(store.phase, "stopping")
        XCTAssertEqual(recorder.log, ["start", "prepare", "block"])
        store.handle(TransportEvent(event: "reconnected"), generation: generation)
        store.handle(TransportEvent(event: "reconnecting"), generation: generation)
        XCTAssertEqual(store.phase, "stopping")
        store.routeState(route("blocked", generation, revision: 2))
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(store.phase, "idle")
    }

    func testFailuresDuringRecoveryFailClosed() {
        let (blocked, blockedRecorder) = fixture()
        var generation = connected(blocked)
        blocked.handle(TransportEvent(event: "reconnecting"), generation: generation)
        blocked.routeState(route("blocked", generation, revision: 2))
        XCTAssertEqual(blocked.phase, "stopping")
        XCTAssertEqual(blockedRecorder.log, ["start", "prepare", "stop"])

        let (exited, exitedRecorder) = fixture()
        generation = connected(exited)
        exited.handle(TransportEvent(event: "reconnecting"), generation: generation)
        exited.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(exited.phase, "stopping")
        XCTAssertEqual(exitedRecorder.log, ["start", "prepare", "block"])
        exited.routeState(route("blocked", generation, revision: 2))
        XCTAssertEqual(exited.phase, "failed")
        XCTAssertEqual(exited.message, "公司连接意外断开。")

        let (expired, expiredRecorder) = fixture()
        generation = connected(expired)
        expired.handle(TransportEvent(event: "reconnecting"), generation: generation)
        expired.handle(TransportEvent(event: "error", message: "隧道持续无法恢复"), generation: generation)
        XCTAssertEqual(expired.phase, "stopping")
        XCTAssertEqual(expired.message, "隧道持续无法恢复")
        XCTAssertEqual(expiredRecorder.log, ["start", "prepare", "block"])
    }

    func testReplacementReadyFailsClosedInEveryOwnedPhase() {
        let mutations: [(inout TransportEvent) -> Void] = [
            { $0.companyPID = 43 },
            { $0.companyStartTime = 8 },
            { $0.companySocket = "/private/tmp/other.sock" },
            { $0.dns = ["10.0.0.54"] },
            { $0.remoteNetworks = ["10.9.0.0/16"] },
            { $0.remoteExcluded = ["10.9.1.0/24"] },
            { $0.companyPID = nil }
        ]
        for mutate in mutations {
            for reached in ["preparing", "connected", "reconnecting"] {
                let (store, recorder) = fixture()
                connect(store)
                let generation = store.generation
                store.handle(readyEvent(), generation: generation)
                if reached != "preparing" { store.routeState(route("active", generation)) }
                if reached == "reconnecting" { store.handle(TransportEvent(event: "reconnecting"), generation: generation) }
                XCTAssertEqual(store.phase, reached)
                var replacement = readyEvent()
                mutate(&replacement)
                store.handle(replacement, generation: generation)
                XCTAssertEqual(store.phase, "stopping", reached)
                XCTAssertEqual(store.message, "公司后端身份发生变化，已停止连接。", reached)
                XCTAssertEqual(store.generation, generation, reached)
                XCTAssertEqual(recorder.log, ["start", "prepare", "block"], "The new backend is never claimed and the route is blocked first")
                store.routeState(route("blocked", generation, revision: 2))
                XCTAssertEqual(recorder.log, ["start", "prepare", "block", "stop"], reached)
                store.handle(TransportEvent(event: "stopped"), generation: generation)
                XCTAssertEqual(store.phase, "failed", reached)
                XCTAssertEqual(recorder.claims.count, 1, reached)
            }
        }
    }

    func testPrimaryStoppingBlocksThenStopsWithinSameGeneration() {
        let (store, recorder) = fixture()
        let generation = connected(store)
        store.primaryStopping()
        XCTAssertEqual(store.phase, "stopping")
        XCTAssertEqual(store.generation, generation, "The block acknowledgement must still match")
        XCTAssertEqual(store.message, "主线路正在停止，正在结束公司连接…")
        XCTAssertEqual(recorder.log, ["start", "prepare", "block"], "The transport survives until root confirms the block")
        XCTAssertEqual(recorder.blockGenerations, [generation])
        store.primaryStopping()
        store.disconnect()
        XCTAssertEqual(recorder.log, ["start", "prepare", "block"])
        store.routeState(route("blocked", generation, revision: 2))
        XCTAssertEqual(recorder.log, ["start", "prepare", "block", "stop"])
        XCTAssertTrue(store.isRunning)
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(store.phase, "idle")
        XCTAssertEqual(store.message, "主线路正在停止，公司连接已结束。")
        XCTAssertFalse(store.isRunning)
    }

    func testPrimaryStoppingCancelsAuthenticationAndIgnoresIdle() {
        let (idle, idleRecorder) = fixture()
        idle.primaryStopping()
        XCTAssertEqual(idle.phase, "idle")
        XCTAssertNil(idle.message)
        XCTAssertEqual(idleRecorder.log, [])

        let (prompted, promptedRecorder) = fixture()
        connect(prompted, secret: nil)
        prompted.password = "typed"
        prompted.primaryStopping()
        XCTAssertFalse(prompted.passwordRequested)
        XCTAssertEqual(prompted.password, "")
        XCTAssertEqual(prompted.phase, "idle")
        XCTAssertFalse(prompted.isRunning)
        prompted.submitPassword()
        XCTAssertEqual(promptedRecorder.log, [])

        let (authenticating, authRecorder) = fixture()
        connect(authenticating)
        let generation = authenticating.generation
        authenticating.primaryStopping()
        XCTAssertEqual(authRecorder.log, ["start", "stop"], "No route exists yet, so there is nothing to block")
        authenticating.handle(readyEvent(), generation: generation)
        XCTAssertEqual(authRecorder.claims, [])
        authenticating.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(authenticating.phase, "idle")
    }

    func testPrimaryStoppingDuringPreparingAndRecoveryBlocksFirst() {
        let (preparing, preparingRecorder) = fixture()
        connect(preparing)
        preparing.handle(readyEvent(), generation: preparing.generation)
        preparing.primaryStopping()
        XCTAssertEqual(preparing.phase, "stopping")
        XCTAssertEqual(preparingRecorder.log, ["start", "prepare", "block"])

        let (recovering, recoveringRecorder) = fixture()
        let generation = connected(recovering)
        recovering.handle(TransportEvent(event: "reconnecting"), generation: generation)
        recovering.primaryStopping()
        XCTAssertEqual(recovering.phase, "stopping")
        XCTAssertEqual(recoveringRecorder.log, ["start", "prepare", "block"])
    }

    func testConfirmedTeardownReleasesPrimaryStoppingBlockWait() {
        let (store, recorder) = fixture()
        let generation = connected(store)
        store.primaryStopping()
        store.primaryStopped()
        XCTAssertEqual(recorder.log, ["start", "prepare", "block", "stop"], "The controller is gone, so stop no longer waits for the block")
        XCTAssertNotEqual(store.generation, generation)
        XCTAssertEqual(store.phase, "stopping")
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(store.phase, "idle")
        XCTAssertEqual(store.message, "主线路已停止，公司连接已结束。")
    }

    func testPrimaryStoppingKeepsQuarantineWithoutCleanupAcknowledgement() {
        let (store, recorder) = fixture(ackTimeoutTicks: 2)
        let generation = connected(store)
        store.primaryStopping()
        store.tickTimeout()
        XCTAssertEqual(store.phase, "stopping")
        XCTAssertEqual(recorder.log, ["start", "prepare", "block"])
        store.tickTimeout()
        XCTAssertEqual(store.phase, "quarantined")
        XCTAssertEqual(recorder.log, ["start", "prepare", "block", "stop"])
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(store.phase, "quarantined", "A stopped transport alone does not prove the route is blocked")
        store.primaryStopping()
        XCTAssertEqual(store.phase, "quarantined", "A stopping primary is not a confirmed controller exit")
        XCTAssertTrue(store.busy)
        store.primaryStopped()
        XCTAssertEqual(store.phase, "failed")
        XCTAssertFalse(store.busy)
    }

    func testPrimaryStoppingBlockFailureQuarantinesAndStops() {
        let (store, recorder) = fixture()
        recorder.blockError = FixtureError()
        let generation = connected(store)
        store.primaryStopping()
        XCTAssertEqual(store.phase, "quarantined")
        XCTAssertEqual(recorder.log, ["start", "prepare", "block", "stop"])
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(store.phase, "quarantined")
        XCTAssertTrue(store.isRunning)
    }

    func testDisconnectBlocksThenStopsThenWaitsForStopped() {
        let (store, recorder) = fixture()
        let generation = connected(store)
        store.routeState(route("blocked", UUID(), revision: 2))
        XCTAssertEqual(store.phase, "connected")
        store.disconnect()
        XCTAssertEqual(store.phase, "stopping")
        XCTAssertTrue(store.busy)
        XCTAssertEqual(recorder.log, ["start", "prepare", "block"], "Transport must survive until root confirms the block")
        XCTAssertEqual(recorder.blockGenerations, [generation])
        store.disconnect()
        store.routeState(route("blocked", UUID(), revision: 2))
        store.handle(TransportEvent(event: "stopped"), generation: UUID())
        XCTAssertEqual(recorder.log, ["start", "prepare", "block"])
        store.routeState(route("blocked", generation, revision: 2))
        XCTAssertEqual(recorder.log, ["start", "prepare", "block", "stop"])
        XCTAssertEqual(store.phase, "stopping")
        XCTAssertTrue(store.isRunning)
        store.routeState(route("blocked", generation, revision: 3))
        XCTAssertEqual(recorder.log.filter { $0 == "stop" }.count, 1)
        store.handle(TransportEvent(event: "stopped"), generation: UUID())
        XCTAssertEqual(store.phase, "stopping")
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(store.phase, "idle")
        XCTAssertFalse(store.isRunning)
        XCTAssertNil(store.activeProfileID)
    }

    func testOutOfOrderActiveCannotReviveOrSatisfyTeardown() {
        let (store, recorder) = fixture()
        let generation = connected(store)
        store.disconnect()
        store.routeState(route("active", generation, revision: 0))
        store.routeState(route("blocked", generation, revision: 3))
        store.routeState(route("active", generation, revision: 2))
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(store.phase, "idle", "An older active revision is ignored")
        XCTAssertEqual(recorder.log.filter { $0 == "stop" }.count, 1)
    }

    func testDisconnectBeforeRouteClaimStopsAndLateReadyCannotPrepare() {
        let (store, recorder) = fixture()
        connect(store)
        let generation = store.generation
        store.disconnect()
        XCTAssertEqual(recorder.log, ["start", "stop"], "No route exists yet, so there is nothing to block")
        store.handle(readyEvent(), generation: generation)
        XCTAssertEqual(recorder.claims, [])
        XCTAssertEqual(store.phase, "stopping")
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(store.phase, "idle")
    }

    func testStaleEventsCannotAffectNewAttempt() {
        let (store, recorder) = fixture()
        let old = connected(store)
        store.handle(stats(received: 10, sent: 10), generation: old)
        store.disconnect()
        store.routeState(route("blocked", old, revision: 2))
        store.handle(TransportEvent(event: "stopped"), generation: old)
        connect(store)
        let current = store.generation
        XCTAssertNotEqual(current, old)
        store.handle(readyEvent(), generation: old)
        store.handle(TransportEvent(event: "error", message: "old failure"), generation: old)
        store.handle(TransportEvent(event: "stopped"), generation: old)
        store.handle(stats(received: 99, sent: 99), generation: old)
        store.routeState(route("active", old, revision: 9))
        XCTAssertEqual(store.phase, "authenticating")
        XCTAssertNil(store.message)
        XCTAssertEqual(store.received, 0)
        XCTAssertEqual(recorder.claims.count, 1)
        XCTAssertEqual(recorder.log.filter { $0 == "stop" }.count, 1)
        store.handle(readyEvent(), generation: current)
        store.routeState(route("active", current, revision: 1))
        XCTAssertEqual(store.phase, "connected", "Revisions restart with each attempt")
    }

    func testPrimaryStopDuringPasswordDialogClosesWithoutTransportStop() {
        let (store, recorder) = fixture()
        connect(store, secret: nil)
        store.password = "typed"
        store.primaryStopped()
        XCTAssertFalse(store.passwordRequested)
        XCTAssertFalse(store.busy)
        XCTAssertFalse(store.isRunning)
        XCTAssertEqual(store.password, "")
        XCTAssertEqual(store.phase, "idle")
        store.submitPassword()
        XCTAssertEqual(recorder.log, [])
    }

    func testPrimaryStopCascadesImmediatelyAndOwnerStoppedFinishesCleanup() {
        let (store, recorder) = fixture()
        let generation = connected(store)
        store.primaryStopped()
        XCTAssertEqual(recorder.log, ["start", "prepare", "stop"], "The controller is gone, so stop does not wait for a block")
        XCTAssertNotEqual(store.generation, generation)
        XCTAssertEqual(store.phase, "stopping")
        store.primaryStopped()
        store.disconnect()
        store.routeState(route("active", generation, revision: 2))
        store.handle(stats(received: 50, sent: 50), generation: generation)
        store.handle(TransportEvent(event: "error", message: "late"), generation: generation)
        XCTAssertEqual(store.phase, "stopping")
        XCTAssertEqual(store.received, 0)
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(store.phase, "idle")
        XCTAssertFalse(store.isRunning)
        XCTAssertEqual(recorder.log.filter { $0 == "stop" }.count, 1)
    }

    func testAuthenticationErrorStopsOnceAndRequiresFreshPassword() {
        let (store, recorder) = fixture()
        connect(store, secret: nil)
        store.password = "wrong"
        store.submitPassword()
        let generation = store.generation
        store.handle(TransportEvent(event: "error", message: "认证失败"), generation: generation)
        store.handle(TransportEvent(event: "error", message: "second"), generation: generation)
        XCTAssertEqual(recorder.log, ["start", "stop"])
        XCTAssertEqual(store.message, "认证失败")
        XCTAssertTrue(store.busy)
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(store.phase, "failed")
        XCTAssertEqual(store.message, "认证失败")
        XCTAssertFalse(store.busy)
        connect(store, secret: nil)
        XCTAssertTrue(store.passwordRequested)
        XCTAssertEqual(store.password, "")
        XCTAssertEqual(recorder.passwords, ["wrong"])
    }

    func testStartFailureLeavesNoTransportOwnership() {
        let (store, recorder) = fixture()
        recorder.startError = FixtureError()
        connect(store)
        XCTAssertEqual(store.phase, "failed")
        XCTAssertFalse(store.isRunning)
        XCTAssertEqual(recorder.log, ["start"])
        XCTAssertEqual(store.message, "fixture failure")
    }

    func testIncompleteReadyStopsWithoutClaim() {
        let (store, recorder) = fixture()
        connect(store)
        store.handle(TransportEvent(event: "ready"), generation: store.generation)
        XCTAssertEqual(recorder.log, ["start", "stop"])
        XCTAssertEqual(store.phase, "stopping")
    }

    func testPrepareFailureQuarantinesUntilStoppedAndBlocked() {
        let (store, recorder) = fixture()
        recorder.prepareError = FixtureError()
        connect(store)
        let generation = store.generation
        store.handle(readyEvent(), generation: generation)
        XCTAssertEqual(store.phase, "quarantined")
        XCTAssertEqual(recorder.log, ["start", "prepare", "block", "stop"])
        XCTAssertTrue(store.busy)
        connect(store)
        store.disconnect()
        XCTAssertEqual(recorder.log.filter { $0 == "start" }.count, 1, "Quarantine prevents restart")
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(store.phase, "quarantined", "A stopped transport alone does not prove the route is blocked")
        store.routeState(route("blocked", generation))
        XCTAssertEqual(store.phase, "failed")
        XCTAssertFalse(store.busy)
        recorder.prepareError = nil
        connect(store)
        XCTAssertEqual(store.phase, "authenticating")
        XCTAssertEqual(recorder.log.filter { $0 == "start" }.count, 2)
    }

    func testBlockFailureStopsAndPrimaryStopProvesControllerGone() {
        let (store, recorder) = fixture()
        recorder.blockError = FixtureError()
        let generation = connected(store)
        store.disconnect()
        XCTAssertEqual(store.phase, "quarantined")
        XCTAssertEqual(recorder.log, ["start", "prepare", "block", "stop"])
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(store.phase, "quarantined")
        store.primaryStopped()
        XCTAssertEqual(store.phase, "failed")
        XCTAssertFalse(store.busy)
        XCTAssertFalse(store.isRunning)
        XCTAssertEqual(recorder.log.filter { $0 == "stop" }.count, 1)
    }

    func testPrimaryStopBeforeOwnerStoppedKeepsQuarantineUntilTransportExits() {
        let (store, recorder) = fixture()
        recorder.blockError = FixtureError()
        let generation = connected(store)
        store.disconnect()
        store.primaryStopped()
        XCTAssertEqual(store.phase, "quarantined")
        XCTAssertTrue(store.isRunning)
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(store.phase, "failed")
        XCTAssertFalse(store.isRunning)
        XCTAssertEqual(recorder.log.filter { $0 == "stop" }.count, 1)
    }

    func testMissingBlockAcknowledgementStopsOnlyAfterTimeoutAndStaysQuarantined() {
        let (store, recorder) = fixture(ackTimeoutTicks: 3)
        let generation = connected(store)
        store.disconnect()
        store.tickTimeout(); store.tickTimeout()
        XCTAssertEqual(recorder.log, ["start", "prepare", "block"])
        XCTAssertEqual(store.phase, "stopping")
        store.tickTimeout()
        XCTAssertEqual(recorder.log, ["start", "prepare", "block", "stop"])
        XCTAssertEqual(store.phase, "quarantined")
        store.tickTimeout()
        XCTAssertEqual(recorder.log.filter { $0 == "stop" }.count, 1)
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(store.phase, "quarantined")
        XCTAssertTrue(store.busy)
        store.routeState(route("blocked", generation, revision: 2))
        XCTAssertEqual(store.phase, "failed")
    }

    func testMissingActivationTimesOutIntoQuarantine() {
        let (store, recorder) = fixture(ackTimeoutTicks: 2)
        connect(store)
        store.tickTimeout(); store.tickTimeout()
        XCTAssertEqual(store.phase, "authenticating", "Authentication has no root acknowledgement to bound")
        store.handle(readyEvent(), generation: store.generation)
        store.tickTimeout()
        XCTAssertEqual(store.phase, "preparing")
        store.tickTimeout()
        XCTAssertEqual(store.phase, "quarantined")
        XCTAssertEqual(recorder.log, ["start", "prepare", "block", "stop"])
    }

    func testUnexpectedTransportExitStillWaitsForBlockedRoute() {
        let (store, recorder) = fixture()
        let generation = connected(store)
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(store.phase, "stopping")
        XCTAssertTrue(store.busy)
        XCTAssertEqual(recorder.log, ["start", "prepare", "block"], "A transport that already exited is not stopped again")
        store.routeState(route("blocked", generation, revision: 2))
        XCTAssertEqual(store.phase, "failed")
        XCTAssertEqual(store.message, "公司连接意外断开。")
    }

    func testRootInitiatedBlockAfterConnectionStopsTransport() {
        let (store, recorder) = fixture()
        let generation = connected(store)
        store.routeState(route("blocked", generation, revision: 2))
        XCTAssertEqual(store.phase, "stopping")
        XCTAssertEqual(recorder.log, ["start", "prepare", "stop"])
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(store.phase, "failed")
    }

    func testRatesAreIndependentPerStoreAndEndWithSession() {
        let (store, recorder) = fixture()
        let (other, _) = fixture()
        let generation = connected(store)
        let otherGeneration = connected(other)
        store.handle(stats(received: 1_000, sent: 500), generation: generation)
        XCTAssertEqual(store.downloadRate, 0)
        recorder.time += 2
        store.handle(stats(received: 3_000, sent: 900), generation: generation)
        XCTAssertEqual(store.received, 3_000)
        XCTAssertEqual(store.sent, 900)
        XCTAssertEqual(store.downloadRate, 1_000)
        XCTAssertEqual(store.uploadRate, 200)
        store.handle(stats(received: 3_000, sent: 900), generation: otherGeneration)
        XCTAssertEqual(store.received, 3_000)
        XCTAssertEqual(other.received, 0)
        XCTAssertEqual(other.downloadRate, 0)
        store.handle(TransportEvent(event: "stats_unavailable"), generation: generation)
        XCTAssertEqual(store.downloadRate, 0)
        XCTAssertEqual(store.received, 3_000, "Totals survive an unavailable sample")
        store.disconnect()
        recorder.time += 1
        store.handle(stats(received: 9_000, sent: 9_000), generation: generation)
        XCTAssertEqual(store.received, 3_000, "Late statistics cannot enter a closing session")
        store.routeState(route("blocked", generation, revision: 2))
        store.handle(TransportEvent(event: "stopped"), generation: generation)
        XCTAssertEqual(store.received, 0)
        XCTAssertEqual(store.uploadRate, 0)
    }
}

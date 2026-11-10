import XCTest
import GoConnectCore
@testable import GoConnect

@MainActor
final class UpdateOperationGuardTests: XCTestCase {
    private func fixture(service: NetworkServiceClient? = nil) throws -> AppStore {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent("GoConnect-OperationGuard-" + UUID().uuidString)
        let isolatedService = service ?? NetworkServiceClient(statusReader: { _ in NetworkServiceStatus(state: "ready") })
        let store = AppStore(dataDirectory: folder, diagnostics: FaultDiagnostics(directory: folder.appendingPathComponent("logs")), refreshRuntime: false,
                             networkServiceClient: isolatedService)
        addTeardownBlock { try? FileManager.default.removeItem(at: folder) }
        return store
    }
    func testPendingUpdatePreventsNetworkAndServiceActionsBeforeAnyPreparation() throws {
        let store = try fixture()
        store.updateOperationBlocked = true
        store.connect()
        XCTAssertEqual(store.phase, "idle")
        XCTAssertFalse(store.passwordRequested)
        store.manageNetworkService()
        XCTAssertEqual(store.phase, "idle")
        XCTAssertFalse(store.networkService.working)
        XCTAssertFalse(store.canTestNodes)
    }
    func testQuickConnectCannotSwitchSelectedProfileDuringUpdate() throws {
        let store = try fixture()
        let original = store.configuration.activeProfileID
        let other = SavedConnection()
        store.configuration.connections.append(other)
        store.updateOperationBlocked = true
        store.quickConnect(other.id)
        XCTAssertEqual(store.configuration.activeProfileID, original)
        XCTAssertEqual(store.phase, "idle")
    }
    func testSafetyIncludesDraftAndBackgroundSubscriptionActivity() throws {
        let store = try fixture()
        XCTAssertNil(store.updateSafetyState.blockingReason)
        store.subscriptionWorking = true
        XCTAssertNotNil(store.updateSafetyState.blockingReason)
        store.subscriptionWorking = false
        store.beginNewProfile()
        XCTAssertNotNil(store.updateSafetyState.blockingReason)
    }
    func testExplicitInstallationDrainsExistingConnectionThroughNormalStopPath() async throws {
        let store = try fixture()
        store.phase = "connected"
        let prepared = await store.prepareForUpdateInstallation()
        XCTAssertTrue(prepared)
        XCTAssertEqual(store.phase, "idle")
        XCTAssertNil(store.updateSafetyState.blockingReason)
    }
    func testPreparationWithDraftKeepsConnectionAndDraftIntact() async throws {
        let script = ServiceStatusScript()
        let store = try fixture(service: NetworkServiceClient(statusReader: { _ in try await script.read() }))
        store.beginNewProfile()
        store.phase = "connected"
        let prepared = await store.prepareForUpdateInstallation()
        XCTAssertFalse(prepared)
        XCTAssertEqual(store.phase, "connected")
        XCTAssertNotNil(store.profileDraft)
        let reads = await script.reads
        XCTAssertEqual(reads, 0, "A draft must block before teardown or a service probe")
    }

    func testPreparationReplacesBusyDiagnosticCacheWithFreshIdleAfterTeardown() async throws {
        let script = ServiceStatusScript([.value(NetworkServiceStatus(state: "ready", busy: true)),
                                          .value(NetworkServiceStatus(state: "ready"))])
        let service = NetworkServiceClient(statusReader: { _ in try await script.read() })
        _ = await service.refreshStatus()
        XCTAssertTrue(service.status.busy)
        let store = try fixture(service: service)
        store.phase = "connected"
        let prepared = await store.prepareForUpdateInstallation()
        XCTAssertTrue(prepared)
        XCTAssertEqual(store.phase, "idle")
        XCTAssertFalse(service.status.busy)
        XCTAssertNil(store.updateSafetyState.blockingReason)
        let reads = await script.reads
        XCTAssertEqual(reads, 2)
    }

    func testPreparationWaitsForRealBusyWorkerWithoutTenHzHelperSpawning() async throws {
        let script = ServiceStatusScript(fallback: NetworkServiceStatus(state: "ready", busy: true))
        let store = try fixture(service: NetworkServiceClient(statusReader: { _ in try await script.read() }))
        store.phase = "connected"
        let prepared = await store.prepareForUpdateInstallation(timeout: .milliseconds(80), servicePollInterval: .milliseconds(30))
        XCTAssertFalse(prepared)
        XCTAssertEqual(store.phase, "idle")
        XCTAssertNotNil(store.updateSafetyState.blockingReason)
        let reads = await script.reads
        XCTAssertGreaterThanOrEqual(reads, 1)
        XCTAssertLessThanOrEqual(reads, 3, "Only one serialized read per polling interval")
    }

    func testPreparationCanWaitForWorkerToFinishAfterLocalClientStops() async throws {
        let script = ServiceStatusScript([.value(NetworkServiceStatus(state: "ready", busy: true)),
                                          .value(NetworkServiceStatus(state: "ready"))])
        let store = try fixture(service: NetworkServiceClient(statusReader: { _ in try await script.read() }))
        store.phase = "connected"
        let prepared = await store.prepareForUpdateInstallation(timeout: .seconds(1), servicePollInterval: .milliseconds(10))
        XCTAssertTrue(prepared)
        XCTAssertEqual(store.phase, "idle")
        let reads = await script.reads
        XCTAssertEqual(reads, 2)
    }

    func testPreparationRejectsUnreadableAndUntrustedFalseBusyReplies() async throws {
        let steps: [ServiceStatusScript.Step] = [.failure, .value(NetworkServiceStatus(state: "unavailable")),
            .value(NetworkServiceStatus(state: "needsUpdate")), .value(NetworkServiceStatus(state: "requiresAuthorization")),
            .value(NetworkServiceStatus(state: "checking"))]
        for step in steps {
            let script = ServiceStatusScript([step])
            let store = try fixture(service: NetworkServiceClient(statusReader: { _ in try await script.read() }))
            store.phase = "connected"
            let prepared = await store.prepareForUpdateInstallation(timeout: .seconds(1))
            XCTAssertFalse(prepared)
            XCTAssertEqual(store.phase, "idle")
            XCTAssertNotNil(store.updateSafetyState.blockingReason, "A false busy bit without trusted idle proof cannot authorize installation or quit")
        }
    }

    func testPreparationSupportsFreshVerifiedMissingInstallation() async throws {
        let service = NetworkServiceClient(statusReader: { _ in NetworkServiceStatus(state: "notInstalled") })
        let store = try fixture(service: service)
        let prepared = await store.prepareForUpdateInstallation()
        XCTAssertTrue(prepared, "Users who never installed a helper can still update the app")
    }

    func testCancelledPreparationDoesNotAcceptALateIdleReply() async throws {
        let script = ServiceStatusScript([.suspended])
        let store = try fixture(service: NetworkServiceClient(statusReader: { _ in try await script.read() }))
        store.phase = "connected"
        let preparation = Task { await store.prepareForUpdateInstallation(timeout: .seconds(2)) }
        try await waitForServiceReads(1, script: script)
        preparation.cancel()
        let prepared = await preparation.value
        XCTAssertFalse(prepared)
        XCTAssertNotNil(store.updateSafetyState.blockingReason)
        await script.resumeNext(NetworkServiceStatus(state: "ready"))
        await Task.yield()
        XCTAssertNotNil(store.updateSafetyState.blockingReason)
    }

    func testAlreadyCancelledPreparationNeverDisconnectsOrProbes() async throws {
        let script = ServiceStatusScript()
        let store = try fixture(service: NetworkServiceClient(statusReader: { _ in try await script.read() }))
        store.phase = "connected"
        let preparation = Task { await store.prepareForUpdateInstallation() }
        preparation.cancel()
        let prepared = await preparation.value
        XCTAssertFalse(prepared)
        XCTAssertEqual(store.phase, "connected")
        let reads = await script.reads
        XCTAssertEqual(reads, 0)
    }

    func testPreparationTimeoutIsBoundedAndDoesNotAcceptLateIdle() async throws {
        let script = ServiceStatusScript([.suspended])
        let store = try fixture(service: NetworkServiceClient(statusReader: { _ in try await script.read() }))
        store.phase = "connected"
        let start = ContinuousClock.now
        let prepared = await store.prepareForUpdateInstallation(timeout: .milliseconds(40))
        XCTAssertFalse(prepared)
        XCTAssertLessThan(start.duration(to: .now), .seconds(1))
        XCTAssertNotNil(store.updateSafetyState.blockingReason)
        await script.resumeNext(NetworkServiceStatus(state: "ready"))
        await Task.yield()
        XCTAssertNotNil(store.updateSafetyState.blockingReason)
        let reads = await script.reads
        XCTAssertEqual(reads, 1)
    }

    func testMaintenanceStartingDuringPreparationSupersedesIdleRead() async throws {
        let script = ServiceStatusScript([.suspended, .suspended])
        let service = NetworkServiceClient(statusReader: { _ in try await script.read() })
        let store = try fixture(service: service)
        store.phase = "connected"
        let preparation = Task { await store.prepareForUpdateInstallation(timeout: .seconds(2)) }
        try await waitForServiceReads(1, script: script)
        let maintenance = Task { try await service.ensureReady() }
        try await waitForServiceReads(2, script: script)
        XCTAssertTrue(service.working)
        await script.resumeNext(NetworkServiceStatus(state: "ready"))
        let prepared = await preparation.value
        XCTAssertFalse(prepared)
        XCTAssertNotNil(store.updateSafetyState.blockingReason)
        await script.resumeNext(NetworkServiceStatus(state: "ready", busy: true))
        try await maintenance.value
        XCTAssertTrue(service.status.busy, "The old idle read cannot overwrite the maintenance result")
    }

    func testSubscriptionAndCompanyDraftGuardsKeepConnectionIntact() async throws {
        for companyDraft in [false, true] {
            let script = ServiceStatusScript()
            let store = try fixture(service: NetworkServiceClient(statusReader: { _ in try await script.read() }))
            store.phase = "connected"
            store.companyDraftOpen = companyDraft
            store.subscriptionWorking = !companyDraft
            let prepared = await store.prepareForUpdateInstallation()
            XCTAssertFalse(prepared)
            XCTAssertEqual(store.phase, "connected")
            let reads = await script.reads
            XCTAssertEqual(reads, 0)
        }
    }
}

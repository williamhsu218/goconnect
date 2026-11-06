import XCTest
import GoConnectCore
@testable import GoConnect

@MainActor
final class UpdateOperationGuardTests: XCTestCase {
    private func fixture() throws -> AppStore {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent("GoConnect-OperationGuard-" + UUID().uuidString)
        let store = AppStore(dataDirectory: folder, diagnostics: FaultDiagnostics(directory: folder.appendingPathComponent("logs")), refreshRuntime: false)
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
}

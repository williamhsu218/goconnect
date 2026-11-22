import XCTest
import GoConnectCore
@testable import GoConnect

@MainActor
private final class FakeLoginItemManager: LoginItemManaging {
    var state: LoginItemState
    var registerError: Error?
    var unregisterError: Error?
    var openedSettingsCount = 0
    var registerCalls = 0
    var unregisterCalls = 0

    init(state: LoginItemState = .disabled) {
        self.state = state
    }

    func currentState() -> LoginItemState { state }

    func register() throws {
        registerCalls += 1
        if let registerError { throw registerError }
        state = .enabled
    }

    func unregister() throws {
        unregisterCalls += 1
        if let unregisterError { throw unregisterError }
        state = .disabled
    }

    func openSystemSettings() {
        openedSettingsCount += 1
    }
}

@MainActor
final class PreferencesSettingsTests: XCTestCase {
    private func makeStore() -> AppStore {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        addTeardownBlock { try? FileManager.default.removeItem(at: directory) }
        return AppStore(dataDirectory: directory, refreshRuntime: false)
    }

    func testLoginItemStoreTracksRegisterUnregisterApprovalAndErrors() {
        let manager = FakeLoginItemManager(state: .disabled)
        let loginStore = LoginItemStore(manager: manager)

        XCTAssertFalse(loginStore.isEnabled)
        XCTAssertTrue(loginStore.canToggle)
        XCTAssertNil(loginStore.unavailableReason)

        loginStore.setEnabled(true)
        XCTAssertEqual(manager.registerCalls, 1)
        XCTAssertEqual(loginStore.state, .enabled)
        XCTAssertTrue(loginStore.isEnabled)
        XCTAssertNil(loginStore.errorMessage)

        manager.state = .requiresApproval
        loginStore.refresh()
        XCTAssertTrue(loginStore.requiresApproval)
        XCTAssertTrue(loginStore.isEnabled)

        loginStore.openSystemSettings()
        XCTAssertEqual(manager.openedSettingsCount, 1)

        manager.unregisterError = NSError(domain: "test", code: 2, userInfo: [NSLocalizedDescriptionKey: "取消注册失败"])
        loginStore.setEnabled(false)
        XCTAssertEqual(manager.unregisterCalls, 1)
        XCTAssertEqual(loginStore.errorMessage, "取消注册失败")
        XCTAssertEqual(loginStore.state, .requiresApproval)

        manager.unregisterError = nil
        loginStore.setEnabled(false)
        XCTAssertNil(loginStore.errorMessage)
        XCTAssertEqual(loginStore.state, .disabled)
        XCTAssertFalse(loginStore.isEnabled)
    }

    func testUnavailableLoginItemDoesNotAttemptRegistration() {
        let manager = FakeLoginItemManager(state: .unavailable("测试构建不可用"))
        let loginStore = LoginItemStore(manager: manager)

        XCTAssertFalse(loginStore.canToggle)
        XCTAssertEqual(loginStore.unavailableReason, "测试构建不可用")
        loginStore.setEnabled(true)
        XCTAssertEqual(manager.registerCalls, 0)
    }

    func testCompanyConnectionSettingsReadOnlyReasonCoversEveryGuard() {
        let store = makeStore()
        XCTAssertNil(store.companyConnectionSettingsReadOnlyReason)
        XCTAssertTrue(store.canEditCompanyConnectionSettings)

        store.configurationReadable = false
        XCTAssertEqual(store.companyConnectionSettingsReadOnlyReason, "配置无法读取，请在诊断页查看配置文件。")
        XCTAssertFalse(store.canEditCompanyConnectionSettings)
        store.configurationReadable = true

        store.updateOperationBlocked = true
        XCTAssertEqual(store.companyConnectionSettingsReadOnlyReason, "正在准备安装更新，完成后可调整。")
        XCTAssertFalse(store.canEditCompanyConnectionSettings)
        store.updateOperationBlocked = false

        store.phase = "connected"
        XCTAssertEqual(store.companyConnectionSettingsReadOnlyReason, "请先断开主、副连接，再调整此功能。")
        XCTAssertFalse(store.canEditCompanyConnectionSettings)
        store.phase = "idle"

        store.beginEditingProfile(store.configuration.activeProfileID)
        XCTAssertEqual(store.companyConnectionSettingsReadOnlyReason, "请先完成或关闭线路配置编辑，再调整此功能。")
        XCTAssertFalse(store.canEditCompanyConnectionSettings)
        store.closeProfileEditor()

        store.companyDraftOpen = true
        XCTAssertEqual(store.companyConnectionSettingsReadOnlyReason, "公司副连接设置窗口已打开。")
        XCTAssertFalse(store.canEditCompanyConnectionSettings)
        store.companyDraftOpen = false

        XCTAssertNil(store.companyConnectionSettingsReadOnlyReason)
        XCTAssertTrue(store.canEditCompanyConnectionSettings)
    }
}

import XCTest
import Sparkle
import GoConnectCore
@testable import GoConnect

@MainActor
final class AppUpdaterTests: XCTestCase {
    private func fixture(build: String = "58", defaults: UserDefaults? = nil, validConfiguration: Bool = true,
                         state: @escaping @MainActor () -> UpdateSafetyState = { UpdateSafetyState() },
                         prepare: (@MainActor () async -> Bool)? = nil,
                         notify: @escaping @MainActor (String) -> Void = { _ in }) throws -> (AppUpdater, SPUUpdater, UserDefaults) {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent("goconnect-updater-test-\(UUID()).app")
        try FileManager.default.createDirectory(at: folder.appendingPathComponent("Contents"), withIntermediateDirectories: true)
        var info: [String: Any] = [
            "CFBundleIdentifier": "com.willhsu.GoConnect.UpdateQA", "CFBundleName": "Update Test",
            "CFBundlePackageType": "APPL", "CFBundleVersion": build,
            "GoConnectSigningMode": "team", "GoConnectUpdatesEnabled": true,
            "GoConnectUpdateQAMode": true,
            "SUAllowsAutomaticUpdates": false, "SUAutomaticallyUpdate": false, "SUEnableAutomaticChecks": false,
            "SURequireSignedFeed": true, "SUVerifyUpdateBeforeExtraction": true,
            "SUFeedURL": "http://127.0.0.1:54321/appcast.xml",
            "SUPublicEDKey": Data(repeating: 1, count: 32).base64EncodedString()
        ]
        if !validConfiguration { info["GoConnectUpdatesEnabled"] = false }
        try PropertyListSerialization.data(fromPropertyList: info, format: .xml, options: 0).write(to: folder.appendingPathComponent("Contents/Info.plist"))
        let bundle = try XCTUnwrap(Bundle(path: folder.path))
        let suite = "GoConnect.UpdaterTests.\(UUID())"
        let settings = defaults ?? UserDefaults(suiteName: suite)!
        addTeardownBlock {
            try? FileManager.default.removeItem(at: folder)
            if defaults == nil { settings.removePersistentDomain(forName: suite) }
        }
        let appUpdater = AppUpdater(bundle: bundle, defaults: settings, safetyState: state, prepareForInstallation: prepare, notify: notify)
        let driver = SPUStandardUserDriver(hostBundle: bundle, delegate: nil)
        let sparkle = SPUUpdater(hostBundle: bundle, applicationBundle: bundle, userDriver: driver, delegate: appUpdater)
        return (appUpdater, sparkle, settings)
    }

    private func item() throws -> SUAppcastItem {
        try XCTUnwrap(SUAppcastItem(dictionary: ["title": "Test update",
            "enclosure": ["url": "http://127.0.0.1:54321/test.zip", "sparkle:version": "60",
                          "sparkle:shortVersionString": "1.0.3", "length": "100"]]))
    }

    func testCheckAndDownloadCanUseVPNWithoutLockingConnections() throws {
        var state = UpdateSafetyState(connectionOrServiceActive: true)
        var notices: [String] = []
        let (app, sparkle, _) = try fixture(state: { state }, notify: { notices.append($0) })
        XCTAssertTrue(app.isConfigured)
        XCTAssertTrue(app.canTerminateForUpdate()) // An ordinary quit keeps its prior behavior.
        try app.updater(sparkle, mayPerform: .updates)
        try app.updater(sparkle, shouldProceedWithUpdate: item(), updateCheck: .updates)
        XCTAssertTrue(app.isUpdateCycleActive)
        XCTAssertFalse(app.blocksNewConnection)
        state.hasUnsavedDraft = true
        XCTAssertTrue(app.canTerminateForUpdate())
        app.updater(sparkle, didFinishUpdateCycleFor: .updates, error: nil)
        XCTAssertFalse(app.blocksNewConnection)
        XCTAssertTrue(notices.isEmpty)
    }

    func testAutomaticSchedulerSessionEdgesNeverLockNetworking() throws {
        let (app, _, _) = try fixture()
        app.synchronizeSessionState(true)
        XCTAssertTrue(app.isUpdateCycleActive)
        XCTAssertFalse(app.blocksNewConnection)
        // Sparkle schedules a timer without didFinishUpdateCycleFor being called.
        app.synchronizeSessionState(false)
        XCTAssertFalse(app.isUpdateCycleActive)
        XCTAssertFalse(app.blocksNewConnection)
    }

    func testAlreadyCurrentIsNotReportedAsFailedCheck() throws {
        let (app, sparkle, _) = try fixture(state: { UpdateSafetyState(connectionOrServiceActive: true) })
        try app.updater(sparkle, mayPerform: .updates)
        app.updater(sparkle, didFinishUpdateCycleFor: .updates,
                    error: NSError(domain: SUSparkleErrorDomain, code: Int(SUError.noUpdateError.rawValue)))
        XCTAssertFalse(app.blocksNewConnection)
        XCTAssertFalse(app.isUpdateCycleActive)
        XCTAssertEqual(app.statusDescription, "当前已是最新版本，可保持 VPN 连接。")
    }

    func testRelaunchPolicyQueryHasNoInstallationSideEffects() throws {
        let (app, sparkle, defaults) = try fixture(state: { UpdateSafetyState(connectionOrServiceActive: true) })
        XCTAssertTrue(app.updaterShouldRelaunchApplication(sparkle))
        XCTAssertFalse(app.installationInProgress)
        XCTAssertFalse(app.blocksNewConnection)
        XCTAssertNil(defaults.string(forKey: "GoConnectPendingUpdateBuild"))
    }

    func testPreparedInstallerAndColdRestartGuardQuitWithoutLockingConnections() throws {
        let (app, sparkle, defaults) = try fixture()
        app.updater(sparkle, willInstallUpdate: try item())
        app.updater(sparkle, didFinishUpdateCycleFor: .updates, error: nil)
        XCTAssertFalse(app.installationInProgress)
        XCTAssertFalse(app.blocksNewConnection)
        let (restarted, recoveredSparkle, _) = try fixture(defaults: defaults, state: { UpdateSafetyState(connectionOrServiceActive: true) })
        XCTAssertFalse(restarted.blocksNewConnection)
        XCTAssertTrue(restarted.hasUnconfirmedInstallation)
        XCTAssertFalse(restarted.canTerminateForUpdate())
        restarted.updater(recoveredSparkle, didFinishUpdateCycleFor: .updates, error: NSError(domain: "offline", code: 1))
        XCTAssertFalse(restarted.blocksNewConnection)
        XCTAssertTrue(restarted.hasUnconfirmedInstallation)
        XCTAssertFalse(restarted.canTerminateForUpdate())
        let (updated, _, _) = try fixture(build: "59", defaults: defaults)
        XCTAssertFalse(updated.blocksNewConnection)
        XCTAssertFalse(updated.hasUnconfirmedInstallation)
    }

    func testInterruptedInstallKeepsQuitProtectionEvenIfUpdateConfigurationIsUnavailable() throws {
        let (app, sparkle, defaults) = try fixture()
        app.updater(sparkle, willInstallUpdate: try item())
        let (restarted, _, _) = try fixture(defaults: defaults, validConfiguration: false,
            state: { UpdateSafetyState(connectionOrServiceActive: true) })
        XCTAssertFalse(restarted.isConfigured)
        XCTAssertFalse(restarted.blocksNewConnection)
        XCTAssertTrue(restarted.hasUnconfirmedInstallation)
        XCTAssertFalse(restarted.canTerminateForUpdate())
    }

    func testInstallWaitsForNormalTeardownBeforeInvokingSparkle() async throws {
        var state = UpdateSafetyState(connectionOrServiceActive: true)
        var preparations = 0
        let installed = expectation(description: "install after disconnect")
        let (app, sparkle, _) = try fixture(state: { state }, prepare: {
            preparations += 1
            state.connectionOrServiceActive = false
            return true
        })
        XCTAssertTrue(app.updater(sparkle, shouldPostponeRelaunchForUpdate: try item(), untilInvokingBlock: {
            XCTAssertFalse(state.connectionOrServiceActive)
            installed.fulfill()
        }))
        XCTAssertTrue(app.blocksNewConnection)
        await fulfillment(of: [installed], timeout: 2)
        XCTAssertEqual(preparations, 1)
        XCTAssertFalse(app.hasDeferredInstallation)
        XCTAssertTrue(app.canTerminateForUpdate())
    }

    func testUnsavedDraftDefersInstallationWithoutDisconnectingVPN() async throws {
        var state = UpdateSafetyState(connectionOrServiceActive: true, hasUnsavedDraft: true)
        var preparations = 0
        let installed = expectation(description: "explicit retry after save")
        let (app, sparkle, _) = try fixture(state: { state }, prepare: {
            preparations += 1
            state.connectionOrServiceActive = false
            return true
        })
        XCTAssertTrue(app.updater(sparkle, shouldPostponeRelaunchForUpdate: try item(), untilInvokingBlock: { installed.fulfill() }))
        XCTAssertEqual(preparations, 0)
        XCTAssertTrue(state.connectionOrServiceActive)
        XCTAssertFalse(app.blocksNewConnection)
        XCTAssertTrue(app.hasDeferredInstallation)
        XCTAssertFalse(app.canTerminateForUpdate())
        state.hasUnsavedDraft = false
        app.resumeDeferredInstallation()
        await fulfillment(of: [installed], timeout: 2)
        XCTAssertEqual(preparations, 1)
    }

    func testAbortDuringTeardownReleasesGateAndCannotInvokeStaleInstaller() async throws {
        var continuation: CheckedContinuation<Bool, Never>?
        var installed = false
        let started = expectation(description: "teardown started")
        let (app, sparkle, _) = try fixture(prepare: {
            await withCheckedContinuation { continuation = $0; started.fulfill() }
        })
        XCTAssertTrue(app.updater(sparkle, shouldPostponeRelaunchForUpdate: try item(), untilInvokingBlock: { installed = true }))
        await fulfillment(of: [started], timeout: 2)
        XCTAssertTrue(app.blocksNewConnection)
        app.updater(sparkle, didFinishUpdateCycleFor: .updates, error: NSError(domain: "cancel", code: 1))
        XCTAssertFalse(app.blocksNewConnection)
        continuation?.resume(returning: true)
        await Task.yield()
        XCTAssertFalse(installed)
        XCTAssertFalse(app.hasDeferredInstallation)
    }

    func testOptionalDelegateMethodsUseTheActualObjectiveCSelectors() throws {
        let (app, _, _) = try fixture()
        for selector in ["updater:mayPerformUpdateCheck:error:", "updater:shouldProceedWithUpdate:updateCheck:error:",
                         "updaterShouldRelaunchApplication:", "updater:shouldPostponeRelaunchForUpdate:untilInvokingBlock:",
                         "updater:willInstallUpdateOnQuit:immediateInstallationBlock:",
                         "updater:didFinishUpdateCycleForUpdateCheck:error:"] {
            XCTAssertTrue(app.responds(to: NSSelectorFromString(selector)), selector)
        }
    }

    func testTestRunnerAndPreviewBuildsStayDisabledWithoutStartingSparkle() {
        let app = AppUpdater(safetyState: { UpdateSafetyState() }, notify: { _ in })
        app.start()
        XCTAssertFalse(app.isConfigured)
        XCTAssertFalse(app.canCheckForUpdates)
        XCTAssertFalse(app.blocksNewConnection)
        XCTAssertFalse(app.automaticallyChecksForUpdates)
    }
}

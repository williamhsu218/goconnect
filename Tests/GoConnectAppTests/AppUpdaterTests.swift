import XCTest
import Sparkle
import GoConnectCore
@testable import GoConnect

@MainActor
final class AppUpdaterTests: XCTestCase {
    private func fixture(build: String = "58", defaults: UserDefaults? = nil, validConfiguration: Bool = true,
                         state: @escaping @MainActor () -> UpdateSafetyState = { UpdateSafetyState() },
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
        let appUpdater = AppUpdater(bundle: bundle, defaults: settings, safetyState: state, notify: notify)
        let driver = SPUStandardUserDriver(hostBundle: bundle, delegate: nil)
        let sparkle = SPUUpdater(hostBundle: bundle, applicationBundle: bundle, userDriver: driver, delegate: appUpdater)
        return (appUpdater, sparkle, settings)
    }

    func testCheckCycleAndTerminationReevaluateCurrentSafetyState() throws {
        var state = UpdateSafetyState(connectionOrServiceActive: true)
        var notices: [String] = []
        let (app, sparkle, _) = try fixture(state: { state }, notify: { notices.append($0) })
        XCTAssertTrue(app.isConfigured)
        XCTAssertTrue(app.canTerminateForUpdate()) // An ordinary quit keeps its prior behavior.
        XCTAssertThrowsError(try app.updater(sparkle, mayPerform: .updates))
        XCTAssertFalse(app.blocksNewConnection)
        state = UpdateSafetyState()
        try app.updater(sparkle, mayPerform: .updates)
        XCTAssertTrue(app.blocksNewConnection)
        state.hasUnsavedDraft = true
        XCTAssertFalse(app.canTerminateForUpdate())
        XCTAssertFalse(notices.isEmpty)
        state.hasUnsavedDraft = false
        XCTAssertTrue(app.canTerminateForUpdate())
        app.updater(sparkle, didFinishUpdateCycleFor: .updates, error: nil)
        XCTAssertFalse(app.blocksNewConnection)
    }

    func testPreparedInstallerLocksCurrentProcessButColdRestartOnlyGuardsQuit() throws {
        let (app, sparkle, defaults) = try fixture()
        XCTAssertTrue(app.updaterShouldRelaunchApplication(sparkle))
        app.updater(sparkle, didFinishUpdateCycleFor: .updates, error: nil)
        XCTAssertTrue(app.installationInProgress)
        XCTAssertTrue(app.blocksNewConnection)
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
        XCTAssertTrue(app.updaterShouldRelaunchApplication(sparkle))
        let (restarted, _, _) = try fixture(defaults: defaults, validConfiguration: false,
            state: { UpdateSafetyState(connectionOrServiceActive: true) })
        XCTAssertFalse(restarted.isConfigured)
        XCTAssertFalse(restarted.blocksNewConnection)
        XCTAssertTrue(restarted.hasUnconfirmedInstallation)
        XCTAssertFalse(restarted.canTerminateForUpdate())
    }

    func testRelaunchGateRejectsActivityThatStartedAfterCheck() throws {
        var state = UpdateSafetyState()
        let (app, sparkle, _) = try fixture(state: { state })
        try app.updater(sparkle, mayPerform: .updates)
        state.connectionOrServiceActive = true
        XCTAssertFalse(app.updaterShouldRelaunchApplication(sparkle))
        XCTAssertFalse(app.canTerminateForUpdate())
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

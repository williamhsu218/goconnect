import XCTest
import GoConnectCore
@testable import GoConnect

@MainActor
final class AppUpdateQAStoreTests: XCTestCase {
    private func fixture(identifier: String = "com.willhsu.GoConnect.UpdateQA", flag: Any? = true) throws -> (Bundle, URL) {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("GoConnect-QAStore-" + UUID().uuidString)
        let app = root.appendingPathComponent("UpdateQA.app"), data = root.appendingPathComponent("data")
        try FileManager.default.createDirectory(at: app.appendingPathComponent("Contents"), withIntermediateDirectories: true)
        var info: [String: Any] = ["CFBundleIdentifier": identifier, "CFBundlePackageType": "APPL",
                                   "GoConnectUpdateQADataDirectory": data.path]
        info["GoConnectUpdateQAMode"] = flag
        try PropertyListSerialization.data(fromPropertyList: info, format: .xml, options: 0)
            .write(to: app.appendingPathComponent("Contents/Info.plist"))
        addTeardownBlock { try? FileManager.default.removeItem(at: root) }
        return (try XCTUnwrap(Bundle(path: app.path)), data)
    }

    func testQAEntryCanPrepareInstallationWithoutAnyBundledOrProductionHelper() async throws {
        let (bundle, data) = try fixture()
        XCTAssertFalse(FileManager.default.fileExists(atPath: bundle.bundleURL.appendingPathComponent("Contents/Resources/Runtime").path))
        let store = AppDelegate.makeStore(bundle: bundle)
        XCTAssertEqual(store.file.url.deletingLastPathComponent().resolvingSymlinksInPath(), data.resolvingSymlinksInPath())
        store.phase = "connected" // The QA window's existing simulated connection.
        let prepared = await store.prepareForUpdateInstallation()
        XCTAssertTrue(prepared, "QA installation must not query a missing helper or the live production VPN")
        XCTAssertEqual(store.phase, "idle")
        XCTAssertEqual(store.networkService.status.state, "notInstalled")
        XCTAssertEqual(store.networkService.status.message, "隔离更新验收不访问正式网络服务。")
        XCTAssertNil(store.updateSafetyState.blockingReason)
    }

    func testQAEntryStillProtectsUnsavedDraftBeforeDisconnecting() async throws {
        let (bundle, _) = try fixture()
        let store = AppDelegate.makeStore(bundle: bundle)
        store.phase = "connected"
        store.beginNewProfile()
        let prepared = await store.prepareForUpdateInstallation()
        XCTAssertFalse(prepared)
        XCTAssertEqual(store.phase, "connected")
        XCTAssertNotNil(store.profileDraft)
        XCTAssertEqual(store.networkService.status.state, "checking", "A blocked preparation must not read even the QA status source")
    }

    func testIsolationRequiresBothExactBundleIdentityAndExplicitBooleanFlag() throws {
        for (identifier, flag, expected) in [
            ("com.willhsu.GoConnect.UpdateQA", true as Any?, true),
            ("com.willhsu.GoConnect", true as Any?, false),
            ("com.willhsu.GoConnect.UpdateQA.other", true as Any?, false),
            ("com.willhsu.GoConnect.UpdateQA", false as Any?, false),
            ("com.willhsu.GoConnect.UpdateQA", "true" as Any?, false),
            ("com.willhsu.GoConnect.UpdateQA", nil, false)
        ] {
            let (bundle, _) = try fixture(identifier: identifier, flag: flag)
            XCTAssertEqual(AppDelegate.isUpdateQABundle(bundle), expected)
        }
    }
}

import XCTest
@testable import GoConnectCore

final class UpdateSafetyPolicyTests: XCTestCase {
    private var releaseInfo: [String: Any] { [
        "GoConnectSigningMode": "team", "GoConnectUpdatesEnabled": true,
        "SUAllowsAutomaticUpdates": false, "SUAutomaticallyUpdate": false, "SUEnableAutomaticChecks": false,
        "SURequireSignedFeed": true, "SUVerifyUpdateBeforeExtraction": true,
        "SUFeedURL": AppUpdateConfiguration.feedURLString,
        "SUPublicEDKey": Data(repeating: 1, count: 32).base64EncodedString(),
        "GoConnectUpdateChannel": "preview"
    ] }

    func testProtectedOperationsBlockInstallationPreparationButVPNCanBeDrained() {
        XCTAssertNil(UpdateSafetyState().blockingReason)
        for state in [UpdateSafetyState(connectionOrServiceActive: true), UpdateSafetyState(hasUnsavedDraft: true),
                      UpdateSafetyState(isTestingNodes: true), UpdateSafetyState(isUpdatingSubscriptions: true),
                      UpdateSafetyState(isMaintainingService: true)] {
            XCTAssertNotNil(state.blockingReason)
        }
        XCTAssertNil(UpdateSafetyState(connectionOrServiceActive: true).preparationBlockingReason)
        for state in [UpdateSafetyState(hasUnsavedDraft: true), UpdateSafetyState(isTestingNodes: true),
                      UpdateSafetyState(isUpdatingSubscriptions: true), UpdateSafetyState(isMaintainingService: true)] {
            XCTAssertNotNil(state.preparationBlockingReason)
        }
    }

    func testOnlyExplicitSignedReleaseConfigurationEnablesUpdater() {
        let configuration = AppUpdateConfiguration(bundleIdentifier: "com.willhsu.GoConnect", info: releaseInfo)
        XCTAssertEqual(configuration?.allowedChannels, ["preview"])
        XCTAssertNil(AppUpdateConfiguration(bundleIdentifier: "com.willhsu.GoConnect.UIPreview", info: releaseInfo))
        XCTAssertNil(AppUpdateConfiguration(bundleIdentifier: nil, info: releaseInfo))
        for key in ["GoConnectSigningMode", "GoConnectUpdatesEnabled", "SUAllowsAutomaticUpdates", "SUAutomaticallyUpdate", "SUEnableAutomaticChecks", "SURequireSignedFeed", "SUVerifyUpdateBeforeExtraction", "SUFeedURL", "SUPublicEDKey"] {
            var info = releaseInfo; info.removeValue(forKey: key)
            XCTAssertNil(AppUpdateConfiguration(bundleIdentifier: "com.willhsu.GoConnect", info: info), key)
        }
        for (key, value) in [("GoConnectSigningMode", "adhoc" as Any), ("GoConnectUpdatesEnabled", false as Any),
                             ("SUAllowsAutomaticUpdates", true as Any), ("SUAutomaticallyUpdate", true as Any), ("SUEnableAutomaticChecks", true as Any),
                             ("SURequireSignedFeed", false as Any), ("SUVerifyUpdateBeforeExtraction", false as Any)] {
            var info = releaseInfo; info[key] = value
            XCTAssertNil(AppUpdateConfiguration(bundleIdentifier: "com.willhsu.GoConnect", info: info), key)
        }
    }

    func testFeedCannotBeMovedToLatestReleaseOrUntrustedHost() {
        for value in ["http://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml",
                      "https://github.com/williamhsu218/goconnect/releases/latest/download/appcast.xml",
                      "https://example.com/appcast.xml", AppUpdateConfiguration.feedURLString + "?token=example"] {
            var info = releaseInfo; info["SUFeedURL"] = value
            XCTAssertNil(AppUpdateConfiguration(bundleIdentifier: "com.willhsu.GoConnect", info: info))
        }
    }

    func testInvalidKeysAndUnknownChannelsFailClosed() {
        for key in ["", "invalid", Data(repeating: 0, count: 31).base64EncodedString(), Data(repeating: 0, count: 64).base64EncodedString()] {
            var info = releaseInfo; info["SUPublicEDKey"] = key
            XCTAssertNil(AppUpdateConfiguration(bundleIdentifier: "com.willhsu.GoConnect", info: info))
        }
        var info = releaseInfo; info["GoConnectUpdateChannel"] = "nightly"
        XCTAssertNil(AppUpdateConfiguration(bundleIdentifier: "com.willhsu.GoConnect", info: info))
        info["GoConnectUpdateChannel"] = "stable"
        XCTAssertEqual(AppUpdateConfiguration(bundleIdentifier: "com.willhsu.GoConnect", info: info)?.allowedChannels, [])
    }

    func testLocalFeedRequiresExactIsolatedQABundleAndExplicitMode() {
        var info = releaseInfo; info["GoConnectUpdateQAMode"] = true
        info["SUFeedURL"] = "http://127.0.0.1:54321/appcast.xml"
        let qaID = "com.willhsu.GoConnect.UpdateQA"
        XCTAssertNotNil(AppUpdateConfiguration(bundleIdentifier: qaID, info: info))
        XCTAssertNil(AppUpdateConfiguration(bundleIdentifier: "com.willhsu.GoConnect", info: info))
        XCTAssertNil(AppUpdateConfiguration(bundleIdentifier: "com.willhsu.GoConnect.UIPreview", info: info))
        info["GoConnectUpdateQAMode"] = false
        XCTAssertNil(AppUpdateConfiguration(bundleIdentifier: qaID, info: info))
        info["GoConnectUpdateQAMode"] = true
        for value in ["http://localhost:54321/appcast.xml", "http://127.0.0.2:54321/appcast.xml",
                      "http://127.0.0.1/appcast.xml", "http://127.0.0.1:54321/other.xml",
                      "http://user:pass@127.0.0.1:54321/appcast.xml", "http://127.0.0.1:54321/appcast.xml?x=1",
                      "http://127.0.0.1:54321/appcast.xml#x", "https://example.com/appcast.xml"] {
            info["SUFeedURL"] = value
            XCTAssertNil(AppUpdateConfiguration(bundleIdentifier: qaID, info: info), value)
        }
    }
}

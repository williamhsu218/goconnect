import Foundation
import XCTest
@testable import GoConnectCore

final class ApplicationRoutingSupportTests: XCTestCase {
    private func fixture(_ name: String = "Native.app") throws -> (URL, AllowedApplication) {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).resolvingSymlinksInPath()
        let app = root.appendingPathComponent(name)
        try FileManager.default.createDirectory(at: app.appendingPathComponent("Contents/MacOS"), withIntermediateDirectories: true)
        let data = try PropertyListSerialization.data(fromPropertyList: ["CFBundleExecutable": "Native"], format: .xml, options: 0)
        try data.write(to: app.appendingPathComponent("Contents/Info.plist"))
        let binary = app.appendingPathComponent("Contents/MacOS/Native")
        try Data("#!/bin/sh\nexit 0\n".utf8).write(to: binary)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: binary.path)
        addTeardownBlock { try FileManager.default.removeItem(at: root) }
        return (app, AllowedApplication(name: name, bundleID: "test.native", path: app.path))
    }

    private func executableFixture(_ name: String = "agy") throws -> (URL, AllowedApplication) {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).resolvingSymlinksInPath()
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        let executable = root.appendingPathComponent(name)
        try Data("#!/bin/sh\nexit 0\n".utf8).write(to: executable)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: executable.path)
        addTeardownBlock { try FileManager.default.removeItem(at: root) }
        return (executable, AllowedApplication(name: name, bundleID: "executable." + name, path: executable.path))
    }

    func testNativeAppAndNonASCIIPathAreSupported() throws {
        let (_, app) = try fixture("测试 App.app")
        XCTAssertNil(ApplicationRoutingSupport.issue(for: app))
        XCTAssertNoThrow(try ApplicationRoutingSupport.validate([app]))
    }

    func testWrappedIOSAppFailsWithItsNameBeforeSessionStartup() throws {
        let (url, app) = try fixture("Weather.app")
        try FileManager.default.removeItem(at: url.appendingPathComponent("Contents"))
        try FileManager.default.createDirectory(at: url.appendingPathComponent("Wrapper/Weather.app"), withIntermediateDirectories: true)
        XCTAssertTrue(ApplicationRoutingSupport.issue(for: app)?.contains("iPhone / iPad") == true)
        XCTAssertThrowsError(try ApplicationRoutingSupport.validate([app])) { error in
            XCTAssertTrue(error.localizedDescription.contains("Weather.app"))
            XCTAssertTrue(error.localizedDescription.contains("iPhone / iPad"))
        }
    }

    func testSavedAppsAreRecheckedAfterRemovalAndExecutableReplacement() throws {
        let (url, app) = try fixture()
        XCTAssertNil(ApplicationRoutingSupport.issue(for: app))
        let binary = url.appendingPathComponent("Contents/MacOS/Native")
        try FileManager.default.removeItem(at: binary)
        try FileManager.default.createSymbolicLink(at: binary, withDestinationURL: URL(fileURLWithPath: "/bin/sh"))
        XCTAssertNotNil(ApplicationRoutingSupport.issue(for: app))
        try FileManager.default.removeItem(at: url)
        XCTAssertTrue(ApplicationRoutingSupport.issue(for: app)?.contains("已移除") == true)
    }

    func testUnsupportedPathCannotEnterRoutingSession() throws {
        let (_, app) = try fixture("Comma,App.app")
        XCTAssertNotNil(ApplicationRoutingSupport.issue(for: app))
        let (safariURL, _) = try fixture("Safari.app")
        let safari = AllowedApplication(name: "Safari", bundleID: "com.apple.Safari", path: safariURL.path)
        XCTAssertTrue(ApplicationRoutingSupport.issue(for: safari)?.contains("Safari") == true)
    }
    func testProcessRoutingDoesNotRequireNativeLauncherButRejectsMissingApp() throws {
        let (url, app) = try fixture("Wrapped.app")
        try FileManager.default.removeItem(at: url.appendingPathComponent("Contents"))
        try FileManager.default.createDirectory(at: url.appendingPathComponent("Wrapper"), withIntermediateDirectories: true)
        XCTAssertNil(ApplicationRoutingSupport.issue(for: app, requiresLauncher: false))
        XCTAssertNotNil(ApplicationRoutingSupport.issue(for: app))
        try FileManager.default.removeItem(at: url)
        XCTAssertNotNil(ApplicationRoutingSupport.issue(for: app, requiresLauncher: false))
    }

    func testStandaloneExecutableServiceUsesCanonicalExactFile() throws {
        let (url, service) = try executableFixture()
        XCTAssertNil(ApplicationRoutingSupport.issue(for: service, requiresLauncher: false))
        XCTAssertTrue(ApplicationRoutingSupport.issue(for: service)?.contains("旧版") == true)
        XCTAssertEqual(service.targetTypeTitle, "可执行服务")

        try FileManager.default.setAttributes([.posixPermissions: 0o644], ofItemAtPath: url.path)
        XCTAssertTrue(ApplicationRoutingSupport.issue(for: service, requiresLauncher: false)?.contains("不可执行") == true)
    }

    func testStandaloneExecutableSymlinkIsRejected() throws {
        let (url, _) = try executableFixture("agy-real")
        let link = url.deletingLastPathComponent().appendingPathComponent("agy-link")
        try FileManager.default.createSymbolicLink(at: link, withDestinationURL: url)
        let service = AllowedApplication(name: "agy", bundleID: "executable.agy", path: link.path)
        XCTAssertTrue(ApplicationRoutingSupport.issue(for: service, requiresLauncher: false)?.contains("真实安装位置") == true)
    }

    func testNewSessionEnablesLiveRoutingAndAllowsEmptyWhitelist() throws {
        let session = RouteSession(ownerPID: 100, controlDirectory: "/tmp/test", token: "test", socksPort: 1000, appPaths: [], gateway: "127.0.0.1")
        let data = try JSONEncoder().encode(session)
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
        XCTAssertEqual(object["liveRouting"] as? Bool, true)
        XCTAssertEqual((object["appPaths"] as? [String])?.count, 0)
    }

}

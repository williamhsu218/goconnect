import Foundation
import XCTest
@testable import GoConnectCore

final class ExecutableRoutingPathTests: XCTestCase {
    private func root() throws -> URL {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).resolvingSymlinksInPath()
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        addTeardownBlock { try FileManager.default.removeItem(at: url) }
        return url
    }

    private func executable(_ url: URL) throws {
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try Data("#!/bin/sh\nexit 0\n".utf8).write(to: url)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: url.path)
    }

    private func link(_ url: URL, to target: URL) throws {
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        if FileManager.default.fileExists(atPath: url.path) { try FileManager.default.removeItem(at: url) }
        try FileManager.default.createSymbolicLink(at: url, withDestinationURL: target)
    }

    func testStableEntryFollowsUpgradeAndSessionContainsOnlyNewExactPath() throws {
        let root = try root()
        let old = root.appendingPathComponent("versions/1/claude")
        let new = root.appendingPathComponent("versions/2/claude")
        let entry = root.appendingPathComponent("bin/claude")
        try executable(old); try executable(new); try link(entry, to: old)
        let saved = try ExecutableRoutingPath().savedPath(for: entry)
        let app = AllowedApplication(name: "claude", bundleID: "executable.claude", path: saved)
        XCTAssertEqual(saved, entry.path)
        XCTAssertEqual(try ApplicationRoutingSupport.routingPaths(for: [app]), [old.path])
        try link(entry, to: new)
        try FileManager.default.removeItem(at: old)
        let paths = try ApplicationRoutingSupport.routingPaths(for: [app])
        XCTAssertEqual(paths, [new.path])
        let session = RouteSession(ownerPID: 100, controlDirectory: root.path, token: "test", socksPort: 1000, appPaths: paths, gateway: "127.0.0.1")
        let readback = try JSONDecoder().decode(RouteSession.self, from: JSONEncoder().encode(session))
        XCTAssertEqual(readback.appPaths, [new.path])
        XCTAssertEqual(try JSONDecoder().decode(AllowedApplication.self, from: JSONEncoder().encode(app)).path, entry.path)
    }

    func testHomebrewVersionsAreSavedAsEntryAndOldRecordsRecoverAfterRemoval() throws {
        for layout in ["Caskroom/claude-code@latest", "Cellar/claude-code"] {
            let root = try root()
            let suffix = layout.hasPrefix("Cellar") ? "bin/claude" : "claude"
            let old = root.appendingPathComponent(layout + "/1/" + suffix)
            let new = root.appendingPathComponent(layout + "/2/" + suffix)
            let entry = root.appendingPathComponent("bin/claude")
            let resolver = ExecutableRoutingPath(homebrewPrefixes: [root.path])
            try executable(old); try executable(new); try link(entry, to: old)
            XCTAssertEqual(try resolver.savedPath(for: old), entry.path)
            try link(entry, to: new)
            XCTAssertEqual(try resolver.resolvedPath(for: old.path), new.path)
            try FileManager.default.removeItem(at: old)
            XCTAssertEqual(try resolver.resolvedPath(for: old.path), new.path)
        }
    }

    func testLegacyHomebrewEntryCannotSwitchToDifferentPackageOrExecutable() throws {
        let root = try root()
        let old = root.appendingPathComponent("Caskroom/claude-code@latest/1/claude")
        let entry = root.appendingPathComponent("bin/claude")
        let resolver = ExecutableRoutingPath(homebrewPrefixes: [root.path])
        for relative in ["Caskroom/other/2/claude", "Caskroom/claude-code@latest/2/other/claude", "unrelated/claude"] {
            let target = root.appendingPathComponent(relative)
            try executable(target); try link(entry, to: target)
            XCTAssertThrowsError(try resolver.resolvedPath(for: old.path))
        }
        try executable(old)
        XCTAssertEqual(try resolver.resolvedPath(for: old.path), old.path)
    }

    func testBrokenCyclicAndInvalidEntryTargetsFailWithoutGuessing() throws {
        let root = try root()
        let entry = root.appendingPathComponent("bin/claude")
        let target = root.appendingPathComponent("real/claude")
        try link(entry, to: target)
        XCTAssertThrowsError(try ExecutableRoutingPath().resolvedPath(for: entry.path))
        try executable(target)
        try FileManager.default.setAttributes([.posixPermissions: 0o644], ofItemAtPath: target.path)
        XCTAssertThrowsError(try ExecutableRoutingPath().resolvedPath(for: entry.path))
        try FileManager.default.removeItem(at: target)
        try FileManager.default.createDirectory(at: target, withIntermediateDirectories: true)
        XCTAssertThrowsError(try ExecutableRoutingPath().resolvedPath(for: entry.path))
        try FileManager.default.removeItem(at: entry)
        try FileManager.default.createSymbolicLink(at: entry, withDestinationURL: entry)
        XCTAssertThrowsError(try ExecutableRoutingPath().resolvedPath(for: entry.path))
    }

    func testResolvedTargetCannotInjectRuleSyntaxAndAliasesAreDeduplicated() throws {
        let root = try root()
        let entry = root.appendingPathComponent("bin/claude")
        let unsafe = root.appendingPathComponent("real/bad,GoConnect")
        try executable(unsafe); try link(entry, to: unsafe)
        XCTAssertThrowsError(try ExecutableRoutingPath().resolvedPath(for: entry.path))
        let target = root.appendingPathComponent("real/claude")
        try executable(target); try link(entry, to: target)
        let apps = [entry, target].map { AllowedApplication(name: "claude", bundleID: "executable.claude", path: $0.path) }
        XCTAssertEqual(try ApplicationRoutingSupport.routingPaths(for: apps), [target.path])
        XCTAssertThrowsError(try ExecutableRoutingPath().resolvedPath(for: root.path + "/bin/../bin/claude"))
    }

    func testDirectExclusionWinsWhenWhitelistAndExclusionUseDifferentAliases() throws {
        let root = try root()
        let entry = root.appendingPathComponent("bin/claude")
        let target = root.appendingPathComponent("real/claude")
        try executable(target); try link(entry, to: target)
        let allowed = AllowedApplication(name: "claude", bundleID: "executable.claude", path: entry.path)
        let excluded = AllowedApplication(name: "claude", bundleID: "executable.claude", path: target.path)
        let connection = SavedConnection(applications: [allowed], excludedApplications: [excluded])
        XCTAssertEqual(connection.conflictingApplications, [allowed])
        XCTAssertTrue(connection.effectiveVPNApplications.isEmpty)
    }
}

import XCTest
@testable import GoConnectCore

final class AccessModeTests: XCTestCase {
    func testSubscriptionCannotEnableCompanyRouting() throws {
        var config = Configuration()
        config.connections[0].profile.name = "Subscription fixture"
        config.connections[0].subscription = SubscriptionSelection(subscriptionID: UUID(), nodeName: "Fixture")
        config.connections[0].remoteNetworks = ["192.168.14.70"]
        var draft = ConnectionDraft(editing: config.activeConnection)
        draft.connection.companyRoutesEnabled = true
        XCTAssertFalse(draft.connection.usesCompanyRoutes)
        XCTAssertThrowsError(try config.save(draft))
    }

    func testLegacyGlobalMigratesToExplicitProxyWithoutLosingIntent() throws {
        var original = Configuration()
        original.routingMode = .global
        original.applications = [AllowedApplication(name: "Browser", bundleID: "test.browser", path: "/Applications/Browser.app")]
        original.excludedApplications = [AllowedApplication(name: "Chat", bundleID: "test.chat", path: "/Applications/Chat.app")]
        original.connections[0].directDomains = [try DomainRule("example.org", includeSubdomains: true)]
        original.connections[0].remoteNetworks = ["192.168.14.70"]
        var object = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(original)) as? [String: Any])
        object["schemaVersion"] = 5
        var rows = try XCTUnwrap(object["connections"] as? [[String: Any]])
        rows[0].removeValue(forKey: "accessMode"); object["connections"] = rows
        let restored = try JSONDecoder().decode(Configuration.self, from: JSONSerialization.data(withJSONObject: object))
        XCTAssertEqual(restored.schemaVersion, 7)
        XCTAssertEqual(restored.accessMode, .applicationProxy)
        XCTAssertEqual(restored.routingMode, .global)
        XCTAssertEqual(restored.applications, original.applications)
        XCTAssertEqual(restored.excludedApplications, original.excludedApplications)
        XCTAssertEqual(restored.activeConnection.directDomains, original.activeConnection.directDomains)
        XCTAssertEqual(restored.activeConnection.remoteNetworks, original.activeConnection.remoteNetworks)
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let file = ConfigurationFile(url: directory.appendingPathComponent("configuration.json"))
        let legacy = try JSONSerialization.data(withJSONObject: object)
        try legacy.write(to: file.url)
        try file.save(restored)
        let backup = directory.appendingPathComponent("configuration.v5.backup.json")
        XCTAssertEqual(try Data(contentsOf: backup), legacy)
        let attributes = try FileManager.default.attributesOfItem(atPath: backup.path)
        XCTAssertEqual((attributes[.posixPermissions] as? NSNumber)?.intValue, 0o600)

    }
    func testCompanyModeRequiresExplicitNetworksAndPersistsSeparately() throws {
        var config = Configuration()
        config.connections[0].profile.server = "vpn.example.com"
        config.connections[0].profile.username = "test"
        var draft = ConnectionDraft(editing: config.activeConnection)
        draft.connection.companyRoutesEnabled = true
        XCTAssertThrowsError(try config.save(draft))
        draft.connection.remoteNetworks = ["192.168.14.70"]
        try config.save(draft)
        XCTAssertTrue(config.activeConnection.usesCompanyRoutes)
        XCTAssertThrowsError(try config.save(draft), "stale editor must not overwrite mode")
    }
    func testQuarantineSurvivesAppRestartUntilDifferentBoot() {
        let marker = NetworkQuarantine(bootID: "boot-a")
        XCTAssertTrue(marker.applies(bootID: "boot-a"))
        XCTAssertFalse(marker.applies(bootID: "boot-b"))
    }
    func testChromeArgumentsDoNotModifyExistingProfileOrEnableQUIC() {
        let args = ProxyApplicationSupport.chromeArguments(port: 12345, profileDirectory: "/tmp/space and quote' profile")
        XCTAssertEqual(args[0], "--user-data-dir=/tmp/space and quote' profile")
        XCTAssertTrue(args.contains("--proxy-server=http://127.0.0.1:12345"))
        XCTAssertTrue(args.contains("--disable-quic"))
        XCTAssertFalse(args.contains { $0.contains("Default") })
    }
}

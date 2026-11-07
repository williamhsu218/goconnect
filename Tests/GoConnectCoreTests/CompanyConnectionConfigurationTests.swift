import XCTest
@testable import GoConnectCore

final class CompanyConnectionConfigurationTests: XCTestCase {
    private func fixture() -> (Configuration, CompanyConnectionConfiguration) {
        var config = Configuration()
        var primary = SavedConnection(); primary.profile.name = "Primary"
        primary.subscription = SubscriptionSelection(subscriptionID: UUID(), nodeName: "HY2")
        var company = SavedConnection(); company.profile.server = "https://vpn.example.invalid"; company.profile.username = "fixture"
        config.connections = [primary, company]; config.activeProfileID = primary.id
        var value = CompanyConnectionConfiguration(); value.enabled = true; value.primaryProfileID = primary.id; value.companyProfileID = company.id
        value.networks = ["10.201.0.0/16"]; value.domains = ["corp.example.invalid"]
        return (config, value)
    }
    func testReferencesAndPrivateScopeFailClosed() throws {
        let (config, value) = fixture()
        XCTAssertEqual(try value.validated(in: config).dnsServers, [], "DNS is learned from the secondary; not required ahead of authentication")
        for network in ["0.0.0.0/0", "8.8.8.0/24", "fc00::/7", "100.64.0.0/10"] {
            var bad = value; bad.networks = [network]; XCTAssertThrowsError(try bad.validated(in: config))
        }
        var changed = config; changed.connections.removeLast(); XCTAssertThrowsError(try value.validated(in: changed))
        var same = value; same.companyProfileID = same.primaryProfileID; XCTAssertThrowsError(try same.validated(in: config))
    }
    func testDNSAndDomainsRejectAmbiguousInput() throws {
        let (config, value) = fixture()
        for dns in ["127.0.0.1", "::1", "1.1.1.1:53", "169.254.1.1", "resolver.example.com"] {
            var bad = value; bad.dnsServers = [dns]; XCTAssertThrowsError(try bad.validated(in: config))
        }
        for domain in ["local", "corp.local", "*.example.com", "10.20.0.1", "CORP.EXAMPLE.COM"] {
            var bad = value; bad.domains = [domain]; XCTAssertThrowsError(try bad.validated(in: config))
        }
    }
    func testSeparateSidecarSurvivesLegacyMainSaveAndIsPrivate() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let (config, value) = fixture()
        let sidecar = CompanyConnectionFile(url: directory.appendingPathComponent("company-connection.json"))
        try sidecar.save(value)
        let bytes = try Data(contentsOf: sidecar.url)
        let main = ConfigurationFile(url: directory.appendingPathComponent("configuration.json"))
        try main.save(config); try main.save(main.load())
        XCTAssertEqual(try Data(contentsOf: sidecar.url), bytes)
        XCTAssertEqual(try sidecar.load(), value)
        XCTAssertEqual((try FileManager.default.attributesOfItem(atPath: sidecar.url.path)[.posixPermissions] as? NSNumber)?.intValue, 0o600)
        var stale = config; stale.connections.removeLast(); XCTAssertThrowsError(try sidecar.load().validated(in: stale))
        let link = CompanyConnectionFile(url: directory.appendingPathComponent("link.json"))
        try FileManager.default.createSymbolicLink(at: link.url, withDestinationURL: sidecar.url)
        XCTAssertThrowsError(try link.load()); XCTAssertThrowsError(try link.save(value))
    }
}

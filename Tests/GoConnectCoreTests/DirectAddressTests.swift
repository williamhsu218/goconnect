import XCTest
@testable import GoConnectCore

final class DirectAddressTests: XCTestCase {
    func testAddressNormalizationAndProfileRoundtrip() throws {
        let subnet = try DomainRule("192.168.2.129/24")
        XCTAssertEqual(subnet.domain, "192.168.2.0/24")
        XCTAssertTrue(subnet.isAddress)
        XCTAssertFalse(subnet.includeSubdomains)
        XCTAssertEqual(try DomainRule("FD00:1234::1234/64").domain, "fd00:1234::/64")
        XCTAssertEqual(try DomainRule("10.211.55.3").domain, "10.211.55.3")
        XCTAssertFalse(try DomainRule("example.com").isAddress)
        var config = Configuration(); config.directDomains = [subnet, try DomainRule("example.com")]
        let decoded = try JSONDecoder().decode(Configuration.self, from: JSONEncoder().encode(config))
        XCTAssertEqual(decoded.directDomains, config.directDomains)
        try config.add(name: "Other", copyCurrent: false)
        XCTAssertTrue(config.directDomains.isEmpty)
    }
    func testInvalidNetworkRules() {
        for value in ["0.0.0.0/0", "::/0", "127.0.0.1", "224.0.0.251", "ff02::fb", "10.0.0.1/33", "fd00::/129", "10.0.0.1/-1", "192.168.2.1:80", "http://192.168.2.1", "fe80::1%en0", "::ffff:192.168.1.1"] {
            XCTAssertThrowsError(try DomainRule(value), value)
        }
    }
}

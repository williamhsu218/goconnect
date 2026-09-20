import XCTest
@testable import GoConnectCore

final class RemoteNetworkTests: XCTestCase {
    func testBoundsAndCanonicalization() throws {
        XCTAssertEqual(try RemoteNetwork.canonical("192.168.14.70/24"), "192.168.14.0/24")
        XCTAssertEqual(try RemoteNetwork.canonical("fd12::123/64"), "fd12::/64")
        for invalid in ["0.0.0.0/0", "192.0.0.0/8", "172.0.0.0/8", "100.64.0.0/10", "fe80::1", "8.8.8.8", "example.com"] {
            XCTAssertThrowsError(try RemoteNetwork.canonical(invalid), invalid)
        }
    }
    func testLegacyDefaultsAndLineIsolation() throws {
        var config = Configuration()
        var profile = config.connections[0]
        profile.profile.name = "First"; profile.profile.server = "vpn.example.com"; profile.profile.username = "test"
        config.connections[0] = profile
        let encoded = try JSONEncoder().encode(config)
        var json = try XCTUnwrap(JSONSerialization.jsonObject(with: encoded) as? [String: Any])
        var rows = try XCTUnwrap(json["connections"] as? [[String: Any]])
        rows[0].removeValue(forKey: "automaticRemoteNetworks"); rows[0].removeValue(forKey: "remoteNetworks"); json["connections"] = rows
        let restored = try JSONDecoder().decode(Configuration.self, from: JSONSerialization.data(withJSONObject: json))
        XCTAssertTrue(restored.activeConnection.automaticRemoteNetworks)
        XCTAssertTrue(restored.activeConnection.remoteNetworks.isEmpty)
        try config.add(name: "Second", copyCurrent: true)
        var draft = ConnectionDraft(editing: config.activeConnection)
        draft.connection.remoteNetworks = ["192.168.14.70"]
        try config.save(draft)
        XCTAssertTrue(config.connections[0].remoteNetworks.isEmpty)
        XCTAssertEqual(config.activeConnection.remoteNetworks, ["192.168.14.70"])
        // A stale editor must not overwrite another saved network policy.
        XCTAssertThrowsError(try config.save(draft))
    }
}

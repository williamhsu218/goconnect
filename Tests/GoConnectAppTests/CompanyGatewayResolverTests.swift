import GoConnectCore
import XCTest
@testable import GoConnect

final class CompanyGatewayResolverTests: XCTestCase {
    private func fixture() -> (Configuration, CompanyConnectionConfiguration) {
        var configuration = Configuration()
        let selection = SubscriptionSelection(subscriptionID: UUID(), nodeName: "Primary")
        configuration.connections[0].subscription = selection
        var secondary = SavedConnection()
        secondary.profile.server = "https://192.0.2.7:8443"
        secondary.profile.username = "fixture"
        configuration.connections.append(secondary)

        var company = CompanyConnectionConfiguration()
        company.enabled = true
        company.primaryProfileID = configuration.activeProfileID
        company.companyProfileID = secondary.id
        company.networks = ["10.201.0.0/16"]
        company.domains = ["corp.example.invalid"]
        return (configuration, company)
    }

    func testVLESSAndHY2ReserveTheSameCompanyGatewayAndScope() async throws {
        let (configuration, company) = fixture()
        let expected = CompanyReservation(networks: company.networks, domains: company.domains, dnsServers: [],
                                          gatewayHost: "192.0.2.7", gatewayPort: 8443, gatewayIPs: ["192.0.2.7"])
        for kind in ["vless", "hysteria2"] {
            let node = SubscriptionNode(name: "Primary", type: kind, proxy: ["type": .string(kind)])
            let reservation = try await CompanyGatewayResolver.reservation(company, configuration: configuration, primaryNode: node)
            XCTAssertEqual(reservation, expected, kind)
        }
    }

    func testMissingUnsupportedAndMismatchedPrimaryCannotReserveCompanyRoutes() async throws {
        let (configuration, company) = fixture()
        let unsupported = SubscriptionNode(name: "VLESS", type: "trojan", proxy: ["type": .string("trojan")])
        let mismatched = SubscriptionNode(name: "Primary", type: "vless", proxy: ["type": .string("trojan")])
        for node in [nil, unsupported, mismatched] {
            do {
                _ = try await CompanyGatewayResolver.reservation(company, configuration: configuration, primaryNode: node)
                XCTFail("Unexpected company reservation for \(String(describing: node?.type))")
            } catch CompanyConnectionError.invalid(let message) {
                XCTAssertTrue(message.contains("HY2") && message.contains("VLESS"))
            }
        }
    }
}

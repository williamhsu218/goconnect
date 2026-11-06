import XCTest
@testable import GoConnectCore

final class DisconnectReasonTests: XCTestCase {
    func testServerDecisionExplainsWhyTransportRecoveryTerminated() {
        for transport in [DisconnectReason.deadPeer, .tlsRead, .tlsWrite, .tls, .reconnect] {
            for terminal in [DisconnectReason.server, .idleTimeout, .sessionExpired, .cookieRejected] {
                XCTAssertEqual(transport.merging(terminal), terminal)
                XCTAssertEqual(terminal.merging(transport), terminal)
            }
        }
    }

    func testGenericFollowupDoesNotHideSpecificFailure() {
        XCTAssertEqual(DisconnectReason.tlsRead.merging(.tls).merging(.reconnect), .tlsRead)
        XCTAssertEqual(DisconnectReason.server.merging(.cookieRejected).merging(.reconnect), .cookieRejected)
        XCTAssertEqual(DisconnectReason.sessionExpired.merging(.server), .sessionExpired)
        XCTAssertEqual(DisconnectReason.unknown.merging(.tlsRead), .tlsRead)
    }
}

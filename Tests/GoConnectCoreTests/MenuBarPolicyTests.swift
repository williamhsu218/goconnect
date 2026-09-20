import XCTest
@testable import GoConnectCore

final class MenuBarPolicyTests: XCTestCase {
    func policy(_ state: VPNControlState, busy: Bool = false, readable: Bool = true,
                draft: Bool = false, testing: Bool = false) -> MenuBarControlPolicy {
        MenuBarControlPolicy(state: state, busy: busy, configurationReadable: readable,
                             editingProfile: draft, testingNodes: testing)
    }
    func testInProgressAndDiagnosticStatesCannotStartAnotherSession() {
        for state in [VPNControlState.preparingService, .maintainingService, .disconnecting, .diagnostic, .endingDiagnostic] {
            let p = policy(state, busy: true)
            XCTAssertFalse(p.actionEnabled, state.rawValue)
            XCTAssertFalse(p.quickConnectEnabled, state.rawValue)
            XCTAssertEqual(p.actionTitle, state.title)
        }
    }
    func testCancelAndDisconnectRemainAvailableWhileBusyOrEditingAnotherProfile() {
        for state in [VPNControlState.waitingForPassword, .connecting, .authorizing] {
            let p = policy(state, busy: true, draft: true)
            XCTAssertTrue(p.actionEnabled)
            XCTAssertEqual(p.actionTitle, "取消连接")
            XCTAssertFalse(p.quickConnectEnabled)
        }
        for state in [VPNControlState.connected, .needsRestart, .interrupted] {
            let p = policy(state, busy: true, readable: false, draft: true)
            XCTAssertTrue(p.actionEnabled)
            XCTAssertEqual(p.actionTitle, "断开连接")
            XCTAssertFalse(p.quickConnectEnabled)
        }
    }
    func testUnreadableDataDraftAndNodeTestsBlockNewConnections() {
        for state in [VPNControlState.off, .failed] {
            XCTAssertTrue(policy(state).quickConnectEnabled)
            for p in [policy(state, busy: true), policy(state, readable: false),
                      policy(state, draft: true), policy(state, testing: true)] {
                XCTAssertFalse(p.actionEnabled)
                XCTAssertFalse(p.quickConnectEnabled)
            }
        }
    }
    func testExternalProfileTitlesCannotCreateMultilineOrOversizedMenus() {
        XCTAssertEqual(MenuBarControlPolicy.shortTitle("A\nB\rC"), "A B C")
        let title = MenuBarControlPolicy.shortTitle(String(repeating: "线", count: 80))
        XCTAssertEqual(title.count, 30)
        XCTAssertTrue(title.hasSuffix("…"))
    }
}

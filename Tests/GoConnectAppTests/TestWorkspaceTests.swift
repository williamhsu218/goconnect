import XCTest
import GoConnectCore
@testable import GoConnect

@MainActor
final class TestWorkspaceTests: XCTestCase {
    func testCopyIsAdditivePreservesSourceAndClearsPasswordPreference() throws {
        var source = Configuration()
        source.connections[0].profile.name = "Source"
        source.connections[0].profile.server = "https://vpn.example.invalid"
        source.connections[0].profile.rememberPassword = true
        let original = source
        let empty = Configuration()
        let (first, _) = try AppStore.mergeTestProfiles(source: source, cache: [], into: empty, subscriptions: [])
        XCTAssertEqual(source, original)
        XCTAssertEqual(first.activeProfileID, source.activeProfileID)
        XCTAssertEqual(first.connections.count, 1)
        XCTAssertFalse(first.connections[0].profile.rememberPassword)
        var edited = first
        edited.connections[0].profile.name = "Test edit"
        let (again, _) = try AppStore.mergeTestProfiles(source: source, cache: [], into: edited, subscriptions: [])
        XCTAssertEqual(again.connections[0].profile.name, "Test edit")
        XCTAssertEqual(again.connections.count, 1)
    }
    func testCopyDoesNotOverwriteExistingSubscriptionCredentials() throws {
        let id = UUID()
        let current = Subscription(id: id, payload: .init(name: "Test", url: "", nodes: [], updated: 1, userInfo: ""))
        let source = Subscription(id: id, payload: .init(name: "Source", url: "", nodes: [], updated: 2, userInfo: ""))
        let (_, result) = try AppStore.mergeTestProfiles(source: Configuration(), cache: [source], into: Configuration(), subscriptions: [current])
        XCTAssertEqual(result.count, 1)
        XCTAssertEqual(result[0].payload.name, "Test")
    }
}

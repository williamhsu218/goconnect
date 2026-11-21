import XCTest
import GoConnectCore
@testable import GoConnect

@MainActor
final class WorkspaceRoutingTests: XCTestCase {
    private func makeStore() -> AppStore {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        addTeardownBlock { try? FileManager.default.removeItem(at: directory) }
        return AppStore(dataDirectory: directory, refreshRuntime: false)
    }

    func testDirectRulesAreNormalizedAndPersistOnlyOnTheActiveLine() throws {
        let store = makeStore()
        var other = SavedConnection()
        other.profile.name = "备用线路"
        other.directDomains = [try DomainRule("backup.example")]
        store.configuration.connections.append(other)
        var candidate = try DomainRule("example.com")
        candidate.domain = " Example.COM. "
        let expectedRules = [try DomainRule("example.com"), try DomainRule("203.0.113.24")]
        try store.saveDirectRules([candidate, expectedRules[1]], for: store.configuration.activeProfileID)

        XCTAssertEqual(store.configuration.directDomains, expectedRules)
        XCTAssertEqual(store.configuration.connections.last, other)
        XCTAssertEqual(try store.file.load(), store.configuration)

        store.configuration.routingMode = .global
        let globalRules = [try DomainRule("example.net")]
        try store.saveDirectRules(globalRules, for: store.configuration.activeProfileID)
        XCTAssertEqual(try store.file.load().directDomains, globalRules)
        XCTAssertEqual(try store.file.load().routingMode, .global)
        XCTAssertEqual(try store.file.load().connections.last, other)
    }

    func testDirectRuleGuardsAndInvalidInputLeaveConfigurationUnchanged() throws {
        let store = makeStore()
        let previous = store.configuration
        try store.file.save(previous)
        let id = previous.activeProfileID
        let rule = try DomainRule("example.com")
        var duplicate = rule
        duplicate.domain = "*.EXAMPLE.COM."
        XCTAssertThrowsError(try DomainRule.validated([rule, duplicate]))
        XCTAssertThrowsError(try DomainRule.validated(Array(repeating: rule, count: 257)))
        XCTAssertThrowsError(try store.saveDirectRules([rule], for: UUID()))
        XCTAssertThrowsError(try store.saveDirectRules([rule, duplicate], for: id))

        store.configurationReadable = false
        XCTAssertTrue(store.directRulesReadOnlyReason?.contains("无法读取") == true)
        XCTAssertThrowsError(try store.saveDirectRules([rule], for: id))
        store.configurationReadable = true

        store.updateOperationBlocked = true
        XCTAssertTrue(store.directRulesReadOnlyReason?.contains("更新") == true)
        XCTAssertThrowsError(try store.saveDirectRules([rule], for: id))
        store.updateOperationBlocked = false

        store.phase = "connected"
        XCTAssertTrue(store.directRulesReadOnlyReason?.contains("断开") == true)
        XCTAssertThrowsError(try store.saveDirectRules([rule], for: id))

        store.companyConfiguration.enabled = true
        store.companyConfiguration.companyProfileID = id
        XCTAssertTrue(store.directRulesReadOnlyReason?.contains("公司副线路") == true)
        XCTAssertThrowsError(try store.saveDirectRules([rule], for: id))

        XCTAssertEqual(store.configuration, previous)
        XCTAssertEqual(try store.file.load(), previous)
    }

    func testRoutingListsKeepIndependentSearchesAndTypeFilters() {
        let store = makeStore()
        let app = AllowedApplication(name: "Example Browser", bundleID: "example.browser", path: "/Applications/Example Browser.app")
        let cli = AllowedApplication(name: "Example Service", bundleID: "", path: "/usr/bin/example-service")
        store.catalog = [app, cli]
        store.applicationSearch = "BROWSER"
        store.excludedApplicationSearch = "/usr/bin/example"
        store.applicationFilter = .apps
        store.excludedApplicationFilter = .cli

        store.navigate(to: .applications)
        XCTAssertEqual(store.filteredApps(excluded: false), [app])
        store.navigate(to: .exclusions)
        XCTAssertEqual(store.filteredApps(excluded: true), [cli])
        store.navigate(to: .applications)
        XCTAssertEqual(store.applicationFilter, .apps)
        XCTAssertEqual(store.excludedApplicationFilter, .cli)
        XCTAssertEqual(store.filteredApps(excluded: false), [app])

        store.resetApplicationListFilters()
        XCTAssertEqual(store.filteredApps(excluded: false).count, 2)
        XCTAssertEqual(store.filteredApps(excluded: true).count, 2)
        XCTAssertEqual(store.applicationFilter, .all)
        XCTAssertEqual(store.excludedApplicationFilter, .all)
    }

    func testReloadingTheEditorKeepsSaveNoticeAndNewLineDoesNotDiscardEdits() throws {
        let store = makeStore()
        store.configuration.profile.server = "https://vpn.example.com"
        store.configuration.profile.username = "preview"
        let id = store.configuration.activeProfileID
        store.beginEditingProfile(id)
        store.profileDraft?.connection.profile.name = "已编辑线路"
        store.saveProfileDraft()
        XCTAssertNil(store.profileEditorError)
        XCTAssertEqual(store.message, "线路已保存。")

        store.beginEditingProfile(id, preservingMessage: true)
        XCTAssertEqual(store.message, "线路已保存。")
        XCTAssertFalse(store.profileDraftHasChanges)
        store.beginNewProfile()
        XCTAssertTrue(store.profileDraft?.isNew == true)

        store.profileDraft?.connection.profile.name = "未保存草稿"
        let dirtyDraft = store.profileDraft
        store.beginNewProfile()
        XCTAssertEqual(store.profileDraft, dirtyDraft)
    }

    func testDeletingAnotherLineKeepsUnsavedEdits() throws {
        let store = makeStore()
        let other = SavedConnection()
        store.configuration.connections.append(other)
        store.beginEditingProfile(store.configuration.activeProfileID)
        store.profileDraft?.connection.profile.name = "未保存线路名称"
        store.draftPassword = "test-only"
        let draft = store.profileDraft

        store.deleteProfile(other.id)

        XCTAssertEqual(store.profileDraft, draft)
        XCTAssertEqual(store.draftPassword, "test-only")
        XCTAssertTrue(store.profileDraftHasChanges)
        XCTAssertFalse(store.configuration.connections.contains { $0.id == other.id })
        XCTAssertEqual(try store.file.load(), store.configuration)
    }

    func testDeletingTheEditedLineClosesItsDraftOnlyAfterDeletionSucceeds() {
        let store = makeStore()
        let id = store.configuration.activeProfileID
        store.beginEditingProfile(id)
        store.profileDraft?.connection.profile.name = "未保存线路名称"
        let draft = store.profileDraft

        store.deleteProfile(id)
        XCTAssertEqual(store.profileDraft, draft)

        store.configuration.connections.append(SavedConnection())
        store.deleteProfile(id)
        XCTAssertNil(store.profileDraft)
        XCTAssertFalse(store.configuration.connections.contains { $0.id == id })
    }
}

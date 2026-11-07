import XCTest
import GoConnectCore
@testable import GoConnect

@MainActor
final class CompanyFeatureSettingsTests: XCTestCase {
    func testDisablingPreservesCompanyScopeAndProfilesAcrossReload() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = AppStore(dataDirectory: directory, refreshRuntime: false)
        var saved = CompanyConnectionConfiguration()
        saved.enabled = true
        saved.primaryProfileID = UUID(); saved.companyProfileID = UUID()
        saved.networks = ["10.201.0.0/24"]
        saved.domains = ["corp.example.invalid"]
        saved.dnsServers = ["10.201.0.53"]
        try store.companyFile.save(saved)
        store.companyConfiguration = saved
        let originalConfiguration = store.configuration

        try store.disableCompanyConnection()

        saved.enabled = false
        XCTAssertEqual(store.companyConfiguration, saved)
        XCTAssertEqual(try store.companyFile.load(), saved)
        XCTAssertEqual(store.configuration, originalConfiguration)
        let reloaded = AppStore(dataDirectory: directory, refreshRuntime: false)
        XCTAssertEqual(reloaded.companyConfiguration, saved)
        XCTAssertFalse(reloaded.showsCompanyConnection)
    }

    func testCannotHideWhilePrimaryOrCompanyAuthenticationIsActive() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = AppStore(dataDirectory: directory, refreshRuntime: false)
        store.companyConfiguration.enabled = true
        try store.companyFile.save(store.companyConfiguration)
        let original = try Data(contentsOf: store.companyFile.url)

        store.phase = "connected"
        XCTAssertThrowsError(try store.disableCompanyConnection())
        XCTAssertEqual(try Data(contentsOf: store.companyFile.url), original)
        store.phase = "idle"
        store.company.connect(profile: VPNProfile(), primaryReady: true, controllerReserved: true, updateBlocked: false)
        XCTAssertTrue(store.company.passwordRequested)
        XCTAssertThrowsError(try store.disableCompanyConnection())
        XCTAssertTrue(store.showsCompanyConnection)
        XCTAssertEqual(try Data(contentsOf: store.companyFile.url), original)
        store.company.cancelAuthentication()
    }

    func testDraftAndUpdatePreventChangesAndFailedSavePreservesEnabledState() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = AppStore(dataDirectory: directory, refreshRuntime: false)
        store.companyConfiguration.enabled = true
        store.companyDraftOpen = true
        XCTAssertThrowsError(try store.disableCompanyConnection())
        XCTAssertTrue(store.canSaveCompanyConnectionSettings, "The owning company sheet must remain saveable")
        store.companyDraftOpen = false
        store.updateOperationBlocked = true
        XCTAssertThrowsError(try store.disableCompanyConnection())
        store.updateOperationBlocked = false
        try FileManager.default.createDirectory(at: store.companyFile.url, withIntermediateDirectories: true)
        XCTAssertThrowsError(try store.disableCompanyConnection())
        XCTAssertTrue(store.companyConfiguration.enabled, "A failed write cannot hide enabled company routing")
    }
}

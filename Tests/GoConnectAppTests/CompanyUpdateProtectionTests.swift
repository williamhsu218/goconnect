import XCTest
import GoConnectCore
@testable import GoConnect

@MainActor
final class CompanyUpdateProtectionTests: XCTestCase {
    func testCompanySettingsDraftAndReservedProfileProtectUpdatesAndEdits() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = AppStore(dataDirectory: directory, refreshRuntime: false)
        store.companyDraftOpen = true
        XCTAssertTrue(store.updateSafetyState.hasUnsavedDraft)
        XCTAssertNotNil(store.updateSafetyState.preparationBlockingReason)
        store.companyDraftOpen = false
        XCTAssertFalse(store.updateSafetyState.hasUnsavedDraft)
        var company = SavedConnection(); company.profile.name = "Company"
        store.configuration.connections.append(company)
        store.companyConfiguration.enabled = true; store.companyConfiguration.companyProfileID = company.id
        store.phase = "connected"
        store.beginEditingProfile(company.id)
        XCTAssertFalse(store.canSaveProfileDraft, "Pre-armed company gateway/scope cannot be edited under the current controller")
        store.closeProfileEditor()
        let count = store.configuration.connections.count
        store.deleteProfile(company.id)
        XCTAssertEqual(store.configuration.connections.count, count)
        store.phase = "idle"
        store.beginEditingProfile(company.id)
        XCTAssertTrue(store.canSaveProfileDraft)
    }
}

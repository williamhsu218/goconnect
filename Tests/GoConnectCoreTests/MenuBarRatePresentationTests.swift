import XCTest
@testable import GoConnectCore

final class MenuBarRatePresentationTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func display(enabled: Bool = true, state: VPNControlState = .connected,
                         available: Bool = true, age: TimeInterval = 0,
                         download: Double = 1_572_864, upload: Double = 32_768) -> MenuBarRatePresentation {
        MenuBarRatePresentation(enabled: enabled, state: state, available: available,
            sampledAt: now.addingTimeInterval(-age), download: download, upload: upload, now: now)
    }

    func testDisabledHasNoTextOrExpiry() {
        let value = display(enabled: false)
        XCTAssertEqual(value.title, "")
        XCTAssertNil(value.accessibilityDescription)
        XCTAssertNil(value.expiresAt)
    }

    func testFreshConnectedDirectionsAndExpiry() {
        let value = display(age: 2)
        XCTAssertEqual(value.title, "↓ 1.5 MB/s  ↑ 32.0 KB/s")
        XCTAssertEqual(value.accessibilityDescription, "下载1.5 MB/s，上传32.0 KB/s")
        XCTAssertEqual(value.expiresAt, now.addingTimeInterval(3))
        XCTAssertEqual(display(state: .needsRestart).title, value.title)
    }

    func testOnlyActiveSessionCanShowRates() {
        for state in VPNControlState.allCases where state != .connected && state != .needsRestart {
            let value = display(state: state)
            XCTAssertEqual(value.title, "↓ —  ↑ —", state.rawValue)
            XCTAssertNil(value.expiresAt, state.rawValue)
        }
    }

    func testStaleFutureUnavailableAndMissingTimestampAreUnknown() {
        for age in [5.0, 50.0, -0.1, .infinity, -.infinity, .nan] {
            XCTAssertEqual(display(age: age).title, "↓ —  ↑ —")
            XCTAssertNil(display(age: age).expiresAt)
        }
        XCTAssertEqual(display(available: false).title, "↓ —  ↑ —")
        let missing = MenuBarRatePresentation(enabled: true, state: .connected, available: true,
            sampledAt: nil, download: 10, upload: 20, now: now)
        XCTAssertEqual(missing.title, "↓ —  ↑ —")
    }

    func testRealZeroIsDifferentFromMissingAndInvalidDirections() {
        XCTAssertEqual(display(download: 0, upload: 0).title, "↓ 0 B/s  ↑ 0 B/s")
        for invalid in [Double.nan, .infinity, -.infinity, -1] {
            XCTAssertEqual(display(download: invalid, upload: 0).title, "↓ —  ↑ 0 B/s")
        }
        XCTAssertNil(display(download: .nan, upload: .nan).expiresAt)
    }

    func testUnitsAndExtremeRatesStayBounded() {
        XCTAssertEqual(display(download: 1024, upload: 1_073_741_824).title, "↓ 1.0 KB/s  ↑ 1.0 GB/s")
        let extreme = display(download: .greatestFiniteMagnitude, upload: .greatestFiniteMagnitude)
        XCTAssertEqual(extreme.title, "↓ ≥999 GB/s  ↑ ≥999 GB/s")
        XCTAssertLessThanOrEqual(extreme.title.count, 28)
    }

    func testSingleDirectionUsesSelectedSampleAndAccessibility() {
        for (direction, title, description) in [
            (MenuBarRateDirection.download, "↓ 1.5 MB/s", "下载1.5 MB/s"),
            (.upload, "↑ 32.0 KB/s", "上传32.0 KB/s")
        ] {
            let value = MenuBarRatePresentation(enabled: true, direction: direction, state: .connected,
                available: true, sampledAt: now, download: 1_572_864, upload: 32_768, now: now)
            XCTAssertEqual(value.title, title)
            XCTAssertEqual(value.accessibilityDescription, description)
            XCTAssertEqual(value.expiresAt, now.addingTimeInterval(5))
            let stale = MenuBarRatePresentation(enabled: true, direction: direction, state: .connected,
                available: true, sampledAt: now.addingTimeInterval(-5), download: 10, upload: 20, now: now)
            XCTAssertEqual(stale.title, direction == .download ? "↓ —" : "↑ —")
        }
    }

    func testInvalidUnselectedDirectionDoesNotHideValidSelectedData() {
        let value = MenuBarRatePresentation(enabled: true, direction: .upload, state: .connected,
            available: true, sampledAt: now, download: .nan, upload: 0, now: now)
        XCTAssertEqual(value.title, "↑ 0 B/s")
        XCTAssertNotNil(value.expiresAt)
        let unavailable = MenuBarRatePresentation(enabled: true, direction: .download, state: .connected,
            available: true, sampledAt: now, download: .nan, upload: 100, now: now)
        XCTAssertEqual(unavailable.title, "↓ —")
        XCTAssertNil(unavailable.expiresAt)
    }
}

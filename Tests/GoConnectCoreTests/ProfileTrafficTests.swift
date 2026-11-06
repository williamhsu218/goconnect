import XCTest
@testable import GoConnectCore

final class ProfileTrafficTests: XCTestCase {
    private let id = UUID()
    private func date(_ value: String) -> Date { ISO8601DateFormatter().date(from: value)! }

    func testUnknownHoursAreDifferentFromMeasuredZeroAndEmptyTimeline() throws {
        var ledger = try TrafficTimeline(timeZoneIdentifier: "Asia/Shanghai")
        let now = date("2026-11-05T08:30:00+08:00")
        XCTAssertFalse(ledger.totals(profileID: id).hasData)
        ledger.record(profileID: id, name: "A", received: 0, sent: 0, at: now)
        let points = ledger.series(profileID: id, granularity: .hourly, anchor: now)
        XCTAssertEqual(points.count, 24)
        XCTAssertEqual(points.filter { $0.totals.hasData }.count, 1)
        XCTAssertEqual(points[8].totals.total, 0)
        XCTAssertTrue(points[8].totals.hasData)
        XCTAssertFalse(points[9].totals.hasData)
    }

    func testMidnightMonthAndYearBoundariesDoNotDoubleCount() throws {
        var ledger = try TrafficTimeline(timeZoneIdentifier: "Asia/Shanghai")
        let last = date("2025-12-31T23:59:59+08:00"), next = date("2026-01-01T00:00:00+08:00")
        ledger.record(profileID: id, name: "A", received: 100, sent: 20, at: last)
        ledger.record(profileID: id, name: "A", received: 300, sent: 40, at: next)
        XCTAssertEqual(ledger.totals(profileID: id, interval: ledger.interval(granularity: .hourly, anchor: last)).total, 120)
        XCTAssertEqual(ledger.totals(profileID: id, interval: ledger.interval(granularity: .hourly, anchor: next)).total, 340)
        let years = ledger.series(profileID: id, granularity: .yearly, anchor: next)
        XCTAssertEqual(years.map { $0.totals.total }, [120, 340])
        XCTAssertEqual(ledger.totals(profileID: id).total, 460)
    }

    func testLeapMonthAndMonthlyAggregation() throws {
        var ledger = try TrafficTimeline(timeZoneIdentifier: "UTC")
        let leap = date("2024-02-29T12:00:00Z")
        ledger.record(profileID: id, name: "A", received: 512, sent: 512, at: leap)
        XCTAssertEqual(ledger.series(profileID: id, granularity: .daily, anchor: leap).count, 29)
        let months = ledger.series(profileID: id, granularity: .monthly, anchor: leap)
        XCTAssertEqual(months.count, 12); XCTAssertEqual(months[1].totals.total, 1024)
        XCTAssertFalse(months[0].totals.hasData)
    }

    func testYearlySeriesUsesOnlyTheSelectedProfilesHistoryRange() throws {
        var ledger = try TrafficTimeline(timeZoneIdentifier: "UTC")
        let other = UUID(), now = date("2026-11-06T08:00:00Z")
        ledger.record(profileID: other, name: "Older", received: 100, sent: 10,
                      at: date("2020-01-01T12:00:00Z"))
        ledger.record(profileID: id, name: "Current", received: 200, sent: 20, at: now)
        let selected = ledger.series(profileID: id, granularity: .yearly, anchor: now)
        XCTAssertEqual(selected.count, 1)
        XCTAssertEqual(selected.first?.totals.total, 220)
        XCTAssertEqual(ledger.calendar.component(.year, from: try XCTUnwrap(selected.first?.start)), 2026)
        XCTAssertEqual(ledger.series(profileID: other, granularity: .yearly, anchor: now).count, 7)
    }

    func testDSTDaysUseRealHourBoundaries() throws {
        var ledger = try TrafficTimeline(timeZoneIdentifier: "America/New_York")
        let spring = date("2024-03-10T12:00:00-04:00"), fall = date("2024-11-03T12:00:00-05:00")
        ledger.record(profileID: id, name: "A", received: 10, sent: 1, at: date("2024-11-03T01:30:00-04:00"))
        ledger.record(profileID: id, name: "A", received: 20, sent: 2, at: date("2024-11-03T01:30:00-05:00"))
        XCTAssertEqual(ledger.series(profileID: id, granularity: .hourly, anchor: spring).count, 23)
        let hours = ledger.series(profileID: id, granularity: .hourly, anchor: fall)
        XCTAssertEqual(hours.count, 25)
        XCTAssertEqual(hours.filter { $0.totals.hasData }.count, 2)
        XCTAssertEqual(hours.reduce(UInt64(0)) { $0 + $1.totals.total }, 33)
    }

    func testHalfHourTimeZoneAndClockMovingBackKeepStableBuckets() throws {
        var ledger = try TrafficTimeline(timeZoneIdentifier: "Asia/Kolkata")
        let later = date("2026-11-05T10:15:00+05:30"), earlier = date("2026-11-05T09:15:00+05:30")
        ledger.record(profileID: id, name: "A", received: 10, sent: 0, at: later)
        ledger.record(profileID: id, name: "A", received: 20, sent: 0, at: earlier)
        XCTAssertEqual(ledger.profiles.first?.firstRecordedAt, earlier)
        XCTAssertEqual(ledger.profiles.first?.lastUpdatedAt, later)
        XCTAssertEqual(ledger.series(profileID: id, granularity: .hourly, anchor: later).filter { $0.totals.hasData }.count, 2)
    }

    func testRenamingPreservesIdentityAndDuplicateProfileStartsEmpty() throws {
        var ledger = try TrafficTimeline(timeZoneIdentifier: "UTC")
        let now = date("2026-11-05T12:00:00Z"), other = UUID()
        ledger.record(profileID: id, name: "Old", received: 10, sent: 0, at: now)
        ledger.record(profileID: id, name: "New", received: 20, sent: 0, at: now)
        XCTAssertEqual(ledger.profiles.count, 1); XCTAssertEqual(ledger.profiles[0].name, "New")
        XCTAssertEqual(ledger.totals(profileID: id).received, 30)
        XCTAssertFalse(ledger.totals(profileID: other).hasData)
    }

    func testCounterOverflowSaturatesWithAnExplicitFlag() throws {
        var total = TrafficTotals(received: UInt64.max, sent: 0, samples: 1)
        total.merge(TrafficTotals(received: 1, sent: 10, samples: 1))
        XCTAssertEqual(total.received, .max); XCTAssertEqual(total.total, .max)
        XCTAssertTrue(total.overflowed)
    }

    func testDuplicateOrUnalignedPersistedHoursAreRejected() throws {
        let now = date("2026-11-05T12:00:00Z")
        let profile = TrafficProfile(id: id, name: "A", firstRecordedAt: now, lastUpdatedAt: now)
        let record = TrafficHourRecord(profileID: id, start: now, totals: TrafficTotals(received: 1, samples: 1))
        XCTAssertThrowsError(try TrafficTimeline(timeZoneIdentifier: "UTC", profiles: [profile], records: [record, record]))
        let unaligned = TrafficHourRecord(profileID: id, start: now.addingTimeInterval(1), totals: record.totals)
        XCTAssertThrowsError(try TrafficTimeline(timeZoneIdentifier: "UTC", profiles: [profile], records: [unaligned]))
        XCTAssertThrowsError(try TrafficTimeline(timeZoneIdentifier: "invalid-zone"))
    }
}

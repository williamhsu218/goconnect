import Foundation

public enum TrafficGranularity: String, CaseIterable, Identifiable, Sendable {
    case hourly, daily, monthly, yearly
    public var id: String { rawValue }
    public var title: String {
        switch self { case .hourly: "分时"; case .daily: "日"; case .monthly: "月"; case .yearly: "年" }
    }
}

public struct TrafficTotals: Equatable, Sendable {
    public var received: UInt64
    public var sent: UInt64
    public var samples: Int
    public var overflowed: Bool
    public var hasData: Bool { samples > 0 }
    public var total: UInt64 { Self.add(received, sent).value }
    public init(received: UInt64 = 0, sent: UInt64 = 0, samples: Int = 0, overflowed: Bool = false) {
        self.received = received; self.sent = sent; self.samples = samples
        self.overflowed = overflowed || Self.add(received, sent).overflow
    }
    public mutating func merge(_ other: Self) {
        let rx = Self.add(received, other.received), tx = Self.add(sent, other.sent)
        let count = samples.addingReportingOverflow(other.samples)
        received = rx.value; sent = tx.value; samples = count.overflow ? Int.max : count.partialValue
        overflowed = overflowed || other.overflowed || rx.overflow || tx.overflow || Self.add(received, sent).overflow
    }
    private static func add(_ lhs: UInt64, _ rhs: UInt64) -> (value: UInt64, overflow: Bool) {
        let sum = lhs.addingReportingOverflow(rhs)
        return (sum.overflow ? .max : sum.partialValue, sum.overflow)
    }
}

public struct TrafficProfile: Identifiable, Equatable, Sendable {
    public var id: UUID
    public var name: String
    public var firstRecordedAt: Date
    public var lastUpdatedAt: Date
    public init(id: UUID, name: String, firstRecordedAt: Date, lastUpdatedAt: Date) {
        self.id = id; self.name = name; self.firstRecordedAt = firstRecordedAt; self.lastUpdatedAt = lastUpdatedAt
    }
}

public struct TrafficHourRecord: Sendable {
    public var profileID: UUID
    public var start: Date
    public var totals: TrafficTotals
    public init(profileID: UUID, start: Date, totals: TrafficTotals) {
        self.profileID = profileID; self.start = start; self.totals = totals
    }
}

public struct TrafficSeriesPoint: Identifiable, Sendable {
    public var start: Date
    public var totals: TrafficTotals
    public var id: Date { start }
}

/// Hour boundaries use one persisted time zone, so travel or a system setting change
/// cannot silently move historic bytes to a different day. Values are observed local
/// counters, attributed to the reporting hour; no traffic is invented for sample gaps.
public struct TrafficTimeline: Sendable {
    public let timeZoneIdentifier: String
    public private(set) var profiles: [TrafficProfile]
    private var hours: [UUID: [Date: TrafficTotals]] = [:]
    public var calendar: Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: timeZoneIdentifier)!
        return calendar
    }
    public init(timeZoneIdentifier: String = TimeZone.current.identifier,
                profiles: [TrafficProfile] = [], records: [TrafficHourRecord] = []) throws {
        guard TimeZone(identifier: timeZoneIdentifier) != nil,
              Set(profiles.map(\.id)).count == profiles.count,
              profiles.allSatisfy({ !$0.name.isEmpty && $0.firstRecordedAt.timeIntervalSince1970.isFinite && $0.lastUpdatedAt.timeIntervalSince1970.isFinite && $0.lastUpdatedAt >= $0.firstRecordedAt }) else {
            throw TrafficHistoryError.invalidData
        }
        self.timeZoneIdentifier = timeZoneIdentifier; self.profiles = profiles
        guard profiles.allSatisfy({ calendar.dateInterval(of: .year, for: $0.firstRecordedAt) != nil && calendar.dateInterval(of: .year, for: $0.lastUpdatedAt) != nil }) else { throw TrafficHistoryError.invalidData }
        let ids = Set(profiles.map(\.id))
        for record in records {
            guard ids.contains(record.profileID), record.totals.samples > 0,
                  record.start.timeIntervalSince1970.isFinite,
                  calendar.dateInterval(of: .hour, for: record.start)?.start == record.start,
                  hours[record.profileID]?[record.start] == nil else { throw TrafficHistoryError.invalidData }
            hours[record.profileID, default: [:]][record.start] = record.totals
        }
    }
    @discardableResult
    public mutating func record(profileID: UUID, name: String, received: UInt64, sent: UInt64, at date: Date) -> TrafficHourRecord? {
        guard date.timeIntervalSince1970.isFinite,
              let start = calendar.dateInterval(of: .hour, for: date)?.start else { return nil }
        if let index = profiles.firstIndex(where: { $0.id == profileID }) {
            profiles[index].name = name.isEmpty ? "未命名线路" : name
            profiles[index].firstRecordedAt = min(profiles[index].firstRecordedAt, date)
            profiles[index].lastUpdatedAt = max(profiles[index].lastUpdatedAt, date)
        } else {
            profiles.append(TrafficProfile(id: profileID, name: name.isEmpty ? "未命名线路" : name, firstRecordedAt: date, lastUpdatedAt: date))
        }
        var total = hours[profileID]?[start] ?? TrafficTotals()
        total.merge(TrafficTotals(received: received, sent: sent, samples: 1))
        hours[profileID, default: [:]][start] = total
        return TrafficHourRecord(profileID: profileID, start: start, totals: total)
    }
    public func totals(profileID: UUID, interval: DateInterval? = nil) -> TrafficTotals {
        var total = TrafficTotals()
        for (date, value) in hours[profileID] ?? [:] {
            if let interval, !(date >= interval.start && date < interval.end) { continue }
            total.merge(value)
        }
        return total
    }
    public func interval(granularity: TrafficGranularity, anchor: Date, profileID: UUID? = nil) -> DateInterval {
        let component: Calendar.Component
        switch granularity { case .hourly: component = .day; case .daily: component = .month; case .monthly, .yearly: component = .year }
        let current = calendar.dateInterval(of: component, for: anchor)!
        guard granularity == .yearly else { return current }
        let selected = profileID.map { id in profiles.filter { $0.id == id } } ?? profiles
        let first = selected.map(\.firstRecordedAt).min() ?? anchor
        let start = calendar.dateInterval(of: .year, for: min(first, anchor))!.start
        let last = max(selected.map(\.lastUpdatedAt).max() ?? anchor, anchor)
        return DateInterval(start: start, end: calendar.dateInterval(of: .year, for: last)!.end)
    }
    public func series(profileID: UUID, granularity: TrafficGranularity, anchor: Date) -> [TrafficSeriesPoint] {
        let range = interval(granularity: granularity, anchor: anchor, profileID: profileID)
        let component: Calendar.Component
        switch granularity { case .hourly: component = .hour; case .daily: component = .day; case .monthly: component = .month; case .yearly: component = .year }
        var values: [Date: TrafficTotals] = [:]
        for (date, value) in hours[profileID] ?? [:] where date >= range.start && date < range.end {
            guard let start = calendar.dateInterval(of: component, for: date)?.start else { continue }
            var total = values[start] ?? TrafficTotals(); total.merge(value); values[start] = total
        }
        var points: [TrafficSeriesPoint] = [], cursor = range.start
        while cursor < range.end {
            points.append(TrafficSeriesPoint(start: cursor, totals: values[cursor] ?? TrafficTotals()))
            guard let next = calendar.date(byAdding: component, value: 1, to: cursor), next > cursor else { break }
            cursor = next
        }
        return points
    }
}

public enum TrafficHistoryError: Error { case invalidData, unavailable }

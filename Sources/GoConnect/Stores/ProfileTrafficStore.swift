import Foundation
import Observation
import GoConnectCore

@MainActor @Observable
final class ProfileTrafficStore {
    private(set) var revision = 0
    private(set) var readable = true
    private(set) var storageError: String?
    @ObservationIgnored private var timeline: TrafficTimeline
    @ObservationIgnored private var database: TrafficHistoryDatabase?
    private struct HourKey: Hashable { let id: UUID; let start: Date }
    private struct Session { let id: UUID; let name: String; var received: UInt64 = 0; var sent: UInt64 = 0 }
    @ObservationIgnored private var pendingProfiles: [UUID: TrafficProfile] = [:]
    @ObservationIgnored private var pendingHours: [HourKey: TrafficHourRecord] = [:]
    @ObservationIgnored private var session: Session?
    @ObservationIgnored private var flushTask: Task<Void, Never>?
    @ObservationIgnored private let flushInterval: TimeInterval

    var profiles: [TrafficProfile] { _ = revision; return timeline.profiles }
    var calendar: Calendar { timeline.calendar }
    var timeZone: TimeZone { calendar.timeZone }

    init(file: URL, timeZone: TimeZone = .current, flushInterval: TimeInterval = 5) {
        self.flushInterval = flushInterval.isFinite ? max(0.01, min(flushInterval, 60)) : 5
        timeline = try! TrafficTimeline(timeZoneIdentifier: timeZone.identifier)
        do {
            let database = try TrafficHistoryDatabase(file: file, timeZone: timeZone)
            timeline = try database.load(); self.database = database
        } catch {
            readable = false
            storageError = "流量历史无法读取，原文件已保留；本次不写入新的历史记录。"
        }
    }
    deinit { flushTask?.cancel() }
    func beginSession(profileID: UUID, name: String) {
        endSession()
        session = Session(id: profileID, name: name)
    }
    func consume(received: UInt64, sent: UInt64, at date: Date = Date()) {
        guard var current = session, date.timeIntervalSince1970.isFinite else { return }
        // Both counters belong to the same core. After a reset one direction
        // may already exceed its old value by the time the first sample arrives.
        let reset = received < current.received || sent < current.sent
        let rx = reset ? received : received - current.received
        let tx = reset ? sent : sent - current.sent
        current.received = received; current.sent = sent; session = current
        recordSample(profileID: current.id, name: current.name, received: rx, sent: tx, at: date)
    }
    /// Used both for real counter deltas and synthetic fixtures in the isolated app.
    func recordSample(profileID: UUID, name: String, received: UInt64, sent: UInt64, at date: Date) {
        guard readable, let hour = timeline.record(profileID: profileID, name: name, received: received, sent: sent, at: date),
              let profile = timeline.profiles.first(where: { $0.id == profileID }) else { return }
        pendingProfiles[profileID] = profile; pendingHours[HourKey(id: profileID, start: hour.start)] = hour
        revision += 1
        scheduleFlush()
    }
    func endSession() { session = nil; flush() }
    @discardableResult
    func flush() -> Bool {
        flushTask?.cancel(); flushTask = nil
        guard readable, let database else { return false }
        guard !pendingHours.isEmpty else { return true }
        do {
            try database.save(profiles: Array(pendingProfiles.values), hours: Array(pendingHours.values))
            pendingProfiles.removeAll(); pendingHours.removeAll(); storageError = nil
            return true
        } catch {
            storageError = "部分流量统计尚未保存，当前记录保留在内存中，将自动重试。"
            scheduleFlush()
            return false
        }
    }
    private func scheduleFlush() {
        guard flushTask == nil, readable, !pendingHours.isEmpty else { return }
        let delay = flushInterval
        flushTask = Task { [weak self] in
            do { try await Task.sleep(for: .seconds(delay)) } catch { return }
            guard !Task.isCancelled, let self else { return }
            self.flushTask = nil
            self.flush()
        }
    }
    func totals(profileID: UUID, interval: DateInterval? = nil) -> TrafficTotals {
        _ = revision; return timeline.totals(profileID: profileID, interval: interval)
    }
    func interval(granularity: TrafficGranularity, anchor: Date, profileID: UUID? = nil) -> DateInterval {
        _ = revision; return timeline.interval(granularity: granularity, anchor: anchor, profileID: profileID)
    }
    func series(profileID: UUID, granularity: TrafficGranularity, anchor: Date) -> [TrafficSeriesPoint] {
        _ = revision; return timeline.series(profileID: profileID, granularity: granularity, anchor: anchor)
    }
}

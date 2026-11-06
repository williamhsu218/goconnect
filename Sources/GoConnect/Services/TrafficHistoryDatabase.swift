import Foundation
import SQLite3
import Darwin
import GoConnectCore

/// Small, transactional updates to the current hour avoid rewriting years of JSON
/// at every sample. Counters are decimal text to preserve the full UInt64 range.
final class TrafficHistoryDatabase {
    private var handle: OpaquePointer?
    private let transient = unsafeBitCast(-1, to: sqlite3_destructor_type.self)

    init(file: URL, timeZone: TimeZone) throws {
        let manager = FileManager.default
        let existed = manager.fileExists(atPath: file.path)
        if let attributes = try? manager.attributesOfItem(atPath: file.path), attributes[.type] as? FileAttributeType == .typeSymbolicLink {
            throw TrafficHistoryError.invalidData
        }
        if !existed {
            try manager.createDirectory(at: file.deletingLastPathComponent(), withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        }
        let flags = SQLITE_OPEN_READWRITE | SQLITE_OPEN_FULLMUTEX | SQLITE_OPEN_NOFOLLOW | (existed ? 0 : SQLITE_OPEN_CREATE)
        // macOS exposes its temporary directory through /var -> /private/var.
        // Resolve only ancestors; retain NOFOLLOW protection on the database itself.
        guard let parent = realpath(file.deletingLastPathComponent().path, nil) else { throw TrafficHistoryError.unavailable }
        defer { free(parent) }
        // URL.resolvingSymlinksInPath can normalize /private/var back to /var;
        // retain the POSIX realpath spelling for SQLite's NOFOLLOW check.
        let path = String(cString: parent) + "/" + file.lastPathComponent
        guard sqlite3_open_v2(path, &handle, flags, nil) == SQLITE_OK else {
            sqlite3_close(handle); handle = nil; throw TrafficHistoryError.unavailable
        }
        do {
            sqlite3_busy_timeout(handle, 250)
            if !existed {
                try execute("BEGIN IMMEDIATE")
                do {
                    try execute("CREATE TABLE metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL)")
                    try execute("CREATE TABLE profiles (id TEXT PRIMARY KEY, name TEXT NOT NULL, first_recorded REAL NOT NULL, last_updated REAL NOT NULL)")
                    try execute("CREATE TABLE hours (profile_id TEXT NOT NULL REFERENCES profiles(id), start INTEGER NOT NULL, received TEXT NOT NULL, sent TEXT NOT NULL, samples INTEGER NOT NULL, overflowed INTEGER NOT NULL, PRIMARY KEY(profile_id,start))")
                    let statement = try prepare("INSERT INTO metadata VALUES ('time_zone', ?)")
                    defer { sqlite3_finalize(statement) }
                    try bind(timeZone.identifier, to: statement, at: 1); try finish(statement)
                    try execute("PRAGMA user_version=1"); try execute("COMMIT")
                } catch { try? execute("ROLLBACK"); throw error }
            }
            guard try scalar("PRAGMA user_version") == "1", try scalar("PRAGMA quick_check(1)") == "ok" else {
                throw TrafficHistoryError.invalidData
            }
            try execute("PRAGMA foreign_keys=ON")
            try execute("PRAGMA synchronous=FULL")
            try manager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: file.path)
        } catch { sqlite3_close(handle); handle = nil; throw error }
    }
    deinit { sqlite3_close(handle) }

    func load() throws -> TrafficTimeline {
        let zone = try scalar("SELECT value FROM metadata WHERE key='time_zone'")
        var profiles: [TrafficProfile] = [], records: [TrafficHourRecord] = []
        let profileStatement = try prepare("SELECT id,name,first_recorded,last_updated FROM profiles")
        defer { sqlite3_finalize(profileStatement) }
        while true {
            let status = sqlite3_step(profileStatement)
            if status == SQLITE_DONE { break }
            guard status == SQLITE_ROW, let id = UUID(uuidString: text(profileStatement, 0)) else { throw TrafficHistoryError.invalidData }
            profiles.append(TrafficProfile(id: id, name: text(profileStatement, 1), firstRecordedAt: Date(timeIntervalSince1970: sqlite3_column_double(profileStatement, 2)), lastUpdatedAt: Date(timeIntervalSince1970: sqlite3_column_double(profileStatement, 3))))
        }
        let hourStatement = try prepare("SELECT profile_id,start,received,sent,samples,overflowed FROM hours")
        defer { sqlite3_finalize(hourStatement) }
        while true {
            let status = sqlite3_step(hourStatement)
            if status == SQLITE_DONE { break }
            let count = sqlite3_column_int64(hourStatement, 4), overflowed = sqlite3_column_int(hourStatement, 5)
            guard status == SQLITE_ROW, let id = UUID(uuidString: text(hourStatement, 0)),
                  let received = UInt64(text(hourStatement, 2)), let sent = UInt64(text(hourStatement, 3)),
                  count > 0, count <= Int64(Int.max), overflowed == 0 || overflowed == 1 else { throw TrafficHistoryError.invalidData }
            records.append(TrafficHourRecord(profileID: id, start: Date(timeIntervalSince1970: Double(sqlite3_column_int64(hourStatement, 1))), totals: TrafficTotals(received: received, sent: sent, samples: Int(count), overflowed: overflowed == 1)))
        }
        return try TrafficTimeline(timeZoneIdentifier: zone, profiles: profiles, records: records)
    }

    func save(profiles: [TrafficProfile], hours: [TrafficHourRecord]) throws {
        try execute("BEGIN IMMEDIATE")
        do {
            let line = try prepare("INSERT INTO profiles VALUES (?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name, first_recorded=excluded.first_recorded, last_updated=excluded.last_updated")
            defer { sqlite3_finalize(line) }
            for profile in profiles {
                sqlite3_reset(line); sqlite3_clear_bindings(line)
                try bind(profile.id.uuidString, to: line, at: 1); try bind(profile.name, to: line, at: 2)
                try check(sqlite3_bind_double(line, 3, profile.firstRecordedAt.timeIntervalSince1970))
                try check(sqlite3_bind_double(line, 4, profile.lastUpdatedAt.timeIntervalSince1970)); try finish(line)
            }
            let hour = try prepare("INSERT INTO hours VALUES (?,?,?,?,?,?) ON CONFLICT(profile_id,start) DO UPDATE SET received=excluded.received, sent=excluded.sent, samples=excluded.samples, overflowed=excluded.overflowed")
            defer { sqlite3_finalize(hour) }
            for record in hours {
                sqlite3_reset(hour); sqlite3_clear_bindings(hour)
                try bind(record.profileID.uuidString, to: hour, at: 1)
                try check(sqlite3_bind_int64(hour, 2, Int64(record.start.timeIntervalSince1970)))
                try bind(String(record.totals.received), to: hour, at: 3); try bind(String(record.totals.sent), to: hour, at: 4)
                try check(sqlite3_bind_int64(hour, 5, Int64(record.totals.samples)))
                try check(sqlite3_bind_int(hour, 6, record.totals.overflowed ? 1 : 0)); try finish(hour)
            }
            try execute("COMMIT")
        } catch { try? execute("ROLLBACK"); throw error }
    }

    private func prepare(_ sql: String) throws -> OpaquePointer {
        var statement: OpaquePointer?
        guard sqlite3_prepare_v2(handle, sql, -1, &statement, nil) == SQLITE_OK, let statement else { throw TrafficHistoryError.invalidData }
        return statement
    }
    private func execute(_ sql: String) throws { try check(sqlite3_exec(handle, sql, nil, nil, nil)) }
    private func check(_ code: Int32) throws { guard code == SQLITE_OK else { throw TrafficHistoryError.unavailable } }
    private func bind(_ value: String, to statement: OpaquePointer, at index: Int32) throws {
        try check(sqlite3_bind_text(statement, index, value, -1, transient))
    }
    private func finish(_ statement: OpaquePointer) throws {
        guard sqlite3_step(statement) == SQLITE_DONE else { throw TrafficHistoryError.unavailable }
    }
    private func text(_ statement: OpaquePointer, _ index: Int32) -> String {
        guard let value = sqlite3_column_text(statement, index) else { return "" }
        return String(cString: value)
    }
    private func scalar(_ sql: String) throws -> String {
        let statement = try prepare(sql); defer { sqlite3_finalize(statement) }
        guard sqlite3_step(statement) == SQLITE_ROW else { throw TrafficHistoryError.invalidData }
        return text(statement, 0)
    }
}

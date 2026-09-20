import Foundation
import OSLog
import Darwin

public enum FaultEvent: String, Sendable {
    case proxyCheckStarted = "proxy.check_started", proxyCheckSucceeded = "proxy.check_succeeded", proxyCheckFailed = "proxy.check_failed"
    case appStarted = "app.started", appStopping = "app.stopping"
    case heartbeat = "app.heartbeat", mainStalled = "main.stalled", mainRecovered = "main.recovered"
    case loadAverage100 = "system.load_average_1m_x100", memoryPressure = "system.memory_pressure"
    case schedulerGap = "scheduler.gap", sleep = "system.sleep", wake = "system.wake"
    case pathSatisfied = "network.satisfied", pathUnsatisfied = "network.unsatisfied", pathRequiresConnection = "network.requires_connection"
    case transportStarted = "transport.started", transportExited = "transport.exited", transportError = "transport.error"
    case idle = "phase.idle", connecting = "phase.connecting", connected = "phase.connected", failed = "phase.failed"
    case stopping = "phase.stopping", protected = "phase.protected", authorizing = "phase.authorizing", testing = "phase.testing"
    case preparingService = "phase.preparingService", preparingCheckService = "phase.preparingCheckService"
    case checking = "phase.checking", serviceMaintenance = "phase.serviceMaintenance"
}

// Background-only bounded disk I/O. No strings from configuration, errors or core output.
public final class FaultDiagnostics: @unchecked Sendable {
    public static let shared = FaultDiagnostics()
    public static var directory: URL { FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Library/Logs/GoConnect", isDirectory: true) }
    private let queue = DispatchQueue(label: "com.willhsu.GoConnect.diagnostics", qos: .utility)
    private let capacity = DispatchSemaphore(value: 128)
    private let logger = Logger(subsystem: "com.willhsu.GoConnect", category: "FaultDiagnostics")
    private let directory: URL
    private let limit: Int
    private var lockFD: Int32 = -1
    deinit { if lockFD >= 0 { Darwin.close(lockFD) } }
    public init(directory: URL = FaultDiagnostics.directory, limit: Int = 1_048_576) { self.directory = directory; self.limit = limit }

    public func record(_ event: FaultEvent, value: Int = 0) {
        guard capacity.wait(timeout: .now()) == .success else { return }
        let timestamp = Date()
        queue.async { [self] in
            defer { capacity.signal() }
            logger.notice("\(event.rawValue, privacy: .public) value=\(value, privacy: .public)")
            struct Entry: Encodable { let time: Date; let pid: Int32; let event: String; let value: Int }
            let encoder = JSONEncoder(); encoder.dateEncodingStrategy = .iso8601; encoder.outputFormatting = .sortedKeys
            do {
                let data = try encoder.encode(Entry(time: timestamp, pid: getpid(), event: event.rawValue, value: value)) + Data([10])
                try append(data)
            } catch { logger.error("diagnostic file write failed") }
        }
    }
    private func prepareDirectory() throws {
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        var st = stat()
        guard lstat(directory.path, &st) == 0, st.st_mode & S_IFMT == S_IFDIR, st.st_uid == getuid(), st.st_mode & 0o077 == 0 else { throw CocoaError(.fileWriteNoPermission) }
    }
    private func openFile(_ url: URL, flags: Int32) throws -> Int32 {
        let fd = Darwin.open(url.path, flags | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC, 0o600)
        guard fd >= 0 else { throw CocoaError(.fileReadNoPermission) }
        var st = stat()
        guard fstat(fd, &st) == 0, st.st_mode & S_IFMT == S_IFREG, st.st_nlink == 1, st.st_uid == getuid(), st.st_mode & 0o077 == 0 else {
            Darwin.close(fd); throw CocoaError(.fileReadNoPermission)
        }
        return fd
    }
    private func append(_ data: Data) throws {
        try prepareDirectory()
        if lockFD < 0 {
            let fd = try openFile(directory.appendingPathComponent("app.lock"), flags: O_CREAT | O_RDWR)
            guard flock(fd, LOCK_EX | LOCK_NB) == 0 else { Darwin.close(fd); throw CocoaError(.fileLocking) }
            lockFD = fd
        }
        guard data.count <= limit else { throw CocoaError(.fileWriteOutOfSpace) }
        let path = directory.appendingPathComponent("app.jsonl")
        var fd = try openFile(path, flags: O_CREAT | O_WRONLY | O_APPEND)
        defer { Darwin.close(fd) }
        var st = stat(); guard fstat(fd, &st) == 0 else { throw CocoaError(.fileReadUnknown) }
        if st.st_size + Int64(data.count) > limit {
            guard rename(path.path, path.path + ".1") == 0 else { throw CocoaError(.fileWriteUnknown) }
            Darwin.close(fd); fd = -1
            fd = try openFile(path, flags: O_CREAT | O_EXCL | O_WRONLY)
        }
        try data.withUnsafeBytes { buffer in
            var written = 0
            while written < data.count {
                let count = Darwin.write(fd, buffer.baseAddress!.advanced(by: written), data.count - written)
                if count < 0 && errno == EINTR { continue }
                guard count > 0 else { throw CocoaError(.fileWriteUnknown) }
                written += count
            }
        }
        guard fsync(fd) == 0 else { throw CocoaError(.fileWriteUnknown) }
    }
    public func export() async -> String {
        await withCheckedContinuation { continuation in
            queue.async { [self] in
                var result = ""
                do {
                    try prepareDirectory()
                    for name in ["app.jsonl.1", "app.jsonl"] {
                        result += "\n--- \(name) ---\n"
                        guard let fd = try? openFile(directory.appendingPathComponent(name), flags: O_RDONLY) else { result += "unavailable\n"; continue }
                        let handle = FileHandle(fileDescriptor: fd, closeOnDealloc: true)
                        let size = (try? handle.seekToEnd()) ?? 0
                        try? handle.seek(toOffset: size > 131072 ? size - 131072 : 0)
                        var data = (try? handle.read(upToCount: 131072)) ?? Data()
                        if size > 131072, let newline = data.firstIndex(of: 10) { data = data.suffix(from: data.index(after: newline)) }
                        result += String(decoding: data, as: UTF8.self)
                        try? handle.close()
                    }
                } catch { result += "App diagnostic files unavailable\n" }
                continuation.resume(returning: result)
            }
        }
    }
}

// One outstanding main-queue probe. Never enqueue a growing backlog during a hang.
// A late background tick is classified separately: sleep/system-wide stalls are
// not evidence that the application's main thread alone was blocked.
public struct ResponsivenessState: Sendable {
    private var pendingSince: TimeInterval?
    private var lastTick: TimeInterval?
    private var reported = false
    public init() {}
    public mutating func reset() { pendingSince = nil; lastTick = nil; reported = false }
    public mutating func tick(now: TimeInterval) -> (enqueue: Bool, event: FaultEvent?, seconds: Int) {
        if let lastTick, now - lastTick > 10 {
            let gap = Int(now - lastTick); let enqueue = pendingSince == nil
            reset(); self.lastTick = now
            // Keep the original probe pending until it runs, even across sleep.
            pendingSince = now
            return (enqueue, .schedulerGap, gap)
        }
        lastTick = now
        guard let pendingSince else { self.pendingSince = now; return (true, nil, 0) }
        let lag = Int(now - pendingSince)
        if lag >= 5 && !reported { reported = true; return (false, .mainStalled, lag) }
        return (false, nil, lag)
    }
    public mutating func resume(now: TimeInterval) {
        if pendingSince != nil { pendingSince = now }; lastTick = now; reported = false
    }
    public mutating func acknowledge(now: TimeInterval) -> Int? {
        let lag = reported ? Int(now - (pendingSince ?? now)) : nil
        pendingSince = nil; reported = false
        return lag
    }
}

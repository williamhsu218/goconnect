import CryptoKit
import Darwin
import Foundation

public enum ApplicationLaunchReadiness {
    public struct ProcessIdentity {
        public let uid: UInt32
        public let gid: UInt32
        public let path: String
    }

    public static func parseProcesses(_ text: String) -> [ProcessIdentity] {
        text.split(separator: "\n").compactMap { line in
            let parts = line.split(maxSplits: 3, omittingEmptySubsequences: true, whereSeparator: { $0 == " " || $0 == "\t" })
            guard parts.count == 4, let uid = UInt32(parts[0]), let gid = UInt32(parts[1]), Int(parts[2]) != nil else { return nil }
            return ProcessIdentity(uid: uid, gid: gid, path: String(parts[3]))
        }
    }

    public static func groupName(uid: UInt32, path: String) -> String {
        let hash = SHA256.hash(data: Data(path.utf8)).prefix(8).map { String(format: "%02x", $0) }.joined()
        return "goc_\(uid)_\(hash)"
    }

    public static func requiringRestart(_ apps: [AllowedApplication], uid: UInt32,
                                        processes: [ProcessIdentity], groups: [String: UInt32]) -> [AllowedApplication] {
        apps.filter { app in
            let expected = groups[groupName(uid: uid, path: app.path)]
            return processes.contains { $0.uid == uid && $0.path.hasPrefix(app.path + "/Contents/") && $0.gid != expected }
        }
    }

    /// Read-only preflight. The root worker still checks again to handle launch races.
    public static func requiringRestart(_ apps: [AllowedApplication]) throws -> [AllowedApplication] {
        guard !apps.isEmpty else { return [] }
        let process = Process(), output = Pipe()
        process.executableURL = URL(fileURLWithPath: "/bin/ps")
        process.arguments = ["-axo", "uid=,gid=,pid=,comm="]
        process.standardOutput = output; process.standardError = FileHandle.nullDevice
        try process.run()
        let data = output.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        guard process.terminationStatus == 0, !data.isEmpty else { throw ReadError() }
        let uid = getuid()
        var groups: [String: UInt32] = [:]
        for app in apps {
            let name = groupName(uid: uid, path: app.path)
            if let group = getgrnam(name) { groups[name] = group.pointee.gr_gid }
        }
        return requiringRestart(apps, uid: uid, processes: parseProcesses(String(decoding: data, as: UTF8.self)), groups: groups)
    }

    private struct ReadError: LocalizedError {
        var errorDescription: String? { "无法检查应用运行状态，请稍后重试。" }
    }
}

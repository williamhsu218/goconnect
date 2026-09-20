import Foundation
import GoConnectCore

@MainActor
final class RouteClient {
    private var directory: URL?
    private var serviceProcess: Process?
    private var heartbeat: Timer?
    private var stopping = false
    private var lastState = ""
    private var commandSequence: UInt64 = 0
    var onState: ((String) -> Void)?
    var onSnapshot: ((RouteSnapshot) -> Void)?
    var isRunning: Bool { directory != nil }

    func updateApplications(_ apps: [AllowedApplication]) throws -> UInt64 {
        guard let directory, !stopping, lastState == "ready" else { throw CocoaError(.executableNotLoadable) }
        try ApplicationRoutingSupport.validate(apps, requiresLauncher: false)
        commandSequence += 1
        struct Command: Encodable { let sequence: UInt64; let appPaths: [String]; let action = "updateApps" }
        let url = directory.appendingPathComponent("command.json")
        let data = try JSONEncoder().encode(Command(sequence: commandSequence, appPaths: apps.map(\.path)))
        guard apps.count <= 256, data.count <= 65536 else { throw CocoaError(.fileWriteOutOfSpace) }
        try data.write(to: url, options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
        return commandSequence
    }

    func start(port: UInt16, token: String, apps: [AllowedApplication], gateway: String, runtime: URL, mode: RoutingMode, directDomains: [DomainRule] = [], remoteNetworks: [String] = [], remoteExcluded: [String] = []) throws {
        try ApplicationRoutingSupport.validate(apps, requiresLauncher: false)
        guard directory == nil else { throw CocoaError(.executableLoad) }
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("GoConnect-" + UUID().uuidString)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
        directory = dir; stopping = false; lastState = ""; commandSequence = 0
        do {
            var session = RouteSession(ownerPID: ProcessInfo.processInfo.processIdentifier, controlDirectory: dir.path,
                                       token: token, socksPort: port, appPaths: apps.map(\.path), gateway: gateway, routingMode: mode, directDomains: directDomains)
            session.remoteNetworks = try RemoteNetwork.validated(remoteNetworks)
            session.remoteExcluded = try RemoteNetwork.validated(remoteExcluded)
            let url = dir.appendingPathComponent("session.json")
            try JSONEncoder().encode(session).write(to: url, options: .atomic)
            try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
            let lease = dir.appendingPathComponent("lease")
            try Data(token.utf8).write(to: lease, options: .atomic)
            try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: lease.path)
            let p = Process(); p.executableURL = runtime.appendingPathComponent("bin/GoConnectTransport")
            p.arguments = ["service-route", url.path]
            p.environment = ["PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "LANG": "en_US.UTF-8"]
            p.standardOutput = FileHandle.nullDevice; p.standardError = FileHandle.nullDevice
            p.terminationHandler = { [weak self] _ in Task { @MainActor in self?.tick() } }
            try p.run(); serviceProcess = p
            let timer = Timer(timeInterval: 1, repeats: true) { [weak self] _ in
                Task { @MainActor in self?.tick() }
            }
            RunLoop.main.add(timer, forMode: .common)
            heartbeat = timer
        } catch { cleanup(); throw error }
    }
    func startCompany(port: UInt16, gateway: String, addresses: [String], networks: [String], token: String, runtime: URL) throws {
        guard directory == nil else { throw CocoaError(.executableLoad) }
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("GoConnect-" + UUID().uuidString)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
        directory = dir; stopping = false; lastState = ""
        struct Session: Encodable { let ownerPID: Int32; let controlDirectory: String; let token: String; let companyShared = true; let socksPort: UInt16; let gateway: String; let vpnAddresses: [String]; let remoteNetworks: [String] }
        do {
            let r = Session(ownerPID: ProcessInfo.processInfo.processIdentifier, controlDirectory: dir.path, token: token, socksPort: port, gateway: gateway, vpnAddresses: addresses, remoteNetworks: try RemoteNetwork.validated(networks))
            let file = dir.appendingPathComponent("session.json")
            try JSONEncoder().encode(r).write(to: file, options: .atomic)
            try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: file.path)
            let lease = dir.appendingPathComponent("lease")
            try Data(token.utf8).write(to: lease, options: .atomic)
            try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: lease.path)
            let p = Process(); p.executableURL = runtime.appendingPathComponent("bin/GoConnectTransport")
            p.arguments = ["service-route", file.path]
            p.standardOutput = FileHandle.nullDevice; p.standardError = FileHandle.nullDevice
            p.environment = ["PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "LANG": "en_US.UTF-8"]
            try p.run(); serviceProcess = p
            let timer = Timer(timeInterval: 1, repeats: true) { [weak self] _ in Task { @MainActor in self?.tick() } }
            RunLoop.main.add(timer, forMode: .common); heartbeat = timer
        } catch { cleanup(); throw error }
    }
    private func tick() {
        guard let dir = directory else { return }
        if !stopping {
            try? FileManager.default.setAttributes([.modificationDate: Date()], ofItemAtPath: dir.appendingPathComponent("lease").path)
        }
        let snapshot = (try? Data(contentsOf: dir.appendingPathComponent("status"))).flatMap { try? JSONDecoder().decode(RouteSnapshot.self, from: $0) }
        if let snapshot { onSnapshot?(snapshot) }
        let state = snapshot?.state ?? ""
        if state == "stopped" || state == "failed" {
            let outcome = stopping ? "stopped" : state
            cleanup(); onState?(outcome); return
        }
        if serviceProcess?.isRunning == false {
            let outcome = stopping ? "stopped" : "serviceFailed"
            // Revoke the supervisor lease even if its last snapshot still says ready.
            try? FileManager.default.removeItem(at: dir.appendingPathComponent("lease"))
            cleanup(); onState?(outcome); return
        }
        if state == "ready", !stopping, state != lastState { lastState = state; onState?(state) }
    }
    func stop() {
        guard let dir = directory else { return }
        stopping = true
        try? FileManager.default.removeItem(at: dir.appendingPathComponent("lease"))
        // A started worker exits via its lease; a pending client can close its socket.
        let supervisorStarted = FileManager.default.fileExists(atPath: dir.appendingPathComponent("status").path)
        if !supervisorStarted, serviceProcess?.isRunning == true { serviceProcess?.terminate() }
        tick()
    }
    private func cleanup() {
        heartbeat?.invalidate(); heartbeat = nil
        if let directory { try? FileManager.default.removeItem(at: directory) }
        directory = nil; serviceProcess = nil; lastState = ""
    }
}

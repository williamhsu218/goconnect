import Foundation
import GoConnectCore

@MainActor
final class RouteClient {
    private var directory: URL?
    private var serviceProcess: Process?
    private var heartbeat: Timer?
    private var leaseHeartbeat: RouteLeaseHeartbeat?
    private var stopping = false
    private var lastState = ""
    private let temporaryDirectory: URL
    init(temporaryDirectory: URL = FileManager.default.temporaryDirectory) {
        self.temporaryDirectory = temporaryDirectory
    }
    private(set) var companyState: CompanyRouteState?
    private var pendingCompanyBlock: String?
    private var pendingCompanyCommit: String?
    var companySocketPath: String? { directory?.appendingPathComponent("company.sock").path }
    var onState: ((String) -> Void)?
    var onSnapshot: ((RouteSnapshot) -> Void)?
    var isRunning: Bool { directory != nil }

    func start(port: UInt16, token: String, apps: [AllowedApplication], gateway: String, runtime: URL, mode: RoutingMode, directDomains: [DomainRule] = [], remoteNetworks: [String] = [], remoteExcluded: [String] = [], company: CompanyReservation? = nil) throws {
        let paths = try ApplicationRoutingSupport.routingPaths(for: apps)
        guard directory == nil else { throw CocoaError(.executableLoad) }
        let parent = company == nil ? temporaryDirectory : URL(fileURLWithPath: "/private/tmp")
        let dir = parent.appendingPathComponent("gcc-" + UUID().uuidString.prefix(8))
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
        directory = dir; stopping = false; lastState = ""
        do {
            var session = RouteSession(ownerPID: ProcessInfo.processInfo.processIdentifier, controlDirectory: dir.path,
                                       token: token, socksPort: port, appPaths: paths, gateway: gateway, routingMode: mode, directDomains: directDomains)
            session.company = company
            session.remoteNetworks = try RemoteNetwork.validated(remoteNetworks)
            session.remoteExcluded = try RemoteNetwork.validated(remoteExcluded)
            let url = dir.appendingPathComponent("session.json")
            try JSONEncoder().encode(session).write(to: url, options: .atomic)
            try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
            let lease = dir.appendingPathComponent("lease")
            try Data(token.utf8).write(to: lease, options: .atomic)
            try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: lease.path)
            leaseHeartbeat = try RouteLeaseHeartbeat(lease: lease)
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
    private func tick() {
        guard let dir = directory else { return }
        if !stopping { leaseHeartbeat?.ownerResponded() }
        let snapshot = (try? Data(contentsOf: dir.appendingPathComponent("status"))).flatMap { try? JSONDecoder().decode(RouteSnapshot.self, from: $0) }
        if let snapshot {
            companyState = snapshot.company
            if let company = companyState, let block = pendingCompanyBlock {
                if company.state == "blocked", company.generation == block { pendingCompanyBlock = nil }
                else { try? sendCompany(action: "block", generation: block) }
            }
            if pendingCompanyBlock == nil, let company = companyState, company.state == "prepared", company.generation == pendingCompanyCommit {
                pendingCompanyCommit = nil
                try? sendCompany(action: "commit", generation: company.generation)
            }
            onSnapshot?(snapshot)
        }
        let state = snapshot?.state ?? ""
        if state == "stopped" || state == "failed" {
            let outcome = state
            cleanup(); onState?(outcome); return
        }
        if serviceProcess?.isRunning == false {
            let supervisorStarted = FileManager.default.fileExists(atPath: dir.appendingPathComponent("status").path)
            let outcome = stopping && !supervisorStarted ? "stopped" : "serviceFailed"
            // Revoke the supervisor lease even if its last snapshot still says ready.
            try? FileManager.default.removeItem(at: dir.appendingPathComponent("lease"))
            cleanup(); onState?(outcome); return
        }
        if state == "ready", !stopping, state != lastState { lastState = state; onState?(state) }
    }
    func prepareCompany(_ claim: CompanyBackendClaim, generation: UUID) throws {
        guard let state = companyState, ["blocked", "failed"].contains(state.state), !stopping else {
            throw CompanyConnectionError.invalid("公司出口尚未预留或正在停止。")
        }
        pendingCompanyCommit = generation.uuidString
        do { try sendCompany(action: "prepare", generation: generation.uuidString, backend: claim) }
        catch { pendingCompanyCommit = nil; throw error }
    }
    func blockCompany(generation: UUID) throws {
        pendingCompanyCommit = nil
        pendingCompanyBlock = generation.uuidString
        try sendCompany(action: "block", generation: generation.uuidString)
    }
    private func sendCompany(action: String, generation: String, backend: CompanyBackendClaim? = nil) throws {
        struct Command: Encodable { let expectedRevision: UInt64; let generation, action: String; let backend: CompanyBackendClaim? }
        guard let directory, let state = companyState, !stopping else { throw CompanyConnectionError.invalid("公司控制会话已结束。") }
        let url = directory.appendingPathComponent("company-command.json")
        try JSONEncoder().encode(Command(expectedRevision: state.revision, generation: generation, action: action, backend: backend)).write(to: url, options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
    }
    func stop() {
        guard let dir = directory else { return }
        stopping = true
        leaseHeartbeat?.stop(); leaseHeartbeat = nil
        try? FileManager.default.removeItem(at: dir.appendingPathComponent("lease"))
        // A started worker exits via its lease; a pending client can close its socket.
        let supervisorStarted = FileManager.default.fileExists(atPath: dir.appendingPathComponent("status").path)
        if !supervisorStarted, serviceProcess?.isRunning == true { serviceProcess?.terminate() }
        tick()
    }
    private func cleanup() {
        leaseHeartbeat?.stop(); leaseHeartbeat = nil
        heartbeat?.invalidate(); heartbeat = nil
        if serviceProcess?.isRunning == true { serviceProcess?.terminate() }
        if let directory { try? FileManager.default.removeItem(at: directory) }
        directory = nil; serviceProcess = nil; lastState = ""; companyState = nil; pendingCompanyCommit = nil; pendingCompanyBlock = nil
    }
}

import Foundation
import GoConnectCore

@MainActor
final class TransportClient {
    private var process: Process?
    private var input: FileHandle?
    private var stopping = false
    private var generation = UUID()
    private var readerTask: Task<Void, Never>?
    private let runtimeOverride: URL?
    private let diagnostics: FaultDiagnostics
    init(runtimeURL: URL? = nil, diagnostics: FaultDiagnostics = .shared) {
        runtimeOverride = runtimeURL; self.diagnostics = diagnostics
    }
    var onEvent: ((TransportEvent) -> Void)?
    var isRunning: Bool { process != nil }
    var runtimeURL: URL { runtimeOverride ?? Bundle.main.resourceURL!.appendingPathComponent("Runtime") }
    var isAvailable: Bool { FileManager.default.isExecutableFile(atPath: runtimeURL.appendingPathComponent("bin/GoConnectTransport").path) && FileManager.default.isExecutableFile(atPath: runtimeURL.appendingPathComponent("bin/openconnect").path) }

    func start(profile: VPNProfile, password: String, token: String) throws {
        let request = TransportRequest(server: try profile.validatedServer(), username: profile.username,
                                       group: profile.group, password: password, token: token,
                                       openconnectPath: runtimeURL.appendingPathComponent("bin/openconnect").path)
        try start(command: "session", message: JSONEncoder().encode(request) + Data([10]))
    }
    func start(node: SubscriptionNode, token: String) throws {
        struct Request: Encodable { let node: SubscriptionNode; let token: String }
        try start(command: "subscription-session", message: JSONEncoder().encode(Request(node: node, token: token)) + Data([10]))
    }
    func startProxy(port: UInt16, token: String, rules: [DomainRule]) throws {
        struct Request: Encodable { let port: UInt16; let token: String; let directDomains: [DomainRule] }
        try start(command: "proxy-front", message: JSONEncoder().encode(Request(port: port, token: token, directDomains: rules)) + Data([10]))
    }
    private func start(command: String, message: Data) throws {
        guard process == nil else { throw CocoaError(.executableLoad) }
        stopping = false; generation = UUID(); let run = generation
        let p = Process(), stdin = Pipe(), stdout = Pipe()
        p.executableURL = runtimeURL.appendingPathComponent("bin/GoConnectTransport")
        p.arguments = [command]
        p.standardInput = stdin; p.standardOutput = stdout; p.standardError = FileHandle.nullDevice
        // No user shell, no credentials in argv, no inherited DYLD/Go environment.
        p.environment = ["PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "LANG": "en_US.UTF-8"]
        try p.run(); diagnostics.record(.transportStarted, value: Int(p.processIdentifier))
        diagnostics.record(.transportKind, value: command == "session" ? 1 : command == "subscription-session" ? 2 : 3)
        process = p; input = stdin.fileHandleForWriting
        input?.write(message)
        let handle = stdout.fileHandleForReading
        let diagnostics = diagnostics
        readerTask = Task.detached { [weak self] in
            var decoder = EventDecoder()
            while !Task.isCancelled {
                let data = handle.availableData
                if data.isEmpty { break }
                for event in decoder.feed(data) where event.event != "stopped" { await self?.deliver(event, run: run) }
            }
            try? handle.close()
            p.waitUntilExit()
            diagnostics.record(.transportExited, value: Int(p.terminationStatus))
            if p.terminationReason == .uncaughtSignal { diagnostics.record(.transportExitSignal, value: Int(p.terminationStatus)) }
            await self?.finished(run: run)
        }
    }
    private func deliver(_ event: TransportEvent, run: UUID) {
        guard generation == run else { return }
        // Drain final core diagnostics even after the UI has begun cleanup.
        if event.event == "diagnostic" {
            guard let code = event.code, let known = FaultEvent(rawValue: code), DisconnectReason.isCoreDiagnostic(known) else { return }
            diagnostics.record(known, value: event.value ?? 0)
            onEvent?(event)
            return
        }
        guard !stopping || event.event == "quarantined" else { return }
        if event.event == "error" { diagnostics.record(.transportError) }
        onEvent?(event)
    }
    private func finished(run: UUID) {
        guard generation == run else { return }
        process = nil; input = nil
        onEvent?(TransportEvent(event: "stopped"))
    }
    func stop() {
        guard !stopping, let p = process else { return }
        stopping = true
        diagnostics.record(.transportStopRequested, value: Int(p.processIdentifier))
        let run = generation
        Task { [weak self] in
            try? await Task.sleep(for: .seconds(10))
            guard let self, self.generation == run, self.process != nil else { return }
            p.terminate()
            self.onEvent?(TransportEvent(event: "quarantined"))
            self.onEvent?(TransportEvent(event: "error", message: "组件尚未退出，已停止等待；请导出诊断，不要反复重连。"))
            // Keep process ownership until it exits. UI must not spawn a second session.
        }
        // EOF triggers the helper to stop its own process group and children.
        try? input?.close(); input = nil
    }
}

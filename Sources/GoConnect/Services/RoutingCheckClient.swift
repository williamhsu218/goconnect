import Foundation
import GoConnectCore

@MainActor
final class RoutingCheckClient {
    private var process: Process?
    private var input: FileHandle?
    var onEvent: ((TransportEvent) -> Void)?
    var onStopped: (() -> Void)?

    func start(runtime: URL, mode: RoutingMode) throws {
        guard process == nil else { throw CocoaError(.executableLoad) }
        let p = Process(), stdin = Pipe(), stdout = Pipe()
        p.executableURL = runtime.appendingPathComponent("bin/GoConnectTransport")
        p.arguments = ["routing-check", "--tun"]
        if mode == .global { p.arguments?.append("--global") }
        p.standardInput = stdin; p.standardOutput = stdout; p.standardError = FileHandle.nullDevice
        p.environment = ["PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "LANG": "en_US.UTF-8"]
        try p.run(); process = p; input = stdin.fileHandleForWriting
        let handle = stdout.fileHandleForReading
        Task.detached { [weak self] in
            var decoder = EventDecoder()
            while true {
                let data = handle.availableData
                if data.isEmpty { break }
                for event in decoder.feed(data) { await self?.deliver(event) }
            }
            try? handle.close(); p.waitUntilExit()
            await self?.finished()
        }
    }
    private func deliver(_ event: TransportEvent) { onEvent?(event) }
    private func finished() {
        process = nil; try? input?.close(); input = nil
        onStopped?()
    }
    func stop() { try? input?.close(); input = nil }
}

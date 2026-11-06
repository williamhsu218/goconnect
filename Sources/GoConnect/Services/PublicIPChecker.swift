import Foundation
import GoConnectCore

/// A short-lived, cancellable helper; credentials go through stdin, never argv.
@MainActor
final class PublicIPChecker {
    private var process: Process?
    private var generation = UUID()
    var onResult: ((String?) -> Void)?

    func start(runtime: URL, port: UInt16, token: String) throws {
        stop()
        struct Request: Encodable { let port: UInt16; let token: String }
        let data = try JSONEncoder().encode(Request(port: port, token: token))
        let child = Process(), input = Pipe(), output = Pipe()
        child.executableURL = runtime.appendingPathComponent("bin/GoConnectTransport")
        child.arguments = ["public-ip"]
        child.environment = ["PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "LANG": "C"]
        child.standardInput = input; child.standardOutput = output
        child.standardError = FileHandle.nullDevice
        try child.run()
        process = child
        let run = generation
        do {
            try input.fileHandleForWriting.write(contentsOf: data)
            try input.fileHandleForWriting.close()
        } catch {
            child.terminate(); process = nil
            throw error
        }
        Task.detached { [weak self] in
            let handle = output.fileHandleForReading
            var data = Data()
            while data.count <= 4096, let chunk = try? handle.read(upToCount: 4096), !chunk.isEmpty {
                data.append(chunk)
                if data.contains(10) { break }
            }
            try? handle.close()
            child.waitUntilExit()
            let event = data.count <= 4096 ? try? JSONDecoder().decode(TransportEvent.self, from: data) : nil
            await self?.finished(run: run, ip: child.terminationStatus == 0 && event?.event == "public_ip" ? event?.publicIP : nil)
        }
    }

    private func finished(run: UUID, ip: String?) {
        guard generation == run else { return }
        process = nil
        onResult?(ip)
    }

    func stop() {
        generation = UUID()
        if let process, process.isRunning { process.terminate() }
        process = nil
    }
}

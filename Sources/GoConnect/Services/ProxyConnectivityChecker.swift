import Foundation
import Observation
import GoConnectCore

/// Checks a request through the explicit proxy, independently of tunnel readiness.
/// Never changes system proxy settings or falls back to a direct request.
@MainActor @Observable
final class ProxyConnectivityChecker {
    enum State: Equatable { case idle, checking, reachable(Int), failed }
    private(set) var state: State = .idle
    private var generation = 0
    private var process: Process?
    private var task: Task<Void, Never>?

    var title: String {
        switch state {
        case .idle: "代理请求尚未验证"
        case .checking: "正在检查代理请求…"
        case .reachable(let milliseconds): "代理请求成功 · \(milliseconds) ms"
        case .failed: "代理请求失败，请检查线路或稍后重试"
        }
    }

    func reset() {
        generation += 1
        task?.cancel()
        if process?.isRunning == true { process?.terminate() }
        state = .idle
    }

    func check(port: UInt16) {
        guard process?.isRunning != true else { return }
        generation += 1
        let current = generation
        state = .checking
        FaultDiagnostics.shared.record(.proxyCheckStarted)
        task = Task { [weak self] in
            guard let self, current == self.generation, !Task.isCancelled else { return }
            let dir = FileManager.default.temporaryDirectory.appendingPathComponent("GoConnect-proxy-check-" + UUID().uuidString)
            let output = dir.appendingPathComponent("response")
            var file: FileHandle?
            defer {
                try? file?.close()
                try? FileManager.default.removeItem(at: dir)
            }
            do {
                try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
                guard FileManager.default.createFile(atPath: output.path, contents: nil, attributes: [.posixPermissions: 0o600]) else { throw CocoaError(.fileWriteUnknown) }
                file = try FileHandle(forWritingTo: output)
                let p = Process()
                p.executableURL = URL(fileURLWithPath: "/usr/bin/curl")
                p.arguments = ProxyConnectivityResult.arguments(port: port)
                p.environment = ["PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "LANG": "C"]
                p.standardInput = FileHandle.nullDevice
                p.standardOutput = file
                p.standardError = FileHandle.nullDevice
                try p.run()
                process = p
                let start = ContinuousClock.now
                while p.isRunning && start.duration(to: .now) < .seconds(12) && !Task.isCancelled {
                    try? await Task.sleep(for: .milliseconds(100))
                }
                if p.isRunning { p.terminate() }
                // Never wait synchronously for an OS-blocked child; a still-running
                // process remains retained and prevents overlapping retries.
                guard current == generation, !Task.isCancelled else { return }
                let response = String(decoding: (try? Data(contentsOf: output)) ?? Data(), as: UTF8.self)
                let result = ProxyConnectivityResult(exitCode: p.isRunning ? -1 : p.terminationStatus, output: response)
                state = result.succeeded ? .reachable(result.milliseconds ?? 0) : .failed
                FaultDiagnostics.shared.record(result.succeeded ? .proxyCheckSucceeded : .proxyCheckFailed, value: result.milliseconds ?? 0)
            } catch {
                guard current == generation, !Task.isCancelled else { return }
                state = .failed
                FaultDiagnostics.shared.record(.proxyCheckFailed)
            }
        }
    }
}

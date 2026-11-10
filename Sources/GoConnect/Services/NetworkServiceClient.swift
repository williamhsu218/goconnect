import Foundation
import Observation
import GoConnectCore
import Darwin

struct NetworkServiceStatus: Decodable, Sendable {
    var state: String
    var version: String?
    var busy = false
    var message: String?
    var ready: Bool { state == "ready" }
    // Other states (notably needsUpdate) may return before querying the service.
    // Only the fixed helper's positive idle reply or absent installation is proof.
    var confirmsIdleForUpdate: Bool { !busy && (ready || state == "notInstalled") }
    var title: String {
        switch state {
        case "ready": "已授权，可直接连接"
        case "notInstalled": "尚未安装"
        case "needsUpdate": "需要更新"
        case "requiresAuthorization": "当前用户需要授权"
        case "checking": "正在检测"
        default: "服务未就绪"
        }
    }
}

@MainActor @Observable
final class NetworkServiceClient {
    private(set) var status = NetworkServiceStatus(state: "checking")
    private(set) var working = false
    private var generation = 0
    private var idleConfirmationRequired = false
    @ObservationIgnored private let statusReader: StatusReader
    typealias StatusReader = @Sendable (URL) async throws -> NetworkServiceStatus

    // An internal dependency for isolated tests, never selected by preferences,
    // environment variables or a user-supplied executable/URL.
    init(statusReader: StatusReader? = nil) {
        self.statusReader = statusReader ?? { try await Self.readStatus($0) }
    }

    var blocksUpdateInstallation: Bool { status.busy || idleConfirmationRequired }

    func requireIdleConfirmation() {
        generation += 1 // A refresh started before teardown is not evidence after it.
        idleConfirmationRequired = true
    }

    private(set) var testHostMessage: String?
    private(set) var testHostAvailable = !Product.isNetworkTestApp
    private struct Identity: Decodable { let flavor, label: String }
    private struct HostStatus: Decodable { let available: Bool; let reason: String? }

    /// Rechecked immediately before every test-only privileged operation.
    func assertTestHostAvailable() async throws {
        guard Product.isNetworkTestApp else { return }
        let executable = executable
        let result = await Task.detached { () -> (Bool, String?) in
            guard let identity = try? Self.run(executable, arguments: ["service-identity"]), identity.code == 0,
                  let value = try? JSONDecoder().decode(Identity.self, from: identity.output),
                  value.flavor == "test", value.label == "com.willhsu.GoConnect.DualVPNTest.network" else {
                return (false, "测试版网络组件身份不匹配，已阻止系统操作。")
            }
            guard let response = try? Self.run(executable, arguments: ["test-host-status"]), response.code == 0,
                  let host = try? JSONDecoder().decode(HostStatus.self, from: response.output) else {
                return (false, "无法确认正式版网络状态，已暂停测试接管。")
            }
            return (host.available, host.available ? nil : "正式版 VPN 正在运行或状态未确认；请在单独验收窗口断开后重试。")
        }.value
        testHostAvailable = result.0; testHostMessage = result.1
        guard result.0 else { throw ServiceError(result.1 ?? "当前不能接管测试网络。") }
    }
    private var executable: URL { Bundle.main.resourceURL!.appendingPathComponent("Runtime/bin/GoConnectTransport") }

    func diagnosticLogs() async -> String {
        guard !Product.isIsolatedTestApp else { return "隔离测试包不读取正式服务日志。" }
        let executable = executable
        return await Task.detached {
            guard let result = try? Self.run(executable, arguments: ["service-diagnostics"]), result.code == 0 else {
                return "网络服务故障日志不可用：服务可能未更新、未运行或读取超时。"
            }
            return String(decoding: result.output, as: UTF8.self)
        }.value
    }

    func refresh() {
        guard !working else { return }
        Task { _ = await refreshStatus() }
    }

    /// Await one bounded, current read. A late refresh must never overwrite a
    /// maintenance result or satisfy a newer installation confirmation.
    func refreshStatus(timeout: Duration = .seconds(5)) async -> NetworkServiceStatus? {
        guard !working, !Task.isCancelled else { return nil }
        generation += 1
        let current = generation
        let result: NetworkServiceStatus
        do {
            if Product.isIsolatedTestApp {
                result = NetworkServiceStatus(state: "notInstalled", message: "隔离测试包不访问正式服务。")
            } else {
                if Product.isNetworkTestApp { try await assertTestHostAvailable() }
                result = try await Self.boundedStatusRead(executable, reader: statusReader, timeout: timeout)
            }
        } catch {
            guard !Task.isCancelled else { return nil }
            result = NetworkServiceStatus(state: "unavailable", message: "无法确认本机网络服务状态，请重新检测后重试。")
        }
        guard current == generation, !working, !Task.isCancelled else { return nil }
        status = result
        if result.confirmsIdleForUpdate { idleConfirmationRequired = false }
        return result
    }

    func ensureReady(forceInstall: Bool = false) async throws {
        guard !Product.isIsolatedTestApp else { throw ServiceError("隔离测试包不使用正式网络服务。") }
        guard !working else { throw ServiceError("本机服务正在处理操作，请稍候。") }
        working = true; generation += 1
        defer { working = false }
        try await assertTestHostAvailable()
        status = try await Self.boundedStatusRead(executable, reader: statusReader, timeout: .seconds(5))
        if status.ready && !forceInstall { return }
        guard !status.busy else { throw ServiceError("请先断开 VPN 或停止分流自检，再更新服务。") }
        try await authorize(arguments: ["service-install", String(getuid())])
        status = try await Self.boundedStatusRead(executable, reader: statusReader, timeout: .seconds(5))
        guard status.ready else { throw ServiceError("服务尚未就绪，请在系统设置中检查 GoConnect 后台项目，然后重新检测。") }
    }

    func uninstall() async throws {
        guard !working else { throw ServiceError("本机服务正在处理操作，请稍候。") }
        working = true; generation += 1
        defer { working = false }
        try await assertTestHostAvailable()
        status = try await Self.boundedStatusRead(executable, reader: statusReader, timeout: .seconds(5))
        guard !status.busy else { throw ServiceError("请先结束测试版连接，再移除测试服务。") }
        try await authorize(arguments: ["service-uninstall"])
        status = try await Self.boundedStatusRead(executable, reader: statusReader, timeout: .seconds(5))
        guard status.state == "notInstalled" else { throw ServiceError("服务移除后仍能读取到安装信息，请重新检测。") }
    }

    private func authorize(arguments: [String]) async throws {
        guard !Product.isIsolatedTestApp else { throw ServiceError("隔离测试包不安装、替换或移除正式网络服务。") }
        try await assertTestHostAvailable()
        let command = "exec " + ([executable.path] + arguments).map(ShellQuote.argument).joined(separator: " ")
        let script = "do shell script " + ShellQuote.appleScriptString(command) + " with administrator privileges"
        let result = try await Task.detached {
            try Self.run(URL(fileURLWithPath: "/usr/bin/osascript"), arguments: ["-e", script])
        }.value
        guard result.code == 0 else {
            if result.error.contains("-128") { throw ServiceError("已取消管理员授权，本机服务未启用。") }
            throw ServiceError(result.error.isEmpty ? "本机服务操作失败，请重新检测后重试。" : String(result.error.prefix(700)))
        }
    }

    nonisolated private static func boundedStatusRead(_ executable: URL, reader: @escaping StatusReader,
                                                     timeout: Duration) async throws -> NetworkServiceStatus {
        guard timeout > .zero else { throw ServiceError("读取本机服务状态超时。") }
        let completion = StatusReadCompletion()
        let result = try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                completion.install(continuation)
                completion.register(Task {
                    do {
                        try Task.checkCancellation()
                        completion.finish(.success(try await reader(executable)))
                    }
                    catch { completion.finish(.failure(error)) }
                })
                completion.register(Task {
                    do { try await Task.sleep(for: timeout) }
                    catch { return }
                    completion.finish(.failure(ServiceError("读取本机服务状态超时。")))
                })
            }
        } onCancel: {
            completion.finish(.failure(CancellationError()))
        }
        try Task.checkCancellation()
        return result
    }

    // Internal for subprocess regressions using a temporary fake helper. The
    // production caller always supplies the sealed Bundle Runtime executable.
    nonisolated static func readStatus(_ executable: URL) async throws -> NetworkServiceStatus {
        let control = StatusProcessControl()
        let result = try await withTaskCancellationHandler {
            try await Task.detached {
                let process = Process(), output = Pipe()
                process.executableURL = executable; process.arguments = ["service-status"]
                process.environment = ["PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "LANG": "en_US.UTF-8"]
                process.standardInput = FileHandle.nullDevice
                process.standardOutput = output; process.standardError = FileHandle.nullDevice
                try control.launch(process)
                defer {
                    if process.isRunning { control.cancel() }
                    control.finished()
                }
                defer { try? output.fileHandleForReading.close() }
                // The helper's reply is at most 4096 bytes. No unbounded pipe
                // buffering, stderr deadlock or indefinitely hung status child.
                var data = Data()
                while data.count <= 4096 {
                    guard let chunk = try output.fileHandleForReading.read(upToCount: 4097 - data.count), !chunk.isEmpty else { break }
                    data.append(chunk)
                }
                if data.count > 4096 {
                    control.cancel()
                    throw ServiceError("本机服务状态回应过大。")
                }
                process.waitUntilExit()
                guard process.terminationStatus == 0 else { throw ServiceError("无法读取本机服务状态。") }
                return try JSONDecoder().decode(NetworkServiceStatus.self, from: data)
            }.value
        } onCancel: {
            control.cancel()
        }
        try Task.checkCancellation()
        return result
    }

    nonisolated private static func run(_ executable: URL, arguments: [String]) throws -> (code: Int32, output: Data, error: String) {
        let process = Process(), output = Pipe(), error = Pipe()
        process.executableURL = executable; process.arguments = arguments
        process.environment = ["PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "LANG": "en_US.UTF-8"]
        process.standardInput = FileHandle.nullDevice
        process.standardOutput = output; process.standardError = error
        try process.run()
        let data = output.fileHandleForReading.readDataToEndOfFile()
        let errorData = error.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        return (process.terminationStatus, data, String(decoding: errorData, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines))
    }
}

/// A cancellation/timeout race that does not wait for an uncooperative reader.
/// Production readers also terminate their own status-only child on cancellation.
private final class StatusReadCompletion: @unchecked Sendable {
    private let lock = NSLock()
    private var result: Result<NetworkServiceStatus, Error>?
    private var continuation: CheckedContinuation<NetworkServiceStatus, Error>?
    private var tasks: [Task<Void, Never>] = []

    func install(_ continuation: CheckedContinuation<NetworkServiceStatus, Error>) {
        lock.lock()
        if let result { lock.unlock(); continuation.resume(with: result) }
        else { self.continuation = continuation; lock.unlock() }
    }
    func register(_ task: Task<Void, Never>) {
        lock.lock()
        if result != nil { lock.unlock(); task.cancel() }
        else { tasks.append(task); lock.unlock() }
    }
    func finish(_ result: Result<NetworkServiceStatus, Error>) {
        lock.lock()
        guard self.result == nil else { lock.unlock(); return }
        self.result = result
        let continuation = continuation, tasks = tasks
        self.continuation = nil; self.tasks = []
        lock.unlock()
        tasks.forEach { $0.cancel() }
        continuation?.resume(with: result)
    }
}

private final class StatusProcessControl: @unchecked Sendable {
    private let lock = NSLock()
    private var process: Process?
    private var cancelled = false

    func launch(_ process: Process) throws {
        lock.lock(); defer { lock.unlock() }
        guard !cancelled else { throw CancellationError() }
        try process.run()
        self.process = process
    }
    func finished() {
        lock.lock(); process = nil; lock.unlock()
    }
    func cancel() {
        lock.lock()
        guard !cancelled else { lock.unlock(); return }
        cancelled = true; let process = process; lock.unlock()
        guard let process, process.isRunning else { return }
        // Only the child launched for this read; never the installed service or VPN.
        process.terminate()
        DispatchQueue.global().asyncAfter(deadline: .now() + .milliseconds(250)) {
            if process.isRunning { _ = kill(process.processIdentifier, SIGKILL) }
        }
    }
}

private struct ServiceError: LocalizedError {
    let message: String
    init(_ message: String) { self.message = message }
    var errorDescription: String? { message }
}

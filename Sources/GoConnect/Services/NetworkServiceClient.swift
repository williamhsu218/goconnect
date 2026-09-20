import Foundation
import Observation
import GoConnectCore

struct NetworkServiceStatus: Decodable, Sendable {
    var state: String
    var version: String?
    var busy = false
    var message: String?
    var ready: Bool { state == "ready" }
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
    private var executable: URL { Bundle.main.resourceURL!.appendingPathComponent("Runtime/bin/GoConnectTransport") }

    func diagnosticLogs() async -> String {
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
        generation += 1
        let current = generation, executable = executable
        Task {
            let result = await Self.readStatus(executable)
            if current == generation && !working { status = result }
        }
    }

    func ensureReady(forceInstall: Bool = false) async throws {
        guard !working else { throw ServiceError("本机服务正在处理操作，请稍候。") }
        working = true; generation += 1
        defer { working = false }
        status = await Self.readStatus(executable)
        if status.ready && !forceInstall { return }
        guard !status.busy else { throw ServiceError("请先断开 VPN 或停止分流自检，再更新服务。") }
        try await authorize(arguments: ["service-install", String(getuid())])
        status = await Self.readStatus(executable)
        guard status.ready else { throw ServiceError("服务尚未就绪，请在系统设置中检查 GoConnect 后台项目，然后重新检测。") }
    }

    func uninstall() async throws {
        guard !working else { throw ServiceError("本机服务正在处理操作，请稍候。") }
        working = true; generation += 1
        defer { working = false }
        try await authorize(arguments: ["service-uninstall"])
        status = await Self.readStatus(executable)
        guard status.state == "notInstalled" else { throw ServiceError("服务移除后仍能读取到安装信息，请重新检测。") }
    }

    private func authorize(arguments: [String]) async throws {
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

    nonisolated private static func readStatus(_ executable: URL) async -> NetworkServiceStatus {
        await Task.detached {
            guard let result = try? run(executable, arguments: ["service-status"]), result.code == 0,
                  let status = try? JSONDecoder().decode(NetworkServiceStatus.self, from: result.output) else {
                return NetworkServiceStatus(state: "unavailable")
            }
            return status
        }.value
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

private struct ServiceError: LocalizedError {
    let message: String
    init(_ message: String) { self.message = message }
    var errorDescription: String? { message }
}

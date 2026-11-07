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
        guard !Product.isIsolatedTestApp else { status = NetworkServiceStatus(state: "notInstalled", message: "隔离测试包不访问正式服务。"); return }
        guard !working else { return }
        generation += 1
        let current = generation, executable = executable
        Task {
            if Product.isNetworkTestApp { try? await self.assertTestHostAvailable() }
            let result = await Self.readStatus(executable)
            if current == generation && !working { status = result }
        }
    }

    func ensureReady(forceInstall: Bool = false) async throws {
        guard !Product.isIsolatedTestApp else { throw ServiceError("隔离测试包不使用正式网络服务。") }
        guard !working else { throw ServiceError("本机服务正在处理操作，请稍候。") }
        working = true; generation += 1
        defer { working = false }
        try await assertTestHostAvailable()
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
        try await assertTestHostAvailable()
        status = await Self.readStatus(executable)
        guard !status.busy else { throw ServiceError("请先结束测试版连接，再移除测试服务。") }
        try await authorize(arguments: ["service-uninstall"])
        status = await Self.readStatus(executable)
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

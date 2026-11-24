import Foundation
import GoConnectCore
import Darwin

/// Uses the same validated, unprivileged subscription-session path as connections.
/// Credentials go through stdin; temporary files are private and always removed.
@MainActor
final class NodeLatencyTester {
    static let testURL = "https://www.gstatic.com/generate_204"
    private var helper: Process?
    private var curl: Process?
    private var input: Pipe?

    func stop() {
        try? input?.fileHandleForWriting.close()
        if helper?.isRunning == true { helper?.terminate() }
        if curl?.isRunning == true { curl?.terminate() }
    }

    func measure(_ node: SubscriptionNode, runtime: URL) async -> NodeLatencyResult {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("GoConnect-latency-" + UUID().uuidString)
        var files: [FileHandle] = []
        defer {
            files.forEach { try? $0.close() }
            try? FileManager.default.removeItem(at: directory)
        }
        let executable = runtime.appendingPathComponent("bin/GoConnectTransport")
        guard let path = await NetworkServiceClient.readRecoveryPath(executable, timeout: .seconds(2)) else {
            return NodeLatencyResult(outcome: Task.isCancelled ? .cancelled : .unmeasured, detail: "无法确认当前物理出口，请在网络稳定后重测")
        }
        var result = NodeLatencyResult(outcome: .unmeasured)
        do {
            try Task.checkCancellation()
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
            func outputFile(_ name: String) throws -> (URL, FileHandle) {
                let url = directory.appendingPathComponent(name)
                guard FileManager.default.createFile(atPath: url.path, contents: nil, attributes: [.posixPermissions: 0o600]) else { throw CocoaError(.fileWriteUnknown) }
                let handle = try FileHandle(forWritingTo: url); files.append(handle); return (url, handle)
            }
            let (statusURL, statusOutput) = try outputFile("status")
            let p = Process(), pipe = Pipe()
            helper = p; input = pipe
            p.executableURL = executable
            p.arguments = ["subscription-session"]
            p.environment = ["PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "LANG": "en_US.UTF-8"]
            p.standardInput = pipe; p.standardOutput = statusOutput; p.standardError = FileHandle.nullDevice
            try p.run()
            let token = UUID().uuidString + UUID().uuidString
            struct Request: Encodable { let node: SubscriptionNode; let token: String; let testInterface: String }
            var data = try JSONEncoder().encode(Request(node: node, token: token, testInterface: path.interface)); data.append(10)
            guard data.count <= 65_536 else { throw CocoaError(.fileReadTooLarge) }
            try pipe.fileHandleForWriting.write(contentsOf: data)
            var port: UInt16?
            let start = ContinuousClock.now
            while start.duration(to: .now) < .seconds(13) {
                try Task.checkCancellation()
                guard p.isRunning else { break }
                let lines = String(decoding: (try? Data(contentsOf: statusURL)) ?? Data(), as: UTF8.self).split(separator: "\n")
                for line in lines {
                    if let event = try? JSONDecoder().decode(TransportEvent.self, from: Data(line.utf8)), event.event == "ready" { port = event.port }
                }
                if port != nil { break }
                try await Task.sleep(for: .milliseconds(100))
            }
            if let port {
                let (responseURL, responseOutput) = try outputFile("response")
                let request = Process(), configPipe = Pipe()
                curl = request
                request.executableURL = URL(fileURLWithPath: "/usr/bin/curl")
                request.arguments = ["--disable", "--config", "-"]
                request.environment = ["PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "LANG": "C"]
                request.standardInput = configPipe; request.standardOutput = responseOutput; request.standardError = FileHandle.nullDevice
                try request.run()
                let config = """
                url = "\(Self.testURL)"
                proxy = "socks5h://127.0.0.1:\(port)"
                proxy-user = "goconnect:\(token)"
                noproxy = ""
                connect-timeout = 6
                max-time = 8
                silent
                output = "/dev/null"
                write-out = "%{http_code} %{time_total}"
                """
                try configPipe.fileHandleForWriting.write(contentsOf: Data(config.utf8))
                try configPipe.fileHandleForWriting.close()
                let began = ContinuousClock.now
                while request.isRunning && began.duration(to: .now) < .seconds(10) {
                    try Task.checkCancellation()
                    try await Task.sleep(for: .milliseconds(100))
                }
                if request.isRunning { result = NodeLatencyResult(outcome: .timeout) }
                else { result = .parse(curlExitCode: request.terminationStatus, output: String(decoding: (try? Data(contentsOf: responseURL)) ?? Data(), as: UTF8.self)) }
                let current = await NetworkServiceClient.readRecoveryPath(executable, timeout: .seconds(2))
                if current == path {
                    result.physicalInterface = path.interface
                } else {
                    result = NodeLatencyResult(outcome: .unmeasured, detail: "测试期间物理出口变化，请重测")
                }
            } else {
                result = NodeLatencyResult(outcome: .unmeasured, detail: "未能建立独立测试路径，请检查物理网络及节点地址")
            }
        } catch is CancellationError { result = NodeLatencyResult(outcome: .cancelled) }
        catch {
            result = NodeLatencyResult(outcome: .unmeasured, detail: "本机测试进程未能完成请求")
        }
        stop()
        // Let subscription-session revoke its stdin lease and reap its child core.
        for _ in 0..<30 {
            if helper?.isRunning != true && curl?.isRunning != true { break }
            await Task.detached { try? await Task.sleep(for: .milliseconds(100)) }.value
        }
        if let curl, curl.isRunning { Darwin.kill(curl.processIdentifier, SIGKILL) }
        if let helper, helper.isRunning { Darwin.kill(helper.processIdentifier, SIGKILL) }
        self.helper = nil; self.curl = nil; self.input = nil
        return Task.isCancelled ? NodeLatencyResult(outcome: .cancelled) : result
    }
}

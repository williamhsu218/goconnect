import AppKit
import Darwin
import Foundation

private struct TesterConfiguration: Decodable {
    let server: String
    let username: String
    let group: String
    let openconnectPath: String
    let transportPath: String
    let interface: String
    let gatewayIPs: [String]
    let network: String
    let dnsName: String
    let profileName: String
    let evidenceDirectory: String

    static func load(arguments: [String]) throws -> Self {
        guard arguments.count == 3, arguments[1] == "--config", arguments[2].hasPrefix("/") else {
            throw TesterError("启动参数无效。请通过项目测试脚本启动。")
        }
        let supplied = arguments[2], url = URL(fileURLWithPath: arguments[2])
        guard url.standardizedFileURL.path == supplied, url.resolvingSymlinksInPath().path == supplied else {
            throw TesterError("测试配置必须使用无符号链接的规范绝对路径。")
        }
        let descriptor = open(supplied, O_RDONLY | O_NOFOLLOW | O_CLOEXEC)
        guard descriptor >= 0 else { throw TesterError("无法安全打开测试配置。") }
        let handle = FileHandle(fileDescriptor: descriptor, closeOnDealloc: true)
        defer { try? handle.close() }
        var information = stat()
        guard fstat(descriptor, &information) == 0,
              information.st_mode & mode_t(S_IFMT) == mode_t(S_IFREG),
              information.st_mode & 0o7777 == 0o600, information.st_uid == geteuid(),
              information.st_nlink == 1, information.st_size > 0, information.st_size <= 65_536 else {
            throw TesterError("测试配置须为当前用户拥有的 0600 普通文件，且不得超过 64 KB。")
        }
        let bytes = try handle.read(upToCount: 65_537) ?? Data()
        guard bytes.count <= 65_536 else { throw TesterError("测试配置超过大小限制。") }
        let value = try JSONDecoder().decode(Self.self, from: bytes)
        try value.validatePackagedExecutable()
        return value
    }

    private func validatePackagedExecutable() throws {
        guard let resources = Bundle.main.resourceURL else { throw TesterError("测试应用缺少资源目录。") }
        let supplied = URL(fileURLWithPath: transportPath)
        let expected = resources.resolvingSymlinksInPath().appendingPathComponent("Runtime/bin/GoConnectTransport")
        let canonical = supplied.resolvingSymlinksInPath().standardizedFileURL
        guard supplied.standardizedFileURL.path == transportPath, canonical.path == transportPath,
              expected.standardizedFileURL.path == expected.resolvingSymlinksInPath().standardizedFileURL.path,
              canonical.path == expected.standardizedFileURL.path,
              canonical.lastPathComponent == "GoConnectTransport",
              FileManager.default.isExecutableFile(atPath: canonical.path) else {
            throw TesterError("仅允许启动此测试应用内的 GoConnectTransport。")
        }
        var information = stat()
        guard lstat(transportPath, &information) == 0,
              information.st_mode & mode_t(S_IFMT) == mode_t(S_IFREG), information.st_mode & 0o022 == 0 else {
            throw TesterError("测试网络组件不是安全的普通可执行文件。")
        }
    }
}

private struct TesterRequest: Encodable {
    let server: String
    let username: String
    let group: String
    let password: String
    let openconnectPath: String
    let interface: String
    let gatewayIPs: [String]
    let network: String
    let dnsName: String
    let evidenceDirectory: String
    init(configuration: TesterConfiguration, password: String) {
        server = configuration.server; username = configuration.username; group = configuration.group
        self.password = password; openconnectPath = configuration.openconnectPath
        interface = configuration.interface; gatewayIPs = configuration.gatewayIPs
        network = configuration.network; dnsName = configuration.dnsName
        evidenceDirectory = configuration.evidenceDirectory
    }
}
private struct TesterError: LocalizedError {
    let message: String
    init(_ message: String) { self.message = message }
    var errorDescription: String? { message }
}
private struct TesterEvent: Decodable {
    let event: String
    let message: String?
}

@MainActor
private final class TesterDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate {
    private var window: NSWindow!
    private let passwordField = NSSecureTextField()
    private let startButton = NSButton(title: "连接并测试", target: nil, action: nil)
    private let stopButton = NSButton(title: "停止", target: nil, action: nil)
    private let statusLabel = NSTextField(labelWithString: "等待输入公司线路密码")
    private let output = NSTextView()
    private var summaryLabel: NSTextField!
    private var configuration: TesterConfiguration?
    private var process: Process?
    private var input: FileHandle?
    private var generation = UUID()
    private var stopRequested = false
    private var quarantine = false
    private var cleanupConfirmed = false
    private var pendingWindowClose = false
    private var pendingApplicationQuit = false

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.regular)
        buildWindow()
        do {
            let value = try TesterConfiguration.load(arguments: CommandLine.arguments)
            configuration = value
            summaryLabel.stringValue = "\(value.profileName)  ·  \(value.server)\n公司目标：\(value.network)  ·  DNS：\(value.dnsName)\n物理接口：\(value.interface)"
        } catch {
            statusLabel.stringValue = "无法启动测试"
            startButton.isEnabled = false; passwordField.isEnabled = false
            append("配置检查失败：" + error.localizedDescription)
        }
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        window.makeFirstResponder(passwordField)
    }

    private func buildWindow() {
        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 620, height: 470),
                          styleMask: [.titled, .closable, .miniaturizable, .resizable], backing: .buffered, defer: false)
        window.title = "GoConnect 公司隧道独立测试"
        window.minSize = NSSize(width: 570, height: 420); window.center()
        window.delegate = self; window.isReleasedWhenClosed = false
        let title = NSTextField(labelWithString: "公司隧道验收")
        title.font = .systemFont(ofSize: 20, weight: .semibold)
        summaryLabel = NSTextField(wrappingLabelWithString: "正在读取测试配置…")
        summaryLabel.font = .systemFont(ofSize: 12); summaryLabel.textColor = .secondaryLabelColor
        summaryLabel.isSelectable = true
        summaryLabel.setContentCompressionResistancePriority(.required, for: .vertical)
        let boundary = NSTextField(wrappingLabelWithString: "不修改 GoConnect、系统路由或 DNS。仅测试指定公司目标。")
        boundary.font = .systemFont(ofSize: 12, weight: .medium); boundary.textColor = .secondaryLabelColor
        passwordField.placeholderString = "公司 VPN 密码（仅用于本次连接）"
        passwordField.font = .systemFont(ofSize: 13)
        passwordField.target = self; passwordField.action = #selector(startTest)
        passwordField.setAccessibilityLabel("公司 VPN 密码")
        startButton.target = self; startButton.action = #selector(startTest)
        startButton.bezelStyle = .rounded; startButton.keyEquivalent = "\r"
        stopButton.target = self; stopButton.action = #selector(stopTest)
        stopButton.bezelStyle = .rounded; stopButton.isEnabled = false
        let buttons = NSStackView(views: [startButton, stopButton]); buttons.orientation = .horizontal; buttons.spacing = 8
        let inputRow = NSStackView(views: [passwordField, buttons]); inputRow.orientation = .horizontal; inputRow.spacing = 10
        passwordField.setContentHuggingPriority(.defaultLow, for: .horizontal)
        statusLabel.font = .systemFont(ofSize: 12, weight: .medium); statusLabel.lineBreakMode = .byTruncatingTail
        let scroll = NSScrollView(); scroll.borderType = .bezelBorder; scroll.hasVerticalScroller = true
        output.isEditable = false; output.isSelectable = true
        output.font = .monospacedSystemFont(ofSize: 11, weight: .regular)
        output.textContainerInset = NSSize(width: 8, height: 8)
        output.isHorizontallyResizable = false; output.isVerticallyResizable = true
        output.autoresizingMask = [.width]; output.textContainer?.widthTracksTextView = true
        scroll.documentView = output
        let content = NSStackView(views: [title, summaryLabel, boundary, inputRow, statusLabel, scroll])
        content.orientation = .vertical; content.alignment = .leading; content.spacing = 12
        content.translatesAutoresizingMaskIntoConstraints = false
        window.contentView!.addSubview(content)
        NSLayoutConstraint.activate([
            content.leadingAnchor.constraint(equalTo: window.contentView!.leadingAnchor, constant: 24),
            content.trailingAnchor.constraint(equalTo: window.contentView!.trailingAnchor, constant: -24),
            content.topAnchor.constraint(equalTo: window.contentView!.topAnchor, constant: 22),
            content.bottomAnchor.constraint(equalTo: window.contentView!.bottomAnchor, constant: -22),
            summaryLabel.widthAnchor.constraint(equalTo: content.widthAnchor),
            boundary.widthAnchor.constraint(equalTo: content.widthAnchor),
            inputRow.widthAnchor.constraint(equalTo: content.widthAnchor),
            statusLabel.widthAnchor.constraint(equalTo: content.widthAnchor),
            scroll.widthAnchor.constraint(equalTo: content.widthAnchor),
            scroll.heightAnchor.constraint(greaterThanOrEqualToConstant: 140)
        ])
        buildMenu()
    }
    private func buildMenu() {
        let menu = NSMenu(), appItem = NSMenuItem(), appMenu = NSMenu()
        appMenu.addItem(NSMenuItem(title: "退出公司隧道测试", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q"))
        appItem.submenu = appMenu; menu.addItem(appItem)
        let editItem = NSMenuItem(), editMenu = NSMenu(title: "编辑")
        for (title, action, key) in [("剪切", #selector(NSText.cut(_:)), "x"), ("拷贝", #selector(NSText.copy(_:)), "c"), ("粘贴", #selector(NSText.paste(_:)), "v"), ("全选", #selector(NSText.selectAll(_:)), "a")] {
            editMenu.addItem(NSMenuItem(title: title, action: action, keyEquivalent: key))
        }
        editItem.submenu = editMenu; menu.addItem(editItem); NSApp.mainMenu = menu
    }

    @objc private func startTest() {
        guard process == nil, !quarantine, let configuration else { return }
        guard !passwordField.stringValue.isEmpty else {
            statusLabel.stringValue = "请输入公司线路密码。"; window.makeFirstResponder(passwordField); return
        }
        guard passwordField.stringValue.utf8.count <= 8_192,
              !passwordField.stringValue.contains(where: { $0 == "\n" || $0 == "\r" || $0 == "\0" }) else {
            passwordField.stringValue = ""; statusLabel.stringValue = "密码过长或包含换行、控制分隔符。"; return
        }
        var requestData: Data
        do {
            requestData = try JSONEncoder().encode(TesterRequest(configuration: configuration, password: passwordField.stringValue))
            requestData.append(10)
        } catch { passwordField.stringValue = ""; statusLabel.stringValue = "无法准备测试请求。"; return }
        passwordField.stringValue = ""
        guard requestData.count <= 65_536 else {
            requestData.resetBytes(in: 0..<requestData.count)
            statusLabel.stringValue = "测试请求超过大小限制。"; return
        }
        let child = Process(), stdin = Pipe(), stdout = Pipe(), attempt = UUID()
        child.executableURL = URL(fileURLWithPath: configuration.transportPath); child.arguments = ["company-test"]
        child.environment = ["PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "LANG": "en_US.UTF-8"]
        child.standardInput = stdin; child.standardOutput = stdout; child.standardError = FileHandle.nullDevice
        generation = attempt; stopRequested = false; cleanupConfirmed = false
        do { try child.run() }
        catch {
            requestData.resetBytes(in: 0..<requestData.count)
            statusLabel.stringValue = "无法启动测试组件。"; append("组件启动失败；尚未开始网络测试。"); return
        }
        process = child; input = stdin.fileHandleForWriting
        startButton.isEnabled = false; passwordField.isEnabled = false; stopButton.isEnabled = true
        statusLabel.stringValue = "正在连接公司线路…"; append("已启动独立测试；等待组件报告实际结果。")
        // The password exists only in the one-time stdin request. The lifetime
        // pipe remains open until Stop/quit; no keychain or credential log exists.
        do { try input?.write(contentsOf: requestData) }
        catch { append("请求发送失败，正在结束测试。"); requestStop() }
        requestData.resetBytes(in: 0..<requestData.count)
        let readHandle = stdout.fileHandleForReading
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            var pending = Data()
            while true {
                let bytes = readHandle.availableData
                if bytes.isEmpty { break }
                pending.append(bytes)
                if pending.count > 65_536 {
                    DispatchQueue.main.async { [weak self] in
                        guard let self, self.generation == attempt else { return }
                        self.append("组件输出超过安全限制，已请求停止测试。"); self.requestStop()
                    }
                    break
                }
                while let newline = pending.firstIndex(of: 10) {
                    let line = Data(pending[..<newline]); pending.removeSubrange(...newline)
                    guard let event = try? JSONDecoder().decode(TesterEvent.self, from: line),
                          ["status", "result", "error", "complete", "stopped", "quarantined"].contains(event.event) else { continue }
                    DispatchQueue.main.async { [weak self] in
                        guard let self, self.generation == attempt else { return }; self.handle(event)
                    }
                }
            }
            try? readHandle.close(); child.waitUntilExit()
            let exitCode = child.terminationStatus
            DispatchQueue.main.async { [weak self] in self?.finished(attempt: attempt, exitCode: exitCode) }
        }
    }
    private func handle(_ event: TesterEvent) {
        if event.event == "stopped" { cleanupConfirmed = true }
        if event.event == "quarantined" { quarantine = true }
        if let message = event.message, !message.isEmpty {
            let text = String(message.prefix(2_000)); append(text)
            if !stopRequested { statusLabel.stringValue = text }
        }
        if event.event == "complete", !stopRequested { statusLabel.stringValue = "组件报告测试完成，等待实际退出与清理…" }
    }
    @objc private func stopTest() { requestStop() }
    private func requestStop() {
        guard let owned = process, !stopRequested else { return }
        stopRequested = true; statusLabel.stringValue = "正在停止测试并等待组件清理…"
        stopButton.isEnabled = false; try? input?.close(); input = nil
        DispatchQueue.main.asyncAfter(deadline: .now() + 15) { [weak self, weak owned] in
            guard let self, let owned, self.process === owned, owned.isRunning else { return }
            self.quarantine = true
            self.statusLabel.stringValue = "组件尚未退出，已发送终止信号；退出前禁止再次连接。"
            self.append("停止等待超时，仅终止本次测试进程；未操作 GoConnect 或其它进程。")
            owned.terminate()
            DispatchQueue.main.asyncAfter(deadline: .now() + 8) { [weak self, weak owned] in
                guard let self, let owned, self.process === owned, owned.isRunning else { return }
                self.quarantine = true
                self.append("测试组件仍未退出，仅强制终止此测试进程；本轮保持锁定，不作为清理通过。")
                kill(owned.processIdentifier, SIGKILL)
            }
        }
    }
    private func finished(attempt: UUID, exitCode: Int32) {
        guard generation == attempt else { return }
        try? input?.close(); input = nil; process = nil
        let wasStopped = stopRequested; stopRequested = false
        quarantine = quarantine || !cleanupConfirmed
        stopButton.isEnabled = false; startButton.isEnabled = configuration != nil && !quarantine; passwordField.isEnabled = configuration != nil && !quarantine
        if quarantine { statusLabel.stringValue = "清理未正常确认（\(exitCode)），本轮已锁定；请保留诊断。" }
        else if exitCode != 0 { statusLabel.stringValue = "测试未完成（\(exitCode)），临时通道已清理；请检查具体失败原因。" }
        else { statusLabel.stringValue = wasStopped ? "本轮已停止，临时通道已清理。" : "只读探测结束，临时通道已清理；业务结果见明细。" }
        append(statusLabel.stringValue)
        if pendingApplicationQuit { NSApp.reply(toApplicationShouldTerminate: true) }
        else if pendingWindowClose { window.close(); NSApp.terminate(nil) }
    }
    private func append(_ text: String) {
        var value = output.string + (output.string.isEmpty ? "" : "\n") + text
        if value.count > 10_000 { value = String(value.suffix(10_000)) }
        output.string = value; output.scrollToEndOfDocument(nil)
    }
    func windowShouldClose(_ sender: NSWindow) -> Bool {
        guard process != nil else { return true }; pendingWindowClose = true; requestStop(); return false
    }
    func windowWillClose(_ notification: Notification) { if process == nil { NSApp.terminate(nil) } }
    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        guard process != nil else { return .terminateNow }; pendingApplicationQuit = true; requestStop(); return .terminateLater
    }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { process == nil }
}

MainActor.assumeIsolated {
    signal(SIGPIPE, SIG_IGN)
    let application = NSApplication.shared
    let testerDelegate = TesterDelegate()
    application.delegate = testerDelegate
    withExtendedLifetime(testerDelegate) { application.run() }
}

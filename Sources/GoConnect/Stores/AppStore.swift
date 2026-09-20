import AppKit
import Observation
import GoConnectCore
import UniformTypeIdentifiers

enum Page: String, CaseIterable, Identifiable {
    case connection = "连接", profiles = "线路配置", subscriptions = "订阅管理", applications = "应用白名单", exclusions = "直连 App", diagnostics = "诊断"
    var id: String { rawValue }
    var icon: String { switch self { case .connection: "network"; case .profiles: "server.rack"; case .subscriptions: "square.stack.3d.up"; case .applications: "square.grid.2x2"; case .exclusions: "arrow.triangle.branch"; case .diagnostics: "waveform.path.ecg" } }
}

struct ActivityEntry: Identifiable {
    let id = UUID()
    let date = Date()
    let message: String
    let isError: Bool
}

@MainActor @Observable
final class AppStore {
    var page: Page = .connection
    var configuration = Configuration()
    var subscriptions: [Subscription] = []
    var subscriptionWorking = false
    var subscriptionsReadable = true
    var password = ""
    var catalog: [AllowedApplication] = []
    var scanning = false
    private var pendingAppUpdate: (sequence: UInt64, connection: SavedConnection)?
    var updatingApplications: Bool { pendingAppUpdate != nil }
    var canEditApplications: Bool {
        configurationReadable && !updatingApplications && !busy
    }
    var search = ""
    var phase = "idle" { didSet { if phase != oldValue, let event = FaultEvent(rawValue: "phase." + phase) { FaultDiagnostics.shared.record(event) } } }
    var testOnly = false
    var message: String?
    var isError = false
    var activity: [ActivityEntry] = []
    var addresses: [String] = []
    var received: UInt64 = 0
    var sent: UInt64 = 0
    var connectedAt: Date?
    var localReady = false
    var hasOtherRouter = false
    var configurationReadable = true
    var profileDraft: ConnectionDraft?
    var draftPassword = ""
    var profileEditorError: String?
    var discardProfileChangesRequested = false
    var pendingProfilePage: Page?
    var routeSnapshot: RouteSnapshot?
    var routingCheckRunning = false
    var routingCheckPassed: Bool?
    var routingCheckSteps: [String] = []
    var passwordRequested = false
    private(set) var pendingPasswordTest = false
    private var token = ""
    private var socksPort: UInt16 = 0
    private var routeReported = false
    private var stopFailed = false
    private var handledCommand: UInt64 = 0
    private let transport = TransportClient()
    private let route = RouteClient()
    private let proxyFront = TransportClient()
    var proxyPort: UInt16?
    let proxyConnectivity = ProxyConnectivityChecker()
    var recoveryRequired = false
    var accessTitle: String { configuration.routingMode.title }
    var usesCompanyRoutes: Bool { configuration.activeConnection.usesCompanyRoutes }
    var accessExplanation: String { configuration.routingMode.explanation }
    private let routingCheck = RoutingCheckClient()
    let networkService = NetworkServiceClient()
    let keychain = KeychainStore()
    let nodeLatency: NodeLatencyStore
    let file: ConfigurationFile

    init() {
        let base = ProcessInfo.processInfo.environment["GOCONNECT_DATA_DIR"].map { URL(fileURLWithPath: $0) }
            ?? FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0].appendingPathComponent(Product.bundleID)
        file = ConfigurationFile(url: base.appendingPathComponent("configuration.json"))
        let quarantineURL = base.appendingPathComponent("network-quarantine.json")
        if let data = try? Data(contentsOf: quarantineURL) {
            recoveryRequired = (try? JSONDecoder().decode(NetworkQuarantine.self, from: data).applies()) ?? true
        }
        nodeLatency = NodeLatencyStore(url: base.appendingPathComponent("node-latency.json"))
        do { configuration = try file.load() }
        catch { configurationReadable = false; show(error.localizedDescription, error: true) }
        transport.onEvent = { [weak self] event in self?.handle(event) }
        proxyFront.onEvent = { [weak self] event in self?.handleProxy(event) }
        route.onState = { [weak self] state in self?.handleRoute(state) }
        route.onSnapshot = { [weak self] snapshot in
            guard let self else { return }
            self.routeSnapshot = snapshot
            if let result = snapshot.commandResult, result.sequence > self.handledCommand {
                self.handledCommand = result.sequence
                if let pending = self.pendingAppUpdate, pending.sequence == result.sequence {
                    self.pendingAppUpdate = nil
                    if result.success {
                        guard let index = self.configuration.connections.firstIndex(where: { $0.id == pending.connection.id }) else {
                            self.show("名单已在本次连接生效，但原线路不存在，无法保存。", error: true); return
                        }
                        // Preserve unrelated profile edits made while waiting for the service.
                        self.configuration.connections[index].applications = pending.connection.applications
                        self.configuration.connections[index].excludedApplications = pending.connection.excludedApplications
                        do { try self.file.save(self.configuration) }
                        catch { self.show("名单已在本次连接生效，但保存失败：" + error.localizedDescription, error: true); return }
                    }
                    self.show(result.message ?? (result.success ? "名单已实时生效。" : "更新失败，已保留原名单。"), error: !result.success)
                }
            }
        }
        routingCheck.onEvent = { [weak self] event in self?.handleRoutingCheck(event) }
        routingCheck.onStopped = { [weak self] in
            guard let self else { return }
            self.routingCheckRunning = false
            self.phase = "idle"
            if self.routingCheckPassed == nil { self.routingCheckPassed = false; self.show("分流自检已结束，未取得完整验证结果。", error: true) }
        }
        do { subscriptions = try subscriptionFile.load() }
        catch { subscriptionsReadable = false; show("订阅缓存无法读取，原文件已保留。", error: true) }
        refreshEnvironment()
    }
    var busy: Bool { passwordRequested || networkService.working || !["idle", "failed"].contains(phase) }
    var statusTitle: String {
        switch phase {
        case "connecting": "正在建立隧道"
        case "preparingService", "preparingCheckService": "正在准备本机服务"
        case "serviceMaintenance": "正在维护本机服务"
        case "authorizing": "正在准备透明分流"
        case "connected": "\(accessTitle)已启动"
        case "testing": "隧道测试中"
        case "protected": "VPN 中断，已匹配连接暂停"
        case "stopping": "正在断开"
        case "checking": "正在验证本机分流"
        case "failed": "连接未完成"
        default: "尚未连接"
        }
    }
    var statusSubtitle: String {
        switch phase {
        case "connected": accessExplanation
        case "testing": configuration.activeConnection.subscription == nil ? "临时隧道已建立，尚未接管任何 App 流量。" : "节点代理已就绪，尚未接管 App 流量；实际可达性需通过请求验证。"
        case "authorizing": "正在准备连接入口，不会自动打开应用。"
        case "preparingService", "preparingCheckService", "serviceMaintenance": "首次安装或更新服务需要管理员授权；日常连接无需重复授权。"
        case "protected": "可手动断开以恢复正常联网，再重新连接。"
        case "connecting": configuration.activeConnection.subscription == nil ? "正在验证服务器并完成 AnyConnect 身份认证。" : "正在准备所选订阅节点。"
        case "stopping": "正在停止隧道并释放网络配置。"
        case "checking": "使用测试应用检查实际 TUN、TCP 和 UDP 路径。"
        case "failed": "请检查线路配置，或在诊断中查看连接记录。"
        default: "选择线路后，开启连接开关。"
        }
    }
    func show(_ text: String, error: Bool = false) {
        message = text; isError = error
        activity.insert(ActivityEntry(message: text, isError: error), at: 0)
        if activity.count > 100 { activity.removeLast(activity.count - 100) }
    }
    func save() -> Bool {
        guard configurationReadable else { show("原配置无法读取，请先在诊断中查看配置文件。", error: true); return false }
        do {
            if configuration.profile.rememberPassword {
                if !password.isEmpty { try keychain.write(password, id: configuration.profile.id) }
            } else { try keychain.delete(configuration.profile.id) }
            try file.save(configuration); show("配置已保存到本机。")
            return true
        } catch { show(error.localizedDescription, error: true); return false }
    }
    func selectProfile(_ id: UUID) {
        guard !busy, id != configuration.activeProfileID, save() else { return }
        let previous = configuration
        do { try configuration.select(id); try file.save(configuration); password = ""; phase = "idle"; search = ""; show("已切换连接配置。") }
        catch { configuration = previous; show(error.localizedDescription, error: true) }
    }
    func quickConnect(_ id: UUID) {
        guard !busy, profileDraft == nil else { return }
        guard !recoveryRequired else { show("上次网络进程未能退出，已暂停重连。请导出诊断并重启 macOS 后再连接。", error: true); return }
        guard !nodeLatency.isRunning else { show("请先停止节点测试，再连接 VPN。", error: true); return }
        selectProfile(id)
        guard configuration.activeProfileID == id else { return }
        page = .connection; NSApp.activate(ignoringOtherApps: true)
        connect()
    }
    func toggle(_ app: AllowedApplication, excluded: Bool = false) {
        guard canEditApplications else { return }
        var candidate = configuration
        var list = excluded ? candidate.excludedApplications : candidate.applications
        if list.contains(where: { $0.id == app.id }) { list.removeAll { $0.id == app.id } }
        else {
            if let issue = ApplicationRoutingSupport.issue(for: app, requiresLauncher: false) { show("「\(app.name)」：\(issue)", error: true); return }
            list.append(app)
        }
        if excluded { candidate.excludedApplications = list } else { candidate.applications = list }
        applyApplicationSelection(candidate)
    }
    private func applyApplicationSelection(_ candidate: Configuration) {
        do { try file.save(candidate); configuration = candidate; show("分流名单已保存，下次连接自动应用，无需配置应用代理。") }
        catch { show(error.localizedDescription, error: true) }
    }
    func selected(_ app: AllowedApplication, excluded: Bool = false) -> Bool {
        (excluded ? configuration.excludedApplications : configuration.applications).contains { $0.id == app.id }
    }
    func launchApplication(_ app: AllowedApplication) {
        guard !app.isExecutableService else {
            show("可执行服务由原调用方启动；GoConnect 只按它的进程路径分流。")
            return
        }
        let options = NSWorkspace.OpenConfiguration()
        NSWorkspace.shared.openApplication(at: URL(fileURLWithPath: app.path), configuration: options) { [weak self] _, error in
            if let error { Task { @MainActor in self?.show(error.localizedDescription, error: true) } }
        }
    }
    func copyProxyEndpoint() {
        guard let port = proxyPort else { return }
        NSPasteboard.general.clearContents(); NSPasteboard.general.setString("http://127.0.0.1:\(port)", forType: .string)
        show("已复制 HTTP 代理地址。SOCKS5 使用同一端口，不需要密码。")
    }
    func filteredApps(excluded: Bool) -> [AllowedApplication] {
        let all = Dictionary((catalog + (excluded ? configuration.excludedApplications : configuration.applications)).map { ($0.path, $0) }, uniquingKeysWith: { first, _ in first }).values
        return all.filter { search.isEmpty || $0.name.localizedCaseInsensitiveContains(search) || $0.bundleID.localizedCaseInsensitiveContains(search) || $0.path.localizedCaseInsensitiveContains(search) }
            .sorted { a, b in
                if selected(a, excluded: excluded) != selected(b, excluded: excluded) { return selected(a, excluded: excluded) }
                return a.name.localizedStandardCompare(b.name) == .orderedAscending
            }
    }
    func scanApplications() {
        guard !scanning else { return }; scanning = true
        Task {
            let apps = await Task.detached { ApplicationCatalog.scan() }.value
            catalog = apps; scanning = false
        }
    }
    func addApplication(from url: URL, excluded: Bool = false) {
        addApplications(from: [url], excluded: excluded)
    }
    func addApplications(from urls: [URL], excluded: Bool = false) {
        guard canEditApplications, !urls.isEmpty else { return }
        var candidate = configuration
        var list = excluded ? candidate.excludedApplications : candidate.applications
        var newlyAdded: [AllowedApplication] = []

        for url in urls {
            let standardized = url.standardizedFileURL
            let resolved = standardized.resolvingSymlinksInPath()
            let isApp = resolved.pathExtension.lowercased() == "app" || standardized.pathExtension.lowercased() == "app"

            guard let app = AllowedApplication.from(url: url) else {
                if isApp {
                    show("无法读取该应用，请选择有效的 .app。", error: true)
                } else if !FileManager.default.isExecutableFile(atPath: resolved.path) {
                    show("请选择有效的 .app 或可执行文件。", error: true)
                } else {
                    show("无法读取该服务，请选择真实路径下的普通可执行文件，不要选择符号链接。", error: true)
                }
                return
            }

            if isApp && app.bundleID == Product.bundleID {
                show("无法读取该应用，请选择有效的 .app。", error: true)
                return
            }

            if let issue = ApplicationRoutingSupport.issue(for: app, requiresLauncher: false) {
                show(issue, error: true)
                return
            }

            if !list.contains(where: { $0.id == app.id }) && !newlyAdded.contains(where: { $0.id == app.id }) {
                newlyAdded.append(app)
            }
        }

        if newlyAdded.isEmpty {
            if urls.count == 1, let firstURL = urls.first, let app = AllowedApplication.from(url: firstURL) {
                show("「\(app.name)」已在名单中。")
            } else {
                show("所选目标已在名单中。")
            }
            return
        }

        if list.count + newlyAdded.count > 256 {
            show("每份名单最多支持 256 个 App 或服务，已超出上限。", error: true)
            return
        }

        list.append(contentsOf: newlyAdded)
        if excluded { candidate.excludedApplications = list } else { candidate.applications = list }
        applyApplicationSelection(candidate)
    }
    func addApplication(excluded: Bool = false) {
        guard canEditApplications else { return }
        let panel = NSOpenPanel(); panel.title = excluded ? "添加直连应用" : "添加白名单应用"
        panel.allowedContentTypes = [.applicationBundle]; panel.allowsMultipleSelection = true
        panel.directoryURL = URL(fileURLWithPath: "/Applications")
        if panel.runModal() == .OK {
            addApplications(from: panel.urls, excluded: excluded)
        }
    }
    func addExecutableService(excluded: Bool = false) {
        guard canEditApplications else { return }
        let panel = NSOpenPanel(); panel.title = excluded ? "添加直连可执行服务" : "添加白名单可执行服务"
        panel.allowedContentTypes = [.unixExecutable]; panel.allowsMultipleSelection = true
        panel.canChooseFiles = true; panel.canChooseDirectories = false
        let preferred = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".local/bin")
        panel.directoryURL = FileManager.default.fileExists(atPath: preferred.path) ? preferred : FileManager.default.homeDirectoryForCurrentUser
        if panel.runModal() == .OK {
            addApplications(from: panel.urls, excluded: excluded)
        }
    }
    func refreshEnvironment() {
        networkService.refresh()
        localReady = transport.isAvailable && ["mihomo"].allSatisfy { FileManager.default.isExecutableFile(atPath: transport.runtimeURL.appendingPathComponent("bin/" + $0).path) }
        // Explicit proxies may coexist; never scan every process on the UI thread.
        hasOtherRouter = false
    }
    func connect(test: Bool = false) {
        guard !busy, profileDraft == nil else { return }
        guard !recoveryRequired else { show("上次网络进程未能退出，已暂停重连。请导出诊断并重启 macOS 后再连接。", error: true); return }
        guard !nodeLatency.isRunning else { show("请先停止节点测试，再连接 VPN。", error: true); return }
        do {
            if configuration.activeConnection.subscription == nil { _ = try configuration.profile.validatedServer() }
            else if selectedSubscriptionNode == nil { show("当前节点已不存在，请在订阅管理中重新选择。", error: true); page = .subscriptions; return }
            guard localReady else { show("运行组件不完整，请重新运行项目构建脚本。", error: true); return }

            if configuration.activeConnection.companyRoutesEnabled && (configuration.activeConnection.subscription != nil || configuration.activeConnection.remoteNetworks.isEmpty) {
                show("公司内网模式仅支持 AnyConnect，请先在线路配置中填写明确的公司网段。", error: true); return
            }
            let secret = password.isEmpty && configuration.profile.rememberPassword ? (try keychain.read(configuration.profile.id) ?? "") : password
            guard configuration.activeConnection.subscription != nil || !secret.isEmpty else { pendingPasswordTest = test; passwordRequested = true; return }
            guard !secret.contains("\n"), !secret.contains("\r") else { show("密码不能包含换行。", error: true); return }
            guard save() else { return }
            testOnly = test; routeReported = false; stopFailed = false; routeSnapshot = nil; handledCommand = 0; connectedAt = nil; addresses = []; received = 0; sent = 0
            token = UUID().uuidString + UUID().uuidString
            phase = test ? "connecting" : "preparingService"
            show("正在准备透明 App 分流服务。")
            password = ""
            Task {
                do {
                    if !test { try await networkService.ensureReady() }
                    guard phase == "preparingService" || phase == "connecting" else { finishStopping(); return }
                    phase = "connecting"
                    if let node = selectedSubscriptionNode { try transport.start(node: node, token: token) }
                    else { try transport.start(profile: configuration.profile, password: secret, token: token) }
                } catch { phase = "failed"; show(error.localizedDescription, error: true) }
            }
        } catch { phase = "failed"; show(error.localizedDescription, error: true) }
    }
    func disconnect() {
        guard busy else { return }
        if passwordRequested { passwordRequested = false; return }
        if routingCheckRunning { phase = "stopping"; routingCheck.stop(); return }
        stopFailed = false
        stopAll()
    }
    private func stopAll() {
        pendingAppUpdate = nil
        proxyConnectivity.reset()
        phase = "stopping"; proxyPort = nil; proxyFront.stop(); route.stop(); transport.stop()
        finishStopping()
    }
    private func finishStopping() {
        if phase == "stopping", !route.isRunning, !transport.isRunning, !proxyFront.isRunning {
            phase = stopFailed ? "failed" : "idle"
            if !stopFailed { show("已断开。") }
        }
    }
    private func handle(_ event: TransportEvent) {
        switch event.event {
        case "ready":
            guard phase == "connecting", let port = event.port else { return }
            socksPort = port; addresses = event.addresses ?? []; connectedAt = Date()
            if testOnly { phase = "testing"; show(configuration.activeConnection.subscription == nil ? "VPN 隧道已建立；App 白名单尚未启用。" : "节点代理已就绪，尚未启用 App 分流。") }
            else {
                phase = "authorizing"
                do {
                    guard let gateway = event.gateway else { throw CocoaError(.fileReadCorruptFile) }
                    try route.start(port: port, token: token, apps: configuration.routingApplications,
                                    gateway: gateway, runtime: transport.runtimeURL, mode: configuration.routingMode,
                                    directDomains: configuration.effectiveDirectDomains,
                                    remoteNetworks: usesCompanyRoutes ? configuration.activeConnection.remoteNetworks : [])
                }
                catch { show(error.localizedDescription, error: true); stopFailed = true; stopAll() }
            }
        case "quarantined": quarantineNetwork()
        case "stats": received = event.received ?? received; sent = event.sent ?? sent
        case "error":
            show(event.message ?? "VPN 连接失败。", error: true)
            if phase == "stopping" { return }
            stopFailed = true; stopAll()
        case "stopped":
            if phase == "stopping" { finishStopping() }
            else if phase == "connected" || phase == "protected" { stopFailed = true; stopAll() }
            else if phase != "idle" && phase != "failed" {
                show("隧道已结束，请检查连接信息后重试。", error: true); stopFailed = true; stopAll()
            }
        default: break
        }
    }
    private func handleRoute(_ state: String) {
        if state == "ready" && phase == "authorizing" {
            routeReported = true
            phase = "connected"
            show(configuration.routingMode.title + "已启用，应用无需配置代理。")
        } else if ["failed", "serviceFailed"].contains(state) && phase != "stopping" {
            show(routeSnapshot?.message ?? "本机分流服务未能启动，请在诊断中重新检测服务和网络环境。", error: true)
            stopFailed = true; stopAll()
        } else if state == "stopped" {
            if phase == "stopping" { finishStopping() }
            else {
                show("分流进程已结束，请重新连接。", error: true); stopFailed = true; stopAll()
            }
        }
    }
    private func handleProxy(_ event: TransportEvent) {
        if event.event == "quarantined" { quarantineNetwork(); return }
        if event.event == "ready", phase == "authorizing", let port = event.port {
            proxyPort = port; phase = "connected"; show(usesCompanyRoutes ? "代理与公司网段已就绪，共用当前 AnyConnect 会话。" : "代理入口已就绪。请在目标应用中设置代理；仅加入名单不会生效。")
            proxyConnectivity.check(port: port)
        } else if event.event == "error" {
            show(event.message ?? "代理入口异常。", error: true)
            if phase != "stopping" { stopFailed = true; stopAll() }
        } else if event.event == "stopped" {
            if phase == "stopping" { finishStopping() }
            else if phase == "connected" || phase == "authorizing" { stopFailed = true; stopAll() }
        }
    }
    private func quarantineNetwork() {
        recoveryRequired = true
        let url = file.url.deletingLastPathComponent().appendingPathComponent("network-quarantine.json")
        do {
            try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
            try JSONEncoder().encode(NetworkQuarantine()).write(to: url, options: .atomic)
            try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
        } catch { /* The in-memory guard remains active even when storage fails. */ }
        show("网络进程尚未退出，已暂停重连；请导出诊断并重启 macOS 后再连接。", error: true)
    }
    func checkRouting(mode: RoutingMode? = nil) {
        show("请连接后使用普通启动的应用验证名单路由；公司内网需验证实际服务端口。")
    }
    func manageNetworkService(uninstall: Bool = false) {
        guard !busy else { return }
        phase = "serviceMaintenance"
        Task {
            do {
                if uninstall { try await networkService.uninstall() }
                else { try await networkService.ensureReady(forceInstall: true) }
                phase = "idle"
                show(uninstall ? "本机服务已卸载，线路配置和钥匙串保持不变。" : "本机服务已授权，后续连接无需重复输入管理员密码。")
            } catch { phase = "failed"; show(error.localizedDescription, error: true) }
        }
    }
    func submitPassword(_ value: String, remember: Bool) {
        password = value; configuration.profile.rememberPassword = remember
        passwordRequested = false
        connect(test: pendingPasswordTest)
    }
    private func handleRoutingCheck(_ event: TransportEvent) {
        switch event.event {
        case "check-progress":
            if let message = event.message { routingCheckSteps.append(message) }
        case "error": show(event.message ?? "分流自检未通过。", error: true)
        case "check-finished":
            routingCheckPassed = event.passed == true && event.tun == true
            if routingCheckPassed == true { show("本机 TUN 分流自检通过，原网络已恢复。") }
        default: break
        }
    }
    func revealConfiguration() { NSWorkspace.shared.activateFileViewerSelecting([file.url]) }
    func exportDiagnostics() {
        let panel = NSSavePanel(); panel.nameFieldStringValue = "GoConnect-diagnostics.txt"
        guard panel.runModal() == .OK, let url = panel.url else { return }
        let report = "GoConnect \(Product.version)\nmacOS \(ProcessInfo.processInfo.operatingSystemVersionString)\n运行组件: \(localReady)\n状态: \(phase)\n模式: \(accessTitle)\n白名单数量: \(configuration.applications.count)\n直连排除数量: \(configuration.excludedApplications.count)\nTailscale 已识别: \(routeSnapshot?.tailscaleActive == true)\n\n" + "本次运行事件数: \(activity.count)；错误数: \(activity.filter(\.isError).count)\n原始消息可能含服务器或路径信息，导出仅包含结构化故障事件。"
        Task {
            let appLogs = await FaultDiagnostics.shared.export()
            let serviceLogs = await networkService.diagnosticLogs()
            let output = report + "\n\n持久故障日志（UTC；仅事件代码与数值）\n" + appLogs + "\n\n网络服务日志\n" + serviceLogs
            do {
                try await Task.detached {
                    try output.write(to: url, atomically: true, encoding: .utf8)
                    try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
                }.value
                show("已导出摘要及持久故障日志。服务未更新时会注明日志不可用。")
            } catch { show(error.localizedDescription, error: true) }
        }
    }
}

extension AllowedApplication {
    static func from(url: URL) -> AllowedApplication? {
        let standardized = url.standardizedFileURL
        let resolved = standardized.resolvingSymlinksInPath()
        if resolved.pathExtension.lowercased() == "app" || standardized.pathExtension.lowercased() == "app" {
            return ApplicationCatalog.application(at: url)
        } else {
            return ApplicationCatalog.executable(at: url)
        }
    }
}

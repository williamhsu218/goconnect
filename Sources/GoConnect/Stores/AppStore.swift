import AppKit
import Observation
import GoConnectCore
import UniformTypeIdentifiers

enum Page: String, CaseIterable, Identifiable {
    case connection = "连接", profiles = "线路配置", subscriptions = "订阅管理", traffic = "流量统计", applications = "代理应用与服务", exclusions = "直连 App", diagnostics = "诊断"
    var id: String { rawValue }
    var icon: String { switch self { case .connection: "network"; case .profiles: "server.rack"; case .subscriptions: "square.stack.3d.up"; case .traffic: "chart.bar.xaxis"; case .applications: "square.grid.2x2"; case .exclusions: "arrow.triangle.branch"; case .diagnostics: "waveform.path.ecg" } }
}

struct ActivityEntry: Identifiable {
    let id = UUID()
    let date = Date()
    let message: String
    let isError: Bool
}

@MainActor @Observable
final class AppStore {
    var companyConfiguration = CompanyConnectionConfiguration()
    var companyConfigurationReadable = true
    var companyDraftOpen = false
    var companyProtectedProfileID: UUID? { busy && companyConfiguration.enabled ? companyConfiguration.companyProfileID : company?.activeProfileID }
    var company: CompanyConnectionStore!
    private let companyTransport = TransportClient()
    private var companyReservation: CompanyReservation?
    private var companyTimer: Timer?
    var companyFile: CompanyConnectionFile { CompanyConnectionFile(url: file.url.deletingLastPathComponent().appendingPathComponent("company-connection.json")) }
    var page: Page = .connection
    var configuration = Configuration()
    var subscriptions: [Subscription] = []
    var updateOperationBlocked = false
    var subscriptionWorking = false
    var subscriptionsReadable = true
    var password = ""
    var catalog: [AllowedApplication] = []
    var scanning = false
    var canEditApplications: Bool {
        configurationReadable && !busy
    }
    var search = ""
    var phase = "idle" { didSet { if phase != oldValue, let event = FaultEvent(rawValue: "phase." + phase) { diagnostics.record(event) } } }
    var testOnly = false
    var message: String?
    var isError = false
    var activity: [ActivityEntry] = []
    var addresses: [String] = []
    var received: UInt64 = 0
    var sent: UInt64 = 0
    var trafficAvailable = false
    var trafficUpdatedAt: Date?
    var downloadRate: Double = 0
    var uploadRate: Double = 0
    @ObservationIgnored private var trafficMeter = SessionTrafficMeter()
    var publicIP: String?
    var publicIPChecking = false
    var publicIPFailed = false
    var publicIPCheckedAt: Date?
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
    var passwordRequested = false
    private(set) var pendingPasswordTest = false
    private var token = ""
    private var socksPort: UInt16 = 0
    private var routeReported = false
    private var stopFailed = false
    private var disconnectReason = DisconnectReason.unknown
    private var disconnectRecoverySuggestion: String?
    private var unexpectedDisconnectReported = false
    private var transportRecovering = false
    private let transport: TransportClient
    private let publicIPChecker: PublicIPChecker
    private let route: RouteClient
    private var connectionGeneration = UUID()
    var recoveryRequired = false
    var accessTitle: String { configuration.routingMode.title }
    var usesCompanyRoutes: Bool { configuration.activeConnection.usesCompanyRoutes }
    var accessExplanation: String { configuration.routingMode.explanation }
    let networkService: NetworkServiceClient
    let keychain = KeychainStore()
    let nodeLatency: NodeLatencyStore
    @ObservationIgnored private var legacyYunyooUsage: YunyooUsageStore?
    // Kept for legacy data/types; the current UI never mounts quota/login views.
    // Do not load provider credentials or caches during ordinary app startup.
    var yunyooUsage: YunyooUsageStore {
        if let legacyYunyooUsage { return legacyYunyooUsage }
        let value = YunyooUsageStore(file: file.url.deletingLastPathComponent().appendingPathComponent("yunyoo-usage.json"))
        legacyYunyooUsage = value
        return value
    }
    let trafficHistory: ProfileTrafficStore
    var trafficProfileID: UUID?
    let file: ConfigurationFile
    private let diagnostics: FaultDiagnostics

    init(dataDirectory: URL? = nil, diagnostics: FaultDiagnostics = .shared, refreshRuntime: Bool = true,
         transportClient: TransportClient? = nil, routeClient: RouteClient? = nil, publicIPChecker: PublicIPChecker? = nil,
         yunyooUsageStore: YunyooUsageStore? = nil, networkServiceClient: NetworkServiceClient? = nil) {
        self.networkService = networkServiceClient ?? NetworkServiceClient()
        self.publicIPChecker = publicIPChecker ?? PublicIPChecker()
        self.transport = transportClient ?? TransportClient(); self.route = routeClient ?? RouteClient()
        self.diagnostics = diagnostics
        let base = dataDirectory ?? ProcessInfo.processInfo.environment["GOCONNECT_DATA_DIR"].map { URL(fileURLWithPath: $0) }
            ?? FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0].appendingPathComponent(Product.bundleID)
        file = ConfigurationFile(url: base.appendingPathComponent("configuration.json"))
        trafficHistory = ProfileTrafficStore(file: base.appendingPathComponent("traffic-history.sqlite"))
        legacyYunyooUsage = yunyooUsageStore
        let quarantineURL = base.appendingPathComponent("network-quarantine.json")
        if let data = try? Data(contentsOf: quarantineURL) {
            recoveryRequired = (try? JSONDecoder().decode(NetworkQuarantine.self, from: data).applies()) ?? true
        }
        nodeLatency = NodeLatencyStore(url: base.appendingPathComponent("node-latency.json"))
        do { configuration = try file.load() }
        catch { configurationReadable = false; show(error.localizedDescription, error: true) }
        transport.onEvent = { [weak self] event in self?.handle(event) }
        route.onState = { [weak self] state in self?.handleRoute(state) }
        route.onSnapshot = { [weak self] snapshot in
            guard let self else { return }
            self.routeSnapshot = snapshot
            if let state = snapshot.company { self.company?.routeState(state) }
        }
        self.publicIPChecker.onResult = { [weak self] ip in
            guard let self, self.connectedAt != nil, ["connected", "authorizing", "testing"].contains(self.phase) else { return }
            self.publicIP = ip; self.publicIPChecking = false; self.publicIPFailed = ip == nil
            self.publicIPCheckedAt = ip == nil ? nil : Date()
        }
        do { subscriptions = try subscriptionFile.load() }
        catch { subscriptionsReadable = false; show("订阅缓存无法读取，原文件已保留。", error: true) }
        company = CompanyConnectionStore(actions: CompanyConnectionActions(start: { [weak self] profile, secret, token, generation in
            guard let self, self.phase == "connected", !self.updateOperationBlocked,
                  let socket = self.route.companySocketPath, let state = self.route.companyState,
                  let port = state.proxyPort, let proxyToken = state.proxyToken else {
                throw CompanyConnectionError.invalid("请先建立已预留公司出口的主连接。")
            }
            self.companyTransport.onEvent = { [weak self] event in
                guard let self else { return }
                // OpenConnect reports the loopback HTTP proxy as VPNGATEWAY.
                // Actual outer destinations are authorized by the root CONNECT
                // proxy on every dial, not by this informational ready field.
                if event.event == "ready", event.gateway.map({ $0 == "127.0.0.1" || self.companyReservation?.gatewayIPs.contains($0) == true }) != true {
                    self.company.handle(TransportEvent(event: "error", message: "公司网关与预先批准的地址不一致，已停止副连接。"), generation: generation)
                } else { self.company.handle(event, generation: generation) }
                self.finishStopping()
            }
            try self.companyTransport.startCompany(profile: profile, password: secret, token: token, socket: socket, proxyPort: port, proxyToken: proxyToken)
        }, prepare: { [weak self] claim, generation in
            guard let self else { throw CocoaError(.userCancelled) }
            try self.route.prepareCompany(claim, generation: generation)
        }, block: { [weak self] generation in
            guard let self else { throw CocoaError(.userCancelled) }
            try self.route.blockCompany(generation: generation)
        }, stop: { [weak self] in self?.companyTransport.stop() }))
        do { companyConfiguration = try companyFile.load() }
        catch { companyConfigurationReadable = false; show("公司连接配置无法读取，原文件已保留。", error: true) }
        if Product.isIsolatedTestApp, !FileManager.default.fileExists(atPath: file.url.path), companyConfigurationReadable {
            var primary = SavedConnection(routingMode: .global)
            primary.profile.name = "HY2 主线路 · 测试示例"
            primary.subscription = SubscriptionSelection(subscriptionID: UUID(), nodeName: "隔离测试 HY2")
            var secondary = SavedConnection()
            secondary.profile.name = "公司 AnyConnect · 测试示例"; secondary.profile.server = "https://vpn.example.invalid"; secondary.profile.username = "test-user"
            configuration.connections = [primary, secondary]; configuration.activeProfileID = primary.id
            companyConfiguration.primaryProfileID = primary.id; companyConfiguration.companyProfileID = secondary.id
            companyConfiguration.networks = ["10.201.0.0/16"]; companyConfiguration.domains = ["corp.example.invalid"]
            do { try file.save(configuration); try companyFile.save(companyConfiguration) }
            catch { show(error.localizedDescription, error: true) }
        }
        companyTimer = Timer(timeInterval: 1, repeats: true) { [weak self] timer in
            Task { @MainActor [weak self] in guard let self else { timer.invalidate(); return }; self.company.tickTimeout() }
        }
        RunLoop.main.add(companyTimer!, forMode: .common)
        if refreshRuntime { refreshEnvironment() }
    }
    private var updateNetworkRuntimeActive: Bool {
        busy || transport.isRunning || route.isRunning || company.isRunning || company.busy || companyTransport.isRunning || recoveryRequired
    }
    var updateSafetyState: UpdateSafetyState {
        UpdateSafetyState(connectionOrServiceActive: updateNetworkRuntimeActive || networkService.blocksUpdateInstallation,
                          hasUnsavedDraft: profileDraft != nil || companyDraftOpen, isTestingNodes: nodeLatency.isRunning,
                          isUpdatingSubscriptions: subscriptionWorking,
                          isMaintainingService: networkService.working || phase == "serviceMaintenance")
    }
    func allowsOperationDuringUpdate() -> Bool {
        guard !updateOperationBlocked else {
            show("正在安装应用更新，请等待重启后再连接或维护网络服务。", error: true)
            return false
        }
        return true
    }
    /// Called only after the user confirms installation of the fully downloaded update.
    /// Keep the existing teardown path and wait for its actual completion before quitting.
    func prepareForUpdateInstallation(timeout: Duration = .seconds(20),
                                      servicePollInterval: Duration = .seconds(1)) async -> Bool {
        guard !Task.isCancelled, timeout > .zero else { return false }
        if let reason = updateSafetyState.preparationBlockingReason {
            show(reason, error: true)
            return false
        }
        let clock = ContinuousClock(), deadline = ContinuousClock.now.advanced(by: timeout)
        // Do not trust a diagnostic read made while the old session was active.
        // If preparation fails, installation/quit remains protected until a new
        // trusted read confirms idle; unknown/unauthorized replies are not idle.
        networkService.requireIdleConfirmation()
        if updateNetworkRuntimeActive { disconnect() }
        while clock.now < deadline {
            guard !Task.isCancelled else { return false }
            if let reason = updateSafetyState.preparationBlockingReason {
                show(reason, error: true)
                return false
            }
            let pause: Duration
            if updateNetworkRuntimeActive {
                pause = .milliseconds(100) // Local teardown only; no helper spawning.
            } else {
                let remaining = clock.now.duration(to: deadline)
                guard let current = await networkService.refreshStatus(timeout: min(.seconds(5), remaining)),
                      !Task.isCancelled, clock.now < deadline else { return false }
                if current.confirmsIdleForUpdate {
                    return updateSafetyState.blockingReason == nil
                }
                guard current.ready && current.busy else {
                    show("无法确认本机网络服务已空闲，已暂停安装；请检测环境后重试。", error: true)
                    return false
                }
                // A worker may still be unwinding after its GUI client exits.
                // Poll serially at most once a second in production, not at 10 Hz.
                pause = max(.milliseconds(1), servicePollInterval)
            }
            do { try await Task.sleep(for: min(pause, clock.now.duration(to: deadline))) }
            catch { return false }
        }
        show("VPN 或网络服务尚未安全停止，已暂停安装；可检查诊断后重试。", error: true)
        return false
    }
    var busy: Bool { passwordRequested || networkService.working || !["idle", "failed"].contains(phase) }
    func showTraffic(for id: UUID) {
        trafficProfileID = id
        navigate(to: .traffic)
    }
    var statusTitle: String {
        switch phase {
        case "connecting": "正在建立隧道"
        case "preparingService", "preparingCheckService": "正在准备本机服务"
        case "serviceMaintenance": "正在维护本机服务"
        case "authorizing": "正在准备透明分流"
        case "connected": "\(accessTitle)已启动"
        case "reconnecting": "正在恢复连接"
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
        case "reconnecting": "隧道暂时不可用，正在自动恢复；分流规则保持，可随时关闭连接。"
        case "testing": configuration.activeConnection.subscription == nil ? "临时隧道已建立，尚未接管任何 App 流量。" : "节点代理已就绪，尚未接管 App 流量；实际可达性需通过请求验证。"
        case "authorizing": "正在准备连接入口，不会自动打开应用。"
        case "preparingService", "preparingCheckService", "serviceMaintenance": "首次安装或更新服务需要管理员授权；日常连接无需重复授权。"
        case "protected": "可手动断开以恢复正常联网，再重新连接。"
        case "connecting": configuration.activeConnection.subscription == nil ? "正在验证服务器并完成 AnyConnect 身份认证。" : "正在准备所选订阅节点。"
        case "stopping": "正在停止隧道并释放网络配置。"
        case "checking": "使用测试应用检查实际 TUN、TCP 和 UDP 路径。"
        case "failed": disconnectRecoverySuggestion ?? "请检查线路配置，或在诊断中查看连接记录。"
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
        guard !company.isRunning, !company.busy, !busy, id != configuration.activeProfileID, save() else { return }
        let previous = configuration
        do { try configuration.select(id); try file.save(configuration); password = ""; phase = "idle"; search = ""; show("已切换连接配置。") }
        catch { configuration = previous; show(error.localizedDescription, error: true) }
    }
    func quickConnect(_ id: UUID) {
        guard allowsOperationDuringUpdate() else { return }
        if Product.isNetworkTestApp, let reason = networkService.testHostMessage { show(reason, error: true); networkService.refresh(); return }
        guard !busy, !company.isRunning, !company.busy, !companyDraftOpen, profileDraft == nil else { return }
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
                    show("无法解析该服务，请选择有效的可执行文件或固定入口。", error: true)
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
        let panel = NSOpenPanel(); panel.title = excluded ? "添加直连应用" : "添加代理应用"
        panel.allowedContentTypes = [.applicationBundle]; panel.allowsMultipleSelection = true
        panel.directoryURL = URL(fileURLWithPath: "/Applications")
        if panel.runModal() == .OK {
            addApplications(from: panel.urls, excluded: excluded)
        }
    }
    func addExecutableService(excluded: Bool = false) {
        guard canEditApplications else { return }
        let panel = NSOpenPanel(); panel.title = excluded ? "添加直连可执行服务" : "添加代理可执行服务"
        panel.allowedContentTypes = [.unixExecutable, .symbolicLink]; panel.allowsMultipleSelection = true
        panel.resolvesAliases = false
        panel.message = "可选择固定入口（如 /opt/homebrew/bin/claude），连接时会自动解析当前版本。"
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
        guard !Product.isIsolatedTestApp else { show("此隔离测试包仅验收界面与配置，不接管系统网络。", error: true); return }
        guard allowsOperationDuringUpdate() else { return }
        if Product.isNetworkTestApp, let reason = networkService.testHostMessage { show(reason, error: true); networkService.refresh(); return }
        guard !busy, !company.isRunning, !company.busy, !companyDraftOpen, profileDraft == nil else { return }
        guard !recoveryRequired else { show("上次网络进程未能退出，已暂停重连。请导出诊断并重启 macOS 后再连接。", error: true); return }
        guard !nodeLatency.isRunning else { show("请先停止节点测试，再连接 VPN。", error: true); return }
        do {
            if configuration.activeConnection.subscription == nil { _ = try configuration.profile.validatedServer() }
            else if selectedSubscriptionNode == nil { show("当前节点已不存在，请在订阅管理中重新选择。", error: true); page = .subscriptions; return }
            guard localReady else { show("运行组件不完整，请重新运行项目构建脚本。", error: true); return }
            if !test { _ = try ApplicationRoutingSupport.routingPaths(for: configuration.routingApplications) }

            if configuration.activeConnection.companyRoutesEnabled && (configuration.activeConnection.subscription != nil || configuration.activeConnection.remoteNetworks.isEmpty) {
                show("公司内网模式仅支持 AnyConnect，请先在线路配置中填写明确的公司网段。", error: true); return
            }
            let secret = password.isEmpty && configuration.profile.rememberPassword ? (try keychain.read(configuration.profile.id) ?? "") : password
            guard configuration.activeConnection.subscription != nil || !secret.isEmpty else { pendingPasswordTest = test; passwordRequested = true; return }
            guard !secret.contains("\n"), !secret.contains("\r") else { show("密码不能包含换行。", error: true); return }
            guard save() else { return }
            testOnly = test; routeReported = false; stopFailed = false; routeSnapshot = nil; connectedAt = nil; addresses = []; received = 0; sent = 0
            trafficHistory.endSession()
            resetSessionMetrics()
            disconnectReason = .unknown; unexpectedDisconnectReported = false
            disconnectRecoverySuggestion = nil
            transportRecovering = false
            token = UUID().uuidString + UUID().uuidString
            connectionGeneration = UUID()
            let attempt = connectionGeneration
            phase = test ? "connecting" : "preparingService"
            show("正在准备透明 App 分流服务。")
            password = ""
            Task {
                guard connectionGeneration == attempt else { return }
                do {
                    try await networkService.assertTestHostAvailable()
                    if !test {
                        if companyConfiguration.enabled {
                            guard companyConfigurationReadable else { throw CompanyConnectionError.invalid("请先处理公司连接配置读取错误。") }
                            guard selectedSubscriptionNode?.type == "hysteria2" else { throw CompanyConnectionError.invalid("双连接测试当前只支持 HY2 主线路。") }
                            companyReservation = try await CompanyGatewayResolver.reservation(companyConfiguration, configuration: configuration)
                        } else { companyReservation = nil }
                        guard connectionGeneration == attempt, phase == "preparingService" else { return }
                        try await networkService.ensureReady()
                    }
                    guard connectionGeneration == attempt else { return }
                    guard phase == "preparingService" || phase == "connecting" else { finishStopping(); return }
                    phase = "connecting"
                    if let node = selectedSubscriptionNode { try transport.start(node: node, token: token) }
                    else { try transport.start(profile: configuration.profile, password: secret, token: token) }
                } catch {
                    guard connectionGeneration == attempt else { return }
                    phase = "failed"; show(error.localizedDescription, error: true)
                }
            }
        } catch { phase = "failed"; show(error.localizedDescription, error: true) }
    }
    func disconnect() {
        company?.primaryStopping()
        guard busy else { return }
        if passwordRequested { passwordRequested = false; return }
        diagnostics.record(.disconnectRequested)
        stopFailed = false
        stopAll()
    }
    private func stopAll() {
        company?.primaryStopping()
        trafficHistory.endSession()
        publicIPChecker.stop(); publicIPChecking = false; publicIP = nil; publicIPCheckedAt = nil
        trafficAvailable = false
        connectionGeneration = UUID()
        transportRecovering = false
        phase = "stopping"; route.stop(); transport.stop()
        finishStopping()
    }
    private func finishStopping() {
        if phase == "stopping", !route.isRunning, !transport.isRunning, !company.isRunning, !companyTransport.isRunning {
            phase = stopFailed ? "failed" : "idle"
            if !stopFailed { show("已断开。") }
        }
    }
    func handle(_ event: TransportEvent) {
        switch event.event {
        case "diagnostic":
            if let code = event.code, let reason = DisconnectReason(diagnosticCode: code) { disconnectReason = disconnectReason.merging(reason) }
        case "reconnecting":
            guard connectedAt != nil, ["connected", "authorizing", "testing"].contains(phase), !transportRecovering else { return }
            transportRecovering = true
            publicIPChecker.stop(); publicIP = nil; publicIPChecking = false; publicIPCheckedAt = nil
            if phase == "connected" { phase = "reconnecting" }
            show("隧道暂时不可用，正在自动恢复。")
        case "reconnected":
            guard transportRecovering, ["reconnecting", "authorizing", "testing"].contains(phase) else { return }
            transportRecovering = false
            disconnectReason = .unknown
            if phase == "reconnecting" { phase = "connected" }
            refreshPublicIP()
            show("隧道连接已恢复。")
        case "ready":
            guard phase == "connecting", let port = event.port else { return }
            socksPort = port; addresses = event.addresses ?? []; connectedAt = Date()
            if !testOnly {
                trafficHistory.beginSession(profileID: configuration.activeProfileID, name: configuration.activeConnection.displayName)
            }
            if testOnly { phase = "testing"; show(configuration.activeConnection.subscription == nil ? "VPN 隧道已建立；应用分流尚未启用。" : "节点代理已就绪，尚未启用 App 分流。") }
            else {
                phase = "authorizing"
                do {
                    guard let gateway = event.gateway else { throw CocoaError(.fileReadCorruptFile) }
                    try route.start(port: port, token: token, apps: configuration.routingApplications,
                                    gateway: gateway, runtime: transport.runtimeURL, mode: configuration.routingMode,
                                    directDomains: configuration.effectiveDirectDomains,
                                    remoteNetworks: usesCompanyRoutes ? configuration.activeConnection.remoteNetworks : [], company: companyReservation,
                                    directApps: configuration.excludedApplications)
                }
                catch { show(error.localizedDescription, error: true); stopFailed = true; stopAll() }
            }
            refreshPublicIP()
        case "quarantined": quarantineNetwork()
        case "stats":
            guard connectedAt != nil, ["authorizing", "connected", "reconnecting", "testing"].contains(phase),
                  let received = event.received, let sent = event.sent else { return }
            let now = Date()
            let rates = trafficMeter.sample(received: received, sent: sent, time: now.timeIntervalSinceReferenceDate)
            self.received = received; self.sent = sent
            downloadRate = rates.download; uploadRate = rates.upload
            trafficAvailable = true; trafficUpdatedAt = now
            if !testOnly { trafficHistory.consume(received: received, sent: sent, at: now) }
        case "stats_unavailable":
            guard connectedAt != nil, ["authorizing", "connected", "reconnecting", "testing"].contains(phase) else { return }
            trafficAvailable = false
            trafficMeter.reset(); downloadRate = 0; uploadRate = 0
        case "error":
            if connectedAt != nil && !["idle", "failed", "stopping"].contains(phase) {
                reportUnexpectedDisconnect(source: 1)
            } else { show(event.message ?? "VPN 连接失败。", error: true) }
            if phase == "stopping" { return }
            stopFailed = true; stopAll()
        case "stopped":
            if phase == "stopping" { finishStopping() }
            else if connectedAt != nil && !["idle", "failed"].contains(phase) {
                reportUnexpectedDisconnect(source: 1); stopFailed = true; stopAll()
            }
            else if phase != "idle" && phase != "failed" {
                show("隧道已结束，请检查连接信息后重试。", error: true); stopFailed = true; stopAll()
            }
        default: break
        }
    }
    func refreshPublicIP() {
        guard connectedAt != nil, ["authorizing", "connected", "testing"].contains(phase), socksPort > 0 else { return }
        publicIPChecking = true; publicIPFailed = false; publicIP = nil; publicIPCheckedAt = nil
        do { try publicIPChecker.start(runtime: transport.runtimeURL, port: socksPort, token: token) }
        catch { publicIPChecking = false; publicIPFailed = true }
    }

    private func resetSessionMetrics() {
        publicIPChecker.stop()
        trafficAvailable = false; trafficUpdatedAt = nil
        trafficMeter.reset(); downloadRate = 0; uploadRate = 0
        publicIP = nil; publicIPChecking = false; publicIPFailed = false; publicIPCheckedAt = nil
    }
    private func reportUnexpectedDisconnect(source: Int) {
        guard !unexpectedDisconnectReported else { return }
        unexpectedDisconnectReported = true
        diagnostics.record(.unexpectedDisconnect, value: disconnectReason.rawValue)
        diagnostics.record(.disconnectSource, value: source)
        if let connectedAt { diagnostics.record(.connectionDuration, value: max(0, Int(Date().timeIntervalSince(connectedAt)))) }
        let component = source == 2 ? "本机分流服务意外结束" : source == 3 ? "代理入口意外结束" : disconnectReason.explanation
        disconnectRecoverySuggestion = source == 1 ? disconnectReason.recoverySuggestion : nil
        let guidance = disconnectRecoverySuggestion ?? "已记录诊断日志，请在「诊断」中导出以分析原因。"
        show("连接意外断开：\(component)。\(guidance)", error: true)
        // Make background/menu-bar use visible without taking focus or
        // requiring notification permission during a live VPN session.
        if !NSApp.isActive { NSApp.requestUserAttention(.criticalRequest) }
    }
    private func handleRoute(_ state: String) {
        if ["failed", "serviceFailed"].contains(state), phase == "stopping" {
            stopFailed = true
            show(state == "failed" ? (routeSnapshot?.message ?? "本机分流清理未完成，请导出诊断后处理。") : "分流监督进程已结束，尚未确认路由清理完成；请导出诊断。", error: true)
            finishStopping()
            return
        }
        if state == "ready" && phase == "authorizing" {
            routeReported = true
            phase = transportRecovering ? "reconnecting" : "connected"
            show(configuration.routingMode.title + "已启用，应用无需配置代理。")
        } else if ["failed", "serviceFailed"].contains(state) && phase != "stopping" {
            if ["connected", "reconnecting", "protected"].contains(phase) { reportUnexpectedDisconnect(source: 2) }
            else { show(routeSnapshot?.message ?? "本机分流服务未能启动，请在诊断中重新检测服务和网络环境。", error: true) }
            stopFailed = true; stopAll()
        } else if state == "stopped" {
            company?.primaryStopped()
            if phase == "stopping" { finishStopping() }
            else {
                if ["connected", "reconnecting", "protected"].contains(phase) { reportUnexpectedDisconnect(source: 2) }
                else { show("分流进程已结束，请重新连接。", error: true) }
                stopFailed = true; stopAll()
            }
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
    func manageNetworkService(uninstall: Bool = false) {
        guard allowsOperationDuringUpdate() else { return }
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
    func revealConfiguration() { NSWorkspace.shared.activateFileViewerSelecting([file.url]) }
    func exportDiagnostics() {
        let panel = NSSavePanel(); panel.nameFieldStringValue = "GoConnect-diagnostics.txt"
        guard panel.runModal() == .OK, let url = panel.url else { return }
        let report = "GoConnect \(Product.version)\nmacOS \(ProcessInfo.processInfo.operatingSystemVersionString)\n运行组件: \(localReady)\n状态: \(phase)\n模式: \(accessTitle)\n代理名单数量: \(configuration.applications.count)\n直连排除数量: \(configuration.excludedApplications.count)\nTailscale 已识别: \(routeSnapshot?.tailscaleActive == true)\n\n" + "本次运行事件数: \(activity.count)；错误数: \(activity.filter(\.isError).count)\n原始消息可能含服务器或路径信息，导出仅包含结构化故障事件。"
        Task {
            let appLogs = await diagnostics.export()
            let serviceLogs = await networkService.diagnosticLogs()
            let legend = "\n断线日志说明：connection.disconnect_requested=主动断开；connection.unexpected_disconnect=意外断开（value: 0原因未知/1服务器断开/2空闲超时/3会话到期/4会话凭据被拒/5DPD超时/6TLS读取/7TLS写入/8TLS连接/9重连失败）；connection.disconnect_source: 1隧道/2本机分流/3代理入口；connection.duration_seconds=连接持续秒数。openconnect.exit_code 与 openconnect.exit_signal 为实际 VPN 进程退出信息，transport.exited 为包装组件退出码。openconnect.reconnecting/reconnected/recovery_expired 分别表示正在恢复、恢复成功、超过 75 秒总恢复上限；reconnect_failed 是恢复失败线索，不单独等于已退出。network.*、system.sleep/wake 可用于时间关联，不单独证明根因。\n"
            let serviceLegend = "\n分流日志说明：session.exit_first=清理前的首次退出原因；session.exit_final=相同原因及清理结果，value 为会话毫秒数；details.stage/reason/check 标明阶段、退出原因与具体检查结果。system.command_timeout/failed/slow/late_result 记录系统命令超时、失败、慢执行和超时后返回；以 PID 与 operationID 关联，durationMs/timeoutMs 为毫秒。transparent.route_cleanup_failed 区分读表、删除和复核失败，routeIndex 对应会话路由清单序号；worker.exited 的 details 包含退出码与信号。transparent.health_deferred/health_recovered 表示只读健康查询进入有界容错窗口/完整检查恢复；route_missing 是首次退出原因，route_add_incomplete 是路由清理失败原因，不是独立事件名。worker.exit.json 保留最近退出摘要，请核对 PID 和时间；服务旧版可能没有这些字段。\n"
            let bridgeLegend = "\n桥接日志说明：bridge.jsonl/.1 的 bridge.trace 只记录固定阶段及数值；transaction 为显式标记探针的匿名事务号，flow 为目标连接编号，details.pid 为桥接 PID（外层 pid 为 App），timeMicros 为 UTC 微秒。socks_receive → udp_write_start/udp_write → pipe_write_start/pipe_write → pipe_read → stack_deliver → udp_read → socks_send 是关联环节，并非并发日志的严格写入顺序；udp_dial 标记拨号失败，udp_queue 标记队列拒绝或待发报文丢弃（0.11.14 起）。outcome/errno 为固定结果及错误号，bytes 为该阶段字节数，packetKind 为 STUN 类型。先检查同一桥接 PID 内的 sequence 连续性、dropped 与 appDropped；导出每个文件仅保留末尾 128 KiB，缺少记录不等于丢包。pipe_write 成功只证明交给本地通道，不证明远端收到。\n"
            let output = report + legend + serviceLegend + bridgeLegend + "\n\n持久故障日志（UTC；仅固定代码、类别与数值）\n" + appLogs + "\n\n网络服务日志\n" + serviceLogs
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

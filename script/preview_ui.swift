// Compiled only by preview_ui.py in a disposable source copy, never into the product.
import AppKit
import SwiftUI
import Combine
import GoConnectCore

@main
struct GoConnectPreviewApp: App {
    @NSApplicationDelegateAdaptor(PreviewDelegate.self) private var delegate
    private var controller: PreviewController { delegate.controller }
    var body: some Scene {
        WindowGroup("GoConnect QA", id: "preview") {
            PreviewContent(controller: controller)
        }
        .defaultSize(MainWindowSizing.preferredSize)
        .windowResizability(.contentMinSize)
        .windowToolbarStyle(.unified(showsTitle: false))
        .commands {
            WorkspaceCommands(store: controller.store)
            CommandMenu("测试场景") {
                Button("主线路已连接") { controller.seed("connected") }
                Button("未连接") { controller.seed("off") }
                Button("全局模式") { controller.seed("global") }
                Button("双线路已连接") { controller.seed("dual") }
                Button("公司副连接恢复中") { controller.seed("company-reconnecting") }
                Button("公司副连接待连接") { controller.seed("company-waiting") }
                Divider()
                Menu("外观") {
                    Button("浅色") { controller.theme = .light }
                    Button("深色") { controller.theme = .dark }
                    Button("跟随系统") { controller.theme = nil }
                }
                Toggle("模拟减少动态效果", isOn: Binding(
                    get: { controller.reduceMotion },
                    set: { controller.reduceMotion = $0 }
                ))
                Divider()
                ForEach(["reconnecting", "failed", "connecting", "diagnostic", "tunnel", "long", "stale", "empty", "sparse", "recovery"], id: \.self) { scenario in
                    Button(scenario) { controller.seed(scenario) }
                }
            }
        }
        Settings {
            PreferencesView(store: controller.store)
                .frame(width: PreferencesView.windowSize.width, height: PreferencesView.windowSize.height)
        }
    }
}

@MainActor
final class PreviewDelegate: NSObject, NSApplicationDelegate {
    let controller = PreviewController()
    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.regular)
        NSApp.activate(ignoringOtherApps: true)
        let evidence = ProcessInfo.processInfo.environment["GOCONNECT_PREVIEW_EVIDENCE"]!
        try? String(ProcessInfo.processInfo.processIdentifier).write(toFile: evidence + "/preview-pid.txt", atomically: true, encoding: .utf8)
    }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }
}

@MainActor @Observable
final class PreviewController {
    let store: AppStore
    var theme: ColorScheme? = .light
    var increasedContrast = false
    var reduceTransparency = false
    var reduceMotion = false
    private var lastCommand = Data()
    private var scenario = "connected"
    init() {
        let root = URL(fileURLWithPath: ProcessInfo.processInfo.environment["GOCONNECT_DATA_DIR"]!)
        let service = NetworkServiceClient(statusReader: { _ in
            NetworkServiceStatus(state: "notInstalled", message: "QA 预览使用示例数据。")
        })
        store = AppStore(dataDirectory: root, diagnostics: FaultDiagnostics(directory: root.appendingPathComponent("logs")),
                         refreshRuntime: false, networkServiceClient: service)
        seed("connected")
    }
    func seed(_ name: String) {
        scenario = name
        unsetenv("GOCONNECT_PREVIEW_CONTROL_STATE")
        store.closeProfileEditor()
        var configuration = Configuration()
        let subscriptionID = UUID()
        let apps = [
            AllowedApplication(name: "示例浏览器", bundleID: "test.preview.browser", path: "/Applications/Preview Browser.app"),
            AllowedApplication(name: "示例编辑器", bundleID: "test.preview.editor", path: "/Applications/Preview Editor.app"),
            AllowedApplication(name: "示例命令行", bundleID: "test.preview.cli", path: "/usr/bin/true")
        ]
        var profiles: [SavedConnection] = []
        for index in 0..<18 {
            var profile = VPNProfile()
            profile.name = index == 17 ? (name == "long" ? "Tokyo Japan · 工作专线与个人应用测试 · Very Long Connection Name 0123456789" : "Tokyo Japan") : "测试线路 \(index + 1)"
            profile.server = "https://vpn.example.com"; profile.username = "preview"
            var saved = SavedConnection(profile: profile, applications: apps, routingMode: name == "global" ? .global : .whitelist)
            saved.directDomains = [try! DomainRule("example.com"), try! DomainRule("198.51.100.0/24")]
            saved.excludedApplications = [AllowedApplication(name: "示例直连应用", bundleID: "test.preview.direct", path: "/Applications/Preview Direct.app")]
            if name != "tunnel" {
                saved.subscription = SubscriptionSelection(subscriptionID: subscriptionID, nodeName: "Tokyo Japan")
            }
            profiles.append(saved)
        }
        var company = CompanyConnectionConfiguration()
        if name == "dual" || name.hasPrefix("company-") {
            profiles[0].profile.name = "示例公司线路"
            profiles[0].subscription = nil
            company.enabled = true
            company.primaryProfileID = profiles.last!.id
            company.companyProfileID = profiles[0].id
            company.networks = ["192.168.20.0/24", "192.168.21.0/24", "192.168.22.0/24"]
            company.domains = ["intranet.example.invalid"]
        }
        configuration.connections = profiles; configuration.activeProfileID = profiles.last!.id
        store.configuration = configuration
        store.companyConfiguration = company
        let companyPhase = ["dual": "connected", "company-reconnecting": "reconnecting"][name] ?? "idle"
        store.company.setPreviewPhase(companyPhase)
        store.trafficProfileID = configuration.activeProfileID
        if name != "empty" {
            let calendar = store.trafficHistory.calendar
            let today = calendar.startOfDay(for: Date())
            if name == "sparse" {
                for (hour, received, sent) in [(0, 0, 0), (2, 125_829_120, 4_194_304)] {
                    guard let sample = calendar.date(byAdding: .hour, value: hour, to: today), sample <= Date() else { continue }
                    store.trafficHistory.recordSample(profileID: configuration.activeProfileID, name: configuration.activeConnection.displayName,
                        received: UInt64(received), sent: UInt64(sent), at: sample)
                }
            } else {
                for day in 0..<45 {
                    guard let date = calendar.date(byAdding: .day, value: -day, to: today) else { continue }
                    for hour in 0..<24 {
                        guard let sample = calendar.date(byAdding: .hour, value: hour, to: date), sample <= Date() else { continue }
                        store.trafficHistory.recordSample(profileID: configuration.activeProfileID, name: configuration.activeConnection.displayName,
                            received: UInt64((day % 7 + 1) * (hour % 5 + 1)) * 20_971_520,
                            sent: UInt64((day % 3 + 1) * (hour % 4 + 1)) * 1_048_576, at: sample)
                    }
                }
            }
            if let priorYear = calendar.date(byAdding: .year, value: -1, to: today) {
                store.trafficHistory.recordSample(profileID: configuration.activeProfileID, name: configuration.activeConnection.displayName, received: 8_589_934_592, sent: 536_870_912, at: priorYear)
            }
            store.trafficHistory.flush()
        }
        store.catalog = apps
        let now = Int64(Date().timeIntervalSince1970)
        let quota = "upload=4294967296; download=214748364800; total=1099511627776; updated=\(now)" + (name == "stale" ? "; stale=1; auth_required=1" : "")
        var sampleNodes = [
            SubscriptionNode(name: "Tokyo Japan", type: "hysteria2", proxy: [:]),
            SubscriptionNode(name: "Singapore", type: "hysteria2", proxy: [:])
        ]
        if name == "node-cards" {
            sampleNodes.append(SubscriptionNode(name: "Osaka Japan · 跨区视频会议与远程办公测试线路", type: "vless", proxy: [:]))
            sampleNodes.append(contentsOf: (1...9).map { index in
                SubscriptionNode(name: String(format: "Hong Kong %02d", index), type: "hysteria2", proxy: [:])
            })
        }
        store.subscriptions = name == "empty" ? [] : [Subscription(id: subscriptionID, payload: SubscriptionPayload(
            name: "测试订阅", url: "https://example.com/subscription", nodes: sampleNodes, updated: now, userInfo: quota
        ))]
        if name == "multi-subscription" {
            store.subscriptions.append(Subscription(payload: SubscriptionPayload(
                name: "备用订阅", url: "https://example.invalid/backup",
                nodes: [SubscriptionNode(name: "Osaka Japan", type: "hysteria2", proxy: [:])],
                updated: now, userInfo: ""
            )))
        }
        store.localReady = true; store.configurationReadable = true
        store.recoveryRequired = name == "recovery"
        store.testOnly = name == "diagnostic"
        store.passwordRequested = false
        store.phase = ["off", "empty"].contains(name) ? "idle" : ["failed", "connecting", "reconnecting"].contains(name) ? name : name == "diagnostic" ? "testing" : "connected"
        store.connectedAt = ["off", "empty", "connecting", "failed"].contains(name) ? nil : Date().addingTimeInterval(-8316)
        store.received = 4_101_045_780; store.sent = 448_790_528
        store.downloadRate = 13_212_057; store.uploadRate = 862_208
        store.trafficAvailable = true; store.trafficUpdatedAt = Date().addingTimeInterval(name == "stale" ? -30 : 0)
        store.publicIP = name == "long" ? "2001:db8:1234:5678:9abc:def0:1234:5678" : "203.0.113.24"
        store.publicIPCheckedAt = Date(); store.publicIPChecking = false; store.publicIPFailed = false
        store.addresses = name == "tunnel" ? ["192.0.2.24", "2001:db8::24"] : []
        store.resetApplicationListFilters()
        store.message = nil; store.isError = false; store.activity = []
        if name == "recovery" { store.phase = "failed" }
    }
    func poll() {
        if scenario != "stale" { store.trafficUpdatedAt = Date() }
        let directory = ProcessInfo.processInfo.environment["GOCONNECT_PREVIEW_EVIDENCE"]!
        // Record the settled window, including the native title bar's contribution,
        // rather than only the immediately requested dimensions during a resize.
        defer { writeWindowSnapshot(directory: directory) }
        let path = directory + "/preview-command.json"
        guard let data = FileManager.default.contents(atPath: path), data != lastCommand,
              let command = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return }
        lastCommand = data
        if let requested = command["scenario"] as? String, requested != scenario { seed(requested) }
        if let state = command["controlState"] as? String, let control = VPNControlState(rawValue: state) {
            setenv("GOCONNECT_PREVIEW_CONTROL_STATE", state, 1)
            let phases: [VPNControlState: String] = [.off: "idle", .waitingForPassword: "idle", .preparingService: "preparingService", .maintainingService: "serviceMaintenance", .connecting: "connecting", .authorizing: "authorizing", .connected: "connected", .needsRestart: "connected", .reconnecting: "reconnecting", .interrupted: "protected", .disconnecting: "stopping", .diagnostic: "testing", .endingDiagnostic: "stopping", .failed: "failed"]
            store.phase = phases[control] ?? "idle"
            store.testOnly = control == .diagnostic || control == .endingDiagnostic
        }
        if let page = command["page"] as? String {
            let pages: [String: Page] = [
                "connection": .connection, "profiles": .profiles, "subscriptions": .subscriptions,
                "traffic": .traffic, "applications": .applications, "exclusions": .exclusions,
                "directRules": .directRules, "diagnostics": .diagnostics
            ]
            if let target = pages[page] { store.page = target }
        }
        if let value = command["theme"] as? String { theme = value == "dark" ? .dark : value == "system" ? nil : .light }
        increasedContrast = command["contrast"] as? Bool ?? false
        reduceTransparency = command["reduceTransparency"] as? Bool ?? false
        reduceMotion = command["reduceMotion"] as? Bool ?? false
        if let message = command["message"] as? String { store.show(message, error: true) }
        if let width = command["width"] as? Double, let height = command["height"] as? Double,
           let window = NSApp.windows.first(where: { $0.identifier?.rawValue.hasPrefix("preview") == true }) {
            let recordManualResize = command["recordManualResize"] as? Bool == true
            if recordManualResize {
                NotificationCenter.default.post(name: NSWindow.willStartLiveResizeNotification, object: window)
            }
            window.setContentSize(NSSize(width: width, height: height))
            if recordManualResize {
                NotificationCenter.default.post(name: NSWindow.didEndLiveResizeNotification, object: window)
            }
            window.setFrameOrigin(NSPoint(x: 60, y: 160))
        }
        if let open = command["password"] as? Bool, open { store.passwordRequested = true }
    }
    private func writeWindowSnapshot(directory: String) {
        if let window = NSApp.windows.first(where: { $0.identifier?.rawValue.hasPrefix("preview") == true }) {
            let info: [String: Any] = ["number": window.windowNumber, "width": window.frame.width, "height": window.frame.height,
                "contentWidth": window.contentLayoutRect.width, "contentHeight": window.contentLayoutRect.height,
                "title": window.title, "scenario": scenario, "controlState": store.connectionControlState.rawValue,
                "page": store.page.rawValue, "companyPhase": store.company.phase, "routingMode": store.configuration.routingMode.rawValue,
                "activeProfileID": store.configuration.activeProfileID.uuidString, "profileCount": store.configuration.connections.count,
                "theme": theme == .dark ? "dark" : theme == .light ? "light" : "system",
                "contrast": increasedContrast, "reduceTransparency": reduceTransparency, "reduceMotion": reduceMotion]
            if let data = try? JSONSerialization.data(withJSONObject: info, options: [.prettyPrinted]) {
                try? data.write(to: URL(fileURLWithPath: directory + "/preview-window.json"), options: .atomic)
            }
        }
    }
}

@MainActor
extension AppStore {
    /// The preview power button changes its fixture without opening a connection.
    func setConnectionEnabled(_ enabled: Bool) {
        guard connectionControlState.canToggle, enabled != connectionControlState.isOn else { return }
        if enabled {
            guard localReady, configurationReadable, !recoveryRequired else { return }
        } else {
            company.setPreviewPhase("idle")
        }
        unsetenv("GOCONNECT_PREVIEW_CONTROL_STATE")
        passwordRequested = false
        phase = enabled ? "connected" : "idle"
        connectedAt = enabled ? Date() : nil
        trafficAvailable = enabled
        trafficUpdatedAt = enabled ? Date() : nil
        publicIP = enabled ? "203.0.113.24" : nil
        publicIPCheckedAt = enabled ? Date() : nil
    }
}

private struct PreviewContent: View {
    @Environment(\.accessibilityReduceMotion) private var systemReduceMotion
    @Bindable var controller: PreviewController
    private let timer = Timer.publish(every: 0.4, on: .main, in: .common).autoconnect()
    var body: some View {
        ContentView(store: controller.store)
            .preferredColorScheme(controller.theme)
            .environment(\.previewContrast, controller.increasedContrast ? .increased : .standard)
            .environment(\.previewReduceTransparency, controller.reduceTransparency)
            .environment(\.previewReduceMotion, systemReduceMotion || controller.reduceMotion)
            .onReceive(timer) { _ in controller.poll() }
    }
}

private struct PreviewContrastKey: EnvironmentKey { static let defaultValue = ColorSchemeContrast.standard }
private struct PreviewTransparencyKey: EnvironmentKey { static let defaultValue = false }
private struct PreviewMotionKey: EnvironmentKey { static let defaultValue = false }
extension EnvironmentValues {
    var previewContrast: ColorSchemeContrast {
        get { self[PreviewContrastKey.self] }
        set { self[PreviewContrastKey.self] = newValue }
    }
    var previewReduceTransparency: Bool {
        get { self[PreviewTransparencyKey.self] }
        set { self[PreviewTransparencyKey.self] = newValue }
    }
    var previewReduceMotion: Bool {
        get { self[PreviewMotionKey.self] }
        set { self[PreviewMotionKey.self] = newValue }
    }
}

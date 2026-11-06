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
        WindowGroup("GoConnect · 界面测试", id: "preview") {
            PreviewContent(controller: controller)
                .frame(minWidth: 940, minHeight: 640)
        }
        .defaultSize(width: 980, height: 700)
        .windowResizability(.contentMinSize)
        .commands {
            SidebarCommands()
            CommandMenu("测试场景") {
                ForEach(["connected", "off", "reconnecting", "failed", "connecting", "diagnostic", "tunnel", "long", "stale", "empty", "sparse", "recovery"], id: \.self) { scenario in
                    Button(scenario) { controller.seed(scenario) }
                }
            }
        }
        Settings { PreferencesView().frame(width: 480, height: 480) }
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
        store = AppStore(dataDirectory: root, refreshRuntime: false)
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
            var saved = SavedConnection(profile: profile, applications: apps)
            saved.directDomains = [try! DomainRule("example.com")]
            saved.excludedApplications = [AllowedApplication(name: "示例直连应用", bundleID: "test.preview.direct", path: "/Applications/Preview Direct.app")]
            if name != "tunnel" {
                saved.subscription = SubscriptionSelection(subscriptionID: subscriptionID, nodeName: "Tokyo Japan")
            }
            profiles.append(saved)
        }
        configuration.connections = profiles; configuration.activeProfileID = profiles.last!.id
        store.configuration = configuration
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
        store.subscriptions = name == "empty" ? [] : [Subscription(id: subscriptionID, payload: SubscriptionPayload(name: "测试订阅", url: "https://example.com/subscription", nodes: [SubscriptionNode(name: "Tokyo Japan", type: "hysteria2", proxy: [:]), SubscriptionNode(name: "Singapore", type: "hysteria2", proxy: [:])], updated: now, userInfo: quota))]
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
        store.message = nil; store.isError = false; store.search = ""; store.activity = []
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
            let pages: [String: Page] = ["connection": .connection, "profiles": .profiles, "subscriptions": .subscriptions, "traffic": .traffic, "applications": .applications, "exclusions": .exclusions, "diagnostics": .diagnostics]
            if let target = pages[page] { store.page = target }
        }
        if let value = command["theme"] as? String { theme = value == "dark" ? .dark : value == "system" ? nil : .light }
        increasedContrast = command["contrast"] as? Bool ?? false
        reduceTransparency = command["reduceTransparency"] as? Bool ?? false
        reduceMotion = command["reduceMotion"] as? Bool ?? false
        if let message = command["message"] as? String { store.show(message, error: true) }
        if let width = command["width"] as? Double, let height = command["height"] as? Double,
           let window = NSApp.windows.first(where: { $0.identifier?.rawValue.hasPrefix("preview") == true }) {
            window.setContentSize(NSSize(width: width, height: height))
            window.setFrameOrigin(NSPoint(x: 60, y: 160))
        }
        if let open = command["password"] as? Bool, open { store.passwordRequested = true }
    }
    private func writeWindowSnapshot(directory: String) {
        if let window = NSApp.windows.first(where: { $0.identifier?.rawValue.hasPrefix("preview") == true }) {
            let info: [String: Any] = ["number": window.windowNumber, "width": window.frame.width, "height": window.frame.height,
                "contentWidth": window.contentLayoutRect.width, "contentHeight": window.contentLayoutRect.height,
                "title": window.title, "scenario": scenario, "controlState": store.connectionControlState.rawValue,
                "activeProfileID": store.configuration.activeProfileID.uuidString, "profileCount": store.configuration.connections.count,
                "theme": theme == .dark ? "dark" : theme == .light ? "light" : "system",
                "contrast": increasedContrast, "reduceTransparency": reduceTransparency, "reduceMotion": reduceMotion]
            if let data = try? JSONSerialization.data(withJSONObject: info, options: [.prettyPrinted]) {
                try? data.write(to: URL(fileURLWithPath: directory + "/preview-window.json"), options: .atomic)
            }
        }
    }
}

private struct PreviewContent: View {
    @Bindable var controller: PreviewController
    private let timer = Timer.publish(every: 0.4, on: .main, in: .common).autoconnect()
    var body: some View {
        ContentView(store: controller.store)
            .preferredColorScheme(controller.theme)
            .environment(\.previewContrast, controller.increasedContrast ? .increased : .standard)
            .environment(\.previewReduceTransparency, controller.reduceTransparency)
            .environment(\.previewReduceMotion, controller.reduceMotion)
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

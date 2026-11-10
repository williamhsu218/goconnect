import SwiftUI
import Combine
import GoConnectCore

@main
struct GoConnectApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    private var store: AppStore { delegate.store }
    var body: some Scene {
        WindowGroup("GoConnect", id: "main") {
            MainWindowContent(store: store, delegate: delegate)
                .frame(minWidth: 940, minHeight: 640)
        }
        .defaultSize(width: 980, height: 700)
        .windowResizability(.contentMinSize)
        .windowStyle(.titleBar)
        .commands {
            AppUpdateCommands(updater: delegate.updater)
            if !AppDelegate.isUpdateQA {
            SidebarCommands()
            CommandGroup(replacing: .newItem) {
                Button("新建线路") { store.beginNewProfile() }.keyboardShortcut("n")
                    .disabled(store.profileDraft != nil || !store.configurationReadable || store.configuration.connections.count >= 64)
                Button("保存线路") { store.saveProfileDraft() }.keyboardShortcut("s").disabled(!store.canSaveProfileDraft)
                Button("添加代理应用…") { store.addApplication() }.keyboardShortcut("o").disabled(store.busy || store.profileDraft != nil)
            }
            CommandMenu("VPN") {
                Button(store.menuBarControlPolicy.actionTitle) {
                    delegate.menuBar?.toggleConnection()
                }.keyboardShortcut("k", modifiers: [.command, .shift])
                    .disabled(!store.menuBarControlPolicy.actionEnabled)
                Divider()
                Button("重新显示菜单栏快捷菜单") { delegate.menuBar?.restoreAndShow() }
                    .keyboardShortcut("m", modifiers: [.command, .shift])
            }
            }
        }
        Settings {
            PreferencesView(store: store, showMenuBar: { delegate.menuBar?.restoreAndShow() }, updater: delegate.updater)
                .frame(width: 500, height: 620)
        }
    }
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    static var isUpdateQA: Bool {
        isUpdateQABundle(.main)
    }
    static func isUpdateQABundle(_ bundle: Bundle) -> Bool {
        bundle.bundleIdentifier == "com.willhsu.GoConnect.UpdateQA" &&
        bundle.object(forInfoDictionaryKey: "GoConnectUpdateQAMode") as? Bool == true
    }
    let store: AppStore = AppDelegate.makeStore()

    // Use the same factory in the signed QA entry point and its regressions.
    // The production bundle always retains the real, fixed helper status reader.
    static func makeStore(bundle: Bundle = .main) -> AppStore {
        guard isUpdateQABundle(bundle) else { return AppStore() }
        let path = bundle.object(forInfoDictionaryKey: "GoConnectUpdateQADataDirectory") as? String
        let temporary = FileManager.default.temporaryDirectory.resolvingSymlinksInPath()
        let proposed = path.map { URL(fileURLWithPath: $0).resolvingSymlinksInPath() }
        let directory = proposed.flatMap { $0.path.hasPrefix(temporary.path + "/") ? $0 : nil }
            ?? temporary.appendingPathComponent("GoConnect-UpdateQA-" + UUID().uuidString)
        let service = NetworkServiceClient(statusReader: { _ in
            NetworkServiceStatus(state: "notInstalled", message: "隔离更新验收不访问正式网络服务。")
        })
        return AppStore(dataDirectory: directory, diagnostics: FaultDiagnostics(directory: directory.appendingPathComponent("logs")),
                        refreshRuntime: false, networkServiceClient: service)
    }
    lazy var updater = AppUpdater(safetyState: { [weak self] in
        self?.store.updateSafetyState ?? UpdateSafetyState(connectionOrServiceActive: true)
    }, prepareForInstallation: { [weak self] in
        guard let self else { return false }
        return await self.store.prepareForUpdateInstallation()
    }, notify: { [weak self] text in self?.store.show(text, error: true) })
    private var updateObservation: AnyCancellable?
    private(set) var menuBar: MenuBarController?
    private var openMainWindow: (() -> Void)?
    private var pendingMainWindow = false
    private var openSettingsWindow: (() -> Void)?
    private var pendingSettingsWindow = false

    func registerSettingsOpener(_ opener: @escaping () -> Void) {
        openSettingsWindow = opener
        if pendingSettingsWindow {
            pendingSettingsWindow = false
            showSettingsWindow()
        }
    }

    private func showSettingsWindow() {
        NSApp.activate(ignoringOtherApps: true)
        if let openSettingsWindow { openSettingsWindow() }
        else { pendingSettingsWindow = true }
    }

    func registerMainWindowOpener(_ openMainWindow: @escaping () -> Void) {
        self.openMainWindow = openMainWindow
        if pendingMainWindow {
            pendingMainWindow = false
            showMainWindow()
        }
    }

    private func showMainWindow() {
        NSApp.activate(ignoringOtherApps: true)
        if let window = NSApp.windows.first(where: { $0.identifier?.rawValue.hasPrefix("main") == true }) {
            window.deminiaturize(nil)
            window.makeKeyAndOrderFront(nil)
        } else if let openMainWindow {
            openMainWindow()
        } else {
            // A status-menu action can arrive before SwiftUI mounts its first window.
            pendingMainWindow = true
        }
    }
    func applicationDidFinishLaunching(_ notification: Notification) {
        if !Self.isUpdateQA { AppFaultMonitor.shared.start() }
        NSApp.setActivationPolicy(.regular)
        // App lifetime owns the status item; it must not depend on a window appearing.
        if !Self.isUpdateQA && !Product.isIsolatedTestApp {
            menuBar = MenuBarController(store: store,
                openSettings: { [weak self] in self?.showSettingsWindow() },
                openMainWindow: { [weak self] in self?.showMainWindow() })
        }
        updateObservation = updater.$installationInProgress
            .sink { [weak self] installation in self?.store.updateOperationBlocked = installation }
        if Self.isUpdateQA || ProcessInfo.processInfo.environment["GOCONNECT_DATA_DIR"] == nil { updater.start() }
        AppBranding.refreshDockIcon()
        NSApp.activate(ignoringOtherApps: true)
    }
    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        updater.canTerminateForUpdate() ? .terminateNow : .terminateCancel
    }
    func applicationWillTerminate(_ notification: Notification) {
        if !Self.isUpdateQA {
            FaultDiagnostics.shared.record(.appStopping)
            menuBar?.tearDown(); store.nodeLatency.stop(); store.disconnect()
        }
        store.trafficHistory.flush()
    }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        showMainWindow()
        // The shared opener restores or creates the main window, including when
        // another window (such as Settings) is still visible. Skip default handling.
        return false
    }
}

private struct MainWindowContent: View {
    let store: AppStore
    let delegate: AppDelegate
    @Environment(\.openWindow) private var openWindow
    @Environment(\.openSettings) private var openSettings

    var body: some View {
        Group {
            if AppDelegate.isUpdateQA { AppUpdateQAHarness(store: store, updater: delegate.updater) }
            else { ContentView(store: store) }
        }
            .onAppear {
                delegate.registerMainWindowOpener { openWindow(id: "main") }
                delegate.registerSettingsOpener { openSettings() }
            }
    }
}

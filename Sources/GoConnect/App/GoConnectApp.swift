import SwiftUI
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
        .windowStyle(.hiddenTitleBar)
        .commands {
            SidebarCommands()
            CommandGroup(replacing: .newItem) {
                Button("新建线路") { store.beginNewProfile() }.keyboardShortcut("n")
                    .disabled(store.profileDraft != nil || !store.configurationReadable || store.configuration.connections.count >= 64)
                Button("保存线路") { store.saveProfileDraft() }.keyboardShortcut("s").disabled(!store.canSaveProfileDraft)
                Button("添加白名单应用…") { store.addApplication() }.keyboardShortcut("o").disabled(store.busy || store.profileDraft != nil)
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
        Settings {
            PreferencesView(showMenuBar: { delegate.menuBar?.restoreAndShow() })
                .frame(width: 440, height: 280)
        }
    }
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    let store = AppStore()
    private(set) var menuBar: MenuBarController?
    private var openMainWindow: (() -> Void)?
    private var pendingMainWindow = false

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
        AppFaultMonitor.shared.start()
        NSApp.setActivationPolicy(.regular)
        // App lifetime owns the status item; it must not depend on a window appearing.
        menuBar = MenuBarController(store: store) { [weak self] in self?.showMainWindow() }
        AppBranding.refreshDockIcon()
        NSApp.activate(ignoringOtherApps: true)
    }
    func applicationWillTerminate(_ notification: Notification) { FaultDiagnostics.shared.record(.appStopping); menuBar?.tearDown(); store.nodeLatency.stop(); store.disconnect() }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }
}

private struct MainWindowContent: View {
    let store: AppStore
    let delegate: AppDelegate
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        ContentView(store: store)
            .onAppear {
                delegate.registerMainWindowOpener { openWindow(id: "main") }
            }
    }
}

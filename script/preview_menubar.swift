// Only compiled by preview_menubar.py; network and credential implementations are absent.
import AppKit
import SwiftUI
import Observation
import Combine
import GoConnectCore

enum Page: String {
    case profiles = "线路配置", subscriptions = "订阅管理", diagnostics = "诊断"
    var icon: String { "square.grid.2x2" }
}

@MainActor @Observable final class PreviewNodeLatency { var isRunning = false }

/// Implements only the display inputs and inert actions required by the real controller.
@MainActor @Observable final class AppStore {
    var state: VPNControlState = .connected
    var configuration = Configuration()
    var configurationReadable = true
    var profileDraft: Int?
    let nodeLatency = PreviewNodeLatency()
    var trafficAvailable = true
    var trafficUpdatedAt: Date? = Date()
    var downloadRate: Double = 12.6 * 1024 * 1024
    var uploadRate: Double = 842 * 1024
    var liveSamples = true
    var dark = false
    var nativeMenuImage: NSImage?
    var nativeMenuDark = false
    var actionMessage = "此测试 App 不包含网络服务、真实线路或钥匙串实现。"

    init() { configuration.profile.name = "隔离演示线路" }
    var connectionControlState: VPNControlState { state }
    var accessTitle: String { configuration.routingMode.title }
    var menuBarControlPolicy: MenuBarControlPolicy {
        MenuBarControlPolicy(state: state, busy: false, configurationReadable: true,
                             editingProfile: false, testingNodes: false)
    }
    func setConnectionEnabled(_ enabled: Bool) { actionMessage = "连接操作已隔离，未执行。" }
    func quickConnect(_ id: UUID) { actionMessage = "快速连接已隔离，未执行。" }
    func navigate(to page: Page) { actionMessage = "演示导航：" + page.rawValue }
    func sample() {
        guard liveSamples else { return }
        trafficUpdatedAt = Date()
    }
}

@main struct MenuBarPreviewApp: App {
    @NSApplicationDelegateAdaptor(MenuBarPreviewDelegate.self) private var delegate
    var body: some Scene {
        WindowGroup("GoConnect · 菜单栏验收", id: "preview") {
            MenuBarPreviewContent(store: delegate.store, restore: { delegate.menuBar?.restoreAndShow() },
                                  registerSettings: { delegate.settingsOpener = $0 })
        }
        .defaultSize(width: 580, height: 570)
        .windowResizability(.contentSize)
        Settings { Form { MenuBarSettings() }.formStyle(.grouped).frame(width: 500, height: 235) }
    }
}

@MainActor final class MenuBarPreviewDelegate: NSObject, NSApplicationDelegate {
    let store = AppStore()
    var menuBar: MenuBarController?
    var settingsOpener: (() -> Void)?
    private var auditTimer: Timer?
    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.regular)
        menuBar = MenuBarController(store: store, openSettings: { [weak self] in
            NSApp.activate(ignoringOtherApps: true)
            self?.settingsOpener?()
        }) {
            NSApp.activate(ignoringOtherApps: true)
            NSApp.windows.first?.makeKeyAndOrderFront(nil)
        }
        let path = ProcessInfo.processInfo.environment["GOCONNECT_PREVIEW_EVIDENCE"]!
        try? String(ProcessInfo.processInfo.processIdentifier).write(toFile: path + "/preview-pid.txt", atomically: true, encoding: .utf8)
        auditTimer = Timer.scheduledTimer(withTimeInterval: 0.5, repeats: true) { [weak self] _ in
            Task { @MainActor [weak self] in self?.recordNativePresentation(path: path) }
        }
        NSApp.activate(ignoringOtherApps: true)
    }
    // QA-only reflection records actual native control layout, without adding a product API.
    private func recordNativePresentation(path: String) {
        guard let menuBar,
              let item = Mirror(reflecting: menuBar).children.first(where: { $0.label == "statusItem" })?.value as? NSStatusItem,
              let button = item.button else { return }
        let record: [String: Any] = [
            "title": button.title, "itemLength": item.length, "buttonWidth": button.bounds.width,
            "textWidth": button.attributedTitle.size().width,
            "template": button.image?.isTemplate ?? false,
            "imageWidth": button.image?.size.width ?? 0,
            "attached": button.window != nil, "visible": item.isVisible,
            "accessibilityValue": button.accessibilityValue() as? String ?? "",
            "showsRates": UserDefaults.standard.bool(forKey: MenuBarPreferences.showsNetworkRatesKey),
            "state": store.state.rawValue, "liveSamples": store.liveSamples,
            "appearance": button.effectiveAppearance.name.rawValue,
            "direction": UserDefaults.standard.string(forKey: MenuBarPreferences.rateDirectionKey) ?? "both",
            "imageHeight": button.image?.size.height ?? 0,
        ]
        if let data = try? JSONSerialization.data(withJSONObject: record, options: [.sortedKeys]) {
            try? data.write(to: URL(fileURLWithPath: path + "/native-status.json"), options: .atomic)
        }
        if let bitmap = button.bitmapImageRepForCachingDisplay(in: button.bounds) {
            button.cacheDisplay(in: button.bounds, to: bitmap)
            if let data = bitmap.representation(using: .png, properties: [:]) {
                try? data.write(to: URL(fileURLWithPath: path + "/native-button.png"), options: .atomic)
                store.nativeMenuImage = NSImage(data: data)
                store.nativeMenuDark = button.effectiveAppearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua
            }
        }
    }
    func applicationWillTerminate(_ notification: Notification) { auditTimer?.invalidate(); menuBar?.tearDown() }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }
}

private struct MenuBarPreviewContent: View {
    @Bindable var store: AppStore
    let restore: () -> Void
    let registerSettings: (@escaping () -> Void) -> Void
    @Environment(\.openSettings) private var openSettings
    private let tick = Timer.publish(every: 1, on: .main, in: .common).autoconnect()

    var body: some View {
        Form {
            MenuBarSettings(showMenuBar: restore)
            if let image = store.nativeMenuImage {
                Section("当前原生菜单栏按钮 · 原样") {
                    Image(nsImage: image).padding(8)
                        .background(store.nativeMenuDark ? Color(white: 0.15) : .white)
                        .clipShape(RoundedRectangle(cornerRadius: 5))
                }
            }
            Section("隔离演示数据") {
                Picker("连接状态", selection: $store.state) {
                    ForEach(VPNControlState.allCases, id: \.self) { Text($0.title).tag($0) }
                }
                Toggle("持续更新样本", isOn: $store.liveSamples)
                Toggle("深色预览", isOn: $store.dark)
                HStack {
                    Button("常用速率") { store.downloadRate = 12.6 * 1024 * 1024; store.uploadRate = 842 * 1024 }
                    Button("真实零值") { store.downloadRate = 0; store.uploadRate = 0 }
                    Button("极大值") { store.downloadRate = .greatestFiniteMagnitude; store.uploadRate = .greatestFiniteMagnitude }
                    Button("单位边界") { store.downloadRate = 99.96 * 1024 * 1024; store.uploadRate = 1023.8 * 1024 * 1024 }
                }
                Text(store.actionMessage).font(.caption).foregroundStyle(.secondary)
            }
            Section("实际模板图标 · 原始尺寸") {
                HStack(spacing: 25) {
                    icon(.off); icon(.connecting); icon(.connected); icon(.failed)
                }
            }
        }
        .formStyle(.grouped)
        .frame(width: 580, height: 650)
        .preferredColorScheme(store.dark ? .dark : .light)
        .onAppear { registerSettings { openSettings() } }
        .onReceive(tick) { _ in store.sample() }
    }

    private func icon(_ state: VPNControlState) -> some View {
        VStack(spacing: 6) {
            Image(nsImage: MenuBarStatusIcon.image(for: state)).renderingMode(.template)
            Text(state.title).font(.caption)
        }
    }
}

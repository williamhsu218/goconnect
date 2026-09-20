import AppKit
import Observation
import OSLog
import GoConnectCore

/// Own only the native status item. AppStore remains the source of connection state.
@MainActor
final class MenuBarController: NSObject, NSMenuDelegate {
    private static let itemName = "GoConnectStatusItem"
    private let store: AppStore
    private let openMainWindow: () -> Void
    private var statusItem: NSStatusItem?
    private let menu = NSMenu()
    private let logger = Logger(subsystem: Product.bundleID, category: "MenuBar")

    init(store: AppStore, openMainWindow: @escaping () -> Void) {
        self.store = store
        self.openMainWindow = openMainWindow
        super.init()
        menu.autoenablesItems = false
        menu.delegate = self
        install()
        observeStatus()
    }

    private func install() {
        guard statusItem == nil else { return }
        // A stable, named position replaces SwiftUI's anonymous Item-0 preference.
        // The first insertion starts near the right edge, avoiding the old hidden position.
        UserDefaults.standard.register(defaults: ["NSStatusItem Preferred Position \(Self.itemName)": 0])
        let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        item.autosaveName = Self.itemName
        item.behavior = []
        item.menu = menu
        item.button?.image = MenuBarStatusIcon.image(for: store.connectionControlState)
        item.button?.imageScaling = .scaleProportionallyDown
        item.button?.setAccessibilityIdentifier("GoConnectMenuBar")
        item.button?.setAccessibilityLabel("GoConnect 菜单栏快捷操作")
        item.isVisible = true
        statusItem = item
        logger.info("Status item installed; visible=\(item.isVisible, privacy: .public)")
        DispatchQueue.main.async { [weak self, weak item] in
            guard let self, let item else { return }
            self.logger.info("Status item attached; visible=\(item.isVisible, privacy: .public), width=\(item.button?.bounds.width ?? 0, privacy: .public), window=\(item.button?.window != nil, privacy: .public)")
        }
    }

    func restoreAndShow() {
        menu.cancelTracking()
        if let item = statusItem { NSStatusBar.system.removeStatusItem(item) }
        statusItem = nil
        UserDefaults.standard.set(0, forKey: "NSStatusItem Preferred Position \(Self.itemName)")
        install()
        refreshStatus()
        // Wait for the newly inserted button to acquire its menu-bar window.
        DispatchQueue.main.async { [weak self] in self?.statusItem?.button?.performClick(nil) }
    }

    func toggleConnection() {
        guard store.menuBarControlPolicy.actionEnabled else { return }
        if !store.connectionControlState.isOn { openMainWindow() }
        store.setConnectionEnabled(!store.connectionControlState.isOn)
    }

    func tearDown() {
        if let item = statusItem { NSStatusBar.system.removeStatusItem(item) }
        statusItem = nil
    }

    private func observeStatus() {
        withObservationTracking {
            refreshStatus()
        } onChange: { [weak self] in
            Task { @MainActor [weak self] in self?.observeStatus() }
        }
    }

    private func refreshStatus() {
        let state = store.connectionControlState
        let line = store.configuration.activeConnection.displayName
        statusItem?.button?.image = MenuBarStatusIcon.image(for: state)
        statusItem?.button?.toolTip = "GoConnect · \(state.title)\n\(line) · \(store.accessTitle)"
        statusItem?.button?.setAccessibilityValue(state.title)
    }

    func menuNeedsUpdate(_ menu: NSMenu) { rebuildMenu() }

    private func rebuildMenu() {
        menu.removeAllItems()
        let connection = store.configuration.activeConnection
        let state = store.connectionControlState
        let policy = store.menuBarControlPolicy
        add(state.title, symbol: state.symbol, enabled: false)
        add(connection.displayName, symbol: "server.rack", enabled: false)
        add(connection.connectionSummary, enabled: false)
        do {
            add("应用配置记录：\(connection.applications.count) 个", enabled: false)
            if let port = store.proxyPort {
                add("复制代理地址 · 127.0.0.1:\(port)", symbol: "doc.on.doc") { [weak self] in self?.store.copyProxyEndpoint() }
            }
            add("直连域名与 IP：\(connection.directDomains.count) 条", enabled: false)
        }
        if connection.usesCompanyRoutes { add("同线路公司网段：\(connection.remoteNetworks.count) 条", enabled: false) }
        menu.addItem(.separator())
        add(policy.actionTitle, symbol: state.isOn ? "stop.circle" : "power",
            enabled: policy.actionEnabled, key: "k", modifiers: [.command, .shift]) { [weak self] in
            self?.toggleConnection()
        }
        if state == .waitingForPassword {
            add("输入连接密码…", symbol: "key") { [weak self] in self?.openMainWindow() }
        }
        let profiles = NSMenu()
        profiles.autoenablesItems = false
        for profile in store.configuration.connections {
            let item = action(profile.displayName, enabled: policy.quickConnectEnabled) { [weak self] in
                guard let self, self.store.menuBarControlPolicy.quickConnectEnabled else { return }
                self.openMainWindow()
                self.store.quickConnect(profile.id)
            }
            item.state = profile.id == store.configuration.activeProfileID ? .on : .off
            item.toolTip = profile.displayName + " · " + profile.connectionSummary
            profiles.addItem(item)
        }
        let quick = NSMenuItem(title: "快速连接", action: nil, keyEquivalent: "")
        quick.submenu = profiles
        quick.isEnabled = policy.quickConnectEnabled
        menu.addItem(quick)
        if !policy.quickConnectEnabled {
            let reason = !store.configurationReadable ? "配置读取异常，请打开诊断"
                : store.nodeLatency.isRunning ? "节点测试结束后可快速连接"
                : store.profileDraft != nil ? "结束线路编辑后可快速连接"
                : "断开当前连接后可切换线路"
            add(reason, enabled: false)
        }
        menu.addItem(.separator())
        add("打开 GoConnect", symbol: "macwindow", key: "o") { [weak self] in self?.openMainWindow() }
        for page in [Page.profiles, .subscriptions, .diagnostics] {
            add(page.rawValue, symbol: page.icon) { [weak self] in
                guard let self else { return }
                self.openMainWindow()
                self.store.navigate(to: page)
            }
        }
        menu.addItem(.separator())
        add("退出 GoConnect", key: "q") { NSApp.terminate(nil) }
    }

    private func add(_ title: String, symbol: String? = nil, enabled: Bool = true,
                     key: String = "", modifiers: NSEvent.ModifierFlags = .command,
                     handler: (() -> Void)? = nil) {
        let item = action(title, enabled: enabled, handler: handler)
        item.keyEquivalent = key
        item.keyEquivalentModifierMask = modifiers
        if let symbol { item.image = NSImage(systemSymbolName: symbol, accessibilityDescription: nil) }
        menu.addItem(item)
    }

    private func action(_ title: String, enabled: Bool, handler: (() -> Void)?) -> NSMenuItem {
        let item = MenuActionItem(title: MenuBarControlPolicy.shortTitle(title), action: #selector(MenuActionItem.run), keyEquivalent: "")
        item.handler = handler
        item.target = item
        item.isEnabled = enabled
        item.toolTip = title
        return item
    }
}

@MainActor
private final class MenuActionItem: NSMenuItem {
    var handler: (() -> Void)?
    @objc func run() { handler?() }
}

import AppKit
import Combine
import Observation
import OSLog
import GoConnectCore

/// Own only the native status item. AppStore remains the source of connection state.
@MainActor
final class MenuBarController: NSObject, NSMenuDelegate {
    private static let itemName = Product.isNetworkTestApp ? "GoConnectDualVPNTestStatusItem" : "GoConnectStatusItem"
    private func icon(for state: VPNControlState) -> NSImage {
        guard Product.isNetworkTestApp, let image = NSImage(systemSymbolName: "testtube.2", accessibilityDescription: "GoConnect 双 VPN 测试版") else { return MenuBarStatusIcon.image(for: state) }
        image.isTemplate = true
        return image
    }
    private let store: AppStore
    private let openMainWindow: () -> Void
    private let openSettings: () -> Void
    private let defaults: UserDefaults
    private var statusItem: NSStatusItem?
    private var preferenceObservation: AnyCancellable?
    private var expiryTimer: Timer?
    private var observationGeneration: UInt64 = 0
    private var active = true
    private var renderedRateKey: String?
    private let menu = NSMenu()
    private let logger = Logger(subsystem: Product.bundleID, category: "MenuBar")

    init(store: AppStore, defaults: UserDefaults = .standard,
         openSettings: @escaping () -> Void, openMainWindow: @escaping () -> Void) {
        self.store = store
        self.defaults = defaults
        self.openMainWindow = openMainWindow
        self.openSettings = openSettings
        super.init()
        menu.autoenablesItems = false
        menu.delegate = self
        install()
        observeStatus()
        preferenceObservation = NotificationCenter.default.publisher(for: UserDefaults.didChangeNotification, object: defaults)
            .receive(on: DispatchQueue.main)
            .sink { [weak self] _ in
                Task { @MainActor [weak self] in self?.observeStatus() }
            }
    }

    private func install() {
        guard statusItem == nil else { return }
        renderedRateKey = nil
        // A stable, named position replaces SwiftUI's anonymous Item-0 preference.
        // The first insertion starts near the right edge, avoiding the old hidden position.
        UserDefaults.standard.register(defaults: ["NSStatusItem Preferred Position \(Self.itemName)": 0])
        let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        item.autosaveName = Self.itemName
        item.behavior = []
        item.menu = menu
        item.button?.image = icon(for: store.connectionControlState)
        item.button?.imageScaling = .scaleProportionallyDown
        item.button?.setAccessibilityIdentifier(Product.isNetworkTestApp ? "GoConnectDualVPNTestMenuBar" : "GoConnectMenuBar")
        item.button?.setAccessibilityLabel(Product.isNetworkTestApp ? "GoConnect 双 VPN 测试版快捷操作" : "GoConnect 菜单栏快捷操作")
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
        observeStatus()
        // Wait for the newly inserted button to acquire its menu-bar window.
        DispatchQueue.main.async { [weak self] in self?.statusItem?.button?.performClick(nil) }
    }

    func toggleConnection() {
        guard store.menuBarControlPolicy.actionEnabled else { return }
        if !store.connectionControlState.isOn { openMainWindow() }
        store.setConnectionEnabled(!store.connectionControlState.isOn)
    }

    func tearDown() {
        active = false
        preferenceObservation?.cancel()
        preferenceObservation = nil
        expiryTimer?.invalidate()
        expiryTimer = nil
        if let item = statusItem { NSStatusBar.system.removeStatusItem(item) }
        statusItem = nil
    }

    private func observeStatus() {
        guard active else { return }
        observationGeneration &+= 1
        let generation = observationGeneration
        withObservationTracking {
            refreshStatus()
        } onChange: { [weak self] in
            Task { @MainActor [weak self] in
                guard let self, self.observationGeneration == generation else { return }
                self.observeStatus()
            }
        }
    }

    private func refreshStatus() {
        guard active, let item = statusItem, let button = item.button else { return }
        let state = store.connectionControlState
        let line = store.configuration.activeConnection.displayName
        let enabled = defaults.bool(forKey: MenuBarPreferences.showsNetworkRatesKey)
        let direction = defaults.string(forKey: MenuBarPreferences.rateDirectionKey)
            .flatMap(MenuBarRateDirection.init(rawValue:)) ?? .both
        let presentation = MenuBarRatePresentation(
            enabled: enabled, direction: direction, state: state,
            available: enabled && store.trafficAvailable,
            sampledAt: enabled ? store.trafficUpdatedAt : nil,
            download: enabled ? store.downloadRate : 0,
            upload: enabled ? store.uploadRate : 0)
        button.title = ""
        button.imagePosition = .imageOnly
        button.imageScaling = enabled ? .scaleNone : .scaleProportionallyDown
        item.length = enabled ? MenuBarRateImage.statusItemWidth : NSStatusItem.squareLength
        if enabled {
            let key = state.rawValue + "|" + presentation.title
            if key != renderedRateKey {
                button.image = MenuBarRateImage.image(icon: icon(for: state), lines: presentation.lines)
                renderedRateKey = key
            }
        } else {
            renderedRateKey = nil
            button.image = icon(for: state)
        }
        let rates = presentation.accessibilityDescription.map { "\n当前线路 · " + $0 } ?? ""
        button.toolTip = (Product.isNetworkTestApp ? "GoConnect 双 VPN 测试版 ·" : "GoConnect ·") + " \(state.title)\n\(line) · \(store.accessTitle)" + rates
        button.setAccessibilityValue(state.title + (presentation.accessibilityDescription.map { "，" + $0 } ?? ""))
        expiryTimer?.invalidate()
        expiryTimer = nil
        if let deadline = presentation.expiresAt {
            let timer = Timer(fire: deadline, interval: 0, repeats: false) { [weak self] _ in
                Task { @MainActor [weak self] in self?.observeStatus() }
            }
            expiryTimer = timer
            RunLoop.main.add(timer, forMode: .common)
        }
    }

    func menuNeedsUpdate(_ menu: NSMenu) { rebuildMenu() }

    private func rebuildMenu() {
        menu.removeAllItems()
        let connection = store.configuration.activeConnection
        let state = store.connectionControlState
        let policy = store.menuBarControlPolicy
        if Product.isNetworkTestApp { add("GoConnect 双 VPN 测试版", symbol: "testtube.2", enabled: false) }
        add(state.title, symbol: state.symbol, enabled: false)
        add(connection.displayName, symbol: "server.rack", enabled: false)
        add(connection.connectionSummary, enabled: false)
        do {
            add(connection.routingMode == .global ? "直连 App / 服务：\(connection.excludedApplications.count) 个" : "代理应用与服务：\(connection.applications.count) 个", enabled: false)

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
        if store.companyConfiguration.enabled {
            menu.addItem(.separator())
            add("公司副连接 · " + store.company.phase, symbol: "building.2", enabled: false)
            if store.company.isRunning || store.company.busy {
                add("断开公司连接", enabled: !["stopping", "quarantined"].contains(store.company.phase)) { [weak self] in self?.store.company.disconnect() }
            } else {
                add("连接公司内网…", enabled: store.phase == "connected" && store.companyConfigurationIssue == nil && !store.updateOperationBlocked) { [weak self] in
                    guard let self else { return }
                    self.openMainWindow(); self.store.navigate(to: .connection); self.store.connectCompany()
                }
            }
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
        add("设置…", symbol: "gearshape", key: ",") { [weak self] in self?.openSettings() }
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

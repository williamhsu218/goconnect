import AppKit
import SwiftUI
import GoConnectCore

enum PreferencesTab: String, CaseIterable, Identifiable {
    case general = "通用"
    case advanced = "高级"
    case updates = "更新与关于"

    var id: String { rawValue }
}

struct PreferencesView: View {
    static let windowSize = CGSize(width: 520, height: 460)
    private static let tabAnimation = Animation.timingCurve(0.16, 0.78, 0.27, 1, duration: 0.28)

    var store: AppStore? = nil
    var showMenuBar: () -> Void = {}
    var updater: AppUpdater? = nil
    var initialTab: PreferencesTab = .general

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.layoutDirection) private var layoutDirection
    @State private var selectedTab: PreferencesTab
    @State private var tabDirection: CGFloat = 1
    @State private var loginItem = LoginItemStore()
    @State private var showingPrivacyDetails = false
    @State private var copiedVersion = false

    init(
        store: AppStore? = nil,
        showMenuBar: @escaping () -> Void = {},
        updater: AppUpdater? = nil,
        initialTab: PreferencesTab = .general
    ) {
        self.store = store
        self.showMenuBar = showMenuBar
        self.updater = updater
        self.initialTab = initialTab
        _selectedTab = State(initialValue: initialTab)
    }

    private var availableTabs: [PreferencesTab] {
        var tabs: [PreferencesTab] = [.general]
        if store != nil { tabs.append(.advanced) }
        tabs.append(.updates)
        return tabs
    }

    private var thirdPartyDirectoryURL: URL? {
        guard let url = Bundle.main.resourceURL?.appendingPathComponent("ThirdParty"),
              FileManager.default.fileExists(atPath: url.path) else { return nil }
        return url
    }

    var body: some View {
        VStack(spacing: 0) {
            SlidingTabs(items: availableTabs, selection: selectedTab, accessibilityTitle: "设置分页") { tab in
                selectTab(tab)
            }
            .padding(.horizontal, 20)
            .padding(.top, 12)
            .padding(.bottom, 10)

            Divider()

            ZStack {
                tabContent(selectedTab)
                    .id(selectedTab)
                    .transition(tabTransition)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .clipped()
        }
        .background(AppTheme.windowBackground)
        .tint(AppTheme.accent)
        .onAppear { loginItem.refresh() }
        .onReceive(NotificationCenter.default.publisher(for: NSApplication.didBecomeActiveNotification)) { _ in
            loginItem.refresh()
        }
    }

    private func selectTab(_ tab: PreferencesTab) {
        guard tab != selectedTab else { return }
        let tabs = availableTabs
        let previousIndex = tabs.firstIndex(of: selectedTab) ?? 0
        let nextIndex = tabs.firstIndex(of: tab) ?? 0
        let direction: CGFloat = nextIndex > previousIndex ? 1 : -1
        withAnimation(reduceMotion ? .easeInOut(duration: 0.16) : Self.tabAnimation) {
            tabDirection = layoutDirection == .leftToRight ? direction : -direction
            selectedTab = tab
        }
    }

    private var tabTransition: AnyTransition {
        if reduceMotion { return .opacity }
        return .asymmetric(
            insertion: .offset(x: tabDirection * 18).combined(with: .opacity),
            removal: .opacity.animation(.easeOut(duration: 0.16))
        )
    }

    @ViewBuilder
    private func tabContent(_ tab: PreferencesTab) -> some View {
        switch tab {
        case .general:
            Form {
                startupSection
                MenuBarSettings(showMenuBar: showMenuBar)
            }
            .formStyle(.grouped)
            .scrollContentBackground(.hidden)

        case .advanced:
            Form {
                if let store {
                    CompanyFeatureSettings(store: store)
                    storageAndServiceSection(store: store)
                }
            }
            .formStyle(.grouped)
            .scrollContentBackground(.hidden)

        case .updates:
            Form {
                brandHeaderSection
                if let updater {
                    AppUpdateSettings(updater: updater)
                }
                privacyAndLicenseSection
            }
            .formStyle(.grouped)
            .scrollContentBackground(.hidden)
        }
    }

    private var startupSection: some View {
        Section("启动") {
            Toggle("登录 macOS 时自动启动", isOn: Binding(
                get: { loginItem.isEnabled },
                set: { loginItem.setEnabled($0) }
            ))
            .disabled(!loginItem.canToggle)
            .accessibilityIdentifier("LaunchAtLoginEnabled")

            if loginItem.requiresApproval {
                HStack(spacing: 10) {
                    Label("需要在「系统设置 → 登录项」中允许 GoConnect。", systemImage: "exclamationmark.triangle.fill")
                        .font(.caption)
                        .foregroundStyle(AppTheme.warning)
                        .fixedSize(horizontal: false, vertical: true)
                    Spacer(minLength: 8)
                    Button("打开登录项…") { loginItem.openSystemSettings() }
                        .appInlineActionStyle()
                        .fixedSize()
                }
            } else if let unavailable = loginItem.unavailableReason {
                Text(unavailable)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            } else {
                Text("仅启动应用并常驻菜单栏，不会在开机时自动连接 VPN。")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            if let error = loginItem.errorMessage {
                InlineNotice(text: error)
            }
        }
    }

    private func storageAndServiceSection(store: AppStore) -> some View {
        let status = store.networkService.status
        return Section("本机数据与服务") {
            HStack(spacing: 12) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("配置与本地数据")
                    Text("线路配置、订阅缓存与本机流量库")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Spacer(minLength: 8)
                Button("在 Finder 中打开", systemImage: "folder") {
                    store.revealConfiguration()
                }
                .appActionStyle()
                .fixedSize()
            }

            HStack(spacing: 12) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("本机特权网络服务")
                    Text(status.version.map { "服务协议 v\($0) · 用于透明 App 分流与 TUN 路由" } ?? "用于透明 App 分流与 TUN 路由")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Spacer(minLength: 8)
                if status.state == "checking" {
                    HStack(spacing: 6) {
                        ProgressView().controlSize(.mini)
                        Text(status.title)
                            .font(.caption.weight(.medium))
                            .foregroundStyle(.secondary)
                    }
                    .padding(.horizontal, 9)
                    .padding(.vertical, 5)
                    .background(Color.secondary.opacity(0.1), in: Capsule())
                } else {
                    StatusPill(
                        title: status.title,
                        symbol: status.ready ? "checkmark.shield.fill" : "exclamationmark.triangle.fill",
                        color: status.ready ? AppTheme.success : AppTheme.warning
                    )
                }
            }
        }
    }

    private var brandHeaderSection: some View {
        Section {
            HStack(spacing: 14) {
                Image(nsImage: AppBranding.icon)
                    .resizable()
                    .interpolation(.high)
                    .scaledToFit()
                    .frame(width: 44, height: 44)
                    .accessibilityHidden(true)

                VStack(alignment: .leading, spacing: 3) {
                    Text(AppBranding.displayName)
                        .font(.headline)
                    Text("版本 \(Product.version)（Build \(Product.build)）")
                        .font(.callout)
                        .monospacedDigit()
                        .foregroundStyle(.secondary)
                    Text("透明 App 分流 · AnyConnect / Clash 订阅")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                Spacer(minLength: 8)

                Button(copiedVersion ? "已复制" : "复制版本", systemImage: copiedVersion ? "checkmark" : "doc.on.doc") {
                    copyVersionSummary()
                }
                .appActionStyle()
                .fixedSize()
            }
            .padding(.vertical, 2)
        }
    }

    private var privacyAndLicenseSection: some View {
        Section("隐私与许可") {
            FullRowDisclosure("隐私与存储说明", isExpanded: $showingPrivacyDetails) {
                Text("AnyConnect 密码保存在内存或钥匙串；订阅地址及节点凭据保存在仅当前用户可读的本机缓存。订阅更新会访问服务商地址，不记录访问网址。连接期间公网 DNS 由主线路接管；启用公司副连接时，仅配置的公司域名交由公司 DNS 处理。")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.top, 2)
            } label: {
                Label("隐私与存储说明", systemImage: "hand.raised")
                    .font(.callout.weight(.medium))
            }

            if let thirdPartyURL = thirdPartyDirectoryURL {
                HStack(spacing: 12) {
                    Label("第三方开源组件许可", systemImage: "doc.text")
                        .font(.callout)
                    Spacer(minLength: 8)
                    Button("打开目录") {
                        NSWorkspace.shared.open(thirdPartyURL)
                    }
                    .appInlineActionStyle()
                    .fixedSize()
                }
            }
        }
    }

    private func copyVersionSummary() {
        let summary = "\(AppBranding.displayName) \(Product.version) (Build \(Product.build)) · macOS \(ProcessInfo.processInfo.operatingSystemVersionString)"
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(summary, forType: .string)
        withAnimation(reduceMotion ? nil : .easeInOut(duration: 0.15)) {
            copiedVersion = true
        }
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) {
            withAnimation(reduceMotion ? nil : .easeInOut(duration: 0.15)) {
                copiedVersion = false
            }
        }
    }
}

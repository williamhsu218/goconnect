import SwiftUI
import GoConnectCore

struct ContentView: View {
    private static let pageAnimation = Animation.timingCurve(0.16, 0.78, 0.27, 1, duration: 0.34)
    @Bindable var store: AppStore
    @State private var showingRoutingHelp = false
    @State private var displayedPage: Page
    @State private var pageDirection: CGFloat = 1
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.layoutDirection) private var layoutDirection

    init(store: AppStore) {
        self.store = store
        _displayedPage = State(initialValue: store.page)
    }

    var body: some View {
        workspaceContent
            .frame(minWidth: MainWindowSizing.minimumSize.width, minHeight: MainWindowSizing.minimumSize.height)
            .background {
                MainWindowSizing()
                    .frame(width: 0, height: 0)
                    .allowsHitTesting(false)
                    .accessibilityHidden(true)
            }
            .overlay(alignment: .bottom) {
                ZStack {
                    if let message = store.message, !message.isEmpty {
                        let messageID = store.messageID
                        ToastNotificationView(
                            message: message,
                            messageID: messageID,
                            isError: store.isError,
                            onDismiss: {
                                if store.messageID == messageID { store.message = nil }
                            }
                        )
                        .transition(reduceMotion ? .opacity : .offset(y: 12).combined(with: .opacity))
                    }
                }
                .animation(reduceMotion ? nil : .spring(response: 0.35, dampingFraction: 0.8), value: store.message)
            }
            .tint(AppTheme.accent)
            .toolbar {
                ToolbarItem(placement: .principal) { workspaceNavigation }
            }
            .onChange(of: store.page) { _, page in showPage(page) }
            .sheet(isPresented: $store.passwordRequested) { ConnectionPasswordSheet(store: store) }
            .confirmationDialog("放弃未保存的更改？", isPresented: $store.discardProfileChangesRequested) {
                Button("放弃更改", role: .destructive) { store.discardProfileChanges() }
                Button("继续编辑", role: .cancel) { store.pendingProfilePage = nil }
            } message: {
                Text("已保存的线路不会改变。")
            }
    }

    private var workspaceContent: some View {
        let section = WorkspaceSection.containing(displayedPage)
        let isCurrentSection = section == WorkspaceSection.containing(store.page)
        return ZStack {
            VStack(spacing: 0) {
                if section.pages.count > 1 {
                    sectionNavigation(section, page: displayedPage)
                }
                ZStack {
                    pageContent(displayedPage)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                        .allowsHitTesting(displayedPage == store.page)
                        .accessibilityHidden(displayedPage != store.page)
                        .id(displayedPage)
                        .transition(pageTransition(distance: 18))
                }
                .clipped()
            }
            .allowsHitTesting(isCurrentSection)
            .accessibilityHidden(!isCurrentSection)
            .id(section)
            .transition(pageTransition(distance: 24))
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .clipped()
    }

    private func showPage(_ page: Page) {
        let order = WorkspaceSection.orderedPages
        let previousIndex = order.firstIndex(of: displayedPage) ?? 0
        let nextIndex = order.firstIndex(of: page) ?? 0
        let direction: CGFloat = nextIndex > previousIndex ? 1 : -1
        withAnimation(reduceMotion ? .easeInOut(duration: 0.16) : Self.pageAnimation) {
            pageDirection = layoutDirection == .leftToRight ? direction : -direction
            displayedPage = page
        }
    }

    private func pageTransition(distance: CGFloat) -> AnyTransition {
        if reduceMotion { return .opacity }
        return .asymmetric(
            insertion: .offset(x: pageDirection * distance).combined(with: .opacity),
            removal: .opacity.animation(.easeOut(duration: 0.18))
        )
    }

    @ViewBuilder
    private func pageContent(_ page: Page) -> some View {
        switch page {
        case .connection: ConnectionView(store: store)
        case .profiles: ProfilesView(store: store)
        case .subscriptions: SubscriptionsView(store: store)
        case .traffic: TrafficHistoryView(store: store)
        case .applications: ApplicationsView(store: store)
        case .exclusions: ApplicationsView(store: store, excluded: true)
        case .directRules: DirectRulesView(store: store)
        case .diagnostics: DiagnosticsView(store: store)
        }
    }

    private var workspaceNavigation: some View {
        let selection = WorkspaceSection.containing(displayedPage)
        return HStack(spacing: 2) {
            ForEach(WorkspaceSection.allCases, id: \.self) { section in
                let selected = section == selection
                Button {
                    if !selected {
                        let destination = section == .routing && store.configuration.routingMode == .global
                            ? Page.exclusions : section.defaultPage
                        store.navigate(to: destination)
                    }
                } label: {
                    HStack(spacing: 5) {
                        if section == .connection && store.connectionControlState != .off {
                            connectionIndicator
                        }
                        Text(section.rawValue)
                            .font(.callout.weight(selected ? .semibold : .regular))
                    }
                    .padding(.horizontal, 11)
                    .frame(minHeight: 29)
                    .contentShape(Capsule())
                    .tabBounds(section)
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(selected ? .isSelected : [])
                .accessibilityValue(section == .connection ? Text(store.connectionDisplayTitle) : Text(""))
                .help(section == .connection ? "\(store.connectionDisplayTitle) · \(store.configuration.activeConnection.displayName)" : section.navigationHelp)
            }
        }
        .padding(3)
        .fixedSize()
        .tabHighlight(selection, fill: Color.primary.opacity(0.10),
                      animation: reduceMotion ? nil : .spring(duration: 0.36, bounce: 0.10))
    }

    private var connectionIndicator: some View {
        let state = store.connectionControlState
        let needsAttention = state.needsAttention || (state == .connected && store.pathHealth?.state == "degraded")
        return Image(systemName: needsAttention ? "exclamationmark.circle.fill" : state.symbol)
            .font(.system(size: 9, weight: .semibold))
            .foregroundStyle(needsAttention ? AppTheme.warning : state == .connected ? AppTheme.success : AppTheme.accent)
            .accessibilityHidden(true)
    }

    private func sectionNavigation(_ section: WorkspaceSection, page: Page) -> some View {
        SlidingTabs(items: section.pages, selection: page, accessibilityTitle: "\(section.rawValue)子页") {
            store.navigate(to: $0)
        }
        .frame(maxWidth: .infinity)
        .overlay(alignment: .trailing) {
            if section == .routing {
                Button("分流说明", systemImage: "info.circle") { showingRoutingHelp.toggle() }
                    .labelStyle(.iconOnly)
                    .appActionStyle()
                    .help("查看连接边界与分流优先级")
                    .popover(isPresented: $showingRoutingHelp) {
                        ConnectionBoundaryHelpView(mode: store.configuration.routingMode)
                    }
            }
        }
        .padding(.horizontal, AppTheme.pagePadding)
        .padding(.vertical, 6)
    }
}

private enum WorkspaceSection: String, CaseIterable {
    case connection = "连接"
    case lines = "线路节点"
    case traffic = "流量统计"
    case routing = "分流规则"
    case diagnostics = "诊断"

    static let orderedPages = allCases.flatMap(\.pages)

    static func containing(_ page: Page) -> Self {
        allCases.first { $0.pages.contains(page) } ?? .connection
    }

    var pages: [Page] {
        switch self {
        case .connection: [.connection]
        case .lines: [.subscriptions, .profiles]
        case .traffic: [.traffic]
        case .routing: [.applications, .exclusions, .directRules]
        case .diagnostics: [.diagnostics]
        }
    }

    var defaultPage: Page { pages[0] }
    var navigationHelp: String { self == .diagnostics ? "查看网络诊断和服务状态" : rawValue }
}

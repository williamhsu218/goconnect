import SwiftUI
import GoConnectCore

struct ContentView: View {
    @Bindable var store: AppStore
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        NavigationSplitView {
            VStack(alignment: .leading, spacing: 0) {
                List(selection: Binding(get: { store.page }, set: { store.navigate(to: $0) })) {
                    Section {
                        sidebarRow(for: .connection)
                    }

                    Section("线路与节点") {
                        sidebarRow(for: .profiles)
                        sidebarRow(for: .subscriptions)
                    }

                    Section("分流规则") {
                        sidebarRow(for: .applications)
                        sidebarRow(for: .exclusions)
                    }

                    Section("系统维护") {
                        sidebarRow(for: .diagnostics)
                    }
                }
                .listStyle(.sidebar)

                HStack(spacing: 8) {
                    Image(systemName: store.connectionControlState == .off ? "network" : store.connectionControlState.symbol)
                        .foregroundStyle(store.connectionControlState == .connected ? AppTheme.success : .secondary)
                    Text(store.connectionControlState.title).font(.caption).lineLimit(1)
                    Spacer()
                    Text("v\(Product.version)").font(.caption).foregroundStyle(.secondary)
                }
                .padding(14)
                .accessibilityElement(children: .combine)

            }
            .navigationSplitViewColumnWidth(min: 190, ideal: 215, max: 250)
        } detail: {
            Group {
                    switch store.page {
                    case .connection: ConnectionView(store: store)
                    case .profiles: ProfilesView(store: store)
                    case .subscriptions: SubscriptionsView(store: store)
                    case .applications: ApplicationsView(store: store)
                    case .exclusions: ApplicationsView(store: store, excluded: true)
                    case .diagnostics: DiagnosticsView(store: store)
                    }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .safeAreaInset(edge: .bottom, spacing: 0) {
                if let message = store.message, !message.isEmpty {
                    ToastNotificationView(
                        message: message,
                        isError: store.isError,
                        onDismiss: {
                            withAnimation(reduceMotion ? nil : .spring(response: 0.35, dampingFraction: 0.8)) {
                                store.message = nil
                            }
                        }
                    )
                    .transition(reduceMotion ? .opacity : .move(edge: .bottom).combined(with: .opacity))
                    .zIndex(100)
                }
            }
            .animation(reduceMotion ? nil : .spring(response: 0.35, dampingFraction: 0.8), value: store.message)
        }
        .tint(AppTheme.accent)
        .navigationTitle(store.page.rawValue)
        .toolbar {
            activeConnectionToolbar
            ToolbarItem(placement: .primaryAction) {
                Button {
                    store.navigate(to: .connection)
                } label: {
                    Label(store.connectionControlState.title, systemImage: store.connectionControlState == .off ? "network" : store.connectionControlState.symbol)
                }
                .help("查看连接状态")
                .accessibilityLabel("查看连接状态：\(store.connectionControlState.title)")
            }
        }
        .sheet(isPresented: $store.passwordRequested) { ConnectionPasswordSheet(store: store) }
        .confirmationDialog("放弃未保存的更改？", isPresented: $store.discardProfileChangesRequested) {
            Button("放弃更改", role: .destructive) { store.discardProfileChanges() }
            Button("继续编辑", role: .cancel) { store.pendingProfilePage = nil }
        } message: {
            Text("已保存的线路不会改变。")
        }
    }

    @ToolbarContentBuilder
    private var activeConnectionToolbar: some ToolbarContent {
        if #available(macOS 26.0, *) {
            ToolbarItem(placement: .principal) { activeConnectionLabel }
                .sharedBackgroundVisibility(.hidden)
        } else {
            ToolbarItem(placement: .principal) { activeConnectionLabel }
        }
    }

    private var activeConnectionLabel: some View {
        Label(store.configuration.activeConnection.displayName, systemImage: "server.rack")
            .labelStyle(.titleAndIcon)
            .font(.callout)
            .foregroundStyle(.secondary)
            .lineLimit(1)
            .truncationMode(.middle)
            .frame(minWidth: 0, maxWidth: 140)
            .fixedSize(horizontal: false, vertical: true)
            .padding(.horizontal, 6)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("当前线路：\(store.configuration.activeConnection.displayName)")
            .accessibilityValue(store.accessTitle)
            .help("当前线路：\(store.configuration.activeConnection.displayName) · \(store.accessTitle)")
    }

    private func sidebarRow(for page: Page) -> some View {
        HStack(spacing: 8) {
            Label(page.rawValue, systemImage: page.icon)
            Spacer()
            pageIndicator(for: page)
        }
        .badge(page == .applications ? store.configuration.applications.count : (page == .exclusions ? store.configuration.excludedApplications.count : 0))
        .tag(page)
        .padding(.vertical, 3)
    }

    @ViewBuilder
    private func pageIndicator(for page: Page) -> some View {
        switch page {
        case .connection:
            if store.connectionControlState.needsAttention {
                Image(systemName: "exclamationmark.circle.fill")
                    .foregroundStyle(AppTheme.warning).accessibilityLabel("连接需要处理")
            }
        case .profiles:
            if store.profileDraftHasChanges {
                Image(systemName: "pencil.circle")
                    .foregroundStyle(AppTheme.warning).accessibilityLabel("有未保存的线路修改")
            }
        case .subscriptions:
            if store.subscriptionWorking || store.nodeLatency.isRunning {
                ProgressView()
                    .controlSize(.mini)
            }
        case .diagnostics:
            if store.connectionControlState == .diagnostic {
                Image(systemName: "waveform.path.ecg").foregroundStyle(.secondary).accessibilityLabel("正在诊断")
            }
        case .applications:
            if store.updatingApplications || store.scanning {
                ProgressView()
                    .controlSize(.mini)
            }
        case .exclusions:
            if store.updatingApplications || store.scanning {
                ProgressView()
                    .controlSize(.mini)
            }
        }
    }
}

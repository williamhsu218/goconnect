import SwiftUI
import GoConnectCore

struct ContentView: View {
    @Bindable var store: AppStore

    var body: some View {
        NavigationSplitView {
            VStack(alignment: .leading, spacing: 0) {
                HStack(spacing: 10) {
                    AppBrandIcon(size: 26)
                    VStack(alignment: .leading, spacing: 2) {
                        Text("GoConnect")
                            .font(.system(size: 13, weight: .bold))
                        Text("为应用选择连接")
                            .font(.system(size: 11))
                            .foregroundStyle(.secondary)
                    }
                }
                .padding(.horizontal, 16)
                .padding(.top, 14)
                .padding(.bottom, 10)

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

                VStack(spacing: 0) {
                    Divider()
                        .opacity(0.6)

                    HStack(alignment: .center) {
                        HStack(spacing: 6) {
                            Image(systemName: "shield.checkered")
                                .font(.system(size: 11))
                                .foregroundStyle(store.connectionControlState == .connected ? AppTheme.success : .secondary)

                            Text("v\(Product.version)")
                                .font(.system(size: 11, weight: .medium, design: .monospaced))
                                .foregroundStyle(.secondary)
                        }

                        Spacer()

                        HStack(spacing: 4) {
                            Circle()
                                .fill(store.connectionControlState == .connected ? AppTheme.success : Color.secondary.opacity(0.4))
                                .frame(width: 6, height: 6)
                            Text(store.configuration.activeConnection.subscription != nil ? "订阅" : "AnyConnect")
                                .font(.system(size: 10, weight: .medium))
                                .foregroundStyle(.secondary)
                                .lineLimit(1)
                        }
                        .padding(.horizontal, 7)
                        .padding(.vertical, 3)
                        .background(Color.primary.opacity(0.04), in: Capsule())
                        .overlay(Capsule().strokeBorder(AppTheme.subtleBorder, lineWidth: 0.5))
                    }
                    .padding(.horizontal, 16)
                    .padding(.vertical, 10)
                    .help("GoConnect v\(Product.version) · 当前模式：\(store.accessTitle)")
                }
            }
            .navigationSplitViewColumnWidth(min: 190, ideal: 215, max: 250)
        } detail: {
            ZStack(alignment: .bottom) {
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

                if let message = store.message, !message.isEmpty {
                    ToastNotificationView(
                        message: message,
                        isError: store.isError,
                        onDismiss: {
                            withAnimation(.spring(response: 0.35, dampingFraction: 0.8)) {
                                store.message = nil
                            }
                        }
                    )
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                    .zIndex(100)
                }
            }
            .animation(.spring(response: 0.35, dampingFraction: 0.8), value: store.message)
        }
        .tint(AppTheme.accent)
        .sheet(isPresented: $store.passwordRequested) { ConnectionPasswordSheet(store: store) }
        .confirmationDialog("放弃未保存的更改？", isPresented: $store.discardProfileChangesRequested) {
            Button("放弃更改", role: .destructive) { store.discardProfileChanges() }
            Button("继续编辑", role: .cancel) { store.pendingProfilePage = nil }
        } message: {
            Text("已保存的线路不会改变。")
        }
    }

    private func sidebarRow(for page: Page) -> some View {
        HStack(spacing: 8) {
            Label(page.rawValue, systemImage: page.icon)
            Spacer()
            pageIndicator(for: page)
        }
        .tag(page)
        .padding(.vertical, 3)
    }

    @ViewBuilder
    private func pageIndicator(for page: Page) -> some View {
        switch page {
        case .connection:
            if store.connectionControlState.needsAttention {
                Circle()
                    .fill(AppTheme.warning)
                    .frame(width: 7, height: 7)
            }
        case .profiles:
            if store.profileDraftHasChanges {
                Circle()
                    .fill(AppTheme.warning)
                    .frame(width: 7, height: 7)
                    .help("有未保存的线路修改")
            }
        case .subscriptions:
            if store.subscriptionWorking {
                ProgressView()
                    .controlSize(.mini)
            }
        case .diagnostics:
            if store.nodeLatency.isRunning || store.connectionControlState == .diagnostic {
                PulsingDot(color: AppTheme.accent)
            }
        case .applications:
            if store.updatingApplications || store.scanning {
                ProgressView()
                    .controlSize(.mini)
            } else if !store.configuration.applications.isEmpty {
                CountBadge(count: store.configuration.applications.count)
            }
        case .exclusions:
            if store.updatingApplications || store.scanning {
                ProgressView()
                    .controlSize(.mini)
            } else if !store.configuration.excludedApplications.isEmpty {
                CountBadge(count: store.configuration.excludedApplications.count)
            }
        }
    }
}

struct PulsingDot: View {
    var color: Color = AppTheme.success
    var size: CGFloat = 7
    @State private var isPulsing = false

    var body: some View {
        Circle()
            .fill(color)
            .frame(width: size, height: size)
            .overlay(
                Circle()
                    .stroke(color, lineWidth: 1.5)
                    .scaleEffect(isPulsing ? 2.0 : 1.0)
                    .opacity(isPulsing ? 0 : 0.75)
            )
            .onAppear {
                withAnimation(.easeInOut(duration: 1.2).repeatForever(autoreverses: false)) {
                    isPulsing = true
                }
            }
    }
}

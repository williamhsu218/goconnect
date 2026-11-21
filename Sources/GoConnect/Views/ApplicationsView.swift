import SwiftUI
import AppKit
import UniformTypeIdentifiers
import GoConnectCore

enum ApplicationFilter: String, CaseIterable, Identifiable {
    case all = "全部"
    case apps = "应用 (.app)"
    case cli = "命令行服务 (CLI)"

    var id: String { rawValue }
}

struct ApplicationsView: View {
    @Bindable var store: AppStore
    var excluded = false

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var isDropTargeted = false

    private var searchText: Binding<String> {
        excluded ? $store.excludedApplicationSearch : $store.applicationSearch
    }

    private var typeSelection: Binding<ApplicationFilter> {
        excluded ? $store.excludedApplicationFilter : $store.applicationFilter
    }

    private var filter: ApplicationFilter { typeSelection.wrappedValue }

    private var apps: [AllowedApplication] {
        let list = store.filteredApps(excluded: excluded)
        switch filter {
        case .all:
            return list
        case .apps:
            return list.filter { !$0.isExecutableService }
        case .cli:
            return list.filter { $0.isExecutableService }
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: AppTheme.sectionSpacing) {
            VStack(alignment: .leading, spacing: 4) {
                PageHeading(
                    title: excluded ? "直连 App / 服务" : "代理应用与服务",
                    subtitle: "当前线路 · \(store.configuration.activeConnection.displayName)"
                )
                Text(excluded ? "这些 App 和服务使用原网络，直连规则优先。" : "按名单代理模式仅让名单中的应用与服务走所选线路。")
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }

            if store.busy {
                Label("连接期间名单为只读；断开后修改，下次连接生效。", systemImage: "lock")
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }

            if !excluded && store.configuration.routingMode == .global {
                Label("当前使用全局模式；代理名单保留，切换为按名单代理后生效。", systemImage: "info.circle")
                    .font(.callout).foregroundStyle(.secondary)
            }

            listControls

            ZStack {
                if apps.isEmpty {
                    emptyStateView
                        .transition(.opacity)
                } else {
                    List(apps) { app in
                        applicationRow(app)
                    }
                    .listStyle(.inset)
                    .scrollContentBackground(.hidden)
                    .transition(.opacity)
                }

                if isDropTargeted {
                    dropTargetOverlay
                }
            }
            .cardSurface(emphasized: isDropTargeted)
            .frame(maxHeight: .infinity)
            .onDrop(of: [.fileURL], isTargeted: $isDropTargeted) { providers in
                handleDrop(providers: providers)
            }

            Text("App 按包路径匹配；CLI 可选择固定入口，连接时自动解析当前版本并精确匹配。连接期间升级 CLI 后需重新连接。GoConnect 不会自动启动或重启目标。")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(AppTheme.pagePadding)
        .frame(maxWidth: AppTheme.contentWidth, maxHeight: .infinity, alignment: .topLeading)
        .frame(maxWidth: .infinity, alignment: .top)
        .onAppear {
            if store.catalog.isEmpty {
                store.scanApplications()
            }
        }
    }

    private var listControls: some View {
        HStack(spacing: 12) {
            InlineSearchField(prompt: "搜索名称或路径", text: searchText)
                .frame(maxWidth: 190)
                .frame(maxWidth: .infinity, alignment: .leading)

            filterBar

            HStack(spacing: 8) {
                Button("刷新", systemImage: "arrow.clockwise") { store.scanApplications() }
                    .appActionStyle()
                    .disabled(store.scanning)
                    .accessibilityLabel("刷新应用")
                    .help("重新扫描已安装应用")

                Menu("添加", systemImage: "plus") {
                    Button("添加 App…", systemImage: "app.badge") { store.addApplication(excluded: excluded) }
                    Button("添加可执行服务…", systemImage: "terminal") { store.addExecutableService(excluded: excluded) }
                }
                .menuStyle(.borderedButton)
                .disabled(!store.canEditApplications)
                .help(excluded ? "添加到直连名单" : "添加到代理名单")
            }
            .fixedSize()
            .frame(maxWidth: .infinity, alignment: .trailing)
        }
    }

    @ViewBuilder
    private func applicationRow(_ app: AllowedApplication) -> some View {
        HStack(spacing: 12) {
            if app.isExecutableService {
                cliIconView
            } else {
                ApplicationIcon(application: app, size: 32)
            }

            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(app.name)
                        .font(.body.weight(.medium))
                    if app.isExecutableService {
                        Text("CLI")
                            .font(.system(size: 10, weight: .semibold, design: .monospaced))
                            .foregroundStyle(.secondary)
                            .padding(.horizontal, 5)
                            .padding(.vertical, 1)
                            .background(Color.secondary.opacity(0.12), in: Capsule())
                    }
                }

                HStack(spacing: 6) {
                    if app.isExecutableService {
                        Text(displayPath(for: app.path))
                            .font(.system(.caption, design: .monospaced))
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                            .truncationMode(.middle)
                    } else {
                        Text(app.bundleID)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                }
            }

            Spacer()

            Toggle(
                "将 \(app.name) 加入\(excluded ? "直连名单" : "代理名单")",
                isOn: Binding(
                    get: { store.selected(app, excluded: excluded) },
                    set: { _ in store.toggle(app, excluded: excluded) }
                )
            )
            .labelsHidden()
            .toggleStyle(.switch)
            .controlSize(.small)
            .disabled(!store.canEditApplications)
        }
        .padding(.vertical, 7)
        .listRowInsets(EdgeInsets(top: 0, leading: 12, bottom: 0, trailing: 12))
        .help(app.path)
        .contextMenu {
            Button {
                let url = URL(fileURLWithPath: app.path)
                NSWorkspace.shared.activateFileViewerSelecting([url])
            } label: {
                Label("在 Finder 中显示", systemImage: "folder")
            }

            Button {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(app.path, forType: .string)
                store.show("已复制路径：\(app.path)")
            } label: {
                Label("复制完整路径", systemImage: "doc.on.doc")
            }

            if !app.isExecutableService {
                Button {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(app.bundleID, forType: .string)
                    store.show("已复制 Bundle ID：\(app.bundleID)")
                } label: {
                    Label("复制 Bundle ID", systemImage: "app.badge")
                }
            }

            Divider()

            Button(role: .destructive) {
                if store.selected(app, excluded: excluded) {
                    store.toggle(app, excluded: excluded)
                }
            } label: {
                Label("从名单中移除", systemImage: "trash")
            }
            .disabled(!store.canEditApplications || !store.selected(app, excluded: excluded))
        }
    }

    private var filterBar: some View {
        SlidingTabs(items: ApplicationFilter.allCases, selection: filter, accessibilityTitle: "目标类型", compact: true) { item in
            guard item != filter else { return }
            withAnimation(reduceMotion ? nil : .smooth(duration: 0.26)) {
                typeSelection.wrappedValue = item
            }
        }
    }

    private var cliIconView: some View {
        ZStack {
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .fill(Color(nsColor: .controlBackgroundColor))
            Image(systemName: "terminal.fill")
                .font(.system(size: 16, weight: .semibold))
                .foregroundStyle(.secondary)
        }
        .frame(width: 32, height: 32)
        .overlay(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .strokeBorder(AppTheme.subtleBorder, lineWidth: 0.5)
        )
    }

    private var emptyStateView: some View {
        ContentUnavailableView(emptyTitle, systemImage: emptyIcon,
            description: Text(store.scanning ? "请稍候" : "调整搜索条件，或从 Finder 拖入 App 和可执行文件。"))
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding()
    }

    private var emptyIcon: String {
        if store.scanning { return "hourglass" }
        if !searchText.wrappedValue.isEmpty { return "magnifyingglass" }
        switch filter {
        case .all: return "square.dashed"
        case .apps: return "app.badge"
        case .cli: return "terminal"
        }
    }

    private var emptyTitle: String {
        if store.scanning { return "正在扫描已安装应用…" }
        let query = searchText.wrappedValue
        if !query.isEmpty { return "未找到匹配「\(query)」的目标" }
        switch filter {
        case .all: return "暂无应用或服务目标"
        case .apps: return "暂无 .app 应用"
        case .cli: return "暂无命令行服务 (CLI)"
        }
    }

    private var dropTargetOverlay: some View {
        RoundedRectangle(cornerRadius: AppTheme.cardRadius, style: .continuous)
            .strokeBorder(AppTheme.accent, style: StrokeStyle(lineWidth: 2, dash: [6, 4]))
            .background(AppTheme.accent.opacity(0.08))
            .overlay {
                VStack(spacing: 8) {
                    Image(systemName: "arrow.down.doc.fill")
                        .font(.system(size: 30))
                        .foregroundStyle(AppTheme.accent)
                    Text("释放以添加到名单")
                        .font(.headline)
                        .foregroundStyle(AppTheme.accent)
                    Text(excluded ? "添加至直连名单" : "添加至代理名单")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .padding(20)
                .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                .overlay(
                    RoundedRectangle(cornerRadius: 12, style: .continuous)
                        .strokeBorder(Color.primary.opacity(0.1), lineWidth: 1)
                )
                .shadow(color: .black.opacity(0.15), radius: 12, y: 6)
            }
            .allowsHitTesting(false)
            .animation(.easeInOut(duration: 0.2), value: isDropTargeted)
    }

    private func displayPath(for path: String) -> String {
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        if path == home {
            return "~"
        } else if path.hasPrefix(home + "/") {
            return "~" + path.dropFirst(home.count)
        }
        return path
    }

    private func handleDrop(providers: [NSItemProvider]) -> Bool {
        guard store.canEditApplications else { return false }
        let group = DispatchGroup()
        var urls: [URL] = []
        let lock = NSLock()

        for provider in providers {
            if provider.canLoadObject(ofClass: URL.self) {
                group.enter()
                _ = provider.loadObject(ofClass: URL.self) { url, _ in
                    defer { group.leave() }
                    if let url {
                        lock.lock()
                        urls.append(url)
                        lock.unlock()
                    }
                }
            } else if provider.hasItemConformingToTypeIdentifier(UTType.fileURL.identifier) {
                group.enter()
                provider.loadItem(forTypeIdentifier: UTType.fileURL.identifier, options: nil) { item, _ in
                    defer { group.leave() }
                    var fileURL: URL?
                    if let url = item as? URL {
                        fileURL = url
                    } else if let url = item as? NSURL {
                        fileURL = url as URL
                    } else if let data = item as? Data {
                        if let url = URL(dataRepresentation: data, relativeTo: nil) {
                            fileURL = url
                        } else if let str = String(data: data, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) {
                            if str.hasPrefix("file://") {
                                fileURL = URL(string: str)
                            } else if str.hasPrefix("/") {
                                fileURL = URL(fileURLWithPath: str)
                            }
                        }
                    }
                    if let url = fileURL {
                        lock.lock()
                        urls.append(url)
                        lock.unlock()
                    }
                }
            }
        }

        group.notify(queue: .main) {
            if !urls.isEmpty {
                store.addApplications(from: urls, excluded: excluded)
            }
        }
        return true
    }
}

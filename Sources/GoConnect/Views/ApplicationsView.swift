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

    @State private var filter: ApplicationFilter = .all
    @State private var isDropTargeted = false

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
            PageHeader(
                title: excluded ? "直连 App / 服务" : "应用 / 服务白名单",
                subtitle: excluded ? "这些 App 和服务使用原网络，直连规则优先。" : "白名单模式仅让这些 App 和服务走 VPN，其它流量直连。"
            ) {
                Menu("添加…", systemImage: "plus") {
                    Button("添加 App…", systemImage: "app.badge") { store.addApplication(excluded: excluded) }
                    Button("添加可执行服务…", systemImage: "terminal") { store.addExecutableService(excluded: excluded) }
                }
                .appActionStyle(primary: true)
                .fixedSize()
                .disabled(!store.canEditApplications)
            }

            if store.busy {
                Label("连接期间名单为只读；断开后修改，下次连接生效。", systemImage: "lock")
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }

            if let port = store.proxyPort {
                HStack {
                    Text(verbatim: "HTTP / SOCKS5 · 127.0.0.1:\(port)").font(.callout.monospaced()).textSelection(.enabled)
                    Spacer()
                    Button("复制地址") { store.copyProxyEndpoint() }
                }
            }

            HStack(spacing: 12) {
                filterBar

                Spacer()

                CountBadge(count: (excluded ? store.configuration.excludedApplications : store.configuration.applications).count)
                    .help("已保存的分流目标")

                Button {
                    store.scanApplications()
                } label: {
                    Label("刷新", systemImage: "arrow.clockwise")
                }
                .appActionStyle()
                .fixedSize(horizontal: true, vertical: false)
                .disabled(store.scanning)
            }

            ZStack {
                if apps.isEmpty {
                    emptyStateView
                } else {
                    List(apps) { app in
                        applicationRow(app)
                    }
                    .listStyle(.inset)
                    .scrollContentBackground(.hidden)
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

            Text("App 按包路径匹配；可执行服务按完整文件路径精确匹配。GoConnect 不会自动启动或重启目标。")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(AppTheme.pagePadding)
        .frame(maxWidth: AppTheme.contentWidth, maxHeight: .infinity, alignment: .topLeading)
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .searchable(text: $store.search, prompt: "搜索 App、服务或路径")
        .onAppear {
            if store.catalog.isEmpty {
                store.scanApplications()
            }
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
                    Text("·")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                    Text(excluded ? "原网络直连" : "白名单模式下走线路")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }

            Spacer()

            Toggle(
                "保存 \(app.name)",
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
        HStack(spacing: 3) {
            ForEach(ApplicationFilter.allCases) { item in
                Button {
                    withAnimation(.easeOut(duration: 0.16)) {
                        filter = item
                    }
                } label: {
                    ZStack {
                        RoundedRectangle(cornerRadius: 7, style: .continuous)
                            .fill(filter == item ? AppTheme.accent : Color.white.opacity(0.001))

                        Text(item.rawValue)
                            .font(.callout.weight(filter == item ? .semibold : .medium))
                            .foregroundStyle(filter == item ? Color.white : Color.secondary)
                            .lineLimit(1)
                            .padding(.horizontal, 10)
                    }
                    .frame(maxWidth: .infinity)
                    .frame(height: 28)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .contentShape(Rectangle())
                .accessibilityLabel("筛选：\(item.rawValue)")
                .accessibilityAddTraits(filter == item ? .isSelected : [])
            }
        }
        .padding(3)
        .frame(maxWidth: 420)
        .background(Color.primary.opacity(0.055), in: RoundedRectangle(cornerRadius: 10, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .strokeBorder(AppTheme.subtleBorder.opacity(0.8), lineWidth: 0.5)
        )
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
        VStack(spacing: 12) {
            Image(systemName: emptyIcon)
                .font(.system(size: 36))
                .foregroundStyle(.tertiary)
            Text(emptyTitle)
                .font(.body)
                .foregroundStyle(.secondary)
            if !store.scanning {
                Text("可直接从 Finder 拖拽 .app 或终端可执行文件到此处添加")
                    .font(.caption)
                    .foregroundStyle(.tertiary)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding()
    }

    private var emptyIcon: String {
        if store.scanning { return "hourglass" }
        if !store.search.isEmpty { return "magnifyingglass" }
        switch filter {
        case .all: return "square.dashed"
        case .apps: return "app.badge"
        case .cli: return "terminal"
        }
    }

    private var emptyTitle: String {
        if store.scanning { return "正在扫描已安装应用…" }
        if !store.search.isEmpty { return "未找到匹配「\(store.search)」的目标" }
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
                    Text(excluded ? "添加至直连名单" : "添加至白名单")
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

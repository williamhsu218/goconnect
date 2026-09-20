import SwiftUI
import GoConnectCore

struct ProfilesView: View {
    @Bindable var store: AppStore
    @State private var searchText = ""
    @State private var deleting: SavedConnection?
    @State private var selectedProfileID: UUID?
    @State private var pendingProfileSwitch: UUID?
    @State private var pendingNewProfile = false
    @State private var pendingDuplicateProfile: UUID?
    @State private var pendingUseProfile: UUID?

    private var filteredConnections: [SavedConnection] {
        let trimmed = searchText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return store.configuration.connections }
        return store.configuration.connections.filter { conn in
            conn.displayName.localizedCaseInsensitiveContains(trimmed) ||
            conn.profile.server.localizedCaseInsensitiveContains(trimmed) ||
            (conn.subscription?.nodeName.localizedCaseInsensitiveContains(trimmed) == true) ||
            conn.routingMode.title.localizedCaseInsensitiveContains(trimmed)
        }
    }

    var body: some View {
        HStack(spacing: 0) {
            // Master Pane (Left)
            masterPane
                .frame(width: 290)
                .background(Color(nsColor: .windowBackgroundColor))

            Divider()

            // Detail Pane (Right)
            detailPane
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Color(nsColor: .windowBackgroundColor))
        }
        .onAppear {
            ensureProfileSelected()
        }
        .onChange(of: store.profileDraft == nil) { _, isNil in
            if isNil {
                handleDraftBecameNil()
            }
        }
        .onChange(of: store.profileDraft?.id) { _, newID in
            if let newID {
                selectedProfileID = newID
            }
        }
        .onChange(of: store.discardProfileChangesRequested) { _, requested in
            if !requested && store.profileDraft != nil {
                pendingProfileSwitch = nil
                pendingNewProfile = false
                pendingDuplicateProfile = nil
                pendingUseProfile = nil
            }
        }
        .confirmationDialog(
            "删除这条线路？",
            isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
            presenting: deleting
        ) { connection in
            Button("删除线路", role: .destructive) {
                if store.profileDraft?.id == connection.id {
                    selectedProfileID = nil
                }
                store.closeProfileEditor()
                store.deleteProfile(connection.id)
                deleting = nil
            }
            Button("取消", role: .cancel) {
                deleting = nil
            }
        } message: { connection in
            Text("将删除“\(connection.displayName)”及其保存的密码。")
        }
    }

    private var masterPane: some View {
        VStack(spacing: 0) {
            // Header
            VStack(alignment: .leading, spacing: 10) {
                HStack(alignment: .center) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("线路配置")
                            .font(.system(size: 17, weight: .bold))
                        Text("\(store.configuration.connections.count) 条已保存线路")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    Button("新建线路", systemImage: "plus") {
                        createNewProfile()
                    }
                    .appActionStyle(primary: true)
                    .fixedSize(horizontal: true, vertical: false)
                    .disabled(!store.configurationReadable || store.configuration.connections.count >= 64)
                }

                // Search field
                HStack(spacing: 6) {
                    Image(systemName: "magnifyingglass")
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                    TextField("搜索线路…", text: $searchText)
                        .textFieldStyle(.plain)
                        .font(.callout)
                    if !searchText.isEmpty {
                        Button {
                            searchText = ""
                        } label: {
                            Image(systemName: "xmark.circle.fill")
                                .font(.system(size: 11))
                                .foregroundStyle(.secondary)
                        }
                        .buttonStyle(.plain)
                    }
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 5)
                .background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 6))
                .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(AppTheme.subtleBorder, lineWidth: 0.5))

                if store.busy {
                    HStack(spacing: 6) {
                        Image(systemName: "info.circle")
                            .font(.system(size: 11))
                        Text("当前线路连接中，仍可查看与编辑其它线路。")
                            .font(.caption2)
                    }
                    .foregroundStyle(.secondary)
                }
            }
            .padding(.horizontal, 14)
            .padding(.top, 16)
            .padding(.bottom, 10)

            Divider()

            // List of profile cards
            ScrollView {
                LazyVStack(spacing: 8) {
                    if let draft = store.profileDraft, draft.isNew {
                        newDraftCard(draft: draft)
                    }

                    ForEach(filteredConnections) { connection in
                        ProfileMasterCard(
                            connection: connection,
                            isSelected: selectedProfileID == connection.id && store.profileDraft?.isNew != true,
                            isActive: connection.id == store.configuration.activeProfileID,
                            isBusy: store.busy,
                            onSelect: {
                                selectProfileItem(connection.id)
                            },
                            onUse: {
                                useProfileForConnection(connection.id)
                            },
                            onDuplicate: {
                                duplicateProfile(connection.id)
                            },
                            onDelete: {
                                requestDeleteProfile(connection)
                            },
                            canDelete: (!store.busy || connection.id != store.configuration.activeProfileID) && store.configuration.connections.count > 1 && store.configurationReadable
                        )
                    }
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 10)
            }
        }
    }

    private func newDraftCard(draft: ConnectionDraft) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                Image(systemName: "plus.circle.fill")
                    .font(.system(size: 15))
                    .foregroundStyle(AppTheme.accent)
                Text(draft.connection.displayName.isEmpty ? "新建线路（草稿）" : draft.connection.displayName)
                    .font(.system(size: 13, weight: .semibold))
                    .lineLimit(1)
                Spacer()
                StatusPill(title: "新草稿", symbol: "sparkles", color: AppTheme.accent)
            }
            Text(draft.connection.profile.server.isEmpty ? "尚未配置服务器" : draft.connection.profile.server)
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .background(
            RoundedRectangle(cornerRadius: AppTheme.itemRadius, style: .continuous)
                .fill(AppTheme.selectedBackground)
        )
        .overlay(
            RoundedRectangle(cornerRadius: AppTheme.itemRadius, style: .continuous)
                .strokeBorder(AppTheme.accent.opacity(0.45), lineWidth: 1.5)
        )
    }

    private var detailPane: some View {
        Group {
            if let draft = store.profileDraft {
                ProfileEditorView(
                    store: store,
                    draft: Binding(
                        get: {
                            if let current = store.profileDraft, current.id == draft.id { return current }
                            return draft
                        },
                        set: { updated in
                            if store.profileDraft?.id == draft.id { store.profileDraft = updated }
                        }
                    )
                )
                .id(draft.id)
            } else {
                VStack(spacing: 12) {
                    Image(systemName: "server.rack")
                        .font(.system(size: 38))
                        .foregroundStyle(.tertiary)
                    Text("请选择或新建一条线路")
                        .font(.headline)
                        .foregroundStyle(.secondary)
                    Button("加载当前线路") {
                        ensureProfileSelected()
                    }
                    .appActionStyle()
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
    }

    private func ensureProfileSelected() {
        guard store.profileDraft == nil else {
            selectedProfileID = store.profileDraft?.id
            return
        }
        let targetID: UUID
        if let currentSelected = selectedProfileID, store.configuration.connections.contains(where: { $0.id == currentSelected }) {
            targetID = currentSelected
        } else if store.configuration.connections.contains(where: { $0.id == store.configuration.activeProfileID }) {
            targetID = store.configuration.activeProfileID
        } else if let first = store.configuration.connections.first {
            targetID = first.id
        } else {
            return
        }
        selectedProfileID = targetID
        store.beginEditingProfile(targetID)
    }

    private func handleDraftBecameNil() {
        if let switchID = pendingProfileSwitch {
            pendingProfileSwitch = nil
            selectedProfileID = switchID
            store.beginEditingProfile(switchID)
            return
        }
        if pendingNewProfile {
            pendingNewProfile = false
            store.beginNewProfile()
            selectedProfileID = store.profileDraft?.id
            return
        }
        if let dupID = pendingDuplicateProfile {
            pendingDuplicateProfile = nil
            store.beginNewProfile(copying: dupID)
            selectedProfileID = store.profileDraft?.id
            return
        }
        if let useID = pendingUseProfile {
            pendingUseProfile = nil
            store.selectProfile(useID)
            store.page = .connection
            return
        }
        ensureProfileSelected()
    }

    private func selectProfileItem(_ id: UUID) {
        guard id != store.profileDraft?.id || store.profileDraft?.isNew == true else { return }
        if store.profileDraftHasChanges {
            pendingProfileSwitch = id
            store.cancelProfileEditing()
        } else {
            selectedProfileID = id
            store.closeProfileEditor()
            store.beginEditingProfile(id)
        }
    }

    private func createNewProfile() {
        guard store.configurationReadable, store.configuration.connections.count < 64 else { return }
        if store.profileDraftHasChanges {
            pendingNewProfile = true
            store.cancelProfileEditing()
        } else {
            store.closeProfileEditor()
            store.beginNewProfile()
            selectedProfileID = store.profileDraft?.id
        }
    }

    private func useProfileForConnection(_ id: UUID) {
        guard !store.busy else { return }
        if store.profileDraftHasChanges {
            pendingUseProfile = id
            store.cancelProfileEditing()
        } else {
            store.closeProfileEditor()
            store.selectProfile(id)
            store.page = .connection
        }
    }

    private func duplicateProfile(_ id: UUID) {
        guard store.configurationReadable, store.configuration.connections.count < 64 else { return }
        if store.profileDraftHasChanges {
            pendingDuplicateProfile = id
            store.cancelProfileEditing()
        } else {
            store.closeProfileEditor()
            store.beginNewProfile(copying: id)
            selectedProfileID = store.profileDraft?.id
        }
    }

    private func requestDeleteProfile(_ connection: SavedConnection) {
        deleting = connection
    }
}

private struct ProfileMasterCard: View {
    let connection: SavedConnection
    let isSelected: Bool
    let isActive: Bool
    let isBusy: Bool
    let onSelect: () -> Void
    let onUse: () -> Void
    let onDuplicate: () -> Void
    let onDelete: () -> Void
    let canDelete: Bool
    private var inUse: Bool { isActive && isBusy }

    private var subtitleText: String {
        if let sub = connection.subscription {
            return "订阅 · \(sub.nodeName)"
        }
        if connection.profile.server.isEmpty {
            return "未配置服务器"
        }
        return connection.profile.server
    }

    var body: some View {
        Button {
            onSelect()
        } label: {
            VStack(alignment: .leading, spacing: 7) {
                HStack(alignment: .center, spacing: 8) {
                    Image(systemName: connection.subscription != nil ? "arrow.triangle.swap" : "network")
                        .font(.system(size: 14, weight: .medium))
                        .foregroundStyle(isSelected || isActive ? AppTheme.accent : .secondary)
                        .frame(width: 18)

                    Text(connection.displayName)
                        .font(.system(size: 13, weight: isSelected ? .semibold : .medium))
                        .foregroundStyle(.primary)
                        .lineLimit(1)

                    Spacer()

                    if isActive {
                        StatusPill(
                            title: inUse ? "使用中" : "当前",
                            symbol: inUse ? "network" : "checkmark",
                            color: inUse ? AppTheme.success : AppTheme.accent
                        )
                    }
                }

                Text(subtitleText)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)

                HStack(spacing: 6) {
                    ModeBadge(mode: connection.routingMode)
                    Spacer()
                    Text("\(connection.directDomains.count) 规则 · \(connection.excludedApplications.count) 直连")
                        .font(.system(size: 10))
                        .foregroundStyle(.tertiary)
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
            .background(
                RoundedRectangle(cornerRadius: AppTheme.itemRadius, style: .continuous)
                    .fill(isSelected ? AppTheme.selectedBackground : Color.clear)
            )
            .overlay(
                RoundedRectangle(cornerRadius: AppTheme.itemRadius, style: .continuous)
                    .strokeBorder(isSelected ? AppTheme.accent.opacity(0.35) : AppTheme.subtleBorder.opacity(0.6), lineWidth: isSelected ? 1.5 : 1)
            )
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .contextMenu {
            Button("用于连接") {
                onUse()
            }
            .disabled(isBusy || isActive)

            Button("复制线路…") {
                onDuplicate()
            }

            Divider()

            Button("删除线路…", role: .destructive) {
                onDelete()
            }
            .disabled(inUse || !canDelete)
        }
    }
}

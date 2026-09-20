import SwiftUI
import GoConnectCore

struct ProfileEditorView: View {
    @Bindable var store: AppStore
    @Binding var draft: ConnectionDraft
    @FocusState private var focusedField: Field?
    private enum Field { case name, server, username, group, password }
    private var readOnly: Bool { !store.canSaveProfileDraft }
    private var profileSubtitle: String {
        if draft.isNew {
            return "填写连接信息，保存后加入线路列表"
        }
        if let nodeName = draft.connection.subscription?.nodeName {
            return "订阅节点 · \(nodeName)"
        }
        if draft.connection.profile.server.isEmpty {
            return "未配置服务器"
        }
        return "服务器 · \(draft.connection.profile.server)"
    }

    var body: some View {
        VStack(spacing: 0) {
            // Top Action Bar
            topActionBar

            Divider()

            // Error Banner (if any)
            if let error = store.profileEditorError {
                HStack(spacing: 8) {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .foregroundStyle(AppTheme.warning)
                    Text(error)
                        .font(.callout)
                        .foregroundStyle(AppTheme.warning)
                    Spacer()
                    Button {
                        store.profileEditorError = nil
                    } label: {
                        Image(systemName: "xmark")
                            .foregroundStyle(.secondary)
                    }
                    .buttonStyle(.plain)
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .background(AppTheme.warning.opacity(0.12), in: RoundedRectangle(cornerRadius: AppTheme.itemRadius))
                .padding(.horizontal, 24)
                .padding(.top, 12)
            }

            // Scrollable Inspector Form
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    if readOnly {
                        HStack(spacing: 8) {
                            Image(systemName: "lock.fill")
                                .foregroundStyle(AppTheme.warning)
                            Text("这条线路正在连接使用中，断开连接后可修改配置。")
                                .font(.callout)
                                .foregroundStyle(.secondary)
                        }
                        .padding(.horizontal, 12)
                        .padding(.vertical, 8)
                        .background(AppTheme.warning.opacity(0.08), in: RoundedRectangle(cornerRadius: AppTheme.itemRadius))
                    }

                    // Section 1: 连接凭据
                    credentialsSection

                    // Section 2: 分流模式与应用
                    routingModeSection

                    // Section 3: 公司内网网段
                    companySubnetsSection

                    // Section 4: 直连域名与 IP 例外规则
                    directRulesSection
                }
                .padding(.horizontal, 24)
                .padding(.top, 16)
                .padding(.bottom, 28)
                .frame(maxWidth: 820)
            }
        }
        .onAppear {
            if !readOnly && draft.isNew {
                focusedField = .name
            }
        }
        .onChange(of: draft.connection) { _, _ in
            store.profileEditorError = nil
        }
        .onChange(of: draft.connection.profile.rememberPassword) { _, remember in
            if !remember { store.draftPassword = "" }
        }
    }

    // MARK: - Top Action Bar
    private var topActionBar: some View {
        ViewThatFits(in: .horizontal) {
            HStack(alignment: .center, spacing: 20) {
                profileIdentity
                    .layoutPriority(1)
                Spacer(minLength: 16)
                profileActions
            }

            VStack(alignment: .leading, spacing: 12) {
                profileIdentity
                HStack {
                    Spacer(minLength: 0)
                    profileActions
                }
            }
        }
        .padding(.horizontal, 20)
        .padding(.vertical, 12)
        .background(Color(nsColor: .windowBackgroundColor))
    }

    private var profileIdentity: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 8) {
                Text(draft.isNew ? "新建线路" : (draft.connection.displayName.isEmpty ? "未命名线路" : draft.connection.displayName))
                    .font(.system(size: 17, weight: .bold))
                    .lineLimit(1)

                if readOnly {
                    StatusPill(title: "使用中 (只读)", symbol: "lock.fill", color: AppTheme.warning)
                } else if draft.isNew {
                    StatusPill(title: "新草稿", symbol: "sparkles", color: AppTheme.accent)
                }
            }

            Text(profileSubtitle)
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
        }
    }

    @ViewBuilder
    private var profileActions: some View {
        if store.profileDraftHasChanges {
            HStack(spacing: 8) {
                StatusPill(title: "未保存", symbol: "circle.fill", color: AppTheme.warning)

                Button("放弃更改") {
                    store.cancelProfileEditing()
                }
                .appActionStyle()
                .fixedSize(horizontal: true, vertical: false)
                .keyboardShortcut(.cancelAction)

                Button("保存线路", systemImage: "checkmark") {
                    store.saveProfileDraft()
                }
                .appActionStyle(primary: true)
                .fixedSize(horizontal: true, vertical: false)
                .disabled(readOnly)
                .keyboardShortcut("s", modifiers: .command)
            }
            .transition(.opacity.combined(with: .scale(scale: 0.95)))
        } else {
            Label("已是最新配置", systemImage: "checkmark.circle")
                .font(.caption)
                .foregroundStyle(.secondary)
                .fixedSize()
        }
    }

    // MARK: - Section 1: 连接凭据
    private var credentialsSection: some View {
        Surface {
            HStack {
                Label("连接凭据", systemImage: "key.horizontal.fill")
                    .font(.headline)
                Spacer()
                if draft.connection.subscription != nil {
                    Text("订阅线路")
                        .font(.caption.weight(.medium))
                        .foregroundStyle(AppTheme.accent)
                        .padding(.horizontal, 8)
                        .padding(.vertical, 3)
                        .background(AppTheme.accent.opacity(0.1), in: Capsule())
                }
            }
            .padding(.bottom, 14)

            if let subscription = draft.connection.subscription {
                field("线路名称", text: $draft.connection.profile.name, prompt: "订阅线路", focus: .name)
                HStack(spacing: 8) {
                    Image(systemName: "network")
                        .foregroundStyle(AppTheme.accent)
                    Text(subscription.nodeName)
                        .font(.callout.weight(.medium))
                    Spacer()
                    Button("管理订阅 →") {
                        store.navigate(to: .subscriptions)
                    }
                    .appInlineActionStyle()
                }
                .padding(.top, 12)
                Text("在订阅管理中切换节点；此处保存线路名称与分流模式。")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .padding(.top, 6)
            } else {
                VStack(alignment: .leading, spacing: 14) {
                    ViewThatFits(in: .horizontal) {
                        HStack(alignment: .top, spacing: 16) {
                            field("线路名称", text: $draft.connection.profile.name, prompt: "例如：公司 VPN", focus: .name)
                                .frame(minWidth: 240)
                            field("服务器", text: $draft.connection.profile.server, prompt: "vpn.example.com", focus: .server)
                                .frame(minWidth: 240)
                        }
                        VStack(alignment: .leading, spacing: 14) {
                            field("线路名称", text: $draft.connection.profile.name, prompt: "例如：公司 VPN", focus: .name)
                            field("服务器", text: $draft.connection.profile.server, prompt: "vpn.example.com", focus: .server)
                        }
                    }

                    ViewThatFits(in: .horizontal) {
                        HStack(alignment: .top, spacing: 16) {
                            field("用户名", text: $draft.connection.profile.username, prompt: "VPN 账号", focus: .username)
                                .frame(minWidth: 240)
                            field("连接组（可选）", text: $draft.connection.profile.group, prompt: "由管理员提供", focus: .group)
                                .frame(minWidth: 240)
                        }
                        VStack(alignment: .leading, spacing: 14) {
                            field("用户名", text: $draft.connection.profile.username, prompt: "VPN 账号", focus: .username)
                            field("连接组（可选）", text: $draft.connection.profile.group, prompt: "由管理员提供", focus: .group)
                        }
                    }
                }

                Divider()
                    .padding(.vertical, 8)

                Toggle("将密码保存到钥匙串", isOn: $draft.connection.profile.rememberPassword)
                    .font(.callout)

                if draft.connection.profile.rememberPassword {
                    VStack(alignment: .leading, spacing: 6) {
                        SecureField(draft.original?.profile.rememberPassword == true ? "留空则保留已保存的密码" : "也可在首次连接时填写", text: $store.draftPassword)
                            .textFieldStyle(.roundedBorder)
                            .controlSize(.large)
                            .accessibilityLabel("保存的 VPN 密码")
                            .focused($focusedField, equals: .password)
                        Text("密码加密保存在 macOS 本机钥匙串中，日常连接免密。")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                    .padding(.top, 4)
                } else {
                    Text("连接时在弹窗中输入密码。")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .padding(.top, 2)
                }
            }
        }
        .disabled(readOnly)
    }

    // MARK: - Section 2: 分流模式与应用
    private var routingModeSection: some View {
        Surface {
            HStack {
                Label("分流模式与应用", systemImage: "slider.horizontal.3")
                    .font(.headline)
                Spacer()
            }
            .padding(.bottom, 12)

            Picker("分流模式", selection: $draft.connection.routingMode) {
                ForEach(RoutingMode.allCases) { mode in
                    Text(mode.title).tag(mode)
                }
            }
            .pickerStyle(.segmented)
            .disabled(readOnly)

            Text(draft.connection.routingMode.explanation)
                .font(.callout)
                .foregroundStyle(.secondary)
                .padding(.top, 8)

            Divider()
                .padding(.vertical, 8)

            HStack(alignment: .center) {
                HStack(spacing: 16) {
                    Label("白名单 App \(draft.connection.applications.count)", systemImage: "square.grid.2x2")
                        .font(.callout.weight(.medium))
                    Label("直连 App \(draft.connection.excludedApplications.count)", systemImage: "arrow.triangle.branch")
                        .font(.callout.weight(.medium))
                }
                Spacer()
                Button("管理应用名单 →") {
                    store.navigate(to: .applications)
                }
                .appInlineActionStyle()
            }

            Text("名单随线路保存，连接后由透明服务实时分流；无需在应用中配置 HTTP/SOCKS 代理。")
                .font(.caption)
                .foregroundStyle(.secondary)
                .padding(.top, 4)

            if !draft.connection.conflictingApplications.isEmpty {
                Label("直连名单优先：" + draft.connection.conflictingApplications.map(\.name).joined(separator: "、"), systemImage: "exclamationmark.circle")
                    .font(.caption)
                    .foregroundStyle(.orange)
                    .padding(.top, 6)
            }
        }
    }

    // MARK: - Section 3: 公司内网网段
    @ViewBuilder
    private var companySubnetsSection: some View {
        if draft.connection.subscription == nil {
            Surface {
                HStack(alignment: .center) {
                    VStack(alignment: .leading, spacing: 4) {
                        Label("公司内网网段", systemImage: "building.2.fill")
                            .font(.headline)
                        Text("与应用流量共用本线路的 AnyConnect 会话，适用于 Finder SMB、SSH、内网 Web 等。")
                            .font(.callout)
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    Toggle("启用公司网段访问", isOn: $draft.connection.companyRoutesEnabled)
                        .labelsHidden()
                        .toggleStyle(.switch)
                        .disabled(readOnly)
                }
            }

            if draft.connection.companyRoutesEnabled {
                RemoteNetworksEditor(
                    automatic: $draft.connection.automaticRemoteNetworks,
                    networks: $draft.connection.remoteNetworks,
                    subscription: false,
                    readOnly: readOnly
                )
            }
        } else {
            Surface {
                Label("订阅节点连接", systemImage: "arrow.triangle.swap")
                    .font(.headline)
                Text("订阅流量按 App 模式分流；订阅节点不具备公司内网访问能力。")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .padding(.top, 8)
            }
        }
    }

    // MARK: - Section 4: 直连域名与 IP 例外规则
    private var directRulesSection: some View {
        DomainRulesEditor(rules: $draft.connection.directDomains, readOnly: readOnly)
    }

    private func field(_ title: String, text: Binding<String>, prompt: String, focus: Field) -> some View {
        VStack(alignment: .leading, spacing: 7) {
            Text(title).font(.callout).foregroundStyle(.secondary)
            TextField(prompt, text: text)
                .textFieldStyle(.roundedBorder)
                .controlSize(.large)
                .accessibilityLabel(title)
                .focused($focusedField, equals: focus)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

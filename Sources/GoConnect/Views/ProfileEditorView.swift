import SwiftUI
import GoConnectCore

struct ProfileEditorView: View {
    @Bindable var store: AppStore
    @Binding var draft: ConnectionDraft
    @FocusState private var focusedField: Field?
    private enum Field { case name, server, username, group, password }
    private var readOnly: Bool { !store.canSaveProfileDraft }

    var body: some View {
        VStack(spacing: 0) {
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 16) { profileIdentity; Spacer(minLength: 8); profileActions }
                VStack(alignment: .leading, spacing: 12) { profileIdentity; profileActions }
            }
            .padding(20)
            Divider()

            if let error = store.profileEditorError {
                Label(error, systemImage: "exclamationmark.circle.fill")
                    .font(.callout).foregroundStyle(.red)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 20).padding(.top, 12)
            }

            Form {
                if readOnly {
                    Section {
                        Label("这条线路正在使用中，断开后可修改配置。", systemImage: "lock")
                            .font(.callout).foregroundStyle(.secondary)
                    }
                }
                credentialsSection
                routingModeSection
                companySubnetsSection
                DomainRulesEditor(rules: $draft.connection.directDomains, readOnly: readOnly)
            }
            .formStyle(.grouped)
        }
        .onAppear { if !readOnly && draft.isNew { focusedField = .name } }
        .onChange(of: draft.connection) { _, _ in store.profileEditorError = nil }
        .onChange(of: draft.connection.profile.rememberPassword) { _, remember in
            if !remember { store.draftPassword = "" }
        }
    }

    private var profileIdentity: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(draft.isNew ? "新建线路" : draft.connection.displayName)
                .font(.headline).lineLimit(1)
            Label(readOnly ? "使用中 · 只读" : (store.profileDraftHasChanges ? "有未保存的更改" : "编辑后保存生效"),
                  systemImage: readOnly ? "lock" : (store.profileDraftHasChanges ? "pencil.circle" : "server.rack"))
                .font(.caption).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var profileActions: some View {
        HStack(spacing: 8) {
            Button("放弃更改") { store.cancelProfileEditing() }
                .appActionStyle().disabled(!store.profileDraftHasChanges)
            Button("保存线路", systemImage: "checkmark") { store.saveProfileDraft() }
                .appActionStyle(primary: true)
                .disabled(readOnly || !store.profileDraftHasChanges)
        }
        .fixedSize(horizontal: true, vertical: false)
    }

    private var credentialsSection: some View {
        Section("连接信息") {
            TextField("线路名称", text: $draft.connection.profile.name, prompt: Text("例如：工作网络"))
                .focused($focusedField, equals: .name)
            if let subscription = draft.connection.subscription {
                LabeledContent("订阅节点", value: subscription.nodeName)
                Button("管理订阅", systemImage: "arrow.up.forward") { store.navigate(to: .subscriptions) }
                    .appInlineActionStyle()
            } else {
                TextField("服务器", text: $draft.connection.profile.server, prompt: Text("vpn.example.com"))
                    .focused($focusedField, equals: .server)
                TextField("用户名", text: $draft.connection.profile.username, prompt: Text("VPN 账号"))
                    .focused($focusedField, equals: .username)
                TextField("连接组", text: $draft.connection.profile.group, prompt: Text("可选，由管理员提供"))
                    .focused($focusedField, equals: .group)
                Toggle("将密码保存到钥匙串", isOn: $draft.connection.profile.rememberPassword)
                if draft.connection.profile.rememberPassword {
                    SecureField("VPN 密码", text: $store.draftPassword,
                        prompt: Text(draft.original?.profile.rememberPassword == true ? "留空则保留已有密码" : "也可在连接时填写"))
                        .focused($focusedField, equals: .password)
                    Text("密码加密保存在本机钥匙串。")
                        .font(.caption).foregroundStyle(.secondary)
                } else {
                    Text("连接时在弹窗中输入密码。")
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
        }
        .disabled(readOnly)
    }

    private var routingModeSection: some View {
        Section("分流模式") {
            Picker("应用分流", selection: $draft.connection.routingMode) {
                ForEach(RoutingMode.allCases) { mode in Text(mode.title).tag(mode) }
            }
            .pickerStyle(.segmented).disabled(readOnly)
            Text(draft.connection.routingMode.explanation).font(.callout).foregroundStyle(.secondary)
            LabeledContent("白名单 App / 服务", value: "\(draft.connection.applications.count) 个")
            LabeledContent("直连 App / 服务", value: "\(draft.connection.excludedApplications.count) 个")
            Button("管理应用名单", systemImage: "arrow.up.forward") {
                store.navigate(to: draft.connection.routingMode == .global ? .exclusions : .applications)
            }.appInlineActionStyle()
            if !draft.connection.conflictingApplications.isEmpty {
                Label("直连名单优先：" + draft.connection.conflictingApplications.map(\.name).joined(separator: "、"),
                      systemImage: "exclamationmark.circle")
                    .font(.caption).foregroundStyle(.primary)
            }
        }
    }

    @ViewBuilder private var companySubnetsSection: some View {
        if draft.connection.subscription == nil {
            Section("公司内网") {
                Toggle("启用公司网段访问", isOn: $draft.connection.companyRoutesEnabled).disabled(readOnly)
                Text("与应用流量共用本线路的 AnyConnect 会话，适用于 Finder SMB、SSH 和内网 Web。")
                    .font(.callout).foregroundStyle(.secondary)
            }
            if draft.connection.companyRoutesEnabled {
                RemoteNetworksEditor(automatic: $draft.connection.automaticRemoteNetworks,
                    networks: $draft.connection.remoteNetworks, subscription: false, readOnly: readOnly)
            }
        } else {
            Section("公司内网") {
                Label("订阅节点不具备公司内网访问能力。", systemImage: "info.circle")
                    .font(.callout).foregroundStyle(.secondary)
            }
        }
    }
}

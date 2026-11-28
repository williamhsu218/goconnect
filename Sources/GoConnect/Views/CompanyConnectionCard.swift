import SwiftUI
import GoConnectCore

struct CompanyConnectionCard: View {
    @Bindable var store: AppStore
    @Environment(\.colorScheme) private var colorScheme
    @State private var editing = false
    @State private var showingDetails = false
    private var company: CompanyConnectionStore { store.company }
    private var connectionStatus: String {
        if company.phase == "idle" && store.companyConfiguration.enabled {
            return store.phase == "connected" ? "待连接" : "等待主线路就绪"
        }
        return company.statusTitle
    }

    private var statusColor: Color {
        switch company.phase {
        case "connected": AppTheme.success
        case "reconnecting", "quarantined", "failed": AppTheme.warning
        default: .secondary
        }
    }

    var body: some View {
        Surface {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 12) {
                    Image(systemName: "building.2")
                        .font(.body.weight(.medium))
                        .foregroundStyle(.secondary)
                        .frame(width: 34, height: 34)
                        .background(AppTheme.windowBackground, in: RoundedRectangle(cornerRadius: AppTheme.itemRadius))
                        .accessibilityHidden(true)

                    VStack(alignment: .leading, spacing: 4) {
                        Text("公司副连接").font(.headline)
                        Text(connectionStatus)
                            .font(.callout)
                            .foregroundStyle(statusColor)
                    }
                    Spacer(minLength: 8)
                    Button("配置公司连接", systemImage: "gearshape") { editing = true }
                        .labelStyle(.iconOnly)
                        .appActionStyle()
                        .disabled(!store.canEditCompanyConnectionSettings)
                        .help("配置公司连接")
                    if company.isRunning || company.busy {
                        Button("断开公司连接") { company.disconnect() }
                            .appActionStyle()
                            .disabled(company.phase == "quarantined" || company.phase == "stopping")
                    } else {
                        Button("连接公司内网") { store.connectCompany() }
                            .appActionStyle()
                            .disabled(Product.isIsolatedTestApp || store.phase != "connected" || !store.companyConfiguration.enabled || store.companyConfigurationIssue != nil || store.updateOperationBlocked)
                    }
                }
                if store.companyConfiguration.enabled {
                    FullRowDisclosure("公司连接范围与详情", isExpanded: $showingDetails) {
                        VStack(alignment: .leading, spacing: 5) {
                            Text("仅将已配置的公司地址送入副线路。")
                            Text("\(store.companyConfiguration.networks.count) 个公司网段 · IPv4 / TCP")
                            if company.activeDNSServers.isEmpty {
                                Text(store.companyConfiguration.dnsServers.isEmpty ? "DNS 自动获取" : "DNS 手动指定")
                            } else {
                                Text("本次公司 DNS：\(company.activeDNSServers.joined(separator: "、"))")
                                    .textSelection(.enabled)
                            }
                            if company.phase == "connected" || company.phase == "reconnecting" {
                                HStack(spacing: 10) {
                                    Text("公司会话")
                                    Label(NetworkFormatters.formatBytes(company.received), systemImage: "arrow.down")
                                        .foregroundStyle(AppTheme.downloadTint(for: colorScheme))
                                        .accessibilityLabel("公司会话已接收 \(NetworkFormatters.formatBytes(company.received))")
                                    Label(NetworkFormatters.formatBytes(company.sent), systemImage: "arrow.up")
                                        .foregroundStyle(AppTheme.uploadTint(for: colorScheme))
                                        .accessibilityLabel("公司会话已发送 \(NetworkFormatters.formatBytes(company.sent))")
                                }
                                .monospacedDigit()
                            }
                        }
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                        .padding(.top, 6)
                    }
                    .font(.callout)
                }
                if let issue = store.companyConfigurationIssue { InlineNotice(text: issue) }
                if let message = company.message {
                    Text(message)
                        .font(.callout)
                        .foregroundStyle(statusColor)
                        .textSelection(.enabled)
                }
                if Product.isIsolatedTestApp {
                    Label("隔离测试包：可检查界面和配置；网络接管与服务维护已停用。", systemImage: "testtube.2")
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
        }
        .sheet(isPresented: $editing) { CompanyConnectionSettings(store: store) }
        .sheet(isPresented: Binding(get: { company.passwordRequested }, set: { if !$0 { company.cancelAuthentication() } })) {
            VStack(alignment: .leading, spacing: 16) {
                Text("公司线路身份验证").font(.headline)
                Text("密码仅用于这次公司连接。取消不会断开主线路。").font(.callout).foregroundStyle(.secondary)
                SecureField("公司密码", text: Binding(get: { company.password }, set: { company.password = $0 })).onSubmit { company.submitPassword() }
                if let message = company.message { Text(message).font(.caption).foregroundStyle(.orange) }
                HStack { Button("取消", role: .cancel) { company.cancelAuthentication() }; Spacer(); Button("连接") { company.submitPassword() }.keyboardShortcut(.defaultAction).disabled(company.password.isEmpty) }
            }.padding(24).frame(width: 380)
        }
    }
}

struct CompanyConnectionSettings: View {
    @Bindable var store: AppStore
    @Environment(\.dismiss) private var dismiss
    @State private var draft: CompanyConnectionConfiguration
    @State private var networks: String
    @State private var domains: String
    @State private var dns: String
    @State private var error: String?
    init(store: AppStore, initiallyEnabled: Bool? = nil) {
        self.store = store
        var config = store.companyConfiguration
        if let initiallyEnabled { config.enabled = initiallyEnabled }
        _draft = State(initialValue: config); _networks = State(initialValue: config.networks.joined(separator: "\n"))
        _domains = State(initialValue: config.domains.joined(separator: "\n")); _dns = State(initialValue: config.dnsServers.joined(separator: "\n"))
    }
    private func lines(_ text: String) -> [String] { text.split(whereSeparator: { $0.isWhitespace || $0 == "," || $0 == ";" }).map(String.init) }
    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("公司副连接").font(.title2.weight(.semibold))
            Text("请在连接 HY2 或 VLESS 主线路前完成配置。公司副线路断开后，这些公司地址保持阻断。").font(.callout).foregroundStyle(.secondary)
            Form {
                LabeledContent("主线路", value: store.configuration.activeConnection.displayName)
                Toggle("启用公司副连接", isOn: $draft.enabled)
                Picker("AnyConnect 公司线路", selection: $draft.companyProfileID) {
                    Text("请选择").tag(nil as UUID?)
                    ForEach(store.configuration.connections.filter { $0.subscription == nil && $0.id != store.configuration.activeProfileID }) { line in Text(line.displayName).tag(Optional(line.id)) }
                }
                LabeledContent("公司网段") { TextField("", text: $networks, prompt: Text("10.20.0.0/16，每行一个"), axis: .vertical).labelsHidden().accessibilityLabel("公司网段").textFieldStyle(.roundedBorder).multilineTextAlignment(.leading).lineLimit(3...5).frame(maxWidth: .infinity) }
                LabeledContent("公司域名") { TextField("", text: $domains, prompt: Text("corp.example.com，可留空"), axis: .vertical).labelsHidden().accessibilityLabel("公司域名").textFieldStyle(.roundedBorder).multilineTextAlignment(.leading).lineLimit(2...3).frame(maxWidth: .infinity) }
                LabeledContent("DNS 覆盖") { TextField("", text: $dns, prompt: Text("留空自动获取"), axis: .vertical).labelsHidden().accessibilityLabel("公司 DNS 覆盖").textFieldStyle(.roundedBorder).multilineTextAlignment(.leading).lineLimit(1...2).frame(maxWidth: .infinity) }
            }.formStyle(.grouped)
            Text("支持明确的 IPv4 私网与 TCP 业务。不添加公司默认路由或全局搜索域，连接结束后须手动重连；与局域网、Tailscale 或已有规则冲突时拒绝启用。").font(.caption).foregroundStyle(.secondary)
            if let error { Text(error).font(.callout).foregroundStyle(.red).textSelection(.enabled) }
            HStack {
                Button("取消", role: .cancel) { dismiss() }
                Spacer()
                Button("保存") {
                    guard store.canSaveCompanyConnectionSettings else {
                        error = "请先结束连接、线路编辑或更新，再保存公司配置。"
                        return
                    }
                    draft.primaryProfileID = store.configuration.activeProfileID
                    draft.networks = lines(networks); draft.domains = lines(domains).map { $0.lowercased() }; draft.dnsServers = lines(dns)
                    do {
                        draft = try draft.validated(in: store.configuration)
                        try store.companyFile.save(draft); store.companyConfiguration = draft; dismiss()
                    } catch { self.error = error.localizedDescription }
                }.keyboardShortcut(.defaultAction).disabled(!store.canSaveCompanyConnectionSettings)
            }
        }.padding(24).frame(width: 570, height: 560)
        .onAppear { store.companyDraftOpen = true }
        .onDisappear { store.companyDraftOpen = false }
    }
}

extension AppStore {
    func connectCompany() {
        guard !Product.isIsolatedTestApp else { return }
        do {
            let value = try companyConfiguration.validated(in: configuration)
            guard value.enabled, let id = value.companyProfileID,
                  let profile = configuration.connections.first(where: { $0.id == id })?.profile else { return }
            company.connect(profile: profile, primaryReady: phase == "connected", controllerReserved: routeSnapshot?.company != nil, updateBlocked: updateOperationBlocked)
        } catch { show(error.localizedDescription, error: true) }
    }
}

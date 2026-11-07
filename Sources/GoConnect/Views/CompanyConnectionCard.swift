import SwiftUI
import GoConnectCore

struct CompanyConnectionCard: View {
    @Bindable var store: AppStore
    @State private var editing = false
    private var company: CompanyConnectionStore { store.company }
    private var title: String {
        switch company.phase {
        case "authenticating": "正在验证公司身份"
        case "preparing": "正在验证公司路由与 DNS"
        case "connected": "公司内网已连接"
        case "stopping": "正在结束公司连接"
        case "quarantined": "公司连接已隔离"
        case "failed": "公司连接未完成"
        default: store.companyConfiguration.enabled ? "公司内网待连接" : "公司内网未启用"
        }
    }
    var body: some View {
        GroupBox {
            VStack(alignment: .leading, spacing: 12) {
                HStack(spacing: 10) {
                    Image(systemName: "building.2.crop.circle").font(.title2).foregroundStyle(.secondary)
                    VStack(alignment: .leading, spacing: 3) {
                        Text(title).font(.headline)
                        Text(store.companyConfiguration.enabled ? "保留主线路，仅将已批准的公司地址送入副线路。" : "可在 HY2 主线路之上连接一条 AnyConnect 公司线路。")
                            .font(.callout).foregroundStyle(.secondary)
                    }
                    Spacer(minLength: 8)
                    Button("配置…") { editing = true }.disabled(store.busy || company.isRunning || company.busy)
                    if company.isRunning || company.busy {
                        Button("断开公司连接") { company.disconnect() }.disabled(company.phase == "quarantined" || company.phase == "stopping")
                    } else {
                        Button("连接公司内网") { store.connectCompany() }
                            .disabled(Product.isIsolatedTestApp || store.phase != "connected" || !store.companyConfiguration.enabled || store.updateOperationBlocked)
                    }
                }
                if store.companyConfiguration.enabled {
                    HStack(spacing: 18) {
                        Label("\(store.companyConfiguration.networks.count) 个公司网段", systemImage: "point.3.connected.trianglepath.dotted")
                        Label(store.companyConfiguration.dnsServers.isEmpty ? "DNS 自动获取" : "DNS 手动指定", systemImage: "network")
                        Text("IPv4 · TCP").monospacedDigit()
                    }.font(.caption).foregroundStyle(.secondary)
                }
                if company.phase == "connected" {
                    Text("公司会话  ↓ \(ByteCountFormatter.string(fromByteCount: Int64(clamping: company.received), countStyle: .binary))    ↑ \(ByteCountFormatter.string(fromByteCount: Int64(clamping: company.sent), countStyle: .binary))")
                        .font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                }
                if let message = company.message { Text(message).font(.callout).foregroundStyle(company.phase == "failed" || company.phase == "quarantined" ? .orange : .secondary).textSelection(.enabled) }
                if Product.isIsolatedTestApp {
                    Label("隔离测试包：可检查界面和配置；网络接管与服务维护已停用。", systemImage: "testtube.2")
                        .font(.caption).foregroundStyle(.secondary)
                }
            }.padding(8)
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

private struct CompanyConnectionSettings: View {
    @Bindable var store: AppStore
    @Environment(\.dismiss) private var dismiss
    @State private var draft: CompanyConnectionConfiguration
    @State private var networks: String
    @State private var domains: String
    @State private var dns: String
    @State private var error: String?
    init(store: AppStore) {
        self.store = store
        let config = store.companyConfiguration
        _draft = State(initialValue: config); _networks = State(initialValue: config.networks.joined(separator: "\n"))
        _domains = State(initialValue: config.domains.joined(separator: "\n")); _dns = State(initialValue: config.dnsServers.joined(separator: "\n"))
    }
    private func lines(_ text: String) -> [String] { text.split(whereSeparator: { $0.isWhitespace || $0 == "," || $0 == ";" }).map(String.init) }
    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("公司副连接").font(.title2.weight(.semibold))
            Text("在连接主线路前完成配置。公司副线路断开后，这些公司地址保持阻断。").font(.callout).foregroundStyle(.secondary)
            Form {
                Toggle("启用公司副连接", isOn: $draft.enabled)
                Picker("AnyConnect 公司线路", selection: $draft.companyProfileID) {
                    Text("请选择").tag(nil as UUID?)
                    ForEach(store.configuration.connections.filter { $0.subscription == nil && $0.id != store.configuration.activeProfileID }) { line in Text(line.displayName).tag(Optional(line.id)) }
                }
                LabeledContent("公司网段") { TextField("10.20.0.0/16，每行一个", text: $networks, axis: .vertical).lineLimit(3...5) }
                LabeledContent("公司域名") { TextField("corp.example.com，可留空", text: $domains, axis: .vertical).lineLimit(2...3) }
                LabeledContent("DNS 覆盖") { TextField("留空：自动读取公司 VPN 下发的 DNS", text: $dns, axis: .vertical).lineLimit(1...2) }
            }.formStyle(.grouped)
            Text("首版支持明确的 IPv4 私网与 TCP 业务。不添加默认路由、全局搜索域或自动重连；局域网、Tailscale 与已有规则冲突时拒绝启用。").font(.caption).foregroundStyle(.secondary)
            if let error { Text(error).font(.callout).foregroundStyle(.red).textSelection(.enabled) }
            HStack {
                Button("取消", role: .cancel) { dismiss() }
                Spacer()
                Button("保存") {
                    draft.primaryProfileID = store.configuration.activeProfileID
                    draft.networks = lines(networks); draft.domains = lines(domains).map { $0.lowercased() }; draft.dnsServers = lines(dns)
                    do {
                        draft = try draft.validated(in: store.configuration)
                        try store.companyFile.save(draft); store.companyConfiguration = draft; dismiss()
                    } catch { self.error = error.localizedDescription }
                }.keyboardShortcut(.defaultAction).disabled(!store.companyConfigurationReadable)
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

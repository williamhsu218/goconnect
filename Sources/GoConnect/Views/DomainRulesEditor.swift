import SwiftUI
import GoConnectCore
import UniformTypeIdentifiers

/// The enclosing page owns the rule list and decides when changes are saved.
struct DomainRulesEditor: View {
    let readRules: () -> [DomainRule]
    let writeRules: ([DomainRule]) throws -> Void
    var readOnly = false
    var savesImmediately = false
    @State private var domain = ""
    @State private var subdomains = true
    @State private var error: String?
    @State private var importing = false
    @State private var exporting = false
    @State private var showingHelp = false
    @State private var importPreview: DirectRuleImport?
    private var rules: [DomainRule] { readRules() }

    var body: some View {
        Section("\(savesImmediately ? "规则" : "直连域名与 IP") · \(rules.count) 条") {
            Text("按名单代理和全局模式共用这些规则。匹配的域名、IP 或网段优先直连。")
                .font(.callout).foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            HStack(spacing: 12) {
                Button("导入地址…", systemImage: "square.and.arrow.down") { importing = true }
                    .appInlineActionStyle().disabled(readOnly)
                Button("导出地址…", systemImage: "square.and.arrow.up") { exporting = true }
                    .appInlineActionStyle().disabled(rules.isEmpty)
            }
            if !readOnly {
                HStack(spacing: 12) {
                    TextField("域名或 IP", text: $domain, prompt: Text("example.com 或 192.168.2.0/24"))
                        .labelsHidden().textFieldStyle(.roundedBorder)
                        .frame(maxWidth: .infinity)
                        .accessibilityLabel("添加直连域名或 IP").onSubmit(add)
                    Button("添加规则", systemImage: "plus", action: add)
                        .appActionStyle()
                        .fixedSize(horizontal: true, vertical: false)
                        .disabled(domain.isEmpty)
                }
                Toggle("包含子域名", isOn: $subdomains).font(.callout)
                    .disabled((try? DomainRule(domain).isAddress) == true)
                Text(savesImmediately ? "更改自动保存，下次连接生效。" : "点击“保存线路”后保存更改。")
                    .font(.caption).foregroundStyle(.secondary)
            }
            if let error {
                Label {
                    Text(error).foregroundStyle(.primary)
                } icon: {
                    Image(systemName: "exclamationmark.circle.fill").foregroundStyle(.red)
                }
                .font(.caption)
            }
            if rules.isEmpty {
                Text("尚未设置自定义规则；本地和 Tailscale 例外保留，其它流量按 App 模式分流。").font(.callout).foregroundStyle(.secondary).padding(.vertical, 20)
            } else {
                LazyVStack(spacing: 0) {
                    ForEach(rules) { rule in
                        if rule.id != rules.first?.id { Divider() }
                        HStack(spacing: 12) {
                            Image(systemName: rule.isAddress ? "network" : "globe").foregroundStyle(.secondary)
                            VStack(alignment: .leading, spacing: 5) {
                                Text(rule.domain).font(.body.weight(.medium)).textSelection(.enabled)
                                Text(rule.isAddress ? (rule.domain.contains("/") ? "IP 网段" : "IP 地址") : (rule.includeSubdomains ? "域名及子域名" : "仅完整域名")).font(.caption).foregroundStyle(.secondary)
                            }
                            Spacer()
                            if !readOnly {
                                Button { apply(rules.filter { $0.id != rule.id }) } label: { Image(systemName: "trash") }
                                    .buttonStyle(.borderless).accessibilityLabel("移除 \(rule.domain)")
                            }
                        }.padding(.vertical, 10)
                    }
                }
            }
            FullRowDisclosure("匹配与导入说明", isExpanded: $showingHelp) {
                VStack(alignment: .leading, spacing: 8) {
                    Text("每条线路独立保存。根据目标 IP 和可识别的域名匹配。加密隐藏域名或仅使用 IP 的请求可能无法匹配域名规则。仅添加确实需要直连的子网，避免把公司内网一并放行。")
                    Text("支持 UTF-8 文本，每行一个域名、IP 或网段。导入先预览，再合并新增规则。")
                }
                .font(.caption)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.vertical, 6)
            }
        }
        .fileImporter(isPresented: $importing, allowedContentTypes: [.plainText, UTType(filenameExtension: "list") ?? .plainText]) { result in
            guard !readOnly else { return }
            do {
                let url = try result.get()
                let scoped = url.startAccessingSecurityScopedResource()
                defer { if scoped { url.stopAccessingSecurityScopedResource() } }
                importPreview = try DirectRuleImport(imported: DirectRuleFile.read(url), existing: rules)
                error = nil
            } catch let issue as CocoaError where issue.code == .userCancelled {
                // Closing the file picker leaves the draft untouched.
            } catch { self.error = error.localizedDescription }
        }
        .fileExporter(isPresented: $exporting, document: DirectRuleTextDocument(rules: rules),
                      contentType: .plainText, defaultFilename: "GoConnect-直连地址") { result in
            if case .failure(let issue) = result, (issue as? CocoaError)?.code != .userCancelled {
                error = issue.localizedDescription
            }
        }
        .sheet(item: $importPreview) { preview in
            DirectRuleImportView(preview: preview) {
                guard !readOnly else { return }
                guard rules == preview.original else {
                    error = "草稿已变化，请重新导入以确认新增地址。"; return
                }
                apply(preview.merged)
            }
        }
        .onChange(of: readOnly) { _, locked in
            if locked { importing = false; importPreview = nil }
        }
    }
    private func add() {
        guard !readOnly else { return }
        do {
            let rule = try DomainRule(domain, includeSubdomains: subdomains)
            guard rules.count < 256 || rules.contains(where: { $0.id == rule.id }) else { throw ConfigurationError.invalidDomainRules }
            if apply(rules.filter { $0.id != rule.id } + [rule]) { domain = "" }
        } catch { self.error = error.localizedDescription }
    }

    @discardableResult
    private func apply(_ replacement: [DomainRule]) -> Bool {
        do {
            try writeRules(replacement)
            error = nil
            return true
        } catch {
            self.error = error.localizedDescription
            return false
        }
    }
}

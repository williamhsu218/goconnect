import SwiftUI
import GoConnectCore
import UniformTypeIdentifiers

/// Edits a line's draft; persistence belongs to the enclosing profile editor.
struct DomainRulesEditor: View {
    @Binding var rules: [DomainRule]
    var readOnly = false
    @State private var domain = ""
    @State private var subdomains = true
    @State private var error: String?
    @State private var importing = false
    @State private var exporting = false
    @State private var importPreview: DirectRuleImport?

    var body: some View {
        Section("直连域名与 IP · \(rules.count) 条") {
            Text("匹配这些域名、IP 或网段时直连，优先于 App 分流模式。").font(.callout).foregroundStyle(.secondary).padding(.top, 10)
            HStack(spacing: 12) {
                Button("导入地址…", systemImage: "square.and.arrow.down") { importing = true }
                    .appInlineActionStyle().disabled(readOnly)
                Button("导出地址…", systemImage: "square.and.arrow.up") { exporting = true }
                    .appInlineActionStyle().disabled(rules.isEmpty)
            }.padding(.top, 8)
            Text("支持 UTF-8 文本，每行一个域名、IP 或网段。导入先预览，再合并新增；不改变应用名单和分流模式。")
                .font(.caption).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
            if !readOnly {
                HStack(spacing: 12) {
                    TextField("域名或 IP", text: $domain, prompt: Text("example.com 或 192.168.2.0/24"))
                        .labelsHidden().textFieldStyle(.roundedBorder).controlSize(.large)
                        .frame(maxWidth: .infinity)
                        .accessibilityLabel("添加直连域名或 IP").onSubmit(add)
                    Button("添加规则", systemImage: "plus", action: add)
                        .appActionStyle()
                        .fixedSize(horizontal: true, vertical: false)
                        .disabled(domain.isEmpty)
                }.padding(.top, 18)
                Toggle("包含子域名", isOn: $subdomains).font(.callout).padding(.top, 12)
                    .disabled((try? DomainRule(domain).isAddress) == true)
                Text("添加后点击“保存线路”一起保存；取消编辑不会改变原配置。").font(.caption).foregroundStyle(.secondary).padding(.top, 8)
                if let error {
                    Label {
                        Text(error).foregroundStyle(.primary)
                    } icon: {
                        Image(systemName: "exclamationmark.circle.fill").foregroundStyle(.red)
                    }.font(.caption).padding(.top, 8)
                }
            }
            if rules.isEmpty {
                Text("尚未设置自定义规则；本地和 Tailscale 例外保留，其它流量按 App 模式分流。").font(.callout).foregroundStyle(.secondary).padding(.vertical, 20)
            } else {
                LazyVStack(spacing: 0) {
                    ForEach(rules) { rule in
                        Divider()
                        HStack(spacing: 12) {
                            Image(systemName: rule.isAddress ? "network" : "globe").foregroundStyle(.secondary)
                            VStack(alignment: .leading, spacing: 5) {
                                Text(rule.domain).font(.body.weight(.medium)).textSelection(.enabled)
                                Text(rule.isAddress ? (rule.domain.contains("/") ? "IP 网段" : "IP 地址") : (rule.includeSubdomains ? "域名及子域名" : "仅完整域名")).font(.caption).foregroundStyle(.secondary)
                            }
                            Spacer()
                            if !readOnly {
                                Button { rules.removeAll { $0.id == rule.id } } label: { Image(systemName: "trash") }
                                    .buttonStyle(.borderless).accessibilityLabel("移除 \(rule.domain)")
                            }
                        }.padding(.vertical, 14)
                    }
                }.padding(.top, 18)
            }
            Text("每条线路独立保存。根据目标 IP 和可识别的域名匹配；加密隐藏域名或仅使用 IP 的请求可能无法匹配域名规则。仅添加确实需要直连的子网，避免把公司内网一并放行。")
                .font(.caption).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true).padding(.top, 8)
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
                rules = preview.merged
                error = nil
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
            rules.removeAll { $0.id == rule.id }; rules.append(rule)
            domain = ""; error = nil
        } catch { self.error = error.localizedDescription }
    }
}

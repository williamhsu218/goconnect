import SwiftUI
import GoConnectCore

/// Edits a line's draft; persistence belongs to the enclosing profile editor.
struct DomainRulesEditor: View {
    @Binding var rules: [DomainRule]
    var readOnly = false
    @State private var domain = ""
    @State private var subdomains = true
    @State private var error: String?

    var body: some View {
        Surface {
            HStack { Text("直连域名与 IP").font(.headline); CountBadge(count: rules.count); Spacer() }
            Text("匹配这些域名、IP 或网段时直连，优先于 App 分流模式。").font(.callout).foregroundStyle(.secondary).padding(.top, 10)
            if !readOnly {
                HStack(spacing: 12) {
                    TextField("example.com 或 192.168.2.0/24", text: $domain).textFieldStyle(.roundedBorder).controlSize(.large)
                        .accessibilityLabel("添加直连域名或 IP").onSubmit(add)
                    Button("添加规则", systemImage: "plus", action: add)
                        .appActionStyle(primary: true)
                        .disabled(domain.isEmpty)
                }.padding(.top, 18)
                Toggle("包含子域名", isOn: $subdomains).font(.callout).padding(.top, 12)
                    .disabled((try? DomainRule(domain).isAddress) == true)
                Text("添加后点击“保存线路”一起保存；取消编辑不会改变原配置。").font(.caption).foregroundStyle(.secondary).padding(.top, 8)
                if let error { Label(error, systemImage: "exclamationmark.circle").font(.caption).foregroundStyle(.orange).padding(.top, 8) }
            }
            if rules.isEmpty {
                Text("尚未设置自定义规则；本地和 Tailscale 例外保留，其它流量按 App 模式分流。").font(.callout).foregroundStyle(.secondary).padding(.vertical, 20)
            } else {
                LazyVStack(spacing: 0) {
                    ForEach(rules) { rule in
                        Divider()
                        HStack(spacing: 12) {
                            Image(systemName: rule.isAddress ? "network" : "globe").foregroundStyle(AppTheme.accent)
                            VStack(alignment: .leading, spacing: 5) {
                                Text(rule.domain).font(.body.weight(.medium)).textSelection(.enabled)
                                Text(rule.isAddress ? (rule.domain.contains("/") ? "IP 网段" : "IP 地址") : (rule.includeSubdomains ? "域名及子域名" : "仅完整域名")).font(.caption).foregroundStyle(.secondary)
                            }
                            Spacer()
                            Text("直连").font(.caption.weight(.medium)).foregroundStyle(.green).padding(.horizontal, 9).padding(.vertical, 5).background(Color.green.opacity(0.09), in: Capsule())
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

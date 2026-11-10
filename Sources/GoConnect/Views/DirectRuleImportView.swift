import SwiftUI
import UniformTypeIdentifiers
import GoConnectCore

struct DirectRuleTextDocument: FileDocument {
    static var readableContentTypes: [UTType] { [.plainText] }
    var text: String
    init(rules: [DomainRule]) { text = DirectRuleFile.export(rules) }
    init(configuration: ReadConfiguration) throws {
        guard let data = configuration.file.regularFileContents,
              let value = String(data: data, encoding: .utf8) else { throw DirectRuleFileError.encoding }
        text = value
    }
    func fileWrapper(configuration: WriteConfiguration) throws -> FileWrapper {
        FileWrapper(regularFileWithContents: Data(text.utf8))
    }
}

struct DirectRuleImportView: View {
    let preview: DirectRuleImport
    let confirm: () -> Void
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("导入直连地址").font(.title2.weight(.semibold))
            Text("新增 \(preview.additions.count) 条 · 已有 \(preview.existingCount) 条")
                .font(.headline)
            Text("仅合并到当前线路草稿。已有规则保留，点击“保存线路”后才会保存，下次连接生效。")
                .foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
            if preview.conflictCount > 0 {
                Label("\(preview.conflictCount) 条同名域名的匹配范围不同，将保留已有设置。", systemImage: "info.circle")
                    .font(.callout).fixedSize(horizontal: false, vertical: true)
            }
            if preview.additions.isEmpty {
                ContentUnavailableView("没有新增地址", systemImage: "checkmark.circle",
                                       description: Text("文件中的地址已在当前线路中。"))
                    .frame(height: 160)
            } else {
                List(preview.additions) { rule in
                    HStack {
                        Text(rule.domain).textSelection(.enabled)
                        Spacer()
                        Text(rule.isAddress ? "IP / 网段" : rule.includeSubdomains ? "含子域名" : "仅此域名")
                            .font(.caption).foregroundStyle(.secondary)
                    }
                }.frame(height: 240)
            }
            HStack {
                Spacer()
                Button("取消") { dismiss() }.keyboardShortcut(.cancelAction)
                Button("合并到草稿") { confirm(); dismiss() }
                    .keyboardShortcut(.defaultAction).disabled(preview.additions.isEmpty)
            }
        }.padding(24).frame(width: 520)
    }
}

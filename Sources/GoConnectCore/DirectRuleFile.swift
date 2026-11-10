import Foundation

/// A portable, address-only list. Import never changes a profile or a live route.
public enum DirectRuleFile {
    public static let maximumBytes = 65_536
    public static let maximumRules = 256

    public static func read(_ url: URL) throws -> [DomainRule] {
        let handle = try FileHandle(forReadingFrom: url)
        defer { try? handle.close() }
        return try parse(handle.read(upToCount: maximumBytes + 1) ?? Data())
    }

    public static func parse(_ data: Data) throws -> [DomainRule] {
        guard data.count <= maximumBytes else { throw DirectRuleFileError.tooLarge }
        guard var text = String(data: data, encoding: .utf8) else { throw DirectRuleFileError.encoding }
        if text.hasPrefix("\u{feff}") { text.removeFirst() }
        var rules: [DomainRule] = []
        var seen: [String: Bool] = [:]
        let lines = text.replacingOccurrences(of: "\r\n", with: "\n").replacingOccurrences(of: "\r", with: "\n")
        for (offset, source) in lines.components(separatedBy: "\n").enumerated() {
            let line = source.trimmingCharacters(in: .whitespaces)
            if line.isEmpty || line.hasPrefix("#") || line.hasPrefix("//") { continue }
            let rule: DomainRule
            do {
                let fields = line.split(separator: ",", omittingEmptySubsequences: false)
                    .map { $0.trimmingCharacters(in: .whitespaces) }
                if fields.count == 1 {
                    rule = try DomainRule(line)
                } else {
                    guard fields.count == 3 || fields.count == 4, fields[2] == "DIRECT" else {
                        throw DirectRuleFileError.line(offset + 1)
                    }
                    let kind = fields[0]
                    guard ["DOMAIN", "DOMAIN-SUFFIX", "IP-CIDR", "IP-CIDR6"].contains(kind) else {
                        throw DirectRuleFileError.line(offset + 1)
                    }
                    rule = try DomainRule(fields[1], includeSubdomains: kind == "DOMAIN-SUFFIX")
                    let address = kind == "IP-CIDR" || kind == "IP-CIDR6"
                    guard rule.isAddress == address,
                          !address || (rule.domain.contains("/") && rule.domain.contains(":") == (kind == "IP-CIDR6")),
                          fields.count == 3 || (address && fields[3] == "no-resolve") else {
                        throw DirectRuleFileError.line(offset + 1)
                    }
                }
            } catch { throw DirectRuleFileError.line(offset + 1) }
            if let previous = seen[rule.domain] {
                guard previous == rule.includeSubdomains else { throw DirectRuleFileError.conflict(offset + 1) }
                continue
            }
            seen[rule.domain] = rule.includeSubdomains
            rules.append(rule)
            guard rules.count <= maximumRules else { throw DirectRuleFileError.tooMany }
        }
        guard !rules.isEmpty else { throw DirectRuleFileError.empty }
        return rules
    }

    public static func export(_ rules: [DomainRule]) -> String {
        let lines = rules.map { rule -> String in
            if rule.isAddress { return rule.domain }
            return "\(rule.includeSubdomains ? "DOMAIN-SUFFIX" : "DOMAIN"),\(rule.domain),DIRECT"
        }
        return "# GoConnect 直连地址；UTF-8；导入时合并新增，保留已有设置。\n" + lines.joined(separator: "\n") + "\n"
    }
}

public struct DirectRuleImport: Identifiable {
    public let id = UUID()
    public let original: [DomainRule]
    public let additions: [DomainRule]
    public let existingCount: Int
    public let conflictCount: Int
    public var merged: [DomainRule] { original + additions }

    public init(imported: [DomainRule], existing: [DomainRule]) throws {
        original = existing
        // Revalidate public model values and remove file duplicates through the same parser.
        let validated = try DirectRuleFile.parse(Data(DirectRuleFile.export(imported).utf8))
        additions = validated.filter { rule in !existing.contains { $0.domain == rule.domain } }
        existingCount = validated.count - additions.count
        conflictCount = validated.filter { rule in existing.contains { $0.domain == rule.domain && $0.includeSubdomains != rule.includeSubdomains } }.count
        guard existing.count + additions.count <= DirectRuleFile.maximumRules else { throw DirectRuleFileError.tooMany }
    }
}

public enum DirectRuleFileError: LocalizedError {
    case tooLarge, tooMany, encoding, empty, line(Int), conflict(Int)
    public var errorDescription: String? {
        switch self {
        case .tooLarge: return "文件超过 64 KB，请缩小直连地址清单。"
        case .tooMany: return "合并后最多支持 256 条直连规则；尚未修改草稿。"
        case .encoding: return "请使用 UTF-8 编码的文本文件。"
        case .empty: return "文件中没有可导入的直连地址。"
        case .line(let number): return "第 \(number) 行无效。请填写域名、IP、网段，或使用指向 DIRECT 的 DOMAIN / DOMAIN-SUFFIX / IP-CIDR / IP-CIDR6 规则；不支持完整订阅或代理规则。"
        case .conflict(let number): return "第 \(number) 行与文件内同名域名的匹配范围冲突，请保留一种设置后重试。"
        }
    }
}

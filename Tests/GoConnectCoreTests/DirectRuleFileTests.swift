import XCTest
@testable import GoConnectCore

final class DirectRuleFileTests: XCTestCase {
    private func parse(_ text: String) throws -> [DomainRule] { try DirectRuleFile.parse(Data(text.utf8)) }

    func testPlainTextBOMCommentsCanonicalizationAndDeduplication() throws {
        let rules = try parse("\u{feff}# UTF-8\r\n\n Example.COM. \r\n*.example.com\n// note\n192.168.2.129/24\nFD00:1234::1/64")
        XCTAssertEqual(rules.map(\.domain), ["example.com", "192.168.2.0/24", "fd00:1234::/64"])
        XCTAssertEqual(rules.map(\.includeSubdomains), [true, false, false])
    }

    func testDirectOnlyFormatsAndExportRoundTrip() throws {
        let text = "DOMAIN,exact.example.com,DIRECT\nDOMAIN-SUFFIX,example.org,DIRECT\nIP-CIDR,192.168.2.1/24,DIRECT,no-resolve\nIP-CIDR6,fd00:1234::1/64,DIRECT\n192.168.1.2"
        let rules = try parse(text)
        XCTAssertFalse(rules[0].includeSubdomains)
        XCTAssertTrue(rules[1].includeSubdomains)
        XCTAssertEqual(try parse(DirectRuleFile.export(rules)), rules)
    }

    func testRejectsEntireInvalidOrProxyListWithCorrectLineNumber() {
        for line in ["https://example.com", "DOMAIN-SUFFIX,example.com,PROXY", "MATCH,DIRECT", "GEOIP,CN,DIRECT", "PROCESS-NAME,Claude,DIRECT", "rules:", "IP-CIDR,example.org,DIRECT", "DOMAIN,192.168.2.1,DIRECT", "IP-CIDR6,192.168.2.1/24,DIRECT", "DOMAIN-SUFFIX,example.org,DIRECT,no-resolve", "0.0.0.0/0", "::/0", "127.0.0.1"] {
            XCTAssertThrowsError(try parse("# note\r\nvalid.example\r\n" + line)) { error in
                guard case DirectRuleFileError.line(3) = error else { return XCTFail("Unexpected error: \(error)") }
            }
        }
        XCTAssertThrowsError(try parse("example.com\nDOMAIN,example.com,DIRECT"))
    }

    func testEncodingEmptySizeAndRuleLimits() throws {
        XCTAssertThrowsError(try DirectRuleFile.parse(Data([0xff, 0xfe])))
        XCTAssertThrowsError(try parse("# only comment\n"))
        XCTAssertThrowsError(try DirectRuleFile.parse(Data(repeating: 32, count: DirectRuleFile.maximumBytes + 1)))
        let full = (0..<256).map { "host\($0).example.com" }.joined(separator: "\n")
        let rules = try parse(full)
        XCTAssertEqual(rules.count, 256)
        XCTAssertThrowsError(try parse(full + "\nanother.example.com"))
        XCTAssertEqual(try DirectRuleImport(imported: rules, existing: rules).additions.count, 0)
        XCTAssertThrowsError(try DirectRuleImport(imported: parse("another.example.com"), existing: rules))
    }

    func testMergePreservesExistingScopeAndDraftIsolation() throws {
        var config = Configuration()
        config.profile.name = "Test"
        config.profile.server = "https://vpn.example.com"
        config.profile.username = "test"
        config.routingMode = .global
        config.applications = [AllowedApplication(name: "AI", bundleID: "test.ai", path: "/Applications/AI.app")]
        config.excludedApplications = [AllowedApplication(name: "Direct", bundleID: "test.direct", path: "/Applications/Direct.app")]
        config.directDomains = [try DomainRule("example.com", includeSubdomains: false)]
        let original = config
        let preview = try DirectRuleImport(imported: parse("example.com\nnew.example.org"), existing: config.directDomains)
        XCTAssertEqual(preview.existingCount, 1)
        XCTAssertEqual(preview.conflictCount, 1)
        XCTAssertFalse(preview.merged[0].includeSubdomains)
        var draft = ConnectionDraft(editing: config.activeConnection)
        draft.connection.directDomains = preview.merged
        XCTAssertEqual(config, original, "Importing into a draft must not persist changes")
        try config.save(draft)
        var expected = original
        expected.directDomains.append(try DomainRule("new.example.org"))
        XCTAssertEqual(config, expected, "Only direct addresses may change on save")
    }

    func testDomesticPresetDoesNotMatchProtectedAIOrInfrastructureDomains() throws {
        let root = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        let rules = try DirectRuleFile.read(root.appendingPathComponent("examples/direct-domestic.txt"))
        let protected = ["claude.ai", "api.anthropic.com", "chatgpt.com", "api.openai.com", "cdn.oaistatic.com", "files.oaiusercontent.com", "gemini.google.com", "generativelanguage.googleapis.com", "antigravity.google", "github.com", "raw.githubusercontent.com", "api.deepseek.com", "doubao.com", "workbuddy.ai", "aistudio.google.com", "qwen.ai", "dashscope.aliyuncs.com", "cloudflare-dns.com", "dns.google"]
        for host in protected {
            XCTAssertFalse(rules.contains { host == $0.domain || ($0.includeSubdomains && host.hasSuffix("." + $0.domain)) }, host)
        }
        XCTAssertFalse(rules.contains( where: \.isAddress))
    }
}

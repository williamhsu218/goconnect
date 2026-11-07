import Foundation
import GoConnectCore

extension AppStore {
    var companyConfigurationIssue: String? {
        guard companyConfigurationReadable else { return "公司配置无法读取；请保留原文件并检查诊断。" }
        guard companyConfiguration.enabled else { return nil }
        do { _ = try companyConfiguration.validated(in: configuration); return nil }
        catch { return error.localizedDescription }
    }

    /// Explicit, additive, read-only import; passwords and operational state stay in production.
    func copyProductionProfilesForTesting() {
        guard Product.isNetworkTestApp, !busy, !company.isRunning, !company.busy,
              !companyDraftOpen, profileDraft == nil, configurationReadable, subscriptionsReadable else { return }
        do {
            let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
                .appendingPathComponent("com.willhsu.GoConnect")
            let configURL = base.appendingPathComponent("configuration.json")
            let cacheURL = base.appendingPathComponent("subscriptions.json")
            let source = try Self.readTestImport(Configuration.self, from: configURL)
            let cache = FileManager.default.fileExists(atPath: cacheURL.path)
                ? try Self.readTestImport([Subscription].self, from: cacheURL) : []
            let merged = try Self.mergeTestProfiles(source: source, cache: cache, into: configuration, subscriptions: subscriptions)
            try subscriptionFile.save(merged.1)
            try file.save(merged.0)
            configuration = merged.0; subscriptions = merged.1
            show("线路与订阅已复制到测试区；已有测试配置保留，VPN 密码需重新输入。")
        } catch { show("无法复制正式版线路：" + error.localizedDescription, error: true) }
    }

    private static func readTestImport<T: Decodable>(_ type: T.Type, from url: URL) throws -> T {
        let attrs = try FileManager.default.attributesOfItem(atPath: url.path)
        guard attrs[.type] as? FileAttributeType == .typeRegular,
              (attrs[.ownerAccountID] as? NSNumber)?.uint32Value == getuid(),
              ((attrs[.size] as? NSNumber)?.intValue ?? Int.max) <= 16 * 1024 * 1024 else {
            throw CompanyConnectionError.invalid("源文件类型、所有者或大小不符合要求。")
        }
        return try JSONDecoder().decode(type, from: Data(contentsOf: url))
    }

    static func mergeTestProfiles(source: Configuration, cache: [Subscription], into current: Configuration,
                                  subscriptions: [Subscription]) throws -> (Configuration, [Subscription]) {
        guard source.schemaVersion == current.schemaVersion else { throw CompanyConnectionError.invalid("正式版配置版本不兼容。") }
        var result = current
        let fresh = current.connections.count == 1 && current.connections[0].profile.server.isEmpty && current.connections[0].subscription == nil
        if fresh { result.connections = [] }
        var ids = Set(result.connections.map(\.id))
        for var line in source.connections where !ids.contains(line.id) {
            line.profile.rememberPassword = false
            result.connections.append(line); ids.insert(line.id)
        }
        guard !result.connections.isEmpty, result.connections.count <= 64 else { throw CompanyConnectionError.invalid("复制后线路数量须为 1–64 条。") }
        if fresh { result.activeProfileID = source.activeProfileID }
        var items = subscriptions
        var subscriptionIDs = Set(items.map(\.id))
        for item in cache where !subscriptionIDs.contains(item.id) { items.append(item); subscriptionIDs.insert(item.id) }
        return (result, items)
    }
}

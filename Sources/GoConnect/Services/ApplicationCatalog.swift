import AppKit
import GoConnectCore

struct ApplicationCatalog {
    static func application(at url: URL) -> AllowedApplication? {
        let resolved = url.resolvingSymlinksInPath().standardizedFileURL
        guard resolved.pathExtension == "app", let bundle = Bundle(url: resolved),
              let id = bundle.bundleIdentifier, bundle.executableURL != nil else { return nil }
        let info = bundle.infoDictionary ?? [:]
        let name = info["CFBundleDisplayName"] as? String ?? info["CFBundleName"] as? String ?? resolved.deletingPathExtension().lastPathComponent
        return AllowedApplication(name: name, bundleID: id, path: resolved.path)
    }
    static func executable(at url: URL) -> AllowedApplication? {
        let standardized = url.standardizedFileURL
        let resolved = standardized.resolvingSymlinksInPath()
        guard standardized.path == resolved.path, resolved.pathExtension.lowercased() != "app",
              FileManager.default.isExecutableFile(atPath: resolved.path),
              let values = try? resolved.resourceValues(forKeys: [.isRegularFileKey]), values.isRegularFile == true else {
            return nil
        }
        let name = resolved.lastPathComponent
        guard !name.isEmpty else { return nil }
        return AllowedApplication(name: name, bundleID: "executable." + name, path: resolved.path)
    }
    static func scan() -> [AllowedApplication] {
        let roots = [URL(fileURLWithPath: "/Applications"), FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Applications"), URL(fileURLWithPath: "/System/Applications")]
        var result: [AllowedApplication] = []
        for root in roots {
            guard let iterator = FileManager.default.enumerator(at: root, includingPropertiesForKeys: [.isDirectoryKey], options: [.skipsHiddenFiles, .skipsPackageDescendants]) else { continue }
            for case let url as URL in iterator {
                if let app = application(at: url), app.bundleID != Product.bundleID { result.append(app) }
            }
        }
        return Dictionary(result.map { ($0.path, $0) }, uniquingKeysWith: { first, _ in first }).values.sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
    }
}

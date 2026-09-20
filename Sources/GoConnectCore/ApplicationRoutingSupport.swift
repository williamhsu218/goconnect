import Foundation

/// Validates canonical App bundles and standalone executable services. Legacy
/// tagging additionally requires a native App launcher. Recheck before
/// connecting: a saved routing target may have been removed or replaced.
public enum ApplicationRoutingSupport {
    public static func issue(for app: AllowedApplication, requiresLauncher: Bool = true) -> String? {
        let path = app.path
        let url = URL(fileURLWithPath: path)
        let files = FileManager.default
        guard files.fileExists(atPath: path) else { return "分流目标已移除或移动，请重新选择。" }
        guard path.hasPrefix("/"), !path.contains(where: { ",\0\r\n".contains($0) }),
              url.standardizedFileURL.path == path, url.resolvingSymlinksInPath().path == path else {
            return "分流路径不受支持，请选择真实安装位置。"
        }
        if app.isExecutableService {
            guard !requiresLauncher else { return "独立可执行服务不支持旧版 App 启动标记。" }
            guard files.isExecutableFile(atPath: path),
                  let values = try? url.resourceValues(forKeys: [.isRegularFileKey]), values.isRegularFile == true else {
                return "服务文件无效、不可执行或已被替换，请重新选择。"
            }
            return nil
        }
        if requiresLauncher && (path.hasPrefix("/System/") || ["com.apple.Safari", "com.apple.SafariTechnologyPreview"].contains(app.bundleID)
            || ["Safari.app", "Safari Technology Preview.app"].contains(url.lastPathComponent)) {
            return "系统应用及 Safari 不支持当前 App 分流启动方式。"
        }
        if requiresLauncher && files.fileExists(atPath: url.appendingPathComponent("Wrapper").path) {
            return "iPhone / iPad 版应用暂不支持 App 分流，请使用 Mac 版。"
        }
        guard url.pathExtension.lowercased() == "app" else {
            return "应用路径不受支持，请从实际安装位置重新选择。"
        }
        if !requiresLauncher {
            guard let values = try? url.resourceValues(forKeys: [.isDirectoryKey]), values.isDirectory == true else { return "应用目录无效，请重新选择。" }
            return nil
        }
        let infoURL = url.appendingPathComponent("Contents/Info.plist")
        guard let data = try? Data(contentsOf: infoURL),
              let info = (try? PropertyListSerialization.propertyList(from: data, format: nil)) as? [String: Any],
              let name = info["CFBundleExecutable"] as? String, !name.isEmpty,
              name != ".", !name.contains(where: { "/\0\r\n".contains($0) }) else {
            return "无法读取 Mac 应用的启动文件，请重新安装或选择 Mac 版。"
        }
        let binary = url.appendingPathComponent("Contents/MacOS").appendingPathComponent(name).resolvingSymlinksInPath()
        guard binary.path.hasPrefix(path + "/Contents/"), files.isExecutableFile(atPath: binary.path),
              let values = try? binary.resourceValues(forKeys: [.isRegularFileKey]), values.isRegularFile == true else {
            return "应用启动文件无效或已移动，请重新安装或选择应用。"
        }
        return nil
    }

    public static func validate(_ apps: [AllowedApplication], requiresLauncher: Bool = true) throws {
        guard apps.count <= 256 else { throw SupportError(message: "每份名单最多支持 256 个 App 或服务。") }
        for app in apps {
            if let issue = issue(for: app, requiresLauncher: requiresLauncher) { throw SupportError(message: "「\(app.name)」：\(issue)") }
        }
    }

    private struct SupportError: LocalizedError {
        let message: String
        var errorDescription: String? { message }
    }
}

import Foundation

/// Keep a CLI's entry point in the configuration; send only its currently
/// resolved, exact executable path to the privileged routing service.
public struct ExecutableRoutingPath {
    private let homebrewPrefixes: [String]

    public init(homebrewPrefixes: [String] = ["/opt/homebrew", "/usr/local"]) {
        self.homebrewPrefixes = homebrewPrefixes
    }

    public func savedPath(for url: URL) throws -> String {
        let path = url.standardizedFileURL.path
        let entry = homebrewEntry(for: path) ?? path
        _ = try resolvedPath(for: entry)
        return entry
    }

    public func resolvedPath(for path: String) throws -> String {
        try validateSyntax(path)
        // This also recovers older records whose version directory was removed.
        // Accept the bin entry only if it still points into the same package
        // and to the same relative executable, allowing just the version to vary.
        let entry = homebrewEntry(for: path) ?? path
        let resolved = URL(fileURLWithPath: entry).resolvingSymlinksInPath().standardizedFileURL.path
        try validateSyntax(resolved)
        let files = FileManager.default
        guard files.isExecutableFile(atPath: resolved),
              let attributes = try? files.attributesOfItem(atPath: resolved),
              attributes[.type] as? FileAttributeType == .typeRegular else {
            throw PathError(message: "服务入口无效、不可执行或已移除，请确认程序仍已安装。")
        }
        return resolved
    }

    private func validateSyntax(_ path: String) throws {
        guard path.hasPrefix("/"), !path.contains(where: { ",\0\r\n".contains($0) }),
              URL(fileURLWithPath: path).standardizedFileURL.path == path else {
            throw PathError(message: "服务路径不受支持，请选择有效的可执行文件或固定入口。")
        }
    }

    private func homebrewEntry(for path: String) -> String? {
        for prefix in homebrewPrefixes {
            guard path.hasPrefix(prefix + "/") else { continue }
            let parts = String(path.dropFirst(prefix.count + 1)).split(separator: "/").map(String.init)
            guard parts.count >= 4, ["Caskroom", "Cellar"].contains(parts[0]) else { continue }
            let entry = prefix + "/bin/" + parts.last!
            let target = URL(fileURLWithPath: entry).resolvingSymlinksInPath().standardizedFileURL.path
            guard target.hasPrefix(prefix + "/") else { continue }
            let current = String(target.dropFirst(prefix.count + 1)).split(separator: "/").map(String.init)
            guard current.count == parts.count, current.prefix(2) == parts.prefix(2),
                  current.dropFirst(3) == parts.dropFirst(3) else { continue }
            return entry
        }
        return nil
    }

    private struct PathError: LocalizedError {
        let message: String
        var errorDescription: String? { message }
    }
}

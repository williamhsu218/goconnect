import Foundation

/// Configuring an application is not evidence that all of its helper processes
/// use the proxy. Never label an application as verified from its bundle ID.
public enum ProxyApplicationSupport {
    public static func guidance(for app: AllowedApplication) -> String {
        if app.bundleID == "com.google.Chrome" {
            return "可用独立浏览器配置启动代理；已有窗口和 QUIC 不保证遵循代理。"
        }
        return "需在应用自身设置 HTTP/SOCKS 代理；辅助进程、登录和 UDP 尚未验证。"
    }
    public static func chromeArguments(port: UInt16, profileDirectory: String) -> [String] {
        ["--user-data-dir=" + profileDirectory, "--proxy-server=http://127.0.0.1:\(port)", "--disable-quic", "--no-first-run", "--no-default-browser-check"]
    }
}

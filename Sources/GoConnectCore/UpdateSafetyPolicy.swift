import Foundation

/// Only application state is needed to make this decision; no network or credential reads.
public struct UpdateSafetyState: Equatable, Sendable {
    public var connectionOrServiceActive: Bool
    public var hasUnsavedDraft: Bool
    public var isTestingNodes: Bool
    public var isUpdatingSubscriptions: Bool
    public var isMaintainingService: Bool

    public init(connectionOrServiceActive: Bool = false, hasUnsavedDraft: Bool = false,
                isTestingNodes: Bool = false, isUpdatingSubscriptions: Bool = false,
                isMaintainingService: Bool = false) {
        self.connectionOrServiceActive = connectionOrServiceActive
        self.hasUnsavedDraft = hasUnsavedDraft
        self.isTestingNodes = isTestingNodes
        self.isUpdatingSubscriptions = isUpdatingSubscriptions
        self.isMaintainingService = isMaintainingService
    }

    /// Checking, downloading and signature verification do not modify app data or networking.
    /// These gates apply only after the user requests installation/relaunch.
    public var preparationBlockingReason: String? {
        if hasUnsavedDraft { return "请先保存或放弃正在编辑的线路，再安装更新。" }
        if isTestingNodes { return "请先结束节点测试，再安装更新。" }
        if isUpdatingSubscriptions { return "请先等待订阅操作完成，再安装更新。" }
        if isMaintainingService { return "请先等待本机网络服务维护完成，再安装更新。" }
        return nil
    }
    public var blockingReason: String? {
        if let reason = preparationBlockingReason { return reason }
        if connectionOrServiceActive { return "正在等待 VPN 正常断开和网络服务空闲，再安装更新。" }
        return nil
    }
}

/// Release configuration is sealed into the signed app, never read from a user-entered URL.
public struct AppUpdateConfiguration: Equatable, Sendable {
    public static let feedURLString = "https://raw.githubusercontent.com/williamhsu218/goconnect/updates/appcast.xml"
    public let feedURL: URL
    public let publicKey: String
    public let channel: String
    public var allowedChannels: Set<String> { channel == "preview" ? ["preview"] : [] }

    public init?(bundleIdentifier: String?, info: [String: Any]) {
        let qa = bundleIdentifier == "com.willhsu.GoConnect.UpdateQA" && info["GoConnectUpdateQAMode"] as? Bool == true
        let production = bundleIdentifier == "com.willhsu.GoConnect" && info["GoConnectUpdateQAMode"] as? Bool != true
        guard production || qa,
              info["GoConnectSigningMode"] as? String == "team",
              info["GoConnectUpdatesEnabled"] as? Bool == true,
              info["SUAllowsAutomaticUpdates"] as? Bool == false,
              info["SUAutomaticallyUpdate"] as? Bool == false,
              info["SUEnableAutomaticChecks"] as? Bool == false,
              info["SURequireSignedFeed"] as? Bool == true,
              info["SUVerifyUpdateBeforeExtraction"] as? Bool == true,
              let feed = info["SUFeedURL"] as? String,
              let url = URL(string: feed),
              let key = info["SUPublicEDKey"] as? String,
              let data = Data(base64Encoded: key), data.count == 32 else { return nil }
        if qa {
            guard url.scheme == "http", url.host == "127.0.0.1", let port = url.port, (1...65535).contains(port),
                  url.path == "/appcast.xml", url.user == nil, url.password == nil,
                  url.query == nil, url.fragment == nil else { return nil }
        } else {
            guard feed == Self.feedURLString else { return nil }
        }
        let channel = info["GoConnectUpdateChannel"] as? String ?? "stable"
        guard channel == "stable" || channel == "preview" else { return nil }
        self.publicKey = key
        self.feedURL = url
        self.channel = channel
    }
}

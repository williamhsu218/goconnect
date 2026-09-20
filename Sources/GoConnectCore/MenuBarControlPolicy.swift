import Foundation

public struct MenuBarControlPolicy: Equatable, Sendable {
    public let actionTitle: String
    public let actionEnabled: Bool
    public let quickConnectEnabled: Bool

    public init(state: VPNControlState, busy: Bool, configurationReadable: Bool,
                editingProfile: Bool, testingNodes: Bool) {
        quickConnectEnabled = !busy && !state.isOn && state.canToggle
            && configurationReadable && !editingProfile && !testingNodes
        actionEnabled = state.canToggle && (state.isOn || quickConnectEnabled)
        if !state.canToggle { actionTitle = state.title }
        else if [.waitingForPassword, .connecting, .authorizing].contains(state) { actionTitle = "取消连接" }
        else { actionTitle = state.isOn ? "断开连接" : "连接 VPN" }
    }

    public static func shortTitle(_ text: String) -> String {
        let line = text.components(separatedBy: .newlines).joined(separator: " ")
        return line.count <= 30 ? line : String(line.prefix(29)) + "…"
    }
}

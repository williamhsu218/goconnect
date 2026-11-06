import SwiftUI

/// Only mounted by the separately signed UpdateQA bundle. No network panels or
/// menu-bar actions are mounted, and all records/diagnostics use its temporary data.
struct AppUpdateQAHarness: View {
    let store: AppStore
    @ObservedObject var updater: AppUpdater
    var body: some View {
        Form {
            Section("GoConnect 更新验收") {
                LabeledContent("验收版本", value: Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "—")
                Text("此窗口只验收更新流程，使用独立临时数据。")
            }
            AppUpdateSettings(updater: updater)
            Section("更新保护状态") {
                Toggle("模拟 VPN 处于连接状态", isOn: Binding(get: { store.phase == "connected" },
                    set: { store.phase = $0 ? "connected" : "idle" }))
                    .disabled(updater.blocksNewConnection)
                Toggle("模拟线路草稿未保存", isOn: Binding(get: { store.profileDraft != nil },
                    set: { if $0 { store.beginNewProfile() } else { store.profileDraft = nil } }))
                    .disabled(updater.blocksNewConnection)
                Text(store.updateSafetyState.blockingReason ?? "可以检查更新")
            }
        }.formStyle(.grouped)
    }
}

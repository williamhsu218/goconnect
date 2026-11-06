import SwiftUI
import GoConnectCore

struct MenuBarSettings: View {
    var showMenuBar: () -> Void = {}
    @AppStorage(MenuBarPreferences.showsNetworkRatesKey) private var showsNetworkRates = false
    @AppStorage(MenuBarPreferences.rateDirectionKey) private var rateDirection = MenuBarRateDirection.both

    var body: some View {
        Section("菜单栏") {
            Toggle("显示网络速率", isOn: $showsNetworkRates)
                .accessibilityIdentifier("MenuBarShowsNetworkRates")
            Picker("显示方向", selection: $rateDirection) {
                ForEach(MenuBarRateDirection.allCases, id: \.self) { direction in
                    Text(direction.title).tag(direction)
                }
            }
            .disabled(!showsNetworkRates)
            .accessibilityIdentifier("MenuBarRateDirection")
            Text("显示当前 GoConnect 线路所选方向的速率；断开或暂无新数据时显示“—”。仅统计当前线路，不代表整台 Mac 的总网速。")
                .font(.caption).foregroundStyle(.secondary)
            Button("重新显示菜单栏快捷菜单", action: showMenuBar)
        }
    }
}

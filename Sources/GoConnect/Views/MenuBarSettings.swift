import SwiftUI
import GoConnectCore

struct MenuBarSettings: View {
    var showMenuBar: () -> Void = {}
    @AppStorage(MenuBarPreferences.showsNetworkRatesKey) private var showsNetworkRates = false
    @AppStorage(MenuBarPreferences.rateDirectionKey) private var rateDirection = MenuBarRateDirection.both
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var previewImage: NSImage {
        let icon = MenuBarStatusIcon.image(for: .connected)
        guard showsNetworkRates else { return icon }
        let sample = MenuBarRatePresentation(
            enabled: true,
            direction: rateDirection,
            state: .connected,
            available: true,
            sampledAt: Date(),
            download: 12_600_000,
            upload: 842_000
        )
        return MenuBarRateImage.image(icon: icon, lines: sample.lines)
    }

    var body: some View {
        Section("菜单栏") {
            HStack(spacing: 12) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("菜单栏效果预览")
                        .font(.callout.weight(.medium))
                    Text(showsNetworkRates ? "已连接状态 · \(rateDirection.title)速率" : "仅显示连接状态图标")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Spacer(minLength: 8)
                HStack(spacing: 8) {
                    Image(nsImage: previewImage)
                        .renderingMode(.template)
                        .foregroundStyle(.primary)
                        .fixedSize()
                }
                .padding(.horizontal, 10)
                .frame(height: 26)
                .background(Color.primary.opacity(0.06), in: Capsule())
                .overlay(Capsule().strokeBorder(AppTheme.subtleBorder, lineWidth: 0.75))
                .accessibilityHidden(true)
            }

            Toggle("显示网络速率", isOn: $showsNetworkRates)
                .accessibilityIdentifier("MenuBarShowsNetworkRates")

            if showsNetworkRates {
                Picker("显示方向", selection: $rateDirection) {
                    ForEach(MenuBarRateDirection.allCases, id: \.self) { direction in
                        Text(direction.title).tag(direction)
                    }
                }
                .accessibilityIdentifier("MenuBarRateDirection")
                .transition(reduceMotion ? .opacity : .opacity.combined(with: .move(edge: .top)))
            }

            Text("仅统计当前 GoConnect 线路；断开或暂无新数据时显示“—”，不代表整台 Mac 的总网速。")
                .font(.caption)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)

            HStack(spacing: 12) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("菜单栏快捷菜单")
                    Text("刘海屏遮挡或图标不可见时，重置并弹出菜单")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Spacer(minLength: 8)
                Button("重新显示", action: showMenuBar)
                    .appActionStyle()
                    .fixedSize()
            }
        }
        .animation(reduceMotion ? nil : .smooth(duration: 0.22), value: showsNetworkRates)
    }
}

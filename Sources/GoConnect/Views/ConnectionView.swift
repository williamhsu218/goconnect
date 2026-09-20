import SwiftUI
import GoConnectCore

struct ConnectionView: View {
    @Bindable var store: AppStore

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: AppTheme.sectionSpacing) {
                PageHeading(title: "连接", subtitle: "选择已保存线路和分流模式，开启 VPN。")

                VPNConnectionCard(store: store)

                if store.recoveryRequired {
                    HStack(alignment: .top, spacing: 10) {
                        Image(systemName: "exclamationmark.triangle.fill")
                            .foregroundStyle(.orange)
                        VStack(alignment: .leading, spacing: 2) {
                            Text("网络恢复提示")
                                .font(.subheadline.weight(.semibold))
                            Text("网络进程尚未完全退出，已暂停重连。请导出诊断，重启 macOS 后再试。")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                    .padding(12)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Color.orange.opacity(0.08), in: RoundedRectangle(cornerRadius: AppTheme.itemRadius))
                    .overlay(
                        RoundedRectangle(cornerRadius: AppTheme.itemRadius)
                            .strokeBorder(Color.orange.opacity(0.3), lineWidth: 1)
                    )
                }

                ViewThatFits(in: .horizontal) {
                    HStack(alignment: .top, spacing: AppTheme.sectionSpacing) {
                        ApplicationRoutingCard(store: store)
                        DirectRoutingCard(store: store)
                    }
                    VStack(alignment: .leading, spacing: AppTheme.sectionSpacing) {
                        ApplicationRoutingCard(store: store)
                        DirectRoutingCard(store: store)
                    }
                }
            }
            .padding(AppTheme.pagePadding)
            .frame(maxWidth: AppTheme.contentWidth, alignment: .topLeading)
            .frame(maxWidth: .infinity, alignment: .topLeading)
        }
    }
}

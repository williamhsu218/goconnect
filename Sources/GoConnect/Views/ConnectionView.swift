import SwiftUI
import GoConnectCore

struct ConnectionView: View {
    @Bindable var store: AppStore

    private let groupSpacing: CGFloat = 16

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: groupSpacing) {
                if Product.isNetworkTestApp { TestWorkspaceCard(store: store) }
                VPNConnectionCard(store: store)
                if store.showsCompanyConnection {
                    CompanyConnectionCard(store: store)
                }

                if store.recoveryRequired {
                    HStack(alignment: .top, spacing: 10) {
                        Image(systemName: "exclamationmark.triangle.fill")
                            .foregroundStyle(.orange)
                            .accessibilityHidden(true)
                        VStack(alignment: .leading, spacing: 2) {
                            Text("网络恢复提示")
                                .font(.subheadline.weight(.semibold))
                            Text("网络进程尚未完全退出，已暂停重连。请导出诊断，重启 macOS 后再试。")
                                .font(.callout)
                                .foregroundStyle(.secondary)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                    }
                    .padding(12)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Color.orange.opacity(0.08), in: RoundedRectangle(cornerRadius: AppTheme.itemRadius))
                    .overlay(
                        RoundedRectangle(cornerRadius: AppTheme.itemRadius)
                            .strokeBorder(Color.orange.opacity(0.3), lineWidth: 1)
                    )
                    .accessibilityElement(children: .combine)
                }

                SummaryColumns(spacing: groupSpacing) {
                    TrafficUsageCard(store: store)
                    ApplicationRoutingCard(store: store)
                }
                .environment(\.surfaceFillsHeight, true)
            }
            .padding(AppTheme.pagePadding)
            .frame(maxWidth: AppTheme.contentWidth, alignment: .topLeading)
            .frame(maxWidth: .infinity, alignment: .topLeading)
        }
        .background { ConnectionBackdrop().ignoresSafeArea(.container, edges: .top) }
    }
}

/// Keeps the traffic and routing summaries side by side until either column would fall
/// below a readable width, then stacks them. A single visible card spans the whole row.
private struct SummaryColumns: Layout {
    var spacing: CGFloat
    var leadingShare: CGFloat = 0.53
    var minimumColumnWidth: CGFloat = 280

    // A card that renders nothing takes no column.
    private func visible(_ subviews: Subviews) -> [LayoutSubview] {
        subviews.filter { $0.sizeThatFits(.unspecified).height > 0 }
    }

    private func columns(for width: CGFloat, count: Int) -> (leading: CGFloat, trailing: CGFloat)? {
        guard count == 2 else { return nil }
        let available = width - spacing
        let leading = (available * leadingShare).rounded()
        let trailing = available - leading
        guard min(leading, trailing) >= minimumColumnWidth else { return nil }
        return (leading: leading, trailing: trailing)
    }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let items = visible(subviews)
        let fallback = minimumColumnWidth * 2 + spacing
        let width = proposal.width.flatMap { $0.isFinite ? $0 : nil } ?? fallback
        if let split = columns(for: width, count: items.count) {
            let height = max(
                items[0].sizeThatFits(ProposedViewSize(width: split.leading, height: nil)).height,
                items[1].sizeThatFits(ProposedViewSize(width: split.trailing, height: nil)).height
            )
            return CGSize(width: width, height: height)
        }
        let heights = items.map { $0.sizeThatFits(ProposedViewSize(width: width, height: nil)).height }
        return CGSize(width: width, height: heights.reduce(0, +) + spacing * CGFloat(max(0, heights.count - 1)))
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let items = visible(subviews)
        if let split = columns(for: bounds.width, count: items.count) {
            items[0].place(at: bounds.origin, anchor: .topLeading,
                           proposal: ProposedViewSize(width: split.leading, height: bounds.height))
            items[1].place(at: CGPoint(x: bounds.minX + split.leading + spacing, y: bounds.minY), anchor: .topLeading,
                           proposal: ProposedViewSize(width: split.trailing, height: bounds.height))
            return
        }
        var y = bounds.minY
        for item in items {
            let height = item.sizeThatFits(ProposedViewSize(width: bounds.width, height: nil)).height
            item.place(at: CGPoint(x: bounds.minX, y: y), anchor: .topLeading,
                       proposal: ProposedViewSize(width: bounds.width, height: height))
            y += height + spacing
        }
    }
}

private struct TestWorkspaceCard: View {
    let store: AppStore
    var body: some View {
        GroupBox {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Label("双 VPN 功能测试版", systemImage: "testtube.2").font(.headline)
                    Spacer()
                    Button("复制正式版线路…") { store.copyProductionProfilesForTesting() }
                        .disabled(store.busy || store.company.busy || store.company.isRunning || store.companyDraftOpen || store.profileDraft != nil)
                }
                Text("独立配置、钥匙串和测试网络服务。复制线路会包含订阅节点凭据；VPN 密码需重新输入。").font(.callout).foregroundStyle(.secondary)
                if let reason = store.networkService.testHostMessage {
                    Label(reason, systemImage: "pause.circle").font(.callout).foregroundStyle(.orange)
                } else {
                    Text("请在单独验收窗口连接；测试服务需单独授权。完成后先断开测试版，再恢复正式版连接。").font(.caption).foregroundStyle(.secondary)
                }
                Button("重新检测正式版状态") { store.networkService.refresh() }.font(.caption)
            }.padding(8).frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

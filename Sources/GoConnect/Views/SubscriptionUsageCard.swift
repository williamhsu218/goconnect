import SwiftUI
import GoConnectCore

struct SubscriptionUsageCard: View {
    @Bindable var store: AppStore
    let subscription: Subscription

    private var title: some View {
        Label("套餐剩余流量", systemImage: "chart.pie").font(.headline)
    }

    private var actions: some View {
        HStack(spacing: 8) {
            if store.subscriptionWorking { ProgressView().controlSize(.small) }
            Button("刷新流量") { store.updateSubscription(subscription) }.appActionStyle()
                .disabled(store.subscriptionWorking || store.yunyooUsage.relayPublishing)
            YunyooRelayControls(store: store.yunyooUsage, subscription: subscription) { store.updateSubscription(subscription) }
                .disabled(store.subscriptionWorking)
        }
    }

    private func amount(_ bytes: Int64) -> String {
        YunyooTrafficSnapshot.formatGB(Double(bytes) / 1_073_741_824)
    }

    private func remainingAmount(_ remaining: Int64) -> some View {
        MetricValueText(value: amount(remaining))
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("套餐剩余 \(amount(remaining))")
    }

    private func usageSummary(used: Int64, total: Int64) -> some View {
        Text("已用 \(amount(used)) / 总量 \(amount(total))")
            .font(.callout).monospacedDigit().foregroundStyle(.secondary)
            .fixedSize(horizontal: false, vertical: true)
    }

    var body: some View {
        let values = subscription.quotaValues
        if let total = values["total"], total > 0 {
            let up = max(0,values["upload"] ?? 0), down = max(0,values["download"] ?? 0)
            let used = up > Int64.max - down ? Int64.max : up + down
            let remaining = used >= total ? 0 : total - used
            Surface {
                VStack(alignment: .leading, spacing: 10) {
                    ViewThatFits(in: .horizontal) {
                        HStack(spacing: 8) {
                            title
                            Spacer(minLength: 8)
                            actions
                        }
                        VStack(alignment: .leading, spacing: 8) {
                            title
                            actions
                        }
                    }
                    ViewThatFits(in: .horizontal) {
                        HStack(alignment: .firstTextBaseline, spacing: 12) {
                            remainingAmount(remaining)
                            Spacer(minLength: 8)
                            usageSummary(used: used, total: total)
                        }
                        VStack(alignment: .leading, spacing: 4) {
                            remainingAmount(remaining)
                            usageSummary(used: used, total: total)
                        }
                    }
                    ProgressView(value: min(1,Double(used) / Double(total)))
                        .accessibilityLabel("套餐已用比例")
                    if let updated = values["updated"] {
                        Text("上次更新于 \(Date(timeIntervalSince1970:Double(updated)).formatted(date:.abbreviated,time:.standard)) · 本组节点共享套餐额度 · 手动刷新")
                            .font(.caption).monospacedDigit().foregroundStyle(.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    if values["auth_required"] == 1 {
                        InlineNotice(text: "授权已过期，重新授权即可获取最新用量；当前保留上次数据。")
                    } else if values["stale"] == 1 {
                        InlineNotice(text: "上次刷新未成功，当前保留上次数据，可手动重试。")
                    }
                }
            }
        }
    }
}

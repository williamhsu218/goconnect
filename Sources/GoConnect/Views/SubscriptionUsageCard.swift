import SwiftUI
import GoConnectCore

struct SubscriptionUsageCard: View {
    @Bindable var store: AppStore
    let subscription: Subscription

    var body: some View {
        let values = subscription.quotaValues
        if let total = values["total"], total > 0 {
            let up = max(0,values["upload"] ?? 0), down = max(0,values["download"] ?? 0)
            let used = up > Int64.max - down ? Int64.max : up + down
            let remaining = used >= total ? 0 : total - used
            Surface {
                VStack(alignment: .leading, spacing: 12) {
                    HStack {
                        Label("套餐剩余流量", systemImage: "chart.pie").font(.headline)
                        Spacer()
                        if store.subscriptionWorking { ProgressView().controlSize(.small) }
                        Button("刷新流量") { store.updateSubscription(subscription) }.appActionStyle()
                            .disabled(store.subscriptionWorking || store.yunyooUsage.relayPublishing)
                        YunyooRelayControls(store: store.yunyooUsage, subscription: subscription) { store.updateSubscription(subscription) }
                            .disabled(store.subscriptionWorking)
                    }
                    HStack(alignment: .firstTextBaseline) {
                        Text(YunyooTrafficSnapshot.formatGB(Double(remaining) / 1_073_741_824))
                            .font(.system(size: 26, weight: .semibold, design: .rounded)).monospacedDigit()
                        Spacer()
                        Text("已用 \(YunyooTrafficSnapshot.formatGB(Double(used) / 1_073_741_824)) / 总量 \(YunyooTrafficSnapshot.formatGB(Double(total) / 1_073_741_824))")
                            .font(.callout).foregroundStyle(.secondary)
                    }
                    ProgressView(value: min(1,Double(used) / Double(total)))
                    if let updated = values["updated"] {
                        Text("上次更新于 \(Date(timeIntervalSince1970:Double(updated)).formatted(date:.abbreviated,time:.standard)) · 本组节点共享套餐额度 · 手动刷新")
                            .font(.caption).foregroundStyle(.secondary)
                        if values["auth_required"] == 1 {
                            Text("授权已过期，重新授权即可获取最新用量；当前保留上次数据。").font(.caption).foregroundStyle(.orange)
                        } else if values["stale"] == 1 {
                            Text("上次刷新未成功，当前保留上次数据，可手动重试。").font(.caption).foregroundStyle(.orange)
                        }
                    }
                }
            }
        }
    }
}

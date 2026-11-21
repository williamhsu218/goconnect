import SwiftUI
import GoConnectCore

extension TrafficTotals {
    /// A saturated counter only proves a lower bound.
    func amountText(_ bytes: UInt64) -> String {
        (overflowed ? "≥ " : "") + NetworkFormatters.formatBytes(bytes)
    }
}

/// The active line's locally recorded traffic. No provider figures appear here.
struct TrafficUsageCard: View {
    @Bindable var store: AppStore

    var body: some View {
        let history = store.trafficHistory
        let profileID = store.configuration.activeProfileID
        let _ = history.revision
        VStack(alignment: .leading, spacing: 7) {
            TimelineView(.everyMinute) { context in
                ConnectionSummaryLink(title: "本机流量", symbol: "chart.bar.xaxis",
                                      detail: summary(for: profileID, at: context.date)) {
                    store.showTraffic(for: profileID)
                }
                .help("查看这条线路在本机的流量统计")
            }
            if let error = history.storageError { InlineNotice(text: error) }
        }
    }

    private func summary(for profileID: UUID, at date: Date) -> String {
        let history = store.trafficHistory
        guard history.readable else { return "无法读取" }
        let today = history.totals(profileID: profileID, interval: history.interval(granularity: .hourly, anchor: date))
        let month = history.totals(profileID: profileID, interval: history.interval(granularity: .daily, anchor: date))
        guard today.hasData || month.hasData else { return "暂无记录" }
        let dayAmount = today.hasData ? today.amountText(today.total) : "—"
        let monthAmount = month.hasData ? month.amountText(month.total) : "—"
        return "今日 \(dayAmount) · 本月 \(monthAmount)"
    }
}

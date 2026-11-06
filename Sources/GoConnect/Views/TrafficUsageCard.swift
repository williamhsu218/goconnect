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
        Surface {
            VStack(alignment: .leading, spacing: 10) {
                SectionHeader("当前线路流量", symbol: "chart.bar.xaxis") {
                    Button("查看统计") { store.showTraffic(for: profileID) }.appInlineActionStyle()
                }
                // The tick rolls 今日 and 本月 over while no new samples arrive.
                TimelineView(.everyMinute) { context in
                    HStack(alignment: .top, spacing: 12) {
                        usage("今日消耗", period: .hourly, profileID: profileID, at: context.date)
                        usage("本月消耗", period: .daily, profileID: profileID, at: context.date)
                    }
                }
                if let error = history.storageError { InlineNotice(text: error) }
                Text("仅统计这台 Mac 上 GoConnect 的本地记录，不代表服务商计费。")
                    .font(.caption).foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    /// An hourly series spans its anchor day, and a daily series its anchor month.
    private func usage(_ title: String, period: TrafficGranularity, profileID: UUID, at date: Date) -> some View {
        let history = store.trafficHistory
        let totals = history.totals(profileID: profileID, interval: history.interval(granularity: period, anchor: date))
        let placeholder = history.readable ? "暂无记录" : "无法读取"
        return VStack(alignment: .leading, spacing: 4) {
            Text(title).font(.callout).foregroundStyle(.secondary)
            MetricValueText(value: totals.hasData ? totals.amountText(totals.total) : "—", size: 24)
            Group {
                if totals.hasData {
                    Text("下载 \(totals.amountText(totals.received))")
                    Text("上传 \(totals.amountText(totals.sent))")
                } else {
                    Text(placeholder)
                }
            }
            .font(.caption).monospacedDigit().foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(title)
        .accessibilityValue(totals.hasData
            ? "合计 \(totals.amountText(totals.total))，下载 \(totals.amountText(totals.received))，上传 \(totals.amountText(totals.sent))"
            : placeholder)
    }
}

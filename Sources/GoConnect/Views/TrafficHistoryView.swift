import Charts
import SwiftUI
import GoConnectCore

struct TrafficHistoryView: View {
    @Bindable var store: AppStore
    @State private var granularity: TrafficGranularity = .daily
    @State private var anchor = Date()

    private var history: ProfileTrafficStore { store.trafficHistory }

    /// Falls back to the active line when an ID has neither a saved line nor recorded
    /// history. Browsing here never writes to the connection selection.
    private var selectedProfileID: UUID {
        if let id = store.trafficProfileID,
           store.configuration.connections.contains(where: { $0.id == id }) || history.profiles.contains(where: { $0.id == id }) {
            return id
        }
        return store.configuration.activeProfileID
    }

    private var archivedProfiles: [TrafficProfile] {
        let saved = Set(store.configuration.connections.map(\.id))
        return history.profiles.filter { !saved.contains($0.id) }.sorted { $0.lastUpdatedAt > $1.lastUpdatedAt }
    }

    private struct PeriodStep {
        let component: Calendar.Component
        let previous: String
        let next: String
        let current: String
    }

    private var periodStep: PeriodStep? {
        switch granularity {
        case .hourly: return PeriodStep(component: .day, previous: "前一天", next: "后一天", current: "今天")
        case .daily: return PeriodStep(component: .month, previous: "上个月", next: "下个月", current: "本月")
        case .monthly: return PeriodStep(component: .year, previous: "上一年", next: "下一年", current: "今年")
        case .yearly: return nil
        }
    }

    var body: some View {
        let now = Date()
        let profileID = selectedProfileID
        let hasHistory = history.totals(profileID: profileID).hasData
        ScrollView {
            VStack(alignment: .leading, spacing: AppTheme.sectionSpacing) {
                PageHeading(title: "流量统计", subtitle: "按线路查看这台 Mac 上的本地流量记录。")
                VStack(alignment: .leading, spacing: 10) {
                    selectionControls(profileID: profileID)
                    archivedNote(profileID: profileID)
                    HStack(spacing: 8) {
                        if hasHistory { periodNavigator(profileID: profileID, now: now) }
                        Spacer(minLength: 12)
                        Text(timeZoneText(at: now))
                            .font(.caption).monospacedDigit().foregroundStyle(.secondary)
                            .lineLimit(1).truncationMode(.middle)
                    }
                }
                if let error = history.storageError { InlineNotice(text: error) }
                if hasHistory {
                    periodContent(profileID: profileID, now: now)
                } else {
                    emptyState
                }
                Text("自开始使用此版本时记录，数据来自这台 Mac 上 GoConnect 的计数。不包含其他设备，也不代表服务商计费；协议开销未精确计入。")
                    .font(.caption).foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .padding(AppTheme.pagePadding)
            .frame(maxWidth: AppTheme.contentWidth, alignment: .topLeading)
            .frame(maxWidth: .infinity, alignment: .topLeading)
        }
    }

    private func selectionControls(profileID: UUID) -> some View {
        HStack(spacing: 12) {
            Picker("线路", selection: Binding(get: { profileID }, set: { store.trafficProfileID = $0 })) {
                ForEach(store.configuration.connections) { connection in
                    Text(connection.id == store.configuration.activeProfileID ? "\(connection.displayName)（当前）" : connection.displayName)
                        .tag(connection.id)
                }
                if !archivedProfiles.isEmpty {
                    Section("已删除线路") {
                        ForEach(archivedProfiles) { profile in
                            Text("\(profile.name)（已删除）").tag(profile.id)
                        }
                    }
                }
            }
            .pickerStyle(.menu)
            .frame(maxWidth: 300, alignment: .leading)
            Spacer(minLength: 12)
            Picker("统计粒度", selection: $granularity) {
                ForEach(TrafficGranularity.allCases) { item in
                    Text(item.title).tag(item)
                }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .fixedSize()
        }
    }

    @ViewBuilder
    private func archivedNote(profileID: UUID) -> some View {
        if let profile = archivedProfiles.first(where: { $0.id == profileID }) {
            Label("这条线路已删除，本地记录仍保留。首次记录：\(formatter("yyyy 年 M 月 d 日").string(from: profile.firstRecordedAt))", systemImage: "archivebox")
                .font(.caption).monospacedDigit().foregroundStyle(.secondary)
        }
    }

    @ViewBuilder
    private func periodNavigator(profileID: UUID, now: Date) -> some View {
        if let step = periodStep {
            let calendar = history.calendar
            let isCurrent = calendar.isDate(anchor, equalTo: now, toGranularity: step.component)
            let firstRecorded = history.profiles.first(where: { $0.id == profileID })?.firstRecordedAt
            let canGoBack = firstRecorded.map { anchor > $0 && !calendar.isDate(anchor, equalTo: $0, toGranularity: step.component) } ?? false
            Button { move(by: -1, component: step.component, now: now) } label: { Image(systemName: "chevron.left") }
                .appActionStyle().disabled(!canGoBack)
                .accessibilityLabel(step.previous).help(step.previous)
            if granularity == .hourly {
                DatePicker("统计日期", selection: $anchor, in: ...now, displayedComponents: .date)
                    .labelsHidden()
                    .datePickerStyle(.compact)
                    .environment(\.calendar, calendar)
                    .environment(\.timeZone, history.timeZone)
                    .fixedSize()
            } else {
                Text(formatter(granularity == .daily ? "yyyy 年 M 月" : "yyyy 年").string(from: anchor))
                    .font(.callout.weight(.medium)).monospacedDigit()
                    .lineLimit(1).fixedSize()
            }
            Button { move(by: 1, component: step.component, now: now) } label: { Image(systemName: "chevron.right") }
                .appActionStyle().disabled(isCurrent)
                .accessibilityLabel(step.next).help(step.next)
            Button(step.current) { anchor = now }
                .appActionStyle().disabled(isCurrent)
        } else {
            Text("全部年份").font(.callout.weight(.medium))
        }
    }

    private func move(by value: Int, component: Calendar.Component, now: Date) {
        guard let target = history.calendar.date(byAdding: component, value: value, to: anchor) else { return }
        anchor = min(target, now)
    }

    @ViewBuilder
    private func periodContent(profileID: UUID, now: Date) -> some View {
        let periodAnchor = granularity == .yearly ? now : anchor
        let totals = history.totals(profileID: profileID, interval: history.interval(granularity: granularity, anchor: periodAnchor, profileID: profileID))
        let buckets = makeBuckets(profileID: profileID, anchor: periodAnchor, now: now)
        Surface {
            VStack(alignment: .leading, spacing: 14) {
                HStack(alignment: .top, spacing: 16) {
                    periodAmount("下载", totals: totals, bytes: totals.received)
                    periodAmount("上传", totals: totals, bytes: totals.sent)
                    periodAmount("合计", totals: totals, bytes: totals.total)
                }
                if totals.overflowed { InlineNotice(text: "部分计数已达上限，实际流量不低于显示值。") }
                Divider()
                if totals.hasData {
                    TrafficChartLegend()
                    TrafficChart(buckets: buckets, granularity: granularity)
                } else {
                    Text("这段时间暂无记录，不代表流量为 0。")
                        .font(.callout).foregroundStyle(.secondary)
                }
            }
        }
        if totals.hasData {
            TrafficBucketTable(buckets: buckets.filter { $0.elapsed || $0.totals.hasData })
        }
    }

    private func periodAmount(_ title: String, totals: TrafficTotals, bytes: UInt64) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title).font(.callout).foregroundStyle(.secondary)
            MetricValueText(value: totals.hasData ? totals.amountText(bytes) : "—")
            if !totals.hasData {
                Text("未记录").font(.caption).foregroundStyle(.secondary)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(title)
        .accessibilityValue(totals.hasData ? totals.amountText(bytes) : "未记录")
    }

    @ViewBuilder
    private var emptyState: some View {
        Surface {
            Group {
                if history.readable {
                    ContentUnavailableView("暂无记录", systemImage: "chart.bar.xaxis",
                        description: Text("这条线路还没有本地流量记录，连接后开始统计。"))
                } else {
                    ContentUnavailableView("暂时无法显示记录", systemImage: "exclamationmark.triangle",
                        description: Text("读取恢复前，不会把未知流量显示为 0。"))
                }
            }
            .frame(maxWidth: .infinity, minHeight: 240)
        }
    }

    private func makeBuckets(profileID: UUID, anchor: Date, now: Date) -> [TrafficBucket] {
        let axis: DateFormatter, row: DateFormatter
        switch granularity {
        case .hourly: axis = formatter("HH:mm"); row = axis
        case .daily: axis = formatter("d"); row = formatter("M 月 d 日 EEE")
        case .monthly: axis = formatter("M 月"); row = formatter("yyyy 年 M 月")
        case .yearly: axis = formatter("yyyy"); row = formatter("yyyy 年")
        }
        // The clock repeats an hour when daylight saving ends; each one keeps its own bar.
        var seen: [String: Int] = [:]
        return history.series(profileID: profileID, granularity: granularity, anchor: anchor).map { point in
            let axisText = axis.string(from: point.start), rowText = row.string(from: point.start)
            let occurrence = (seen[axisText] ?? 0) + 1
            seen[axisText] = occurrence
            let repeated = occurrence > 1 ? "（第 \(occurrence) 次）" : ""
            return TrafficBucket(id: point.start, axisLabel: axisText + repeated, rowLabel: rowText + repeated,
                                 totals: point.totals, elapsed: point.start <= now)
        }
    }

    private func formatter(_ pattern: String) -> DateFormatter {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "zh_Hans")
        formatter.calendar = history.calendar
        formatter.timeZone = history.timeZone
        formatter.dateFormat = pattern
        return formatter
    }

    private func timeZoneText(at date: Date) -> String {
        let seconds = history.timeZone.secondsFromGMT(for: date)
        let minutes = abs(seconds) / 60
        let offset = (seconds < 0 ? "−" : "+") + String(format: "%02d:%02d", minutes / 60, minutes % 60)
        return "统计时区 \(history.timeZone.identifier)（UTC\(offset)）"
    }
}

private struct TrafficBucket: Identifiable {
    let id: Date
    let axisLabel: String
    let rowLabel: String
    let totals: TrafficTotals
    let elapsed: Bool
}

private enum TrafficChartStyle {
    static let download = Color.blue
    static let upload = Color.teal
    static let unrecorded = Color.secondary.opacity(0.14)
}

private struct TrafficChartLegend: View {
    var body: some View {
        HStack(spacing: 14) {
            item("下载", color: TrafficChartStyle.download)
            item("上传", color: TrafficChartStyle.upload)
            item("未记录", color: TrafficChartStyle.unrecorded)
        }
        .font(.caption).foregroundStyle(.secondary)
        .accessibilityElement(children: .combine)
    }

    private func item(_ title: String, color: Color) -> some View {
        HStack(spacing: 5) {
            RoundedRectangle(cornerRadius: 2)
                .fill(color)
                .overlay(RoundedRectangle(cornerRadius: 2).strokeBorder(AppTheme.subtleBorder))
                .frame(width: 10, height: 10)
                .accessibilityHidden(true)
            Text(title)
        }
    }
}

/// Bars use evenly spaced text categories, so every label follows the statistics time
/// zone and a 23- or 25-hour day keeps one slot per bucket. A faint band marks elapsed
/// buckets with no record; a measured zero leaves its slot empty.
private struct TrafficChart: View {
    let buckets: [TrafficBucket]
    let granularity: TrafficGranularity

    private let barWidth = MarkDimension.ratio(0.62)

    private var largestBytes: Double {
        buckets.map { Double($0.totals.received) + Double($0.totals.sent) }.max() ?? 0
    }

    /// Plots in one readable unit so the automatic ticks land on round numbers.
    private var scale: (divisor: Double, unit: String) {
        let units = ["B", "KB", "MB", "GB", "TB"]
        var divisor = 1.0, index = 0
        while largestBytes / divisor >= 1024, index < units.count - 1 {
            divisor *= 1024; index += 1
        }
        return (divisor, units[index])
    }

    private var domainTop: Double { max(largestBytes / scale.divisor * 1.08, 1) }

    private var axisLabels: [String] {
        let every: Int
        switch granularity {
        case .hourly: every = 3
        case .daily: every = 5
        case .monthly: every = 1
        case .yearly: every = max(1, buckets.count / 8)
        }
        return buckets.enumerated().filter { $0.offset % every == 0 }.map(\.element.axisLabel)
    }

    var body: some View {
        let unit = scale.unit
        let labeled = Set(axisLabels)
        Chart {
            ForEach(buckets) { bucket in
                marks(for: bucket)
            }
        }
        .chartForegroundStyleScale(["下载": TrafficChartStyle.download, "上传": TrafficChartStyle.upload])
        .chartLegend(.hidden)
        .chartXScale(domain: buckets.map(\.axisLabel))
        .chartYScale(domain: 0...domainTop)
        .chartXAxis {
            // Every slot gets a mark and only the chosen ones draw, so thinning does not rest
            // on how a category axis treats a value subset. Text keeps its full width
            // rather than being capped to the width of one slot.
            AxisMarks(values: buckets.map(\.axisLabel)) { value in
                if let label = value.as(String.self), labeled.contains(label) {
                    AxisTick(centered: false)
                    AxisValueLabel(centered: false, collisionResolution: .disabled, orientation: .horizontal) {
                        Text(label).monospacedDigit().fixedSize()
                    }
                }
            }
        }
        .chartYAxis {
            AxisMarks(position: .leading) { value in
                AxisGridLine()
                AxisValueLabel {
                    if let amount = value.as(Double.self) {
                        Text("\(amount.formatted(.number.precision(.fractionLength(0...1)))) \(unit)")
                            .monospacedDigit()
                    }
                }
            }
        }
        .frame(height: 220)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("下载与上传流量堆叠柱状图")
        .accessibilityValue("\(buckets.filter(\.totals.hasData).count) 个时段有记录，单位 \(unit)，具体数值见下方明细")
    }

    @ChartContentBuilder
    private func marks(for bucket: TrafficBucket) -> some ChartContent {
        if bucket.totals.hasData {
            BarMark(x: .value("时段", bucket.axisLabel),
                    y: .value("流量", Double(bucket.totals.received) / scale.divisor),
                    width: barWidth)
                .foregroundStyle(by: .value("方向", "下载"))
            BarMark(x: .value("时段", bucket.axisLabel),
                    y: .value("流量", Double(bucket.totals.sent) / scale.divisor),
                    width: barWidth)
                .foregroundStyle(by: .value("方向", "上传"))
        } else if bucket.elapsed {
            RectangleMark(x: .value("时段", bucket.axisLabel),
                          yStart: .value("流量", 0.0),
                          yEnd: .value("流量", domainTop),
                          width: barWidth)
                .foregroundStyle(TrafficChartStyle.unrecorded)
        }
    }
}

private struct TrafficBucketTable: View {
    let buckets: [TrafficBucket]

    var body: some View {
        Surface {
            VStack(alignment: .leading, spacing: 0) {
                HStack(spacing: 8) {
                    Text("时段").frame(maxWidth: .infinity, alignment: .leading)
                    amountCell("下载")
                    amountCell("上传")
                    amountCell("合计")
                }
                .font(.caption.weight(.medium)).foregroundStyle(.secondary)
                .padding(.bottom, 8)
                .accessibilityHidden(true)
                ForEach(buckets) { bucket in
                    Divider()
                    row(for: bucket)
                }
            }
        }
    }

    private func row(for bucket: TrafficBucket) -> some View {
        let totals = bucket.totals
        return HStack(spacing: 8) {
            Text(bucket.rowLabel).frame(maxWidth: .infinity, alignment: .leading)
            if totals.hasData {
                amountCell(totals.amountText(totals.received))
                amountCell(totals.amountText(totals.sent))
                amountCell(totals.amountText(totals.total)).fontWeight(.medium)
            } else {
                amountCell("未记录")
            }
        }
        .font(.callout).monospacedDigit()
        .foregroundStyle(totals.hasData ? .primary : .secondary)
        .padding(.vertical, 6)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(bucket.rowLabel)
        .accessibilityValue(totals.hasData
            ? "下载 \(totals.amountText(totals.received))，上传 \(totals.amountText(totals.sent))，合计 \(totals.amountText(totals.total))"
            : "未记录")
    }

    private func amountCell(_ text: String) -> some View {
        Text(text)
            .lineLimit(1)
            .minimumScaleFactor(0.8)
            .frame(width: 104, alignment: .trailing)
    }
}

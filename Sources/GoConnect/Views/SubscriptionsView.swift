import SwiftUI
import GoConnectCore

enum RegionFlagHelper {
    private static let rules: [(flag: String, keywords: [String])] = [
        ("🇭🇰", ["香港", "HK", "HONG KONG", "HONGKONG", "HKG"]),
        ("🇯🇵", ["日本", "JP", "JAPAN", "TOKYO", "东京", "東京", "OSAKA", "大阪"]),
        ("🇺🇸", ["美国", "美國", "US", "USA", "AMERICA", "UNITED STATES", "洛杉矶", "硅谷", "西雅图", "纽约", "波特兰", "芝加哥", "SAN JOSE", "LOS ANGELES", "SEATTLE", "NEW YORK"]),
        ("🇸🇬", ["新加坡", "SG", "SINGAPORE", "狮城", "獅城"]),
        ("🇹🇼", ["台湾", "台灣", "TW", "TAIWAN", "台北", "TAIPEI", "台中", "新北"]),
        ("🇬🇧", ["英国", "英國", "UK", "BRITAIN", "LONDON", "伦敦", "倫敦", "ENGLAND", "GREAT BRITAIN"]),
        ("🇩🇪", ["德国", "德國", "DE", "GERMANY", "FRANKFURT", "法兰克福", "BERLIN", "柏林"]),
        ("🇰🇷", ["韩国", "韓國", "KR", "KOREA", "SEOUL", "首尔", "首爾"]),
        ("🇨🇦", ["加拿大", "CA", "CANADA", "TORONTO", "多伦多", "VANCOUVER", "温哥华"]),
        ("🇦🇺", ["澳大利亚", "澳洲", "AU", "AUSTRALIA", "SYDNEY", "悉尼", "MELBOURNE", "墨尔本"]),
        ("🇫🇷", ["法国", "法國", "FR", "FRANCE", "PARIS", "巴黎"]),
        ("🇳🇱", ["荷兰", "荷蘭", "NL", "NETHERLANDS", "AMSTERDAM", "阿姆斯特丹"]),
        ("🇷🇺", ["俄罗斯", "俄羅斯", "RU", "RUSSIA", "MOSCOW", "莫斯科"]),
        ("🇮🇳", ["印度", "IN", "INDIA", "MUMBAI", "孟买"]),
        ("🇲🇾", ["马来西亚", "馬來西亞", "MY", "MALAYSIA", "KUALA LUMPUR", "吉隆坡"]),
        ("🇹🇭", ["泰国", "泰國", "TH", "THAILAND", "BANGKOK", "曼谷"]),
        ("🇻🇳", ["越南", "VN", "VIETNAM", "HO CHI MINH", "胡志明"]),
        ("🇵🇭", ["菲律宾", "菲律賓", "PH", "PHILIPPINES", "MANILA", "马尼拉"]),
        ("🇹🇷", ["土耳其", "TR", "TURKEY", "ISTANBUL", "伊斯坦布尔"]),
        ("🇦🇷", ["阿根廷", "AR", "ARGENTINA"]),
        ("🇧🇷", ["巴西", "BR", "BRAZIL"]),
        ("🇨🇭", ["瑞士", "CH", "SWITZERLAND"]),
        ("🇸🇪", ["瑞典", "SE", "SWEDEN"]),
        ("🇮🇹", ["意大利", "IT", "ITALY"]),
        ("🇪🇸", ["西班牙", "ES", "SPAIN"]),
        ("🇮🇪", ["爱尔兰", "愛爾蘭", "IE", "IRELAND"]),
        ("🇦🇪", ["阿联酋", "迪拜", "DUBAI", "UAE"])
    ]

    static func detectFlag(in text: String) -> String? {
        // 1. Check if the string already contains a regional indicator emoji flag
        for char in text {
            let scalars = char.unicodeScalars
            if scalars.count >= 2 && scalars.allSatisfy({ (0x1F1E6...0x1F1FF).contains($0.value) }) {
                return String(char)
            }
        }

        // 2. Keyword match
        let upper = text.uppercased()
        for rule in rules {
            for keyword in rule.keywords {
                if keyword.count == 2 && keyword.allSatisfy(\.isASCII) {
                    if containsWord(upper, word: keyword) {
                        return rule.flag
                    }
                } else {
                    if upper.contains(keyword) {
                        return rule.flag
                    }
                }
            }
        }
        return nil
    }

    private static func containsWord(_ text: String, word: String) -> Bool {
        var searchRange = text.startIndex..<text.endIndex
        while let matchRange = text.range(of: word, range: searchRange) {
            let beforeIndex = matchRange.lowerBound
            let afterIndex = matchRange.upperBound

            let validBefore = beforeIndex == text.startIndex || !text[text.index(before: beforeIndex)].isLetter
            let validAfter = afterIndex == text.endIndex || !text[afterIndex].isLetter

            if validBefore && validAfter {
                return true
            }
            searchRange = afterIndex..<text.endIndex
        }
        return false
    }
}

struct NodeFlagIcon: View {
    let flag: String?

    var body: some View {
        ZStack {
            RoundedRectangle(cornerRadius: AppTheme.itemRadius, style: .continuous)
                .fill(flag != nil ? Color(nsColor: .controlBackgroundColor) : AppTheme.accent.opacity(0.09))
                .overlay(
                    RoundedRectangle(cornerRadius: AppTheme.itemRadius, style: .continuous)
                        .strokeBorder(AppTheme.subtleBorder, lineWidth: 1)
                )
            if let flag {
                Text(flag).font(.system(size: 20))
            } else {
                Image(systemName: "network")
                    .font(.system(size: 16, weight: .medium))
                    .foregroundStyle(AppTheme.accent)
            }
        }
        .frame(width: 36, height: 36)
    }
}

struct SubscriptionsView: View {
    @Bindable var store: AppStore
    @State private var selected: UUID?
    @State private var search = ""
    @State private var adding = false
    @State private var name = ""
    @State private var address = ""
    @State private var deleting: Subscription?
    @State private var sortByLatency = false
    private var detail: Subscription? { store.subscriptions.first { $0.id == selected } }

    private func visibleNodes(in item: Subscription) -> [SubscriptionNode] {
        let nodes = item.payload.nodes.filter { search.isEmpty || $0.name.localizedCaseInsensitiveContains(search) || $0.type.localizedCaseInsensitiveContains(search) }
        guard sortByLatency else { return nodes }
        return nodes.enumerated().sorted { a, b in
            let left = store.nodeLatency.result(a.element, in: item.id)?.milliseconds ?? Int.max
            let right = store.nodeLatency.result(b.element, in: item.id)?.milliseconds ?? Int.max
            return left == right ? a.offset < b.offset : left < right
        }.map(\.element)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: AppTheme.sectionSpacing) {
            if let item = detail {
                Button("全部订阅", systemImage: "chevron.left") { selected = nil; search = "" }
                    .buttonStyle(.plain)
                    .foregroundStyle(.secondary)

                PageHeader(title: item.payload.name, subtitle: "\(item.payload.nodes.count) 个节点 · 选择后在线路首页连接") {
                    Button("更新订阅", systemImage: "arrow.clockwise") { store.updateSubscription(item) }
                        .appActionStyle()
                        .fixedSize()
                        .disabled(store.subscriptionWorking)
                }

                if let quota = item.quotaText {
                    HStack(spacing: 8) {
                        Image(systemName: "chart.bar.doc.horizontal")
                            .foregroundStyle(AppTheme.accent)
                        Text(quota)
                            .font(.callout)
                            .foregroundStyle(.secondary)
                    }
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
                    .background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                }

                if let count = item.payload.unsupportedCount, count > 0 {
                    Label("另有 \(count) 个节点的协议或传输暂不支持，未导入。", systemImage: "info.circle")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                TextField("搜索节点名称或协议", text: $search)
                    .textFieldStyle(.roundedBorder)
                    .controlSize(.large)

                HStack(spacing: 12) {
                    if store.nodeLatency.isRunning {
                        ProgressView().controlSize(.small)
                        Text("测试中 \(store.nodeLatency.completed) / \(store.nodeLatency.total)")
                            .font(.callout)
                            .monospacedDigit()
                        Button("停止测试") { store.nodeLatency.stop() }
                            .appActionStyle()
                            .controlSize(.small)
                    } else {
                        Button("测试全部", systemImage: "bolt.horizontal.circle") {
                            store.testNodes(item.payload.nodes, in: item)
                        }
                        .appActionStyle()
                        .disabled(!store.canTestNodes)
                    }
                    Spacer()
                    Toggle("按延迟排序", isOn: $sortByLatency)
                        .toggleStyle(.checkbox)
                        .font(.callout)
                }

                Text("HTTPS 响应延迟 · gstatic.com · 单次请求最长 8 秒；结果仅代表测试时的网络状况。")
                    .font(.caption)
                    .foregroundStyle(.secondary)

                if store.busy && store.usesCompanyRoutes {
                    Label("公司内网已开启，请结束本次连接后再测试节点。", systemImage: "info.circle")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                if let error = store.nodeLatency.storageError {
                    Text(error).font(.caption).foregroundStyle(.orange)
                }

                ScrollView {
                    LazyVStack(spacing: 0) {
                        let nodes = visibleNodes(in: item)
                        ForEach(nodes) { node in
                            let isSelected = store.configuration.activeConnection.subscription == SubscriptionSelection(subscriptionID: item.id, nodeName: node.name)
                            let flag = RegionFlagHelper.detectFlag(in: node.name)

                            HStack(spacing: 12) {
                                NodeFlagIcon(flag: flag)

                                VStack(alignment: .leading, spacing: 4) {
                                    HStack(spacing: 6) {
                                        Text(node.name)
                                            .font(.body.weight(.medium))
                                            .lineLimit(2)
                                            .textSelection(.enabled)
                                        if isSelected {
                                            StatusPill(title: "当前选用", symbol: "checkmark.circle.fill", color: AppTheme.accent)
                                        }
                                    }

                                    HStack(spacing: 6) {
                                        Text(node.type.uppercased())
                                            .font(.system(size: 10, weight: .bold))
                                            .foregroundStyle(.secondary)
                                            .padding(.horizontal, 6)
                                            .padding(.vertical, 2)
                                            .background(Color(nsColor: .separatorColor).opacity(0.18), in: RoundedRectangle(cornerRadius: 4, style: .continuous))
                                    }
                                }
                                .frame(maxWidth: .infinity, alignment: .leading)

                                NodeLatencyBadge(
                                    result: store.nodeLatency.result(node, in: item.id),
                                    pending: store.nodeLatency.state(node, in: item.id)
                                )

                                HStack(spacing: 8) {
                                    Button {
                                        store.testNodes([node], in: item)
                                    } label: {
                                        Image(systemName: "bolt.circle")
                                            .font(.system(size: 15))
                                            .foregroundStyle(store.canTestNodes ? AppTheme.accent : .secondary.opacity(0.4))
                                    }
                                    .buttonStyle(.plain)
                                    .disabled(!store.canTestNodes)
                                    .help(store.busy && store.usesCompanyRoutes ? "公司内网已开启，请结束本次连接后再测试节点" : "测试此节点延迟")
                                    .accessibilityLabel("测试节点 \(node.name)")

                                    if isSelected {
                                        HStack(spacing: 4) {
                                            Image(systemName: "checkmark.circle.fill")
                                                .font(.system(size: 11))
                                            Text("当前选用")
                                                .font(.callout.weight(.medium))
                                        }
                                        .foregroundStyle(AppTheme.accent)
                                        .padding(.horizontal, 10)
                                        .padding(.vertical, 5)
                                        .background(AppTheme.accent.opacity(0.1), in: RoundedRectangle(cornerRadius: AppTheme.buttonRadius))
                                    } else {
                                        Button("使用节点") {
                                            store.useNode(node, in: item)
                                        }
                                        .appActionStyle()
                                        .disabled(store.busy || store.subscriptionWorking)
                                        .help(store.busy ? "当前处于连接中，断开连接后即可切换节点" : "使用此节点作为连接配置")
                                        .accessibilityLabel("使用节点 \(node.name)")
                                    }
                                }
                            }
                            .padding(.horizontal, 16)
                            .padding(.vertical, 12)
                            .background(
                                isSelected ? AppTheme.accent.opacity(0.06) : Color.clear,
                                in: RoundedRectangle(cornerRadius: AppTheme.itemRadius, style: .continuous)
                            )
                            .overlay(
                                RoundedRectangle(cornerRadius: AppTheme.itemRadius, style: .continuous)
                                    .strokeBorder(isSelected ? AppTheme.accent.opacity(0.3) : Color.clear, lineWidth: 1)
                            )

                            if node.id != nodes.last?.id {
                                Divider().padding(.leading, 64)
                            }
                        }
                    }
                    .cardSurface()
                }
            } else {
                PageHeader(title: "订阅管理", subtitle: "导入订阅，选择适合当前网络的节点。") {
                    Button("添加订阅", systemImage: "plus") { adding = true }
                        .appActionStyle(primary: true)
                        .fixedSize()
                        .disabled(store.subscriptionWorking)
                }

                HStack {
                    Button("从 Clash Verge 导入", systemImage: "square.and.arrow.down") {
                        store.importSubscriptions()
                    }
                    .appActionStyle()
                    .disabled(store.subscriptionWorking)
                    Text("复制本机订阅，原应用配置保持不变").font(.caption).foregroundStyle(.secondary)
                }

                if store.subscriptions.isEmpty {
                    ContentUnavailableView("还没有订阅", systemImage: "square.stack.3d.up", description: Text("导入本机 Clash Verge 订阅，或添加 HTTPS 订阅地址。"))
                } else {
                    ScrollView {
                        LazyVStack(spacing: 14) {
                            ForEach(store.subscriptions) { item in
                                let isActive = store.configuration.activeConnection.subscription?.subscriptionID == item.id
                                Surface {
                                    HStack(spacing: 14) {
                                        CardIcon(symbol: "square.stack.3d.up")
                                        VStack(alignment: .leading, spacing: 6) {
                                            HStack(spacing: 8) {
                                                Text(item.payload.name).font(.headline)
                                                if isActive {
                                                    StatusPill(title: "当前使用中", symbol: "bolt.fill", color: AppTheme.accent)
                                                }
                                            }
                                            Text("\(item.payload.nodes.count) 个节点 · " + Date(timeIntervalSince1970: Double(item.payload.updated)).formatted(date: .abbreviated, time: .shortened))
                                                .font(.caption)
                                                .foregroundStyle(.secondary)
                                        }
                                        .frame(maxWidth: .infinity, alignment: .leading)

                                        Button {
                                            selected = item.id
                                        } label: {
                                            HStack(spacing: 5) {
                                                Text("选择节点")
                                                Image(systemName: "chevron.right")
                                                    .font(.system(size: 11, weight: .semibold))
                                            }
                                        }
                                        .appActionStyle()
                                        .help("查看并选择此订阅下的节点")

                                        MoreActionsMenu(title: "管理 \(item.payload.name)") {
                                            Button("更新订阅") { store.updateSubscription(item) }.disabled(store.subscriptionWorking)
                                            Button("删除订阅…", role: .destructive) { deleting = item }.disabled(store.subscriptionWorking)
                                        }
                                    }
                                    if let quota = item.quotaText {
                                        HStack(spacing: 6) {
                                            Image(systemName: "chart.bar.doc.horizontal")
                                                .font(.caption)
                                                .foregroundStyle(.secondary)
                                            Text(quota)
                                                .font(.caption)
                                                .foregroundStyle(.secondary)
                                        }
                                        .padding(.top, 10)
                                    }
                                }
                            }
                        }
                    }
                }
            }
            if store.subscriptionWorking {
                HStack {
                    ProgressView().controlSize(.small)
                    Text("正在读取订阅…").font(.callout).foregroundStyle(.secondary)
                }
            }
            if store.busy {
                Label("连接期间可浏览和更新订阅，断开后可切换节点。", systemImage: "info.circle")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(AppTheme.pagePadding)
        .frame(maxWidth: AppTheme.contentWidth, maxHeight: .infinity, alignment: .topLeading)
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .sheet(isPresented: $adding) {
            VStack(alignment: .leading, spacing: 18) {
                PageHeading(title: "添加订阅", subtitle: "使用服务商提供的 Clash YAML 订阅地址。")
                TextField("订阅名称", text: $name).textFieldStyle(.roundedBorder).controlSize(.large)
                SecureField("HTTPS 订阅地址", text: $address).textFieldStyle(.roundedBorder).controlSize(.large)
                Text("地址可能包含访问令牌，仅保存在本机。更新失败时保留已有节点。").font(.caption).foregroundStyle(.secondary)
                if store.subscriptionWorking { ProgressView().controlSize(.small) }
                if store.isError, let message = store.message { Text(message).font(.callout).foregroundStyle(.orange) }
                HStack {
                    Spacer()
                    Button("取消") { adding = false }.disabled(store.subscriptionWorking)
                    Button("添加") {
                        store.addSubscription(name: name, url: address)
                    }
                    .appActionStyle(primary: true)
                    .disabled(store.subscriptionWorking || name.trimmingCharacters(in: .whitespaces).isEmpty || address.isEmpty)
                }
            }
            .padding(28)
            .frame(width: 460)
            .interactiveDismissDisabled(store.subscriptionWorking)
        }
        .onChange(of: store.subscriptionWorking) { before, working in
            if adding && before && !working && !store.isError { adding = false; name = ""; address = "" }
        }
        .confirmationDialog("删除订阅？", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }), presenting: deleting) { item in
            Button("删除订阅", role: .destructive) { store.removeSubscription(item); deleting = nil }
            Button("取消", role: .cancel) { deleting = nil }
        } message: { _ in Text("仅删除 GoConnect 中的副本。") }
    }
}

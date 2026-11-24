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
    @State private var selectedSubscriptionID: UUID?
    @State private var adding = false
    @State private var name = ""
    @State private var address = ""
    @State private var deleting: Subscription?
    @FocusState private var nameFocused: Bool

    private var selectedSubscription: Subscription? {
        if let selected = store.subscriptions.first(where: { $0.id == selectedSubscriptionID }) {
            return selected
        }
        let activeID = store.configuration.activeConnection.subscription?.subscriptionID
        return store.subscriptions.first { $0.id == activeID } ?? store.subscriptions.first
    }

    var body: some View {
        Group {
            if let selectedSubscription {
                HSplitView {
                    subscriptionList
                        .frame(minWidth: 200, idealWidth: 224, maxWidth: 240)
                    SubscriptionNodesView(store: store, item: selectedSubscription)
                        .frame(minWidth: 412, maxWidth: .infinity, maxHeight: .infinity)
                }
            } else {
                VStack(spacing: 12) {
                    ContentUnavailableView {
                        Label("还没有订阅", systemImage: "square.stack.3d.up")
                    } description: {
                        Text("导入本机 Clash Verge 订阅，或添加 HTTPS 订阅地址。")
                    } actions: {
                        subscriptionActions
                    }
                    subscriptionProgress
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .sheet(isPresented: $adding) {
            VStack(alignment: .leading, spacing: 20) {
                Text("添加订阅").font(.title2.weight(.semibold))
                VStack(alignment: .leading, spacing: 14) {
                    VStack(alignment: .leading, spacing: 6) {
                        Text("订阅名称").font(.callout)
                        TextField("例如：工作订阅", text: $name).focused($nameFocused).accessibilityLabel("订阅名称")
                    }
                    VStack(alignment: .leading, spacing: 6) {
                        Text("HTTPS 订阅地址").font(.callout)
                        SecureField("粘贴服务商提供的 HTTPS 地址", text: $address).accessibilityLabel("HTTPS 订阅地址")
                            .help("例如 https://example.com/subscription")
                    }
                }.textFieldStyle(.roundedBorder).controlSize(.large)
                Text("使用服务商提供的 Clash YAML 地址。地址可能含访问令牌，仅保存在本机；更新失败时保留已有节点。")
                    .font(.callout).foregroundStyle(.secondary)
                if store.isError, let message = store.message {
                    Label(message, systemImage: "exclamationmark.circle.fill").font(.callout).foregroundStyle(.red)
                }
                HStack {
                    if store.subscriptionWorking { ProgressView().controlSize(.small) }
                    Spacer()
                    Button("取消") { adding = false }.appActionStyle().keyboardShortcut(.cancelAction).disabled(store.subscriptionWorking)
                    Button("添加") { store.addSubscription(name: name, url: address) }
                        .appActionStyle(primary: true).keyboardShortcut(.defaultAction)
                        .disabled(store.subscriptionWorking || name.trimmingCharacters(in: .whitespaces).isEmpty || address.isEmpty)
                }
            }
            .padding(28).frame(width: 460).fixedSize(horizontal: false, vertical: true)
            .interactiveDismissDisabled(store.subscriptionWorking)
            .onAppear { nameFocused = true }
        }
        .onChange(of: store.subscriptionWorking) { before, working in
            if adding && before && !working && !store.isError { adding = false; name = ""; address = "" }
        }
        .confirmationDialog("删除订阅？", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }), presenting: deleting) { item in
            Button("删除订阅", role: .destructive) { store.removeSubscription(item); deleting = nil }
            Button("取消", role: .cancel) { deleting = nil }
        } message: { _ in Text("仅删除 GoConnect 中的副本。") }
    }

    private var subscriptionList: some View {
        VStack(alignment: .leading, spacing: 8) {
            VStack(alignment: .leading, spacing: 6) {
                HStack {
                    Text("订阅").font(.headline)
                    Spacer()
                    subscriptionActions
                        .labelStyle(.iconOnly)
                }
                subscriptionProgress
            }
            .padding(.horizontal, 16)
            .padding(.top, 16)

            List(selection: Binding(get: { selectedSubscription?.id }, set: { selectedSubscriptionID = $0 })) {
                ForEach(store.subscriptions) { item in
                    let active = store.configuration.activeConnection.subscription?.subscriptionID == item.id
                    HStack(spacing: 8) {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(item.payload.name).font(.callout.weight(.semibold)).lineLimit(1)
                            Text(active ? "\(item.payload.nodes.count) 个节点 · 当前使用" : "\(item.payload.nodes.count) 个节点")
                                .font(.caption).foregroundStyle(.secondary)
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .help(item.payload.name)
                        MoreActionsMenu(title: "管理 \(item.payload.name)") {
                            Button("更新订阅", systemImage: "arrow.clockwise") { store.updateSubscription(item) }.disabled(store.subscriptionWorking)
                            Button("删除订阅…", systemImage: "trash", role: .destructive) { deleting = item }.disabled(store.subscriptionWorking)
                        }
                    }
                    .padding(.vertical, 6)
                    .tag(item.id)
                }
            }
            .listStyle(.inset)
            .scrollContentBackground(.hidden)
        }
    }

    private var subscriptionActions: some View {
        HStack(spacing: 8) {
            Menu("导入订阅", systemImage: "square.and.arrow.down") {
                Button("导入 YAML 文件…", systemImage: "doc.badge.plus") { store.importSubscriptionFile() }
                Button("从 Clash Verge 导入", systemImage: "square.and.arrow.down") { store.importSubscriptions() }
            }
            .menuStyle(.borderedButton)
            .fixedSize()
            .help("导入订阅")

            Button("添加订阅", systemImage: "plus") { adding = true }
                .appActionStyle()
                .help("添加订阅")
        }
        .disabled(store.subscriptionWorking)
    }

    @ViewBuilder private var subscriptionProgress: some View {
        if store.subscriptionWorking {
            HStack { ProgressView().controlSize(.small); Text("正在读取订阅…").font(.callout).foregroundStyle(.secondary) }
        }
        if store.busy {
            Label("连接期间可浏览和更新订阅，断开后可切换节点。", systemImage: "info.circle")
                .font(.caption).foregroundStyle(.secondary)
        }
    }
}

private struct SubscriptionNodesView: View {
    @Bindable var store: AppStore
    let item: Subscription
    @State private var search = ""
    @State private var sortByLatency = false
    private let columns = [GridItem(.adaptive(minimum: 210), spacing: 12, alignment: .topLeading)]

    private var testingUnavailableReason: String? {
        if !store.localReady { return "运行组件未就绪，请在诊断页检查环境。" }
        if store.nodeLatency.isRunning { return "节点测试正在进行。" }
        if store.subscriptionWorking { return "订阅正在更新，完成后可测试节点。" }
        if store.busy { return "请结束当前连接或诊断后再测试节点。" }
        return nil
    }

    private func visibleNodes(in item: Subscription) -> [SubscriptionNode] {
        let nodes = item.payload.nodes.filter { search.isEmpty || $0.name.localizedCaseInsensitiveContains(search) || $0.type.localizedCaseInsensitiveContains(search) }
        guard sortByLatency && !store.nodeLatency.isRunning else { return nodes }
        return nodes.enumerated().sorted { a, b in
            let left = store.nodeLatency.result(a.element, in: item.id)?.independentMilliseconds ?? Int.max
            let right = store.nodeLatency.result(b.element, in: item.id)?.independentMilliseconds ?? Int.max
            return left == right ? a.offset < b.offset : left < right
        }.map(\.element)
    }

    var body: some View {
        let updated = Date(timeIntervalSince1970: Double(item.payload.updated)).formatted(date: .abbreviated, time: .shortened)
        VStack(alignment: .leading, spacing: 12) {
            PageHeading(title: item.payload.name, subtitle: "\(item.payload.nodes.count) 个节点 · 更新于 \(updated)")
            HStack(spacing: 10) {
                InlineSearchField(prompt: "搜索节点名称或协议", text: $search)
                    .frame(maxWidth: 230)
                Spacer(minLength: 0)
                Button("更新订阅", systemImage: "arrow.clockwise") { store.updateSubscription(item) }
                    .appActionStyle()
                    .disabled(store.subscriptionWorking)
                    .fixedSize(horizontal: true, vertical: false)
            }
            if let count = item.payload.unsupportedCount, count > 0 {
                Label("另有 \(count) 个节点的协议或传输暂不支持，未导入。", systemImage: "info.circle").font(.caption).foregroundStyle(.secondary)
            }
            HStack(spacing: 12) {
                if store.nodeLatency.isRunning {
                    ProgressView().controlSize(.small)
                    Text("测试中 \(store.nodeLatency.completed) / \(store.nodeLatency.total)").font(.callout).monospacedDigit()
                    Button("停止测试") { store.nodeLatency.stop() }.appActionStyle()
                } else {
                    Button("测试全部", systemImage: "bolt.horizontal.circle") { store.testNodes(item.payload.nodes, in: item) }
                        .appActionStyle().disabled(!store.canTestNodes)
                        .help(testingUnavailableReason ?? "测试全部节点的 HTTPS 响应延迟")
                }
                Spacer(minLength: 8)
                Toggle("按延迟排序", isOn: $sortByLatency).toggleStyle(.checkbox)
            }
            Text("经物理网卡测试 gstatic.com 的 HTTPS 响应，每次最长 8 秒。成功率统计最近 5 次有效测试。")
                .font(.caption).foregroundStyle(.secondary)
            if store.busy && store.usesCompanyRoutes {
                Label("公司内网已开启，请结束本次连接后再测试节点。", systemImage: "info.circle").font(.caption).foregroundStyle(.secondary)
            } else if !store.canTestNodes, let reason = testingUnavailableReason {
                Label(reason, systemImage: "info.circle").font(.caption).foregroundStyle(.secondary)
            }
            if let error = store.nodeLatency.storageError { Text(error).font(.caption).foregroundStyle(.red) }
            let nodes = visibleNodes(in: item)
            if nodes.isEmpty {
                ContentUnavailableView.search(text: search)
            } else {
                ScrollView {
                    LazyVGrid(columns: columns, alignment: .leading, spacing: 12) {
                        ForEach(nodes) { node in nodeCard(node) }
                    }
                    .padding(2)
                }
                .id(item.id)
            }
            if store.subscriptionWorking {
                HStack { ProgressView().controlSize(.small); Text("正在更新订阅…").font(.caption).foregroundStyle(.secondary) }
            }
        }
        .padding(AppTheme.pagePadding)
        .onChange(of: item.id) {
            search = ""
            sortByLatency = false
        }
    }

    private func nodeCard(_ node: SubscriptionNode) -> some View {
        let selected = store.configuration.activeConnection.subscription == SubscriptionSelection(subscriptionID: item.id, nodeName: node.name)
        return VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .top, spacing: 10) {
                NodeFlagIcon(flag: RegionFlagHelper.detectFlag(in: node.name))
                    .help("按节点名称识别的地区提示，不代表已验证的实际位置")
                VStack(alignment: .leading, spacing: 4) {
                    Text(node.name).font(.headline).lineLimit(2, reservesSpace: true).textSelection(.enabled).help(node.name)
                    Text(node.type.uppercased()).font(.caption).foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .frame(minHeight: 46, alignment: .topLeading)

            HStack(spacing: 8) {
                NodeLatencyBadge(
                    result: store.nodeLatency.result(node, in: item.id),
                    pending: store.nodeLatency.state(node, in: item.id)
                )
                Spacer(minLength: 0)
                Button("测试", systemImage: "bolt.circle") { store.testNodes([node], in: item) }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                    .disabled(!store.canTestNodes)
                    .accessibilityLabel("测试节点 \(node.name)")
                    .help(testingUnavailableReason ?? "测试 HTTPS 响应延迟")
            }
            .frame(minHeight: 44)

            Group {
                if selected {
                    Label("当前选用", systemImage: "checkmark.circle.fill")
                        .font(.callout.weight(.medium))
                        .foregroundStyle(AppTheme.accent)
                } else {
                    Button { store.useNode(node, in: item) } label: {
                        Text("使用节点").frame(maxWidth: .infinity)
                    }
                    .appActionStyle()
                    .disabled(store.busy || store.subscriptionWorking)
                    .help(store.busy ? "断开 VPN 后可切换节点" : "使用此节点作为当前线路")
                    .accessibilityLabel("使用节点 \(node.name)")
                }
            }
            .frame(maxWidth: .infinity, minHeight: AppTheme.actionHeight)
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .cardSurface(emphasized: selected)
        .accessibilityElement(children: .contain)
    }
}

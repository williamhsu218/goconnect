import SwiftUI
import WebKit
import GoConnectCore

struct YunyooUsageCard: View {
    @Bindable var store: YunyooUsageStore
    let subscription: Subscription
    @State private var settingUp = false
    @State private var serviceID = ""
    @State private var setupError: String?
    @State private var loginAfterSetup = false

    private var binding: YunyooUsageBinding? { store.binding(for: subscription) }
    private var eligible: Bool {
        binding != nil || (subscription.payload.url.hasPrefix("local://") && YunyooUsageStore.singleServer(subscription) != nil)
    }
    var body: some View {
        if eligible {
            Surface {
                VStack(alignment: .leading, spacing: 14) {
                    ViewThatFits(in: .horizontal) {
                      HStack {
                        Label("云悠套餐流量", systemImage: "chart.pie").font(.headline)
                        Spacer()
                        usageActions
                      }
                      VStack(alignment: .leading, spacing: 10) {
                        Label("云悠套餐流量", systemImage: "chart.pie").font(.headline)
                        usageActions
                      }
                    }
                    if let binding {
                        if let value = store.snapshot(for: binding) {
                            ViewThatFits(in: .horizontal) {
                                HStack(alignment: .firstTextBaseline, spacing: 10) {
                                    remainingAmount(value)
                                    Spacer()
                                    usageSummary(value)
                                }
                                VStack(alignment: .leading, spacing: 6) {
                                    remainingAmount(value)
                                    usageSummary(value)
                                }
                            }
                            ProgressView(value: value.usedFraction).tint(value.exceeded ? .orange : AppTheme.accent)
                            if value.exceeded { Text("当前已超过套餐额度。").font(.caption).foregroundStyle(.orange) }
                            Text("更新于 \(value.updatedAt.formatted(date: .abbreviated, time: .standard))")
                                .font(.caption).foregroundStyle(.secondary)
                        } else {
                            Text("登录云悠后显示套餐剩余流量。").foregroundStyle(.secondary)
                        }
                        status(binding)
                        Text("服务 \(binding.serviceID) · 本组节点共享同一 VPS 套餐额度 · 手动刷新")
                            .font(.caption).foregroundStyle(.secondary)
                    } else {
                        Text("关联这台 VPS 的云悠产品 ID，读取套餐总量、已用和剩余流量。")
                            .font(.callout).foregroundStyle(.secondary)
                    }
                    if let error = store.storageError { Text(error).font(.caption).foregroundStyle(.orange) }
                    if let message = store.relayMessage { Text(message).font(.caption).foregroundStyle(.secondary) }
                }
            }
            .sheet(isPresented: $settingUp, onDismiss: {
                if loginAfterSetup { loginAfterSetup = false; store.loginBinding = binding }
            }) { setupSheet }
        }
    }

    private var usageActions: some View {
        HStack(spacing: 8) {
            YunyooRelayControls(store: store, subscription: subscription)
            if let binding {
                if store.state(for: binding) == .refreshing { ProgressView().controlSize(.small) }
                Button("刷新", systemImage: "arrow.clockwise") { Task { await store.refresh(binding) } }
                    .appActionStyle().disabled(store.state(for: binding) == .refreshing || store.relayPublishing)
                Button(store.state(for: binding) == .loginRequired ? "重新登录" : "登录云悠") { store.loginBinding = binding }.appActionStyle()
            } else {
                Button("关联云悠流量") { settingUp = true }.appActionStyle(primary: true).disabled(!store.readable)
            }
        }
    }

    private func remainingAmount(_ value: YunyooTrafficSnapshot) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text("剩余").font(.callout).foregroundStyle(.secondary)
            Text(YunyooTrafficSnapshot.formatGB(value.remainingGB))
                .font(.system(size: 26, weight: .semibold)).monospacedDigit()
        }
    }

    private func usageSummary(_ value: YunyooTrafficSnapshot) -> some View {
        Text("已用 \(YunyooTrafficSnapshot.formatGB(value.displayedUsedGB)) / 总量 \(YunyooTrafficSnapshot.formatGB(value.totalGB))")
            .font(.caption).foregroundStyle(.secondary)
            .fixedSize(horizontal: false, vertical: true)
    }

    @ViewBuilder private func status(_ binding: YunyooUsageBinding) -> some View {
        switch store.state(for: binding) {
        case .loginRequired:
            Label(store.snapshot(for: binding) == nil ? "请登录云悠以读取流量。" : "登录已失效，当前显示上次数据。", systemImage: "person.crop.circle.badge.exclamationmark")
                .font(.caption).foregroundStyle(.orange)
        case .failed(let message): Text(message).font(.caption).foregroundStyle(.orange)
        case .cached:
            if store.snapshot(for: binding) != nil { Text("当前为上次保存的数据，点击刷新获取最新用量。").font(.caption).foregroundStyle(.secondary) }
        default: EmptyView()
        }
    }

    private var setupSheet: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("关联云悠套餐流量").font(.title2.weight(.semibold))
            Text("产品 ID 位于云悠产品详情地址的 id= 后面。此关联仅添加流量数据来源。")
                .font(.callout).foregroundStyle(.secondary)
            TextField("云悠产品 ID", text: $serviceID).textFieldStyle(.roundedBorder).accessibilityLabel("云悠产品 ID")
            Text("节点地址：\(YunyooUsageStore.singleServer(subscription) ?? "—")").font(.caption).foregroundStyle(.secondary)
            if let setupError { Text(setupError).font(.callout).foregroundStyle(.red) }
            HStack {
                Spacer()
                Button("取消") { settingUp = false }.appActionStyle().keyboardShortcut(.cancelAction)
                Button("关联并登录") {
                    do { try store.link(subscription: subscription, serviceID: serviceID); loginAfterSetup = true; settingUp = false }
                    catch { setupError = error.localizedDescription }
                }.appActionStyle(primary: true).keyboardShortcut(.defaultAction)
                    .disabled(!YunyooTrafficSnapshot.validServiceID(serviceID.trimmingCharacters(in: .whitespacesAndNewlines)))
            }
        }.padding(24).frame(width: 450)
    }
}

struct YunyooRelayControls: View {
    @Bindable var store: YunyooUsageStore
    let subscription: Subscription
    var onAuthorized: () -> Void = {}
    @State private var confirming = false
    var body: some View {
        if let settings = store.relaySettings, let binding = store.binding(for:subscription) {
            Button(subscription.quotaValues["auth_required"] == 1 || store.relayNeedsLogin ? "重新授权" : "授权同步端") { confirming = true }
                .appActionStyle().disabled(store.relayPublishing || store.state(for: binding) == .refreshing)
                .confirmationDialog("授权同步端并刷新用量？",isPresented:$confirming,titleVisibility:.visible) {
                    Button("允许并刷新用量") { Task { if await store.publishRelay(binding) { onAuthorized() } } }
                    Button("取消",role:.cancel) {}
                } message: {
                    Text("当前云悠登录会话将通过 HTTPS 保存到 \(settings.baseURL) 的受限文件。该会话具有云悠账号登录权限；VPS 只在手动刷新或重新授权时读取此产品流量。YAML 和订阅客户端不含云悠会话或密码。")
                }
        }
    }
}

private struct YunyooWebView: NSViewRepresentable {
    let client: YunyooQuotaClient
    func makeNSView(context: Context) -> WKWebView { client.webView }
    func updateNSView(_ view: WKWebView, context: Context) {}
}

struct YunyooLoginView: View {
    @Bindable var store: YunyooUsageStore
    let binding: YunyooUsageBinding
    var onRelayAuthorized: (YunyooUsageBinding) -> Void = { _ in }
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                VStack(alignment: .leading, spacing: 3) {
                    Text("登录云悠并同步流量").font(.title2.weight(.semibold))
                    Text("https://yunyoo.cc · 服务 \(binding.serviceID)").font(.caption).foregroundStyle(.secondary)
                }
                Spacer()
                Button("完成登录并刷新") {
                    Task {
                        let publishing = store.relayNeedsLogin
                        if await store.completeLogin(binding) {
                            if publishing { onRelayAuthorized(binding) }
                            store.loginBinding = nil
                        }
                    }
                }.appActionStyle(primary: true)
                    .disabled(store.state(for: binding) == .refreshing || store.relayPublishing)
                Button("关闭") { store.loginBinding = nil }.appActionStyle().keyboardShortcut(.cancelAction)
            }
            Text("请在官方页面完成登录和验证，再点击“完成登录并刷新”获取最新用量。")
                .font(.callout).foregroundStyle(.secondary)
            YunyooWebView(client: store.client)
                .frame(minWidth: 740, minHeight: 500)
            if case .failed(let message) = store.state(for: binding) {
                Text(message).font(.caption).foregroundStyle(.orange)
            }
            if let message = store.relayMessage { Text(message).font(.caption).foregroundStyle(.secondary) }
        }
        .padding(20).frame(width: 820, height: 680)
        .task { try? await store.client.prepare(forceReload: true) }
    }
}

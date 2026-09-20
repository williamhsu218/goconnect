import SwiftUI
import GoConnectCore

struct DiagnosticsView: View {
    let store: AppStore

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: AppTheme.sectionSpacing) {
                PageHeader(title: "连接诊断", subtitle: "查看本地环境和本次运行状态。") {
                    Button("导出摘要", systemImage: "square.and.arrow.up") {
                        store.exportDiagnostics()
                    }
                    .appActionStyle()
                    .fixedSize()
                }

                NetworkServiceCard(store: store)

                LazyVGrid(columns: [GridItem(.adaptive(minimum: 280), spacing: AppTheme.sectionSpacing)], alignment: .leading, spacing: AppTheme.sectionSpacing) {
                    Surface {
                        HStack {
                            Text("运行环境").font(.headline)
                            Spacer()
                            StatusPill(
                                title: store.localReady ? "正常" : "异常",
                                symbol: store.localReady ? "checkmark.circle.fill" : "exclamationmark.triangle.fill",
                                color: store.localReady ? AppTheme.success : AppTheme.warning
                            )
                        }

                        VStack(spacing: 0) {
                            statusRow("运行组件", value: store.localReady ? "已内置" : "缺少组件", good: store.localReady)
                                .padding(.vertical, 8)
                            Divider()
                            statusRow("连接模式", value: store.accessTitle, good: true)
                                .padding(.vertical, 8)
                            Divider()
                            statusRow("Tailscale", value: store.routeSnapshot?.tailscaleActive == true ? "已识别" : "未发现", good: true)
                                .padding(.vertical, 8)
                            Divider()
                            statusRow("网络接管", value: "系统路由 → TUN", good: true)
                                .padding(.vertical, 8)
                            if let snapshot = store.routeSnapshot {
                                Divider()
                                HStack {
                                    Text("实时连接分流").font(.callout)
                                    Spacer()
                                    HStack(spacing: 6) {
                                        Label("\(snapshot.vpn) VPN", systemImage: "shield.fill")
                                            .font(.caption.weight(.semibold).monospacedDigit())
                                            .foregroundStyle(AppTheme.accent)
                                        Text("·").foregroundStyle(.secondary)
                                        Label("\(snapshot.direct) 直连", systemImage: "arrow.triangle.branch")
                                            .font(.caption.monospacedDigit())
                                            .foregroundStyle(.secondary)
                                        if snapshot.unknown > 0 {
                                            Text("·").foregroundStyle(.secondary)
                                            Label("\(snapshot.unknown) 未知", systemImage: "questionmark.circle")
                                                .font(.caption.monospacedDigit())
                                                .foregroundStyle(.secondary)
                                        }
                                    }
                                }
                                .padding(.vertical, 8)
                            }
                        }
                        .padding(.top, 6)

                        HStack(spacing: 10) {
                            Button("重新检测", systemImage: "arrow.clockwise") {
                                store.refreshEnvironment()
                            }
                            .appActionStyle()

                            Button("查看配置", systemImage: "doc.text") {
                                store.revealConfiguration()
                            }
                            .appActionStyle()

                            Spacer()
                        }
                        .padding(.top, 14)
                    }

                    Surface {
                        Text("隧道测试").font(.headline)
                        Text("只检查线路认证与隧道建立，不启用 App 分流。真实可达性仍需通过目标请求验证。")
                            .font(.callout)
                            .foregroundStyle(.secondary)
                            .padding(.top, 12)

                        HStack(spacing: 10) {
                            Button(
                                store.testOnly && store.busy ? "结束测试" : "测试隧道",
                                systemImage: store.testOnly && store.busy ? "stop.fill" : "play.fill"
                            ) {
                                store.testOnly && store.busy ? store.disconnect() : store.connect(test: true)
                            }
                            .appActionStyle()
                            .disabled(store.busy && !store.testOnly)

                            if !store.addresses.isEmpty {
                                Text(store.addresses.joined(separator: " · "))
                                    .font(.caption.monospacedDigit())
                                    .foregroundStyle(.secondary)
                                    .lineLimit(1)
                            }
                            Spacer()
                        }
                        .padding(.top, 14)

                        if let snapshot = store.routeSnapshot, let message = snapshot.message {
                            Text(message)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                                .padding(.top, 10)
                        }
                    }
                }

                Surface {
                    HStack {
                        Label("运行记录", systemImage: "list.bullet.rectangle")
                            .font(.headline)
                        CountBadge(count: store.activity.count)
                        Spacer()
                        if !store.activity.isEmpty {
                            Button(role: .destructive) {
                                store.activity.removeAll()
                            } label: {
                                Label("清空日志", systemImage: "trash")
                            }
                            .appActionStyle()
                            .controlSize(.small)
                            .help("清空当前运行记录")
                        }
                    }

                    if store.activity.isEmpty {
                        Text("尚无连接记录。配置保存、连接和错误会出现在这里。")
                            .font(.callout)
                            .foregroundStyle(.secondary)
                            .padding(.vertical, 16)
                    } else {
                        VStack(spacing: 0) {
                            ForEach(store.activity) { entry in
                                HStack(alignment: .firstTextBaseline, spacing: 10) {
                                    Text(entry.date.formatted(date: .omitted, time: .standard))
                                        .font(.system(.caption, design: .monospaced))
                                        .foregroundStyle(.secondary)
                                        .frame(width: 68, alignment: .leading)

                                    Image(systemName: entry.isError ? "exclamationmark.circle.fill" : "checkmark.circle.fill")
                                        .font(.system(size: 11, weight: .semibold))
                                        .foregroundStyle(entry.isError ? AppTheme.warning : AppTheme.success.opacity(0.85))
                                        .frame(width: 14)

                                    Text(entry.message)
                                        .font(.callout)
                                        .foregroundStyle(entry.isError ? AppTheme.warning : Color.primary)
                                        .textSelection(.enabled)
                                        .lineSpacing(2)
                                        .frame(maxWidth: .infinity, alignment: .leading)
                                }
                                .padding(.vertical, 7)
                                .padding(.horizontal, 8)
                                .background(
                                    entry.isError ? AppTheme.warning.opacity(0.08) : Color.clear,
                                    in: RoundedRectangle(cornerRadius: 6, style: .continuous)
                                )

                                if entry.id != store.activity.last?.id {
                                    Divider().padding(.leading, 70)
                                }
                            }
                        }
                        .padding(.top, 8)
                    }
                }
            }
            .padding(AppTheme.pagePadding)
            .frame(maxWidth: AppTheme.contentWidth, alignment: .topLeading)
            .frame(maxWidth: .infinity, alignment: .topLeading)
        }
    }

    private func statusRow(_ title: String, value: String, good: Bool) -> some View {
        HStack {
            Text(title).font(.callout)
            Spacer()
            HStack(spacing: 5) {
                Image(systemName: good ? "checkmark.circle.fill" : "exclamationmark.circle.fill")
                    .foregroundStyle(good ? AppTheme.success : AppTheme.warning)
                Text(value)
                    .font(.callout)
                    .foregroundStyle(good ? .secondary : AppTheme.warning)
            }
        }
    }
}

struct NetworkServiceCard: View {
    let store: AppStore

    var body: some View {
        let service = store.networkService
        let isReady = service.status.ready

        Surface(emphasized: !isReady) {
            VStack(alignment: .leading, spacing: 14) {
                // Security Panel Header
                HStack(spacing: 14) {
                    ZStack {
                        RoundedRectangle(cornerRadius: 10, style: .continuous)
                            .fill(isReady ? AppTheme.success.opacity(0.12) : AppTheme.warning.opacity(0.12))
                        Image(systemName: isReady ? "checkmark.shield.fill" : "exclamationmark.shield.fill")
                            .font(.system(size: 20, weight: .semibold))
                            .foregroundStyle(isReady ? AppTheme.success : AppTheme.warning)
                    }
                    .frame(width: 42, height: 42)

                    VStack(alignment: .leading, spacing: 3) {
                        HStack(spacing: 8) {
                            Text("本机特权网络服务").font(.headline)
                            if let version = service.status.version, !version.isEmpty {
                                Text("v\(version)")
                                    .font(.caption2.monospaced())
                                    .padding(.horizontal, 6)
                                    .padding(.vertical, 2)
                                    .background(Color.secondary.opacity(0.1), in: Capsule())
                                    .foregroundStyle(.secondary)
                            }
                        }
                        Text("透明分流与虚拟网卡 (TUN) 路由特权守护进程").font(.caption).foregroundStyle(.secondary)
                    }

                    Spacer()

                    if service.working {
                        HStack(spacing: 6) {
                            ProgressView().controlSize(.small)
                            Text("处理中…").font(.caption).foregroundStyle(.secondary)
                        }
                    } else {
                        StatusPill(
                            title: service.status.title,
                            symbol: isReady ? "checkmark.shield.fill" : "exclamationmark.triangle.fill",
                            color: isReady ? AppTheme.success : AppTheme.warning
                        )
                    }
                }

                Divider()

                // Security Details Info
                VStack(alignment: .leading, spacing: 8) {
                    HStack(spacing: 8) {
                        Image(systemName: isReady ? "lock.open.fill" : "lock.fill")
                            .font(.system(size: 13))
                            .foregroundStyle(isReady ? AppTheme.success : AppTheme.warning)
                            .frame(width: 16)
                        Text(
                            service.working ? "正在处理，请完成系统授权。GoConnect 不会保存管理员密码。" :
                            isReady ? "授权有效：普通连接与分流变更无需重复输入管理员密码。" :
                            service.status.state == "notInstalled" ? "尚未安装：首次运行需要安装特权网络服务，以配置系统 TUN 虚拟网卡与路由。" :
                            service.status.state == "needsUpdate" ? "需要更新：检测到新版本网络服务组件，建议更新以确保稳定性与安全性。" :
                            "首次安装或后台组件升级时需要授权；普通连接无需重复输入管理员密码。"
                        )
                        .font(.callout)
                        .foregroundStyle(isReady ? .secondary : AppTheme.warning)
                    }

                    HStack(spacing: 8) {
                        Image(systemName: "shield.lefthalf.filled")
                            .font(.system(size: 13))
                            .foregroundStyle(.secondary)
                            .frame(width: 16)
                        Text("通信保护：基于本地独占 Unix Domain Socket 与进程凭据认证，无任何外部监听端口。")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }

                // Action Toolbar
                HStack(spacing: 12) {
                    if isReady {
                        Button("修复服务", systemImage: "wrench.and.screwdriver") {
                            store.manageNetworkService()
                        }
                        .appActionStyle()
                    } else {
                        Button(action: { store.manageNetworkService() }) {
                            Label(
                                service.status.state == "notInstalled" ? "安装并授权" : service.status.state == "needsUpdate" ? "更新服务" : "修复服务",
                                systemImage: "lock.shield"
                            )
                        }
                        .appActionStyle(primary: true)
                    }

                    Button("重新检测", systemImage: "arrow.clockwise") {
                        service.refresh()
                    }
                    .appActionStyle()

                    Spacer()

                    if service.status.state != "notInstalled" && service.status.state != "checking" {
                        Button(role: .destructive) {
                            store.manageNetworkService(uninstall: true)
                        } label: {
                            Label("卸载服务", systemImage: "trash")
                        }
                        .buttonStyle(.borderless)
                        .foregroundStyle(.red)
                    }
                }
                .disabled(store.busy || service.working)

                if store.busy {
                    HStack(spacing: 6) {
                        Image(systemName: "info.circle")
                        Text("连接期间特权网络服务处于运行状态；断开后可进行组件修复或卸载维护。")
                    }
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .padding(.top, 2)
                }
            }
        }
    }
}

struct PreferencesView: View {
    var showMenuBar: () -> Void = {}
    var body: some View {
        Form {
            Button("重新显示菜单栏快捷菜单", action: showMenuBar)
            LabeledContent("版本", value: Product.version)
            LabeledContent("连接协议", value: "AnyConnect / Clash 订阅节点")
            LabeledContent("运行方式", value: "透明 App 分流 / 无 PF")
            Text("AnyConnect 密码保存在内存或钥匙串；订阅地址及节点凭据保存在仅当前用户可读的本机缓存。订阅更新会访问服务商地址，不记录访问网址。系统 DNS 沿用当前网络。")
                .font(.callout).foregroundStyle(.secondary)
        }.formStyle(.grouped)
    }
}

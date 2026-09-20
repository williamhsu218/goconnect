import SwiftUI
import GoConnectCore

struct ApplicationRoutingCard: View {
    @Bindable var store: AppStore
    @State private var showingHelp = false

    private var mode: RoutingMode {
        store.configuration.routingMode
    }

    private var apps: [AllowedApplication] {
        store.configuration.routingApplications
    }

    private var appsSubtitle: String {
        if apps.isEmpty {
            return "尚未添加分流目标"
        }
        return mode == .global ? "\(apps.count) 个目标直连原网络" : "\(apps.count) 个目标通过当前线路"
    }

    private var appsExplanation: String {
        if mode == .global {
            return "全局模式下，名单内的 App 与独立服务保持原网络直连，其余流量走 VPN。"
        } else {
            return "白名单模式下，仅名单内的 App 与独立服务走 VPN，其余流量保持原网络。"
        }
    }

    var body: some View {
        Surface {
            HStack(spacing: 8) {
                Text("应用分流")
                    .font(.headline)

                Button {
                    showingHelp.toggle()
                } label: {
                    Image(systemName: "questionmark.circle")
                        .font(.system(size: 13))
                        .foregroundStyle(.secondary)
                }
                .buttonStyle(.plain)
                .popover(isPresented: $showingHelp, arrowEdge: .top) {
                    ConnectionBoundaryHelpView(mode: mode)
                }
                .help("查看连接边界与分流说明")

                Spacer()

                ModeBadge(mode: mode)
            }

            VStack(alignment: .leading, spacing: 10) {
                HStack(spacing: 12) {
                    Image(systemName: mode == .global ? "arrow.triangle.branch" : "square.grid.2x2")
                        .font(.system(size: 18))
                        .foregroundStyle(AppTheme.accent)
                        .frame(width: 32, height: 32)
                        .background(AppTheme.accent.opacity(0.1), in: RoundedRectangle(cornerRadius: 8))

                    VStack(alignment: .leading, spacing: 3) {
                        Text(mode == .global ? "直连 App / 服务" : "应用 / 服务白名单")
                            .font(.body.weight(.medium))
                        Text(appsSubtitle)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    CountBadge(count: apps.count)
                }

                Text(appsExplanation)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.vertical, 2)

                HStack {
                    Spacer()
                    Button {
                        let destination: Page = mode == .global ? .exclusions : .applications
                        store.navigate(to: destination)
                    } label: {
                        HStack(spacing: 4) {
                            Text("管理名单")
                            Image(systemName: "arrow.right")
                        }
                        .font(.callout.weight(.medium))
                    }
                    .appActionStyle()
                    .accessibilityLabel("前往\(mode == .global ? "直连 App" : "应用白名单")管理")
                }
            }
            .padding(.top, 12)
        }
    }
}

struct DirectRoutingCard: View {
    @Bindable var store: AppStore

    private var directCount: Int {
        store.configuration.directDomains.count
    }

    private var companyCount: Int? {
        store.usesCompanyRoutes ? store.configuration.activeConnection.remoteNetworks.count : nil
    }

    var body: some View {
        Surface {
            HStack(spacing: 8) {
                Text("直连与内网规则")
                    .font(.headline)
                Spacer()
                if companyCount != nil {
                    CountBadge(count: directCount + (companyCount ?? 0))
                }
            }

            VStack(alignment: .leading, spacing: 10) {
                HStack(spacing: 12) {
                    Image(systemName: "globe.badge.chevron.backward")
                        .font(.system(size: 18))
                        .foregroundStyle(.secondary)
                        .frame(width: 32, height: 32)
                        .background(Color.primary.opacity(0.05), in: RoundedRectangle(cornerRadius: 8))

                    VStack(alignment: .leading, spacing: 3) {
                        Text("直连域名与 IP")
                            .font(.body.weight(.medium))
                        Text(directCount == 0 ? "未配置直连规则" : "\(directCount) 条规则 · 优先原网络")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    CountBadge(count: directCount)
                }

                if let companyCount {
                    Divider().padding(.vertical, 2)
                    HStack(spacing: 12) {
                        Image(systemName: "server.rack")
                            .font(.system(size: 18))
                            .foregroundStyle(.secondary)
                            .frame(width: 32, height: 32)
                            .background(Color.primary.opacity(0.05), in: RoundedRectangle(cornerRadius: 8))

                        VStack(alignment: .leading, spacing: 3) {
                            Text("公司内网网段")
                                .font(.body.weight(.medium))
                            Text("\(companyCount) 个网段 · AnyConnect 会话")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        Spacer()
                        CountBadge(count: companyCount)
                    }
                } else {
                    Text("直连规则始终优先于应用分流模式。可在当前线路中配置域名与 IP 例外。")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                        .padding(.vertical, 2)
                }

                HStack {
                    Spacer()
                    Button {
                        if store.profileDraft == nil {
                            store.beginEditingProfile(store.configuration.activeProfileID)
                        } else {
                            store.navigate(to: .profiles)
                        }
                    } label: {
                        HStack(spacing: 4) {
                            Text("配置规则")
                            Image(systemName: "arrow.right")
                        }
                        .font(.callout.weight(.medium))
                    }
                    .appActionStyle()
                    .accessibilityLabel("前往线路设置配置直连规则")
                }
            }
            .padding(.top, 12)
        }
    }
}

struct ConnectionBoundaryHelpView: View {
    let mode: RoutingMode

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 8) {
                Image(systemName: "shield.lefthalf.filled")
                    .foregroundStyle(AppTheme.accent)
                Text("连接边界与分流说明")
                    .font(.headline)
            }
            Divider()
            CompactInfoRow(
                title: "未列入名单的 App",
                detail: mode == .global ? "通过当前线路" : "保持原网络",
                symbol: mode == .global ? "network" : "arrow.turn.down.right",
                color: mode == .global ? AppTheme.accent : .secondary
            )
            CompactInfoRow(
                title: "直连规则",
                detail: "始终优先于 App 模式",
                symbol: "arrow.triangle.branch"
            )
            CompactInfoRow(
                title: "本地访问",
                detail: "局域网与 Tailscale 保留原路径",
                symbol: "checkmark.shield",
                color: AppTheme.success
            )
            Divider()
            Text("GoConnect 不会启动或重启目标 App。")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(16)
        .frame(width: 290)
    }
}

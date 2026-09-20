import SwiftUI
import GoConnectCore

struct RemoteNetworksEditor: View {
    @Binding var automatic: Bool
    @Binding var networks: [String]
    let subscription: Bool
    let readOnly: Bool
    @State private var address = ""
    @State private var error: String?
    var body: some View {
        Surface {
            Text("远端内网 · 系统连接").font(.headline)
            Text("以下公司网段与应用流量共用本线路的 AnyConnect 会话，适用于 Finder SMB、SSH 等。")
                .font(.callout).foregroundStyle(.secondary).padding(.top, 10)
            if !subscription {
                Text("请填写明确的公司网段").font(.callout).padding(.top, 14)
                Text("不再自动扩大到所有私网。与本地网络或 Tailscale 重叠时停止配置；域名规则和 App 名单不影响这些路由。")
                    .font(.caption).foregroundStyle(.secondary).padding(.top, 6)
            } else {
                Text("订阅线路不具备公司内网访问能力。")
                    .font(.caption).foregroundStyle(.secondary).padding(.top, 10)
            }
            if !readOnly {
                HStack {
                    TextField("10.20.0.10 或 10.20.0.0/16", text: $address).textFieldStyle(.roundedBorder)
                        .accessibilityLabel("远端内网地址").onSubmit(add)
                    Button("添加", systemImage: "plus", action: add).disabled(address.isEmpty)
                }.padding(.top, 16)
            }
            ForEach(networks, id: \.self) { network in
                HStack {
                    Label(network, systemImage: "server.rack").textSelection(.enabled)
                    Spacer()
                    if !readOnly { Button { networks.removeAll { $0 == network } } label: { Image(systemName: "trash") }.buttonStyle(.borderless).accessibilityLabel("移除远端内网 \(network)") }
                }.padding(.top, 10)
            }
            if let error { Text(error).font(.caption).foregroundStyle(.orange).padding(.top, 8) }
            Text("范围随线路保存，重连后应用。所有进程访问这些地址均遵循公司路由，不按 App 或登录用户区分；公网默认路由和系统 DNS 保持原状。")
                .font(.caption).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true).padding(.top, 12)
        }
    }
    private func add() {
        do { networks = try RemoteNetwork.validated(networks + [address]); address = ""; error = nil }
        catch { self.error = error.localizedDescription }
    }
}

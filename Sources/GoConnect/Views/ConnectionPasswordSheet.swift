import SwiftUI

struct ConnectionPasswordSheet: View {
    let store: AppStore
    @Environment(\.dismiss) private var dismiss
    @State private var password = ""
    @State private var remember = false
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text("连接“\(store.configuration.profile.name)”").font(.title2.weight(.semibold))
            Text("输入这条线路的 VPN 密码。").font(.callout).foregroundStyle(.secondary)
            SecureField("VPN 密码", text: $password).textFieldStyle(.roundedBorder).controlSize(.large).accessibilityLabel("连接密码")
            Toggle("保存到本机钥匙串，下次快速连接", isOn: $remember)
            HStack {
                Spacer()
                Button("取消") { dismiss() }.keyboardShortcut(.cancelAction)
                Button("连接") { store.submitPassword(password, remember: remember) }
                    .keyboardShortcut(.defaultAction).disabled(password.isEmpty)
            }
        }.padding(26).frame(width: 420).onAppear { remember = store.configuration.profile.rememberPassword }
    }
}

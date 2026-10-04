import SwiftUI

struct ConnectionPasswordSheet: View {
    let store: AppStore
    @Environment(\.dismiss) private var dismiss
    @State private var password = ""
    @State private var remember = false
    @FocusState private var passwordFocused: Bool
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text("连接“\(store.configuration.profile.name)”").font(.title2.weight(.semibold))
            Text("输入这条线路的 VPN 密码。").font(.callout).foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: 6) {
                LabeledContent("服务器", value: store.configuration.profile.server)
                    .lineLimit(1).truncationMode(.middle).help(store.configuration.profile.server)
                LabeledContent("用户名", value: store.configuration.profile.username)
            }.font(.callout).foregroundStyle(.secondary)
            SecureField("VPN 密码", text: $password).textFieldStyle(.roundedBorder).controlSize(.large).accessibilityLabel("连接密码")
                .focused($passwordFocused)
            Toggle("保存到本机钥匙串，下次快速连接", isOn: $remember)
            HStack {
                Spacer()
                Button("取消") { dismiss() }.appActionStyle().keyboardShortcut(.cancelAction)
                Button("连接") { store.submitPassword(password, remember: remember) }
                    .appActionStyle(primary: true).keyboardShortcut(.defaultAction).disabled(password.isEmpty)
            }
        }.padding(28).frame(width: 420).onAppear { remember = store.configuration.profile.rememberPassword; passwordFocused = true }
    }
}

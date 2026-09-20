import SwiftUI

struct SavedConnectionsView: View {
    @Bindable var store: AppStore
    var body: some View {
        Picker("已保存的线路", selection: Binding(get: { store.configuration.activeProfileID }, set: { store.selectProfile($0) })) {
            ForEach(store.configuration.connections) { connection in Text(connection.displayName).tag(connection.id) }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityLabel("已保存的线路").disabled(store.busy)
    }
}

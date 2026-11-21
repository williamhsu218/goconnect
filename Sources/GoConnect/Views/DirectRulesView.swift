import SwiftUI

struct DirectRulesView: View {
    let store: AppStore

    var body: some View {
        let connection = store.configuration.activeConnection
        VStack(alignment: .leading, spacing: 12) {
            VStack(alignment: .leading, spacing: 8) {
                PageHeading(title: "直连域名与 IP", subtitle: "当前线路 · \(connection.displayName)")
                if let reason = store.directRulesReadOnlyReason {
                    Label(reason, systemImage: "lock")
                        .font(.callout)
                        .foregroundStyle(.secondary)
                }
            }
            .padding(.horizontal, AppTheme.pagePadding)
            .padding(.top, AppTheme.pagePadding)

            Form {
                DomainRulesEditor(
                    readRules: { store.configuration.directDomains },
                    writeRules: { try store.saveDirectRules($0, for: connection.id) },
                    readOnly: !store.canEditActiveDirectRules,
                    savesImmediately: true
                )
                .id(connection.id)
            }
            .formStyle(.grouped)
            .scrollContentBackground(.hidden)
        }
        .frame(maxWidth: AppTheme.contentWidth)
        .frame(maxWidth: .infinity)
    }
}

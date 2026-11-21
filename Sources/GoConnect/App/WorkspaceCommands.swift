import SwiftUI

struct WorkspaceCommands: Commands {
    @Bindable var store: AppStore

    private var addsDirectApplication: Bool { store.page == .exclusions }

    var body: some Commands {
        CommandGroup(replacing: .newItem) {
            Button("新建线路") { store.beginNewProfile() }
                .keyboardShortcut("n")
                .disabled(!store.canCreateProfile || store.profileDraftHasChanges)
            Button("保存线路") { store.saveProfileDraft() }
                .keyboardShortcut("s")
                .disabled(!store.canSaveProfileDraft || !store.profileDraftHasChanges)
            Button(addsDirectApplication ? "添加直连应用…" : "添加代理应用…") {
                store.addApplication(excluded: addsDirectApplication)
            }
            .keyboardShortcut("o")
            .disabled(!store.canEditApplications || store.profileDraftHasChanges)
        }
    }
}

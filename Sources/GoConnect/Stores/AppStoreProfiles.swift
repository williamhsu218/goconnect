import AppKit
import GoConnectCore

extension AppStore {
    var profileDraftHasChanges: Bool { profileDraft?.hasChanges == true || !draftPassword.isEmpty }
    var canSaveProfileDraft: Bool {
        guard let profileDraft, configurationReadable else { return false }
        return (!busy || profileDraft.id != configuration.activeProfileID) && profileDraft.id != companyProtectedProfileID
    }

    var canCreateProfile: Bool { configurationReadable && configuration.connections.count < 64 }

    var directRulesReadOnlyReason: String? {
        if !configurationReadable { return "配置无法读取。请在诊断页查看配置文件。" }
        if updateOperationBlocked { return "正在准备安装更新，完成后可修改。" }
        if configuration.activeProfileID == companyProtectedProfileID { return "公司副线路使用独立配置，请在公司连接设置中管理。" }
        if busy { return "连接期间可查看，断开后可修改。" }
        return nil
    }

    var canEditActiveDirectRules: Bool {
        directRulesReadOnlyReason == nil
    }

    func saveDirectRules(_ rules: [DomainRule], for profileID: UUID) throws {
        guard configuration.activeProfileID == profileID else { throw ConfigurationError.profileChanged }
        guard canEditActiveDirectRules else { throw CocoaError(.fileWriteNoPermission) }
        let validated = try DomainRule.validated(rules)
        guard validated != configuration.directDomains else { return }

        var updated = configuration
        updated.directDomains = validated
        try file.save(updated)
        configuration = updated
    }

    func beginEditingProfile(_ id: UUID, preservingMessage: Bool = false) {
        guard profileDraft == nil, let connection = configuration.connections.first(where: { $0.id == id }) else { return }
        profileDraft = ConnectionDraft(editing: connection)
        draftPassword = ""
        profileEditorError = nil
        if !preservingMessage { message = nil }
        page = .profiles
    }

    func beginNewProfile(copying id: UUID? = nil) {
        guard canCreateProfile, !profileDraftHasChanges else { return }
        let source = id.flatMap { id in configuration.connections.first { $0.id == id } }
        profileDraft = ConnectionDraft(copying: source)
        draftPassword = ""; profileEditorError = nil; message = nil; page = .profiles
    }

    func navigate(to destination: Page) {
        guard destination != page else { return }
        if profileDraftHasChanges {
            pendingProfilePage = destination; discardProfileChangesRequested = true
        } else {
            closeProfileEditor(); page = destination
        }
    }

    func cancelProfileEditing() {
        if profileDraftHasChanges {
            pendingProfilePage = nil; discardProfileChangesRequested = true
        } else { closeProfileEditor() }
    }

    func discardProfileChanges() {
        let destination = pendingProfilePage
        closeProfileEditor()
        if let destination { page = destination }
    }

    func closeProfileEditor() {
        profileDraft = nil; draftPassword = ""; profileEditorError = nil
        pendingProfilePage = nil; discardProfileChangesRequested = false
    }

    func saveProfileDraft() {
        guard let draft = profileDraft, canSaveProfileDraft else { return }
        do {
            var updated = configuration
            try updated.save(draft)
            guard !draftPassword.contains("\n"), !draftPassword.contains("\r") else {
                profileEditorError = "密码不能包含换行。"; return
            }
            let profile = draft.connection.profile
            if profile.rememberPassword {
                if !draftPassword.isEmpty { try keychain.write(draftPassword, id: profile.id) }
            } else if draft.original?.profile.rememberPassword == true {
                try keychain.delete(profile.id)
            }
            try file.save(updated)
            configuration = updated
            if profile.id == configuration.activeProfileID { password = draftPassword }
            closeProfileEditor()
            show(draft.isNew ? "新线路已保存，可在连接页选择。" : "线路已保存。")
        } catch { profileEditorError = error.localizedDescription }
    }

    func deleteProfile(_ id: UUID) {
        guard configurationReadable, (!busy || id != configuration.activeProfileID),
              id != companyProtectedProfileID else { return }
        do {
            var updated = configuration
            try updated.remove(id)
            try file.save(updated)
            let wasActive = configuration.activeProfileID == id
            configuration = updated
            if profileDraft?.id == id { closeProfileEditor() }
            if wasActive {
                password = ""
                resetApplicationListFilters()
                phase = "idle"
            }
        } catch { show(error.localizedDescription, error: true); return }
        do { try keychain.delete(id); show("线路及保存的密码已删除。") }
        catch { show("线路已删除，但钥匙串项目未能移除。请在钥匙串访问中处理。", error: true) }
    }
}

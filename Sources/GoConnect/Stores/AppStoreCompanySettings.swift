import GoConnectCore

extension AppStore {
    /// Keep controls visible throughout authentication, teardown and quarantine.
    var showsCompanyConnection: Bool {
        companyConfiguration.enabled || company.isRunning || company.busy
    }

    var canSaveCompanyConnectionSettings: Bool {
        configurationReadable && companyConfigurationReadable && !busy &&
            !company.isRunning && !company.busy && profileDraft == nil && !updateOperationBlocked
    }

    var canEditCompanyConnectionSettings: Bool {
        canSaveCompanyConnectionSettings && !companyDraftOpen
    }

    func disableCompanyConnection() throws {
        guard canEditCompanyConnectionSettings else {
            throw CompanyConnectionError.invalid("请先结束连接、配置编辑或更新，再关闭公司副连接。")
        }
        var value = companyConfiguration
        value.enabled = false
        try companyFile.save(value)
        companyConfiguration = value
    }
}

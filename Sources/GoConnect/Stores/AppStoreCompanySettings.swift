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

    var companyConnectionSettingsReadOnlyReason: String? {
        if !configurationReadable || !companyConfigurationReadable {
            return "配置无法读取，请在诊断页查看配置文件。"
        }
        if updateOperationBlocked {
            return "正在准备安装更新，完成后可调整。"
        }
        if busy || company.isRunning || company.busy {
            return "请先断开主、副连接，再调整此功能。"
        }
        if profileDraft != nil {
            return "请先完成或关闭线路配置编辑，再调整此功能。"
        }
        if companyDraftOpen {
            return "公司副连接设置窗口已打开。"
        }
        return nil
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

import GoConnectCore

extension AppStore {
    var menuBarControlPolicy: MenuBarControlPolicy {
        MenuBarControlPolicy(state: connectionControlState, busy: busy,
                             configurationReadable: configurationReadable,
                             editingProfile: profileDraft != nil, testingNodes: nodeLatency.isRunning)
    }

    var connectionControlState: VPNControlState {
        VPNControlState(phase: phase, passwordRequested: passwordRequested,
                        testOnly: passwordRequested ? pendingPasswordTest : testOnly,
                        routingCheck: routingCheckRunning,
                        hasUnmanagedApps: false)
    }
    var connectionControlSubtitle: String {
        switch connectionControlState {
        case .off:
            accessExplanation
        case .waitingForPassword: "请在弹窗中输入这条线路的 VPN 密码。"
        case .disconnecting: "正在结束连接，请稍候。"
        case .diagnostic: "正在准备或运行诊断；结束后可开启 VPN。"
        case .endingDiagnostic: "正在释放诊断资源，请稍候。"
        default: statusSubtitle
        }
    }
    func setConnectionEnabled(_ enabled: Bool) {
        guard connectionControlState.canToggle else { return }
        if enabled {
            guard !connectionControlState.isOn else { return }
            connect()
        } else {
            guard connectionControlState.isOn else { return }
            disconnect()
        }
    }
}

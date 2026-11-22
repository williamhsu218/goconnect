import AppKit
import Foundation
import Observation
import ServiceManagement
import GoConnectCore

enum LoginItemState: Equatable {
    case enabled
    case disabled
    case requiresApproval
    case unavailable(String)
}

@MainActor
protocol LoginItemManaging {
    func currentState() -> LoginItemState
    func register() throws
    func unregister() throws
    func openSystemSettings()
}

@MainActor
struct SystemLoginItemManager: LoginItemManaging {
    private let bundle: Bundle

    init(bundle: Bundle = .main) {
        self.bundle = bundle
    }

    private var unavailableReason: String? {
        let isUpdateQA = bundle.bundleIdentifier == "com.willhsu.GoConnect.UpdateQA"
            && bundle.object(forInfoDictionaryKey: "GoConnectUpdateQAMode") as? Bool == true
        if isUpdateQA || Product.isIsolatedTestApp {
            return "测试或隔离验收构建不支持登录项管理。"
        }
        guard bundle.bundleURL.pathExtension == "app" else {
            return "仅打包后的 GoConnect.app 支持登录时自动启动。"
        }
        return nil
    }

    func currentState() -> LoginItemState {
        if let reason = unavailableReason {
            return .unavailable(reason)
        }
        switch SMAppService.mainApp.status {
        case .enabled:
            return .enabled
        case .requiresApproval:
            return .requiresApproval
        case .notRegistered, .notFound:
            return .disabled
        @unknown default:
            return .disabled
        }
    }

    func register() throws {
        if let reason = unavailableReason {
            throw NSError(domain: Product.bundleID, code: 1, userInfo: [NSLocalizedDescriptionKey: reason])
        }
        try SMAppService.mainApp.register()
    }

    func unregister() throws {
        if let reason = unavailableReason {
            throw NSError(domain: Product.bundleID, code: 1, userInfo: [NSLocalizedDescriptionKey: reason])
        }
        try SMAppService.mainApp.unregister()
    }

    func openSystemSettings() {
        SMAppService.openSystemSettingsLoginItems()
    }
}

@MainActor @Observable
final class LoginItemStore {
    private let manager: any LoginItemManaging
    private(set) var state: LoginItemState
    private(set) var errorMessage: String?

    init(manager: (any LoginItemManaging)? = nil) {
        let resolved = manager ?? SystemLoginItemManager()
        self.manager = resolved
        self.state = resolved.currentState()
    }

    var isEnabled: Bool {
        state == .enabled || state == .requiresApproval
    }

    var requiresApproval: Bool {
        state == .requiresApproval
    }

    var unavailableReason: String? {
        if case .unavailable(let reason) = state { return reason }
        return nil
    }

    var canToggle: Bool {
        unavailableReason == nil
    }

    func refresh() {
        state = manager.currentState()
    }

    func setEnabled(_ enabled: Bool) {
        guard canToggle else { return }
        errorMessage = nil
        do {
            if enabled {
                try manager.register()
            } else {
                try manager.unregister()
            }
        } catch {
            errorMessage = error.localizedDescription
        }
        state = manager.currentState()
    }

    func openSystemSettings() {
        manager.openSystemSettings()
    }
}

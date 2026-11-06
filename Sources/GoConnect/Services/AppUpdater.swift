import AppKit
import Combine
import GoConnectCore
import Sparkle

/// The app owns this instance for its entire lifetime. It does not control VPN teardown.
@MainActor
final class AppUpdater: NSObject, ObservableObject, SPUUpdaterDelegate {
    @Published private(set) var isUpdateCycleActive = false
    @Published private(set) var installationInProgress = false
    @Published private(set) var hasDeferredInstallation = false
    @Published private(set) var hasUnconfirmedInstallation = false
    @Published private(set) var statusDescription = "此构建未启用在线更新。"
    @Published private var sparkleCanCheck = false
    @Published private var automaticChecks = false

    let isConfigured: Bool
    private let bundle: Bundle
    private let defaults: UserDefaults
    private let configuration: AppUpdateConfiguration?
    private let safetyState: @MainActor () -> UpdateSafetyState
    private let notify: @MainActor (String) -> Void
    private var sparkle: SPUUpdater?
    private var observations: [NSKeyValueObservation] = []
    private var deferredInstallHandler: (() -> Void)?
    private var installationPrepared = false
    private let pendingBuildKey = "GoConnectPendingUpdateBuild"

    init(bundle: Bundle = .main, defaults: UserDefaults = .standard,
         safetyState: @escaping @MainActor () -> UpdateSafetyState,
         notify: @escaping @MainActor (String) -> Void) {
        self.bundle = bundle
        self.defaults = defaults
        self.safetyState = safetyState
        self.notify = notify
        configuration = AppUpdateConfiguration(bundleIdentifier: bundle.bundleIdentifier, info: bundle.infoDictionary ?? [:])
        isConfigured = configuration != nil && bundle.bundleURL.pathExtension == "app"
        super.init()
        let recoveryHost = bundle.bundleIdentifier == "com.willhsu.GoConnect" ||
            (bundle.bundleIdentifier == "com.willhsu.GoConnect.UpdateQA" && bundle.object(forInfoDictionaryKey: "GoConnectUpdateQAMode") as? Bool == true)
        guard recoveryHost, bundle.bundleURL.pathExtension == "app" else { return }
        if isConfigured { statusDescription = "通过 GitHub 检查更新，安装前需手动断开 VPN。" }
        if let build = defaults.string(forKey: pendingBuildKey) {
            if build == bundle.object(forInfoDictionaryKey: "CFBundleVersion") as? String {
                // A marker does not prove an installer survived this cold launch.
                // Keep quit protection, but never lock networking indefinitely.
                hasUnconfirmedInstallation = true
                statusDescription = "上次更新状态尚待确认，可重新检查；退出前请手动断开 VPN。"
            } else {
                defaults.removeObject(forKey: pendingBuildKey)
            }
        } else if isConfigured {
            statusDescription = "通过 GitHub 检查更新，安装前需手动断开 VPN。"
        }
    }

    var blocksNewConnection: Bool { isUpdateCycleActive || installationInProgress }
    var canCheckForUpdates: Bool { isConfigured && sparkleCanCheck && safetyState().blockingReason == nil }
    var automaticallyChecksForUpdates: Bool {
        get { automaticChecks }
        set {
            guard let sparkle else { return }
            // Sparkle persists this user choice in its own defaults.
            sparkle.automaticallyChecksForUpdates = newValue
            automaticChecks = newValue
        }
    }

    func start() {
        guard isConfigured, sparkle == nil else { return }
        let driver = SPUStandardUserDriver(hostBundle: bundle, delegate: nil)
        let updater = SPUUpdater(hostBundle: bundle, applicationBundle: bundle, userDriver: driver, delegate: self)
        sparkle = updater
        observations = [
            updater.observe(\.canCheckForUpdates, options: [.initial, .new]) { [weak self] updater, _ in
                Task { @MainActor [weak self] in self?.sparkleCanCheck = updater.canCheckForUpdates }
            },
            updater.observe(\.automaticallyChecksForUpdates, options: [.initial, .new]) { [weak self] updater, _ in
                Task { @MainActor [weak self] in self?.automaticChecks = updater.automaticallyChecksForUpdates }
            },
            updater.observe(\.sessionInProgress, options: [.new]) { [weak self] updater, _ in
                Task { @MainActor [weak self] in
                    // A resumed installer can begin without a fresh feed download.
                    if updater.sessionInProgress { self?.isUpdateCycleActive = true }
                }
            }
        ]
        do {
            try updater.start()
        } catch {
            observations.removeAll()
            sparkle = nil
            sparkleCanCheck = false
            statusDescription = "在线更新暂不可用，请使用 GitHub Releases 安装包。"
        }
    }

    func checkForUpdates() {
        guard isConfigured, let sparkle else { notify(statusDescription); return }
        guard allowSafeOperation() else { return }
        guard sparkle.canCheckForUpdates else { return }
        isUpdateCycleActive = true
        statusDescription = "正在检查更新…"
        sparkle.checkForUpdates()
    }

    /// Wire this into AppDelegate.applicationShouldTerminate before any VPN cleanup.
    /// Sparkle's installer sends a normal quit event; the app may cancel that request.
    func canTerminateForUpdate() -> Bool {
        guard blocksNewConnection || hasUnconfirmedInstallation else { return true }
        return allowSafeOperation()
    }

    /// A blocked update is resumed only by a new, explicit user action, never a timer.
    func resumeDeferredInstallation() {
        guard allowSafeOperation() else { return }
        guard let install = deferredInstallHandler else { checkForUpdates(); return }
        hasDeferredInstallation = false
        deferredInstallHandler = nil
        install()
    }

    private func allowSafeOperation() -> Bool {
        guard let reason = safetyState().blockingReason else { return true }
        statusDescription = reason
        notify(reason)
        return false
    }

    private func recordInstallationPending(prepared: Bool) {
        installationInProgress = true
        installationPrepared = installationPrepared || prepared
        if let build = bundle.object(forInfoDictionaryKey: "CFBundleVersion") as? String {
            defaults.set(build, forKey: pendingBuildKey)
        }
    }

    private func clearInstallationPending() {
        installationInProgress = false
        installationPrepared = false
        hasDeferredInstallation = false
        deferredInstallHandler = nil
        // Only a new host build conclusively retires a recovered external
        // installer marker. A failed feed request is not that proof.
        if !hasUnconfirmedInstallation { defaults.removeObject(forKey: pendingBuildKey) }
    }

    func feedURLString(for updater: SPUUpdater) -> String? { configuration?.feedURL.absoluteString }
    func allowedChannels(for updater: SPUUpdater) -> Set<String> { configuration?.allowedChannels ?? [] }
    func allowedSystemProfileKeys(for updater: SPUUpdater) -> [String]? { [] }

    func updater(_ updater: SPUUpdater, mayPerform updateCheck: SPUUpdateCheck) throws {
        if let reason = safetyState().blockingReason {
            throw NSError(domain: "GoConnect.Update", code: 1, userInfo: [NSLocalizedDescriptionKey: reason])
        }
        isUpdateCycleActive = true
    }

    func updater(_ updater: SPUUpdater, shouldProceedWithUpdate updateItem: SUAppcastItem, updateCheck: SPUUpdateCheck) throws {
        if let reason = safetyState().blockingReason {
            throw NSError(domain: "GoConnect.Update", code: 1, userInfo: [NSLocalizedDescriptionKey: reason])
        }
    }

    func updater(_ updater: SPUUpdater, userDidMake choice: SPUUserUpdateChoice, forUpdate updateItem: SUAppcastItem, state: SPUUserUpdateState) {
        switch choice {
        case .install: recordInstallationPending(prepared: state.stage == .installing)
        case .skip: clearInstallationPending()
        case .dismiss:
            if state.stage == .installing { recordInstallationPending(prepared: true) }
            else { clearInstallationPending() }
        @unknown default: break
        }
    }

    func updaterShouldRelaunchApplication(_ updater: SPUUpdater) -> Bool {
        recordInstallationPending(prepared: true)
        return allowSafeOperation()
    }

    func updater(_ updater: SPUUpdater, shouldPostponeRelaunchForUpdate item: SUAppcastItem, untilInvokingBlock installHandler: @escaping () -> Void) -> Bool {
        recordInstallationPending(prepared: true)
        guard safetyState().blockingReason != nil else { return false }
        deferredInstallHandler = installHandler
        hasDeferredInstallation = true
        _ = allowSafeOperation()
        return true
    }

    func updater(_ updater: SPUUpdater, willInstallUpdateOnQuit item: SUAppcastItem, immediateInstallationBlock immediateInstallHandler: @escaping () -> Void) -> Bool {
        // Defensive handling of a recovered installer. This build forbids automatic downloads.
        recordInstallationPending(prepared: true)
        deferredInstallHandler = immediateInstallHandler
        hasDeferredInstallation = true
        statusDescription = "更新已准备就绪，手动断开 VPN 并保存线路后可继续安装。"
        return true
    }

    func updater(_ updater: SPUUpdater, willInstallUpdate item: SUAppcastItem) {
        recordInstallationPending(prepared: true)
    }

    func updater(_ updater: SPUUpdater, didFinishUpdateCycleFor updateCheck: SPUUpdateCheck, error: Error?) {
        isUpdateCycleActive = false
        // A prepared installer can survive an update cycle and install on quit.
        if !installationPrepared { clearInstallationPending() }
        if error != nil { statusDescription = "本次更新检查未完成，可从“检查更新”重试。" }
        else if !installationInProgress { statusDescription = "可从“检查更新”查看 GitHub 上的可用版本。" }
    }
}

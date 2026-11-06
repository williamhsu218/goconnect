import AppKit
import Combine
import GoConnectCore
import Sparkle

/// Downloads may use the active VPN. Only explicit installation owns the teardown gate.
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
    private let prepareForInstallation: (@MainActor () async -> Bool)?
    private let notify: @MainActor (String) -> Void
    private var sparkle: SPUUpdater?
    private var observations: [NSKeyValueObservation] = []
    private var deferredInstallHandler: (() -> Void)?
    private var installationPrepared = false
    private var preparationTask: Task<Void, Never>?
    private var installationAttempt: UUID?
    private let pendingBuildKey = "GoConnectPendingUpdateBuild"

    init(bundle: Bundle = .main, defaults: UserDefaults = .standard,
         safetyState: @escaping @MainActor () -> UpdateSafetyState,
         prepareForInstallation: (@MainActor () async -> Bool)? = nil,
         notify: @escaping @MainActor (String) -> Void) {
        self.bundle = bundle
        self.defaults = defaults
        self.safetyState = safetyState
        self.prepareForInstallation = prepareForInstallation
        self.notify = notify
        configuration = AppUpdateConfiguration(bundleIdentifier: bundle.bundleIdentifier, info: bundle.infoDictionary ?? [:])
        isConfigured = configuration != nil && bundle.bundleURL.pathExtension == "app"
        super.init()
        let recoveryHost = bundle.bundleIdentifier == "com.willhsu.GoConnect" ||
            (bundle.bundleIdentifier == "com.willhsu.GoConnect.UpdateQA" && bundle.object(forInfoDictionaryKey: "GoConnectUpdateQAMode") as? Bool == true)
        guard recoveryHost, bundle.bundleURL.pathExtension == "app" else { return }
        if isConfigured { statusDescription = "可通过当前 VPN 检查和下载更新，确认安装后才断开并重启。" }
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
            statusDescription = "可通过当前 VPN 检查和下载更新，确认安装后才断开并重启。"
        }
    }

    var blocksNewConnection: Bool { installationInProgress }
    var canCheckForUpdates: Bool { isConfigured && sparkleCanCheck && !installationInProgress && !hasDeferredInstallation }
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
                    // Sparkle also toggles this while scheduling automatic checks.
                    // Both edges matter; scheduling is never an installation lock.
                    self?.synchronizeSessionState(updater.sessionInProgress)
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
        guard !installationInProgress, !hasDeferredInstallation else { return }
        guard sparkle.canCheckForUpdates else { return }
        isUpdateCycleActive = true
        statusDescription = "正在检查更新…"
        sparkle.checkForUpdates()
    }

    /// Wire this into AppDelegate.applicationShouldTerminate before any VPN cleanup.
    /// Sparkle's installer sends a normal quit event; the app may cancel that request.
    func canTerminateForUpdate() -> Bool {
        guard installationPrepared || installationInProgress || hasUnconfirmedInstallation else { return true }
        return allowSafeOperation()
    }

    func synchronizeSessionState(_ active: Bool) {
        isUpdateCycleActive = active
    }

    /// The continuation is available only after Sparkle downloaded and verified the update.
    /// Recovered install-on-quit work waits for an explicit button, never a timer.
    func resumeDeferredInstallation() {
        guard !installationInProgress, let install = deferredInstallHandler else { return }
        if let reason = safetyState().preparationBlockingReason {
            statusDescription = reason
            notify(reason)
            return
        }
        let attempt = UUID()
        installationAttempt = attempt
        installationInProgress = true
        statusDescription = "更新已下载并验签，正在正常断开 VPN 后安装重启…"
        preparationTask = Task { @MainActor [weak self] in
            guard let self else { return }
            let prepared: Bool
            if let prepareForInstallation { prepared = await prepareForInstallation() }
            else { prepared = safetyState().blockingReason == nil }
            guard !Task.isCancelled, installationAttempt == attempt else { return }
            preparationTask = nil
            guard prepared, allowSafeOperation() else {
                installationInProgress = false
                statusDescription = safetyState().blockingReason ?? "网络服务尚未安全停止，安装已暂停，可稍后重试。"
                return
            }
            hasDeferredInstallation = false
            deferredInstallHandler = nil
            install()
        }
    }

    private func allowSafeOperation() -> Bool {
        guard let reason = safetyState().blockingReason else { return true }
        statusDescription = reason
        notify(reason)
        return false
    }

    private func recordInstallationPrepared() {
        installationPrepared = true
        if let build = bundle.object(forInfoDictionaryKey: "CFBundleVersion") as? String {
            defaults.set(build, forKey: pendingBuildKey)
        }
    }

    private func clearInstallationPending() {
        preparationTask?.cancel()
        preparationTask = nil
        installationAttempt = nil
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
        isUpdateCycleActive = true
    }

    func updater(_ updater: SPUUpdater, shouldProceedWithUpdate updateItem: SUAppcastItem, updateCheck: SPUUpdateCheck) throws {
        // The signed update may be checked and downloaded over an active VPN.
    }

    func updater(_ updater: SPUUpdater, userDidMake choice: SPUUserUpdateChoice, forUpdate updateItem: SUAppcastItem, state: SPUUserUpdateState) {
        switch choice {
        case .install: break // This choice can mean "download", not "install and relaunch".
        case .skip: clearInstallationPending()
        case .dismiss:
            if state.stage == .installing { recordInstallationPrepared() }
            else { clearInstallationPending() }
        @unknown default: break
        }
    }

    func updaterShouldRelaunchApplication(_ updater: SPUUpdater) -> Bool {
        // Sparkle asks a policy question here. Mutating state can lock a new host.
        // Delay actual installation via the continuation below, not by returning false.
        return true
    }

    func updater(_ updater: SPUUpdater, shouldPostponeRelaunchForUpdate item: SUAppcastItem, untilInvokingBlock installHandler: @escaping () -> Void) -> Bool {
        recordInstallationPrepared()
        deferredInstallHandler = installHandler
        hasDeferredInstallation = true
        // Automatic installation is disabled; this is the user's install/relaunch action.
        // Always postpone for the asynchronous normal teardown and a final safety check.
        resumeDeferredInstallation()
        return true
    }

    func updater(_ updater: SPUUpdater, willInstallUpdateOnQuit item: SUAppcastItem, immediateInstallationBlock immediateInstallHandler: @escaping () -> Void) -> Bool {
        // Defensive handling of a recovered installer. This build forbids automatic downloads.
        recordInstallationPrepared()
        deferredInstallHandler = immediateInstallHandler
        hasDeferredInstallation = true
        statusDescription = "更新已准备就绪，可保持 VPN 连接；确认安装重启时才正常断开。"
        return true
    }

    func updater(_ updater: SPUUpdater, willInstallUpdate item: SUAppcastItem) {
        recordInstallationPrepared()
    }

    func updater(_ updater: SPUUpdater, didFinishUpdateCycleFor updateCheck: SPUUpdateCheck, error: Error?) {
        isUpdateCycleActive = false
        // An abort releases the live connection gate. A surviving installer marker
        // still protects quit, but must never leave the app unable to reconnect.
        if error != nil {
            if installationPrepared { hasUnconfirmedInstallation = true }
            clearInstallationPending()
        } else if !installationPrepared { clearInstallationPending() }
        let noUpdate = (error as NSError?).map {
            $0.domain == SUSparkleErrorDomain && $0.code == SUError.noUpdateError.rawValue
        } ?? false
        if noUpdate { statusDescription = "当前已是最新版本，可保持 VPN 连接。" }
        else if error != nil { statusDescription = "本次更新检查未完成，可从“检查更新”重试。" }
        else if !installationInProgress { statusDescription = "可从“检查更新”查看 GitHub 上的可用版本。" }
    }
}

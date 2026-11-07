import Foundation
import GoConnectCore
import Observation

/// Company transport and root-route operations. There is deliberately no primary-connection entry point.
@MainActor
struct CompanyConnectionActions {
    var start: @MainActor (VPNProfile, String, String, UUID) throws -> Void
    var prepare: @MainActor (CompanyBackendClaim, UUID) throws -> Void
    var block: @MainActor (UUID) throws -> Void
    var stop: @MainActor () -> Void
}

@MainActor
@Observable
final class CompanyConnectionStore {
    private(set) var phase = "idle"
    private(set) var message: String?
    private(set) var passwordRequested = false
    var password = ""
    private(set) var received: UInt64 = 0
    private(set) var sent: UInt64 = 0
    private(set) var downloadRate = 0.0
    private(set) var uploadRate = 0.0
    private(set) var activeProfileID: UUID?
    private(set) var generation = UUID()
    /// Outlives `generation` invalidation so that the owning transport's stopped event can still finish cleanup.
    private var transportGeneration: UUID?

    private let actions: CompanyConnectionActions
    private let ackTimeoutTicks: Int
    private let now: @MainActor () -> TimeInterval
    @ObservationIgnored private var meter = SessionTrafficMeter()
    @ObservationIgnored private var pendingProfile: VPNProfile?
    @ObservationIgnored private var companyToken: String?
    @ObservationIgnored private var prepareDispatched = false
    @ObservationIgnored private var blockRequested = false
    @ObservationIgnored private var routeBlocked = false
    @ObservationIgnored private var stopRequested = false
    @ObservationIgnored private var controllerGone = false
    @ObservationIgnored private var failureMessage: String?
    @ObservationIgnored private var closingMessage: String? = "已断开。"
    @ObservationIgnored private var ackTicks = 0
    @ObservationIgnored private var lastRevision: UInt64 = 0

    /// `tickTimeout()` is expected once per second, so the default bound is 15 seconds.
    init(actions: CompanyConnectionActions, ackTimeoutTicks: Int = 15, now: @escaping @MainActor () -> TimeInterval = { ProcessInfo.processInfo.systemUptime }) {
        self.actions = actions; self.ackTimeoutTicks = ackTimeoutTicks; self.now = now
    }

    var busy: Bool { passwordRequested || tearingDown }
    var isRunning: Bool { transportGeneration != nil || ["authenticating", "preparing", "connected", "stopping", "quarantined"].contains(phase) }
    private var tearingDown: Bool { phase == "stopping" || phase == "quarantined" }
    private var routeAckPending: Bool { prepareDispatched && !routeBlocked && !controllerGone }

    func connect(profile: VPNProfile, primaryReady: Bool, controllerReserved: Bool, updateBlocked: Bool, secret: String? = nil) {
        guard !busy, !isRunning else { return }
        guard primaryReady else { message = "主线路尚未就绪，无法连接公司线路。"; return }
        guard controllerReserved else { message = "公司路由控制尚未就绪，无法连接公司线路。"; return }
        guard !updateBlocked else { message = "正在更新，暂时无法连接公司线路。"; return }
        clearAttempt()
        generation = UUID(); activeProfileID = profile.id
        received = 0; sent = 0; downloadRate = 0; uploadRate = 0
        phase = "authenticating"; message = nil
        guard let secret, !secret.isEmpty else {
            pendingProfile = profile; passwordRequested = true
            return
        }
        dispatch(profile, secret: secret)
    }

    func submitPassword() {
        guard passwordRequested, let profile = pendingProfile else { return }
        let secret = password
        guard !secret.isEmpty else { message = "请输入公司线路密码。"; return }
        password = ""; passwordRequested = false; pendingProfile = nil
        dispatch(profile, secret: secret)
    }

    func cancelAuthentication() {
        password = ""
        if passwordRequested {
            passwordRequested = false; pendingProfile = nil; closingMessage = nil
            finish()
        } else if phase == "authenticating", transportGeneration != nil {
            beginTeardown(failure: nil)
        }
    }

    func disconnect() {
        if passwordRequested { cancelAuthentication(); return }
        guard transportGeneration != nil, !tearingDown else { return }
        beginTeardown(failure: nil)
    }

    /// The primary connection owns the root controller, so its exit removes the need for a blocked acknowledgement.
    func primaryStopped() {
        guard isRunning else { return }
        controllerGone = true; closingMessage = "主线路已停止，公司连接已结束。"
        passwordRequested = false; password = ""; pendingProfile = nil
        guard transportGeneration != nil else { finish(); return }
        generation = UUID()
        if phase != "quarantined" {
            phase = "stopping"; message = "主线路已停止，正在结束公司连接…"; ackTicks = 0
        }
        requestStop()
    }

    func handle(_ event: TransportEvent, generation eventGeneration: UUID) {
        guard eventGeneration == transportGeneration else { return }
        if event.event == "stopped" { transportStopped(); return }
        guard eventGeneration == generation else { return }
        switch event.event {
        case "ready": ready(event)
        case "stats":
            guard phase == "preparing" || phase == "connected", let received = event.received, let sent = event.sent else { return }
            let rates = meter.sample(received: received, sent: sent, time: now())
            self.received = received; self.sent = sent
            downloadRate = rates.download; uploadRate = rates.upload
        case "stats_unavailable":
            meter.reset(); downloadRate = 0; uploadRate = 0
        case "quarantined":
            quarantine("公司传输未正常退出，已隔离公司连接。")
        case "error":
            guard !tearingDown else { return }
            beginTeardown(failure: event.message ?? "公司连接失败。")
        default: break
        }
    }

    func routeState(_ state: CompanyRouteState) {
        guard prepareDispatched, UUID(uuidString: state.generation) == generation, state.revision >= lastRevision else { return }
        lastRevision = state.revision
        switch state.state {
        case "active":
            if phase == "preparing" {
                phase = "connected"; message = nil; ackTicks = 0
            } else if tearingDown {
                routeBlocked = false
            }
        case "failed":
            if !tearingDown { beginTeardown(failure: state.message ?? "公司路由验证失败。") }
        case "blocked":
            if blockRequested {
                routeBlocked = true; ackTicks = 0
                requestStop()
                finishIfSafe()
            } else if phase == "connected" {
                routeBlocked = true
                beginTeardown(failure: state.message ?? "公司路由已被阻断。")
            }
        default: break
        }
    }

    /// Bounds the wait for route activation, the blocked acknowledgement and the transport's stopped event.
    func tickTimeout() {
        guard phase == "preparing" || phase == "stopping" else { return }
        ackTicks += 1
        guard ackTicks >= ackTimeoutTicks else { return }
        quarantine("公司路由或传输未在限定时间内确认，已隔离公司连接。")
    }

    private func dispatch(_ profile: VPNProfile, secret: String) {
        let token = UUID().uuidString + UUID().uuidString
        companyToken = token; transportGeneration = generation
        do {
            try actions.start(profile, secret, token, generation)
        } catch {
            failureMessage = error.localizedDescription
            finish()
        }
    }

    private func ready(_ event: TransportEvent) {
        guard phase == "authenticating", let token = companyToken else { return }
        guard let socket = event.companySocket, !socket.isEmpty, let pid = event.companyPID, pid > 0, let startTime = event.companyStartTime else {
            beginTeardown(failure: "公司后端身份信息不完整，已停止连接。")
            return
        }
        companyToken = nil; prepareDispatched = true
        phase = "preparing"; message = nil; ackTicks = 0
        let claim = CompanyBackendClaim(socketPath: socket, pid: pid, startTime: startTime, token: token, dnsServers: event.dns ?? [], remoteNetworks: event.remoteNetworks ?? [], remoteExcluded: event.remoteExcluded ?? [])
        do {
            try actions.prepare(claim, generation)
        } catch {
            quarantine("公司路由准备失败：\(error.localizedDescription)")
        }
    }

    private func beginTeardown(failure: String?) {
        failureMessage = failure
        phase = "stopping"; message = failure ?? "正在断开公司连接…"; ackTicks = 0
        if routeAckPending { requestBlock() } else { requestStop() }
    }

    private func transportStopped() {
        transportGeneration = nil
        if !tearingDown {
            failureMessage = "公司连接意外断开。"
            phase = "stopping"; message = failureMessage; ackTicks = 0
        }
        if routeAckPending { requestBlock() }
        finishIfSafe()
    }

    private func requestBlock() {
        guard !blockRequested else { return }
        blockRequested = true
        do {
            try actions.block(generation)
        } catch {
            quarantine("公司路由阻断失败：\(error.localizedDescription)")
        }
    }

    private func requestStop() {
        guard transportGeneration != nil, !stopRequested else { return }
        stopRequested = true
        actions.stop()
    }

    private func quarantine(_ reason: String) {
        failureMessage = reason
        phase = "quarantined"; message = reason; ackTicks = 0
        if routeAckPending, !blockRequested {
            blockRequested = true
            try? actions.block(generation)
        }
        requestStop()
    }

    private func finishIfSafe() {
        guard tearingDown, transportGeneration == nil, !routeAckPending else { return }
        finish()
    }

    private func finish() {
        let failure = failureMessage, closing = closingMessage
        phase = failure == nil ? "idle" : "failed"; message = failure ?? closing
        transportGeneration = nil; activeProfileID = nil; generation = UUID()
        received = 0; sent = 0; downloadRate = 0; uploadRate = 0
        clearAttempt()
    }

    private func clearAttempt() {
        meter.reset(); pendingProfile = nil; companyToken = nil; password = ""
        prepareDispatched = false; blockRequested = false; routeBlocked = false
        stopRequested = false; controllerGone = false
        failureMessage = nil; closingMessage = "已断开。"; ackTicks = 0; lastRevision = 0
    }
}

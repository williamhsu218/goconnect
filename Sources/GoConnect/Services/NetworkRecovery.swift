import Foundation
import Network

struct RecoveryNetworkPath: Decodable, Equatable, Sendable {
    let interface: String
    let gateway: String
}

/// The old worker must finish unwinding before this observes the new default path.
@MainActor
enum NetworkRecovery {
    enum Attempt: Equatable { case connected, retry, stop }

    static func retryOriginalLine(until deadline: ContinuousClock.Instant,
                                  delays: [Duration] = [.zero, .seconds(2), .seconds(4)],
                                  waitForPath: (ContinuousClock.Instant) async -> Bool,
                                  attempt: () async -> Attempt) async -> Bool {
        let clock = ContinuousClock()
        for delay in delays {
            guard !Task.isCancelled, clock.now < deadline else { return false }
            do { try await clock.sleep(until: min(deadline, clock.now.advanced(by: delay))) }
            catch { return false }
            guard clock.now < deadline, await waitForPath(deadline),
                  !Task.isCancelled, clock.now < deadline else { return false }
            switch await attempt() {
            case .connected: return !Task.isCancelled
            case .retry: continue
            case .stop: return false
            }
        }
        return false
    }

    static func waitForPhysicalPath(until deadline: ContinuousClock.Instant, runtime: URL) async -> Bool {
        let monitor = NWPathMonitor()
        monitor.start(queue: DispatchQueue(label: "com.willhsu.GoConnect.network-recovery"))
        defer { monitor.cancel() }
        let executable = runtime.appendingPathComponent("bin/GoConnectTransport")
        return await waitForStablePath(until: deadline) { remaining in
            guard monitor.currentPath.status == .satisfied else { return nil }
            let route = await NetworkServiceClient.readRecoveryPath(executable, timeout: min(remaining, .seconds(2)))
            return monitor.currentPath.status == .satisfied ? route : nil
        }
    }

    static func waitForStablePath(until deadline: ContinuousClock.Instant,
                                  stableFor: Duration = .seconds(2), interval: Duration = .milliseconds(500),
                                  readPath: (Duration) async -> RecoveryNetworkPath?) async -> Bool {
        let clock = ContinuousClock()
        var previous: RecoveryNetworkPath?
        var stableSince = clock.now
        while !Task.isCancelled, clock.now < deadline {
            let path = await readPath(clock.now.duration(to: deadline))
            let now = clock.now
            guard !Task.isCancelled, now < deadline else { return false }
            if path != previous {
                previous = path
                stableSince = now
            } else if path != nil, stableSince.duration(to: now) >= stableFor {
                return true
            }
            do { try await clock.sleep(until: min(deadline, now.advanced(by: interval))) }
            catch { return false }
        }
        return false
    }
}

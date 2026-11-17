import Foundation
import Network

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

    static func waitForPhysicalPath(until deadline: ContinuousClock.Instant) async -> Bool {
        let clock = ContinuousClock()
        let monitor = NWPathMonitor()
        monitor.start(queue: DispatchQueue(label: "com.willhsu.GoConnect.network-recovery"))
        defer { monitor.cancel() }
        var previous: NWPath?
        var stableSince = clock.now
        while !Task.isCancelled, clock.now < deadline {
            let path = monitor.currentPath
            if path.status == .satisfied && path.availableInterfaces.contains(where: { $0.name.range(of: #"^(en|bridge|bond|vnic|vmnet)[0-9]{1,5}$"#, options: .regularExpression) != nil }) {
                if path != previous {
                    previous = path
                    stableSince = clock.now
                } else if stableSince.duration(to: clock.now) >= .seconds(2) {
                    return true
                }
            } else { previous = nil }
            do { try await clock.sleep(until: min(deadline, clock.now.advanced(by: .milliseconds(500)))) }
            catch { return false }
        }
        return false
    }
}

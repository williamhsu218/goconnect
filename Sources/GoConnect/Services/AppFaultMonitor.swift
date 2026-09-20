import AppKit
import Network
import GoConnectCore

final class AppFaultMonitor: @unchecked Sendable {
    static let shared = AppFaultMonitor()
    private let queue = DispatchQueue(label: "com.willhsu.GoConnect.responsiveness", qos: .utility)
    private let diagnostics: FaultDiagnostics
    init(diagnostics: FaultDiagnostics = .shared) { self.diagnostics = diagnostics }
    private var pressure: DispatchSourceMemoryPressure?
    private var timer: DispatchSourceTimer?
    private let path = NWPathMonitor()
    private var state = ResponsivenessState()
    private var lastPath: NWPath.Status?
    private var lastHeartbeat = 0.0
    private var sleeping = false
    private var observers: [NSObjectProtocol] = []
    func start() {
        diagnostics.record(.appStarted, value: 805)
        let center = NSWorkspace.shared.notificationCenter
        for (name, event) in [(NSWorkspace.willSleepNotification, FaultEvent.sleep), (NSWorkspace.didWakeNotification, .wake)] {
            observers.append(center.addObserver(forName: name, object: nil, queue: nil) { [weak self] _ in
                self?.queue.async { [weak self] in
                    self?.sleeping = event == .sleep
                    if event == .wake { self?.state.resume(now: ProcessInfo.processInfo.systemUptime) }
                    // Existing main probe remains outstanding across sleep.
                    self?.diagnostics.record(event)
                }
            })
        }
        path.pathUpdateHandler = { [weak self] current in
            guard let self, current.status != lastPath else { return }; lastPath = current.status
            let event: FaultEvent = current.status == .satisfied ? .pathSatisfied : current.status == .unsatisfied ? .pathUnsatisfied : .pathRequiresConnection
            self.diagnostics.record(event)
        }
        path.start(queue: queue)
        let pressure = DispatchSource.makeMemoryPressureSource(eventMask: [.normal, .warning, .critical], queue: queue)
        pressure.setEventHandler { [weak self] in
            guard let pressure = self?.pressure else { return }
            self?.diagnostics.record(.memoryPressure, value: pressure.data.contains(.critical) ? 2 : pressure.data.contains(.warning) ? 1 : 0)
        }
        self.pressure = pressure; pressure.resume()
        let timer = DispatchSource.makeTimerSource(queue: queue)
        timer.schedule(deadline: .now(), repeating: 1, leeway: .milliseconds(200))
        timer.setEventHandler { [weak self] in self?.tick() }
        self.timer = timer; timer.resume()
    }
    private func tick() {
        guard !sleeping else { return }
        let now = ProcessInfo.processInfo.systemUptime
        let result = state.tick(now: now)
        if let event = result.event { diagnostics.record(event, value: result.seconds) }
        if now - lastHeartbeat >= 30 {
            lastHeartbeat = now; diagnostics.record(.heartbeat, value: result.seconds)
            var load = [Double](repeating: 0, count: 3)
            if getloadavg(&load, 3) == 3 { diagnostics.record(.loadAverage100, value: Int(load[0] * 100)) }
        }
        if result.enqueue {
            DispatchQueue.main.async { [weak self] in
                self?.queue.async { [weak self] in
                    if let lag = self?.state.acknowledge(now: ProcessInfo.processInfo.systemUptime) { self?.diagnostics.record(.mainRecovered, value: lag) }
                }
            }
        }
    }
}

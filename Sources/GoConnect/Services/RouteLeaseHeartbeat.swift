import Foundation
import Darwin

// The supervisor lease must not depend on UI rendering or the main RunLoop.
// Keep the original inode open: renewal cannot recreate a removed lease or
// follow a replacement symlink. Stop drains the queue before revocation.
final class RouteLeaseHeartbeat: @unchecked Sendable {
    private let queue: DispatchQueue
    private let queueKey = DispatchSpecificKey<UInt8>()
    private var descriptor: Int32
    private var timer: DispatchSourceTimer?
    private let lease: URL
    private let device: dev_t
    private let inode: ino_t
    private let ownerTimeout: TimeInterval
    private var lastOwnerResponse = DispatchTime.now()

    init(lease: URL, interval: TimeInterval = 1, ownerTimeout: TimeInterval = 30, renewalQueue: DispatchQueue? = nil) throws {
        queue = renewalQueue ?? DispatchQueue(label: "com.willhsu.GoConnect.route-lease", qos: .userInitiated)
        let fd = Darwin.open(lease.path, O_WRONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC)
        guard fd >= 0 else { throw CocoaError(.fileWriteNoPermission) }
        var info = stat()
        guard fstat(fd, &info) == 0, info.st_mode & S_IFMT == S_IFREG,
              info.st_uid == getuid(), info.st_nlink == 1, info.st_mode & 0o077 == 0 else {
            Darwin.close(fd)
            throw CocoaError(.fileWriteNoPermission)
        }
        descriptor = fd
        self.lease = lease
        device = info.st_dev; inode = info.st_ino
        self.ownerTimeout = ownerTimeout
        queue.setSpecific(key: queueKey, value: 1)
        let source = DispatchSource.makeTimerSource(queue: queue)
        source.schedule(deadline: .now(), repeating: interval, leeway: .milliseconds(10))
        source.setEventHandler { [weak self] in
            guard let self, self.descriptor >= 0 else { return }
            let silence = Double(DispatchTime.now().uptimeNanoseconds - self.lastOwnerResponse.uptimeNanoseconds) / 1_000_000_000
            if silence >= self.ownerTimeout {
                // A permanently hung UI must not keep a privileged session
                // alive forever. Only revoke our own inode, never a replacement.
                var pathInfo = stat()
                if lstat(self.lease.path, &pathInfo) == 0,
                   pathInfo.st_dev == self.device, pathInfo.st_ino == self.inode {
                    _ = unlink(self.lease.path)
                }
                self.closeOnQueue()
                return
            }
            _ = futimes(self.descriptor, nil)
        }
        queue.sync {
            timer = source
            source.resume()
        }
    }

    func ownerResponded() {
        let responseTime = DispatchTime.now()
        queue.async { [weak self] in
            guard let self, self.descriptor >= 0 else { return }
            self.lastOwnerResponse = responseTime
        }
    }

    func stop() {
        if DispatchQueue.getSpecific(key: queueKey) != nil { closeOnQueue() }
        else { queue.sync { closeOnQueue() } }
    }
    private func closeOnQueue() {
        timer?.cancel(); timer = nil
        if descriptor >= 0 { Darwin.close(descriptor); descriptor = -1 }
    }
    deinit { stop() }
}

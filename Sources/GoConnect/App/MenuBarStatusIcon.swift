import AppKit
import GoConnectCore

/// Cached template artwork: the system supplies the menu-bar foreground color.
@MainActor
enum MenuBarStatusIcon {
    enum Badge: CaseIterable {
        case none, progress, connected, attention

        init(_ state: VPNControlState) {
            switch state {
            case .off: self = .none
            case .connected: self = .connected
            case .needsRestart, .interrupted, .failed: self = .attention
            case .waitingForPassword, .preparingService, .maintainingService,
                 .connecting, .authorizing, .disconnecting, .diagnostic, .endingDiagnostic:
                self = .progress
            }
        }
    }

    static func image(for state: VPNControlState) -> NSImage {
        images[Badge(state)]!
    }

    private static let images: [Badge: NSImage] = Dictionary(uniqueKeysWithValues: Badge.allCases.map { badge in
        let image = NSImage(size: NSSize(width: 20, height: 18), flipped: false) { _ in
            AppBranding.menuBarIcon.draw(in: NSRect(x: 0, y: 1, width: 17, height: 17))
            guard badge != .none else { return true }

            // Clear a halo instead of painting a background so this remains a true
            // template on light, dark, translucent and selected menu bars.
            let halo = NSBezierPath(ovalIn: NSRect(x: 10, y: 0, width: 10, height: 10))
            NSGraphicsContext.saveGraphicsState()
            NSGraphicsContext.current?.compositingOperation = .destinationOut
            NSColor.black.setFill()
            halo.fill()
            NSGraphicsContext.restoreGraphicsState()

            NSColor.black.setStroke()
            let circle = NSBezierPath(ovalIn: NSRect(x: 11.25, y: 1.25, width: 7.5, height: 7.5))
            circle.lineWidth = 1.25
            circle.stroke()
            let mark = NSBezierPath()
            mark.lineWidth = 1.25
            mark.lineCapStyle = .round
            mark.lineJoinStyle = .round
            switch badge {
            case .connected:
                mark.move(to: NSPoint(x: 12.8, y: 5))
                mark.line(to: NSPoint(x: 14.4, y: 3.4))
                mark.line(to: NSPoint(x: 17.2, y: 6.5))
            case .progress:
                mark.move(to: NSPoint(x: 15, y: 7.2))
                mark.line(to: NSPoint(x: 15, y: 5))
                mark.line(to: NSPoint(x: 16.8, y: 4))
            case .attention:
                mark.move(to: NSPoint(x: 15, y: 7))
                mark.line(to: NSPoint(x: 15, y: 4.8))
                NSColor.black.setFill()
                NSBezierPath(ovalIn: NSRect(x: 14.4, y: 2.5, width: 1.2, height: 1.2)).fill()
            case .none: break
            }
            mark.stroke()
            return true
        }
        image.isTemplate = true
        return (badge, image)
    })
}

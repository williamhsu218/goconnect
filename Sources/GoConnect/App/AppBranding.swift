import AppKit

@MainActor
enum AppBranding {
    static let icon: NSImage = {
        let name = Bundle.main.object(forInfoDictionaryKey: "CFBundleIconFile") as? String ?? "GoConnect"
        let filename = (name as NSString).pathExtension.isEmpty ? name + ".icns" : name
        if let resources = Bundle.main.resourceURL,
           let image = NSImage(contentsOf: resources.appendingPathComponent(filename)) { return image }
        return NSApplication.shared.applicationIconImage
    }()

    // Template vector artwork follows the system menu-bar contrast and selection.
    static let menuBarIcon: NSImage = {
        let size = NSSize(width: 18, height: 18)
        let image = NSImage(size: size, flipped: false) { _ in
            NSColor.black.setStroke()
            NSColor.black.setFill()

            // Open routing loop; reserve the lower-right corner for state badges.
            let outerPath = NSBezierPath()
            outerPath.lineWidth = 1.6
            outerPath.lineCapStyle = .round
            outerPath.lineJoinStyle = .round

            outerPath.move(to: NSPoint(x: 13.2, y: 12.5))
            outerPath.curve(to: NSPoint(x: 9.0, y: 15.5),
                            controlPoint1: NSPoint(x: 12.2, y: 14.6),
                            controlPoint2: NSPoint(x: 10.8, y: 15.5))
            outerPath.curve(to: NSPoint(x: 2.8, y: 9.0),
                            controlPoint1: NSPoint(x: 5.5, y: 15.5),
                            controlPoint2: NSPoint(x: 2.8, y: 12.5))
            outerPath.curve(to: NSPoint(x: 7.8, y: 2.8),
                            controlPoint1: NSPoint(x: 2.8, y: 5.4),
                            controlPoint2: NSPoint(x: 5.0, y: 2.8))
            outerPath.stroke()

            // Inbound route and endpoint.
            let innerPath = NSBezierPath()
            innerPath.lineWidth = 1.6
            innerPath.lineCapStyle = .round
            innerPath.lineJoinStyle = .round

            innerPath.move(to: NSPoint(x: 11.2, y: 9.5))
            innerPath.line(to: NSPoint(x: 6.8, y: 9.5))
            innerPath.stroke()

            let nodeRadius: CGFloat = 1.4
            let nodeRect = NSRect(x: 6.8 - nodeRadius,
                                  y: 9.5 - nodeRadius,
                                  width: nodeRadius * 2,
                                  height: nodeRadius * 2)
            let nodePath = NSBezierPath(ovalIn: nodeRect)
            nodePath.fill()

            return true
        }
        image.isTemplate = true
        return image
    }()

    static func refreshDockIcon() {
        NSApplication.shared.applicationIconImage = icon
        NSApplication.shared.dockTile.display()
    }
}

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

    // A native template symbol follows menu-bar contrast, Dark Mode and selection.
    // Keep the full-color app artwork for the Dock and in-app branding only.
    static let menuBarIcon: NSImage = {
        let symbol = NSImage(systemSymbolName: "network", accessibilityDescription: "GoConnect")!
        let image = symbol.withSymbolConfiguration(NSImage.SymbolConfiguration(pointSize: 16, weight: .regular)) ?? symbol
        image.size = NSSize(width: 18, height: 18)
        image.isTemplate = true
        return image
    }()

    static func refreshDockIcon() {
        NSApplication.shared.applicationIconImage = icon
        NSApplication.shared.dockTile.display()
    }
}

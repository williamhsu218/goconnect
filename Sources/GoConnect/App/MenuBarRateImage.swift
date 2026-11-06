import AppKit

/// Compose monochrome content only. NSStatusBarButton still owns tint, highlight,
/// hit testing, accessibility and menu tracking.
@MainActor
enum MenuBarRateImage {
    private static let font = NSFont.monospacedDigitSystemFont(ofSize: 9, weight: .medium)
    private static let iconWidth: CGFloat = 20
    private static let gap: CGFloat = 2

    static func image(icon: NSImage, lines: [String]) -> NSImage {
        let rows = Array(lines.prefix(2))
        // Fit the current content, not a worst-case rate. In particular, missing
        // samples must not leave an empty column the width of "100.0 MB/s".
        let textWidth = ceil(rows.map {
            NSAttributedString(string: $0, attributes: [.font: font]).size().width
        }.max() ?? 0)
        let size = NSSize(width: iconWidth + gap + textWidth, height: 22)
        let result = NSImage(size: size, flipped: false) { _ in
            icon.draw(in: NSRect(x: 0, y: 2, width: iconWidth, height: 18))
            let attributes: [NSAttributedString.Key: Any] = [.font: font, .foregroundColor: NSColor.black]
            for (index, line) in rows.enumerated() {
                let y: CGFloat = rows.count == 2 ? (index == 0 ? 11 : 0) : 5.5
                NSAttributedString(string: line, attributes: attributes)
                    .draw(at: NSPoint(x: iconWidth + gap, y: y))
            }
            return true
        }
        result.isTemplate = true
        return result
    }
}

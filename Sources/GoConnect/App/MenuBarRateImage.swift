import AppKit

/// Compose monochrome content only. NSStatusBarButton still owns tint, highlight,
/// hit testing, accessibility and menu tracking.
@MainActor
enum MenuBarRateImage {
    private static let font = NSFont.monospacedDigitSystemFont(ofSize: 9, weight: .medium)
    private static let iconWidth: CGFloat = 20
    private static let gap: CGFloat = 2
    private static let textWidth = ceil(
        // Rounding below 100 can produce "100.0"; the capped GB/s value
        // can also be wider than a normal sample. Reserve the true maximum once.
        ["↓ 100.0 MB/s", "↑ 100.0 MB/s", "↓ ≥999 GB/s", "↑ ≥999 GB/s",
         "↓ 1024 MB/s", "↑ 1024 MB/s"].map {
            NSAttributedString(string: $0, attributes: [.font: font]).size().width
        }.max() ?? 0
    )
    static let size = NSSize(width: iconWidth + gap + textWidth, height: 22)
    static let statusItemWidth = size.width + 2

    static func image(icon: NSImage, lines: [String]) -> NSImage {
        let rows = Array(lines.prefix(2))
        let result = NSImage(size: size, flipped: false) { _ in
            icon.draw(in: NSRect(x: 0, y: 2, width: iconWidth, height: 18))
            let attributes: [NSAttributedString.Key: Any] = [.font: font, .foregroundColor: NSColor.black]
            for (index, line) in rows.enumerated() {
                let y: CGFloat = rows.count == 2 ? (index == 0 ? 11 : 0) : 5.5
                let text = NSAttributedString(string: line, attributes: attributes)
                // Keep the outer edge tight for short samples, including "—".
                text.draw(at: NSPoint(x: size.width - text.size().width, y: y))
            }
            return true
        }
        result.isTemplate = true
        return result
    }
}

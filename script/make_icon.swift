import AppKit

guard CommandLine.arguments.count == 2 else {
    fputs("Usage: swift script/make_icon.swift <output.icns>\n", stderr)
    exit(2)
}
let root = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
let source = root.appendingPathComponent("Packaging/GoConnect.png")
guard let artwork = NSImage(contentsOf: source), artwork.size.width == artwork.size.height else {
    fatalError("A square master image is required at \(source.path)")
}
let output = URL(fileURLWithPath: CommandLine.arguments[1])
try FileManager.default.createDirectory(at: output.deletingLastPathComponent(), withIntermediateDirectories: true)
let folder = FileManager.default.temporaryDirectory.appendingPathComponent("GoConnect-\(UUID().uuidString).iconset")
try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
defer { try? FileManager.default.removeItem(at: folder) }

// Keep the supplied PNG intact. Fit it to the same macOS icon canvas at every size.
for size in [16, 32, 128, 256, 512] {
    for scale in [1, 2] {
        let pixels = size * scale
        let bitmap = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: pixels, pixelsHigh: pixels, bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: bitmap)
        NSGraphicsContext.current?.imageInterpolation = .high
        let transform = NSAffineTransform(); transform.scale(by: CGFloat(pixels) / 1024); transform.concat()
        let tile = NSRect(x: 80, y: 80, width: 864, height: 864)
        NSBezierPath(roundedRect: tile, xRadius: 216, yRadius: 216).addClip()
        // Slight overscan keeps the source image's outer white edge outside the tile.
        artwork.draw(in: tile.insetBy(dx: -24, dy: -24), from: .zero,
                     operation: .sourceOver, fraction: 1)
        NSGraphicsContext.restoreGraphicsState()
        let name = "icon_\(size)x\(size)" + (scale == 2 ? "@2x" : "") + ".png"
        try bitmap.representation(using: .png, properties: [:])!.write(to: folder.appendingPathComponent(name))
    }
}
let process = Process(); process.executableURL = URL(fileURLWithPath: "/usr/bin/iconutil")
process.arguments = ["-c", "icns", folder.path, "-o", output.path]
try process.run(); process.waitUntilExit()
guard process.terminationStatus == 0 else { fatalError("iconutil failed") }
print("Generated \(output.path) from \(source.lastPathComponent)")

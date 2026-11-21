import AppKit
import SwiftUI

/// Restores the main window once and remembers the last manual resize across app launches.
struct MainWindowSizing: NSViewRepresentable {
    private static let preferenceKey = "GoConnect.windowSize"
    private static let defaultSize = CGSize(width: 760, height: 532)
    static let minimumSize = CGSize(width: 680, height: 400)

    static var preferredSize: CGSize {
        guard let dimensions = UserDefaults.standard.array(forKey: preferenceKey) as? [Double],
              dimensions.count == 2, dimensions.allSatisfy({ $0.isFinite && $0 > 0 }) else {
            return defaultSize
        }
        return CGSize(width: max(minimumSize.width, dimensions[0]), height: max(minimumSize.height, dimensions[1]))
    }

    func makeNSView(context: Context) -> WindowObserver { WindowObserver() }

    func updateNSView(_ view: WindowObserver, context: Context) {}

    final class WindowObserver: NSView {
        private var sizeBeforeLiveResize: CGSize?

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            let notifications = NotificationCenter.default
            notifications.removeObserver(self)
            sizeBeforeLiveResize = nil
            guard let window else { return }
            notifications.addObserver(self, selector: #selector(userWillResize(_:)), name: NSWindow.willStartLiveResizeNotification, object: window)
            notifications.addObserver(self, selector: #selector(userDidResize(_:)), name: NSWindow.didEndLiveResizeNotification, object: window)

            DispatchQueue.main.async { [weak self, weak window] in
                guard let self, let window, self.window === window else { return }
                self.restoreSize(of: window)
            }
        }

        deinit { NotificationCenter.default.removeObserver(self) }

        @objc private func userWillResize(_ notification: Notification) {
            sizeBeforeLiveResize = window?.frame.size
        }

        @objc private func userDidResize(_ notification: Notification) {
            defer { sizeBeforeLiveResize = nil }
            guard let window, let sizeBeforeLiveResize, sizeBeforeLiveResize != window.frame.size else { return }

            let size = window.contentLayoutRect.size
            guard size.width.isFinite, size.height.isFinite, size.width > 0, size.height > 0 else { return }
            UserDefaults.standard.set([Double(size.width), Double(size.height)], forKey: MainWindowSizing.preferenceKey)
        }

        private func restoreSize(of window: NSWindow) {
            guard !window.inLiveResize, !window.isZoomed, !window.isMiniaturized,
                  !window.styleMask.contains(.fullScreen), window.attachedSheet == nil,
                  let display = window.screen ?? NSScreen.main else { return }

            let size = MainWindowSizing.preferredSize
            let frame = window.frame
            let content = window.contentLayoutRect
            let visible = display.visibleFrame
            let width = min(ceil(size.width + frame.width - content.width), visible.width)
            let height = min(ceil(size.height + frame.height - content.height), visible.height)
            guard abs(frame.width - width) >= 1 || abs(frame.height - height) >= 1 else { return }

            let x = max(visible.minX, min(frame.midX - width / 2, visible.maxX - width))
            let y = max(visible.minY, min(frame.maxY - height, visible.maxY - height))
            window.setFrame(NSRect(x: x, y: y, width: width, height: height), display: true)
        }
    }
}

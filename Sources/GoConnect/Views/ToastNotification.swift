import SwiftUI
import AppKit
import GoConnectCore

struct ToastBanner: View {
    let message: String
    let isError: Bool
    let onDismiss: () -> Void

    @State private var copied = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: isError ? "exclamationmark.triangle.fill" : "checkmark.circle.fill")
                .font(.system(size: 14, weight: .semibold))
                .foregroundStyle(isError ? Color.red : AppTheme.success)

            ViewThatFits(in: .vertical) {
                messageText
                ScrollView { messageText.frame(maxWidth: .infinity, alignment: .leading) }
                    .scrollIndicators(.visible)
            }
            .frame(maxHeight: 120, alignment: .topLeading)

            if isError {
                Button(action: copyError) {
                    Image(systemName: copied ? "checkmark" : "doc.on.doc")
                        .font(.system(size: 12, weight: .medium))
                        .foregroundStyle(copied ? AppTheme.success : .secondary)
                        .frame(width: 28, height: 28)
                }
                .buttonStyle(.plain)
                .accessibilityLabel(copied ? "已复制" : "复制错误信息")
                .help(copied ? "已复制到剪贴板" : "复制错误信息")
            }

            Button(action: onDismiss) {
                Image(systemName: "xmark")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(.secondary)
                    .frame(width: 28, height: 28)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("关闭提示")
            .help("关闭提示")
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .frame(width: 540)
        .fixedSize(horizontal: false, vertical: true)
        .appFloatingSurface()
        .task(id: message + (isError ? "error" : "success")) {
            if #available(macOS 14.0, *) {
                AccessibilityNotification.Announcement(message).post()
            }
            guard !isError else { return }
            try? await Task.sleep(nanoseconds: 3_000_000_000)
            guard !Task.isCancelled else { return }
            withAnimation(reduceMotion ? nil : .spring(response: 0.35, dampingFraction: 0.8)) {
                onDismiss()
            }
        }
    }

    private var messageText: some View {
        Text(message)
            .font(.callout)
            .fixedSize(horizontal: false, vertical: true)
            .lineLimit(isError ? nil : 3)
            .foregroundStyle(.primary)
            .textSelection(.enabled)
    }

    private func copyError() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(message, forType: .string)
        withAnimation(reduceMotion ? nil : .easeInOut(duration: 0.15)) {
            copied = true
        }
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) {
            withAnimation(reduceMotion ? nil : .easeInOut(duration: 0.15)) {
                copied = false
            }
        }
    }
}

struct ToastNotificationView: View {
    let message: String
    let isError: Bool
    let onDismiss: () -> Void
    init(message: String, isError: Bool = false, onDismiss: @escaping () -> Void) {
        self.message = message
        self.isError = isError
        self.onDismiss = onDismiss
    }

    var body: some View {
        ToastBanner(message: message, isError: isError, onDismiss: onDismiss)
            .padding(.bottom, 20)
            .padding(.horizontal, 20)
            .padding(.top, 8)
            .frame(maxWidth: .infinity, alignment: .center)
            .fixedSize(horizontal: false, vertical: true)
    }
}

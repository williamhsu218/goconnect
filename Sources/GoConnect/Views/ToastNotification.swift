import SwiftUI
import AppKit
import GoConnectCore

struct ToastBanner: View {
    let message: String
    let isError: Bool
    let onDismiss: () -> Void

    @State private var copied = false

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: isError ? "exclamationmark.triangle.fill" : "checkmark.circle.fill")
                .font(.system(size: 14, weight: .semibold))
                .foregroundStyle(isError ? AppTheme.warning : AppTheme.success)

            Text(message)
                .font(.callout)
                .lineLimit(3)
                .foregroundStyle(.primary)
                .textSelection(.enabled)

            if isError {
                Button(action: copyError) {
                    Image(systemName: copied ? "checkmark" : "doc.on.doc")
                        .font(.system(size: 12, weight: .medium))
                        .foregroundStyle(copied ? AppTheme.success : .secondary)
                }
                .buttonStyle(.plain)
                .accessibilityLabel(copied ? "已复制" : "复制错误信息")
                .help(copied ? "已复制到剪贴板" : "复制错误信息")
            }

            Button(action: onDismiss) {
                Image(systemName: "xmark")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("关闭提示")
            .help("关闭提示")
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .frame(maxWidth: 540)
        .background(.regularMaterial, in: Capsule())
        .overlay(
            Capsule()
                .strokeBorder(Color.primary.opacity(0.12), lineWidth: 0.5)
        )
        .shadow(color: .black.opacity(0.14), radius: 12, x: 0, y: 5)
        .task(id: message) {
            guard !isError else { return }
            try? await Task.sleep(nanoseconds: 3_000_000_000)
            guard !Task.isCancelled else { return }
            withAnimation(.spring(response: 0.35, dampingFraction: 0.8)) {
                onDismiss()
            }
        }
    }

    private func copyError() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(message, forType: .string)
        withAnimation(.easeInOut(duration: 0.15)) {
            copied = true
        }
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) {
            withAnimation(.easeInOut(duration: 0.15)) {
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
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottom)
            .transition(.asymmetric(
                insertion: .move(edge: .bottom).combined(with: .opacity),
                removal: .move(edge: .bottom).combined(with: .opacity)
            ))
            .animation(.spring(response: 0.35, dampingFraction: 0.8), value: message)
    }
}

struct ToastNotificationModifier: ViewModifier {
    let message: String?
    let isError: Bool
    let onDismiss: () -> Void

    func body(content: Content) -> some View {
        content.overlay(alignment: .bottom) {
            if let message, !message.isEmpty {
                ToastNotificationView(message: message, isError: isError, onDismiss: onDismiss)
                    .zIndex(999)
            }
        }
    }
}

extension View {
    func toastNotification(message: String?, isError: Bool = false, onDismiss: @escaping () -> Void) -> some View {
        modifier(ToastNotificationModifier(message: message, isError: isError, onDismiss: onDismiss))
    }
}

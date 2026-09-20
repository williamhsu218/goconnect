import SwiftUI
import GoConnectCore

enum AppTheme {
    static let accent = Color.accentColor
    static let success = Color.green
    static let warning = Color.orange
    static let cardRadius: CGFloat = 14
    static let itemRadius: CGFloat = 9
    static let buttonRadius: CGFloat = 8
    static let pagePadding: CGFloat = 24
    static let sectionSpacing: CGFloat = 16
    static let contentWidth: CGFloat = 980
    static let actionHeight: CGFloat = 30

    static let windowBackground = Color(nsColor: .windowBackgroundColor)
    static let controlBackground = Color(nsColor: .controlBackgroundColor)
    static let subtleBorder = Color(nsColor: .separatorColor).opacity(0.4)
    static let selectedBackground = Color.accentColor.opacity(0.08)
}

enum NetworkFormatters {
    static func formatBytes(_ bytes: UInt64) -> String {
        let b = Double(bytes)
        let kb: Double = 1024
        let mb: Double = 1024 * 1024
        let gb: Double = 1024 * 1024 * 1024
        let tb: Double = 1024 * 1024 * 1024 * 1024

        if b < kb {
            return "\(bytes) B"
        } else if b < mb {
            return String(format: "%.1f KB", b / kb)
        } else if b < gb {
            return String(format: "%.1f MB", b / mb)
        } else if b < tb {
            return String(format: "%.2f GB", b / gb)
        } else {
            return String(format: "%.2f TB", b / tb)
        }
    }

    static func formatRate(_ bytesPerSecond: Double) -> String {
        guard bytesPerSecond.isFinite && bytesPerSecond > 0 else {
            return "0 B/s"
        }
        let kb: Double = 1024
        let mb: Double = 1024 * 1024
        let gb: Double = 1024 * 1024 * 1024

        if bytesPerSecond < kb {
            return String(format: "%.0f B/s", bytesPerSecond)
        } else if bytesPerSecond < mb {
            return String(format: "%.1f KB/s", bytesPerSecond / kb)
        } else if bytesPerSecond < gb {
            return String(format: "%.1f MB/s", bytesPerSecond / mb)
        } else {
            return String(format: "%.2f GB/s", bytesPerSecond / gb)
        }
    }

    static func formatDuration(_ seconds: TimeInterval) -> String {
        guard seconds.isFinite && seconds > 0 else {
            return "00:00:00"
        }
        let totalSeconds = Int(seconds)
        let hours = totalSeconds / 3600
        let minutes = (totalSeconds % 3600) / 60
        let secs = totalSeconds % 60
        return String(format: "%02d:%02d:%02d", hours, minutes, secs)
    }
}

struct AppBrandIcon: View {
    var size: CGFloat = 42

    var body: some View {
        Image(nsImage: AppBranding.icon).resizable().interpolation(.high).scaledToFit()
            .frame(width: size, height: size).accessibilityHidden(true)
    }
}

private struct CardSurface: ViewModifier {
    var emphasized = false
    @Environment(\.colorScheme) private var colorScheme
    func body(content: Content) -> some View {
        content
            .background(AppTheme.controlBackground, in: RoundedRectangle(cornerRadius: AppTheme.cardRadius, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: AppTheme.cardRadius, style: .continuous)
                .strokeBorder(emphasized ? AppTheme.accent.opacity(0.45) : AppTheme.subtleBorder, lineWidth: emphasized ? 1.5 : 1))
    }
}

extension View {
    func cardSurface(emphasized: Bool = false) -> some View { modifier(CardSurface(emphasized: emphasized)) }
}

struct Surface<Content: View>: View {
    let emphasized: Bool
    let content: Content
    init(emphasized: Bool = false, @ViewBuilder content: () -> Content) {
        self.emphasized = emphasized; self.content = content()
    }
    var body: some View {
        VStack(alignment: .leading, spacing: 0) { content }
            .padding(16).frame(maxWidth: .infinity, alignment: .leading)
            .cardSurface(emphasized: emphasized)
    }
}

private struct AppActionButtonModifier: ViewModifier {
    let primary: Bool

    @ViewBuilder
    func body(content: Content) -> some View {
        if primary {
            content
                .buttonStyle(.borderedProminent)
                .buttonBorderShape(.roundedRectangle(radius: AppTheme.buttonRadius))
                .controlSize(.regular)
                .frame(minHeight: AppTheme.actionHeight)
        } else {
            content
                .buttonStyle(.bordered)
                .buttonBorderShape(.roundedRectangle(radius: AppTheme.buttonRadius))
                .controlSize(.regular)
                .frame(minHeight: AppTheme.actionHeight)
        }
    }
}

private struct AppInlineActionModifier: ViewModifier {
    func body(content: Content) -> some View {
        content
            .buttonStyle(.plain)
            .font(.callout.weight(.medium))
            .foregroundStyle(AppTheme.accent)
            .contentShape(Rectangle())
    }
}

extension View {
    func appActionStyle(primary: Bool = false) -> some View {
        modifier(AppActionButtonModifier(primary: primary))
    }

    func appInlineActionStyle() -> some View {
        modifier(AppInlineActionModifier())
    }
}

struct PageHeading: View {
    let title: String
    let subtitle: String
    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title).font(.system(size: 24, weight: .semibold))
            Text(subtitle).font(.callout).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .layoutPriority(1)
    }
}

struct PageHeader<Action: View>: View {
    let title: String
    let subtitle: String
    @ViewBuilder let action: () -> Action

    var body: some View {
        HStack(alignment: .top, spacing: 16) {
            VStack(alignment: .leading, spacing: 4) {
                Text(title).font(.system(size: 24, weight: .semibold))
                Text(subtitle).font(.callout).foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)

            action()
        }
    }
}

struct CardIcon: View {
    let symbol: String
    var body: some View {
        Image(systemName: symbol).font(.system(size: 17, weight: .semibold))
            .foregroundStyle(AppTheme.accent).frame(width: 36, height: 36)
            .background(AppTheme.accent.opacity(0.09), in: RoundedRectangle(cornerRadius: AppTheme.itemRadius))
    }
}

struct StatusPill: View {
    let title: String
    let symbol: String
    var color: Color = AppTheme.accent

    var body: some View {
        Label(title, systemImage: symbol)
            .font(.caption.weight(.semibold))
            .foregroundStyle(color)
            .padding(.horizontal, 9)
            .padding(.vertical, 5)
            .background(color.opacity(0.1), in: Capsule())
            .fixedSize()
    }
}

struct NoticeBanner: View {
    let message: String
    let isError: Bool
    let dismiss: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: isError ? "exclamationmark.triangle.fill" : "checkmark.circle.fill")
                .foregroundStyle(isError ? AppTheme.warning : AppTheme.success)
                .padding(.top, 1)
            Text(message)
                .font(.callout)
                .fixedSize(horizontal: false, vertical: true)
                .textSelection(.enabled)
            Button(action: dismiss) { Image(systemName: "xmark") }
                .buttonStyle(.plain)
                .foregroundStyle(.secondary)
                .accessibilityLabel("关闭提示")
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .frame(maxWidth: 430, alignment: .leading)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 12))
        .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(Color.primary.opacity(0.1)))
        .shadow(color: .black.opacity(0.12), radius: 16, y: 6)
    }
}

struct ApplicationIcon: View {
    let application: AllowedApplication
    var size: CGFloat = 32

    private var icon: NSImage {
        if let bundle = Bundle(path: application.path),
           let name = bundle.object(forInfoDictionaryKey: "CFBundleIconFile") as? String,
           let resources = bundle.resourceURL {
            let filename = (name as NSString).pathExtension.isEmpty ? name + ".icns" : name
            if let image = NSImage(contentsOf: resources.appendingPathComponent(filename)) { return image }
        }
        return NSWorkspace.shared.icon(forFile: application.path)
    }

    var body: some View {
        Image(nsImage: icon).resizable().scaledToFit().frame(width: size, height: size)
            .accessibilityHidden(true)
    }
}

struct CountBadge: View {
    let count: Int
    var body: some View {
        Text("\(count)").font(.caption.weight(.semibold)).monospacedDigit()
            .foregroundStyle(.secondary).padding(.horizontal, 8).padding(.vertical, 3)
            .background(.quaternary.opacity(0.6), in: Capsule())
    }
}

struct ModeBadge: View {
    let mode: RoutingMode
    var body: some View {
        Text(mode.title).font(.caption.weight(.medium)).foregroundStyle(AppTheme.accent)
            .padding(.horizontal, 9).padding(.vertical, 5)
            .background(AppTheme.accent.opacity(0.08), in: Capsule()).fixedSize()
    }
}

struct ModeSettingButton: View {
    let mode: RoutingMode
    let action: () -> Void
    var body: some View {
        Button(action: action) {
            HStack(spacing: 7) {
                Image(systemName: "slider.horizontal.3")
                Text(mode.title).fontWeight(.medium)
                Image(systemName: "chevron.right").font(.system(size: 9, weight: .semibold))
            }.font(.callout).padding(.horizontal, 5).padding(.vertical, 4)
        }
        .appActionStyle()
        .accessibilityLabel("分流模式：\(mode.title)，打开设置")
        .help("查看或编辑这条线路的分流模式")
    }
}

struct CompactInfoRow: View {
    let title: String
    let detail: String
    let symbol: String
    var color: Color = .secondary

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: symbol)
                .font(.system(size: 13, weight: .semibold))
                .foregroundStyle(color)
                .frame(width: 18, height: 20)
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.callout.weight(.medium))
                Text(detail).font(.caption).foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

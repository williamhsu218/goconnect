import SwiftUI
import GoConnectCore

enum AppTheme {
    static let accent = Color.accentColor
    static let success = Color.green
    static let warning = Color.orange
    static let cardRadius: CGFloat = 14
    static let floatingRadius: CGFloat = 18
    static let itemRadius: CGFloat = 10
    static let buttonRadius: CGFloat = 8
    static let surfacePadding: CGFloat = 18
    static let pagePadding: CGFloat = 24
    static let sectionSpacing: CGFloat = 20
    static let contentWidth: CGFloat = 980
    static let actionHeight: CGFloat = 30

    static let windowBackground = Color(nsColor: .windowBackgroundColor)
    static let controlBackground = Color(nsColor: .controlBackgroundColor)
    static let subtleBorder = Color(nsColor: .separatorColor).opacity(0.4)
    static let surfaceBorder = Color(nsColor: .separatorColor).opacity(0.6)
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

private struct CardSurface: ViewModifier {
    var emphasized = false
    @Environment(\.colorSchemeContrast) private var contrast
    func body(content: Content) -> some View {
        content
            .background(AppTheme.controlBackground, in: RoundedRectangle(cornerRadius: AppTheme.cardRadius, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: AppTheme.cardRadius, style: .continuous)
                .strokeBorder(contrast == .increased ? Color.primary.opacity(0.5) : (emphasized ? AppTheme.accent.opacity(0.25) : AppTheme.surfaceBorder), lineWidth: 1))
    }
}

extension View {
    func cardSurface(emphasized: Bool = false) -> some View { modifier(CardSurface(emphasized: emphasized)) }
}

private struct SurfaceFillsHeightKey: EnvironmentKey { static let defaultValue = false }

extension EnvironmentValues {
    /// Lets side-by-side surfaces share a row height without changing surfaces elsewhere.
    var surfaceFillsHeight: Bool {
        get { self[SurfaceFillsHeightKey.self] }
        set { self[SurfaceFillsHeightKey.self] = newValue }
    }
}

struct Surface<Content: View>: View {
    let emphasized: Bool
    let content: Content
    @Environment(\.surfaceFillsHeight) private var fillsHeight
    init(emphasized: Bool = false, @ViewBuilder content: () -> Content) {
        self.emphasized = emphasized; self.content = content()
    }
    var body: some View {
        VStack(alignment: .leading, spacing: 0) { content }
            .padding(AppTheme.surfacePadding)
            .frame(maxWidth: .infinity, maxHeight: fillsHeight ? .infinity : nil, alignment: .topLeading)
            .cardSurface(emphasized: emphasized)
    }
}

private struct AppActionButtonModifier: ViewModifier {
    let primary: Bool

    @ViewBuilder
    func body(content: Content) -> some View {
        if #available(macOS 26.0, *) {
            if primary {
                content.buttonStyle(.glassProminent).controlSize(.regular)
            } else {
                content.buttonStyle(.bordered).controlSize(.regular)
            }
        } else if primary {
            content
                .buttonStyle(.borderedProminent)
                .controlSize(.regular)
                .frame(minHeight: AppTheme.actionHeight)
        } else {
            content
                .buttonStyle(.bordered)
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

    /// Floating feedback lives in the control layer; data surfaces stay opaque.
    func appFloatingSurface() -> some View {
        modifier(FloatingSurface())
    }
}

private struct FloatingSurface: ViewModifier {
    @Environment(\.accessibilityReduceTransparency) private var reduceTransparency
    @ViewBuilder func body(content: Content) -> some View {
        if reduceTransparency {
            content.background(AppTheme.controlBackground, in: RoundedRectangle(cornerRadius: AppTheme.floatingRadius))
                .overlay(RoundedRectangle(cornerRadius: AppTheme.floatingRadius).strokeBorder(AppTheme.subtleBorder))
        } else if #available(macOS 26.0, *) {
            content.glassEffect(.regular, in: RoundedRectangle(cornerRadius: AppTheme.floatingRadius))
        } else {
            content.background(.regularMaterial, in: RoundedRectangle(cornerRadius: AppTheme.floatingRadius))
        }
    }
}

/// A faint, static wash behind connection content. Sidebar and toolbar keep their own
/// materials, and the wash drops away when the system asks for plainer surfaces.
struct ConnectionBackdrop: View {
    @Environment(\.colorSchemeContrast) private var contrast
    @Environment(\.accessibilityReduceTransparency) private var reduceTransparency

    var body: some View {
        if contrast != .increased && !reduceTransparency {
            LinearGradient(
                colors: [AppTheme.accent.opacity(0.05), AppTheme.accent.opacity(0)],
                startPoint: .topLeading,
                endPoint: UnitPoint(x: 0.75, y: 0.55)
            )
            .allowsHitTesting(false)
            .accessibilityHidden(true)
        }
    }
}

struct SectionHeader<Accessory: View>: View {
    let title: String
    let symbol: String
    let accessory: Accessory

    init(_ title: String, symbol: String, @ViewBuilder accessory: () -> Accessory) {
        self.title = title; self.symbol = symbol; self.accessory = accessory()
    }

    var body: some View {
        HStack(spacing: 8) {
            Label(title, systemImage: symbol).font(.headline)
            Spacer(minLength: 8)
            accessory
        }
    }
}

/// A primary reading with a smaller unit, for example "12.6" and "MB/s".
struct MetricValueText: View {
    let value: String
    var size: CGFloat = 26

    private var number: String {
        value.lastIndex(of: " ").map { String(value[..<$0]) } ?? value
    }
    private var unit: String? {
        value.lastIndex(of: " ").map { String(value[value.index(after: $0)...]) }
    }

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 3) {
            Text(number)
                .font(.system(size: size, weight: .semibold))
                .monospacedDigit()
                .foregroundStyle(value == "—" ? Color.secondary : Color.primary)
            if let unit {
                Text(unit)
                    .font(.system(size: size * 0.5, weight: .medium))
                    .foregroundStyle(.secondary)
            }
        }
        .lineLimit(1)
        .minimumScaleFactor(0.7)
    }
}

/// Stale or risky data stays fully readable; the symbol carries the meaning alongside color.
struct InlineNotice: View {
    let text: String
    var symbol = "exclamationmark.triangle.fill"

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            Image(systemName: symbol)
                .foregroundStyle(AppTheme.warning)
                .accessibilityHidden(true)
            Text(text)
                .fixedSize(horizontal: false, vertical: true)
        }
        .font(.subheadline)
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct PageHeading: View {
    let title: String
    let subtitle: String
    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title).font(.title2.weight(.semibold))
            Text(subtitle).font(.callout).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .layoutPriority(1)
    }
}

struct CardIcon: View {
    let symbol: String
    var body: some View {
        Image(systemName: symbol).font(.system(size: 17, weight: .semibold))
            .foregroundStyle(.secondary).frame(width: 32, height: 32)
    }
}

struct StatusPill: View {
    let title: String
    let symbol: String
    var color: Color = AppTheme.accent

    var body: some View {
        HStack(spacing: 5) {
            Image(systemName: symbol).foregroundStyle(color)
            Text(title).foregroundStyle(.primary)
        }
            .font(.caption.weight(.semibold))
            .padding(.horizontal, 9)
            .padding(.vertical, 5)
            .background(color.opacity(0.1), in: Capsule())
            .fixedSize()
            .accessibilityElement(children: .combine)
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
        Text(mode.title).font(.caption.weight(.medium)).foregroundStyle(.secondary)
            .padding(.horizontal, 9).padding(.vertical, 5)
            .background(.quaternary.opacity(0.5), in: Capsule()).fixedSize()
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

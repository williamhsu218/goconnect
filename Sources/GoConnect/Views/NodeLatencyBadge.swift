import SwiftUI
import GoConnectCore

struct LatencySignalBars: View {
    let level: Int
    let color: Color

    var body: some View {
        HStack(alignment: .bottom, spacing: 2) {
            RoundedRectangle(cornerRadius: 1, style: .continuous)
                .fill(level >= 1 ? color : color.opacity(0.2))
                .frame(width: 2.5, height: 4)
            RoundedRectangle(cornerRadius: 1, style: .continuous)
                .fill(level >= 2 ? color : color.opacity(0.2))
                .frame(width: 2.5, height: 7)
            RoundedRectangle(cornerRadius: 1, style: .continuous)
                .fill(level >= 3 ? color : color.opacity(0.2))
                .frame(width: 2.5, height: 10)
        }
        .frame(height: 10, alignment: .bottom)
        .accessibilityHidden(true)
    }
}

struct NodeLatencyBadge: View {
    let result: NodeLatencyResult?
    let pending: NodeLatencyStore.Pending?

    private var color: Color {
        if let pending {
            return pending == .testing ? AppTheme.accent : .secondary
        }
        guard let result else { return .secondary }
        switch result.outcome {
        case .success:
            guard let ms = result.milliseconds else { return .red }
            if ms < 200 { return AppTheme.success }
            if ms < 500 { return AppTheme.warning }
            return .red
        case .unavailable, .timeout:
            return .red
        case .cancelled:
            return .secondary
        }
    }

    private var signalLevel: Int {
        guard let result, result.outcome == .success, let ms = result.milliseconds else { return 0 }
        if ms < 200 { return 3 }
        if ms < 500 { return 2 }
        return 1
    }

    var body: some View {
        VStack(alignment: .trailing, spacing: 3) {
            if let pending {
                HStack(spacing: 6) {
                    if pending == .testing {
                        ProgressView().controlSize(.mini)
                        Text("测试中")
                            .font(.caption.weight(.medium))
                    } else {
                        Image(systemName: "clock")
                            .font(.system(size: 10))
                        Text("排队中")
                            .font(.caption.weight(.medium))
                    }
                }
                .padding(.horizontal, 9)
                .padding(.vertical, 4)
                .foregroundStyle(color)
                .background(color.opacity(0.1), in: Capsule())
                .overlay(Capsule().strokeBorder(color.opacity(0.2), lineWidth: 0.5))
            } else if let result {
                HStack(spacing: 5) {
                    switch result.outcome {
                    case .success:
                        LatencySignalBars(level: signalLevel, color: color)
                        Text(result.label)
                            .font(.callout.weight(.semibold))
                            .monospacedDigit()
                    case .timeout:
                        Image(systemName: "clock.badge.exclamationmark.fill")
                            .font(.system(size: 10))
                        Text(result.label)
                            .font(.callout.weight(.semibold))
                    case .unavailable:
                        Image(systemName: "exclamationmark.circle.fill")
                            .font(.system(size: 10))
                        Text(result.label)
                            .font(.callout.weight(.semibold))
                    case .cancelled:
                        Image(systemName: "slash.circle")
                            .font(.system(size: 10))
                        Text(result.label)
                            .font(.callout.weight(.semibold))
                    }
                }
                .padding(.horizontal, 9)
                .padding(.vertical, 4)
                .foregroundStyle(color)
                .background(color.opacity(0.1), in: Capsule())
                .overlay(Capsule().strokeBorder(color.opacity(0.25), lineWidth: 0.5))

                Text(result.checkedAt.formatted(date: .abbreviated, time: .shortened))
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            } else {
                HStack(spacing: 5) {
                    LatencySignalBars(level: 0, color: .secondary)
                    Text("未测试")
                        .font(.caption.weight(.medium))
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .foregroundStyle(.secondary)
                .background(Color.secondary.opacity(0.08), in: Capsule())
            }
        }
        .frame(minWidth: 96, alignment: .trailing)
        .help(result?.detail ?? "HTTPS 响应延迟，非下载速度。每次测试的网络状况可能不同。")
    }
}

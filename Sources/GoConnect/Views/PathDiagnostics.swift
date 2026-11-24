import SwiftUI
import GoConnectCore

struct PathCheckRow: View {
    let title: String
    let check: NetworkPathCheck
    @State private var expanded = false

    var body: some View {
        if let attempts = check.attempts, !attempts.isEmpty {
            FullRowDisclosure(title, isExpanded: $expanded, summary: check.summary) {
                ForEach(Array(attempts.enumerated()), id: \.offset) { index, attempt in
                    if index > 0 { Divider() }
                    VStack(alignment: .leading, spacing: 4) {
                        LabeledContent(attempt.targetLabel, value: attempt.resultLabel)
                            .font(.caption.weight(.medium))
                        ProbeTimings(attempt: attempt)
                    }
                }
            } label: { summary }
        } else {
            summary.padding(.vertical, 8)
        }
    }

    private var summary: some View {
        HStack {
            Text(title)
            Spacer(minLength: 4)
            Image(systemName: check.result == "ok" ? "checkmark.circle.fill" : "exclamationmark.circle.fill")
                .foregroundStyle(check.result == "ok" ? AppTheme.success : AppTheme.warning)
                .accessibilityHidden(true)
            Text(check.summary).foregroundStyle(.secondary).multilineTextAlignment(.trailing)
        }.font(.callout)
    }
}

private struct ProbeTimings: View {
    let attempt: NetworkProbeAttempt

    var body: some View {
        let metrics = [
            attempt.connectMs.map { "连接 \($0) ms" },
            attempt.tlsMs.map { "TLS \($0) ms" },
            attempt.responseMs.map { "响应阶段 \($0) ms" },
            attempt.firstByteMs.map { "首字节 \($0) ms" },
            attempt.durationMs.map { "本次共 \($0) ms" }
        ].compactMap { $0 }
        Text(metrics.joined(separator: " · "))
            .font(.caption.monospacedDigit()).foregroundStyle(.secondary)
            .fixedSize(horizontal: false, vertical: true)
    }
}

struct CLIProbePanel: View {
    let store: AppStore
    private enum Target: String, CaseIterable {
        case antigravity = "cloudcode-pa.googleapis.com"
        case antigravityDaily = "daily-cloudcode-pa.sandbox.googleapis.com"
        case codex = "chatgpt.com"
        case openAI = "api.openai.com"
        case claude = "api.anthropic.com"
        var title: String {
            switch self {
            case .antigravity: "Antigravity"
            case .antigravityDaily: "Antigravity（Daily）"
            case .codex: "Codex（ChatGPT）"
            case .openAI: "OpenAI API"
            case .claude: "Claude Code"
            }
        }
    }

    @State private var target = Target.antigravity
    @State private var request: UUID?
    @State private var result: NetworkProbeAttempt?
    @State private var message: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("CLI 服务连通性").font(.headline)
            Picker("服务", selection: $target) {
                ForEach(Target.allCases, id: \.self) { Text($0.title).tag($0) }
            }.accessibilityIdentifier("CLIProbeTarget")
            Text(target.rawValue).font(.caption.monospaced()).foregroundStyle(.secondary)
                .textSelection(.enabled)
            Button(request == nil ? "检测服务" : "检测中…", systemImage: "network") {
                result = nil
                message = nil
                request = UUID()
            }
            .appActionStyle()
            .disabled(request != nil || store.phase != "connected")
            .accessibilityIdentifier("RunCLIProbe")
            if let result {
                let status = result.httpStatus.map { "已收到 HTTP \($0)" } ?? result.resultLabel
                Label(status, systemImage: result.httpStatus == nil ? "exclamationmark.circle" : "network")
                    .font(.callout.weight(.medium))
                ProbeTimings(attempt: result)
            } else if let message {
                Text(message).font(.callout).foregroundStyle(.secondary)
            }
            Text(store.phase == "connected" ? "经当前代理线路检测，不发送账号凭据。首字节从请求开始计时。" : "连接主线路后可检测服务。")
                .font(.caption).foregroundStyle(.secondary)
        }
        .onChange(of: target) { clear() }
        .onChange(of: store.phase) { clear() }
        .onChange(of: store.connectedAt) { clear() }
        .onDisappear { clear() }
        .task(id: request) {
            guard let id = request else { return }
            do {
                let measured = try await store.probeCLI(host: target.rawValue)
                guard !Task.isCancelled, request == id else { return }
                result = measured
            } catch {
                guard !Task.isCancelled, request == id else { return }
                message = "未能完成检测，请检查当前线路后重试。"
            }
            request = nil
        }
    }

    private func clear() {
        request = nil
        result = nil
        message = nil
    }
}

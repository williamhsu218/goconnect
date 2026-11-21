import SwiftUI

struct FullRowDisclosure<Header: View, Content: View>: View {
    @Binding private var isExpanded: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var hovering = false

    private let title: String
    private let summary: String?
    private let header: Header
    private let content: Content

    init(_ title: String, isExpanded: Binding<Bool>, summary: String? = nil,
         @ViewBuilder content: () -> Content, @ViewBuilder label: () -> Header) {
        self.title = title
        _isExpanded = isExpanded
        self.summary = summary
        self.header = label()
        self.content = content()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Button {
                withAnimation(reduceMotion ? .easeInOut(duration: 0.16) : .smooth(duration: 0.26)) {
                    isExpanded.toggle()
                }
            } label: {
                HStack(spacing: 10) {
                    header.frame(maxWidth: .infinity, alignment: .leading)
                    Image(systemName: "chevron.right")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(.secondary)
                        .rotationEffect(.degrees(isExpanded ? 90 : 0))
                        .accessibilityHidden(true)
                }
                .padding(.horizontal, 8)
                .frame(minHeight: AppTheme.actionHeight)
                .background(hovering ? Color.primary.opacity(0.06) : .clear,
                            in: RoundedRectangle(cornerRadius: AppTheme.buttonRadius))
                .contentShape(Rectangle())
                .padding(.horizontal, -8)
            }
            .buttonStyle(.plain)
            .onHover { hovering = $0 }
            .accessibilityLabel(title)
            .accessibilityValue(isExpanded ? "已展开" : summary.map { "已收起，\($0)" } ?? "已收起")

            if isExpanded {
                content
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .transition(reduceMotion ? .opacity : .opacity.combined(with: .offset(y: -5)))
            }
        }
    }
}

extension FullRowDisclosure where Header == Text {
    init(_ title: String, isExpanded: Binding<Bool>, @ViewBuilder content: () -> Content) {
        self.init(title, isExpanded: isExpanded, content: content) { Text(title) }
    }
}

import SwiftUI

struct SlidingTabs<Item: Hashable & RawRepresentable>: View where Item.RawValue == String {
    let items: [Item]
    let selection: Item
    let accessibilityTitle: String
    var compact = false
    var titleForItem: (Item) -> String = { $0.rawValue }
    let onSelect: (Item) -> Void

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.layoutDirection) private var layoutDirection
    @FocusState private var focusedItem: Item?
    @State private var hoveredItem: Item?
    @State private var clickedItem: Item?

    var body: some View {
        HStack(spacing: 2) {
            ForEach(items, id: \.self) { item in
                let selected = item == selection
                Button {
                    if focusedItem != item { clickedItem = item }
                    focusAndSelect(item)
                } label: {
                    Text(titleForItem(item))
                        .font((compact ? Font.subheadline : .callout).weight(.medium))
                        .foregroundStyle(selected ? Color.primary : Color.secondary)
                        .padding(.horizontal, compact ? 12 : 15)
                        .frame(minHeight: compact ? 26 : AppTheme.actionHeight)
                        .background {
                            if hoveredItem == item && !selected {
                                Capsule().fill(Color.primary.opacity(0.06))
                            }
                        }
                        .contentShape(Capsule())
                        .tabBounds(item)
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(selected ? .isSelected : [])
                .focusable()
                .focused($focusedItem, equals: item)
                .focusEffectDisabled(clickedItem == item)
                .onKeyPress(.leftArrow) { move(from: item, toTheRight: false) }
                .onKeyPress(.rightArrow) { move(from: item, toTheRight: true) }
                .onHover { hovered in
                    if hovered {
                        hoveredItem = item
                    } else if hoveredItem == item {
                        hoveredItem = nil
                    }
                }
            }
        }
        .padding(3)
        .fixedSize()
        .tabHighlight(selection, fill: AppTheme.accent.opacity(0.17),
                      animation: reduceMotion ? nil : .spring(duration: 0.34, bounce: 0.10))
        .background(Color.primary.opacity(0.045), in: Capsule())
        .overlay(Capsule().strokeBorder(AppTheme.subtleBorder).allowsHitTesting(false))
        .accessibilityElement(children: .contain)
        .accessibilityLabel(accessibilityTitle)
        .onChange(of: focusedItem) { _, item in
            if item != clickedItem { clickedItem = nil }
        }
    }

    private func focusAndSelect(_ item: Item) {
        focusedItem = item
        onSelect(item)
    }

    private func move(from item: Item, toTheRight: Bool) -> KeyPress.Result {
        let step = (layoutDirection == .leftToRight) == toTheRight ? 1 : -1
        guard let index = items.firstIndex(of: item), items.indices.contains(index + step) else { return .ignored }
        clickedItem = nil
        focusAndSelect(items[index + step])
        return .handled
    }
}

private struct TabBounds<Item: Hashable>: PreferenceKey {
    static var defaultValue: [Item: Anchor<CGRect>] { [:] }

    static func reduce(value: inout [Item: Anchor<CGRect>], nextValue: () -> [Item: Anchor<CGRect>]) {
        value.merge(nextValue(), uniquingKeysWith: { _, next in next })
    }
}

extension View {
    func tabBounds<Item: Hashable>(_ item: Item) -> some View {
        anchorPreference(key: TabBounds<Item>.self, value: .bounds) { [item: $0] }
    }

    func tabHighlight<Item: Hashable>(_ selection: Item, fill: Color, animation: Animation?) -> some View {
        backgroundPreferenceValue(TabBounds<Item>.self) { anchors in
            GeometryReader { geometry in
                if let anchor = anchors[selection] {
                    let frame = geometry[anchor]
                    Capsule()
                        .fill(fill)
                        .frame(width: frame.width, height: frame.height)
                        .position(x: frame.midX, y: frame.midY)
                        .animation(animation, value: selection)
                }
            }
            .allowsHitTesting(false)
            .accessibilityHidden(true)
        }
    }
}

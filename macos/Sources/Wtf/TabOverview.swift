import AppKit
import WtfCore

/// Show All Tabs (⌘⇧\): every tab as a card with the last lines of its
/// screen, grouped like the strip, searchable. Enter or a click opens one;
/// ⌫ with an empty search closes the selected tab. Like TabOverview.svelte.
@MainActor
final class TabOverview: NSView, NSSearchFieldDelegate {
    private static var shown: [ObjectIdentifier: TabOverview] = [:]

    static func toggle(in w: WindowController) {
        if let o = shown[ObjectIdentifier(w)] {
            o.dismiss()
            return
        }
        let o = TabOverview(window: w)
        shown[ObjectIdentifier(w)] = o
        o.frame = w.rootView.bounds
        o.autoresizingMask = [.width, .height]
        w.rootView.addSubview(o)
        o.window?.makeFirstResponder(o.search)
    }

    static func close(in w: WindowController) { shown[ObjectIdentifier(w)]?.dismiss() }

    private unowned let owner: WindowController
    private let search = NSSearchField()
    private let scroll = NSScrollView()
    private let grid = FlippedView()
    private let empty = NSTextField(labelWithString: "")
    /// Previews are taken once, when the overview opens.
    private var previews: [Int: String] = [:]
    private var cards: [(tab: Int, view: NSView)] = []
    private var selected: Int?

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        let blur = NSVisualEffectView()
        blur.material = .hudWindow
        blur.blendingMode = .withinWindow
        blur.state = .active
        blur.autoresizingMask = [.width, .height]
        blur.frame = bounds
        addSubview(blur)

        for t in window.model.tabs {
            let lines = window.terminals[t.focusedPaneId]?.snapshot(rows: 12) ?? []
            previews[t.id] = lines.joined(separator: "\n")
        }
        search.placeholderString = "Search \(window.model.tabs.count) tabs"
        search.delegate = self
        let done = NSButton(title: "Done", target: self, action: #selector(dismiss))
        done.bezelStyle = .rounded
        let header = NSStackView(views: [search, done])
        header.spacing = 8
        header.translatesAutoresizingMaskIntoConstraints = false
        addSubview(header)
        scroll.documentView = grid
        scroll.drawsBackground = false
        scroll.hasVerticalScroller = true
        scroll.translatesAutoresizingMaskIntoConstraints = false
        addSubview(scroll)
        empty.textColor = .secondaryLabelColor
        empty.translatesAutoresizingMaskIntoConstraints = false
        addSubview(empty)
        NSLayoutConstraint.activate([
            header.topAnchor.constraint(equalTo: topAnchor, constant: 12),
            header.centerXAnchor.constraint(equalTo: centerXAnchor),
            search.widthAnchor.constraint(lessThanOrEqualToConstant: 420),
            search.widthAnchor.constraint(greaterThanOrEqualToConstant: 260),
            scroll.topAnchor.constraint(equalTo: topAnchor, constant: 52),
            scroll.leadingAnchor.constraint(equalTo: leadingAnchor),
            scroll.trailingAnchor.constraint(equalTo: trailingAnchor),
            scroll.bottomAnchor.constraint(equalTo: bottomAnchor),
            empty.centerXAnchor.constraint(equalTo: centerXAnchor),
            empty.topAnchor.constraint(equalTo: topAnchor, constant: 120),
        ])
        selected = window.model.activeId
        build()
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }
    override func hitTest(_ point: NSPoint) -> NSView? { super.hitTest(point) ?? self }

    private func haystack(_ t: Tab) -> String {
        let g = t.groupId.flatMap(owner.model.group).map(\.name) ?? ""
        return "\(owner.model.title(t)) \(WindowModel.prettyPath(owner.model.focusedPane(t)?.cwd ?? "")) \(g)"
    }

    private var matching: [Tab] {
        let q = search.stringValue.trimmingCharacters(in: .whitespaces)
        return q.isEmpty ? owner.model.tabs : owner.model.tabs.filter { Fuzzy.score(q, haystack($0)) != nil }
    }

    private var builtWidth: CGFloat = 0

    /// The grid flows to the width: built again when it changes (and once it has one).
    override func layout() {
        super.layout()
        let w = scroll.contentSize.width
        if w > 0, abs(w - builtWidth) > 1 {
            builtWidth = w
            build()
        }
    }

    private func build() {
        grid.subviews.forEach { $0.removeFromSuperview() }
        cards = []
        let list = matching
        empty.stringValue = list.isEmpty ? "No tabs match “\(search.stringValue)”" : ""
        if let s = selected, !list.contains(where: { $0.id == s }) { selected = list.first?.id }
        let width = max(300, scroll.contentSize.width > 0 ? scroll.contentSize.width : bounds.width)
        let cols = max(1, Int((width - 40) / 260))
        let cardW = (width - 40 - CGFloat(cols - 1) * 16) / CGFloat(cols)
        var y: CGFloat = 8, col = 0
        var lastGroup: Int?? = .none
        for t in list {
            if lastGroup == nil || lastGroup! != t.groupId {
                // A section per group, in strip order.
                if col != 0 { y += 196; col = 0 }
                if let g = t.groupId.flatMap(owner.model.group) {
                    let h = NSTextField(labelWithString: (g.name.isEmpty ? "Unnamed group" : g.name) + (g.collapsed ? " · collapsed" : ""))
                    h.font = .systemFont(ofSize: 12, weight: .semibold)
                    h.textColor = NSColor(hex: g.color.hex(dark: Settings.shared.theme?.dark ?? true))
                    h.frame = NSRect(x: 20, y: y, width: width - 40, height: 18)
                    grid.addSubview(h)
                    y += 26
                } else if lastGroup != nil {
                    y += 12
                }
                lastGroup = .some(t.groupId)
            }
            let card = card(t, width: cardW)
            card.frame.origin = NSPoint(x: 20 + CGFloat(col) * (cardW + 16), y: y)
            grid.addSubview(card)
            cards.append((t.id, card))
            col += 1
            if col == cols { col = 0; y += 196 }
        }
        if col != 0 { y += 196 }
        grid.frame = NSRect(x: 0, y: 0, width: width, height: y + 20)
        highlight()
    }

    private func card(_ t: Tab, width: CGFloat) -> NSView {
        let c = CardView(frame: NSRect(x: 0, y: 0, width: width, height: 180))
        c.onClick = { [weak self] in self?.open(t.id) }
        c.onHover = { [weak self] in self?.selected = t.id; self?.highlight() }
        let n = t.root.leaves.count
        let title = NSTextField(labelWithString: Redaction.shared.text(owner.model.title(t)) + (n > 1 ? "   \(n) panes" : ""))
        title.font = .systemFont(ofSize: 12.5, weight: .medium)
        title.lineBreakMode = .byTruncatingTail
        title.frame = NSRect(x: 12, y: 8, width: width - 24, height: 18)
        let preview = NSTextField(wrappingLabelWithString: previews[t.id] ?? "")
        preview.font = .monospacedSystemFont(ofSize: 8.5, weight: .regular)
        preview.textColor = .secondaryLabelColor
        preview.maximumNumberOfLines = 12
        preview.lineBreakMode = .byClipping
        preview.frame = NSRect(x: 12, y: 32, width: width - 24, height: 118)
        let cwd = NSTextField(labelWithString: WindowModel.prettyPath(owner.model.focusedPane(t)?.cwd ?? ""))
        cwd.font = .systemFont(ofSize: 11)
        cwd.textColor = .tertiaryLabelColor
        cwd.lineBreakMode = .byTruncatingMiddle
        cwd.frame = NSRect(x: 12, y: 156, width: width - 24, height: 16)
        [title, preview, cwd].forEach(c.addSubview)
        return c
    }

    private func highlight() {
        for (id, v) in cards {
            v.layer?.borderWidth = id == selected ? 2 : (id == owner.model.activeId ? 1.5 : 0.5)
            v.layer?.borderColor = (id == selected ? NSColor.controlAccentColor : NSColor.separatorColor).cgColor
        }
    }

    func controlTextDidChange(_ note: Notification) { build() }

    func control(_ control: NSControl, textView: NSTextView, doCommandBy selector: Selector) -> Bool {
        let ids = cards.map(\.tab)
        let i = selected.flatMap { ids.firstIndex(of: $0) }
        let cols = max(1, cards.filter { $0.view.frame.minY == cards.first?.view.frame.minY }.count)
        switch selector {
        case #selector(NSResponder.cancelOperation(_:)): dismiss()
        case #selector(NSResponder.insertNewline(_:)): if let s = selected { open(s) }
        case #selector(NSResponder.moveLeft(_:)): if let i, i > 0 { selected = ids[i - 1] }
        case #selector(NSResponder.moveRight(_:)): if let i, i + 1 < ids.count { selected = ids[i + 1] }
        case #selector(NSResponder.moveUp(_:)): if let i, i - cols >= 0 { selected = ids[i - cols] }
        case #selector(NSResponder.moveDown(_:)): if let i, i + cols < ids.count { selected = ids[i + cols] }
        case #selector(NSResponder.deleteBackward(_:)):
            guard search.stringValue.isEmpty, let s = selected else { return false }
            owner.closeTabs([s])
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.2) { [weak self] in self?.build() }
        default: return false
        }
        highlight()
        if let s = selected, let v = cards.first(where: { $0.tab == s })?.view { v.scrollToVisible(v.bounds) }
        return true
    }

    private func open(_ id: Int) {
        dismiss()
        owner.model.activate(id)
    }

    @objc func dismiss() {
        Self.shown[ObjectIdentifier(owner)] = nil
        removeFromSuperview()
        owner.focusActive()
    }
}

final class FlippedView: NSView {
    override var isFlipped: Bool { true }
}

private final class CardView: NSView {
    var onClick: (() -> Void)?
    var onHover: (() -> Void)?

    override init(frame: NSRect) {
        super.init(frame: frame)
        wantsLayer = true
        layer?.cornerRadius = 12
        layer?.backgroundColor = NSColor.textBackgroundColor.withAlphaComponent(0.6).cgColor
        addTrackingArea(NSTrackingArea(rect: .zero, options: [.mouseEnteredAndExited, .activeInKeyWindow, .inVisibleRect], owner: self))
    }

    required init?(coder: NSCoder) { fatalError() }
    override var isFlipped: Bool { true }
    override func hitTest(_ point: NSPoint) -> NSView? { frame.contains(point) ? self : nil }
    override func mouseEntered(with event: NSEvent) { onHover?() }
    override func mouseUp(with event: NSEvent) { onClick?() }
}

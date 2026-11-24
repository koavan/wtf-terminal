import AppKit
import WtfCore

/// The home page: what a window shows at launch, from the strip's Home
/// button, and while it has no tabs. Ways to start, the open tabs, the tabs
/// closed here, and where the rest is. Nothing in it polls.
@MainActor
final class HomeView: NSView {
    private unowned let owner: WindowController
    private let column = NSStackView()
    private var konami = Konami()

    init(owner: WindowController) {
        self.owner = owner
        super.init(frame: .zero)
        column.orientation = .vertical
        column.alignment = .leading
        column.spacing = 2
        column.translatesAutoresizingMaskIntoConstraints = false
        addSubview(column)
        let wide = column.widthAnchor.constraint(equalToConstant: 420)
        wide.priority = .defaultHigh
        let middle = column.centerYAnchor.constraint(equalTo: centerYAnchor)
        middle.priority = .defaultLow
        NSLayoutConstraint.activate([
            wide, middle,
            column.widthAnchor.constraint(lessThanOrEqualTo: widthAnchor, constant: -48),
            column.centerXAnchor.constraint(equalTo: centerXAnchor),
            column.topAnchor.constraint(greaterThanOrEqualTo: topAnchor, constant: 20),
        ])
        setAccessibilityLabel("Home")
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }
    override var acceptsFirstResponder: Bool { true }

    override func keyDown(with event: NSEvent) {
        let key: Konami.Key = switch event.keyCode {
        case 126: .up
        case 125: .down
        case 123: .left
        case 124: .right
        default:
            switch event.charactersIgnoringModifiers?.lowercased() {
            case "a": .a
            case "b": .b
            default: .other
            }
        }
        // Keys on the way to the code don't beep.
        guard key != .other else { return super.keyDown(with: event) }
        guard konami.key(key) else { return }
        Task { [weak self] in
            let on = await Settings.shared.togglePhosphor()
            Toast.show(on ? "▒▒ PHOSPHOR MODE ▒▒  (the code again turns it off)" : "Back to the present.", in: self)
        }
    }

    /// What the page was last built from.
    private var shape: String?

    /// Rebuilds the page when what it lists changed: the open tabs, and the
    /// tabs closed here.
    func refresh() {
        let m = owner.model
        var parts = m.tabs.map { t in
            "\(t.id)\u{1}\(Redaction.shared.text(m.title(t)))\u{1}\(t.groupId.flatMap(m.group)?.name ?? "")\u{1}\(TabStripView.icon(m.focusedPane(t)))"
        }
        parts.append("closed")
        parts += m.closedTabs.prefix(8).map { "\($0.api)\u{1}\(Redaction.shared.text($0.cwd))" }
        let now = parts.joined(separator: "\u{0}")
        guard now != shape else { return }
        shape = now
        column.arrangedSubviews.forEach { $0.removeFromSuperview() }
        let title = NSTextField(labelWithString: "wtf")
        title.font = .systemFont(ofSize: 28, weight: .semibold)
        add(title)
        let model = owner.model
        let open = model.tabs
        let sub = NSTextField(labelWithString: open.isEmpty ? "No tabs are open in this window."
            : open.count == 1 ? "1 tab is open in this window." : "\(open.count) tabs are open in this window.")
        sub.font = .systemFont(ofSize: 13)
        sub.textColor = .secondaryLabelColor
        add(sub)

        if !open.isEmpty {
            heading("Open tabs")
            for t in open.prefix(Self.maxOpen) {
                let title = Redaction.shared.text(model.title(t))
                let group = t.groupId.flatMap(model.group)?.name ?? ""
                row(TabStripView.icon(model.focusedPane(t)), group.isEmpty ? title : "\(title) — \(group)", key: "", tip: title) { [weak owner] in
                    owner?.model.activate(t.id)
                }
            }
            if open.count > Self.maxOpen {
                let more = NSTextField(labelWithString: "and \(open.count - Self.maxOpen) more in the tab strip")
                more.font = .systemFont(ofSize: 12)
                more.textColor = .tertiaryLabelColor
                add(more)
            }
        }

        heading("Start")
        command("plus", "New Tab", "tab.new")
        command("arrow.left.arrow.right", "New API Tab", "tab.newApi")
        command("sparkles", "Start Claude Code…", "agents.start")
        command("macwindow.badge.plus", "New Window", "window.new")

        let closed = owner.model.closedTabs.prefix(8)
        if !closed.isEmpty {
            heading("Recently closed")
            for (i, c) in closed.enumerated() {
                let path = c.cwd.isEmpty ? "Home folder" : Redaction.shared.text(WindowModel.prettyPath(c.cwd))
                row(c.api ? "arrow.left.arrow.right" : "folder", c.api ? "API tab" : path,
                    key: i == 0 ? key("tab.reopen") : "", tip: c.api ? nil : path) { [weak owner] in owner?.model.reopen(at: i) }
            }
        }

        heading("More")
        command("command", "Command Palette", "palette")
        side("square.stack.3d.up", "Workspaces", .workspaces, "sidebar.workspaces")
        side("person.2", "Agents", .agents, "sidebar.agents")
        command("book", "Manual", "help.manual")
        command("gearshape", "Settings…", "config.open")
    }

    /// How many open tabs the page lists.
    static let maxOpen = 12

    private func add(_ v: NSView) { column.addArrangedSubview(v) }

    private func heading(_ text: String) {
        if let last = column.arrangedSubviews.last { column.setCustomSpacing(20, after: last) }
        let l = NSTextField(labelWithString: text)
        l.font = .systemFont(ofSize: 12, weight: .semibold)
        l.textColor = .secondaryLabelColor
        add(l)
        column.setCustomSpacing(6, after: l)
    }

    private func key(_ id: String) -> String { Commands.all.first { $0.id == id }?.key ?? "" }

    private func row(_ symbol: String, _ title: String, key: String, tip: String? = nil, _ run: @escaping () -> Void) {
        let r = HomeRow(symbol: symbol, title: title, key: key, run)
        r.toolTip = tip
        add(r)
        r.widthAnchor.constraint(equalTo: column.widthAnchor).isActive = true
    }

    private func command(_ symbol: String, _ title: String, _ id: String) {
        row(symbol, title, key: key(id)) { [weak owner] in owner.map { Commands.run(id, in: $0) } }
    }

    /// Opens the side pane on a tab (its command would close it when it's already there).
    private func side(_ symbol: String, _ title: String, _ tab: SidebarTab, _ id: String) {
        row(symbol, title, key: key(id)) { [weak owner] in
            guard let owner, !(owner.sidebar.open && owner.sidebar.tab == tab) else { return }
            owner.showSidebar(tab)
        }
    }
}

/// One thing to click on the home page: a symbol, what it does, its shortcut.
@MainActor
private final class HomeRow: NSView {
    private let run: () -> Void
    private var hover = false { didSet { if hover != oldValue { needsDisplay = true } } }
    private var pressed = false { didSet { if pressed != oldValue { needsDisplay = true } } }

    init(symbol: String, title: String, key: String, _ run: @escaping () -> Void) {
        self.run = run
        super.init(frame: .zero)
        translatesAutoresizingMaskIntoConstraints = false
        let icon = NSImageView()
        icon.image = NSImage(systemSymbolName: symbol, accessibilityDescription: nil)
        icon.contentTintColor = .secondaryLabelColor
        icon.symbolConfiguration = .init(pointSize: 13, weight: .regular)
        let label = NSTextField(labelWithString: title)
        label.font = .systemFont(ofSize: 13)
        label.textColor = .labelColor
        label.lineBreakMode = .byTruncatingMiddle
        label.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        let keys = NSTextField(labelWithString: key)
        keys.font = .systemFont(ofSize: 12)
        keys.textColor = .tertiaryLabelColor
        keys.setContentHuggingPriority(.required, for: .horizontal)
        for v in [icon, label, keys] as [NSView] {
            v.translatesAutoresizingMaskIntoConstraints = false
            addSubview(v)
            v.centerYAnchor.constraint(equalTo: centerYAnchor).isActive = true
        }
        NSLayoutConstraint.activate([
            heightAnchor.constraint(equalToConstant: 28),
            icon.centerXAnchor.constraint(equalTo: leadingAnchor, constant: 18),
            label.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 36),
            keys.leadingAnchor.constraint(greaterThanOrEqualTo: label.trailingAnchor, constant: 12),
            keys.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -10),
        ])
        setAccessibilityElement(true)
        setAccessibilityRole(.button)
        setAccessibilityLabel(title)
    }

    required init?(coder: NSCoder) { fatalError() }

    // The whole row is the button, its labels included.
    override func hitTest(_ point: NSPoint) -> NSView? { frame.contains(point) ? self : nil }

    override func draw(_ dirty: NSRect) {
        guard hover || pressed else { return }
        NSColor.labelColor.withAlphaComponent(pressed ? 0.14 : 0.07).setFill()
        NSBezierPath(roundedRect: bounds, xRadius: 6, yRadius: 6).fill()
    }

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        trackingAreas.forEach(removeTrackingArea)
        addTrackingArea(NSTrackingArea(rect: bounds, options: [.mouseEnteredAndExited, .activeInKeyWindow, .inVisibleRect], owner: self))
    }

    override func mouseEntered(with event: NSEvent) { hover = true }
    override func mouseExited(with event: NSEvent) { hover = false; pressed = false }
    override func mouseDown(with event: NSEvent) { pressed = true }

    override func mouseUp(with event: NSEvent) {
        let inside = bounds.contains(convert(event.locationInWindow, from: nil))
        let was = pressed
        pressed = false
        if was, inside { run() }
    }

    override func accessibilityPerformPress() -> Bool {
        run()
        return true
    }
}

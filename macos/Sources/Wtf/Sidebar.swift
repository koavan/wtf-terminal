import AppKit
import WtfCore

/// A side-pane tab's view. Only the shown tab exists: it's made when its
/// tab is chosen and dropped when another is, so hidden tabs do no work.
@MainActor
protocol SidePaneTab: NSView {
    /// The focused pane, or something about it, changed.
    func paneChanged()
    /// A command finished in the focused pane.
    func commandFinished()
    /// What it shows, in a line, for debug self-checks.
    func debugSummary() -> String
}

extension SidePaneTab {
    func paneChanged() {}
    func commandFinished() {}
    func debugSummary() -> String { "" }
}

/// The side pane: an icon rail and the chosen tab, left of the terminals,
/// resizable by its right edge. Like Sidebar.svelte.
@MainActor
final class Sidebar: NSView {
    struct TabInfo {
        let tab: SidebarTab
        let label: String
        let symbol: String
        let key: String
        let make: (WindowController) -> SidePaneTab
    }

    /// The tabs the native app has so far (M4); the rest come with M5.
    static let tabs: [TabInfo] = [
        TabInfo(tab: .files, label: "Files", symbol: "folder", key: "⌘⇧E") { FilesView(window: $0) },
        TabInfo(tab: .ssh, label: "SSH Hosts", symbol: "server.rack", key: "⌘⇧S") { SshView(window: $0) },
        TabInfo(tab: .kube, label: "Kubernetes", symbol: "helm", key: "⌘⇧K") { KubeView(window: $0) },
    ]

    private unowned let owner: WindowController
    private let rail = NSStackView()
    private var railButtons: [SidebarTab: NSButton] = [:]
    private(set) var content: SidePaneTab?
    private var shownTab: SidebarTab?
    private let handle = ResizeHandle()

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        rail.orientation = .vertical
        rail.spacing = 2
        rail.alignment = .centerX
        rail.edgeInsets = NSEdgeInsets(top: 4, left: 4, bottom: 6, right: 6)
        for t in Self.tabs {
            let b = NSButton(image: NSImage(systemSymbolName: t.symbol, accessibilityDescription: t.label)
                ?? NSImage(systemSymbolName: "square", accessibilityDescription: t.label)!, target: self, action: #selector(railClicked(_:)))
            b.isBordered = false
            b.bezelStyle = .regularSquare
            b.imagePosition = .imageOnly
            b.refusesFirstResponder = true
            b.toolTip = "\(t.label) (\(t.key))"
            b.identifier = .init(t.tab.rawValue)
            b.wantsLayer = true
            b.layer?.cornerRadius = 7
            b.widthAnchor.constraint(equalToConstant: 32).isActive = true
            b.heightAnchor.constraint(equalToConstant: 32).isActive = true
            b.symbolConfiguration = .init(pointSize: 15, weight: .regular)
            railButtons[t.tab] = b
            rail.addArrangedSubview(b)
        }
        addSubview(rail)
        let line = NSBox()
        line.boxType = .separator
        line.identifier = .init("railLine")
        addSubview(line)
        addSubview(handle)
        handle.onDrag = { [weak self] dx, start in
            guard let self else { return }
            owner.sidebar.width = SidebarState.clampWidth(start + dx)
            owner.layoutSidebar()
        }
        handle.onDoubleClick = { [weak self] in
            self?.owner.sidebar.width = SidebarState.defaultWidth
            self?.owner.layoutSidebar()
        }
        handle.startWidth = { [weak self] in self?.owner.sidebar.width ?? SidebarState.defaultWidth }
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    /// Shows the window's chosen tab, making its view if it changed.
    func update() {
        let want = owner.sidebar.tab
        let tab = Self.tabs.contains { $0.tab == want } ? want : .files
        Debug.log("sidebar update: want \(want), shown \(String(describing: shownTab))")
        if tab != shownTab {
            content?.removeFromSuperview()
            let v = Self.tabs.first { $0.tab == tab }!.make(owner)
            content = v
            shownTab = tab
            addSubview(v)
        }
        let accent = NSColor(Settings.shared.theme?.accent ?? RGB(0x8d, 0x9c, 0xff))
        for (t, b) in railButtons {
            let on = t == tab
            b.contentTintColor = on ? accent : .secondaryLabelColor
            b.layer?.backgroundColor = on ? accent.withAlphaComponent(0.16).cgColor : nil
        }
        needsLayout = true
    }

    override func layout() {
        super.layout()
        let railW: CGFloat = 44
        rail.frame = NSRect(x: 0, y: 0, width: railW, height: bounds.height)
        subviews.first { $0.identifier?.rawValue == "railLine" }?.frame = NSRect(x: railW - 1, y: 4, width: 1, height: bounds.height - 8)
        content?.frame = NSRect(x: railW + 4, y: 0, width: max(0, bounds.width - railW - 4), height: bounds.height)
        handle.frame = NSRect(x: bounds.width - 4, y: 0, width: 8, height: bounds.height)
    }

    @objc private func railClicked(_ b: NSButton) {
        guard let t = b.identifier.flatMap({ SidebarTab(rawValue: $0.rawValue) }) else { return }
        owner.sidebar.tab = t
        owner.sidebarChanged()
    }
}

/// The 8pt strip at the pane's right edge: drag to resize, double-click for
/// the default width.
@MainActor
final class ResizeHandle: NSView {
    var onDrag: ((_ dx: CGFloat, _ start: CGFloat) -> Void)?
    var onDoubleClick: (() -> Void)?
    var startWidth: (() -> CGFloat)?
    private var start: (x: CGFloat, width: CGFloat)?
    private var hot = false { didSet { needsDisplay = true } }

    override func resetCursorRects() { addCursorRect(bounds, cursor: .resizeLeftRight) }

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        trackingAreas.forEach(removeTrackingArea)
        addTrackingArea(NSTrackingArea(rect: bounds, options: [.mouseEnteredAndExited, .activeInKeyWindow, .inVisibleRect], owner: self))
    }

    override func mouseEntered(with event: NSEvent) { hot = true }
    override func mouseExited(with event: NSEvent) { if start == nil { hot = false } }

    override func draw(_ dirty: NSRect) {
        guard hot else { return }
        NSColor.controlAccentColor.setFill()
        NSBezierPath(roundedRect: NSRect(x: bounds.midX - 1, y: 8, width: 2, height: bounds.height - 16), xRadius: 1, yRadius: 1).fill()
    }

    override func mouseDown(with event: NSEvent) {
        if event.clickCount == 2 {
            onDoubleClick?()
            return
        }
        start = (NSEvent.mouseLocation.x, startWidth?() ?? 300)
    }

    override func mouseDragged(with event: NSEvent) {
        guard let start else { return }
        onDrag?(NSEvent.mouseLocation.x - start.x, start.width)
    }

    override func mouseUp(with event: NSEvent) {
        start = nil
        hot = false
    }
}

/// A side-pane tab header: a title and small tool buttons.
@MainActor
final class PaneHeader: NSView {
    private let title = NSTextField(labelWithString: "")
    private let tools = NSStackView()

    init(_ text: String) {
        super.init(frame: .zero)
        title.stringValue = text.uppercased()
        title.font = .systemFont(ofSize: 11.5, weight: .semibold)
        title.textColor = .labelColor
        title.lineBreakMode = .byTruncatingMiddle
        title.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        tools.spacing = 2
        // The spacer gives way first, so the title keeps the room there is.
        let spacer = NSView()
        spacer.setContentHuggingPriority(.init(1), for: .horizontal)
        spacer.setContentCompressionResistancePriority(.init(1), for: .horizontal)
        title.setContentHuggingPriority(.defaultHigh, for: .horizontal)
        let row = NSStackView(views: [title, spacer, tools])
        row.edgeInsets = NSEdgeInsets(top: 0, left: 8, bottom: 0, right: 6)
        row.translatesAutoresizingMaskIntoConstraints = false
        addSubview(row)
        NSLayoutConstraint.activate([
            row.leadingAnchor.constraint(equalTo: leadingAnchor), row.trailingAnchor.constraint(equalTo: trailingAnchor),
            row.topAnchor.constraint(equalTo: topAnchor), row.bottomAnchor.constraint(equalTo: bottomAnchor),
            heightAnchor.constraint(equalToConstant: 30),
        ])
    }

    required init?(coder: NSCoder) { fatalError() }

    var text: String {
        get { title.stringValue }
        set { title.stringValue = newValue.uppercased() }
    }

    var tooltip: String? {
        get { title.toolTip }
        set { title.toolTip = newValue }
    }

    @discardableResult
    func tool(_ symbol: String, _ tip: String, _ action: @escaping () -> Void) -> ToolButton {
        let b = ToolButton(symbol: symbol, tip: tip, action: action)
        tools.addArrangedSubview(b)
        return b
    }

    /// Pause/resume auto-refresh (the shared side-pane setting) and refresh now.
    func refreshControls(in view: NSView, refresh: @escaping () -> Void) {
        let auto = Settings.shared.config?.sidebarAutoRefresh ?? true
        let pause = tool(auto ? "pause.fill" : "play.fill",
                         auto ? "Pause auto-refresh (side pane updates only when you ask)" : "Resume auto-refresh") {
            let on = !(Settings.shared.config?.sidebarAutoRefresh ?? true)
            Task { @MainActor in
                do {
                    try await Services.config.SetSidebarAutoRefresh(on)
                    Toast.show(on ? "Side pane auto-refresh on" : "Side pane auto-refresh off", in: view)
                } catch {
                    Toast.show("Couldn't change the setting: \(error.localizedDescription)", in: view)
                }
            }
        }
        if !auto { pause.contentTintColor = .controlAccentColor }
        tool("arrow.clockwise", "Refresh now", refresh)
    }
}

/// A small borderless symbol button that never takes keyboard focus.
@MainActor
final class ToolButton: NSButton {
    private let run: () -> Void

    init(symbol: String, tip: String, action: @escaping () -> Void) {
        run = action
        super.init(frame: .zero)
        image = NSImage(systemSymbolName: symbol, accessibilityDescription: tip)
        symbolConfiguration = .init(pointSize: 11.5, weight: .medium)
        isBordered = false
        bezelStyle = .regularSquare
        imagePosition = .imageOnly
        refusesFirstResponder = true
        toolTip = tip
        setAccessibilityLabel(tip)
        contentTintColor = .secondaryLabelColor
        target = self
        self.action = #selector(fire)
        widthAnchor.constraint(equalToConstant: 24).isActive = true
        heightAnchor.constraint(equalToConstant: 24).isActive = true
    }

    required init?(coder: NSCoder) { fatalError() }

    @objc private func fire() { run() }
}

/// A muted note in a side-pane body: a bold line, then text, wrapped to
/// the pane's width and as tall as it needs.
@MainActor
func paneNote(_ bold: String?, _ text: String) -> NoteView { NoteView(bold, text) }

@MainActor
final class NoteView: NSView {
    static let inset: CGFloat = 14
    private var labels: [NSTextField] = []

    init(_ bold: String?, _ text: String) {
        super.init(frame: .zero)
        if let bold {
            let b = NSTextField(wrappingLabelWithString: bold)
            b.font = .systemFont(ofSize: 12, weight: .semibold)
            labels.append(b)
        }
        if !text.isEmpty {
            let t = NSTextField(wrappingLabelWithString: text)
            t.font = .systemFont(ofSize: 12)
            t.textColor = .secondaryLabelColor
            labels.append(t)
        }
        labels.forEach(addSubview)
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    /// The height it needs at a width.
    func height(for width: CGFloat) -> CGFloat {
        let w = max(40, width - 2 * Self.inset)
        var h = Self.inset
        for l in labels {
            l.preferredMaxLayoutWidth = w
            h += ceil(l.cell?.cellSize(forBounds: NSRect(x: 0, y: 0, width: w, height: 10_000)).height ?? 16) + 4
        }
        return h + Self.inset - 4
    }

    override func layout() {
        super.layout()
        let w = max(40, bounds.width - 2 * Self.inset)
        var y = Self.inset
        for l in labels {
            l.preferredMaxLayoutWidth = w
            let h = ceil(l.cell?.cellSize(forBounds: NSRect(x: 0, y: 0, width: w, height: 10_000)).height ?? 16)
            l.frame = NSRect(x: Self.inset, y: y, width: w, height: h)
            y += h + 4
        }
    }
}

import AppKit
import WtfCore

/// Broadcast input: typing in one pane also goes to the other panes of its
/// tab group, or of its split tab. Per window, in memory only (never
/// survives a restart). Like internal/gtkui/broadcast.go.
@MainActor
final class Broadcast {
    static let shared = Broadcast()

    private struct State {
        var groups = Set<Int>()
        var tabs = Set<Int>()
        var on: Bool { !groups.isEmpty || !tabs.isEmpty }
    }

    private var state: [ObjectIdentifier: State] = [:]

    private func get(_ w: WindowController) -> State { state[ObjectIdentifier(w)] ?? State() }

    private func set(_ w: WindowController, _ s: State) {
        state[ObjectIdentifier(w)] = s.on ? s : nil
        w.broadcastChanged()
    }

    func isOn(in w: WindowController) -> Bool { get(w).on }
    func group(_ g: Int, in w: WindowController) -> Bool { get(w).groups.contains(g) }
    func panes(of tab: Int, in w: WindowController) -> Bool { get(w).tabs.contains(tab) }

    /// The tabs a tab's input goes to: its whole group when the group
    /// broadcasts, else just itself when its panes do.
    func scope(_ tab: Int, in w: WindowController) -> [Int] {
        let s = get(w)
        guard let t = w.model.tab(tab) else { return [] }
        if let g = t.groupId, s.groups.contains(g) { return w.model.tabsIn(g).map(\.id) }
        return s.tabs.contains(tab) ? [tab] : []
    }

    func isBroadcasting(tab: Int, in w: WindowController?) -> Bool {
        guard let w else { return false }
        return !scope(tab, in: w).isEmpty
    }

    /// The panes a pane's typing goes to, besides itself.
    func targets(of pane: Int, in w: WindowController) -> [Int] {
        guard get(w).on, let t = w.model.tabOf(pane: pane) else { return [] }
        return scope(t.id, in: w).flatMap { w.model.tab($0)?.root.leaves ?? [] }.filter { $0 != pane }
    }

    func includes(_ pane: Int, in w: WindowController) -> Bool { !targets(of: pane, in: w).isEmpty }

    func toggle(group g: Int, in w: WindowController) {
        var s = get(w)
        if !s.groups.insert(g).inserted { s.groups.remove(g) }
        set(w, s)
    }

    func toggle(panesOf tab: Int, in w: WindowController) {
        var s = get(w)
        if !s.tabs.insert(tab).inserted { s.tabs.remove(tab) }
        set(w, s)
    }

    func stopAll(in w: WindowController) { set(w, State()) }

    /// ⌥⌘B: stops every broadcast, or starts one for the active tab's group
    /// (more than one tab) or its split panes.
    func toggleActive(in w: WindowController) {
        if get(w).on {
            stopAll(in: w)
            return
        }
        guard let t = w.model.active else { return }
        if let g = t.groupId, w.model.tabsIn(g).count > 1 {
            toggle(group: g, in: w)
        } else if t.root.isSplit {
            toggle(panesOf: t.id, in: w)
        } else {
            Toast.show("Broadcast needs a tab group or split panes", in: w.rootView)
        }
    }

    /// Drops broadcasts whose group or tab is gone.
    func prune(in w: WindowController) {
        var s = get(w)
        guard s.on else { return }
        let before = (s.groups, s.tabs)
        s.groups = s.groups.filter { !w.model.tabsIn($0).isEmpty }
        s.tabs = s.tabs.filter { w.model.tab($0) != nil }
        if before != (s.groups, s.tabs) { set(w, s) }
    }

    /// "2 tabs in “prod”", "2 panes of a tab".
    func describe(in w: WindowController) -> String {
        let s = get(w)
        var parts: [String] = []
        for g in s.groups.sorted() {
            guard let group = w.model.group(g) else { continue }
            let n = w.model.tabsIn(g).count
            let tabs = "\(n) \(n == 1 ? "tab" : "tabs")"
            parts.append(group.name.isEmpty ? "\(tabs) in a group" : "\(tabs) in “\(group.name)”")
        }
        for t in s.tabs.sorted() {
            guard let tab = w.model.tab(t) else { continue }
            parts.append("\(tab.root.leaves.count) panes of a tab")
        }
        return parts.joined(separator: " and ")
    }
}

/// The orange bar under the address bar while input is broadcast.
@MainActor
final class BroadcastBar: NSView {
    private let label = NSTextField(labelWithString: "")
    private let stop = NSButton(title: "Stop  ⌥⌘B", target: nil, action: nil)
    var onStop: (() -> Void)?

    override init(frame: NSRect) {
        super.init(frame: frame)
        wantsLayer = true
        layer?.cornerRadius = 8
        layer?.backgroundColor = NSColor.systemOrange.withAlphaComponent(0.22).cgColor
        let icon = NSImageView(image: NSImage(systemSymbolName: "dot.radiowaves.left.and.right", accessibilityDescription: nil)!)
        icon.contentTintColor = .systemOrange
        label.font = .systemFont(ofSize: 12)
        label.lineBreakMode = .byTruncatingTail
        label.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        stop.bezelStyle = .accessoryBarAction
        stop.refusesFirstResponder = true
        stop.toolTip = "Stop broadcasting (⌥⌘B)"
        stop.target = self
        stop.action = #selector(stopPressed)
        stop.wantsLayer = true
        stop.layer?.backgroundColor = NSColor.systemOrange.cgColor
        stop.layer?.cornerRadius = 6
        stop.contentTintColor = .black
        let stack = NSStackView(views: [icon, label, stop])
        stack.spacing = 8
        stack.edgeInsets = NSEdgeInsets(top: 4, left: 10, bottom: 4, right: 6)
        stack.translatesAutoresizingMaskIntoConstraints = false
        addSubview(stack)
        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: leadingAnchor),
            stack.trailingAnchor.constraint(equalTo: trailingAnchor),
            stack.topAnchor.constraint(equalTo: topAnchor),
            stack.bottomAnchor.constraint(equalTo: bottomAnchor),
        ])
        setAccessibilityRole(.group)
    }

    required init?(coder: NSCoder) { fatalError() }

    func set(_ description: String) {
        let s = NSMutableAttributedString(string: "Broadcasting input", attributes: [.font: NSFont.systemFont(ofSize: 12, weight: .semibold)])
        s.append(NSAttributedString(string: " — typing goes to \(description)", attributes: [.font: NSFont.systemFont(ofSize: 12)]))
        label.attributedStringValue = s
    }

    @objc private func stopPressed() { onStop?() }
    override var mouseDownCanMoveWindow: Bool { false }
}

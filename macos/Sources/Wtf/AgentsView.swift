import AppKit
import WtfCore

/// The Agents tab (#33): every Claude Code in wtf's shells, across windows,
/// most urgent first: needs you, done, working, idle. A click goes to it.
/// Event-driven (Agents' changes); the only timer redraws "for 3m" every
/// 30s, and only while the tab shows.
@MainActor
final class AgentsView: NSView, SidePaneTab {
    private unowned let owner: WindowController
    private let header = PaneHeader("Agents")
    private let scroll = NSScrollView()
    private let form = PaneForm()
    private var timer: Timer?

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        addSubview(header)
        header.tool("questionmark.circle", "How the Agents tab works") { ManualWindow.show("agents") }
        let doc = FlippedView()
        doc.addSubview(form)
        scroll.documentView = doc
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        addSubview(scroll)
        Agents.observe(self) { [weak self] in self?.rebuild() }
        Redaction.shared.observe(self) { $0.rebuild() }
        Settings.shared.observe(self) { $0.rebuild() }
        rebuild()
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        timer?.invalidate()
        timer = nil
        guard window != nil else { return }
        timer = Timer.scheduledTimer(withTimeInterval: 30, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated { self?.rebuild() }
        }
    }

    override func layout() {
        super.layout()
        header.frame = NSRect(x: 0, y: 0, width: bounds.width, height: 30)
        scroll.frame = NSRect(x: 0, y: 30, width: bounds.width, height: max(0, bounds.height - 30))
        fit()
    }

    private func fit() {
        let w = scroll.contentSize.width
        form.frame.size.width = w
        form.layoutSubtreeIfNeeded()
        let h = form.fittingSize.height
        form.frame = NSRect(x: 0, y: 0, width: w, height: h)
        scroll.documentView?.frame = NSRect(x: 0, y: 0, width: w, height: h)
    }

    func paneChanged() { rebuild() }

    func debugSummary() -> String {
        "agents: " + Agents.sorted().map { "\($0.session) \(Agents.state($0).rawValue)" }.joined(separator: ", ")
    }

    private func rebuild() {
        form.clear()
        let list = Agents.sorted()
        if list.isEmpty {
            form.add(form.label("No Claude Code running", bold: true))
            if Settings.shared.config?.agentStatus == false {
                form.add(form.note("Only Ask Claude panes (⌘I) are listed: agent-status is off, so a claude you start yourself doesn't report what it's doing."))
                form.add(form.link("Turn on in Settings") { SettingsWindow.show() }, full: false)
            } else {
                form.add(form.note("Start claude in any tab, or Ask Claude (⌘I). Each one shows here: needs you, working, done or idle, with its last prompt. A click goes to it."))
            }
        }
        for st in list {
            form.add(AgentRow(st, owner: owner))
        }
        if !list.isEmpty {
            form.add(form.note("Go to Agent… ⌥⌘A · Next Agent Needing You ⌥⌘N"))
        }
        needsLayout = true
    }
}

/// One agent: its state, tab, folder, and last prompt or what it needs.
@MainActor
private final class AgentRow: NSView {
    private let session: String
    private unowned let owner: WindowController

    init(_ st: Go.bridge.Status, owner: WindowController) {
        session = st.session
        self.owner = owner
        super.init(frame: .zero)
        wantsLayer = true
        layer?.cornerRadius = 7
        let state = Agents.state(st)
        let place = Agents.find(st.session)
        let title = place.flatMap { w, p in w.model.tabOf(pane: p.id).map { w.model.title($0) } } ?? st.session
        let cwd = place.map { WindowModel.prettyPath($0.1.cwd) } ?? ""

        let dot = NSView()
        dot.wantsLayer = true
        dot.layer?.cornerRadius = 4
        dot.layer?.backgroundColor = Agents.color(state).cgColor
        dot.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([dot.widthAnchor.constraint(equalToConstant: 8), dot.heightAnchor.constraint(equalToConstant: 8)])

        let name = NSTextField(labelWithString: Redaction.shared.text(title))
        name.font = .systemFont(ofSize: 12, weight: .semibold)
        name.lineBreakMode = .byTruncatingTail
        name.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        let age = NSTextField(labelWithString: Agents.age(since: st.since))
        age.font = .systemFont(ofSize: 10.5)
        age.textColor = .secondaryLabelColor
        age.setContentCompressionResistancePriority(.required, for: .horizontal)
        let spacer = NSView()
        spacer.setContentHuggingPriority(.init(1), for: .horizontal)
        let top = NSStackView(views: [dot, name, spacer, age])
        top.spacing = 6

        let what = NSTextField(labelWithString: ([state.label] + (cwd.isEmpty ? [] : [cwd])).joined(separator: " · "))
        what.font = .systemFont(ofSize: 11, weight: state == .waiting ? .semibold : .regular)
        what.textColor = state == .waiting ? .systemOrange : .secondaryLabelColor
        what.lineBreakMode = .byTruncatingMiddle
        what.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)

        var lines: [NSView] = [top, what]
        let detail = state == .waiting && !st.message.isEmpty ? st.message : st.prompt
        if !detail.isEmpty {
            let d = NSTextField(wrappingLabelWithString: Redaction.shared.text(detail))
            d.font = .systemFont(ofSize: 11)
            d.textColor = .secondaryLabelColor
            d.maximumNumberOfLines = 2
            d.lineBreakMode = .byTruncatingTail
            d.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
            lines.append(d)
        }
        let stack = NSStackView(views: lines)
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 2
        stack.edgeInsets = NSEdgeInsets(top: 6, left: 8, bottom: 7, right: 8)
        stack.translatesAutoresizingMaskIntoConstraints = false
        addSubview(stack)
        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: leadingAnchor), stack.trailingAnchor.constraint(equalTo: trailingAnchor),
            stack.topAnchor.constraint(equalTo: topAnchor), stack.bottomAnchor.constraint(equalTo: bottomAnchor),
            top.widthAnchor.constraint(equalTo: stack.widthAnchor, constant: -16),
            what.widthAnchor.constraint(lessThanOrEqualTo: stack.widthAnchor, constant: -16),
        ])
        if let d = lines.dropFirst(2).first { d.widthAnchor.constraint(equalTo: stack.widthAnchor, constant: -16).isActive = true }
        if owner.model.active.flatMap({ owner.model.focusedPane($0) })?.sessionId == st.session {
            layer?.backgroundColor = NSColor.selectedContentBackgroundColor.withAlphaComponent(0.18).cgColor
        }
        toolTip = "Go to this Claude Code"
        setAccessibilityRole(.button)
        setAccessibilityLabel("\(title): Claude Code \(state.label.lowercased())")
    }

    required init?(coder: NSCoder) { fatalError() }

    override func mouseDown(with event: NSEvent) { Agents.go(to: session) }

    override func menu(for event: NSEvent) -> NSMenu? {
        let m = NSMenu()
        m.addItem(ClosureMenuItem(title: "Go to Claude Code", keyEquivalent: "") { [session] in Agents.go(to: session) })
        if let (_, p) = Agents.find(session), !p.cwd.isEmpty {
            m.addItem(ClosureMenuItem(title: "Copy Folder Path", keyEquivalent: "") {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(p.cwd, forType: .string)
            })
        }
        return m
    }
}

extension Agents {
    static func color(_ s: AgentState) -> NSColor {
        switch s {
        case .waiting: .systemOrange
        case .busy: .controlAccentColor
        case .done: .systemGreen
        case .idle: .tertiaryLabelColor
        }
    }

    /// How long ago, short: "now", "4m", "2h", "3d".
    static func age(since ms: Int) -> String {
        guard ms > 0 else { return "" }
        let s = max(0, Int(Date().timeIntervalSince1970) - ms / 1000)
        switch s {
        case ..<60: return "now"
        case ..<3600: return "\(s / 60)m"
        case ..<86400: return "\(s / 3600)h"
        default: return "\(s / 86400)d"
        }
    }

    /// The next agent that needs you (then the next done one), after the
    /// focused pane's, round and round.
    static func next(from w: WindowController) {
        let list = sorted().filter { [.waiting, .done].contains(state($0)) }
        guard !list.isEmpty else {
            Toast.show("No Claude Code needs you", in: w.rootView)
            return
        }
        let current = w.model.active.flatMap { w.model.focusedPane($0) }?.sessionId
        let i = list.firstIndex { $0.session == current }.map { ($0 + 1) % list.count } ?? 0
        go(to: list[i].session)
    }
}

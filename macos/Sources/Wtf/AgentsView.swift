import AppKit
import WtfCore

/// The Agents tab (#33): every Claude Code in wtf's shells, across windows,
/// in the order they started (rows don't move), by the name you gave them
/// or their tab's title. A click goes to it; a double-click renames it.
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
        header.tool("plus", "Start Claude Code…") { [weak window] in window?.startAgent() }
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
        "agents: " + Agents.sorted().map {
            "\($0.session) \(Agents.state($0).rawValue)" + (Agents.find($0.session)?.0.background == true ? " background" : "")
                + ($0.subagents.isEmpty ? "" : " subagents=\($0.subagents.count)" + (Agents.subagentsShown.contains($0.session) ? " shown" : ""))
        }.joined(separator: ", ")
    }

    private func rebuild() {
        form.clear()
        // Fixed order (when each started), so a row never moves.
        let list = Agents.listed()
        if list.isEmpty {
            form.add(form.label("No Claude Code running", bold: true))
            if Settings.shared.config?.agentStatus == false {
                form.add(form.note("Only Ask Claude panes (⌘I) are listed: agent-status is off, so a claude you start yourself doesn't report what it's doing."))
                form.add(form.link("Turn on in Settings") { SettingsWindow.show() }, full: false)
            } else {
                form.add(form.note("Start claude in any tab, or Ask Claude (⌘I). Each one shows here: needs you, working, done or idle, with its last prompt. A click goes to it."))
            }
        }
        let parked = list.filter { Agents.find($0.session)?.0.background == true }
        for st in list where !parked.contains(st) {
            form.add(AgentRow(st, owner: owner))
        }
        if !parked.isEmpty {
            let h = NSTextField(labelWithString: "BACKGROUND")
            h.font = .systemFont(ofSize: 10.5, weight: .bold)
            h.textColor = .secondaryLabelColor
            h.toolTip = "Running after their tab closed; they end when wtf quits"
            form.add(h, full: false)
            for st in parked { form.add(AgentRow(st, owner: owner)) }
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
        let title = Agents.title(st)
        let tab = Agents.tabDetail(st)
        let cwd = place.map { WindowModel.prettyPath($0.1.cwd) } ?? ""
        let branch = Worktrees.label(st.git)

        let dot = NSView()
        dot.wantsLayer = true
        dot.layer?.cornerRadius = 4
        dot.layer?.backgroundColor = Agents.color(state).cgColor
        dot.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([dot.widthAnchor.constraint(equalToConstant: 8), dot.heightAnchor.constraint(equalToConstant: 8)])

        let name = NSTextField(labelWithString: title)
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

        let what = NSTextField(labelWithString: [state.label, tab, cwd].filter { !$0.isEmpty }.joined(separator: " · "))
        what.font = .systemFont(ofSize: 11, weight: state == .waiting ? .semibold : .regular)
        what.textColor = state == .waiting ? .systemOrange : .secondaryLabelColor
        what.lineBreakMode = .byTruncatingMiddle
        what.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)

        var lines: [NSView] = [top, what]
        var branchRow: NSView?
        if !branch.isEmpty {
            let b = NSTextField(labelWithString: branch)
            b.font = .monospacedDigitSystemFont(ofSize: 10.5, weight: .regular)
            b.textColor = .secondaryLabelColor
            b.lineBreakMode = .byTruncatingMiddle
            b.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
            let icon = NSImageView(image: NSImage(systemSymbolName: st.git.linked ? "square.stack.3d.down.right" : "arrow.triangle.branch",
                                                  accessibilityDescription: nil)!)
            icon.symbolConfiguration = .init(pointSize: 9, weight: .regular)
            icon.contentTintColor = .secondaryLabelColor
            let row = NSStackView(views: [icon, b])
            row.spacing = 3
            row.toolTip = (st.git.linked ? "In a worktree, on " : "On ") + branch
                + (st.git.upstream.isEmpty ? "" : " (vs \(st.git.upstream), as of the last fetch)")
            lines.append(row)
            branchRow = row
        }
        // Full width (less the insets).
        var wide: [NSView] = []
        let detail = state == .waiting && !st.message.isEmpty ? st.message : st.prompt
        if !detail.isEmpty {
            let d = NSTextField(wrappingLabelWithString: Redaction.shared.text(detail))
            d.font = .systemFont(ofSize: 11)
            d.textColor = .secondaryLabelColor
            d.maximumNumberOfLines = 2
            d.lineBreakMode = .byTruncatingTail
            d.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
            lines.append(d)
            wide.append(d)
        }
        if !st.subagents.isEmpty {
            let open = Agents.subagentsShown.contains(st.session)
            lines.append(Self.subagentToggle(st, open: open, target: self, action: #selector(toggleClicked)))
            if open {
                for sa in st.subagents {
                    let line = SubagentLine(sa)
                    lines.append(line)
                    wide.append(line)
                }
            }
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
        for v in wide { v.widthAnchor.constraint(equalTo: stack.widthAnchor, constant: -16).isActive = true }
        branchRow?.widthAnchor.constraint(lessThanOrEqualTo: stack.widthAnchor, constant: -16).isActive = true
        let peek = NSButton(image: NSImage(systemSymbolName: "eye", accessibilityDescription: "Peek and reply")!, target: nil, action: nil)
        peek.isBordered = false
        peek.toolTip = "Peek and reply"
        peek.target = self
        peek.action = #selector(peekClicked)
        top.addArrangedSubview(peek)
        if owner.model.active.flatMap({ owner.model.focusedPane($0) })?.sessionId == st.session {
            layer?.backgroundColor = NSColor.selectedContentBackgroundColor.withAlphaComponent(0.18).cgColor
        }
        toolTip = (place?.0.background == true ? "Open it in a tab" : "Go to this Claude Code") + "; double-click to name it"
        setAccessibilityRole(.button)
        setAccessibilityLabel("\(title): Claude Code \(state.label.lowercased())")
    }

    required init?(coder: NSCoder) { fatalError() }

    override func mouseDown(with event: NSEvent) {
        if event.clickCount == 2 {
            Agents.rename(session, in: window)
        } else {
            Agents.go(to: session)
        }
    }

    @objc private func peekClicked() { Background.peek(session) }

    @objc private func toggleClicked() { Agents.toggleSubagents(session) }

    /// "▸ 3 sub-agents · 1 working": shows or hides them.
    private static func subagentToggle(_ st: Go.bridge.Status, open: Bool, target: AnyObject, action: Selector) -> NSButton {
        let n = st.subagents.count
        let running = st.subagents.filter(\.running).count
        var title = "\(n) sub-agent" + (n == 1 ? "" : "s")
        if running > 0 { title += " · \(running) working" }
        let b = NSButton(title: title, target: target, action: action)
        b.isBordered = false
        b.image = NSImage(systemSymbolName: open ? "chevron.down" : "chevron.right", accessibilityDescription: nil)
        b.symbolConfiguration = .init(pointSize: 8, weight: .semibold)
        b.imagePosition = .imageLeading
        b.contentTintColor = .secondaryLabelColor
        b.attributedTitle = NSAttributedString(string: title, attributes: [
            .font: NSFont.systemFont(ofSize: 10.5), .foregroundColor: NSColor.secondaryLabelColor,
        ])
        b.toolTip = open ? "Hide sub-agents" : "Show the sub-agents it started and what they're doing"
        b.setAccessibilityLabel((open ? "Hide " : "Show ") + title)
        return b
    }

    override func menu(for event: NSEvent) -> NSMenu? {
        let m = NSMenu()
        let parked = Agents.find(session)?.0.background == true
        m.addItem(ClosureMenuItem(title: parked ? "Open in Tab" : "Go to Claude Code", keyEquivalent: "") { [session] in Agents.go(to: session) })
        m.addItem(ClosureMenuItem(title: "Peek and Reply…", keyEquivalent: "") { [session] in Background.peek(session) })
        m.addItem(ClosureMenuItem(title: "Rename…", keyEquivalent: "") { [session, weak self] in Agents.rename(session, in: self?.window) })
        if Agents.all[session]?.subagents.isEmpty == false {
            let shown = Agents.subagentsShown.contains(session)
            m.addItem(ClosureMenuItem(title: shown ? "Hide Sub-agents" : "Show Sub-agents", keyEquivalent: "") { [session] in
                Agents.toggleSubagents(session)
            })
        }
        if let (_, p) = Agents.find(session), !p.cwd.isEmpty {
            m.addItem(ClosureMenuItem(title: "Copy Folder Path", keyEquivalent: "") {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(p.cwd, forType: .string)
            })
        }
        m.addItem(.separator())
        m.addItem(ClosureMenuItem(title: "End Claude Code…", keyEquivalent: "") { [session, weak owner] in Background.end(session, from: owner?.window) })
        return m
    }
}

/// One sub-agent under its agent: its type and task, then what it's doing
/// (its latest step) or, once done, its report's first line.
@MainActor
private final class SubagentLine: NSView {
    init(_ sa: Go.bridge.Subagent) {
        super.init(frame: .zero)
        let dot = NSView()
        dot.wantsLayer = true
        dot.layer?.cornerRadius = 3
        dot.layer?.backgroundColor = Agents.color(sa.running ? .busy : .idle).cgColor
        dot.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([dot.widthAnchor.constraint(equalToConstant: 6), dot.heightAnchor.constraint(equalToConstant: 6)])

        let kind = NSTextField(labelWithString: sa.type.isEmpty ? "Sub-agent" : sa.type)
        kind.font = .systemFont(ofSize: 11, weight: .medium)
        kind.setContentCompressionResistancePriority(.defaultHigh, for: .horizontal)
        let task = NSTextField(labelWithString: Redaction.shared.text(sa.task))
        task.font = .systemFont(ofSize: 11)
        task.lineBreakMode = .byTruncatingTail
        task.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        task.setContentHuggingPriority(.init(1), for: .horizontal)
        let age = NSTextField(labelWithString: Agents.age(since: sa.running ? sa.since : sa.ended))
        age.font = .systemFont(ofSize: 10.5)
        age.textColor = .secondaryLabelColor
        age.setContentCompressionResistancePriority(.required, for: .horizontal)
        let top = NSStackView(views: [dot, kind, task, age])
        top.spacing = 5

        let what = sa.running ? (sa.doing.isEmpty ? "Working" : sa.doing) : (sa.result.isEmpty ? "Done" : "Done · " + sa.result)
        let detail = NSTextField(labelWithString: Redaction.shared.text(what))
        detail.font = .systemFont(ofSize: 10.5)
        detail.textColor = .secondaryLabelColor
        detail.lineBreakMode = .byTruncatingTail
        detail.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)

        let stack = NSStackView(views: [top, detail])
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 1
        stack.edgeInsets = NSEdgeInsets(top: 2, left: 12, bottom: 2, right: 0)
        stack.translatesAutoresizingMaskIntoConstraints = false
        addSubview(stack)
        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: leadingAnchor), stack.trailingAnchor.constraint(equalTo: trailingAnchor),
            stack.topAnchor.constraint(equalTo: topAnchor), stack.bottomAnchor.constraint(equalTo: bottomAnchor),
            top.widthAnchor.constraint(equalTo: stack.widthAnchor, constant: -12),
            detail.widthAnchor.constraint(lessThanOrEqualTo: stack.widthAnchor, constant: -23),
        ])
        toolTip = Redaction.shared.text([sa.type, sa.task, sa.running ? sa.doing : sa.result].filter { !$0.isEmpty }.joined(separator: "\n"))
        setAccessibilityElement(true)
        setAccessibilityLabel("Sub-agent \(sa.type) \(sa.running ? "working" : "done"): \(Redaction.shared.text(sa.task))")
    }

    required init?(coder: NSCoder) { fatalError() }
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

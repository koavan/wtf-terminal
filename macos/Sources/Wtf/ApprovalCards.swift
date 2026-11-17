// What your agents are waiting on you for (#39): a permission request or a
// question. ApprovalContent builds what one shows (the Agents tab's inline
// rows and the floating cards share it); ApprovalDeck is the stack of cards
// floating over a window's terminals, bottom right (approval-cards).
import AppKit
import WtfCore

/// What kind of request it is, as the chip names it.
enum RequestKind {
    case run, edit, tool, question

    init(_ ap: Go.bridge.Approval) {
        if !ap.questions.isEmpty {
            self = .question
            return
        }
        switch ap.tool {
        case "Bash", "exec_command", "shell", "local_shell", "mcp__plugin_wtf_wtf__run_readonly", "mcp__plugin_wtf_wtf__type_at_prompt":
            self = .run
        case "Edit", "MultiEdit", "Write", "NotebookEdit", "apply_patch":
            self = .edit
        default:
            self = .tool
        }
    }

    var chip: String {
        switch self {
        case .run: "run"
        case .edit: "edit"
        case .tool: "tool"
        case .question: "question"
        }
    }

    /// What the agent wants, after its name.
    @MainActor func verb(_ ap: Go.bridge.Approval) -> String {
        switch self {
        case .run: "wants to run"
        case .edit: "wants to edit"
        case .tool: "wants to use " + Agents.toolName(ap.tool)
        case .question: ap.questions.count == 1 ? "asks you" : "asks you \(ap.questions.count) questions"
        }
    }

    var color: NSColor { self == .question ? .systemBlue : .systemOrange }
}

/// One request's content and buttons.
@MainActor
enum ApprovalContent {
    /// A small rounded label: "run", "edit", "tool", "question".
    static func chip(_ kind: RequestKind) -> NSView {
        let l = NSTextField(labelWithString: kind.chip)
        l.font = .systemFont(ofSize: 9.5, weight: .bold)
        l.textColor = kind.color
        l.alignment = .center
        let box = NSView()
        box.wantsLayer = true
        box.layer?.cornerRadius = 4
        box.layer?.backgroundColor = kind.color.withAlphaComponent(0.14).cgColor
        l.translatesAutoresizingMaskIntoConstraints = false
        box.addSubview(l)
        NSLayoutConstraint.activate([
            l.leadingAnchor.constraint(equalTo: box.leadingAnchor, constant: 5), l.trailingAnchor.constraint(equalTo: box.trailingAnchor, constant: -5),
            l.topAnchor.constraint(equalTo: box.topAnchor, constant: 1), l.bottomAnchor.constraint(equalTo: box.bottomAnchor, constant: -1),
        ])
        box.setContentHuggingPriority(.required, for: .horizontal)
        box.setContentCompressionResistancePriority(.required, for: .horizontal)
        return box
    }

    /// The lines that say what it asks: the command or file, where it would
    /// act, an edit's change, or the questions. `wide` must span the width.
    static func body(_ ap: Go.bridge.Approval, diffLines: Int, into lines: inout [NSView], wide: inout [NSView]) {
        let r = Redaction.shared
        func text(_ s: String, mono: Bool = false, size: CGFloat = 11, color: NSColor = .secondaryLabelColor, max: Int = 0) -> NSTextField {
            let l = NSTextField(wrappingLabelWithString: s)
            l.font = mono ? .monospacedSystemFont(ofSize: size, weight: .regular) : .systemFont(ofSize: size)
            l.textColor = color
            l.maximumNumberOfLines = max
            // Wrapped: the command and where it acts matter most here.
            l.lineBreakMode = mono ? .byCharWrapping : .byWordWrapping
            l.cell?.truncatesLastVisibleLine = true
            l.isSelectable = true
            l.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
            lines.append(l)
            wide.append(l)
            return l
        }
        if !ap.questions.isEmpty {
            for q in ap.questions {
                _ = text(r.text(q.question), size: 12, color: .labelColor, max: 4)
                if !Agents.clickable(ap) && !q.options.isEmpty {
                    _ = text(q.options.map { "· " + r.text($0) }.joined(separator: "\n"), size: 10.5)
                }
            }
            return
        }
        if !ap.description.isEmpty { _ = text(r.text(ap.description), max: 2) }
        if !ap.what.isEmpty { _ = text(r.text(ap.what), mono: true, color: .labelColor, max: 6) }
        for f in ap.facts {
            let l = text(r.text(f), size: 10.5, color: .labelColor)
            l.toolTip = "Where it would act, from this shell's context and what the command says. wtf doesn't judge it."
        }
        if !ap.diff.isEmpty {
            let shown = ap.diff.split(separator: "\n", omittingEmptySubsequences: false).prefix(diffLines)
            let a = NSMutableAttributedString()
            for (i, line) in shown.enumerated() {
                let color: NSColor = line.hasPrefix("- ") ? .systemRed : line.hasPrefix("+ ") ? .systemGreen : .secondaryLabelColor
                a.append(NSAttributedString(string: (i > 0 ? "\n" : "") + r.text(String(line)), attributes: [
                    .font: NSFont.monospacedSystemFont(ofSize: 10.5, weight: .regular), .foregroundColor: color,
                ]))
            }
            let more = ap.diff.split(separator: "\n").count - shown.count
            if more > 0 {
                a.append(NSAttributedString(string: "\n… \(more) more lines in the pane", attributes: [
                    .font: NSFont.systemFont(ofSize: 10.5), .foregroundColor: NSColor.secondaryLabelColor,
                ]))
            }
            let l = text("")
            l.attributedStringValue = a
        }
    }

    /// Allow / Deny / Go to Agent, or a question's options. `shortcut`: the
    /// Allow button names ⌘⏎ (the floating card's).
    static func buttons(_ ap: Go.bridge.Approval, of st: Go.bridge.Status, shortcut: Bool, anchor: NSView) -> NSView {
        let r = Redaction.shared
        let product = Agents.product(st)
        let session = st.session
        let id = ap.id
        func button(_ t: String, _ tip: String, key: Bool = false, _ run: @escaping () -> Void) -> NSButton {
            let b = ClosureButton(title: t, run)
            b.bezelStyle = .accessoryBarAction
            b.controlSize = .small
            b.font = .systemFont(ofSize: 11, weight: key ? .semibold : .regular)
            b.toolTip = tip
            // A click mustn't take the keyboard from the terminal.
            b.refusesFirstResponder = true
            return b
        }
        let go = "Go to Agent"
        if ap.questions.isEmpty {
            let row = NSStackView(views: [
                button(shortcut ? "Allow ⌘⏎" : "Allow", "Let \(product) do this once, as Yes in its own dialog", key: true) { [weak anchor] in
                    answer(id, "allow", anchor)
                },
                button("Deny", "Refuse it and stop \(product)'s turn, as No in its own dialog") { [weak anchor] in answer(id, "deny", anchor) },
                button(go, "Show its pane: its dialog has more choices (always allow, tell it what to do instead)") { Agents.go(to: session) },
            ])
            row.spacing = 2
            return row
        }
        // One button per option, one under another (labels can be long).
        var views: [NSView] = []
        if Agents.clickable(ap) {
            for o in ap.questions[0].options {
                let b = button(r.text(o), "Answer \(r.text(o)), as choosing it in \(product)'s own dialog") { [weak anchor] in
                    choose(id, o, anchor)
                }
                b.lineBreakMode = .byTruncatingTail
                views.append(b)
            }
        }
        let tip = Agents.clickable(ap) ? "Show its pane: its dialog also lets you type your own answer"
            : "Answer in its pane: several questions, or several choices, are answered there"
        views.append(button(go, tip, key: !Agents.clickable(ap)) { Agents.go(to: session) })
        let col = NSStackView(views: views)
        col.orientation = .vertical
        col.alignment = .leading
        col.spacing = 2
        return col
    }

    static func answer(_ id: String, _ decision: String, _ anchor: NSView?) {
        Task { @MainActor in
            do {
                try await Services.bridge.Answer(id, decision)
            } catch {
                Toast.show("Already answered in the pane", in: anchor)
            }
        }
    }

    static func choose(_ id: String, _ option: String, _ anchor: NSView?) {
        Task { @MainActor in
            do {
                try await Services.bridge.AnswerQuestion(id, [option])
            } catch {
                Toast.show("Already answered in the pane", in: anchor)
            }
        }
    }
}

/// The requests waiting on you, floating over a window's terminals, bottom
/// right: the oldest as a card (what it asks and its buttons), the others
/// peeking out under it, "1 of 3". Hidden for an agent whose pane is in
/// front in this window (its own dialog is right there). Collapsed, it's a
/// pill with the count; a new request opens it again. It never takes the
/// keyboard: ⌘⏎ allows the card's request.
@MainActor
final class ApprovalDeck: NSView {
    private unowned let owner: WindowController
    private var collapsed = false
    /// The requests shown, oldest first.
    private(set) var shown: [(Go.bridge.Status, Go.bridge.Approval)] = []
    private var known: Set<String> = []
    private var topId = ""

    static let width: CGFloat = 380

    init(owner: WindowController) {
        self.owner = owner
        super.init(frame: .zero)
        translatesAutoresizingMaskIntoConstraints = false
        isHidden = true
        Agents.observe(self) { [weak self] in self?.refresh() }
        Redaction.shared.observe(self) { $0.refresh() }
        Settings.shared.observe(self) { $0.refresh() }
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    /// The request ⌘⏎ answers (not a question).
    var top: Go.bridge.Approval? { collapsed || isHidden ? nil : shown.first?.1 }

    /// Rebuilds from the agents' requests (they changed, or the tab in front
    /// did).
    func refresh() {
        let front = Set(owner.model.active?.root.leaves.compactMap { owner.model.panes[$0]?.sessionId } ?? [])
        let on = Settings.shared.config?.approvalCards != false
        shown = on ? Agents.approvals().filter { !front.contains($0.0.session) } : []
        let ids = Set(shown.map(\.1.id))
        // Something new asks: open it again.
        if !ids.subtracting(known).isEmpty { collapsed = false }
        known = ids
        subviews.forEach { $0.removeFromSuperview() }
        guard let (st, ap) = shown.first else {
            isHidden = true
            topId = ""
            return
        }
        isHidden = false
        let content: NSView = collapsed ? pill() : card(ap, of: st)
        content.translatesAutoresizingMaskIntoConstraints = false
        addSubview(content)
        var peeks: [NSView] = []
        if !collapsed {
            // The others, peeking out under the card.
            for i in 0..<min(shown.count - 1, 2) {
                let p = NSView()
                p.wantsLayer = true
                p.layer?.cornerRadius = 9
                p.layer?.backgroundColor = NSColor.windowBackgroundColor.blended(withFraction: 0.08 * CGFloat(i + 1), of: .black)?.cgColor
                p.layer?.borderWidth = 1
                p.layer?.borderColor = NSColor.separatorColor.cgColor
                p.translatesAutoresizingMaskIntoConstraints = false
                addSubview(p, positioned: .below, relativeTo: i == 0 ? content : peeks[i - 1])
                let inset = CGFloat(10 * (i + 1))
                NSLayoutConstraint.activate([
                    p.leadingAnchor.constraint(equalTo: content.leadingAnchor, constant: inset),
                    p.trailingAnchor.constraint(equalTo: content.trailingAnchor, constant: -inset),
                    p.bottomAnchor.constraint(equalTo: content.bottomAnchor, constant: CGFloat(5 * (i + 1))),
                    p.heightAnchor.constraint(equalToConstant: 20),
                ])
                peeks.append(p)
            }
        }
        let below = CGFloat(5 * peeks.count)
        NSLayoutConstraint.activate([
            content.topAnchor.constraint(equalTo: topAnchor),
            content.trailingAnchor.constraint(equalTo: trailingAnchor),
            content.bottomAnchor.constraint(equalTo: bottomAnchor, constant: -below),
            collapsed ? content.leadingAnchor.constraint(greaterThanOrEqualTo: leadingAnchor)
                : content.leadingAnchor.constraint(equalTo: leadingAnchor),
        ])
        // A new card slides up into place, once.
        if !collapsed, ap.id != topId {
            content.wantsLayer = true
            content.alphaValue = 0
            NSAnimationContext.runAnimationGroup { ctx in
                ctx.duration = 0.18
                content.animator().alphaValue = 1
            }
        }
        topId = collapsed ? "" : ap.id
    }

    private func card(_ ap: Go.bridge.Approval, of st: Go.bridge.Status) -> NSView {
        let kind = RequestKind(ap)
        let box = NSVisualEffectView()
        box.material = .popover
        box.state = .active
        box.wantsLayer = true
        box.layer?.cornerRadius = 10
        box.layer?.borderWidth = 1
        box.layer?.borderColor = kind.color.withAlphaComponent(0.6).cgColor
        box.shadow = {
            let s = NSShadow()
            s.shadowBlurRadius = 12
            s.shadowColor = NSColor.black.withAlphaComponent(0.25)
            return s
        }()

        var who = Agents.title(st)
        if !ap.subagent.isEmpty { who += " · " + ap.subagent }
        let name = NSTextField(labelWithString: who)
        name.font = .systemFont(ofSize: 12, weight: .semibold)
        name.lineBreakMode = .byTruncatingTail
        name.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        let verb = NSTextField(labelWithString: kind.verb(ap))
        verb.font = .systemFont(ofSize: 12)
        verb.textColor = .secondaryLabelColor
        verb.lineBreakMode = .byTruncatingTail
        verb.setContentCompressionResistancePriority(.init(249), for: .horizontal)
        let spacer = NSView()
        spacer.setContentHuggingPriority(.init(1), for: .horizontal)
        let count = NSTextField(labelWithString: shown.count > 1 ? "1 of \(shown.count)" : "")
        count.font = .monospacedDigitSystemFont(ofSize: 10.5, weight: .regular)
        count.textColor = .secondaryLabelColor
        count.setContentCompressionResistancePriority(.required, for: .horizontal)
        let hide = NSButton(image: NSImage(systemSymbolName: "chevron.down", accessibilityDescription: "Collapse")!, target: self,
                            action: #selector(collapse))
        hide.isBordered = false
        hide.refusesFirstResponder = true
        hide.toolTip = "Collapse to a count; a new request opens it again"
        let top = NSStackView(views: [ApprovalContent.chip(kind), name, verb, spacer, count, hide])
        top.spacing = 6

        var lines: [NSView] = [top]
        var wide: [NSView] = []
        ApprovalContent.body(ap, diffLines: 8, into: &lines, wide: &wide)
        lines.append(ApprovalContent.buttons(ap, of: st, shortcut: true, anchor: owner.rootView))
        let stack = NSStackView(views: lines)
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 4
        stack.edgeInsets = NSEdgeInsets(top: 8, left: 10, bottom: 9, right: 10)
        stack.translatesAutoresizingMaskIntoConstraints = false
        box.addSubview(stack)
        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: box.leadingAnchor), stack.trailingAnchor.constraint(equalTo: box.trailingAnchor),
            stack.topAnchor.constraint(equalTo: box.topAnchor), stack.bottomAnchor.constraint(equalTo: box.bottomAnchor),
            top.widthAnchor.constraint(equalTo: stack.widthAnchor, constant: -20),
        ])
        for v in wide { v.widthAnchor.constraint(equalTo: stack.widthAnchor, constant: -20).isActive = true }
        box.setAccessibilityRole(.group)
        box.setAccessibilityLabel("\(who) \(kind.verb(ap))")
        return box
    }

    /// Collapsed: "3 waiting" with the most urgent kind's color.
    private func pill() -> NSView {
        let kind = shown.contains { RequestKind($0.1) != .question } ? RequestKind.run : .question
        let b = ClosureButton(title: "", { [weak self] in self?.expand() })
        b.isBordered = false
        b.wantsLayer = true
        b.layer?.cornerRadius = 12
        b.layer?.backgroundColor = kind.color.cgColor
        b.refusesFirstResponder = true
        let n = shown.count
        b.attributedTitle = NSAttributedString(string: "  \(n) waiting for you  ", attributes: [
            .font: NSFont.systemFont(ofSize: 11.5, weight: .semibold), .foregroundColor: NSColor.white,
        ])
        b.toolTip = "What your agents are waiting on you for: click to show"
        b.heightAnchor.constraint(equalToConstant: 24).isActive = true
        return b
    }

    @objc private func collapse() {
        collapsed = true
        refresh()
    }

    private func expand() {
        collapsed = false
        refresh()
    }

    /// ⌘⏎: allows the card's request (not a question: its options answer it).
    func allowTop() -> Bool {
        guard let ap = top, ap.questions.isEmpty else { return false }
        ApprovalContent.answer(ap.id, "allow", owner.rootView)
        return true
    }
}

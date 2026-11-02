import AppKit
import WtfCore

// Incident investigator (#27): ask why something is broken; the AI
// investigates with read-only commands that Go's investigate service checks
// against its allowlist. Each one waits for the user's Run (or Skip) before
// it's typed into the shell they asked from, so nothing runs unasked and
// each is a normal command block. It ends with a finding citing the
// steps, and a fix to type at the prompt (never run). The run lives in
// Investigation, per window, so it goes on while the side pane shows
// another tab; InvestigateView only shows it.

private struct InvText: Decodable { let id: String; let text: String }
struct InvRun: Decodable { let id: String; let step: Int; let line: String; let purpose: String }
private struct InvRefusal: Decodable { let id: String; let argv: [String]?; let reason: String; let purpose: String }

/// investigate.Done.
struct InvDone: Decodable {
    var id = ""
    var finding = ""
    var fix = ""
    var steps = 0
    var declined = 0
    var limit = ""
    var usage = AIUsage()
    var provider = ""
    var error = ""
    var cancelled = false
    var millis = 0
}

@MainActor
final class Investigation {
    enum StepState: Equatable { case awaiting, running, finished(Int), timedOut, failed, declined }

    enum Entry {
        case note(String)
        case step(n: Int, line: String, purpose: String, state: StepState, block: Int?)
        case refused(line: String, reason: String)
    }

    private static var byWindow: [ObjectIdentifier: Investigation] = [:]
    private static var running: [String: Investigation] = [:]
    private static var listening = false

    /// The window's investigation, running or finished.
    static func of(_ w: WindowController) -> Investigation? { byWindow[ObjectIdentifier(w)] }

    let id = "inv-" + UUID().uuidString
    let question: String
    /// The pane whose shell runs the commands.
    let paneId: Int
    /// "tab title · kube … · aws …", when it started.
    let context: String
    private weak var owner: WindowController?
    private(set) var entries: [Entry] = []
    private(set) var done: InvDone?
    private(set) var steps = 0
    /// The command waiting for the user's Run or Skip.
    private(set) var pending: InvRun?
    var busy: Bool { done == nil }
    /// Called on every change (the view redraws).
    var onChange: (() -> Void)?

    /// Commands run in this pane's shell; a step that hasn't finished by
    /// then gets Ctrl-C.
    private static let stepTimeout: Duration = .seconds(60)

    private init(question: String, paneId: Int, context: String, owner: WindowController) {
        self.question = question
        self.paneId = paneId
        self.context = context
        self.owner = owner
    }

    /// What the focused pane's shell is set up for, as the view shows it.
    static func contextLine(_ w: WindowController) -> String {
        guard let p = w.activePane else { return "" }
        var parts = [w.model.tabOf(pane: p.id).map { w.model.title($0) } ?? "this shell"]
        if let i = p.infra {
            if !i.kubeContext.isEmpty { parts.append("kube \(i.kubeContext)" + (i.kubeNamespace.isEmpty ? "" : "/\(i.kubeNamespace)")) }
            if !i.awsProfile.isEmpty { parts.append("aws \(i.awsProfile)" + (i.awsRegion.isEmpty ? "" : " \(i.awsRegion)")) }
        }
        return parts.joined(separator: " · ")
    }

    /// Starts one in the focused pane's shell; nil (and a message) when it can't.
    @discardableResult
    static func start(_ question: String, in w: WindowController, failed: (String) -> Void) -> Investigation? {
        let text = question.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return nil }
        if let cur = of(w), cur.busy {
            failed("An investigation is already running: stop it first")
            return nil
        }
        guard let p = w.activePane, let term = w.terminals[p.id], p.isShell else {
            failed("Focus a terminal pane: its shell runs the commands")
            return nil
        }
        guard p.integrated else {
            failed("This shell has no shell integration, so wtf can't tell when a command ends")
            return nil
        }
        guard !p.running else {
            failed("The shell is busy: finish the running program first")
            return nil
        }
        let inv = Investigation(question: text, paneId: p.id, context: contextLine(w), owner: w)
        byWindow[ObjectIdentifier(w)] = inv
        let recent = term.recentBlocks(3).map {
            Go.investigate.Recent(command: $0.text, exitCode: $0.exitCode ?? 0, output: $0.output ?? "")
        }
        let q = Go.investigate.Question(text: text, env: p.env ?? [:], cwd: p.cwd, recent: recent)
        Task {
            do {
                let req = try await Services.investigate.Request(q)
                let out = try await Services.ai.Preview(req)
                if out.firstSend {
                    if AIPreview.show(out, firstSend: true, in: w.window, done: { send in
                        send ? inv.begin(q, shown: true) : inv.finish(InvDone(id: inv.id, cancelled: true))
                    }) == nil {
                        inv.finish(InvDone(id: inv.id, error: "There's no window to show what will be sent"))
                    }
                } else {
                    inv.begin(q, shown: false)
                }
            } catch {
                inv.finish(InvDone(id: inv.id, error: error.localizedDescription))
            }
        }
        return inv
    }

    /// Forgets a finished one (New).
    static func clear(in w: WindowController) {
        if let cur = of(w), !cur.busy { byWindow[ObjectIdentifier(w)] = nil }
    }

    private static func listen() {
        guard !listening else { return }
        listening = true
        Backend.on("investigate:text", InvText.self) { e in running[e.id]?.note(e.text) }
        Backend.on("investigate:run", InvRun.self) { e in running[e.id]?.perform(e) }
        Backend.on("investigate:refused", InvRefusal.self) { e in
            running[e.id]?.add(.refused(line: (e.argv ?? []).joined(separator: " "), reason: e.reason))
        }
        Backend.on("investigate:done", InvDone.self) { d in running.removeValue(forKey: d.id)?.finish(d) }
    }

    private func begin(_ q: Go.investigate.Question, shown: Bool) {
        Self.listen()
        Self.running[id] = self
        Task {
            do {
                try await Services.investigate.Start(id, q, shown)
            } catch {
                Self.running[id] = nil
                finish(InvDone(id: id, error: error.localizedDescription))
            }
        }
    }

    func stop() {
        guard busy else { return }
        if Self.running[id] != nil {
            Task { try? await Services.investigate.Stop(id) }
        } else {
            finish(InvDone(id: id, cancelled: true))
        }
    }

    private func finish(_ d: InvDone) {
        guard busy else { return }
        done = d
        if let p = pending {
            pending = nil
            setStep(p.step, .declined)
        }
        // The finding streamed in as text too: show it once.
        if case .note(let t)? = entries.last, !d.finding.isEmpty,
           t.trimmingCharacters(in: .whitespacesAndNewlines) == d.finding.trimmingCharacters(in: .whitespacesAndNewlines) {
            entries.removeLast()
        }
        changed()
    }

    private func add(_ e: Entry) {
        entries.append(e)
        changed()
    }

    private func note(_ text: String) {
        if case .note(let t)? = entries.last {
            entries[entries.count - 1] = .note(t + text)
        } else {
            entries.append(.note(text))
        }
        changed()
    }

    private func changed() { onChange?() }

    private func setStep(_ n: Int, _ state: StepState, block: Int? = nil) {
        for (i, e) in entries.enumerated() {
            if case .step(n, let line, let purpose, _, let b) = e {
                entries[i] = .step(n: n, line: line, purpose: purpose, state: state, block: block ?? b)
            }
        }
        changed()
    }

    // MARK: Running a step

    /// Shows the command and waits for Run or Skip: nothing runs unasked.
    private func perform(_ r: InvRun) {
        steps = r.step
        pending = r
        add(.step(n: r.step, line: r.line, purpose: r.purpose, state: .awaiting, block: nil))
    }

    /// The user declined the waiting command.
    func skip() {
        guard let r = pending else { return }
        pending = nil
        setStep(r.step, .declined)
        report(r.step, Go.investigate.StepResult(declined: true))
    }

    /// The user approved the waiting command: type it into the shell.
    func approve() {
        guard let r = pending else { return }
        pending = nil
        setStep(r.step, .running)
        Task {
            // The shell reports it's back at its prompt a moment after the
            // last command's block ends: wait for that, briefly.
            let settle = ContinuousClock.now + .seconds(5)
            while !shellIdle, ContinuousClock.now < settle, done == nil {
                try? await Task.sleep(for: .milliseconds(100))
            }
            guard done == nil else { return }
            guard shellIdle, let term = owner?.terminals[paneId] else {
                setStep(r.step, .failed)
                report(r.step, Go.investigate.StepResult(error: "The shell isn't at a prompt any more (a program is running, or the pane closed)"))
                return
            }
            let before = term.lastFinishedBlock
            // Typed without moving the keyboard focus.
            term.type(Shell.clearLine + r.line + "\r")
            let clock = ContinuousClock()
            var deadline = clock.now + Self.stepTimeout
            var interrupted = false
            while true {
                try? await Task.sleep(for: .milliseconds(150))
                guard done == nil else { return }
                if let id = term.lastFinishedBlock, id != before, let b = term.block(id), let code = b.exitCode {
                    let out = term.blockOutput(id) ?? ""
                    setStep(r.step, interrupted ? .timedOut : .finished(code), block: id)
                    report(r.step, Go.investigate.StepResult(exitCode: code, output: out, timedOut: interrupted, block: id))
                    return
                }
                if clock.now > deadline {
                    if interrupted {
                        setStep(r.step, .failed)
                        report(r.step, Go.investigate.StepResult(error: "A command didn't stop after Ctrl-C"))
                        return
                    }
                    term.type("\u{03}")
                    interrupted = true
                    deadline = clock.now + .seconds(5)
                }
            }
        }
    }

    /// The shell is at its prompt, ready for a command.
    private var shellIdle: Bool {
        guard let w = owner, let p = w.model.panes[paneId], w.terminals[paneId] != nil else { return false }
        return p.isShell && !p.running
    }

    private func report(_ step: Int, _ res: Go.investigate.StepResult) {
        let id = self.id
        Task { try? await Services.investigate.Result(id, step, res) }
    }

    /// Scrolls the terminal to a step's command block.
    func reveal(step n: Int) {
        guard let w = owner, let term = w.terminals[paneId] else { return }
        for e in entries {
            if case .step(n, _, _, _, let block?) = e {
                if !term.revealCommand(block) { Toast.show("That command has left the scrollback", in: w.rootView) }
            }
        }
    }

    /// Types the fix at the prompt of the shell it investigated.
    func typeFix() {
        guard let w = owner, let fix = done?.fix, !fix.isEmpty, let term = w.terminals[paneId] else { return }
        switch term.typeAtPrompt(fix) {
        case .busy: Toast.show("The shell is busy — finish the running program first", in: w.rootView)
        case .copied: Toast.show("The fix has several lines and this shell would run them as typed: copied instead", in: w.rootView)
        case .typed: term.focus()
        }
    }

    /// The whole investigation as text, for Copy.
    var transcript: String {
        var lines = ["Question: \(question)", "In: \(context)", ""]
        for e in entries {
            switch e {
            case .note(let t): lines.append(t.trimmingCharacters(in: .whitespacesAndNewlines))
            case .step(let n, let line, _, let state, _): lines.append("[\(n)] $ \(line)" + Self.stateText(state))
            case .refused(let line, let reason): lines.append("refused: \(line) — \(reason)")
            }
        }
        if let d = done, !d.finding.isEmpty { lines += ["", d.finding] }
        return lines.joined(separator: "\n")
    }

    static func stateText(_ s: StepState) -> String {
        switch s {
        case .awaiting: return "  (waiting for you)"
        case .declined: return "  (skipped)"
        case .running: return "  (running)"
        case .finished(0): return ""
        case .finished(let c): return "  (exit \(c))"
        case .timedOut: return "  (timed out)"
        case .failed: return "  (couldn’t run)"
        }
    }
}

/// The Investigate side-pane tab.
@MainActor
final class InvestigateView: NSView, SidePaneTab, NSTextViewDelegate, NSTextFieldDelegate {
    private unowned let owner: WindowController
    private let header = PaneHeader("Investigate")
    private let field = NSTextField()
    private let go = NSButton()
    private let context = NSTextField(wrappingLabelWithString: "")
    private let scroll = NSTextView.scrollableTextView()
    private var text: NSTextView { scroll.documentView as! NSTextView }
    private let status = NSTextField(wrappingLabelWithString: "")
    private let fixButton = NSButton()
    private let copyButton = NSButton()
    private let runButton = NSButton()
    private let skipButton = NSButton()
    private var newTool: ToolButton!
    private var inv: Investigation?

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        newTool = header.tool("square.and.pencil", "New investigation") { [weak self] in self?.newInvestigation() }

        field.placeholderString = "Why is checkout-api crashlooping in staging?"
        field.font = .systemFont(ofSize: 12.5)
        field.usesSingleLineMode = false
        field.cell?.wraps = true
        field.cell?.isScrollable = false
        field.lineBreakMode = .byWordWrapping
        field.delegate = self
        go.bezelStyle = .rounded
        go.controlSize = .small
        go.target = self
        go.action = #selector(goClicked)
        context.font = .systemFont(ofSize: 11)
        context.textColor = .secondaryLabelColor
        let note = NSTextField(wrappingLabelWithString: "The AI may ask to run read-only commands (kubectl get/describe/logs, aws describe-*, helm history, dig, curl -I…) in this shell. Each one waits for your Run; nothing runs unasked. Fixes are typed at the prompt, never run.")
        note.font = .systemFont(ofSize: 11)
        note.textColor = .tertiaryLabelColor

        scroll.borderType = .noBorder
        scroll.drawsBackground = false
        text.isEditable = false
        text.drawsBackground = false
        text.textContainerInset = NSSize(width: 4, height: 6)
        text.delegate = self
        text.linkTextAttributes = [.foregroundColor: NSColor.labelColor, .cursor: NSCursor.pointingHand]

        status.font = .systemFont(ofSize: 11)
        status.textColor = .secondaryLabelColor
        for (b, title, action) in [(fixButton, "Type Fix at Prompt", #selector(fixClicked)), (copyButton, "Copy", #selector(copyClicked)),
                                   (runButton, "Run", #selector(runClicked)), (skipButton, "Skip", #selector(skipClicked))] {
            b.title = title
            b.bezelStyle = .rounded
            b.controlSize = .small
            b.target = self
            b.action = action
        }
        // No key equivalent: Return in the terminal must never approve a command.
        runButton.toolTip = "Types this command into the shell and runs it"
        skipButton.toolTip = "Tells the AI you declined; it continues without it"
        let buttons = NSStackView(views: [copyButton, skipButton, NSView(), runButton, fixButton])
        let ask = NSStackView(views: [field, go])
        ask.alignment = .top
        let stack = NSStackView(views: [header, ask, context, note, scroll, status, buttons])
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 6
        stack.edgeInsets = NSEdgeInsets(top: 0, left: 0, bottom: 8, right: 8)
        stack.translatesAutoresizingMaskIntoConstraints = false
        addSubview(stack)
        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: leadingAnchor), stack.trailingAnchor.constraint(equalTo: trailingAnchor),
            stack.topAnchor.constraint(equalTo: topAnchor), stack.bottomAnchor.constraint(equalTo: bottomAnchor),
            header.widthAnchor.constraint(equalTo: stack.widthAnchor),
            ask.widthAnchor.constraint(equalTo: stack.widthAnchor, constant: -8),
            scroll.widthAnchor.constraint(equalTo: stack.widthAnchor, constant: -8),
            buttons.widthAnchor.constraint(equalTo: stack.widthAnchor, constant: -8),
            field.heightAnchor.constraint(greaterThanOrEqualToConstant: 44),
        ])
        for l in [context, note, status] {
            l.translatesAutoresizingMaskIntoConstraints = false
            l.widthAnchor.constraint(equalTo: stack.widthAnchor, constant: -16).isActive = true
            l.setContentCompressionResistancePriority(.required, for: .vertical)
        }
        scroll.setContentHuggingPriority(.init(1), for: .vertical)
        scroll.setContentCompressionResistancePriority(.init(1), for: .vertical)
        attach(Investigation.of(window))
    }

    required init?(coder: NSCoder) { fatalError() }

    func paneChanged() { if inv?.busy != true { context.stringValue = Investigation.contextLine(owner) } }

    func focusQuestion() { window?.makeFirstResponder(field) }

    func debugSummary() -> String { inv.map { "investigation: \($0.steps) steps, busy \($0.busy)" } ?? "investigate: idle" }

    private func attach(_ i: Investigation?) {
        inv?.onChange = nil
        inv = i
        i?.onChange = { [weak self] in self?.render() }
        if let i { field.stringValue = i.question }
        render()
    }

    private func render() {
        let busy = inv?.busy == true
        go.title = busy ? "Stop" : "Investigate"
        field.isEditable = !busy
        newTool.isHidden = inv == nil || busy
        context.stringValue = inv.map { "In \($0.context)" } ?? Investigation.contextLine(owner)
        let fix = inv?.done?.fix ?? ""
        fixButton.isHidden = fix.isEmpty
        fixButton.toolTip = fix.isEmpty ? nil : "Types \(fix) at the prompt, without Return"
        copyButton.isHidden = inv == nil || busy
        let waiting = inv?.pending != nil
        runButton.isHidden = !waiting
        skipButton.isHidden = !waiting
        if let p = inv?.pending { runButton.title = "Run [\(p.step)]" }
        text.textStorage?.setAttributedString(transcript())
        text.scrollToEndOfDocument(nil)
        status.stringValue = statusText()
    }

    private func statusText() -> String {
        guard let inv else { return "" }
        guard let d = inv.done else {
            if let p = inv.pending { return "Waiting for you: Run [\(p.step)] types it into the shell, Skip declines it. Nothing runs until you choose." }
            return "Step \(inv.steps) of up to 12 · the shell is in use until it ends"
        }
        var parts: [String] = []
        if d.cancelled { parts.append("Stopped") }
        if d.limit == "steps" { parts.append("Stopped at the 12-command limit") }
        if d.limit == "tokens" { parts.append("Stopped at the token limit") }
        let ran = d.steps - d.declined
        parts.append("\(ran) \(ran == 1 ? "command" : "commands") run" + (d.declined > 0 ? ", \(d.declined) skipped" : ""))
        let t = AIDone(millis: d.millis)
        parts.append(t.summary)
        if d.usage.counted { parts.append("\(d.usage.inputTokens + d.usage.outputTokens) tokens") }
        if d.usage.cost > 0 { parts.append(String(format: "$%.4f", d.usage.cost)) }
        return parts.joined(separator: " · ")
    }

    private func transcript() -> NSAttributedString {
        let s = NSMutableAttributedString()
        guard let inv else {
            s.append(NSAttributedString(string: "Ask about something that's broken. The AI runs read-only commands in the focused shell, then gives a finding that cites them.",
                                        attributes: [.font: NSFont.systemFont(ofSize: 12), .foregroundColor: NSColor.secondaryLabelColor]))
            return s
        }
        let body: [NSAttributedString.Key: Any] = [.font: NSFont.systemFont(ofSize: 12), .foregroundColor: NSColor.labelColor]
        let dim: [NSAttributedString.Key: Any] = [.font: NSFont.systemFont(ofSize: 11.5), .foregroundColor: NSColor.secondaryLabelColor]
        let mono = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
        func add(_ t: String, _ a: [NSAttributedString.Key: Any]) { s.append(NSAttributedString(string: t, attributes: a)) }
        for e in inv.entries {
            switch e {
            case .note(let t):
                let t = t.trimmingCharacters(in: .whitespacesAndNewlines)
                if !t.isEmpty { add(Redaction.shared.text(t) + "\n\n", dim) }
            case .step(let n, let line, let purpose, let state, let block):
                let (glyph, color): (String, NSColor) = switch state {
                case .awaiting: ("?", .systemOrange)
                case .declined: ("–", .secondaryLabelColor)
                case .running: ("…", .controlAccentColor)
                case .finished(0): ("✓", .systemGreen)
                case .finished: ("✗", .systemRed)
                case .timedOut: ("⏱", .systemOrange)
                case .failed: ("✗", .systemRed)
                }
                add(glyph + " ", [.font: mono, .foregroundColor: color])
                var a: [NSAttributedString.Key: Any] = [.font: mono, .foregroundColor: NSColor.labelColor]
                if block != nil {
                    a[.link] = URL(string: "wtf-step:\(n)")!
                    a[.toolTip] = "Show it in the terminal"
                }
                add("[\(n)] \(Redaction.shared.text(line))", a)
                add(Investigation.stateText(state) + "\n", [.font: mono, .foregroundColor: NSColor.secondaryLabelColor])
                if state == .awaiting, !purpose.isEmpty { add("Why: \(purpose)\n", dim) }
                add("\n", dim)
            case .refused(let line, let reason):
                add("⊘ refused: \(Redaction.shared.text(line))\n", [.font: mono, .foregroundColor: NSColor.systemOrange])
                add(reason + "\n\n", dim)
            }
        }
        if let d = inv.done {
            if !d.error.isEmpty { add(d.error + "\n", [.font: NSFont.systemFont(ofSize: 12), .foregroundColor: NSColor.systemRed]) }
            if !d.finding.isEmpty { add(Redaction.shared.text(d.finding) + "\n", body) }
        }
        return s
    }

    func textView(_ textView: NSTextView, clickedOnLink link: Any, at charIndex: Int) -> Bool {
        guard let url = link as? URL, url.scheme == "wtf-step", let n = Int(url.absoluteString.dropFirst("wtf-step:".count)) else { return false }
        inv?.reveal(step: n)
        return true
    }

    func control(_ control: NSControl, textView: NSTextView, doCommandBy sel: Selector) -> Bool {
        if sel == #selector(NSResponder.insertNewline(_:)) {
            goClicked()
            return true
        }
        return false
    }

    @objc private func goClicked() {
        if let inv, inv.busy {
            inv.stop()
            return
        }
        let i = Investigation.start(field.stringValue, in: owner) { [weak self] msg in
            guard let self else { return }
            Toast.show(msg, in: self)
        }
        if let i { attach(i) }
    }

    private func newInvestigation() {
        Investigation.clear(in: owner)
        field.stringValue = ""
        attach(nil)
        focusQuestion()
    }

    @objc private func fixClicked() { inv?.typeFix() }
    @objc private func runClicked() { inv?.approve() }
    @objc private func skipClicked() { inv?.skip() }

    @objc private func copyClicked() {
        guard let inv else { return }
        Clipboard.copy(Redaction.shared.text(inv.transcript))
        Toast.show("Copied the investigation", in: self)
    }
}

extension WindowController {
    /// Opens the Investigate tab with the question field focused.
    func showInvestigate() {
        guard ExplainSheet.available else {
            Toast.show("Set up an AI provider in Settings first (⌘,)", in: rootView)
            return
        }
        sidebar.tab = .investigate
        sidebar.open = true
        sidebarChanged()
        (sidePane as? InvestigateView)?.focusQuestion()
    }
}

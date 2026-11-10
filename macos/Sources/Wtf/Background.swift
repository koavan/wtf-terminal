// Background agents (#34): a Claude Code whose tab you close can keep
// running in wtf's background window, a window that's never shown. Its pane
// moves there with its shell and scrollback, the way a tab moves between
// windows, so its hooks, badges and notifications carry on; the Agents tab
// opens it again in a tab, peeks at its screen, types a reply you write, or
// ends it. Background agents end when wtf quits. As
// internal/gtkui/background.go.
import AppKit
import WtfCore

extension WindowManager {
    /// The windows you can see (all but the background one).
    var visibleWindows: [WindowController] { windows.filter { !$0.background } }

    /// How many shells run in the background window.
    var backgroundCount: Int { background?.model.panes.count ?? 0 }

    /// Takes a parked pane into the background window, making it first.
    func park(_ payload: SavedTab.Payload, terminals: [String: TerminalController]) {
        if let bg = background {
            bg.adopt(payload, terminals: terminals)
        } else {
            openBackground { $0.bootstrap(session: nil, tab: (payload: payload, terminals: terminals)) }
        }
    }

    /// Starts claude in a new tab of the background window.
    func startInBackground(cwd: String, command: String) {
        if let bg = background {
            bg.model.open(cwd: cwd, command: command)
        } else {
            openBackground { w in
                w.model.open(cwd: cwd, command: command)
                w.bootstrap(session: nil, tab: nil, keepTabs: true)
            }
        }
    }
}

extension WindowController {
    /// The panes among ids running a Claude Code wtf knows of.
    func agentPanes(_ ids: [Int]) -> [Int] {
        ids.filter { model.panes[$0]?.sessionId.flatMap { Agents.all[$0] } != nil }
    }

    /// Moves panes to the background window, their shells still running.
    /// The last window you can see keeps a fresh tab rather than closing.
    func park(_ ids: [Int]) {
        for id in ids {
            guard let term = terminals[id], let s = model.panes[id]?.sessionId else { continue }
            if model.tabs.count == 1, model.tabOf(pane: id)?.root.leaves.count == 1, WindowManager.shared.visibleWindows.count == 1 {
                model.open(cwd: "")
            }
            markDetached(id)
            guard let payload = model.detachPane(id, buffers: { _ in "" }) else { continue }
            WindowManager.shared.park(payload, terminals: [s: term])
        }
        if !ids.isEmpty {
            Toast.show(ids.count == 1 ? "Claude Code keeps running in the background: the Agents tab opens it again"
                : "Claude Code sessions keep running in the background: the Agents tab opens them again", in: rootView)
        }
    }

    /// Some of the panes being closed run Claude Code: keep them running in
    /// the background, end them, or cancel (agent-close says which, unless
    /// it's "ask").
    func askAgentClose(_ agents: [Int], _ done: @escaping (Bool) -> Void) {
        switch Settings.shared.config?.agentClose ?? "ask" {
        case "background": return done(true)
        case "end": return done(false)
        default: break
        }
        guard let window else { return done(false) }
        let lines = agents.prefix(6).compactMap { id -> String? in
            guard let s = model.panes[id]?.sessionId else { return nil }
            let title = model.tabOf(pane: id).map { model.title($0) } ?? s
            return Redaction.shared.text(title) + " — " + (Agents.state(session: s)?.label.lowercased() ?? "running")
        } + (agents.count > 6 ? ["and \(agents.count - 6) more"] : [])
        let alert = NSAlert()
        alert.messageText = "Keep Claude Code running?"
        alert.informativeText = (agents.count == 1
            ? "Claude Code runs in what you're closing. It can keep running in the background: the Agents tab shows it and opens it again. It ends when wtf quits."
            : "\(agents.count) Claude Code sessions run in what you're closing. They can keep running in the background: the Agents tab shows them and opens them again. They end when wtf quits.")
            + "\n\n" + lines.joined(separator: "\n")
        alert.addButton(withTitle: "Keep Running in Background")
        alert.addButton(withTitle: "End Claude Code")
        alert.addButton(withTitle: "Cancel")
        alert.buttons[1].hasDestructiveAction = true
        alert.showsSuppressionButton = true
        alert.suppressionButton?.title = "Don't ask again (agent-close)"
        alert.beginSheetModal(for: window) { r in
            guard r == .alertFirstButtonReturn || r == .alertSecondButtonReturn else { return }
            let keep = r == .alertFirstButtonReturn
            if alert.suppressionButton?.state == .on {
                Task { try? await Services.config.SetAgentClose(keep ? "background" : "end") }
            }
            done(keep)
        }
    }

    /// Start Claude Code…: a folder and an optional first prompt, then claude
    /// there in a new tab or in the background.
    func startAgent() {
        let f = FormSheet(title: "Start Claude Code", subtitle: "Runs claude in a new shell in this folder, with a first prompt if you give one.")
        let folder = f.text(model.activePane?.cwd ?? NSHomeDirectory())
        let choose = ClosureButton(title: "Choose…") { [weak f, weak folder] in
            let p = NSOpenPanel()
            p.canChooseDirectories = true
            p.canChooseFiles = false
            p.prompt = "Choose"
            p.directoryURL = URL(fileURLWithPath: folder?.stringValue ?? NSHomeDirectory())
            guard let panel = f?.panel else { return }
            p.beginSheetModal(for: panel) { r in
                if r == .OK, let url = p.url { folder?.stringValue = url.path }
            }
        }
        let row = NSStackView(views: [folder, choose])
        f.field("Folder", row)
        folder.widthAnchor.constraint(greaterThanOrEqualToConstant: 300).isActive = true
        let prompt = f.text(placeholder: "First prompt (optional)")
        f.field("Prompt", prompt)
        f.buttons([
            ("Start in Background", false, { [weak self, weak f] in
                f?.close()
                self?.startAgent(in: folder.stringValue, prompt: prompt.stringValue, background: true)
            }),
            ("Start in a Tab", true, { [weak self, weak f] in
                f?.close()
                self?.startAgent(in: folder.stringValue, prompt: prompt.stringValue, background: false)
            }),
        ])
        f.present(in: window, focus: prompt)
    }

    /// Runs claude (with a first prompt) in a new shell in `folder`: a tab
    /// here, or the background window.
    func startAgent(in folder: String, prompt: String, background: Bool) {
        let dir = folder.trimmingCharacters(in: .whitespaces).isEmpty ? NSHomeDirectory() : folder.trimmingCharacters(in: .whitespaces)
        let command = Background.command(prompt: prompt.trimmingCharacters(in: .whitespacesAndNewlines))
        if background {
            WindowManager.shared.startInBackground(cwd: dir, command: command)
            Toast.show("Claude Code started in the background: the Agents tab shows it", in: rootView)
        } else {
            model.open(cwd: dir, command: command)
        }
    }
}

@MainActor
enum Background {
    /// What a new agent's shell runs: claude, through the shell
    /// integration's wrapper (agent-status), with a first prompt.
    static func command(prompt: String) -> String { prompt.isEmpty ? "claude" : "claude " + Shell.quote(prompt) }

    /// Brings a background agent back into a tab of the window in front (a
    /// new window when none shows).
    static func open(_ session: String) {
        guard let bg = WindowManager.shared.background, let p = bg.model.pane(session: session), let t = bg.model.tabOf(pane: p.id) else { return }
        if let front = WindowManager.shared.keyWindow.flatMap({ $0.background ? nil : $0 }) ?? WindowManager.shared.visibleWindows.first {
            bg.moveTab(t.id, to: front)
        } else {
            bg.moveToOwnWindow(t.id)
        }
        // agent-group: back in a window's tabs, it joins "agents".
        DispatchQueue.main.async { Agents.autoGroup(session) }
    }

    /// Ends an agent (its shell too), after asking.
    static func end(_ session: String, from parent: NSWindow?) {
        guard let (w, p) = Agents.find(session) else { return }
        let alert = NSAlert()
        alert.messageText = "End this Claude Code?"
        let title = w.model.tabOf(pane: p.id).map { w.model.title($0) } ?? session
        alert.informativeText = Redaction.shared.text(title) + "\n\nIts shell closes too."
        alert.addButton(withTitle: "End")
        alert.addButton(withTitle: "Cancel")
        alert.buttons[0].hasDestructiveAction = true
        let id = p.id
        let done: (NSApplication.ModalResponse) -> Void = { r in
            if r == .alertFirstButtonReturn { w.model.closePane(id) }
        }
        if let parent { alert.beginSheetModal(for: parent, completionHandler: done) } else { done(alert.runModal()) }
    }

    /// The Peek window last shown (for the self-checks).
    static var lastPeek: PeekWindow?

    /// Shows an agent's screen, updated every second while open, with a reply
    /// box. Nothing is typed unless you click.
    static func peek(_ session: String) {
        let p = PeekWindow(session: session)
        lastPeek = p
        p.show()
    }
}

/// An agent's screen (Hide Secrets applied) and a reply box: what you type is
/// sent with Enter; Enter and Esc go as keys, for Claude Code's own prompts.
@MainActor
final class PeekWindow: NSObject, NSWindowDelegate {
    private static var open: [PeekWindow] = []
    let session: String
    let panel: NSPanel
    let screen = NSTextView()
    let reply = NSTextField()
    private let state = NSTextField(labelWithString: "")
    private var timer: Timer?

    init(session: String) {
        self.session = session
        panel = NSPanel(contentRect: NSRect(x: 0, y: 0, width: 760, height: 520), styleMask: [.titled, .closable, .resizable, .utilityWindow],
                        backing: .buffered, defer: false)
        super.init()
        panel.isReleasedWhenClosed = false
        panel.delegate = self
        let title = Agents.find(session).flatMap { w, p in w.model.tabOf(pane: p.id).map { w.model.title($0) } } ?? session
        panel.title = "Claude Code — " + Redaction.shared.text(title)

        screen.isEditable = false
        screen.isSelectable = true
        screen.font = .monospacedSystemFont(ofSize: 11.5, weight: .regular)
        screen.textContainerInset = NSSize(width: 6, height: 6)
        let scroll = NSScrollView()
        scroll.documentView = screen
        scroll.hasVerticalScroller = true
        screen.autoresizingMask = [.width]
        state.textColor = .secondaryLabelColor
        state.lineBreakMode = .byTruncatingTail
        reply.placeholderString = "Reply to Claude Code (sent with Return)"
        reply.target = self
        reply.action = #selector(sendReply)
        func button(_ title: String, _ tip: String, _ run: @escaping () -> Void) -> NSButton {
            let b = ClosureButton(title: title, run)
            b.toolTip = tip
            return b
        }
        let replyRow = NSStackView(views: [reply,
                                           button("Send", "Type it in Claude Code, then Return") { [weak self] in self?.sendReply() },
                                           button("Return", "Press Return in Claude Code (accepts what its prompt has selected)") { [weak self] in self?.send("\r") },
                                           button("Esc", "Press Esc in Claude Code (cancels its prompt or stops it)") { [weak self] in self?.send("\u{1b}") }])
        reply.setContentHuggingPriority(.init(1), for: .horizontal)
        let actions = NSStackView(views: [
            button("Open in Tab", "Show it in a tab") { [weak self] in
                guard let self else { return }
                panel.close()
                Agents.go(to: session)
            },
            button("End…", "End this Claude Code and its shell") { [weak self] in Background.end(session, from: self?.panel) },
        ])
        let stack = NSStackView(views: [state, scroll, replyRow, actions])
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 8
        stack.edgeInsets = NSEdgeInsets(top: 10, left: 10, bottom: 10, right: 10)
        stack.translatesAutoresizingMaskIntoConstraints = false
        panel.contentView = stack
        for v in [state, scroll, replyRow] {
            v.widthAnchor.constraint(equalTo: stack.widthAnchor, constant: -20).isActive = true
        }
        scroll.setContentHuggingPriority(.init(1), for: .vertical)
        scroll.heightAnchor.constraint(greaterThanOrEqualToConstant: 200).isActive = true
    }

    func show() {
        Self.open.append(self)
        refresh()
        timer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated { self?.refresh() }
        }
        panel.center()
        panel.makeKeyAndOrderFront(nil)
        panel.makeFirstResponder(reply)
    }

    private var term: TerminalController? {
        Agents.find(session).flatMap { w, p in w.terminals[p.id] }
    }

    /// What the screen shows now (for the self-checks).
    var screenText: String { screen.string }

    private func refresh() {
        guard let (w, p) = Agents.find(session), let t = w.terminals[p.id] else {
            state.stringValue = "It's gone: Claude Code quit or its shell closed."
            timer?.invalidate()
            return
        }
        let text = t.snapshot(rows: 40).joined(separator: "\n")
        if screen.string != text {
            screen.string = text
            screen.scrollToEndOfDocument(nil)
        }
        var what = "Not reporting"
        if let st = Agents.all[session] {
            let s = Agents.state(st)
            what = s.label + (s == .waiting && !st.message.isEmpty ? ": " + Redaction.shared.text(st.message) : "")
        }
        state.stringValue = [what, w.background ? "In the background" : "In a tab", WindowModel.prettyPath(p.cwd)].joined(separator: " · ")
    }

    func send(_ text: String) {
        term?.type(text)
    }

    @objc func sendReply() {
        let text = reply.stringValue
        guard !text.trimmingCharacters(in: .whitespaces).isEmpty else { return }
        send(text)
        // Apart from the text, so it isn't taken as part of a paste.
        Task { @MainActor [weak self] in
            try? await Task.sleep(for: .milliseconds(200))
            self?.send("\r")
        }
        reply.stringValue = ""
    }

    func close() { panel.close() }

    func windowWillClose(_ notification: Notification) {
        timer?.invalidate()
        Self.open.removeAll { $0 === self }
        if Background.lastPeek === self { Background.lastPeek = nil }
    }
}

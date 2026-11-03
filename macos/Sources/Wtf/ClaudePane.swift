// Ask Claude (#32): a split beside a shell running the user's own Claude
// Code with wtf's plugin, whose tools work on that shell (its target). The
// plugin's hooks report what Claude Code is doing (bridge:status): the tab
// shows it, and wtf notifies when it needs you or finishes while you're
// looking elsewhere. Kept by backend session, so it survives a tab moving
// to another window.
import AppKit
import WtfCore

private struct BridgeStatus: Decodable {
    let session: String
    let state: String
    let message: String?
}

@MainActor
enum ClaudePane {
    enum State: String { case idle, busy, waiting }

    /// Claude pane session → the shell session it works on.
    private static var targets: [String: String] = [:]
    /// Claude pane session → what Claude Code is doing (none before its
    /// first hook, and after it exits).
    private static var states: [String: State] = [:]
    /// Claude panes split but not started yet → their target.
    private static var pending: [ObjectIdentifier: String] = [:]
    /// What to type in a Claude pane once Claude Code is ready for a prompt:
    /// a `/wtf:` command, with Enter or for you to finish. By session; for a
    /// pane not started yet, by pane.
    struct Message { let text: String; let enter: Bool }
    private static var queued: [String: Message] = [:]
    private static var pendingMessages: [ObjectIdentifier: Message] = [:]
    private static var listening = false

    static func listen() {
        guard !listening else { return }
        listening = true
        Backend.on("bridge:status", BridgeStatus.self) { apply($0) }
    }

    private static func find(_ session: String) -> (WindowController, Pane)? {
        for w in WindowManager.shared.windows {
            if let p = w.model.pane(session: session) { return (w, p) }
        }
        return nil
    }

    static func isClaude(_ p: Pane) -> Bool { target(of: p) != nil }

    /// The shell session a Claude pane works on.
    static func target(of p: Pane) -> String? { p.sessionId.flatMap { targets[$0] } }

    /// The badge for a tab: the state of a Claude pane in it.
    static func state(tab: Int, in w: WindowController) -> State? {
        guard let leaves = w.model.tab(tab)?.root.leaves else { return nil }
        var found: State?
        for id in leaves {
            guard let s = w.model.panes[id]?.sessionId, targets[s] != nil else { continue }
            let st = states[s] ?? .idle
            if st == .waiting { return .waiting }
            if found == nil || st == .busy { found = st }
        }
        return found
    }

    /// Opens the Claude pane for the focused shell, or goes to the one it
    /// has; then types `send` there once Claude Code is ready. Nothing is
    /// typed while it starts (its folder-trust question comes first) or
    /// works: it waits for its next prompt.
    static func ask(in w: WindowController, send: Message? = nil) {
        guard let t = w.model.active, let src = w.model.focusedPane(t), src.kind == .terminal, let session = src.sessionId else {
            Toast.show("Ask Claude works on a shell: focus one first", in: w.rootView)
            return
        }
        if targets[session] != nil {
            if let send { deliver(send, to: session, in: w) } else {
                Toast.show("This is a Claude pane: its tools work on the shell it was opened from", in: w.rootView)
            }
            return
        }
        Task { @MainActor in
            let command: String
            do {
                command = try await Services.bridge.ClaudeCommand()
            } catch {
                Toast.show("Can't start Claude Code: \(error.localizedDescription)", in: w.rootView)
                return
            }
            if let (cs, _) = targets.first(where: { $0.value == session }), let (cw, cp) = find(cs) {
                // Back to its Claude pane; start claude again if it was quit.
                if let tab = cw.model.tabOf(pane: cp.id) { cw.model.activate(tab.id) }
                cw.model.focusPane(cp.id)
                cw.window?.makeKeyAndOrderFront(nil)
                if !cp.busy {
                    states[cs] = nil
                    cw.terminals[cp.id]?.run(command)
                }
                if let send { deliver(send, to: cs, in: cw) }
                return
            }
            guard w.model.active.flatMap({ w.model.focusedPane($0) }) === src,
                  let id = w.model.split(.row, cwd: src.cwd, command: command), let p = w.model.panes[id] else { return }
            pending[ObjectIdentifier(p)] = session
            if let send { pendingMessages[ObjectIdentifier(p)] = send }
        }
    }

    /// Types a message in Claude pane `session` now if Claude Code waits
    /// for a prompt, else when it next does (a newer one replaces it).
    private static func deliver(_ m: Message, to session: String, in w: WindowController) {
        if states[session] == .idle {
            type(m, session)
        } else {
            if states[session] == .busy || states[session] == .waiting {
                Toast.show("Claude Code is busy: wtf types this when it’s done", in: w.rootView)
            }
            queued[session] = m
        }
    }

    private static func type(_ m: Message, _ session: String) {
        guard let (w, p) = find(session), let term = w.terminals[p.id] else { return }
        term.type(m.text)
        if m.enter {
            // Apart from the text, so it isn't taken as part of a paste.
            Task { @MainActor in
                try? await Task.sleep(for: .milliseconds(200))
                term.type("\r")
            }
        }
        if w.model.active?.root.leaves.contains(p.id) == true { w.model.focusPane(p.id) }
    }

    /// A pane's shell started: a Claude pane starts working on its target.
    static func started(_ p: Pane) {
        guard let target = pending.removeValue(forKey: ObjectIdentifier(p)), let s = p.sessionId else { return }
        targets[s] = target
        if let m = pendingMessages.removeValue(forKey: ObjectIdentifier(p)) { queued[s] = m }
        Task { try? await Services.bridge.SetTarget(s, target) }
    }

    /// A shell closed (not moved to another window).
    static func closed(session: String) {
        guard targets.removeValue(forKey: session) != nil else { return }
        states[session] = nil
        queued[session] = nil
        Task { try? await Services.bridge.SetTarget(session, "") }
    }

    private static func apply(_ st: BridgeStatus) {
        guard targets[st.session] != nil, let (w, p) = find(st.session) else { return }
        let old = states[st.session]
        let new = State(rawValue: st.state)
        states[st.session] = new
        w.refreshStrip()
        if new == .idle, let m = queued.removeValue(forKey: st.session) { type(m, st.session) }
        let visible = w.model.active?.root.leaves.contains(p.id) == true && w.window?.isKeyWindow == true && NSApp.isActive
        guard !visible, let tab = w.model.tabOf(pane: p.id) else { return }
        let title = w.model.title(tab)
        switch new {
        case .waiting:
            let body = Redaction.shared.text(st.message ?? "")
            Task { try? await Backend.run("notify", "Notify", ["Claude Code needs you", title, body]) }
        case .idle where old == .busy:
            Task { try? await Backend.run("notify", "Notify", ["Claude Code finished", title, ""]) }
        default:
            break
        }
    }
}

/// #24–#27 through the Claude pane's slash commands (claude-plugin/commands).
extension ClaudePane {
    /// Why block `id` of the focused shell failed.
    static func explain(block id: Int, in w: WindowController) {
        ask(in: w, send: Message(text: "/wtf:explain block \(id)", enter: true))
    }

    /// A command from plain English: `/wtf:cmd ` for you to finish.
    static func command(in w: WindowController) { ask(in: w, send: Message(text: "/wtf:cmd ", enter: false)) }

    /// An investigation: `/wtf:investigate ` for you to say what's wrong.
    static func investigate(in w: WindowController) { ask(in: w, send: Message(text: "/wtf:investigate ", enter: false)) }

    /// A summary of a change preview: block `id`, or a saved plan file.
    static func review(block id: Int? = nil, file: String? = nil, in w: WindowController) {
        let what = file.map { " plan file \(Shell.quote($0))" } ?? id.map { " block \($0)" } ?? ""
        ask(in: w, send: Message(text: "/wtf:review" + what, enter: true))
    }
}

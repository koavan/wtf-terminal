import AppKit
import WtfCore

/// Agent runners (#41): remote Linux machines, reached over SSH, where
/// Claude Code agents run with the Claude account you sign in to there,
/// never an API key. Phase 1: add one from an SSH host, check it (facts
/// only), install wtf-runner after you confirm, and open a tab for what
/// needs you there (installing Claude Code or tmux, signing in, first-run
/// setup). Listed in the Agents tab; like internal/gtkui/runners.go.
@MainActor
enum Runners {
    private(set) static var list: [Go.runner.Status] = []
    /// Runners being checked or installed right now.
    private(set) static var busy: Set<String> = []
    /// Each runner's agents and its connection (#41 phase 3), from its
    /// wtf-runner stream.
    private(set) static var agents: [String: Go.runner.Agents] = [:]
    private static var loaded = false
    private static var observers = Observers()

    static func observe<T: AnyObject>(_ owner: T, _ fn: @escaping (T) -> Void) {
        observers.add(owner, fn)
        load()
    }

    private static func changed() { observers.notify() }

    /// Reads the saved runners and checks each once: only when something
    /// shows them (load on demand), never on a timer.
    static func load() {
        guard !loaded else { return }
        loaded = true
        Backend.on("runner:agents", Go.runner.Agents.self) { apply($0) }
        Task {
            list = (try? await Services.runner.List()) ?? []
            changed()
            for r in list { check(r.id) }
        }
    }

    private static func put(_ st: Go.runner.Status) {
        if let i = list.firstIndex(where: { $0.id == st.id }) { list[i] = st } else { list.append(st) }
    }

    static func check(_ id: String) {
        guard !busy.contains(id) else { return }
        busy.insert(id)
        changed()
        Task {
            if let st = try? await Services.runner.Check(id) { put(st) }
            busy.remove(id)
            changed()
        }
    }

    // MARK: Agents

    /// A runner's agents changed. Their notifications come from Agents,
    /// where they're mirrored (the bridge's status, #41 phase 5).
    private static func apply(_ ev: Go.runner.Agents) {
        guard list.contains(where: { $0.id == ev.runner }) else { return }
        agents[ev.runner] = ev
        changed()
    }

    // MARK: Sessions

    /// A runner's agent's session in wtf's bridge (runner.Session).
    static func session(_ runner: String, _ agent: String) -> String { "rr:\(runner)|\(agent)" }

    static func isRemote(_ session: String) -> Bool { session.hasPrefix("rr:") }

    /// The runner and agent a session names.
    static func parse(_ session: String) -> (runner: String, agent: String)? {
        guard isRemote(session), let bar = session.lastIndex(of: "|") else { return nil }
        return (String(session[session.index(session.startIndex, offsetBy: 3)..<bar]), String(session[session.index(after: bar)...]))
    }

    /// "demo on build-box", for its notifications and cards.
    static func subtitle(_ st: Go.bridge.Status) -> String {
        guard let (r, _) = parse(st.session), let name = list.first(where: { $0.id == r })?.name else { return st.name }
        return "\(st.name) on \(name)"
    }

    // MARK: Open

    /// Tabs showing a runner's agent, by session: a click goes back to its
    /// tab rather than opening another.
    private static var tabs: [String: (window: WindowController, tab: Int)] = [:]

    static func open(session: String) {
        guard let (r, a) = parse(session) else { return }
        open(runner: r, agent: a)
    }

    /// Shows an agent's terminal: its tmux session on the runner, in a tab
    /// (ssh -t … tmux attach). Detach (Ctrl-b d) or close the tab: it keeps
    /// running.
    static func open(runner: String, agent: String) {
        let key = session(runner, agent)
        Task { try? await Services.runner.SeenAgent(runner, agent) }
        if let t = tabs[key], t.window.window != nil, t.window.model.tab(t.tab) != nil {
            t.window.model.activate(t.tab)
            NSApp.activate(ignoringOtherApps: true)
            t.window.window?.makeKeyAndOrderFront(nil)
            return
        }
        guard let w = WindowManager.shared.keyWindow ?? WindowManager.shared.windows.first(where: { !$0.background }) else { return }
        let ag = agents[runner]?.agents.first { $0.id == agent }
        Task {
            do {
                let cmd = try await Services.runner.OpenCommand(runner, agent)
                let id = w.model.open(command: cmd)
                let name = list.first { $0.id == runner }?.name ?? ""
                w.model.rename(id, (ag.map(title) ?? agent) + " · " + name)
                tabs[key] = (w, id)
                NSApp.activate(ignoringOtherApps: true)
                w.window?.makeKeyAndOrderFront(nil)
            } catch {
                Toast.show(error.localizedDescription, in: w.rootView)
            }
        }
    }

    // MARK: Start

    /// The runners agents can start on now: set up and connected.
    static var ready: [Go.runner.Status] {
        list.filter { $0.facts?.reachable == true && $0.problems.isEmpty && agents[$0.id]?.connected == true }
    }

    static let modes: [(label: String, mode: String)] = [
        ("Claude Code's default", ""), ("Ask before every command and edit", "default"), ("Accept edits, ask for commands", "acceptEdits"),
        ("Plan only", "plan"), ("Auto (Claude Code decides)", "auto"),
    ]

    /// Start on Runner…: a folder there, a first prompt, a permission mode
    /// and a name; then it runs in the background, or in a tab too.
    static func start(on runner: String? = nil, owner: WindowController) {
        let rs = ready
        guard !rs.isEmpty else {
            Toast.show(list.isEmpty ? "Add a runner first (Agents tab → Add Runner…)" : "No runner is ready: see the Agents tab", in: owner.rootView)
            return
        }
        let f = FormSheet(title: "Start Claude Code on a Runner",
                          subtitle: "It runs on the runner in tmux, signed in to your Claude account there, and keeps running when wtf quits or this Mac sleeps.")
        let pick = f.popup(rs.map(\.name), selected: rs.firstIndex { $0.id == runner } ?? 0)
        f.field("Runner", pick)
        func lastDir(_ r: Go.runner.Status) -> String {
            agents[r.id]?.agents.last.map { path($0.dir, on: r.id) } ?? "~"
        }
        let folder = f.text(lastDir(rs[pick.indexOfSelectedItem]), placeholder: "~/project")
        f.field("Folder", folder, help: "A folder on the runner")
        let prompt = f.text(placeholder: "First prompt (optional)")
        f.field("Prompt", prompt)
        let mode = f.popup(modes.map(\.label))
        f.field("Permissions", mode, help: "Requests that need you show here and over the terminal")
        let name = f.text(placeholder: "optional")
        f.field("Name", name)
        func go(open: Bool) {
            let r = rs[pick.indexOfSelectedItem]
            let m = modes[mode.indexOfSelectedItem].mode
            Task {
                do {
                    let a = try await Services.runner.Start(r.id, folder.stringValue, prompt.stringValue, m, name.stringValue)
                    if open { self.open(runner: r.id, agent: a.id) } else {
                        Toast.show("Claude Code started on \(r.name): the Agents tab shows it", in: owner.rootView)
                    }
                } catch {
                    Toast.show(error.localizedDescription, in: owner.rootView, seconds: 5)
                }
            }
        }
        f.buttons([
            ("Start in Background", false, { [weak f] in f?.close(); go(open: false) }),
            ("Start and Open", true, { [weak f] in f?.close(); go(open: true) }),
        ])
        f.present(in: owner.window, focus: folder)
    }

    // MARK: Peek and Reply

    /// The end of an agent's screen, and a line to type into it.
    static func peek(runner: String, agent: String, in window: NSWindow?) {
        Task {
            let text: String
            do { text = try await Services.runner.Peek(runner, agent) } catch {
                if let v = window?.contentView { Toast.show(error.localizedDescription, in: v) }
                return
            }
            let ag = agents[runner]?.agents.first { $0.id == agent }
            let f = FormSheet(title: "Peek and Reply", subtitle: (ag.map(title) ?? agent) + ": the end of its screen", width: 640)
            let shown = text.split(separator: "\n", omittingEmptySubsequences: false).suffix(30).joined(separator: "\n")
            f.add(f.note(Redaction.shared.text(shown), mono: true))
            let reply = f.text(placeholder: "Type a reply; Send presses Enter")
            f.field("Reply", reply)
            f.buttons([
                ("Open", false, { [weak f] in f?.close(); open(runner: runner, agent: agent) }),
                ("Send", true, { [weak f] in
                    let t = reply.stringValue
                    f?.close()
                    Task {
                        do { try await Services.runner.Reply(runner, agent, t) } catch {
                            if let v = window?.contentView { Toast.show(error.localizedDescription, in: v) }
                        }
                    }
                }),
            ])
            f.present(in: window, focus: reply)
        }
    }

    /// A folder on a runner, with its home as ~.
    static func path(_ dir: String, on runner: String) -> String {
        guard let home = list.first(where: { $0.id == runner })?.facts?.home, !home.isEmpty else { return dir }
        if dir == home { return "~" }
        return dir.hasPrefix(home + "/") ? "~" + dir.dropFirst(home.count) : dir
    }

    /// An agent's name, else its folder's.
    static func title(_ a: Go.runner.RemoteAgent) -> String {
        a.name.isEmpty ? (a.dir as NSString).lastPathComponent : a.name
    }

    static func agentMenu(_ a: Go.runner.RemoteAgent, runner: String, in view: NSView?) -> NSMenu {
        let m = NSMenu()
        func add(_ t: String, _ run: @escaping () -> Void) { m.addItem(ClosureMenuItem(title: t, keyEquivalent: "", action: run)) }
        if a.alive {
            add("Open in Tab") { open(runner: runner, agent: a.id) }
            add("Peek and Reply…") { peek(runner: runner, agent: a.id, in: view?.window) }
        }
        if a.status?.unseen == true { add("Mark as Seen") { Task { try? await Services.runner.SeenAgent(runner, a.id) } } }
        add("Rename…") {
            let f = FormSheet(title: "Rename Agent")
            let name = f.text(a.name, placeholder: (a.dir as NSString).lastPathComponent)
            f.field("Name", name)
            f.buttons([("Rename", true, { [weak f] in
                f?.close()
                Task { try? await Services.runner.RenameAgent(runner, a.id, name.stringValue) }
            })])
            f.present(in: view?.window, focus: name)
        }
        add("Copy Folder Path") {
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(a.dir, forType: .string)
        }
        m.addItem(.separator())
        if a.alive {
            add("End Claude Code…") {
                Confirm.show(in: view?.window, title: "End \(title(a))?", message: "Its tmux session on the runner closes, and Claude Code with it.", button: "End") {
                    Task { try? await Services.runner.EndAgent(runner, a.id) }
                }
            }
        } else {
            add("Remove from List") { Task { try? await Services.runner.ForgetAgent(runner, a.id) } }
        }
        return m
    }

    // MARK: Add

    /// Add Runner…: an SSH host (from the SSH tab's hosts) and a name.
    static func add(host: SshHost? = nil, owner: WindowController) {
        Task {
            let hosts = (try? await Services.sshhosts.List())?.hosts ?? []
            guard !hosts.isEmpty else {
                Toast.show("Add the machine as an SSH host first (SSH tab)", in: owner.rootView)
                return
            }
            let f = FormSheet(title: "Add Agent Runner",
                              subtitle: "A Linux machine where Claude Code agents run, signed in to your own Claude account (no API key). wtf reaches it over SSH with your keys; adding it changes nothing there.")
            let pick = f.popup(hosts.map { $0.known ? "\($0.alias) (known host)" : $0.alias },
                               selected: host.flatMap { h in hosts.firstIndex { Ssh.key($0) == Ssh.key(h) } } ?? 0)
            f.field("SSH host", pick)
            let name = f.text(placeholder: "the host's name")
            f.field("Name", name, help: "What wtf calls it")
            f.buttons([("Add", true, { [weak f] in
                let h = hosts[pick.indexOfSelectedItem]
                f?.close()
                Task {
                    do {
                        let st = try await Services.runner.Add(h.alias, h.known ? h.port : "", name.stringValue)
                        put(st)
                        changed()
                        check(st.id)
                    } catch {
                        Toast.show(error.localizedDescription, in: owner.rootView)
                    }
                }
            })])
            f.present(in: owner.window, focus: pick)
        }
    }

    // MARK: Fixes

    /// What to do next on a runner, from its facts, in the order it has to
    /// happen; nil when it's ready.
    static func fix(_ st: Go.runner.Status) -> (title: String, run: (WindowController) -> Void)? {
        guard let f = st.facts else { return nil }
        let id = st.id
        if !f.reachable { return ("Open a Shell There…", { tab(id, "shell", "SSH", $0) }) }
        if f.os != "Linux" || f.goarch.isEmpty { return nil }
        if f.tmux.isEmpty, !f.packageManager.isEmpty { return ("Install tmux…", { tab(id, "install-tmux", "Install tmux", $0) }) }
        if f.claude.isEmpty { return ("Install Claude Code…", { tab(id, "install-claude", "Install Claude Code", $0) }) }
        if !f.signedIn || f.authMethod != "claude.ai" { return ("Sign In…", { tab(id, "signin", "Sign in", $0) }) }
        if !f.onboarded { return ("Finish Claude Code Setup…", { tab(id, "setup", "Claude Code setup", $0) }) }
        if f.runnerVersion.isEmpty || st.outdated {
            return (f.runnerVersion.isEmpty ? "Install wtf-runner…" : "Update wtf-runner…", { install(id, owner: $0) })
        }
        return nil
    }

    /// Panes of tabs opened to fix a runner (install, sign in, setup), by
    /// window and pane: when their command ends, the runner is checked
    /// again.
    private static var fixing: [ObjectIdentifier: [Int: String]] = [:]

    /// Opens a tab running what needs you on the runner. When its command
    /// ends (you finished, or left), the runner is checked again.
    static func tab(_ id: String, _ what: String, _ title: String, _ owner: WindowController) {
        Task {
            do {
                let cmd = try await Services.runner.TabCommand(id, what)
                let name = list.first { $0.id == id }?.name ?? ""
                let t = owner.model.open(command: cmd)
                owner.model.rename(t, "\(title) · \(name)")
                if what != "shell", let pane = owner.model.tab(t)?.focusedPaneId {
                    fixing[ObjectIdentifier(owner), default: [:]][pane] = id
                }
                owner.window?.makeKeyAndOrderFront(nil)
            } catch {
                Toast.show(error.localizedDescription, in: owner.rootView)
            }
        }
    }

    /// A command ended in a pane: if it was a runner's fix, check the
    /// runner again so its status shows what changed.
    static func commandEnded(in owner: WindowController, pane: Int) {
        guard let id = fixing[ObjectIdentifier(owner)]?.removeValue(forKey: pane) else { return }
        check(id)
    }

    /// Signs Claude Code out on the runner (claude auth logout), after you
    /// confirm, in a tab; the runner is checked again when it ends.
    static func signOut(_ st: Go.runner.Status, owner: WindowController) {
        let who = st.facts.map(\.account).flatMap { $0.isEmpty ? nil : " (\($0))" } ?? ""
        Confirm.show(in: owner.window, title: "Sign Claude Code out on \(st.name)?",
                     message: "Claude Code on \(st.alias) signs out of your Claude account\(who). Agents can't start there until you sign in again.",
                     button: "Sign Out") {
            tab(st.id, "signout", "Sign out", owner)
        }
    }

    static func install(_ id: String, owner: WindowController) {
        guard let st = list.first(where: { $0.id == id }), let f = st.facts else { return }
        let update = !f.runnerVersion.isEmpty
        Confirm.show(in: owner.window, title: (update ? "Update" : "Install") + " wtf-runner on \(st.name)?",
                     message: "wtf copies these to ~/.local/share/wtf-runner on \(st.alias) over SSH" + (update ? ", replacing version \(f.runnerVersion)" : "") + ". Nothing else there changes.",
                     items: ["wtf-runner (holds the agents wtf starts there)", "wtf-mcp (Claude Code's hook helper)", "status-settings.json (the hooks that report each agent's state)"],
                     button: update ? "Update" : "Install", destructive: false) {
            busy.insert(id)
            changed()
            Task {
                do { put(try await Services.runner.Install(id)) } catch {
                    Toast.show(error.localizedDescription, in: owner.rootView, seconds: 5)
                }
                busy.remove(id)
                changed()
            }
        }
    }

    // MARK: Menu

    static func menu(_ st: Go.runner.Status, owner: WindowController, in view: NSView?) -> NSMenu {
        let m = NSMenu()
        let id = st.id
        func add(_ t: String, enabled: Bool = true, _ run: @escaping () -> Void) {
            let i = ClosureMenuItem(title: t, keyEquivalent: "", action: run)
            i.isEnabled = enabled
            m.addItem(i)
        }
        add("Check Again", enabled: !busy.contains(id)) { check(id) }
        if let c = agents[id], !c.connected { add("Reconnect Now") { Task { try? await Services.runner.Reconnect(id) } } }
        if let fx = fix(st) { add(fx.title) { fx.run(owner) } }
        add("Start Claude Code…", enabled: ready.contains { $0.id == id }) { start(on: id, owner: owner) }
        m.addItem(.separator())
        add("Open a Shell There") { tab(id, "shell", "SSH", owner) }
        if let f = st.facts, f.reachable {
            if !f.claude.isEmpty { add(f.signedIn ? "Sign In Again…" : "Sign In…") { tab(id, "signin", "Sign in", owner) } }
            if !f.claude.isEmpty, f.signedIn { add("Sign Out…") { signOut(st, owner: owner) } }
            if !f.runnerVersion.isEmpty, fix(st)?.title.hasSuffix("wtf-runner…") != true {
                add("Reinstall wtf-runner…") { install(id, owner: owner) }
            }
        }
        add("Rename…") { rename(st, in: view?.window) }
        m.addItem(.separator())
        add("Remove Runner…") { remove(st, owner: owner) }
        return m
    }

    static func rename(_ st: Go.runner.Status, in window: NSWindow?) {
        let f = FormSheet(title: "Rename Runner")
        let name = f.text(st.name)
        f.field("Name", name)
        f.buttons([("Rename", true, { [weak f] in
            f?.close()
            Task {
                try? await Services.runner.Rename(st.id, name.stringValue)
                list = (try? await Services.runner.List()) ?? list
                changed()
            }
        })])
        f.present(in: window, focus: name)
    }

    /// Forgets the runner; optionally removes wtf-runner from it too.
    static func remove(_ st: Go.runner.Status, owner: WindowController) {
        guard let window = owner.window else { return }
        let alert = NSAlert()
        alert.messageText = "Remove \(st.name) from wtf?"
        alert.informativeText = "Claude Code, its sign-in and your files on \(st.alias) stay as they are."
        alert.addButton(withTitle: "Remove")
        alert.addButton(withTitle: "Cancel")
        let also = NSButton(checkboxWithTitle: "Also delete ~/.local/share/wtf-runner there", target: nil, action: nil)
        also.state = .off
        alert.accessoryView = also
        alert.beginSheetModal(for: window) { r in
            guard r == .alertFirstButtonReturn else { return }
            Task {
                do {
                    if also.state == .on { try await Services.runner.Uninstall(st.id) }
                    try await Services.runner.Remove(st.id)
                    list.removeAll { $0.id == st.id }
                    agents[st.id] = nil
                    changed()
                } catch {
                    Toast.show(error.localizedDescription, in: owner.rootView, seconds: 5)
                }
            }
        }
    }

    // MARK: Text

    /// "Linux arm64 · tmux 3.3a · Claude Code 2.1.327".
    static func system(_ f: Go.runner.Facts) -> String {
        [f.os.isEmpty ? "" : "\(f.os) \(f.goarch.isEmpty ? f.arch : f.goarch)", f.tmux,
         f.claudeVersion.isEmpty ? "" : "Claude Code \(f.claudeVersion)",
         f.runnerVersion.isEmpty ? "" : "wtf-runner \(f.runnerVersion)"].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    /// "Signed in as me@x.com (team) · until Dec 17".
    static func account(_ f: Go.runner.Facts) -> String {
        guard f.signedIn else { return "" }
        var s = "Signed in" + (f.account.isEmpty ? "" : " as \(f.account)") + (f.plan.isEmpty ? "" : " (\(f.plan))")
        if f.authMethod != "claude.ai" { s += " with \(f.authMethod)" }
        let iso = ISO8601DateFormatter()
        iso.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let d = iso.date(from: f.loginExpires) {
            s += " · until " + d.formatted(.dateTime.month(.abbreviated).day())
        }
        return s
    }
}

/// One runner in the Agents tab: ready (green), needs something (orange),
/// can't be reached (red), not checked yet (grey); its facts; and the next
/// thing to do there as a link.
@MainActor
final class RunnerRow: NSView {
    private let st: Go.runner.Status
    private unowned let owner: WindowController

    init(_ st: Go.runner.Status, owner: WindowController) {
        self.st = st
        self.owner = owner
        super.init(frame: .zero)
        let busy = Runners.busy.contains(st.id)
        let f = st.facts
        let color: NSColor = f == nil ? .tertiaryLabelColor : !f!.reachable ? .systemRed : st.problems.isEmpty && !st.outdated ? .systemGreen : .systemOrange

        let dot = NSView()
        dot.wantsLayer = true
        dot.layer?.cornerRadius = 4
        dot.layer?.backgroundColor = color.cgColor
        dot.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([dot.widthAnchor.constraint(equalToConstant: 8), dot.heightAnchor.constraint(equalToConstant: 8)])
        let name = NSTextField(labelWithString: st.name)
        name.font = .systemFont(ofSize: 12, weight: .semibold)
        name.lineBreakMode = .byTruncatingTail
        name.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        let spacer = NSView()
        spacer.setContentHuggingPriority(.init(1), for: .horizontal)
        let state = NSTextField(labelWithString: busy ? "checking…" : f == nil ? "" : !f!.reachable ? "unreachable" : st.problems.isEmpty && !st.outdated ? "ready" : "needs setup")
        state.font = .systemFont(ofSize: 10.5)
        state.textColor = .secondaryLabelColor
        let top = NSStackView(views: [dot, name, spacer, state])
        top.spacing = 6

        var lines: [NSView] = [top]
        func line(_ s: String, _ c: NSColor = .secondaryLabelColor, bold: Bool = false) {
            guard !s.isEmpty else { return }
            let l = NSTextField(wrappingLabelWithString: s)
            l.font = .systemFont(ofSize: 11, weight: bold ? .semibold : .regular)
            l.textColor = c
            l.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
            lines.append(l)
        }
        line(st.alias == st.name ? "" : st.alias)
        if let f {
            if f.reachable {
                if let conn = Runners.agents[st.id], !conn.connected, !conn.error.isEmpty {
                    line("Not connected: \(conn.error) (retrying)", .systemOrange)
                }
                line(Runners.system(f))
                line(Runners.account(f))
                if f.apiKeySet { line("ANTHROPIC_API_KEY is set there; agents wtf starts don't get it") }
            } else {
                line(f.error, .systemRed)
            }
            if let p = st.problems.first(where: { _ in f.reachable }) { line(p.prefix(1).uppercased() + p.dropFirst(), .systemOrange, bold: true) }
            else if st.outdated { line("Its wtf-runner (\(f.runnerVersion)) isn't this wtf's", .systemOrange, bold: true) }
        }
        if !busy, let fx = Runners.fix(st) {
            let owner = owner
            let run = ClosureButton(title: fx.title) { fx.run(owner) }
            run.bezelStyle = .inline
            run.controlSize = .small
            lines.append(run)
        } else if !busy, Runners.ready.contains(where: { $0.id == st.id }) {
            let owner = owner, id = st.id
            let run = ClosureButton(title: "Start Claude Code…") { Runners.start(on: id, owner: owner) }
            run.bezelStyle = .inline
            run.controlSize = .small
            lines.append(run)
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
        ])
        for v in lines.dropFirst() where v is NSTextField {
            v.widthAnchor.constraint(lessThanOrEqualTo: stack.widthAnchor, constant: -16).isActive = true
        }
        toolTip = "Right-click for more"
        setAccessibilityRole(.group)
        setAccessibilityLabel("Runner \(st.name): \(state.stringValue)")
    }

    required init?(coder: NSCoder) { fatalError() }

    override func menu(for event: NSEvent) -> NSMenu? { Runners.menu(st, owner: owner, in: self) }
}

/// One agent on a runner, under it: like an agent here, by its name or
/// folder, with what it's doing, its folder and its last prompt (or what it
/// needs). Before its first hook it's "starting" (Claude Code may be on a
/// first-run screen); once its tmux session ends, "ended".
@MainActor
final class RemoteAgentRow: NSView {
    private let agent: Go.runner.RemoteAgent
    private let runner: String

    init(_ a: Go.runner.RemoteAgent, runner: String) {
        agent = a
        self.runner = runner
        super.init(frame: .zero)
        let state = a.status.map(Agents.state)
        let label = !a.alive ? "Ended" + (a.exit.isEmpty ? "" : " (\(a.exit))") : state?.label ?? "Starting"
        let dot = NSView()
        dot.wantsLayer = true
        dot.layer?.cornerRadius = 4
        dot.layer?.backgroundColor = (a.alive ? state.map(Agents.color) ?? .tertiaryLabelColor : .tertiaryLabelColor).cgColor
        dot.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([dot.widthAnchor.constraint(equalToConstant: 7), dot.heightAnchor.constraint(equalToConstant: 7)])
        let name = NSTextField(labelWithString: Runners.title(a))
        name.font = .systemFont(ofSize: 11.5, weight: .semibold)
        name.lineBreakMode = .byTruncatingTail
        name.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        let spacer = NSView()
        spacer.setContentHuggingPriority(.init(1), for: .horizontal)
        let age = NSTextField(labelWithString: a.status.map { Agents.age(since: $0.since) } ?? "")
        age.font = .systemFont(ofSize: 10.5)
        age.textColor = .secondaryLabelColor
        let top = NSStackView(views: [dot, name, spacer, age])
        top.spacing = 6
        let what = NSTextField(labelWithString: [label, Runners.path(a.dir, on: runner)].joined(separator: " · "))
        what.font = .systemFont(ofSize: 11, weight: state == .waiting ? .semibold : .regular)
        what.textColor = state == .waiting && a.alive ? .systemOrange : .secondaryLabelColor
        what.lineBreakMode = .byTruncatingMiddle
        what.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        var lines: [NSView] = [top, what]
        let st = a.status
        let detail = st.map { $0.state == "waiting" && !$0.message.isEmpty ? $0.message : $0.prompt } ?? a.prompt
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
        stack.edgeInsets = NSEdgeInsets(top: 4, left: 22, bottom: 5, right: 8)
        stack.translatesAutoresizingMaskIntoConstraints = false
        addSubview(stack)
        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: leadingAnchor), stack.trailingAnchor.constraint(equalTo: trailingAnchor),
            stack.topAnchor.constraint(equalTo: topAnchor), stack.bottomAnchor.constraint(equalTo: bottomAnchor),
            top.widthAnchor.constraint(equalTo: stack.widthAnchor, constant: -30),
            what.widthAnchor.constraint(lessThanOrEqualTo: stack.widthAnchor, constant: -30),
        ])
        for v in lines.dropFirst(2) { v.widthAnchor.constraint(equalTo: stack.widthAnchor, constant: -30).isActive = true }
        toolTip = a.alive ? "Open its terminal (on the runner, in a tab); right-click for more" : "Right-click for more"
        setAccessibilityRole(.group)
        setAccessibilityLabel("\(Runners.title(a)): \(label)")
    }

    required init?(coder: NSCoder) { fatalError() }

    override func menu(for event: NSEvent) -> NSMenu? { Runners.agentMenu(agent, runner: runner, in: self) }

    override func mouseDown(with event: NSEvent) {
        if agent.alive { Runners.open(runner: runner, agent: agent.id) }
    }
}

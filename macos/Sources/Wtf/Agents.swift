// Agents (#33): every Claude Code running in wtf's shells, from its hooks
// (bridge:status): Ask Claude's panes through wtf's plugin, a claude you
// start yourself through agent-status's settings. The tab strip's badges,
// the Agents side tab and the picker read it here; wtf notifies when one
// needs you or finishes while you're looking elsewhere, and the Dock icon
// counts the ones that need you.
import AppKit
import WtfCore

/// What a tab's or an agent's badge shows.
enum AgentState: String {
    case idle, busy, waiting, done

    /// Most urgent first: needs you, done, working, idle.
    var rank: Int {
        switch self {
        case .waiting: 0
        case .done: 1
        case .busy: 2
        case .idle: 3
        }
    }

    var label: String {
        switch self {
        case .waiting: "Needs you"
        case .done: "Done"
        case .busy: "Working"
        case .idle: "Idle"
        }
    }
}

@MainActor
enum Agents {
    /// Session → its Claude Code.
    private(set) static var all: [String: Go.bridge.Status] = [:]
    private static var listening = false
    private static var observers: [(owner: () -> AnyObject?, f: () -> Void)] = []

    static func listen() {
        guard !listening else { return }
        listening = true
        ApprovalNotifications.shared.setUp()
        Backend.on("bridge:status", Go.bridge.Status.self) { apply($0) }
        NotificationCenter.default.addObserver(forName: NSApplication.didBecomeActiveNotification, object: nil, queue: .main) { _ in
            MainActor.assumeIsolated { WindowManager.shared.windows.forEach { seen(in: $0) } }
        }
    }

    static func state(_ st: Go.bridge.Status) -> AgentState {
        if st.state == "idle" && st.unseen { return .done }
        return AgentState(rawValue: st.state) ?? .idle
    }

    static func state(session: String) -> AgentState? { all[session].map(state) }

    /// A question Claude asks that one option click answers (one question,
    /// one choice); others are answered in the pane.
    static func clickable(_ ap: Go.bridge.Approval) -> Bool {
        ap.questions.count == 1 && !ap.questions[0].multi && !ap.questions[0].options.isEmpty
    }

    static func product(_ st: Go.bridge.Status) -> String { product(provider: st.provider) }

    /// Names an agent's provider: Codex, else Claude Code.
    static func product(provider: String) -> String { provider == "codex" ? "Codex" : "Claude Code" }

    /// Names the agent in a session (Claude Code when unknown).
    static func product(session: String) -> String { all[session].map(product) ?? "Claude Code" }

    /// Names what runs in sessions: Claude Code or Codex when they're all
    /// one, nil when they're mixed.
    static func product(sessions: [String]) -> String? {
        let names = Set(sessions.map { product(session: $0) })
        return names.count > 1 ? nil : names.first ?? "Claude Code"
    }

    /// "Codex runs", "2 Claude Code sessions run" or, mixed, "3 agents
    /// (Claude Code and Codex) run".
    static func run(_ product: String?, count n: Int) -> String {
        guard let product else { return "\(n) agents (Claude Code and Codex) run" }
        return n == 1 ? product + " runs" : "\(n) \(product) sessions run"
    }

    /// The badge for a tab: its most urgent agent.
    static func state(tab: Int, in w: WindowController) -> AgentState? {
        guard let leaves = w.model.tab(tab)?.root.leaves else { return nil }
        return leaves.compactMap { w.model.panes[$0]?.sessionId.flatMap(state(session:)) }.min { $0.rank < $1.rank }
    }

    /// The agents, most urgent first, then the longest in that state.
    static func sorted() -> [Go.bridge.Status] {
        all.values.sorted {
            let (a, b) = (state($0).rank, state($1).rank)
            return a != b ? a < b : $0.since < $1.since
        }
    }

    /// The agents in the order wtf first heard of them, for the Agents tab:
    /// a row stays where it is whatever its state.
    static func listed() -> [Go.bridge.Status] {
        all.values.sorted { $0.started != $1.started ? $0.started < $1.started : $0.session < $1.session }
    }

    /// #39: every permission prompt waiting, with its agent, oldest first.
    static func approvals() -> [(Go.bridge.Status, Go.bridge.Approval)] {
        all.values.flatMap { st in st.approvals.map { (st, $0) } }.sorted { $0.1.since != $1.1.since ? $0.1.since < $1.1.since : $0.1.id < $1.1.id }
    }

    /// A tool as the inbox names it: wtf's own by what they do.
    static func toolName(_ tool: String) -> String {
        switch tool {
        case "mcp__plugin_wtf_wtf__type_at_prompt": "Type at your prompt"
        case "mcp__plugin_wtf_wtf__run_readonly": "Run read-only"
        default: tool.hasPrefix("mcp__") ? tool.split(separator: "__").last.map(String.init) ?? tool : tool
        }
    }

    /// The tab an agent runs in, as titled.
    static func tabTitle(_ session: String) -> String? {
        find(session).flatMap { w, p in w.model.tabOf(pane: p.id).map { w.model.title($0) } }
    }

    /// Its tab's title as a detail beside its name: "" when it has no name,
    /// or when the tab carries the same one.
    static func tabDetail(_ st: Go.bridge.Status) -> String {
        guard !st.name.isEmpty else { return "" }
        let t = Redaction.shared.text(tabTitle(st.session) ?? "")
        return t == Redaction.shared.text(st.name) ? "" : t
    }

    /// What an agent is called: the name you gave it, or its tab's title.
    static func title(_ st: Go.bridge.Status) -> String {
        st.name.isEmpty ? Redaction.shared.text(tabTitle(st.session) ?? st.session) : st.name
    }

    /// Rename…: a name for the agent, shown in the Agents tab, the picker
    /// and its notifications, and as its tab's title. It stays with its shell.
    static func rename(_ session: String, in window: NSWindow?) {
        guard let st = all[session] else { return }
        let s = FormSheet(title: "Rename Agent",
                          subtitle: "Shown in the Agents tab, Go to Agent…, its notifications and as its tab’s title. It stays with this shell: an agent you start there again gets it too. Leave it empty to use the tab’s title.")
        let field = s.text(st.name, placeholder: Redaction.shared.text(tabTitle(session) ?? ""))
        s.field("Name", field)
        s.buttons([("Rename", true, { [weak s] in
            let name = field.stringValue
            renameTab(session, old: st.name, name)
            Task { try? await Services.bridge.Rename(session, name) }
            s?.close()
        })])
        s.present(in: window, focus: field)
    }

    /// Gives an agent's tab its new name. Clearing the name puts the tab
    /// back to automatic, unless you've since renamed the tab yourself.
    private static func renameTab(_ session: String, old: String, _ name: String) {
        guard let (w, p) = find(session), let t = w.model.tabOf(pane: p.id) else { return }
        if WindowModel.cleanTitle(name) != nil {
            w.model.rename(t.id, name)
        } else if !old.isEmpty, t.customTitle == WindowModel.cleanTitle(old) {
            w.model.rename(t.id, "")
        }
    }

    /// agent-group: the tab group agents' tabs join.
    static let groupName = "ungrouped agents"

    /// agent-group: an agent's tab joins its window's "ungrouped agents"
    /// group (made when needed). A tab already in a group stays there. Not
    /// in the background window, whose tabs aren't shown.
    static func autoGroup(_ session: String) {
        guard Settings.shared.config?.agentGroup != false, let (w, p) = find(session), !w.background,
              let t = w.model.tabOf(pane: p.id), t.groupId == nil else { return }
        w.model.joinNamedGroup(t.id, groupName)
    }

    /// Its Claude Code ended: the tab leaves the "ungrouped agents" group, unless
    /// another agent runs in one of its panes.
    private static func autoUngroup(_ pane: Int, in w: WindowController) {
        guard Settings.shared.config?.agentGroup != false, let t = w.model.tabOf(pane: pane),
              let g = t.groupId, g == w.model.groupNamed(groupName) else { return }
        let others = t.root.leaves.contains { id in id != pane && w.model.panes[id]?.sessionId.flatMap { all[$0] } != nil }
        if !others { w.model.removeFromGroup(t.id) }
    }

    /// Calls `f` after every change while `owner` lives.
    static func observe(_ owner: AnyObject, _ f: @escaping () -> Void) {
        observers.append(({ [weak owner] in owner }, f))
    }

    /// The agents whose sub-agents the Agents tab shows (collapsed by
    /// default; remembered while wtf runs).
    private(set) static var subagentsShown: Set<String> = []

    static func toggleSubagents(_ session: String) {
        if subagentsShown.remove(session) == nil { subagentsShown.insert(session) }
        notify()
    }

    /// Something the Agents tab shows changed (Resume's list).
    static func changed() { notify() }

    private static func notify() {
        observers.removeAll { $0.owner() == nil }
        observers.forEach { $0.f() }
    }

    static func find(_ session: String) -> (WindowController, Pane)? {
        for w in WindowManager.shared.windows {
            if let p = w.model.pane(session: session) { return (w, p) }
        }
        return nil
    }

    /// Whether you're looking at a pane: its tab is in front, in the key
    /// window, while wtf is active.
    static func visible(_ p: Pane, in w: WindowController) -> Bool {
        w.model.active?.root.leaves.contains(p.id) == true && w.window?.isKeyWindow == true && NSApp.isActive
    }

    /// Goes to an agent's window, tab and pane; a background one comes back
    /// into a tab first.
    static func go(to session: String) {
        if Runners.isRemote(session) {
            Runners.open(session: session)
            return
        }
        if find(session)?.0.background == true { Background.open(session) }
        guard let (w, p) = find(session) else { return }
        if let tab = w.model.tabOf(pane: p.id) { w.model.activate(tab.id) }
        w.model.focusPane(p.id)
        NSApp.activate(ignoringOtherApps: true)
        w.window?.makeKeyAndOrderFront(nil)
        seen(in: w)
    }

    /// The finished agents in front of you in `w` are no longer "done".
    static func seen(in w: WindowController) {
        guard w.window?.isKeyWindow == true, NSApp.isActive, let leaves = w.model.active?.root.leaves else { return }
        for id in leaves {
            guard let s = w.model.panes[id]?.sessionId else { continue }
            // You're looking at it: its notification has nothing to add.
            ApprovalNotifications.shared.clear(s)
            guard all[s]?.unseen == true else { continue }
            Task { try? await Services.bridge.Seen(s) }
        }
    }

    private static func apply(_ st: Go.bridge.Status) {
        let old = all[st.session].map(state)
        let oldAsks = Set(all[st.session]?.approvals.map(\.id) ?? [])
        // Approvals answered (here, in the pane, or gone with their agent)
        // take their notification away.
        for id in oldAsks.subtracting(st.approvals.map(\.id)) { ApprovalNotifications.shared.remove(approval: id) }
        // One that no longer says how it is (it's working again, or quit)
        // goes too.
        if st.state == "ended" {
            ApprovalNotifications.shared.clear(st.session)
        } else {
            ApprovalNotifications.shared.clear(st.session, unless: state(st))
        }
        if st.state == "ended" {
            all[st.session] = nil
            subagentsShown.remove(st.session)
        } else {
            all[st.session] = st
        }
        let place = find(st.session)
        if let (w, p) = place { noteResumable(st, pane: p, in: w) }
        if let (w, p) = place, !w.background {
            if old == nil, st.state != "ended" {
                autoGroup(st.session)
            } else if st.state == "ended" {
                autoUngroup(p.id, in: w)
            }
        }
        place?.0.refreshStrip()
        // A background agent that quit leaves only its shell: close it.
        if st.state == "ended", let (w, p) = place, w.background { w.model.closePane(p.id) }
        updateDock()
        notify()
        guard st.state != "ended" else { return }
        let now = state(st)
        // #39: a new permission prompt says what it asks and where.
        let ask = st.approvals.first { !oldAsks.contains($0.id) }
        let title: String
        if Runners.isRemote(st.session) {
            // #41: an agent on a runner has no pane here.
            guard now != old || ask != nil else { return }
            title = Runners.subtitle(st)
        } else {
            guard let (w, p) = place else { return }
            if visible(p, in: w) {
                ApprovalNotifications.shared.clear(st.session)
                if st.unseen { Task { try? await Services.bridge.Seen(st.session) } }
                return
            }
            guard now != old || ask != nil, let tab = w.model.tabOf(pane: p.id) else { return }
            title = st.name.isEmpty ? w.model.title(tab) : st.name
        }
        switch now {
        case .waiting:
            var body = Redaction.shared.text(st.message)
            if let ask {
                body = Redaction.shared.text(([toolName(ask.tool) + ": " + ask.what] + ask.facts).joined(separator: "\n"))
            }
            let head = product(st) + " needs you"
            if let ask, !ask.questions.isEmpty {
                // A question: its options answer it, when one click can.
                let r = Redaction.shared
                let q = ask.questions.map(\.question).joined(separator: "\n")
                let options = clickable(ask) ? ask.questions[0].options : []
                Task {
                    if await ApprovalNotifications.shared.postQuestion(id: ask.id, session: st.session, title: product(st) + " asks you",
                                                                       subtitle: title, body: r.text(q), options: options) {
                        if all[st.session]?.approvals.contains(where: { $0.id == ask.id }) != true { ApprovalNotifications.shared.remove(approval: ask.id) }
                        return
                    }
                    ApprovalNotifications.shared.show(title: product(st) + " asks you", subtitle: title, body: r.text(q), agent: st.session, state: .waiting)
                }
                playSound(.attention)
                return
            }
            Task {
                // A permission prompt: Allow and Deny on the notification itself.
                if let ask, await ApprovalNotifications.shared.post(id: ask.id, session: st.session, title: head, subtitle: title, body: body) {
                    // Answered while it was posted.
                    if all[st.session]?.approvals.contains(where: { $0.id == ask.id }) != true { ApprovalNotifications.shared.remove(approval: ask.id) }
                    return
                }
                ApprovalNotifications.shared.show(title: head, subtitle: title, body: body, agent: st.session, state: .waiting)
            }
            playSound(.attention)
        case .done:
            ApprovalNotifications.shared.show(title: product(st) + " finished", subtitle: title,
                                              body: Redaction.shared.text(st.prompt), agent: st.session, state: .done)
            playSound(.complete)
        default:
            break
        }
    }

    /// #38: what a restart would offer to resume in this pane (not an Ask
    /// Claude pane, which belongs to the shell it works on). An agent that
    /// starts in a pane still offering one takes its place; a resumed one
    /// gets its name back.
    private static func noteResumable(_ st: Go.bridge.Status, pane p: Pane, in w: WindowController) {
        guard !ClaudePane.isClaude(p) else { return }
        let a: Go.state.Agent? = st.state == "ended" || st.claudeSession.isEmpty ? nil
            : .init(provider: st.provider, session: st.claudeSession, dir: st.project, name: st.name, transcript: st.transcript)
        if p.agent != a {
            p.agent = a
            w.scheduleSave()
        }
        guard a != nil else { return }
        if p.resumable != nil { Resume.dismiss(p, in: w) }
        if st.name.isEmpty, let name = Resume.pendingNames.removeValue(forKey: st.claudeSession) {
            Task { try? await Services.bridge.Rename(st.session, name) }
            renameTab(st.session, old: "", name)
        }
    }

    /// agent-sound: a short system sound with an agent's notification, one
    /// for each kind. Played here, not by the notification, so it doesn't
    /// depend on wtf's notification permission.
    enum Sound: String {
        case attention = "Glass" // needs you
        case complete = "Pop" // finished
    }

    private static var lastSound = Date.distantPast

    /// Agents that change together make one sound.
    static func playSound(_ s: Sound, preview: Bool = false) {
        guard preview || Settings.shared.config?.agentSound != false else { return }
        guard preview || Date().timeIntervalSince(lastSound) > 1.5 else { return }
        lastSound = Date()
        NSSound(named: NSSound.Name(s.rawValue))?.play()
    }

    private static func updateDock() {
        let n = all.values.filter { state($0) == .waiting }.count
        NSApp.dockTile.badgeLabel = n > 0 ? String(n) : nil
    }
}

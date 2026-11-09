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
            guard let s = w.model.panes[id]?.sessionId, all[s]?.unseen == true else { continue }
            Task { try? await Services.bridge.Seen(s) }
        }
    }

    private static func apply(_ st: Go.bridge.Status) {
        let old = all[st.session].map(state)
        if st.state == "ended" {
            all[st.session] = nil
            subagentsShown.remove(st.session)
        } else {
            all[st.session] = st
        }
        let place = find(st.session)
        place?.0.refreshStrip()
        // A background agent that quit leaves only its shell: close it.
        if st.state == "ended", let (w, p) = place, w.background { w.model.closePane(p.id) }
        updateDock()
        notify()
        guard st.state != "ended", let (w, p) = place else { return }
        let now = state(st)
        if visible(p, in: w) {
            if st.unseen { Task { try? await Services.bridge.Seen(st.session) } }
            return
        }
        guard now != old, let tab = w.model.tabOf(pane: p.id) else { return }
        let title = w.model.title(tab)
        switch now {
        case .waiting:
            let body = Redaction.shared.text(st.message)
            Task { try? await Backend.run("notify", "Notify", ["Claude Code needs you", title, body]) }
        case .done:
            Task { try? await Backend.run("notify", "Notify", ["Claude Code finished", title, Redaction.shared.text(st.prompt)]) }
        default:
            break
        }
    }

    private static func updateDock() {
        let n = all.values.filter { state($0) == .waiting }.count
        NSApp.dockTile.badgeLabel = n > 0 ? String(n) : nil
    }
}

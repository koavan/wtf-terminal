import AppKit
import WtfCore

/// The connection listing, shared by every window: refreshed by whichever
/// Network tab is on screen. Scope and filter last while the app runs (the
/// Processes tab sets the filter to "pid:N").
@MainActor
final class NetStore {
    static let shared = NetStore()

    var conns: [Conn] = []
    var error: String?
    var loaded = false
    var terminalsOnly = true
    var filter = ""
    var updatedAt: Date?
    private var busy = false
    private var observers = Observers()

    func observe<T: AnyObject>(_ owner: T, _ fn: @escaping (T) -> Void) { observers.add(owner, fn) }
    func changed() { observers.notify() }

    func refresh() {
        guard !busy else { return }
        busy = true
        Task {
            do {
                conns = try await Services.netconn.List()
                error = nil
                updatedAt = Date()
            } catch {
                self.error = error.localizedDescription
            }
            loaded = true
            busy = false
            changed()
        }
    }
}

extension WindowController {
    /// The tab and pane in this window whose shell is `session`.
    func place(session: String) -> (tab: Tab, pane: Pane)? {
        guard !session.isEmpty, let p = model.pane(session: session), let t = model.tabOf(pane: p.id) else { return nil }
        return (t, p)
    }

    /// Shows the tab and focuses the pane running `session`, if it's in this window.
    func jump(session: String) {
        guard let (t, p) = place(session: session) else { return }
        model.activate(t.id)
        model.focusPane(p.id)
        focusActive()
    }

    /// Who started a connection or process: its tab's title, or another window.
    func ownerPill(_ session: String) -> SideList.Pill? {
        guard !session.isEmpty else { return nil }
        guard let (t, _) = place(session: session) else {
            return .init(text: "Another window", tip: "Started from a terminal in another window")
        }
        let title = model.title(t)
        return .init(text: title.count > 18 ? String(title.prefix(17)) + "…" : title, color: .controlAccentColor,
                     tip: "Go to “\(title)” (double-click)")
    }
}

/// The Network tab: port forwards, then what's listening, connected and
/// otherwise open — from your terminals or everything. Polled only while
/// on screen and the window is in front. Like NetworkView.svelte.
@MainActor
final class NetworkView: NSView, SidePaneTab, NSSearchFieldDelegate {
    private unowned let owner: WindowController
    private let header = PaneHeader("Network")
    private let scope = NSSegmentedControl(labels: ["Terminals", "All"], trackingMode: .selectOne, target: nil, action: nil)
    private let paused = NSTextField(labelWithString: "")
    private let filter = NSSearchField()
    private let list = SideList()
    private var note: NoteView?
    private var noteButton: NSButton?
    private var poller: Poller?
    private var othersOpen = false
    private var store: NetStore { .shared }
    private var rows: [String: Conn] = [:]

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        header.tool("plus", "New port forward (ssh tunnel or kubectl port-forward)") { [weak self] in
            guard let self else { return }
            TunnelEditor.show(nil, host: nil, in: owner)
        }
        header.refreshControls(in: self) { NetStore.shared.refresh() }
        addSubview(header)
        scope.target = self
        scope.action = #selector(scopeChanged)
        scope.refusesFirstResponder = true
        scope.controlSize = .small
        scope.setAccessibilityLabel("Show connections from")
        addSubview(scope)
        paused.font = .systemFont(ofSize: 11)
        paused.textColor = .secondaryLabelColor
        addSubview(paused)
        filter.placeholderString = "Filter by port, host, process, PID"
        filter.delegate = self
        filter.sendsSearchStringImmediately = true
        filter.refusesFirstResponder = true
        addSubview(filter)
        list.onDoubleClick = { [weak self] id in
            guard let self else { return }
            if id.hasPrefix("t:"), let t = tunnel(id) { TunnelEditor.show(t, host: nil, in: owner) }
            if let c = rows[id], c.state == "LISTEN" || !c.remote.isEmpty || !c.target.isEmpty { owner.jump(session: c.session) }
        }
        list.menuProvider = { [weak self] ids in
            guard let self, let id = ids.first else { return nil }
            if let t = tunnel(id) { return TunnelsSection.menu(t, window: owner) }
            return rows[id].map(menu)
        }
        addSubview(list)
        store.observe(self) { $0.rebuild() }
        SshStore.shared.observe(self) { $0.rebuild() }
        Settings.shared.observe(self) { v in
            v.poller?.update(interval: Poller.sidebarInterval)
            v.rebuild()
        }
        if !SshStore.shared.tunnelsLoaded { SshStore.shared.loadTunnels() }
        poller = Poller(window: owner.window, interval: Poller.sidebarInterval) { NetStore.shared.refresh() }
        rebuild()
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        if window == nil { poller?.stop() }
    }

    override func layout() {
        super.layout()
        header.frame = NSRect(x: 0, y: 0, width: bounds.width, height: 30)
        scope.frame = NSRect(x: 8, y: 32, width: bounds.width - 16, height: 22)
        var y: CGFloat = 58
        if !paused.isHidden {
            paused.frame = NSRect(x: 10, y: y, width: bounds.width - 20, height: 16)
            y += 18
        }
        filter.frame = NSRect(x: 8, y: y, width: bounds.width - 16, height: 24)
        y += 30
        if let note {
            let h = note.height(for: bounds.width)
            note.frame = NSRect(x: 0, y: y, width: bounds.width, height: h)
            y += h
            if let b = noteButton {
                b.frame = NSRect(x: NoteView.inset - 4, y: y - 6, width: b.fittingSize.width, height: 22)
                y += 22
            }
        }
        list.frame = NSRect(x: 0, y: y, width: bounds.width, height: max(0, bounds.height - y))
    }

    func controlTextDidChange(_ obj: Notification) {
        store.filter = filter.stringValue
        rebuild()
    }

    @objc private func scopeChanged() {
        store.terminalsOnly = scope.selectedSegment == 0
        owner.scheduleSave()
        rebuild()
    }

    func debugSummary() -> String {
        "network: \(store.conns.count) conns, error \(store.error ?? "none"), \(rows.count) shown"
    }

    private func tunnel(_ id: String) -> Go.sshhosts.TunnelStatus? {
        id.hasPrefix("t:") ? SshStore.shared.tunnels.first { "t:" + $0.id == id } : nil
    }

    private static func key(_ c: Conn) -> String { "c:\(c.pid)|\(c.protocol)|\(c.local)|\(c.remote)|\(c.state)" }

    private func rebuild() {
        if filter.stringValue != store.filter { filter.stringValue = store.filter }
        scope.selectedSegment = store.terminalsOnly ? 0 : 1
        let auto = Settings.shared.config?.sidebarAutoRefresh ?? true
        paused.isHidden = auto
        paused.stringValue = Poller.pausedText(store.updatedAt)

        let tunnels = SshStore.shared.tunnels
        let forwardPorts = Set(tunnels.filter(Tunnels.isOn).compactMap(Tunnels.localPort))
        let shown = Net.visible(store.conns, terminalsOnly: store.terminalsOnly, filter: store.filter, forwardPorts: forwardPorts)
        let listening = Net.mergeListeners(shown.filter { $0.state == "LISTEN" })
        let connections = shown.filter { $0.state != "LISTEN" && (!$0.remote.isEmpty || !$0.target.isEmpty) }
        let others = shown.filter { $0.state != "LISTEN" && $0.remote.isEmpty && $0.target.isEmpty }
        rows = [:]
        for c in listening + connections + others { rows[Self.key(c)] = c }

        let q = store.filter.trimmingCharacters(in: .whitespaces).lowercased()
        let forwards = tunnels.filter { t in
            q.isEmpty || "\(t.name) \(Tunnels.describe(t)) \(t.command)".lowercased().contains(q)
        }
        var sections: [SideList.Section] = [
            .init(title: forwards.isEmpty ? "Forwards" : "Forwards (\(forwards.count))", rows: forwards.map { t in
                var r = TunnelsSection.row(t, window: owner)
                r.id = "t:" + t.id
                return r
            }, empty: SshStore.shared.tunnelsLoaded && q.isEmpty ? "None yet. + adds an ssh tunnel or a kubectl port-forward that runs in the background." : nil,
            always: true),
        ]
        if !listening.isEmpty {
            sections.append(.init(title: "Listening (\(listening.count))", rows: listening.map { listenRow($0, tunnels: tunnels) }))
        }
        if !connections.isEmpty {
            sections.append(.init(title: "Connections (\(connections.count))", rows: connections.map(connectionRow)))
        }
        if !others.isEmpty {
            sections.append(.init(title: "Other sockets", rows: others.map { c in
                var r = SideList.Row(id: Self.key(c), symbol: "circle.dashed", tint: .secondaryLabelColor, title: c.process,
                                     detail: "\(c.protocol) · \(c.local)")
                if let p = owner.ownerPill(c.session) { r.pills = [p] }
                return r
            }, toggle: { [weak self] in
                self?.othersOpen.toggle()
                self?.rebuild()
            }, open: othersOpen, badge: "\(others.count)"))
        }
        list.set(sections)

        note?.removeFromSuperview()
        note = nil
        noteButton?.removeFromSuperview()
        noteButton = nil
        if let e = store.error {
            note = paneNote("Can't list connections", e)
            noteButton = owner.installLink(for: e)
        } else if store.loaded, shown.isEmpty {
            if !q.isEmpty {
                note = paneNote(nil, "No connections match “\(store.filter)”.")
            } else if store.terminalsOnly {
                note = paneNote("Nothing from your terminals", "Programs you start here — dev servers, ssh, database clients — show up as they connect.")
                let b = ClosureButton(title: "Show all connections") { [weak self] in
                    NetStore.shared.terminalsOnly = false
                    self?.rebuild()
                }
                b.isBordered = false
                b.contentTintColor = .controlAccentColor
                b.font = .systemFont(ofSize: 12, weight: .medium)
                b.refusesFirstResponder = true
                noteButton = b
            } else {
                note = paneNote(nil, "No network connections.")
            }
        }
        if let note { addSubview(note) }
        if let noteButton { addSubview(noteButton) }
        needsLayout = true
    }

    private func listenRow(_ c: Conn, tunnels: [Go.sshhosts.TunnelStatus]) -> SideList.Row {
        let (host, port) = Net.splitAddress(c.local)
        let exposed = Net.isExposed(host)
        var detail = c.protocol + (Net.serviceName(port).map { " · \($0)" } ?? "")
        var forwardTip: String?
        if let f = tunnels.first(where: { Tunnels.isOn($0) && Tunnels.localPort($0) == port }) {
            forwardTip = "Forward: " + Tunnels.describe(f)
            detail += " · forward · " + (f.name.isEmpty ? Tunnels.describe(f) : f.name)
        }
        var pills: [SideList.Pill] = [exposed
            ? .init(text: "network", color: .systemOrange, tip: "Reachable from other machines on your network")
            : .init(text: "local", tip: "Only reachable from this computer")]
        if let p = owner.ownerPill(c.session) { pills.append(p) }
        return .init(id: Self.key(c), symbol: "dot.radiowaves.left.and.right", title: ":\(port)  \(c.process)", detail: detail,
                     pills: pills, tip: "\(c.process) (pid \(c.pid)) listening on \(c.local)" + (forwardTip.map { "\n" + $0 } ?? ""))
    }

    private func connectionRow(_ c: Conn) -> SideList.Row {
        var host: String, port = "", name = "", note = ""
        if !c.remote.isEmpty {
            (host, port) = Net.splitAddress(c.remote)
        } else {
            host = c.targetIP.isEmpty ? c.target : c.targetIP
            if !c.targetIP.isEmpty, c.target != c.targetIP { name = c.target }
            if c.targetResolved {
                note = "\(c.target) resolved here to \(c.targetIP); \(c.process) may be using another of its addresses"
            } else if c.targetIP.isEmpty {
                note = "Couldn't resolve \(c.target)"
            }
        }
        let dest = host + (port.isEmpty ? "" : ":\(port)") + (c.targetResolved ? " ≈" : "")
        let detail = (name.isEmpty ? "" : "\(name) · ") + "\(c.process) · \(c.protocol)" + (Net.serviceName(port).map { " · \($0)" } ?? "")
        var pills: [SideList.Pill] = []
        if let s = Net.stateLabel(c.state) { pills.append(.init(text: s)) }
        if let p = owner.ownerPill(c.session) { pills.append(p) }
        return .init(id: Self.key(c), symbol: "arrow.up.right", title: dest, mono: true, detail: detail, pills: pills,
                     tip: "\(c.process) (pid \(c.pid)) → \(host)\(port.isEmpty ? "" : ":\(port)")" + (note.isEmpty ? "" : "\n\(note)"))
    }

    private func menu(_ c: Conn) -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        func add(_ t: String, _ run: @escaping () -> Void) { m.addItem(ClosureMenuItem(title: t, keyEquivalent: "", action: run)) }
        let o = owner
        let port = Net.splitAddress(c.local).port
        func copy(_ text: String, _ what: String? = nil) {
            Clipboard.copy(text)
            Toast.show("Copied \(what ?? text)", in: o.rootView)
        }
        var top = false
        if c.state == "LISTEN", c.protocol == "TCP" {
            add("Open localhost:\(port) in Browser") { NSWorkspace.shared.open(URL(string: "http://localhost:\(port)")!) }
            top = true
        }
        if o.place(session: c.session) != nil {
            add("Go to Tab") { o.jump(session: c.session) }
            top = true
        }
        if top { m.addItem(.separator()) }
        add("Copy Local Address") { copy(c.local) }
        if !c.remote.isEmpty { add("Copy Remote Address") { copy(c.remote) } }
        if !c.targetIP.isEmpty { add("Copy \(c.targetIP)") { copy(c.targetIP) } }
        if !c.target.isEmpty, c.target != c.targetIP { add("Copy \(c.target)") { copy(c.target) } }
        add("Copy PID (\(c.pid))") { copy("\(c.pid)") }
        m.addItem(.separator())
        add("End \(c.process)…") { [weak self] in self?.end(c) }
        return m
    }

    private func end(_ c: Conn) {
        let port = Net.splitAddress(c.local).port
        Confirm.show(in: owner.window, title: "End \(c.process)?",
                     message: "Process \(c.pid) will be asked to quit" + (c.state == "LISTEN" ? ", freeing port \(port)" : "") + ".",
                     items: ["\(c.protocol) \(c.local)" + (c.remote.isEmpty ? "" : " → \(c.remote)")], button: "End Process") { [weak self] in
            Task {
                guard let v = self?.owner.rootView else { return }
                do {
                    try await Services.netconn.Kill(c.pid)
                    Toast.show("Asked \(c.process) to quit", in: v)
                    try? await Task.sleep(for: .milliseconds(500))
                    NetStore.shared.refresh()
                } catch {
                    Toast.show("Couldn't end \(c.process): \(error.localizedDescription)", in: v)
                }
            }
        }
    }
}

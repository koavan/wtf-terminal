import AppKit
import WtfCore

/// Your processes, shared by every window: refreshed by whichever Processes
/// tab is on screen. Scope and sort last while the app runs.
@MainActor
final class ProcStore {
    static let shared = ProcStore()

    var procs: [Proc] = []
    var error: String?
    var loaded = false
    var terminalsOnly = true
    var sort = Procs.Sort.cpu
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
                procs = try await Services.procs.List()
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

/// The Processes tab: each terminal's process tree, or all your processes
/// by CPU, memory or name; end or force quit them. Polled only while on
/// screen and the window is in front. Like internal/gtkui/processtab.go.
@MainActor
final class ProcessView: NSView, SidePaneTab, NSSearchFieldDelegate {
    static let cap = 300

    private unowned let owner: WindowController
    private let header = PaneHeader("Processes")
    private let scope = NSSegmentedControl(labels: ["Terminals", "All"], trackingMode: .selectOne, target: nil, action: nil)
    private let sort = NSSegmentedControl(labels: ["CPU", "Memory", "A–Z"], trackingMode: .selectOne, target: nil, action: nil)
    private let paused = NSTextField(labelWithString: "")
    private let filter = NSSearchField()
    private let list = SideList()
    private var note: NoteView?
    private var poller: Poller?
    private var store: ProcStore { .shared }

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        header.refreshControls(in: self) { ProcStore.shared.refresh() }
        addSubview(header)
        for s in [scope, sort] {
            s.target = self
            s.refusesFirstResponder = true
            s.controlSize = .small
            addSubview(s)
        }
        scope.action = #selector(scopeChanged)
        scope.setAccessibilityLabel("Show processes from")
        sort.action = #selector(sortChanged)
        sort.setAccessibilityLabel("Sort by")
        sort.toolTip = "Sort by CPU, memory or name"
        paused.font = .systemFont(ofSize: 11)
        paused.textColor = .secondaryLabelColor
        addSubview(paused)
        filter.placeholderString = "Filter by name, command, PID"
        filter.delegate = self
        filter.sendsSearchStringImmediately = true
        filter.refusesFirstResponder = true
        addSubview(filter)
        list.onDoubleClick = { [weak self] id in
            guard let self, let p = proc(id) else { return }
            owner.jump(session: p.session)
        }
        list.menuProvider = { [weak self] ids in ids.first.flatMap { self?.proc($0) }.flatMap { self?.menu($0) } }
        addSubview(list)
        store.observe(self) { $0.rebuild() }
        Settings.shared.observe(self) { v in
            v.poller?.update(interval: Poller.sidebarInterval)
            v.rebuild()
        }
        poller = Poller(window: owner.window, interval: Poller.sidebarInterval) { ProcStore.shared.refresh() }
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
        if !sort.isHidden {
            sort.frame = NSRect(x: 8, y: y, width: bounds.width - 16, height: 22)
            y += 26
        }
        if let note {
            let h = note.height(for: bounds.width)
            note.frame = NSRect(x: 0, y: y, width: bounds.width, height: h)
            y += h
        }
        list.frame = NSRect(x: 0, y: y, width: bounds.width, height: max(0, bounds.height - y))
    }

    func controlTextDidChange(_ obj: Notification) { rebuild() }

    @objc private func scopeChanged() {
        store.terminalsOnly = scope.selectedSegment == 0
        owner.scheduleSave()
        rebuild()
    }

    @objc private func sortChanged() {
        store.sort = [.cpu, .memory, .name][max(0, sort.selectedSegment)]
        owner.scheduleSave()
        rebuild()
    }

    func debugSummary() -> String { "processes: \(store.procs.count) procs, error \(store.error ?? "none")" }

    private func proc(_ id: String) -> Proc? { Int(id).flatMap { pid in store.procs.first { $0.pid == pid } } }

    private func rebuild() {
        scope.selectedSegment = store.terminalsOnly ? 0 : 1
        sort.selectedSegment = [Procs.Sort.cpu, .memory, .name].firstIndex(of: store.sort) ?? 0
        sort.isHidden = store.terminalsOnly
        paused.isHidden = Settings.shared.config?.sidebarAutoRefresh ?? true
        paused.stringValue = Poller.pausedText(store.updatedAt)
        let q = filter.stringValue.trimmingCharacters(in: .whitespaces)

        note?.removeFromSuperview()
        note = nil
        var sections: [SideList.Section] = []
        if let e = store.error {
            note = paneNote("Can't list processes", e)
        } else if store.terminalsOnly {
            var bySession: [String: [Proc]] = [:]
            for p in store.procs where !p.session.isEmpty { bySession[p.session, default: []].append(p) }
            let order = Dictionary(uniqueKeysWithValues: owner.model.tabs.enumerated().map { ($1.id, $0) })
            let groups = bySession.map { session, list -> (key: Int, label: String, session: String, rows: [(proc: Proc, depth: Int)]) in
                let rows = q.isEmpty ? Procs.tree(list) : Procs.sorted(list.filter { Procs.matches($0, q) }, by: .name).map { ($0, 0) }
                let place = owner.place(session: session)
                return (place.flatMap { order[$0.tab.id] } ?? Int.max, place.map { owner.model.title($0.tab) } ?? "Another window", session, rows)
            }.filter { !$0.rows.isEmpty }.sorted { ($0.key, $0.label) < ($1.key, $1.label) }
            sections = groups.map { g in
                let here = owner.place(session: g.session) != nil
                return .init(title: g.label, tip: here ? "Go to “\(g.label)”" : "A terminal in another window",
                             rows: g.rows.map { row($0.proc, depth: $0.depth) },
                             action: here ? { [weak self] in
                                 guard let self, let (t, _) = owner.place(session: g.session) else { return }
                                 owner.model.activate(t.id)
                             } : nil)
            }
            if store.loaded, sections.isEmpty {
                note = paneNote(nil, q.isEmpty ? "No terminal processes yet." : "No processes match “\(q)”.")
            }
        } else {
            let all = Procs.sorted(store.procs.filter { Procs.matches($0, q) }, by: store.sort)
            var rows = all.prefix(Self.cap).map { row($0, depth: 0) }
            if all.count > Self.cap {
                rows.append(.init(id: "more", symbol: "", title: "\(all.count - Self.cap) more — filter to find them.", muted: true))
            }
            sections = [.init(title: nil, rows: rows)]
            if store.loaded, all.isEmpty { note = paneNote(nil, "No processes match “\(q)”.") }
        }
        list.set(sections)
        if let note { addSubview(note) }
        needsLayout = true
    }

    private func row(_ p: Proc, depth: Int) -> SideList.Row {
        let cpuColor: NSColor = p.cpu >= 50 ? .systemOrange : p.cpu >= 10 ? .labelColor : .secondaryLabelColor
        return .init(id: "\(p.pid)", symbol: "", title: p.name,
                     tip: "\(p.command)\nPID \(p.pid) · parent \(p.ppid) · running \(Procs.elapsed(p.elapsed))",
                     columns: [.init(text: "\(p.pid)", width: 46, color: .tertiaryLabelColor),
                               .init(text: Procs.cpu(p.cpu), width: 42, color: cpuColor, bold: p.cpu >= 50),
                               .init(text: Procs.bytes(p.memory), width: 52)],
                     depth: depth, muted: p.shell)
    }

    private func menu(_ p: Proc) -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        func add(_ t: String, _ run: @escaping () -> Void) { m.addItem(ClosureMenuItem(title: t, keyEquivalent: "", action: run)) }
        let o = owner
        let place = o.place(session: p.session)
        if place != nil { add("Go to Tab") { o.jump(session: p.session) } }
        add("Show Network Connections") {
            NetStore.shared.filter = "pid:\(p.pid)"
            NetStore.shared.changed()
            o.sidebar.tab = .network
            o.sidebarChanged()
        }
        m.addItem(.separator())
        add("Copy PID (\(p.pid))") {
            Clipboard.copy("\(p.pid)")
            Toast.show("Copied PID", in: o.rootView)
        }
        add("Copy Command") {
            Clipboard.copy(p.command)
            Toast.show("Copied command", in: o.rootView)
        }
        m.addItem(.separator())
        if p.shell, let (t, _) = place {
            // Interactive shells ignore SIGTERM: close the tab instead.
            add("Close Tab…") { o.closeTabs([t.id]) }
        } else {
            add("End \(p.name)…") { [weak self] in self?.end(p, force: false) }
            add("Force Quit \(p.name)…") { [weak self] in self?.end(p, force: true) }
        }
        return m
    }

    private func end(_ p: Proc, force: Bool) {
        Confirm.show(in: owner.window, title: force ? "Force quit \(p.name)?" : "End \(p.name)?",
                     message: force ? "Process \(p.pid) will stop immediately, without a chance to save or clean up."
                         : "Process \(p.pid) will be asked to quit.",
                     items: [p.command], button: force ? "Force Quit" : "End Process") { [weak self] in
            Task {
                guard let v = self?.owner.rootView else { return }
                do {
                    try await Services.procs.Kill(p.pid, force)
                    Toast.show(force ? "Force quit \(p.name)" : "Asked \(p.name) to quit", in: v)
                    try? await Task.sleep(for: .milliseconds(400))
                    ProcStore.shared.refresh()
                } catch {
                    Toast.show("Couldn't end \(p.name): \(error.localizedDescription)", in: v)
                }
            }
        }
    }
}

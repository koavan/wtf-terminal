import AppKit
import WtfCore

/// When a command ran: today's clock, or the day too.
@MainActor
func clockTime(_ d: Date) -> String {
    let f = DateFormatter()
    f.dateFormat = DateFormatter.dateFormat(fromTemplate: Calendar.current.isDateInToday(d) ? "jmmss" : "MMMdjmmss", options: 0, locale: .current)
    return f.string(from: d)
}

/// Saved history (#17) changed: a command was saved or forgotten.
@MainActor
enum SavedHistory {
    static let notification = Notification.Name("wtf.savedHistoryChanged")
    static func changed() { NotificationCenter.default.post(name: notification, object: nil) }
}

/// The History tab: the commands this pane (or every pane in the window)
/// ran, newest first, with how they went; or Saved: every shell's, across
/// windows and restarts, with the Kubernetes and AWS context each ran in
/// (#17), filtered by it. Click to put one at the prompt, double-click to
/// run it again. Like HistoryView.svelte.
@MainActor
final class HistoryView: NSView, SidePaneTab, NSSearchFieldDelegate {
    private unowned let owner: WindowController
    private let scope = NSSegmentedControl(labels: ["This pane", "All panes", "Saved"], trackingMode: .selectOne, target: nil, action: nil)
    /// Saved: the filters (time, Kubernetes, AWS) and what Search returned.
    private let timePop = NSPopUpButton(frame: .zero, pullsDown: false)
    private let kubePop = NSPopUpButton(frame: .zero, pullsDown: false)
    private let awsPop = NSPopUpButton(frame: .zero, pullsDown: false)
    private var result: Go.cmdhistory.Result?
    private var savedShown: [Go.cmdhistory.Entry] = []
    private var since = 0
    private var kube = ""
    /// "account:…", "profile:…" or "region:…".
    private var aws = ""
    private var loading = false, again = false
    private let trash = ToolButton(symbol: "trash", tip: "Clear history") {}
    /// Saved: ask Claude about it (#31).
    private let askClaude = ToolButton(symbol: "sparkles", tip: "Ask your history (Claude)") {}
    private let filter = NSSearchField()
    private let list = SideList()
    private var note: NoteView?
    private var shown: [History.Entry] = []
    private var records: WindowRecords { owner.records }

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        scope.target = self
        scope.action = #selector(scopeChanged)
        scope.refusesFirstResponder = true
        scope.controlSize = .small
        addSubview(scope)
        trash.target = self
        trash.action = #selector(clear)
        addSubview(trash)
        askClaude.target = self
        askClaude.action = #selector(askHistory)
        askClaude.isHidden = true
        addSubview(askClaude)
        filter.placeholderString = "Filter commands"
        filter.delegate = self
        filter.sendsSearchStringImmediately = true
        filter.refusesFirstResponder = true
        addSubview(filter)
        for pop in [timePop, kubePop, awsPop] {
            pop.controlSize = .small
            pop.font = .systemFont(ofSize: NSFont.smallSystemFontSize)
            pop.target = self
            pop.action = #selector(savedFilterChanged(_:))
            pop.isHidden = true
            addSubview(pop)
        }
        timePop.addItems(withTitles: ["Any time", "Today", "Last 7 days", "Last 30 days"])
        NotificationCenter.default.addObserver(forName: SavedHistory.notification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { if self?.saved == true, self?.window != nil { self?.loadSaved() } }
        }
        list.onClick = { [weak self] id in
            // The second click of a double-click runs it instead.
            guard let self, NSApp.currentEvent?.clickCount ?? 1 == 1 else { return }
            if saved { savedEntry(id).map { owner.activeTerminal?.insert($0.command) } } else { entry(id).map { insert($0) } }
        }
        list.onDoubleClick = { [weak self] id in
            guard let self else { return }
            if saved { savedEntry(id).map { runAgain(command: $0.command) } } else { entry(id).map { runAgain($0) } }
        }
        list.menuProvider = { [weak self] ids in
            guard let self, let id = ids.first else { return nil }
            return saved ? savedEntry(id).map(savedMenu) : entry(id).map(menu)
        }
        addSubview(list)
        records.observe(self) { $0.rebuild() }
        Redaction.shared.observe(self) { $0.rebuild() }
        rebuild()
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    override func layout() {
        super.layout()
        scope.frame = NSRect(x: 8, y: 4, width: bounds.width - (saved ? 72 : 46), height: 22)
        trash.frame = NSRect(x: bounds.width - 32, y: 3, width: 24, height: 24)
        askClaude.frame = NSRect(x: bounds.width - 58, y: 3, width: 24, height: 24)
        filter.frame = NSRect(x: 8, y: 32, width: bounds.width - 16, height: 24)
        var top: CGFloat = 62
        if saved {
            let w = (bounds.width - 16 - 8) / 3
            for (i, pop) in [timePop, kubePop, awsPop].enumerated() {
                pop.frame = NSRect(x: 8 + CGFloat(i) * (w + 4), y: 60, width: w, height: 22)
            }
            top = 88
        }
        list.frame = NSRect(x: 0, y: top, width: bounds.width, height: max(0, bounds.height - top))
        note?.frame = NSRect(x: 0, y: top, width: bounds.width, height: note?.height(for: bounds.width) ?? 0)
    }

    private var all: Bool { owner.sidebar.historyScope == "all" }
    private var saved: Bool { owner.sidebar.historyScope == "saved" }

    func paneChanged() { rebuild() }

    func debugSummary() -> String { saved ? "history saved: \(savedShown.count)" : "history: \(shown.count) shown" }

    func controlTextDidChange(_ obj: Notification) { rebuild() }

    @objc private func scopeChanged() {
        owner.sidebar.historyScope = ["pane", "all", "saved"][max(0, min(2, scope.selectedSegment))]
        owner.scheduleSave()
        rebuild()
    }

    @objc private func askHistory() { ClaudePane.history(in: owner) }

    @objc private func savedFilterChanged(_ pop: NSPopUpButton) {
        switch pop {
        case timePop: since = pop.indexOfSelectedItem
        case kubePop: kube = pop.selectedItem?.representedObject as? String ?? ""
        default: aws = pop.selectedItem?.representedObject as? String ?? ""
        }
        loadSaved()
    }

    private func key(_ e: History.Entry) -> String { "\(e.paneId):\(e.id):\(e.startedAt.timeIntervalSince1970)" }
    private func entry(_ id: String) -> History.Entry? { shown.first { key($0) == id } }

    private var source: [History.Entry] {
        if all { return records.history.merged(Array(owner.model.panes.keys)) }
        return owner.activePane.map { records.history.of($0.id) } ?? []
    }

    private func rebuild() {
        scope.selectedSegment = saved ? 2 : all ? 1 : 0
        for pop in [timePop, kubePop, awsPop] { pop.isHidden = !saved }
        askClaude.isHidden = !saved
        filter.placeholderString = saved ? "Search saved commands" : "Filter commands"
        if saved {
            loadSaved()
            return
        }
        shown = source.filter { History.matches($0, filter.stringValue) }.reversed()
        trash.isEnabled = !(source.isEmpty && filter.stringValue.isEmpty)
        list.set([.init(title: nil, rows: shown.map(row))])
        note?.removeFromSuperview()
        note = nil
        if shown.isEmpty {
            let q = filter.stringValue.trimmingCharacters(in: .whitespaces)
            if !q.isEmpty {
                note = paneNote(nil, "No commands match “\(filter.stringValue)”.")
            } else if !all, let p = owner.activePane, !p.integrated {
                note = paneNote("No history for this shell", "History comes from shell integration, which supports bash and zsh. Check that shell-integration is on in your settings; it applies to new panes.")
            } else {
                note = paneNote("No commands yet", "Commands you run here appear with when they finished and how long they took.")
            }
            addSubview(note!)
        }
        needsLayout = true
    }

    // MARK: Saved (#17)

    private var query: Go.cmdhistory.Query {
        var q = Go.cmdhistory.Query(text: filter.stringValue, limit: 500)
        let cal = Calendar.current
        switch since {
        case 1: q.since = ISO8601DateFormatter().string(from: cal.startOfDay(for: Date()))
        case 2: q.since = ISO8601DateFormatter().string(from: Date().addingTimeInterval(-7 * 86400))
        case 3: q.since = ISO8601DateFormatter().string(from: Date().addingTimeInterval(-30 * 86400))
        default: break
        }
        q.kubeContext = kube
        let parts = aws.split(separator: ":", maxSplits: 1).map(String.init)
        if parts.count == 2 {
            let value = parts[1]
            switch parts[0] {
            case "account": q.awsAccount = value
            case "profile": q.awsProfile = value
            default: q.awsRegion = value
            }
        }
        return q
    }

    /// Asks the backend; only while Saved shows (and once more if asked meanwhile).
    private func loadSaved() {
        guard !loading else { again = true; return }
        loading = true
        let q = query
        Task { @MainActor in
            let r = try? await Services.cmdhistory.Search(q)
            loading = false
            if again {
                again = false
                loadSaved()
                return
            }
            guard saved else { return }
            result = r
            showSaved()
        }
    }

    private func fillPops(_ r: Go.cmdhistory.Result) {
        kubePop.removeAllItems()
        kubePop.addItem(withTitle: "Any Kubernetes")
        func item(_ title: String, _ value: String) -> NSMenuItem {
            let i = NSMenuItem(title: title, action: nil, keyEquivalent: "")
            i.representedObject = value
            return i
        }
        for f in r.kubeContexts { kubePop.menu?.addItem(item("\(f.value) (\(f.count))", f.value)) }
        kubePop.selectItem(at: max(0, kubePop.indexOfItem(withRepresentedObject: kube)))
        awsPop.removeAllItems()
        awsPop.addItem(withTitle: "Any AWS")
        func section(_ title: String, _ kind: String, _ list: [Go.cmdhistory.Facet]) {
            guard !list.isEmpty else { return }
            awsPop.menu?.addItem(.separator())
            awsPop.menu?.addItem(NSMenuItem.sectionHeader(title: title))
            for f in list { awsPop.menu?.addItem(item("\(f.value) (\(f.count))", "\(kind):\(f.value)")) }
        }
        section("Accounts", "account", r.awsAccounts)
        section("Profiles", "profile", r.awsProfiles)
        section("Regions", "region", r.awsRegions)
        awsPop.selectItem(at: max(0, awsPop.indexOfItem(withRepresentedObject: aws)))
        timePop.selectItem(at: since)
    }

    private func showSaved() {
        guard let r = result else { return }
        fillPops(r)
        savedShown = r.entries
        trash.isEnabled = r.saving
        list.set([.init(title: r.total > r.entries.count ? "Newest \(r.entries.count) of \(r.total)" : nil, rows: r.entries.map(savedRow))])
        note?.removeFromSuperview()
        note = nil
        if r.entries.isEmpty {
            if !r.saving {
                note = paneNote("Saving is off", "history-days is 0 in config.toml, so commands aren't saved. Set it to the days to keep (90 by default).")
            } else if !filter.stringValue.isEmpty || since != 0 || !kube.isEmpty || !aws.isEmpty {
                note = paneNote(nil, "No saved commands match.")
            } else {
                note = paneNote("No saved commands yet", "Commands you run in any wtf shell are kept here for \(r.days) days, with the Kubernetes context and AWS account they ran under. Secrets are hidden before they're saved.")
            }
            addSubview(note!)
        }
        needsLayout = true
    }

    private func savedEntry(_ id: String) -> Go.cmdhistory.Entry? { savedShown.first { $0.id == id } }

    private func savedRow(_ e: Go.cmdhistory.Entry) -> SideList.Row {
        let cmd = Redaction.shared.text(e.command)
        let cwd = WindowModel.prettyPath(e.cwd)
        let at = ISO8601DateFormatter().date(from: e.startedAt) ?? Date()
        var r = SideList.Row(id: e.id, symbol: e.exitCode == 0 ? "checkmark.circle.fill" : "xmark.circle.fill",
                             tint: e.exitCode == 0 ? .systemGreen : .systemRed, title: cmd, mono: true,
                             detail: "\(clockTime(at)) · \(Format.duration(ms: e.durationMs)) · \(cwd)",
                             tip: "\(cmd)\nin \(cwd)\nstarted \(clockTime(at))")
        if e.exitCode != 0 { r.pills.append(.init(text: "exit \(e.exitCode)", color: .systemRed)) }
        if !e.kubeContext.isEmpty {
            let ns = e.kubeNamespace.isEmpty ? "" : "/\(e.kubeNamespace)"
            r.pills.append(.init(text: e.kubeContext + ns, color: .controlAccentColor, tip: "Kubernetes context \(e.kubeContext)\(e.kubeNamespace.isEmpty ? "" : ", namespace \(e.kubeNamespace)")"))
        }
        let awsText = [e.awsAccount.isEmpty ? e.awsProfile : e.awsAccount, e.awsRegion].filter { !$0.isEmpty }.joined(separator: " ")
        if !awsText.isEmpty {
            r.pills.append(.init(text: awsText, color: .systemOrange, tip: "AWS profile \(e.awsProfile)\(e.awsAccount.isEmpty ? "" : ", account \(e.awsAccount)")\(e.awsRegion.isEmpty ? "" : ", region \(e.awsRegion)")"))
        }
        return r
    }

    private func savedMenu(_ e: Go.cmdhistory.Entry) -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        func add(_ t: String, _ run: @escaping () -> Void) { m.addItem(ClosureMenuItem(title: t, keyEquivalent: "", action: run)) }
        let o = owner
        add("Run Again") { [weak self] in self?.runAgain(command: e.command) }
        add("Insert at Prompt") { o.activeTerminal?.insert(e.command) }
        m.addItem(.separator())
        add("Copy Command") {
            Clipboard.copy(Redaction.shared.text(e.command))
            Toast.show("Copied command", in: o.rootView)
        }
        add("Copy Folder") {
            Clipboard.copy(e.cwd)
            Toast.show("Copied folder", in: o.rootView)
        }
        m.addItem(.separator())
        add("Save as Snippet…") { SnippetEditor.show(.new(command: e.command), in: o) }
        add("Forget This Command") {
            Task { @MainActor in
                try? await Services.cmdhistory.Forget(e.id)
                SavedHistory.changed()
            }
        }
        return m
    }

    private func forgetAllSaved() {
        let o = owner
        Confirm.show(in: owner.window, title: "Forget all saved commands?",
                     message: "Removes every command wtf saved, from every shell, with its context. Each shell's own history (↑ and Ctrl-R) stays as it is.",
                     items: [], button: "Forget All") {
            Task { @MainActor in
                do {
                    try await Services.cmdhistory.ForgetAll()
                    SavedHistory.changed()
                    Toast.show("Forgot the saved commands", in: o.rootView)
                } catch {
                    Toast.show("Couldn't forget them: \(error.localizedDescription)", in: o.rootView)
                }
            }
        }
    }

    private func row(_ e: History.Entry) -> SideList.Row {
        let cmd = Redaction.shared.text(e.command)
        let cwd = WindowModel.prettyPath(e.cwd)
        let detail: String
        let alert = ""
        if let fin = e.finishedAt {
            detail = "\(clockTime(fin)) · \(Format.duration(ms: e.durationMs ?? 0)) · \(cwd)"
        } else {
            detail = "running · since \(clockTime(e.startedAt)) · \(cwd)"
        }
        var r = SideList.Row(id: key(e), symbol: e.running ? "circle.fill" : e.exitCode == 0 ? "checkmark.circle.fill" : "xmark.circle.fill",
                             tint: e.running ? .controlAccentColor : e.exitCode == 0 ? .systemGreen : .systemRed,
                             title: cmd, mono: true, detail: detail, alert: alert,
                             tip: "\(cmd)\nin \(cwd)\nstarted \(clockTime(e.startedAt))")
        if let code = e.exitCode, code != 0 { r.pills.append(.init(text: "exit \(code)", color: .systemRed)) }
        if all, let t = owner.model.tabOf(pane: e.paneId) {
            let title = owner.model.title(t)
            r.pills.append(.init(text: title.count > 16 ? String(title.prefix(15)) + "…" : title, color: .controlAccentColor, tip: "In “\(title)”"))
        }
        return r
    }

    private func insert(_ e: History.Entry) {
        owner.activeTerminal?.insert(e.command)
    }

    private func runAgain(_ e: History.Entry) { runAgain(command: e.command) }

    private func runAgain(command: String) {
        guard let p = owner.activePane, let term = owner.activeTerminal, p.isShell, !p.running else {
            Toast.show("The shell is busy — finish the running program first", in: owner.rootView)
            return
        }
        term.run(command)
    }

    private func menu(_ e: History.Entry) -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        func add(_ t: String, _ run: @escaping () -> Void) { m.addItem(ClosureMenuItem(title: t, keyEquivalent: "", action: run)) }
        let o = owner
        add("Run Again") { [weak self] in self?.runAgain(e) }
        add("Insert at Prompt") { [weak self] in self?.insert(e) }
        add("Show in Terminal") {
            guard o.model.panes[e.paneId] != nil else { return }
            o.show(pane: e.paneId)
            if o.terminals[e.paneId]?.revealCommand(e.id) != true {
                Toast.show("That command has scrolled out of the terminal", in: o.rootView)
            }
        }
        m.addItem(.separator())
        add("Copy Command") {
            Clipboard.copy(Redaction.shared.text(e.command))
            Toast.show("Copied command", in: o.rootView)
        }
        add("Copy Folder") {
            Clipboard.copy(e.cwd)
            Toast.show("Copied folder", in: o.rootView)
        }
        m.addItem(.separator())
        add("Save as Snippet…") { SnippetEditor.show(.new(command: e.command), in: o) }
        return m
    }

    /// Clears the list, and the shells' own history for this session where
    /// the shell is idle at its prompt (a key binding, nothing typed shows).
    @objc private func clear() {
        if saved { return forgetAllSaved() }
        let targets: [Pane] = all ? Array(owner.model.panes.values) : owner.activePane.map { [$0] } ?? []
        guard !targets.isEmpty else { return }
        let count = targets.reduce(0) { $0 + records.history.of($1.id).count }
        func idle(_ p: Pane) -> Bool { p.integrated && p.isShell && !p.running }
        let busy = targets.filter { $0.integrated && !idle($0) }
        let o = owner
        Confirm.show(in: owner.window, title: targets.count > 1 ? "Clear command history for all panes?" : "Clear command history?",
                     message: "Removes \(count) \(count == 1 ? "command" : "commands") from this list and from the shell's own history for this session (↑ and Ctrl-R). It also goes from wtf’s saved history. History from earlier sessions stays, and so do commands your shell already wrote to its history file.",
                     items: busy.map { "\(o.label(pane: $0.id)): a program is running — only the list will be cleared" },
                     button: "Clear History") { [weak self] in
            guard let self else { return }
            var shells = 0
            for p in targets {
                records.history.clear(p.id)
                if let s = p.sessionId { Task { try? await Services.cmdhistory.ForgetSession(s); SavedHistory.changed() } }
                if idle(p), let term = o.terminals[p.id] {
                    term.type("\u{18}\u{1d}")
                    shells += 1
                }
            }
            records.changed()
            let n = busy.count
            Toast.show(n > 0 ? "Cleared — \(n) busy \(n == 1 ? "shell's" : "shells'") own history left as is"
                : shells > 0 ? "Cleared this session's history" + (shells > 1 ? " in \(shells) shells" : "") : "Cleared the list", in: o.rootView)
        }
    }
}

/// The Pinned tab: output kept in view, newest first; a running command's
/// pin follows it (every 1.5s, only while this tab shows). Like PinsView.svelte.
@MainActor
final class PinsView: NSView, SidePaneTab {
    private unowned let owner: WindowController
    private let header = PaneHeader("Pinned output")
    private let scroll = NSScrollView()
    private let stack = NSStackView()
    private var cards: [Int: PinCard] = [:]
    private var note: NoteView?
    private var trash: ToolButton?
    private var timer: Timer?
    private var records: WindowRecords { owner.records }

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        trash = header.tool("trash", "Unpin all") { [weak self] in self?.unpinAll() }
        addSubview(header)
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 8
        stack.edgeInsets = NSEdgeInsets(top: 4, left: 8, bottom: 12, right: 8)
        let doc = FlippedView()
        doc.addSubview(stack)
        scroll.documentView = doc
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        addSubview(scroll)
        records.observe(self) { $0.rebuild() }
        Redaction.shared.observe(self) { $0.rebuild() }
        rebuild()
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        if window == nil {
            timer?.invalidate()
            timer = nil
        }
    }

    override func layout() {
        super.layout()
        header.frame = NSRect(x: 0, y: 0, width: bounds.width, height: 30)
        scroll.frame = NSRect(x: 0, y: 30, width: bounds.width, height: max(0, bounds.height - 30))
        note?.frame = NSRect(x: 0, y: 30, width: bounds.width, height: note?.height(for: bounds.width) ?? 0)
        fit()
    }

    private func fit() {
        let w = scroll.contentSize.width
        for c in cards.values { c.width = w - 16 }
        stack.frame.size.width = w
        stack.layoutSubtreeIfNeeded()
        let h = stack.fittingSize.height
        stack.frame = NSRect(x: 0, y: 0, width: w, height: h)
        scroll.documentView?.frame = NSRect(x: 0, y: 0, width: w, height: h)
    }

    func debugSummary() -> String { "pins: \(records.pins.list.count)" }

    private func rebuild() {
        let pins = records.pins.list
        trash?.isEnabled = !pins.isEmpty
        let ids = Set(pins.map(\.id))
        for (id, c) in cards where !ids.contains(id) {
            c.removeFromSuperview()
            cards[id] = nil
        }
        for (i, p) in pins.enumerated() {
            let c = cards[p.id] ?? PinCard(owner: owner)
            c.onResize = { [weak self] in self?.needsLayout = true }
            cards[p.id] = c
            c.set(p)
            if stack.arrangedSubviews.firstIndex(of: c) != i {
                c.removeFromSuperview()
                stack.insertArrangedSubview(c, at: min(i, stack.arrangedSubviews.count))
            }
        }
        note?.removeFromSuperview()
        note = nil
        if pins.isEmpty {
            note = paneNote("Nothing pinned", "Keep a command's output in view while you work on: click its ✓/✗ mark (or right-click its output) and choose Pin Output, or select text and right-click → Pin Selection. A command still running keeps its pin up to date.")
            addSubview(note!)
        }
        // Follow running commands only while there are any.
        if records.pins.live.isEmpty {
            timer?.invalidate()
            timer = nil
        } else if timer == nil {
            timer = Timer.scheduledTimer(withTimeInterval: 1.5, repeats: true) { [weak self] _ in
                MainActor.assumeIsolated { self?.follow() }
            }
        }
        needsLayout = true
    }

    private func follow() {
        var changed = false
        for p in records.pins.live {
            guard let b = p.blockId, let text = owner.terminals[p.paneId]?.blockOutput(b), text != p.text else { continue }
            records.pins.update(pane: p.paneId, block: b, text: text)
            changed = true
        }
        if changed { records.changed() }
    }

    private func unpinAll() {
        let n = records.pins.list.count
        guard n > 0 else { return }
        Confirm.show(in: owner.window, title: "Unpin everything?",
                     message: "Removes \(n) pinned \(n == 1 ? "output" : "outputs"). The terminals keep their own scrollback.",
                     button: "Unpin All") { [weak self] in
            self?.records.pins.clear()
            self?.records.changed()
        }
    }
}

/// One pin: a header (status, title, copy, collapse, unpin) over its text.
@MainActor
final class PinCard: NSView {
    private unowned let owner: WindowController
    private var pin: Pins.Pin?
    private let mark = NSImageView()
    private let title = NSTextField(labelWithString: "")
    private let sub = NSTextField(labelWithString: "")
    private let note = NSTextField(labelWithString: "")
    private let textScroll = NSScrollView()
    private let text = NSTextView()
    private var chevron: ToolButton!
    private let head = PinHead()
    var width: CGFloat = 280 { didSet { if width != oldValue { needsLayout = true; invalidateIntrinsicContentSize() } } }
    private var bodyHeight: CGFloat = 180
    /// Set by dragging the bottom edge: kept as the output grows.
    private var chosenHeight: CGFloat?
    private let grip = PinGrip()
    /// The list lays itself out again (a pin changed height).
    var onResize: (() -> Void)?

    init(owner: WindowController) {
        self.owner = owner
        super.init(frame: .zero)
        wantsLayer = true
        layer?.cornerRadius = 8
        layer?.borderWidth = 1
        layer?.borderColor = NSColor.separatorColor.cgColor
        mark.symbolConfiguration = .init(pointSize: 12, weight: .medium)
        title.lineBreakMode = .byTruncatingTail
        sub.font = .systemFont(ofSize: 11)
        sub.textColor = .secondaryLabelColor
        sub.lineBreakMode = .byTruncatingTail
        note.font = .systemFont(ofSize: 11)
        note.textColor = .secondaryLabelColor
        let copy = ToolButton(symbol: "doc.on.doc", tip: "Copy") { [weak self] in self?.copy() }
        chevron = ToolButton(symbol: "chevron.up", tip: "Collapse") { [weak self] in self?.toggle() }
        let unpin = ToolButton(symbol: "xmark", tip: "Unpin") { [weak self] in self?.unpin() }
        head.onDoubleClick = { [weak self] in self?.toggle() }
        head.menuProvider = { [weak self] in self?.menu() }
        [mark, title, sub, copy, chevron, unpin].forEach(head.addSubview)
        head.buttons = [copy, chevron, unpin]
        addSubview(head)
        text.isEditable = false
        text.isSelectable = true
        text.font = .monospacedSystemFont(ofSize: 11.5, weight: .regular)
        text.isHorizontallyResizable = true
        text.textContainer?.widthTracksTextView = false
        text.textContainer?.containerSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
        text.drawsBackground = false
        textScroll.documentView = text
        textScroll.hasVerticalScroller = true
        textScroll.hasHorizontalScroller = true
        textScroll.autohidesScrollers = true
        textScroll.drawsBackground = false
        addSubview(note)
        addSubview(textScroll)
        // Drag the bottom edge to make the output taller (60pt to 70% of the window).
        grip.onDrag = { [weak self] dy, start in
            guard let self else { return }
            let maxH = (window?.contentView?.bounds.height ?? 700) * 0.7
            chosenHeight = min(maxH, max(60, start + dy))
            bodyHeight = chosenHeight!
            invalidateIntrinsicContentSize()
            needsLayout = true
            onResize?()
        }
        grip.startHeight = { [weak self] in self?.bodyHeight ?? 180 }
        addSubview(grip)
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    private var showsBody: Bool { pin.map { !$0.collapsed } ?? false }

    override var intrinsicContentSize: NSSize {
        var h: CGFloat = 40
        if showsBody {
            if pin?.truncated == true { h += 18 }
            h += (pin?.text.isEmpty ?? true) ? 22 : bodyHeight + 6
        }
        return NSSize(width: width, height: h)
    }

    func set(_ p: Pins.Pin) {
        let old = pin
        pin = p
        let status: (String, NSColor) = p.blockId == nil ? ("pin.fill", .controlAccentColor)
            : p.running ? ("circle.fill", .controlAccentColor) : p.exitCode == 0 ? ("checkmark.circle.fill", .systemGreen) : ("xmark.circle.fill", .systemRed)
        mark.image = NSImage(systemSymbolName: status.0, accessibilityDescription: nil)
        mark.contentTintColor = status.1
        title.stringValue = Redaction.shared.text(p.title)
        title.toolTip = title.stringValue
        title.font = p.blockId == nil ? .systemFont(ofSize: 12, weight: .semibold) : .monospacedSystemFont(ofSize: 12, weight: .regular)
        var s = p.running ? "running · following it"
            : p.durationMs.map { Format.duration(ms: $0) + (p.exitCode.map { $0 != 0 ? " · exit \($0)" : "" } ?? "") } ?? "pinned \(clockTime(p.pinnedAt))"
        if let t = owner.model.tabOf(pane: p.paneId) { s += " · \(owner.model.title(t))" } else { s += " · closed tab" }
        sub.stringValue = s
        sub.toolTip = WindowModel.prettyPath(p.cwd)
        chevron.image = NSImage(systemSymbolName: p.collapsed ? "chevron.down" : "chevron.up", accessibilityDescription: nil)
        chevron.toolTip = p.collapsed ? "Expand" : "Collapse"
        note.isHidden = !showsBody || !(p.truncated || p.text.isEmpty)
        note.stringValue = p.text.isEmpty ? (p.running ? "No output yet" : "No output") : "Showing the last \(Int((Double(p.text.count) / 1000).rounded()))k characters"
        textScroll.isHidden = !showsBody || p.text.isEmpty
        let shown = Redaction.shared.text(p.text)
        if old?.text != p.text || text.string != shown {
            // Keep reading where you were unless you were at the end.
            let atEnd = (textScroll.documentVisibleRect.maxY >= text.frame.height - 4)
            text.string = shown
            text.sizeToFit()
            // As tall as the output, up to 180pt; longer output scrolls.
            text.layoutManager?.ensureLayout(for: text.textContainer!)
            let used = text.layoutManager?.usedRect(for: text.textContainer!).height ?? 0
            bodyHeight = chosenHeight ?? min(180, max(40, ceil(used) + 12))
            if atEnd { text.scrollToEndOfDocument(nil) }
        }
        invalidateIntrinsicContentSize()
        needsLayout = true
    }

    override func layout() {
        super.layout()
        head.frame = NSRect(x: 0, y: 0, width: bounds.width, height: 40)
        mark.frame = NSRect(x: 8, y: 12, width: 16, height: 16)
        let buttons = head.buttons
        var x = bounds.width - 6
        for b in buttons.reversed() {
            x -= 22
            b.frame = NSRect(x: x, y: 9, width: 22, height: 22)
        }
        title.frame = NSRect(x: 30, y: 4, width: max(0, x - 34), height: 17)
        sub.frame = NSRect(x: 30, y: 21, width: max(0, x - 34), height: 15)
        var y: CGFloat = 40
        if !note.isHidden {
            note.frame = NSRect(x: 10, y: y, width: bounds.width - 20, height: 16)
            y += 18
        }
        textScroll.frame = NSRect(x: 4, y: y, width: bounds.width - 8, height: bodyHeight)
        grip.frame = NSRect(x: 0, y: y + bodyHeight, width: bounds.width, height: 6)
        grip.isHidden = textScroll.isHidden
    }

    private func copy() {
        guard let p = pin else { return }
        Clipboard.copy(Redaction.shared.text(p.text))
        Toast.show("Copied pinned output", in: owner.rootView)
    }

    private func toggle() {
        guard let p = pin else { return }
        owner.records.pins.toggle(p.id)
        owner.records.changed()
    }

    private func unpin() {
        guard let p = pin else { return }
        owner.records.pins.remove(p.id)
        owner.records.changed()
    }

    private func menu() -> NSMenu? {
        guard let p = pin else { return nil }
        let m = NSMenu()
        m.autoenablesItems = false
        func add(_ t: String, _ run: @escaping () -> Void) { m.addItem(ClosureMenuItem(title: t, keyEquivalent: "", action: run)) }
        let o = owner
        if o.model.panes[p.paneId] != nil {
            add(p.blockId == nil ? "Show Tab" : "Show in Terminal") {
                o.show(pane: p.paneId)
                if let b = p.blockId, o.terminals[p.paneId]?.revealCommand(b) != true {
                    Toast.show("That command has scrolled out of the terminal", in: o.rootView)
                }
            }
        }
        add("Copy Output") { [weak self] in self?.copy() }
        if p.blockId != nil {
            add("Copy Command") {
                Clipboard.copy(Redaction.shared.text(p.title))
                Toast.show("Copied command", in: o.rootView)
            }
            add("Save as Snippet…") { SnippetEditor.show(.new(command: p.title), in: o) }
        }
        m.addItem(.separator())
        add(p.collapsed ? "Expand" : "Collapse") { [weak self] in self?.toggle() }
        add("Unpin") { [weak self] in self?.unpin() }
        return m
    }
}

/// A pin's header: double-click folds it, right-click shows its menu.
@MainActor
final class PinHead: NSView {
    var onDoubleClick: (() -> Void)?
    var menuProvider: (() -> NSMenu?)?
    var buttons: [NSView] = []

    override var isFlipped: Bool { true }

    override func mouseDown(with event: NSEvent) {
        if event.clickCount == 2 { onDoubleClick?() }
    }

    override func menu(for event: NSEvent) -> NSMenu? { menuProvider?() }
}

/// A pin's bottom edge: drag it to resize the output.
@MainActor
final class PinGrip: NSView {
    var onDrag: ((_ dy: CGFloat, _ start: CGFloat) -> Void)?
    var startHeight: (() -> CGFloat)?
    private var start: (y: CGFloat, h: CGFloat)?

    override func resetCursorRects() { addCursorRect(bounds, cursor: .resizeUpDown) }
    override func mouseDown(with event: NSEvent) { start = (NSEvent.mouseLocation.y, startHeight?() ?? 180) }
    override func mouseDragged(with event: NSEvent) {
        guard let start else { return }
        onDrag?(start.y - NSEvent.mouseLocation.y, start.h)
    }
    override func mouseUp(with event: NSEvent) { start = nil }
}

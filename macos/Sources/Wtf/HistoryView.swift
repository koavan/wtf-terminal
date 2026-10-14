import AppKit
import WtfCore

/// When a command ran: today's clock, or the day too.
@MainActor
func clockTime(_ d: Date) -> String {
    let f = DateFormatter()
    f.dateFormat = DateFormatter.dateFormat(fromTemplate: Calendar.current.isDateInToday(d) ? "jmmss" : "MMMdjmmss", options: 0, locale: .current)
    return f.string(from: d)
}

/// The History tab: the commands this pane (or every pane in the window)
/// ran, newest first, with how they went. Click to put one at the prompt,
/// double-click to run it again. Like HistoryView.svelte.
@MainActor
final class HistoryView: NSView, SidePaneTab, NSSearchFieldDelegate {
    private unowned let owner: WindowController
    private let scope = NSSegmentedControl(labels: ["This pane", "All panes"], trackingMode: .selectOne, target: nil, action: nil)
    private let trash = ToolButton(symbol: "trash", tip: "Clear history") {}
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
        filter.placeholderString = "Filter commands"
        filter.delegate = self
        filter.sendsSearchStringImmediately = true
        filter.refusesFirstResponder = true
        addSubview(filter)
        list.onClick = { [weak self] id in
            // The second click of a double-click runs it instead.
            guard NSApp.currentEvent?.clickCount ?? 1 == 1 else { return }
            self?.entry(id).map { self?.insert($0) }
        }
        list.onDoubleClick = { [weak self] id in self?.entry(id).map { self?.runAgain($0) } }
        list.menuProvider = { [weak self] ids in ids.first.flatMap { self?.entry($0) }.flatMap { self?.menu($0) } }
        addSubview(list)
        records.observe(self) { $0.rebuild() }
        Redaction.shared.observe(self) { $0.rebuild() }
        rebuild()
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    override func layout() {
        super.layout()
        scope.frame = NSRect(x: 8, y: 4, width: bounds.width - 46, height: 22)
        trash.frame = NSRect(x: bounds.width - 32, y: 3, width: 24, height: 24)
        filter.frame = NSRect(x: 8, y: 32, width: bounds.width - 16, height: 24)
        list.frame = NSRect(x: 0, y: 62, width: bounds.width, height: max(0, bounds.height - 62))
        note?.frame = NSRect(x: 0, y: 62, width: bounds.width, height: note?.height(for: bounds.width) ?? 0)
    }

    private var all: Bool { owner.sidebar.historyScope == "all" }

    func paneChanged() { rebuild() }

    func debugSummary() -> String { "history: \(shown.count) shown" }

    func controlTextDidChange(_ obj: Notification) { rebuild() }

    @objc private func scopeChanged() {
        owner.sidebar.historyScope = scope.selectedSegment == 1 ? "all" : "pane"
        owner.scheduleSave()
        rebuild()
    }

    private func key(_ e: History.Entry) -> String { "\(e.paneId):\(e.id):\(e.startedAt.timeIntervalSince1970)" }
    private func entry(_ id: String) -> History.Entry? { shown.first { key($0) == id } }

    private var source: [History.Entry] {
        if all { return records.history.merged(Array(owner.model.panes.keys)) }
        return owner.activePane.map { records.history.of($0.id) } ?? []
    }

    private func rebuild() {
        scope.selectedSegment = all ? 1 : 0
        shown = source.filter { History.matches($0, filter.stringValue) }.reversed()
        trash.isEnabled = !(source.isEmpty && filter.stringValue.isEmpty)
        list.set([.init(title: nil, rows: shown.map(row))])
        note?.removeFromSuperview()
        note = nil
        if shown.isEmpty {
            let q = filter.stringValue.trimmingCharacters(in: .whitespaces)
            if !q.isEmpty {
                note = paneNote(nil, "No commands match “\(filter.stringValue)”.")
            } else if !all, owner.activePane?.integrated != true {
                note = paneNote("No history for this shell", "History comes from shell integration, which supports bash and zsh. Check that shell-integration is on in your settings; it applies to new panes.")
            } else {
                note = paneNote("No commands yet", "Commands you run here appear with when they finished and how long they took.")
            }
            addSubview(note!)
        }
        needsLayout = true
    }

    private func row(_ e: History.Entry) -> SideList.Row {
        let cmd = Redaction.shared.text(e.command)
        let cwd = WindowModel.prettyPath(e.cwd)
        let detail: String
        var alert = ""
        if let fin = e.finishedAt {
            detail = "\(clockTime(fin)) · \(Format.duration(ms: e.durationMs ?? 0))" + (e.exitCode == 0 ? "" : " · exit \(e.exitCode ?? 0)") + " · \(cwd)"
            if e.exitCode != 0 { alert = "" }
        } else {
            detail = "running · since \(clockTime(e.startedAt)) · \(cwd)"
        }
        var r = SideList.Row(id: key(e), symbol: e.running ? "circle.fill" : e.exitCode == 0 ? "checkmark.circle.fill" : "xmark.circle.fill",
                             tint: e.running ? .controlAccentColor : e.exitCode == 0 ? .systemGreen : .systemRed,
                             title: cmd, mono: true, detail: detail, alert: alert,
                             tip: "\(cmd)\nin \(cwd)\nstarted \(clockTime(e.startedAt))")
        if all, let t = owner.model.tabOf(pane: e.paneId) {
            let title = owner.model.title(t)
            r.pills = [.init(text: title.count > 16 ? String(title.prefix(15)) + "…" : title, color: .controlAccentColor, tip: "In “\(title)”")]
        }
        return r
    }

    private func insert(_ e: History.Entry) {
        owner.activeTerminal?.insert(e.command)
    }

    private func runAgain(_ e: History.Entry) {
        guard let p = owner.activePane, let term = owner.activeTerminal, p.isShell, !p.running else {
            Toast.show("The shell is busy — finish the running program first", in: owner.rootView)
            return
        }
        term.run(e.command)
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
        let targets: [Pane] = all ? Array(owner.model.panes.values) : owner.activePane.map { [$0] } ?? []
        guard !targets.isEmpty else { return }
        let count = targets.reduce(0) { $0 + records.history.of($1.id).count }
        func idle(_ p: Pane) -> Bool { p.integrated && p.isShell && !p.running }
        let busy = targets.filter { $0.integrated && !idle($0) }
        let o = owner
        Confirm.show(in: owner.window, title: targets.count > 1 ? "Clear command history for all panes?" : "Clear command history?",
                     message: "Removes \(count) \(count == 1 ? "command" : "commands") from this list and from the shell's own history for this session (↑ and Ctrl-R). History from earlier sessions stays, and so do commands your shell already wrote to its history file.",
                     items: busy.map { "\(o.label(pane: $0.id)): a program is running — only the list will be cleared" },
                     button: "Clear History") { [weak self] in
            guard let self else { return }
            var shells = 0
            for p in targets {
                records.history.clear(p.id)
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
            bodyHeight = min(180, max(40, ceil(used) + 12))
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

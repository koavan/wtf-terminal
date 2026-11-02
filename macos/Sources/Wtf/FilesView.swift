import AppKit
import WtfCore

/// The Files tab: the focused pane's folder (or a pinned one) as a tree,
/// with git status. Clicking a file types its path at the prompt;
/// double-clicking a folder cds there. Nothing in it changes files. It reads
/// folders when the root changes and after each command (while
/// auto-refresh is on), never on a timer. Like FileTree.svelte.
@MainActor
final class FilesView: NSView, SidePaneTab, NSTableViewDataSource, NSTableViewDelegate, NSSearchFieldDelegate {
    static let remoteProcesses: Set<String> = ["ssh", "mosh", "mosh-client", "et", "telnet"]
    static let pathType = NSPasteboard.PasteboardType("com.dayanraj.wtf.path")

    private unowned let owner: WindowController
    private let header = PaneHeader("")
    private var back: ToolButton!
    private var up: ToolButton!
    private var pin: ToolButton!
    private let paused = NSTextField(labelWithString: "Paused")
    private let gitBar = NSStackView()
    private let gitLabel = NSTextField(labelWithString: "")
    private let gitButton = NSButton(title: "", target: nil, action: nil)
    private let filter = NSSearchField()
    private let table = FileTable()
    private let scroll = NSScrollView()
    private var note: NoteView?

    private var tree = FileTree()
    private var git: GitIndex?
    private var gitSeq = 0
    private var changesOnly = false
    private var rows: [FileTree.Row] = []
    private var changes: [Go.files.GitChange] = []
    private var shownRoot = ""
    private var loading = Set<String>()
    private var refreshTimer: Timer?
    private var lastCommandKey: String?

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        back = header.tool("chevron.left", "Back") { [weak self] in self?.goBack() }
        up = header.tool("chevron.up", "Up") { [weak self] in self?.goUp() }
        pin = header.tool("pin", "Pin this folder") { [weak self] in self?.togglePin() }
        header.refreshControls(in: self) { [weak self] in self?.refresh() }
        header.tool("rectangle.compress.vertical", "Collapse all") { [weak self] in
            self?.tree.expanded.removeAll()
            self?.rebuild()
        }
        addSubview(header)

        paused.font = .systemFont(ofSize: 11)
        paused.textColor = .secondaryLabelColor
        addSubview(paused)

        let branchIcon = NSImageView(image: NSImage(systemSymbolName: "arrow.triangle.branch", accessibilityDescription: nil)!)
        branchIcon.contentTintColor = .secondaryLabelColor
        gitLabel.font = .monospacedDigitSystemFont(ofSize: 11.5, weight: .regular)
        gitLabel.textColor = .secondaryLabelColor
        gitLabel.lineBreakMode = .byTruncatingTail
        gitLabel.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        gitButton.bezelStyle = .accessoryBarAction
        gitButton.font = .systemFont(ofSize: 11, weight: .semibold)
        gitButton.refusesFirstResponder = true
        gitButton.target = self
        gitButton.action = #selector(toggleChanges)
        gitBar.setViews([branchIcon, gitLabel, NSView(), gitButton], in: .leading)
        gitBar.spacing = 4
        addSubview(gitBar)

        filter.placeholderString = "Filter"
        filter.refusesFirstResponder = true
        filter.font = .systemFont(ofSize: 12)
        filter.delegate = self
        filter.sendsSearchStringImmediately = true
        addSubview(filter)

        let col = NSTableColumn(identifier: .init("row"))
        table.addTableColumn(col)
        table.headerView = nil
        table.rowHeight = 24
        table.intercellSpacing = NSSize(width: 0, height: 0)
        table.style = .plain
        table.backgroundColor = .clear
        table.selectionHighlightStyle = .regular
        table.dataSource = self
        table.delegate = self
        table.target = self
        table.action = #selector(clicked)
        table.doubleAction = #selector(doubleClicked)
        table.owner = self
        table.setDraggingSourceOperationMask(.copy, forLocal: false)
        scroll.documentView = table
        scroll.drawsBackground = false
        scroll.hasVerticalScroller = true
        scroll.autohidesScrollers = true
        addSubview(scroll)
        paneChanged()
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    override func layout() {
        super.layout()
        var y: CGFloat = 0
        header.frame = NSRect(x: 0, y: y, width: bounds.width, height: 30)
        y += 30
        let auto = Settings.shared.config?.sidebarAutoRefresh ?? true
        paused.isHidden = auto
        if !auto {
            paused.frame = NSRect(x: 10, y: y, width: bounds.width - 20, height: 16)
            y += 18
        }
        gitBar.isHidden = git == nil || remote
        if !gitBar.isHidden {
            gitBar.frame = NSRect(x: 12, y: y, width: bounds.width - 20, height: 22)
            y += 26
        }
        filter.frame = NSRect(x: 8, y: y + 2, width: bounds.width - 16, height: 24)
        y += 32
        scroll.frame = NSRect(x: 0, y: y, width: bounds.width, height: max(0, bounds.height - y))
        note?.frame = NSRect(x: 0, y: y, width: bounds.width, height: note?.height(for: bounds.width) ?? 0)
        table.tableColumns.first?.width = scroll.contentSize.width
    }

    // MARK: State

    private var pane: Pane? { owner.activePane }
    private var pinned: String? { owner.sidebar.pinned }
    private var root: String { pinned ?? pane?.cwd ?? "" }
    private var remote: Bool { pinned == nil && pane.map { Self.remoteProcesses.contains($0.process) } == true }
    private var historyKey: String { pinned != nil ? "pinned" : "pane:\(pane?.id ?? 0)" }

    /// The focused pane, its folder or its program changed.
    func paneChanged() {
        let r = root
        if r != shownRoot {
            shownRoot = r
            if !r.isEmpty {
                tree.visit(historyKey, r)
                load(r, force: true)
                if !remote { loadGit(r) } else { git = nil }
            } else {
                git = nil
            }
        } else if remote, git != nil {
            git = nil
        }
        rebuild()
    }

    /// A command finished: read the open folders again shortly after
    /// (it may have changed them), if auto-refresh is on.
    func commandFinished() {
        guard Settings.shared.config?.sidebarAutoRefresh ?? true else { return }
        refreshTimer?.invalidate()
        refreshTimer = Timer.scheduledTimer(withTimeInterval: 0.25, repeats: false) { [weak self] _ in
            MainActor.assumeIsolated { self?.refresh() }
        }
    }

    private func load(_ path: String, force: Bool = false) {
        guard !loading.contains(path), force || tree.folders[path] == nil else { return }
        loading.insert(path)
        Task {
            do {
                let l = try await Services.files.List(path)
                tree.folders[path] = .init(entries: l.entries, truncated: l.truncated)
            } catch {
                tree.folders[path] = .init(entries: nil, error: error.localizedDescription)
            }
            loading.remove(path)
            rebuild()
        }
    }

    private func loadGit(_ dir: String) {
        gitSeq += 1
        let mine = gitSeq
        if let g = git, !Paths.inside(dir, g.status.root) { git = nil }
        Task {
            let s = try? await Services.files.GitStatus(dir)
            guard mine == gitSeq else { return }
            git = s.flatMap { $0.root.isEmpty ? nil : GitIndex($0) }
            rebuild()
        }
    }

    func refresh() {
        let r = root
        guard !r.isEmpty, !remote else { return }
        load(r, force: true)
        for path in tree.expanded where Paths.inside(path, r) && path != r { load(path, force: true) }
        loadGit(r)
    }

    func debugSummary() -> String {
        "files: root \(shownRoot), \(rows.count) rows, git \(git.map { GitIndex.branchLabel($0.status) + " \($0.count) changes" } ?? "none")"
    }

    // MARK: Building

    private func rebuild() {
        header.text = shownRoot.isEmpty ? "" : (WindowModel.prettyPath(shownRoot) == "/" ? "/" : Paths.base(WindowModel.prettyPath(shownRoot)))
        header.tooltip = shownRoot
        let h = tree.history[historyKey] ?? []
        let prev = h.count > 1 ? h[h.count - 2] : nil
        back.isEnabled = prev != nil && !remote
        back.toolTip = prev.map { "Back to \(WindowModel.prettyPath($0)) (⌫)" } ?? "Back"
        let r = root
        up.isEnabled = !r.isEmpty && r != "/" && !remote
        up.toolTip = up.isEnabled ? "Up to \(WindowModel.prettyPath(Paths.parent(r))) (⌥↑)" : "Up"
        pin.toolTip = pinned != nil ? "Unpin: follow the current folder" : "Pin this folder"
        pin.contentTintColor = pinned != nil ? .controlAccentColor : .secondaryLabelColor
        pin.image = NSImage(systemSymbolName: pinned != nil ? "pin.fill" : "pin", accessibilityDescription: pin.toolTip)

        if let g = git {
            gitLabel.stringValue = GitIndex.branchLabel(g.status)
            gitLabel.toolTip = g.status.upstream.isEmpty ? "No upstream branch" : "Tracking \(g.status.upstream)"
            if !g.status.error.isEmpty {
                gitButton.title = "status unavailable"
                gitButton.toolTip = g.status.error
                gitButton.isEnabled = false
            } else if g.count > 0 {
                gitButton.title = "\(g.count)\(g.status.truncated ? "+" : "") \(g.count == 1 ? "change" : "changes")"
                gitButton.toolTip = changesOnly ? "Back to the folder tree" : "List the changed files"
                gitButton.isEnabled = true
                gitButton.contentTintColor = changesOnly ? .controlAccentColor : nil
            } else {
                gitButton.title = "no changes"
                gitButton.isEnabled = false
            }
        }

        note?.removeFromSuperview()
        note = nil
        changes = []
        rows = []
        if remote {
            note = paneNote("Remote session", "This pane is connected to another machine with \(pane?.process ?? "ssh"); its files aren't shown here.")
        } else if r.isEmpty {
            note = paneNote(nil, "Waiting for the shell…")
        } else if let g = git, changesOnly, g.count > 0 {
            changes = g.status.changes.sorted { $0.path < $1.path }
        } else if let err = tree.folders[r]?.error {
            note = paneNote("Can't show \(WindowModel.prettyPath(r))", err)
        } else {
            rows = tree.rows(r, filter: filter.stringValue)
            if rows.isEmpty, tree.folders[r]?.entries != nil {
                note = paneNote(nil, filter.stringValue.trimmingCharacters(in: .whitespaces).isEmpty ? "Empty folder" : "No matches in open folders")
            }
        }
        if let note { addSubview(note) }
        scroll.isHidden = note != nil
        table.reloadData()
        needsLayout = true
    }

    // MARK: Table

    func numberOfRows(in tableView: NSTableView) -> Int { changes.isEmpty ? rows.count : changes.count }

    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        let cell = tableView.makeView(withIdentifier: .init("cell"), owner: self) as? FileRowView ?? FileRowView()
        cell.identifier = .init("cell")
        let dark = Settings.shared.theme?.dark ?? true
        if !changes.isEmpty {
            let c = changes[row]
            let rel = Paths.relative(git?.status.root ?? "", c.path)
            let isDir = c.path.hasSuffix("/")
            let trimmed = isDir ? String(rel.dropLast()) : rel
            let kind = ChangeKind(rawValue: c.status)
            var dir = ""
            if let slash = trimmed.lastIndex(of: "/") { dir = String(trimmed[..<slash]) }
            cell.set(depth: 0, expandable: false, expanded: false, icon: isDir ? "folder" : "doc", folder: isDir,
                     name: Paths.base(trimmed), detail: dir, italic: false, dim: false, strike: kind == .deleted,
                     letter: kind?.letter, dot: nil, color: kind.map { Self.color($0, dark: dark) })
            cell.toolTip = "\(rel) — \(GitIndex.summary(c))" + (c.from.isEmpty ? "" : " (was \(Paths.relative(git?.status.root ?? "", c.from)))")
            return cell
        }
        switch rows[row] {
        case .more(let depth):
            cell.setMore(depth: depth)
        case .entry(let e, let depth):
            let expanded = e.dir && tree.expanded.contains(e.path)
            var letter: String?, dot: ChangeKind?, color: NSColor?
            if let g = git {
                if e.dir { dot = g.folder(e.path) } else if let c = g.file(e.path), let k = ChangeKind(rawValue: c.status) {
                    letter = k.letter
                    color = Self.color(k, dark: dark)
                }
            }
            cell.set(depth: depth, expandable: e.dir, expanded: expanded, icon: e.dir ? (expanded ? "folder.fill" : "folder") : "doc",
                     folder: e.dir, name: e.name, detail: "", italic: e.symlink, dim: e.hidden || e.ignored, strike: false,
                     letter: letter, dot: dot.map { Self.color($0, dark: dark) }, color: color)
            // What git says about it: "Modified · staged", or changes in a folder.
            var tip = e.path
            if let g = git {
                if e.dir, dot != nil { tip += "\nChanges inside" } else if !e.dir, let c = g.file(e.path) {
                    tip += "\n" + GitIndex.summary(c) + (c.from.isEmpty ? "" : " (was \(Paths.base(c.from)))")
                }
            }
            cell.toolTip = tip
        }
        return cell
    }

    static func color(_ k: ChangeKind, dark: Bool) -> NSColor {
        switch k {
        case .modified, .renamed: NSColor(hex: dark ? "#fdd663" : "#c98a00")
        case .added, .untracked: NSColor(hex: dark ? "#81c995" : "#188038")
        case .deleted, .conflict: NSColor(hex: dark ? "#e5484d" : "#d93036")
        }
    }

    private func path(at row: Int) -> (path: String, dir: Bool, deleted: Bool)? {
        guard row >= 0 else { return nil }
        if !changes.isEmpty {
            guard row < changes.count else { return nil }
            let c = changes[row]
            let dir = c.path.hasSuffix("/")
            return (dir ? String(c.path.dropLast()) : c.path, dir, c.status == "deleted")
        }
        guard row < rows.count, case .entry(let e, _) = rows[row] else { return nil }
        return (e.path, e.dir, false)
    }

    @objc private func clicked() {
        guard let p = path(at: table.clickedRow), !p.deleted else { return }
        if p.dir, changes.isEmpty { toggle(p.path) } else { insertPath(p.path) }
    }

    @objc private func doubleClicked() {
        guard let p = path(at: table.clickedRow), p.dir, changes.isEmpty else { return }
        // The first click toggled it; put it back, and cd.
        toggle(p.path)
        cd(p.path)
    }

    private func toggle(_ path: String) {
        if tree.expanded.contains(path) {
            tree.expanded.remove(path)
        } else {
            tree.expanded.insert(path)
            load(path)
        }
        rebuild()
    }

    @objc private func toggleChanges() {
        changesOnly.toggle()
        rebuild()
    }

    func controlTextDidChange(_ note: Notification) { rebuild() }

    func control(_ control: NSControl, textView: NSTextView, doCommandBy selector: Selector) -> Bool {
        switch selector {
        case #selector(NSResponder.moveDown(_:)):
            window?.makeFirstResponder(table)
            keyDown(in: table, .down)
            return true
        case #selector(NSResponder.cancelOperation(_:)):
            filter.stringValue = ""
            rebuild()
            owner.focusActive()
            return true
        default:
            return false
        }
    }

    // MARK: Keyboard (tree focused)

    enum Key { case up, down, left, right, enter, escape, back, parent }

    fileprivate func keyDown(in table: NSTableView, _ key: Key) {
        let entries = rows.indices.filter { if case .entry = rows[$0] { true } else { false } }
        let i = table.selectedRow
        func select(_ r: Int) {
            table.selectRowIndexes([r], byExtendingSelection: false)
            table.scrollRowToVisible(r)
        }
        switch key {
        case .down: if let next = entries.first(where: { $0 > i }) ?? entries.last { select(next) }
        case .up: if let prev = entries.last(where: { $0 < i }) ?? entries.first { select(prev) }
        case .right:
            guard let p = path(at: i), p.dir else { return }
            if tree.expanded.contains(p.path) { if i + 1 < rows.count { select(i + 1) } } else { toggle(p.path); select(i) }
        case .left:
            guard let p = path(at: i) else { return }
            if p.dir, tree.expanded.contains(p.path) {
                toggle(p.path)
                select(i)
            } else if Paths.parent(p.path) != root, let r = rows.firstIndex(where: { if case .entry(let e, _) = $0 { e.path == Paths.parent(p.path) } else { false } }) {
                select(r)
            }
        case .enter:
            guard let p = path(at: i) else { return }
            if p.dir { toggle(p.path); select(i) } else { insertPath(p.path) }
        case .escape: owner.focusActive()
        case .back: goBack()
        case .parent: goUp()
        }
    }

    // MARK: Actions

    private var terminal: TerminalController? { owner.activeTerminal }

    private func pathForShell(_ path: String) -> String { Shell.quote(Paths.relative(pane?.cwd ?? "", path)) }

    /// Types the path at the prompt (as a paste); nothing runs.
    func insertPath(_ path: String) {
        guard let t = terminal else { return }
        t.view.pasteText(pathForShell(path) + " ")
        t.focus()
    }

    private var shellIdle: Bool { pane.map { $0.isShell && !$0.running } ?? false }

    private func cd(_ path: String) {
        guard let t = terminal else { return }
        guard shellIdle else {
            Toast.show("The shell is busy — finish the running program first", in: self)
            return
        }
        t.run("cd \(Shell.quote(path))")
    }

    private func go(_ path: String) {
        if pinned != nil {
            owner.sidebar.pinned = path
            owner.sidebarChanged()
            paneChanged()
        } else {
            cd(path)
        }
    }

    private func goUp() {
        let r = root
        if !r.isEmpty, r != "/", !remote { go(Paths.parent(r)) }
    }

    private func goBack() {
        guard !remote, var h = tree.history[historyKey], h.count > 1 else { return }
        guard pinned != nil || shellIdle else {
            Toast.show("The shell is busy — finish the running program first", in: self)
            return
        }
        h.removeLast()
        tree.history[historyKey] = h
        go(h.last!)
    }

    private func togglePin() {
        owner.sidebar.pinned = pinned != nil ? nil : (root.isEmpty ? nil : root)
        owner.sidebarChanged()
        paneChanged()
    }

    /// Opens a file in $EDITOR, in a split to the right.
    static func openInEditor(_ path: String, line: Int? = nil, in w: WindowController) {
        let cmd = "${EDITOR:-vi} " + (line.map { "+\($0) " } ?? "") + Shell.quote(path)
        w.model.split(.row, cwd: Paths.parent(path), command: cmd)
    }

    // MARK: Menus

    func menu(forRow row: Int) -> NSMenu? {
        guard let p = path(at: row) else { return nil }
        let m = NSMenu()
        m.autoenablesItems = false
        func add(_ title: String, _ hint: String = "", _ action: @escaping () -> Void) {
            let i = ClosureMenuItem(title: title, keyEquivalent: "", action: action)
            if !hint.isEmpty { i.toolTip = hint }
            m.addItem(i)
        }
        if !p.deleted {
            if p.dir {
                add("cd Here", "double-click") { [weak self] in self?.cd(p.path) }
                add("Open in New Tab") { [weak self] in self?.owner.model.open(cwd: p.path) }
                add("Open in New Split") { [weak self] in self?.owner.model.split(.row, cwd: p.path) }
                add("Insert Path") { [weak self] in self?.insertPath(p.path) }
                m.addItem(.separator())
                add("Make Root") { [weak self] in
                    guard let self else { return }
                    owner.sidebar.pinned = p.path
                    owner.sidebarChanged()
                    paneChanged()
                }
            } else {
                add("Insert Path", "click") { [weak self] in self?.insertPath(p.path) }
                add("Open in Editor") { [weak self] in
                    guard let self else { return }
                    Self.openInEditor(p.path, in: owner)
                }
                if (p.path as NSString).lastPathComponent.lowercased().contains("plan") {
                    add("Review Terraform Plan…", "terraform show -json reads it; nothing is applied") { [weak self] in
                        guard let self else { return }
                        ReviewSheet.show(file: p.path, owner: owner)
                    }
                }
                add("Open with Default App") { [weak self] in
                    Task { @MainActor in
                        do { try await Services.files.Open(p.path) } catch { if let self { Toast.show("Couldn't open it: \(error.localizedDescription)", in: self) } }
                    }
                }
                if Inspect.isCertFile(p.path) {
                    add("Inspect Certificate…") { [weak self] in
                        guard let self else { return }
                        CertViewer(.file(p.path), owner: owner).show()
                    }
                }
                m.addItem(.separator())
            }
            add("Reveal in Finder") { [weak self] in
                Task { @MainActor in
                    do { try await Services.files.Reveal(p.path) } catch { if let self { Toast.show("Couldn't reveal it: \(error.localizedDescription)", in: self) } }
                }
            }
            m.addItem(.separator())
            add("Copy Path") { [weak self] in
                Clipboard.copy(p.path)
                if let self { Toast.show("Copied path", in: self) }
            }
            add("Copy Relative Path") { [weak self] in
                guard let self else { return }
                Clipboard.copy(Paths.relative(pane?.cwd ?? "", p.path))
                Toast.show("Copied relative path", in: self)
            }
        }
        if let g = git {
            let items = g.menu(p.path, dir: p.dir)
            if !items.isEmpty {
                if m.numberOfItems > 0 { m.addItem(.separator()) }
                for item in items {
                    switch item {
                    case .heading(let h):
                        let i = NSMenuItem(title: h, action: nil, keyEquivalent: "")
                        i.isEnabled = false
                        m.addItem(i)
                    case .run(let label, let command):
                        add(label) { [weak self] in self?.owner.model.open(cwd: g.status.root, command: command) }
                    }
                }
            }
        }
        return m
    }

    // MARK: Drag

    func tableView(_ tableView: NSTableView, pasteboardWriterForRow row: Int) -> NSPasteboardWriting? {
        guard let p = path(at: row), !p.deleted else { return nil }
        let item = NSPasteboardItem()
        item.setString(p.path, forType: Self.pathType)
        item.setString(pathForShell(p.path), forType: .string)
        return item
    }
}

/// The tree's table: keys, the context menu, and the mouse's back button.
@MainActor
final class FileTable: NSTableView {
    weak var owner: FilesView?

    override func keyDown(with event: NSEvent) {
        let alt = event.modifierFlags.contains(.option)
        let key: FilesView.Key? = switch event.keyCode {
        case 125: .down
        case 126: alt ? .parent : .up
        case 123: .left
        case 124: .right
        case 36, 76: .enter
        case 53: .escape
        case 51: .back
        default: nil
        }
        if let key { owner?.keyDown(in: self, key) } else { super.keyDown(with: event) }
    }

    override func menu(for event: NSEvent) -> NSMenu? {
        let row = self.row(at: convert(event.locationInWindow, from: nil))
        if row >= 0 { selectRowIndexes([row], byExtendingSelection: false) }
        return owner?.menu(forRow: row)
    }

    override func otherMouseUp(with event: NSEvent) {
        // The mouse's back button.
        if event.buttonNumber == 3 { owner?.keyDown(in: self, .back) } else { super.otherMouseUp(with: event) }
    }
}

/// One row of the tree.
@MainActor
final class FileRowView: NSTableCellView {
    private let chevron = NSImageView()
    private let icon = NSImageView()
    private let name = NSTextField(labelWithString: "")
    private let detail = NSTextField(labelWithString: "")
    private let letter = NSTextField(labelWithString: "")
    private let dot = NSView()
    private var depth = 0

    init() {
        super.init(frame: .zero)
        chevron.image = NSImage(systemSymbolName: "chevron.right", accessibilityDescription: nil)
        chevron.symbolConfiguration = .init(pointSize: 9, weight: .semibold)
        chevron.contentTintColor = .tertiaryLabelColor
        icon.symbolConfiguration = .init(pointSize: 12, weight: .regular)
        name.font = .systemFont(ofSize: 12.5)
        name.lineBreakMode = .byTruncatingTail
        detail.font = .systemFont(ofSize: 11)
        detail.textColor = .secondaryLabelColor
        detail.lineBreakMode = .byTruncatingHead
        letter.font = .monospacedSystemFont(ofSize: 10.5, weight: .semibold)
        letter.alignment = .right
        dot.wantsLayer = true
        dot.layer?.cornerRadius = 3
        [chevron, icon, name, detail, letter, dot].forEach(addSubview)
    }

    required init?(coder: NSCoder) { fatalError() }

    func set(depth: Int, expandable: Bool, expanded: Bool, icon symbol: String, folder: Bool, name text: String, detail d: String,
             italic: Bool, dim: Bool, strike: Bool, letter l: String?, dot dotColor: NSColor?, color: NSColor?) {
        self.depth = depth
        chevron.isHidden = !expandable
        chevron.frameCenterRotation = expanded ? -90 : 0
        icon.image = NSImage(systemSymbolName: symbol, accessibilityDescription: nil)
        icon.contentTintColor = folder ? .controlAccentColor : .secondaryLabelColor
        // An attributed value replaces the field's own line breaking: the ellipsis goes in its style.
        let tail = NSMutableParagraphStyle()
        tail.lineBreakMode = .byTruncatingTail
        var attrs: [NSAttributedString.Key: Any] = [.paragraphStyle: tail, .font: italic ? NSFontManager.shared.convert(.systemFont(ofSize: 12.5), toHaveTrait: .italicFontMask) : NSFont.systemFont(ofSize: 12.5),
                                                    .foregroundColor: color ?? NSColor.labelColor]
        if strike { attrs[.strikethroughStyle] = NSUnderlineStyle.single.rawValue }
        name.attributedStringValue = NSAttributedString(string: text, attributes: attrs)
        detail.stringValue = d
        detail.isHidden = d.isEmpty
        letter.stringValue = l ?? ""
        letter.textColor = color?.withAlphaComponent(0.85)
        dot.isHidden = dotColor == nil
        dot.layer?.backgroundColor = dotColor?.withAlphaComponent(0.8).cgColor
        alphaValue = dim ? 0.5 : 1
        icon.isHidden = false
        needsLayout = true
    }

    func setMore(depth: Int) {
        self.depth = depth
        chevron.isHidden = true
        icon.isHidden = true
        let f = NSFontManager.shared.convert(.systemFont(ofSize: 11.5), toHaveTrait: .italicFontMask)
        name.attributedStringValue = NSAttributedString(string: "Some entries not shown", attributes: [.font: f, .foregroundColor: NSColor.secondaryLabelColor])
        detail.isHidden = true
        letter.stringValue = ""
        dot.isHidden = true
        alphaValue = 1
        needsLayout = true
    }

    override func layout() {
        super.layout()
        let x = CGFloat(depth) * 14 + 6
        let h = bounds.height
        chevron.frame = NSRect(x: x, y: (h - 14) / 2, width: 14, height: 14)
        icon.frame = NSRect(x: x + 16, y: (h - 16) / 2, width: 16, height: 16)
        let right: CGFloat = letter.stringValue.isEmpty && dot.isHidden ? 8 : 26
        let nameX = icon.isHidden ? x + 26 : x + 36
        // Measured from the text itself: a reused cell's natural size can be stale.
        let nameW = min(ceil(name.attributedStringValue.size().width) + 4, bounds.width - nameX - right)
        name.frame = NSRect(x: nameX, y: (h - 17) / 2, width: max(0, nameW), height: 17)
        detail.frame = NSRect(x: name.frame.maxX + 6, y: (h - 15) / 2, width: max(0, bounds.width - name.frame.maxX - 6 - right), height: 15)
        letter.frame = NSRect(x: bounds.width - 24, y: (h - 15) / 2, width: 16, height: 15)
        dot.frame = NSRect(x: bounds.width - 14, y: (h - 6) / 2, width: 6, height: 6)
    }
}

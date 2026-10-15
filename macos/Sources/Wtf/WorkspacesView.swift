import AppKit
import WtfCore

/// The snippets files' contents for the focused shell's folder: snippets and
/// workspaces, read afresh when asked (they're hand-edited).
@MainActor
final class SnippetStore {
    static let shared = SnippetStore()
    var list = Go.snippets.List()
    var loaded = false
    var cwd = ""
    private var observers = Observers()

    func observe<T: AnyObject>(_ owner: T, _ fn: @escaping (T) -> Void) { observers.add(owner, fn) }

    func load(cwd: String) {
        self.cwd = cwd
        Task {
            do { list = try await Services.snippets.List(cwd) } catch { list.problems = [error.localizedDescription] }
            loaded = true
            observers.notify()
        }
    }

    func set(_ l: Go.snippets.List, cwd: String? = nil) {
        list = l
        if let cwd { self.cwd = cwd }
        loaded = true
        observers.notify()
    }
}

/// The Workspaces tab: workspaces from the snippets files, grouped by file.
/// Open (▶ or double-click), edit, duplicate, delete; save this window as
/// one. Like the Workspaces part of SnippetsView.svelte.
@MainActor
final class WorkspacesView: NSView, SidePaneTab, NSSearchFieldDelegate {
    private unowned let owner: WindowController
    private let header = PaneHeader("Workspaces")
    private let filter = NSSearchField()
    private let list = SideList()
    private var note: NoteView?
    private var store: SnippetStore { .shared }
    private var lastCwd: String?

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        header.tool("square.and.arrow.down", "Save this window's tabs as a workspace") { [weak self] in
            guard let self else { return }
            WorkspaceEditor.show(.capture(owner.model.captureWorkspace()), in: owner)
        }
        header.tool("plus", "New workspace") { [weak self] in
            guard let self else { return }
            WorkspaceEditor.show(.new, in: owner)
        }
        header.tool("doc.text", "Open your snippets file in a text editor") { [weak self] in self?.owner.openSnippetsFile() }
        header.tool("arrow.clockwise", "Reload") { [weak self] in self?.reload() }
        addSubview(header)
        filter.placeholderString = "Filter workspaces"
        filter.delegate = self
        filter.sendsSearchStringImmediately = true
        filter.refusesFirstResponder = true
        addSubview(filter)
        list.onDoubleClick = { [weak self] id in self?.workspace(id).map { self?.open($0) } }
        list.menuProvider = { [weak self] ids in ids.first.flatMap { self?.workspace($0) }.flatMap { self?.menu($0) } }
        addSubview(list)
        store.observe(self) { $0.rebuild() }
        reload()
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    override func layout() {
        super.layout()
        header.frame = NSRect(x: 0, y: 0, width: bounds.width, height: 30)
        filter.frame = NSRect(x: 8, y: 32, width: bounds.width - 16, height: 24)
        list.frame = NSRect(x: 0, y: 62, width: bounds.width, height: max(0, bounds.height - 62))
        note?.frame = NSRect(x: 0, y: 62, width: bounds.width, height: note?.height(for: bounds.width) ?? 0)
    }

    private var cwd: String { owner.activePane?.cwd ?? "" }

    /// Follows the focused shell's folder: project files depend on it.
    func paneChanged() { if cwd != lastCwd { reload() } }

    private func reload() {
        lastCwd = cwd
        store.load(cwd: cwd)
    }

    func controlTextDidChange(_ obj: Notification) { rebuild() }

    private func workspace(_ id: String) -> Go.snippets.Workspace? { store.list.workspaces.first { $0.id == id } }

    private func rebuild() {
        let q = filter.stringValue.trimmingCharacters(in: .whitespaces).lowercased()
        let shown = store.list.workspaces.filter { w in
            q.isEmpty || ([w.name, w.description] + w.tabs.flatMap { [$0.title, $0.group, $0.command] + $0.panes.map(\.command) })
                .joined(separator: " ").lowercased().contains(q)
        }
        var files: [String] = []
        for w in shown where !files.contains(w.source) { files.append(w.source) }
        let sections = files.map { f -> SideList.Section in
            let kind = store.list.targets.first { $0.path == f }?.kind ?? ""
            let title = kind == "project" ? "This project · \(WindowModel.prettyPath(f))" : (f == store.list.userFile ? "Yours" : WindowModel.prettyPath(f))
            return .init(title: title, tip: f, rows: shown.filter { $0.source == f }.map(row))
        }
        list.set(sections + (store.list.problems.isEmpty ? [] : [.init(title: "Problems", rows: store.list.problems.map {
            .init(id: "p:\($0)", symbol: "exclamationmark.triangle", tint: .systemRed, title: $0)
        })]))
        note?.removeFromSuperview()
        note = nil
        if store.loaded, shown.isEmpty {
            note = q.isEmpty
                ? paneNote("No workspaces yet", "A workspace opens a set of tabs in one go — folders, split panes, tab groups and startup commands. Arrange your tabs, then save this window as a workspace (the first button above) — or press + to build one.")
                : paneNote(nil, "No workspaces match “\(filter.stringValue)”.")
            addSubview(note!)
        }
        needsLayout = true
    }

    private func row(_ w: Go.snippets.Workspace) -> SideList.Row {
        let groups = Array(Set(w.tabs.map(\.group).filter { !$0.isEmpty })).sorted()
        let sub = w.description.isEmpty
            ? ([w.tabs.count == 1 ? "1 tab" : "\(w.tabs.count) tabs"] + groups.map { "group \($0)" }).joined(separator: " · ")
            : w.description
        let tip = w.tabs.enumerated().map { i, t in
            let what = !t.title.isEmpty ? t.title : !t.command.isEmpty ? t.command
                : t.panes.map(\.command).filter { !$0.isEmpty }.joined(separator: " | ")
            return "\(i + 1). \(what.isEmpty ? "shell" : what)"
        }.joined(separator: "\n")
        return .init(id: w.id, symbol: "square.stack.3d.up", title: w.name, detail: sub,
                     pills: w.window == "new" ? [.init(text: "new window")] : [], tip: tip,
                     button: ("play.fill", "Open", { [weak self] in self?.open(w) }))
    }

    private func open(_ w: Go.snippets.Workspace) { WorkspaceOpen.show(Workspace(w), in: owner) }

    private func menu(_ w: Go.snippets.Workspace) -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        func add(_ t: String, _ run: @escaping () -> Void) { m.addItem(ClosureMenuItem(title: t, keyEquivalent: "", action: run)) }
        let o = owner
        add("Open…") { [weak self] in self?.open(w) }
        m.addItem(.separator())
        add("Edit…") { WorkspaceEditor.show(.edit(w), in: o) }
        add("Duplicate…") { WorkspaceEditor.show(.duplicate(w), in: o) }
        m.addItem(.separator())
        add("Delete…") { [weak self] in
            guard let self else { return }
            Confirm.show(in: window, title: "Delete workspace “\(w.name)”?",
                         message: "Removes it from \(WindowModel.prettyPath(w.source)). The file’s snippets, other workspaces and comments stay. Open tabs aren’t affected.",
                         button: "Delete Workspace") { [weak self] in
                guard let self else { return }
                Task {
                    do {
                        store.set(try await Services.snippets.Delete("workspace", w.source, w.index, store.list.versions[w.source] ?? "", cwd))
                        Toast.show("Deleted “\(w.name)”", in: self)
                    } catch {
                        Toast.show(error.localizedDescription, in: self)
                    }
                }
            }
        }
        return m
    }
}

/// The workspace editor: a form of tabs (folders, split panes, groups,
/// startup commands) and placeholders, then a review of exactly what will
/// be written to the snippets file. Like WorkspaceEditor.svelte.
@MainActor
final class WorkspaceEditor: NSObject {
    enum Source {
        case new
        case capture(Go.snippets.WorkspaceDef)
        case edit(Go.snippets.Workspace)
        case duplicate(Go.snippets.Workspace)
    }

    struct PaneDraft { var cwd = "", command = "" }
    struct TabDraft {
        var title = "", group = "", color = "", layout = "row"
        var panes = [PaneDraft()]
    }
    struct ParamDraft { var description = "", def = "", choices = "", raw = false }

    private static var open: [WorkspaceEditor] = []
    private unowned let owner: WindowController
    private let editing: Go.snippets.Workspace?
    private let panel: NSPanel
    private let content = NSStackView()
    private var name = "", summary = "", newWindow = false
    private var tabs: [TabDraft] = []
    private var params: [String: ParamDraft] = [:]
    private var file = ""
    private var error: String?
    private var review: (edit: Go.snippets.Edit, preview: Go.snippets.Preview)?
    private var busy = false

    static func show(_ source: Source, in w: WindowController) {
        let e = WorkspaceEditor(source, w)
        open.append(e)
        w.window?.beginSheet(e.panel)
    }

    private init(_ source: Source, _ w: WindowController) {
        owner = w
        panel = NSPanel(contentRect: NSRect(x: 0, y: 0, width: 680, height: 560), styleMask: [.titled, .docModalWindow, .resizable],
                        backing: .buffered, defer: false)
        var def: Go.snippets.WorkspaceDef?
        switch source {
        case .new: editing = nil
        case .capture(let d): editing = nil; def = d
        case .edit(let ws): editing = ws; def = .init(name: ws.name, description: ws.description, window: ws.window, tabs: ws.tabs, params: ws.params)
        case .duplicate(let ws):
            editing = nil
            def = .init(name: "\(ws.name) (copy)", description: ws.description, window: ws.window, tabs: ws.tabs, params: ws.params)
        }
        super.init()
        if let def {
            name = def.name
            summary = def.description
            newWindow = def.window == "new"
            tabs = def.tabs.map { t in
                TabDraft(title: t.title, group: t.group, color: t.color, layout: t.layout == "column" ? "column" : "row",
                         panes: t.panes.isEmpty ? [PaneDraft(cwd: t.cwd, command: t.command)] : t.panes.map { PaneDraft(cwd: $0.cwd, command: $0.command) })
            }
            for p in def.params {
                params[p.name] = ParamDraft(description: p.description, def: p.default, choices: p.choices.joined(separator: ", "), raw: p.raw)
            }
        }
        if tabs.isEmpty { tabs = [TabDraft()] }
        let store = SnippetStore.shared
        file = editing?.source ?? store.list.targets.first { $0.kind == "user" }?.path ?? store.list.targets.first?.path ?? ""
        content.orientation = .vertical
        content.alignment = .leading
        content.spacing = 10
        content.edgeInsets = NSEdgeInsets(top: 18, left: 22, bottom: 16, right: 22)
        let scroll = NSScrollView()
        scroll.documentView = content
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        panel.contentView = scroll
        content.translatesAutoresizingMaskIntoConstraints = false
        content.widthAnchor.constraint(equalToConstant: 662).isActive = true
        render()
        if !store.loaded || editing == nil {
            store.observe(self) { $0.listLoaded() }
            store.load(cwd: w.activePane?.cwd ?? "")
        }
    }

    private func listLoaded() {
        if file.isEmpty || editing == nil {
            let store = SnippetStore.shared
            if file.isEmpty || !store.list.targets.contains(where: { $0.path == file }) {
                file = store.list.targets.first { $0.kind == "user" }?.path ?? store.list.targets.first?.path ?? ""
            }
        }
        if review == nil { render() }
    }

    // MARK: Building the form

    private func label(_ s: String, bold: Bool = false, size: CGFloat = 12) -> NSTextField {
        let l = NSTextField(labelWithString: s)
        l.font = bold ? .systemFont(ofSize: size, weight: .semibold) : .systemFont(ofSize: size)
        return l
    }

    private func field(_ value: String, _ placeholder: String, width: CGFloat, _ set: @escaping (String) -> Void) -> NSTextField {
        let f = BoundField(value, set)
        f.placeholderString = placeholder
        f.widthAnchor.constraint(equalToConstant: width).isActive = true
        return f
    }

    private func button(_ title: String, _ run: @escaping () -> Void) -> NSButton {
        let b = ClosureButton(title: title, run)
        b.bezelStyle = .accessoryBarAction
        return b
    }

    private func render() {
        content.arrangedSubviews.forEach { $0.removeFromSuperview() }
        if let review {
            renderReview(review)
            return
        }
        let title = label(editing.map { "Edit “\($0.name)”" } ?? "New Workspace", bold: true, size: 15)
        content.addArrangedSubview(title)
        let sub = label("Tabs — with folders, split panes, groups and startup commands — opened in one go")
        sub.textColor = .secondaryLabelColor
        content.addArrangedSubview(sub)
        content.addArrangedSubview(NSStackView(views: [
            label("Name"), field(name, "payments on call", width: 220) { [weak self] in self?.name = $0 },
            label("Description"), field(summary, "optional", width: 250) { [weak self] in self?.summary = $0 },
        ]))

        for (i, t) in tabs.enumerated() { content.addArrangedSubview(card(i, t)) }
        content.addArrangedSubview(button("+ Add Tab") { [weak self] in
            guard let self else { return }
            tabs.append(TabDraft(group: tabs.last?.group ?? ""))
            render()
        })
        let help = NSTextField(wrappingLabelWithString: "Names, groups, folders and commands can use placeholders, asked for when it opens: {{service}}, or {{ns=$kube_namespace}}.")
        help.font = .systemFont(ofSize: 11)
        help.textColor = .secondaryLabelColor
        help.preferredMaxLayoutWidth = 618
        content.addArrangedSubview(help)

        // Placeholders, as used now in titles, groups, folders and commands:
        // redrawn as they're typed, so the field being typed in keeps the keyboard.
        paramsBox.orientation = .vertical
        paramsBox.alignment = .leading
        paramsBox.spacing = 6
        content.addArrangedSubview(paramsBox)
        renderParams()

        let nw = NSButton(checkboxWithTitle: "Open in a new window (otherwise as new tabs in the current one)", target: nil, action: nil)
        nw.state = newWindow ? .on : .off
        let nwWatch = PopupWatcher { [weak self, weak nw] in self?.newWindow = nw?.state == .on }
        nw.target = nwWatch
        nw.action = #selector(PopupWatcher.changed)
        objc_setAssociatedObject(nw, "watch", nwWatch, .OBJC_ASSOCIATION_RETAIN)
        content.addArrangedSubview(nw)

        if editing == nil {
            let targets = SnippetStore.shared.list.targets
            let labels = targets.map { t -> String in
                let kind = switch t.kind {
                case "project": "this project’s, for git"
                case "team": "team file"
                default: "yours"
                }
                return "\(WindowModel.prettyPath(t.path)) — \(kind)\(t.exists ? "" : " (new file)")"
            }
            let pop = NSPopUpButton()
            pop.addItems(withTitles: labels)
            if let i = targets.firstIndex(where: { $0.path == file }) { pop.selectItem(at: i) }
            let pw = PopupWatcher { [weak self, weak pop] in
                guard let self, let pop, pop.indexOfSelectedItem >= 0 else { return }
                file = targets[pop.indexOfSelectedItem].path
            }
            pop.target = pw
            pop.action = #selector(PopupWatcher.changed)
            objc_setAssociatedObject(pop, "watch", pw, .OBJC_ASSOCIATION_RETAIN)
            content.addArrangedSubview(NSStackView(views: [label("Save to"), pop]))
        }
        if let error {
            let e = NSTextField(wrappingLabelWithString: error)
            e.textColor = .systemRed
            e.preferredMaxLayoutWidth = 600
            content.addArrangedSubview(e)
        }
        let cancel = ClosureButton(title: "Cancel") { [weak self] in self?.close() }
        cancel.keyEquivalent = "\u{1b}"
        let next = ClosureButton(title: "Review…") { [weak self] in self?.startReview() }
        next.keyEquivalent = "\r"
        next.isEnabled = !busy
        let row = NSStackView(views: [NSView(), cancel, next])
        row.widthAnchor.constraint(equalToConstant: 618).isActive = true
        content.addArrangedSubview(row)
        resize()
    }

    private func card(_ i: Int, _ t: TabDraft) -> NSView {
        let colors = ["color"] + GroupColor.allCases.map(\.rawValue)
        let colorPop = NSPopUpButton()
        colorPop.addItems(withTitles: colors)
        colorPop.selectItem(withTitle: t.color.isEmpty ? "color" : t.color)
        colorPop.isEnabled = !t.group.trimmingCharacters(in: .whitespaces).isEmpty
        colorPop.setAccessibilityLabel("Group color of tab \(i + 1)")
        let cw = PopupWatcher { [weak self, weak colorPop] in
            guard let self, let colorPop else { return }
            tabs[i].color = colorPop.indexOfSelectedItem <= 0 ? "" : colorPop.titleOfSelectedItem ?? ""
        }
        colorPop.target = cw
        colorPop.action = #selector(PopupWatcher.changed)
        objc_setAssociatedObject(colorPop, "watch", cw, .OBJC_ASSOCIATION_RETAIN)
        let up = button("↑") { [weak self] in self?.move(i, -1) }
        up.isEnabled = i > 0
        up.setAccessibilityLabel("Move tab \(i + 1) up")
        let down = button("↓") { [weak self] in self?.move(i, 1) }
        down.isEnabled = i < tabs.count - 1
        down.setAccessibilityLabel("Move tab \(i + 1) down")
        let remove = button("×") { [weak self] in
            self?.tabs.remove(at: i)
            self?.render()
        }
        remove.isEnabled = tabs.count > 1
        remove.setAccessibilityLabel("Remove tab \(i + 1)")
        let head = NSStackView(views: [
            label("\(i + 1)", bold: true),
            field(t.title, "Tab name (optional)", width: 180) { [weak self] in self?.tabs[i].title = $0; self?.renderParams() },
            field(t.group, "Group (optional)", width: 150) { [weak self] v in
                self?.tabs[i].group = v
                self?.renderParams()
                colorPop.isEnabled = !v.trimmingCharacters(in: .whitespaces).isEmpty
            },
            colorPop, up, down, remove,
        ])
        var rows: [NSView] = [head]
        for (j, p) in t.panes.enumerated() {
            var views: [NSView] = [
                field(p.cwd, "folder (e.g. ~/src/app)", width: 230) { [weak self] in self?.tabs[i].panes[j].cwd = $0; self?.renderParams() },
                field(p.command, "startup command (optional)", width: 300) { [weak self] in self?.tabs[i].panes[j].command = $0; self?.renderParams() },
            ]
            if t.panes.count > 1 {
                let x = button("×") { [weak self] in
                    self?.tabs[i].panes.remove(at: j)
                    self?.render()
                }
                x.setAccessibilityLabel("Remove pane \(j + 1) of tab \(i + 1)")
                views.append(x)
            }
            rows.append(NSStackView(views: views))
        }
        var foot: [NSView] = [button("+ Split") { [weak self] in
            guard let self else { return }
            tabs[i].panes.append(PaneDraft(cwd: tabs[i].panes.last?.cwd ?? ""))
            render()
        }]
        if t.panes.count > 1 {
            let lp = NSPopUpButton()
            lp.addItems(withTitles: ["side by side", "stacked"])
            lp.selectItem(at: t.layout == "column" ? 1 : 0)
            let lw = PopupWatcher { [weak self, weak lp] in self?.tabs[i].layout = lp?.indexOfSelectedItem == 1 ? "column" : "row" }
            lp.target = lw
            lp.action = #selector(PopupWatcher.changed)
            objc_setAssociatedObject(lp, "watch", lw, .OBJC_ASSOCIATION_RETAIN)
            foot.append(lp)
        }
        rows.append(NSStackView(views: foot))
        let stack = NSStackView(views: rows)
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 6
        stack.edgeInsets = NSEdgeInsets(top: 8, left: 10, bottom: 8, right: 10)
        // A plain bordered card: unlike NSBox, it takes its contents' height.
        stack.wantsLayer = true
        stack.layer?.cornerRadius = 8
        stack.layer?.borderWidth = 1
        stack.layer?.borderColor = NSColor.separatorColor.cgColor
        stack.layer?.backgroundColor = NSColor.labelColor.withAlphaComponent(0.03).cgColor
        stack.translatesAutoresizingMaskIntoConstraints = false
        stack.widthAnchor.constraint(equalToConstant: 618).isActive = true
        return stack
    }

    private func move(_ i: Int, _ d: Int) {
        let j = i + d
        guard j >= 0, j < tabs.count else { return }
        tabs.swapAt(i, j)
        render()
    }

    private let paramsBox = NSStackView()
    private var shownParams: [String] = []

    private func renderParams() {
        let names = placeholderNames()
        guard names != shownParams || paramsBox.arrangedSubviews.isEmpty != names.isEmpty else { return }
        shownParams = names
        paramsBox.arrangedSubviews.forEach { $0.removeFromSuperview() }
        guard !names.isEmpty else { return }
        paramsBox.addArrangedSubview(label("Placeholders", bold: true))
        for n in names {
            let p = params[n] ?? ParamDraft()
            paramsBox.addArrangedSubview(NSStackView(views: [
                label(n),
                field(p.description, "what to enter", width: 170) { [weak self] in self?.params[n, default: ParamDraft()].description = $0 },
                field(p.def, "default", width: 150) { [weak self] in self?.params[n, default: ParamDraft()].def = $0 },
                field(p.choices, "choices, comma-separated", width: 190) { [weak self] in self?.params[n, default: ParamDraft()].choices = $0 },
            ]))
        }
        resize()
    }

    private func placeholderNames() -> [String] {
        var texts: [String] = []
        for t in tabs {
            texts += [t.title, t.group]
            for p in t.panes { texts += [p.cwd, p.command] }
        }
        return Placeholders.fields(in: texts, params: []).map(\.name)
    }

    private func resize() {
        content.layoutSubtreeIfNeeded()
        let h = min(max(300, content.fittingSize.height), (owner.window?.frame.height ?? 700) * 0.85)
        panel.setContentSize(NSSize(width: 680, height: h))
    }

    // MARK: Review and save

    private func startReview() {
        guard !name.trimmingCharacters(in: .whitespaces).isEmpty else {
            error = "Give the workspace a name"
            render()
            return
        }
        guard !file.isEmpty else {
            error = "There's no snippets file to save to from here"
            render()
            return
        }
        let names = placeholderNames()
        let def = Go.snippets.WorkspaceDef(
            name: name.trimmingCharacters(in: .whitespaces), description: summary.trimmingCharacters(in: .whitespaces),
            window: newWindow ? "new" : "",
            tabs: tabs.map { .init(title: $0.title, group: $0.group, color: $0.color, layout: $0.layout,
                                   panes: $0.panes.map { .init(cwd: $0.cwd, command: $0.command) }) },
            params: names.map { n in
                let p = params[n] ?? ParamDraft()
                return .init(name: n, description: p.description, default: p.def,
                      choices: p.choices.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }, raw: p.raw)
            })
        let edit = Go.snippets.Edit(kind: "workspace", file: file, index: editing?.index ?? -1,
                                    version: SnippetStore.shared.list.versions[file] ?? "", workspace: def)
        busy = true
        Task {
            do {
                let preview = try await Services.snippets.PlanEdit(edit, owner.activePane?.cwd ?? "")
                busy = false
                error = nil
                review = (edit, preview)
            } catch {
                busy = false
                self.error = error.localizedDescription
            }
            render()
        }
    }

    private func renderReview(_ r: (edit: Go.snippets.Edit, preview: Go.snippets.Preview)) {
        content.addArrangedSubview(label("Review", bold: true, size: 15))
        let verb = r.preview.newFile ? "Creates" : (editing != nil ? "Replaces this workspace in" : "Adds to the end of")
        let sub = label("\(verb) \(WindowModel.prettyPath(r.preview.file)). Nothing is written until you confirm.")
        sub.textColor = .secondaryLabelColor
        content.addArrangedSubview(sub)
        let text = NSTextField(wrappingLabelWithString: r.preview.text)
        text.font = .monospacedSystemFont(ofSize: 11.5, weight: .regular)
        text.isSelectable = true
        text.preferredMaxLayoutWidth = 610
        content.addArrangedSubview(text)
        if editing != nil {
            let n = label("The rest of the file — snippets, other workspaces, your comments — stays as it is.", size: 11)
            n.textColor = .secondaryLabelColor
            content.addArrangedSubview(n)
        }
        let back = ClosureButton(title: "Back") { [weak self] in
            self?.review = nil
            self?.render()
        }
        let save = ClosureButton(title: "Confirm & Save") { [weak self] in self?.save(r.edit) }
        save.keyEquivalent = "\r"
        let row = NSStackView(views: [NSView(), back, save])
        row.widthAnchor.constraint(equalToConstant: 618).isActive = true
        content.addArrangedSubview(row)
        resize()
    }

    private func save(_ edit: Go.snippets.Edit) {
        Task {
            do {
                SnippetStore.shared.set(try await Services.snippets.ApplyEdit(edit, owner.activePane?.cwd ?? ""))
                close()
                let n = edit.workspace.name
                Toast.show(editing != nil ? "Saved “\(n)”" : "Added “\(n)” — open it from the Workspaces tab or Open Workspace…", in: owner.rootView)
            } catch {
                self.error = error.localizedDescription
                review = nil
                render()
            }
        }
    }

    private func close() {
        owner.window?.endSheet(panel)
        Self.open.removeAll { $0 === self }
        owner.focusActive()
    }
}

/// A text field that reports every edit.
@MainActor
final class BoundField: NSTextField, NSTextFieldDelegate {
    private let set: (String) -> Void

    init(_ value: String, _ set: @escaping (String) -> Void) {
        self.set = set
        super.init(frame: .zero)
        stringValue = value
        delegate = self
    }

    required init?(coder: NSCoder) { fatalError() }

    func controlTextDidChange(_ obj: Notification) { set(stringValue) }
}

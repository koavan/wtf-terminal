import AppKit
import WtfCore

extension WindowController {
    enum SnippetHow { case insert, run, tab }

    /// Puts a snippet's command at the focused (or given) shell's prompt,
    /// runs it there, or runs it in a new tab. False when the shell is busy.
    @discardableResult
    func runSnippet(_ command: String, _ how: SnippetHow, pane: Int? = nil) -> Bool {
        if how == .tab {
            model.open(cwd: activePane?.cwd, command: command)
            return true
        }
        guard let id = pane ?? activePane?.id, let p = model.panes[id], let term = terminals[id], p.isShell, !p.running else {
            Toast.show("The shell is busy — finish the running program first, or use Run in New Tab", in: rootView)
            return false
        }
        term.type(Shell.clearLine + command + (how == .run ? "\r" : ""))
        term.focus()
        return true
    }

    /// A snippet with nothing to fill in goes straight to the prompt; the
    /// rest open the run dialog.
    func openSnippet(_ s: Snippet) {
        if !s.isRunbook, s.fields.isEmpty {
            runSnippet(s.command, .insert)
        } else {
            SnippetRun.show(s, in: self)
        }
    }

    func openSnippetsFile() {
        Task {
            do { try await Services.snippets.OpenUserFile() } catch {
                Toast.show("Couldn't open the snippets file: \(error.localizedDescription)", in: rootView)
            }
        }
    }
}

/// The Snippets tab: snippets and runbooks from the snippets files, grouped
/// by file. Run (▶ or double-click), edit, duplicate, delete. Like the
/// Snippets part of SnippetsView.svelte.
@MainActor
final class SnippetsView: NSView, SidePaneTab, NSSearchFieldDelegate {
    private unowned let owner: WindowController
    private let header = PaneHeader("Snippets")
    private let filter = NSSearchField()
    private let list = SideList()
    private var note: NoteView?
    private var examples: NSButton?
    private var store: SnippetStore { .shared }
    private var lastCwd: String?

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        header.tool("plus", "New snippet") { [weak self] in
            guard let self else { return }
            SnippetEditor.show(.new(command: ""), in: owner)
        }
        header.tool("doc.text", "Open your snippets file in a text editor") { [weak self] in
            Task {
                do { try await Services.snippets.OpenUserFile() } catch {
                    if let self { Toast.show("Couldn't open it: \(error.localizedDescription)", in: self) }
                }
            }
        }
        header.tool("arrow.clockwise", "Reload") { [weak self] in self?.reload() }
        addSubview(header)
        filter.placeholderString = "Filter snippets"
        filter.delegate = self
        filter.sendsSearchStringImmediately = true
        filter.refusesFirstResponder = true
        addSubview(filter)
        list.onDoubleClick = { [weak self] id in self?.snippet(id).map { self?.owner.openSnippet($0) } }
        list.menuProvider = { [weak self] ids in ids.first.flatMap { self?.snippet($0) }.flatMap { self?.menu($0) } }
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
        let h = note?.height(for: bounds.width) ?? 0
        note?.frame = NSRect(x: 0, y: 62, width: bounds.width, height: h)
        if let b = examples { b.frame = NSRect(x: NoteView.inset - 4, y: 62 + h - 4, width: b.fittingSize.width, height: 22) }
    }

    private var cwd: String { owner.activePane?.cwd ?? "" }

    func paneChanged() { if cwd != lastCwd { reload() } }

    func debugSummary() -> String { "snippets: \(store.list.snippets.count), problems \(store.list.problems)" }

    private func reload() {
        lastCwd = cwd
        store.load(cwd: cwd)
    }

    func controlTextDidChange(_ obj: Notification) { rebuild() }

    private func snippet(_ id: String) -> Snippet? { store.list.snippets.first { $0.id == id } }

    private func rebuild() {
        let q = filter.stringValue
        let shown = store.list.snippets.filter { $0.matches(q) }
        let sections = store.list.files.compactMap { f -> SideList.Section? in
            let rows = shown.filter { $0.source == f }
            guard !rows.isEmpty else { return nil }
            let kind = store.list.targets.first { $0.path == f }?.kind ?? ""
            let title = kind == "project" ? "This project · \(WindowModel.prettyPath(f))" : kind == "user" ? "Yours" : WindowModel.prettyPath(f)
            return .init(title: title, tip: f, rows: rows.map(row))
        }
        var problems: [String] = []
        for p in store.list.problems where !problems.contains(p) { problems.append(p) }
        list.set(sections + (problems.isEmpty ? [] : [.init(title: "Problems", rows: problems.map {
            .init(id: "p:\($0)", symbol: "exclamationmark.triangle", tint: .systemRed, title: $0, tip: $0)
        })]))
        note?.removeFromSuperview()
        note = nil
        examples?.removeFromSuperview()
        examples = nil
        if store.loaded, sections.isEmpty {
            if q.trimmingCharacters(in: .whitespaces).isEmpty {
                note = paneNote("No snippets yet", "Save commands you run often, with blanks to fill in — or runbooks of steps. Press + to make one, or right-click a command in the terminal → Save as Snippet….")
                let b = ClosureButton(title: "Start from examples") { [weak self] in
                    Task {
                        do { try await Services.snippets.OpenUserFile() } catch {
                            if let self { Toast.show("Couldn't open it: \(error.localizedDescription)", in: self) }
                        }
                        self?.reload()
                    }
                }
                b.isBordered = false
                b.contentTintColor = .controlAccentColor
                b.font = .systemFont(ofSize: 12, weight: .medium)
                b.refusesFirstResponder = true
                examples = b
                addSubview(b)
            } else {
                note = paneNote(nil, "No snippets match “\(q)”.")
            }
            addSubview(note!)
            if let examples { addSubview(examples) }
        }
        needsLayout = true
    }

    private func row(_ s: Snippet) -> SideList.Row {
        let asks = s.isRunbook || !s.fields.isEmpty
        return .init(id: s.id, symbol: s.isRunbook ? "square.3.layers.3d" : "terminal", title: s.name,
                     mono: false, detail: s.subtitle, pills: s.isRunbook ? [.init(text: "\(s.steps.count) steps")] : [],
                     tip: s.tooltip,
                     button: ("play.fill", asks ? "Fill in and run" : "Put it at the prompt", { [weak self] in self?.owner.openSnippet(s) }),
                     monoDetail: !s.isRunbook && s.description.isEmpty)
    }

    private func menu(_ s: Snippet) -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        func add(_ t: String, _ run: @escaping () -> Void) { m.addItem(ClosureMenuItem(title: t, keyEquivalent: "", action: run)) }
        let o = owner
        add(s.isRunbook || !s.fields.isEmpty ? "Run…" : "Insert at Prompt") { o.openSnippet(s) }
        if !s.isRunbook {
            add("Copy Command") {
                Clipboard.copy(s.command)
                Toast.show("Copied command", in: o.rootView)
            }
        }
        m.addItem(.separator())
        add("Edit…") { SnippetEditor.show(.edit(s), in: o) }
        add("Duplicate…") { SnippetEditor.show(.duplicate(s), in: o) }
        m.addItem(.separator())
        add("Delete…") { [weak self] in
            guard let self else { return }
            Confirm.show(in: window, title: "Delete “\(s.name)”?",
                         message: "Removes it from \(WindowModel.prettyPath(s.source)). The file’s other snippets and comments stay.",
                         button: "Delete Snippet") { [weak self] in
                guard let self else { return }
                Task {
                    do {
                        store.set(try await Services.snippets.Delete("snippet", s.source, s.index, store.list.versions[s.source] ?? "", cwd))
                        Toast.show("Deleted “\(s.name)”", in: self)
                    } catch {
                        Toast.show(error.localizedDescription, in: self)
                    }
                }
            }
        }
        return m
    }
}

/// Fill in a snippet's placeholders, then insert or run it — or run a
/// runbook's steps one by one in the shell that was focused. Like SnippetDialog.svelte.
@MainActor
final class SnippetRun: NSObject, NSTextFieldDelegate {
    private static var current: SnippetRun?

    private let snippet: Snippet
    private unowned let owner: WindowController
    private let pane: Int?
    private let sheet: FormSheet
    private let fields: [Field]
    private var inputs: [(field: Field, control: NSControl)] = []
    private let preview: NSTextField
    private var steps: [(label: NSTextField, command: NSTextField, buttons: [NSButton])] = []
    private var done = Set<Int>()

    static func show(_ s: Snippet, in w: WindowController) {
        let r = SnippetRun(s, w)
        current = r
        r.sheet.present(in: w.window, focus: r.inputs.first?.control)
    }

    private init(_ s: Snippet, _ w: WindowController) {
        snippet = s
        owner = w
        pane = w.activePane?.id
        fields = s.fields
        sheet = FormSheet(title: s.name, subtitle: [s.description, "From \(WindowModel.prettyPath(s.source))"].filter { !$0.isEmpty }.joined(separator: " · "),
                          width: s.isRunbook ? 600 : 520)
        preview = sheet.note("", mono: true)
        super.init()
        let facts = ShellFacts(pane: w.activePane)
        for f in fields {
            let value = f.initialValue(facts)
            let control: NSControl
            if !f.choices.isEmpty {
                let pop = sheet.popup(f.choices, selected: max(0, f.choices.firstIndex(of: value) ?? 0))
                pop.target = self
                pop.action = #selector(changed)
                control = pop
            } else {
                let t = sheet.text(value, placeholder: f.default)
                t.delegate = self
                control = t
            }
            sheet.field(f.description.isEmpty ? f.name : "\(f.name) — \(f.description)", control)
            inputs.append((f, control))
        }
        if s.isRunbook {
            for (i, step) in s.steps.enumerated() {
                let badge = NSTextField(labelWithString: "\(i + 1)")
                badge.font = .monospacedDigitSystemFont(ofSize: 12, weight: .bold)
                badge.textColor = .controlAccentColor
                badge.widthAnchor.constraint(equalToConstant: 22).isActive = true
                let title = NSTextField(labelWithString: step.title.isEmpty ? "Step \(i + 1)" : step.title)
                title.font = .systemFont(ofSize: 12.5, weight: .medium)
                title.lineBreakMode = .byTruncatingTail
                title.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
                let insert = ClosureButton(title: "Insert") { [weak self] in self?.step(i, .insert) }
                insert.toolTip = "Put it at the prompt"
                insert.controlSize = .small
                let run = ClosureButton(title: "Run") { [weak self] in self?.step(i, .run) }
                run.controlSize = .small
                run.bezelColor = .controlAccentColor
                let spacer = NSView()
                spacer.setContentHuggingPriority(.init(1), for: .horizontal)
                let head = NSStackView(views: [badge, title, spacer, insert, run])
                head.widthAnchor.constraint(equalToConstant: 552).isActive = true
                let cmd = sheet.note("", mono: true)
                let box = NSStackView(views: [head, cmd])
                box.orientation = .vertical
                box.alignment = .leading
                box.spacing = 3
                sheet.add(box)
                steps.append((badge, cmd, [insert, run]))
            }
            sheet.add(sheet.note("Steps run in the focused terminal, one at a time: watch each one finish before the next."))
            sheet.buttons([], cancel: "Close")
        } else {
            sheet.add(preview)
            sheet.buttons([
                ("Run in New Tab", false, { [weak self] in self?.act(.tab) }),
                ("Run", false, { [weak self] in self?.act(.run) }),
                ("Insert", true, { [weak self] in self?.act(.insert) }),
            ])
            sheet.button("Run")?.keyEquivalent = "\r"
            sheet.button("Run")?.keyEquivalentModifierMask = .command
            sheet.button("Run")?.toolTip = "⌘↩"
            sheet.button("Insert")?.toolTip = "Put it at the prompt to check, then press Enter (↩)"
        }
        sheet.onClose = { Self.current = nil }
        changed()
    }

    private var values: [String: String] {
        var v: [String: String] = [:]
        for (f, c) in inputs { v[f.name] = (c as? NSPopUpButton)?.titleOfSelectedItem ?? (c as? NSTextField)?.stringValue ?? "" }
        return v
    }

    private func filled(_ command: String) -> Result<String, Error> {
        Result { try Placeholders.fill(command, values, fields) }
    }

    func controlTextDidChange(_ obj: Notification) { changed() }

    @objc private func changed() {
        if snippet.isRunbook {
            for (i, s) in steps.enumerated() {
                switch filled(snippet.steps[i].command) {
                case .success(let c):
                    s.command.stringValue = c
                    s.command.font = .monospacedSystemFont(ofSize: 11.5, weight: .regular)
                    s.command.textColor = done.contains(i) ? .secondaryLabelColor : .labelColor
                    s.buttons.forEach { $0.isEnabled = true }
                case .failure(let e):
                    s.command.stringValue = e.localizedDescription
                    s.command.font = .systemFont(ofSize: 11.5)
                    s.command.textColor = .secondaryLabelColor
                    s.buttons.forEach { $0.isEnabled = false }
                }
                s.label.stringValue = done.contains(i) ? "✓" : "\(i + 1)"
                s.label.textColor = done.contains(i) ? .systemGreen : .controlAccentColor
            }
            return
        }
        let ok: Bool
        switch filled(snippet.command) {
        case .success(let c):
            preview.stringValue = c
            preview.font = .monospacedSystemFont(ofSize: 11.5, weight: .regular)
            preview.textColor = .labelColor
            ok = true
        case .failure(let e):
            preview.stringValue = e.localizedDescription
            preview.font = .systemFont(ofSize: 11.5)
            preview.textColor = .secondaryLabelColor
            ok = false
        }
        for b in ["Run in New Tab", "Run", "Insert"] { sheet.button(b)?.isEnabled = ok }
    }

    private func act(_ how: WindowController.SnippetHow) {
        guard case .success(let c) = filled(snippet.command) else { return }
        // The sheet goes first so the terminal can take the keyboard.
        if how == .tab || owner.runSnippetReady(pane) {
            sheet.close()
            owner.runSnippet(c, how, pane: pane)
        } else {
            owner.runSnippet(c, how, pane: pane)
        }
    }

    private func step(_ i: Int, _ how: WindowController.SnippetHow) {
        guard case .success(let c) = filled(snippet.steps[i].command) else { return }
        if owner.runSnippet(c, how, pane: pane) {
            done.insert(i)
            changed()
        }
    }
}

extension WindowController {
    /// Whether a snippet can go into the pane now (an idle shell).
    func runSnippetReady(_ pane: Int?) -> Bool {
        guard let id = pane ?? activePane?.id, let p = model.panes[id] else { return false }
        return terminals[id] != nil && p.isShell && !p.running
    }
}

/// The snippet editor: a command or a runbook of steps, its placeholders,
/// the file to save to; then a review of exactly what will be written.
/// Like SnippetEditor.svelte.
@MainActor
final class SnippetEditor: NSObject {
    enum Source {
        case new(command: String)
        case edit(Snippet)
        case duplicate(Snippet)
    }

    struct ParamDraft { var description = "", def = "", choices = "", raw = false }

    private static var open: [SnippetEditor] = []
    private unowned let owner: WindowController
    private let editing: Snippet?
    private let tags: [String]
    private let panel: NSPanel
    private let content = NSStackView()
    private let paramsBox = NSStackView()
    private var name = "", summary = "", command = ""
    private var runbook = false
    private var steps: [Go.snippets.Step] = [.init()]
    private var params: [String: ParamDraft] = [:]
    private var file = ""
    private var error: String?
    private var review: (edit: Go.snippets.Edit, preview: Go.snippets.Preview)?
    private var busy = false
    private let width: CGFloat = 600

    static func show(_ source: Source, in w: WindowController) {
        let e = SnippetEditor(source, w)
        open.append(e)
        w.window?.beginSheet(e.panel)
    }

    private init(_ source: Source, _ w: WindowController) {
        owner = w
        panel = NSPanel(contentRect: NSRect(x: 0, y: 0, width: 600, height: 480), styleMask: [.titled, .docModalWindow, .resizable],
                        backing: .buffered, defer: false)
        var from: Snippet?
        switch source {
        case .new(let c): editing = nil; command = c
        case .edit(let s): editing = s; from = s
        case .duplicate(let s): editing = nil; from = s
        }
        tags = from?.tags ?? []
        super.init()
        if let s = from {
            name = editing == nil ? "\(s.name) (copy)" : s.name
            summary = s.description
            command = s.command
            runbook = s.isRunbook
            if s.isRunbook { steps = s.steps }
            for p in s.params {
                params[p.name] = ParamDraft(description: p.description, def: p.default, choices: p.choices.joined(separator: ", "), raw: p.raw)
            }
        }
        let store = SnippetStore.shared
        file = editing?.source ?? store.list.targets.first { $0.kind == "user" }?.path ?? store.list.targets.first?.path ?? ""
        content.orientation = .vertical
        content.alignment = .leading
        content.spacing = 10
        content.edgeInsets = NSEdgeInsets(top: 18, left: 22, bottom: 16, right: 22)
        paramsBox.orientation = .vertical
        paramsBox.alignment = .leading
        paramsBox.spacing = 6
        let scroll = NSScrollView()
        scroll.documentView = content
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        panel.contentView = scroll
        content.translatesAutoresizingMaskIntoConstraints = false
        content.widthAnchor.constraint(equalToConstant: width).isActive = true
        render()
        if !store.loaded || editing == nil {
            store.observe(self) { $0.listLoaded() }
            store.load(cwd: w.activePane?.cwd ?? "")
        }
    }

    private func listLoaded() {
        guard editing == nil else { return }
        let targets = SnippetStore.shared.list.targets
        if file.isEmpty || !targets.contains(where: { $0.path == file }) {
            file = targets.first { $0.kind == "user" }?.path ?? targets.first?.path ?? ""
        }
        if review == nil { render() }
    }

    // MARK: Building the form

    private func label(_ s: String, bold: Bool = false, size: CGFloat = 12) -> NSTextField {
        let l = NSTextField(labelWithString: s)
        l.font = bold ? .systemFont(ofSize: size, weight: .semibold) : .systemFont(ofSize: size)
        return l
    }

    private func field(_ value: String, _ placeholder: String, width: CGFloat, mono: Bool = false, _ set: @escaping (String) -> Void) -> NSTextField {
        let f = BoundField(value, set)
        f.placeholderString = placeholder
        if mono { f.font = .monospacedSystemFont(ofSize: 12, weight: .regular) }
        f.widthAnchor.constraint(equalToConstant: width).isActive = true
        return f
    }

    private func small(_ title: String, _ run: @escaping () -> Void) -> NSButton {
        let b = ClosureButton(title: title, run)
        b.bezelStyle = .accessoryBarAction
        return b
    }

    private var inner: CGFloat { width - 44 }

    private func render() {
        content.arrangedSubviews.forEach { $0.removeFromSuperview() }
        if let review {
            renderReview(review)
            return
        }
        content.addArrangedSubview(label(editing.map { "Edit “\($0.name)”" } ?? "New Snippet", bold: true, size: 15))
        let sub = label("A command with blanks to fill in, or a runbook of steps")
        sub.textColor = .secondaryLabelColor
        content.addArrangedSubview(sub)
        let nameField = field(name, "Restart a deployment", width: inner) { [weak self] in
            self?.name = $0
            self?.updateReview()
        }
        content.addArrangedSubview(labelled("Name", nameField))
        content.addArrangedSubview(labelled("Description", field(summary, "optional — shown in the list", width: inner) { [weak self] in self?.summary = $0 }))
        let kind = NSSegmentedControl(labels: ["Command", "Runbook (steps)"], trackingMode: .selectOne, target: nil, action: nil)
        kind.selectedSegment = runbook ? 1 : 0
        let kw = PopupWatcher { [weak self, weak kind] in
            self?.runbook = kind?.selectedSegment == 1
            self?.render()
        }
        kind.target = kw
        kind.action = #selector(PopupWatcher.changed)
        objc_setAssociatedObject(kind, "watch", kw, .OBJC_ASSOCIATION_RETAIN)
        content.addArrangedSubview(kind)
        if runbook {
            for (i, s) in steps.enumerated() { content.addArrangedSubview(stepRow(i, s)) }
            content.addArrangedSubview(small("+ Add Step") { [weak self] in
                self?.steps.append(.init())
                self?.render()
            })
        } else {
            content.addArrangedSubview(labelled("Command", field(command, "kubectl -n {{ns=$kube_namespace}} rollout restart deploy/{{name}}", width: inner, mono: true) { [weak self] in
                self?.command = $0
                self?.renderParams()
            }))
        }
        let hint = label("{{name}} asks for a value; {{name=default}} fills one in. Defaults can follow the shell: $kube_namespace, $kube_context, $aws_profile, $aws_region, $cwd.", size: 11)
        hint.textColor = .secondaryLabelColor
        hint.lineBreakMode = .byWordWrapping
        hint.preferredMaxLayoutWidth = inner
        hint.maximumNumberOfLines = 0
        content.addArrangedSubview(hint)
        content.addArrangedSubview(paramsBox)
        renderParams()

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
            e.preferredMaxLayoutWidth = inner
            content.addArrangedSubview(e)
        }
        let cancel = ClosureButton(title: "Cancel") { [weak self] in self?.close() }
        cancel.keyEquivalent = "\u{1b}"
        let next = ClosureButton(title: "Review…") { [weak self] in self?.startReview() }
        next.keyEquivalent = "\r"
        reviewButton = next
        updateReview()
        let row = NSStackView(views: [NSView(), cancel, next])
        row.widthAnchor.constraint(equalToConstant: inner).isActive = true
        content.addArrangedSubview(row)
        panel.initialFirstResponder = nameField
        resize()
    }

    private weak var reviewButton: NSButton?
    private func updateReview() { reviewButton?.isEnabled = !busy && !name.trimmingCharacters(in: .whitespaces).isEmpty && !file.isEmpty }

    private func labelled(_ title: String, _ control: NSView) -> NSView {
        let s = NSStackView(views: [label(title, bold: true), control])
        s.orientation = .vertical
        s.alignment = .leading
        s.spacing = 4
        return s
    }

    private func stepRow(_ i: Int, _ s: Go.snippets.Step) -> NSView {
        let up = small("↑") { [weak self] in self?.move(i, -1) }
        up.isEnabled = i > 0
        up.setAccessibilityLabel("Move step \(i + 1) up")
        let down = small("↓") { [weak self] in self?.move(i, 1) }
        down.isEnabled = i < steps.count - 1
        down.setAccessibilityLabel("Move step \(i + 1) down")
        let remove = small("×") { [weak self] in
            self?.steps.remove(at: i)
            self?.render()
        }
        remove.isEnabled = steps.count > 1
        remove.setAccessibilityLabel("Remove step \(i + 1)")
        let fields = NSStackView(views: [
            field(s.title, "What this step does (optional)", width: 420) { [weak self] in self?.steps[i].title = $0 },
            field(s.command, "kubectl cordon {{node}}", width: 420, mono: true) { [weak self] in
                self?.steps[i].command = $0
                self?.renderParams()
            },
        ])
        fields.orientation = .vertical
        fields.alignment = .leading
        fields.spacing = 4
        return NSStackView(views: [label("\(i + 1)", bold: true), fields, up, down, remove])
    }

    private func move(_ i: Int, _ d: Int) {
        let j = i + d
        guard j >= 0, j < steps.count else { return }
        steps.swapAt(i, j)
        render()
    }

    private var currentFields: [Field] {
        Placeholders.fields(in: runbook ? steps.map(\.command).filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty } : [command], params: [])
    }

    /// The placeholders as the command stands: redrawn as it's typed, so the
    /// command field keeps the keyboard.
    private func renderParams() {
        paramsBox.arrangedSubviews.forEach { $0.removeFromSuperview() }
        let fields = currentFields
        guard !fields.isEmpty else {
            resize()
            return
        }
        paramsBox.addArrangedSubview(label("Placeholders", bold: true))
        for f in fields {
            let n = f.name
            let p = params[n] ?? ParamDraft()
            let raw = NSButton(checkboxWithTitle: "raw", target: nil, action: nil)
            raw.state = p.raw ? .on : .off
            raw.toolTip = "Insert as typed instead of shell-quoting (for several flags at once)"
            let rw = PopupWatcher { [weak self, weak raw] in self?.params[n, default: ParamDraft()].raw = raw?.state == .on }
            raw.target = rw
            raw.action = #selector(PopupWatcher.changed)
            objc_setAssociatedObject(raw, "watch", rw, .OBJC_ASSOCIATION_RETAIN)
            let name = label(n)
            name.font = .monospacedSystemFont(ofSize: 11.5, weight: .medium)
            name.widthAnchor.constraint(equalToConstant: 80).isActive = true
            name.lineBreakMode = .byTruncatingTail
            let defHint = !f.default.isEmpty && f.default != p.def ? "default (\(f.default) in the command)" : "default"
            paramsBox.addArrangedSubview(NSStackView(views: [
                name,
                field(p.description, "what to enter", width: 140) { [weak self] in self?.params[n, default: ParamDraft()].description = $0 },
                field(p.def, defHint, width: 130) { [weak self] in self?.params[n, default: ParamDraft()].def = $0 },
                field(p.choices, "choices, comma-separated", width: 140) { [weak self] in self?.params[n, default: ParamDraft()].choices = $0 },
                raw,
            ]))
        }
        resize()
    }

    private func resize() {
        content.layoutSubtreeIfNeeded()
        let h = min(max(300, content.fittingSize.height), (owner.window?.frame.height ?? 700) * 0.85)
        panel.setContentSize(NSSize(width: width, height: h))
    }

    // MARK: Review and save

    private func startReview() {
        guard !busy else { return }
        guard !name.trimmingCharacters(in: .whitespaces).isEmpty else {
            error = "Give the snippet a name"
            render()
            return
        }
        guard !file.isEmpty else {
            error = "There's no snippets file to save to from here"
            render()
            return
        }
        let def = Go.snippets.Def(
            name: name, description: summary, tags: tags, command: runbook ? "" : command, steps: runbook ? steps : [],
            params: currentFields.map { f in
                let p = params[f.name] ?? ParamDraft()
                return .init(name: f.name, description: p.description, default: p.def,
                             choices: p.choices.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }, raw: p.raw)
            })
        let edit = Go.snippets.Edit(kind: "snippet", file: file, index: editing?.index ?? -1,
                                    version: SnippetStore.shared.list.versions[file] ?? "", snippet: def)
        busy = true
        Task {
            do {
                let preview = try await Services.snippets.PlanEdit(edit, owner.activePane?.cwd ?? "")
                error = nil
                review = (edit, preview)
            } catch {
                self.error = error.localizedDescription
            }
            busy = false
            render()
        }
    }

    private func renderReview(_ r: (edit: Go.snippets.Edit, preview: Go.snippets.Preview)) {
        content.addArrangedSubview(label("Review", bold: true, size: 15))
        let verb = r.preview.newFile ? "Creates" : (editing != nil ? "Replaces this snippet in" : "Adds to the end of")
        let sub = label("\(verb) \(WindowModel.prettyPath(r.preview.file)). Nothing is written until you confirm.")
        sub.textColor = .secondaryLabelColor
        content.addArrangedSubview(sub)
        let text = NSTextField(wrappingLabelWithString: r.preview.text)
        text.font = .monospacedSystemFont(ofSize: 11.5, weight: .regular)
        text.isSelectable = true
        text.preferredMaxLayoutWidth = inner
        content.addArrangedSubview(text)
        if editing != nil {
            let n = label("The rest of the file — other snippets, your comments — stays as it is.", size: 11)
            n.textColor = .secondaryLabelColor
            content.addArrangedSubview(n)
        }
        let back = ClosureButton(title: "Back") { [weak self] in
            self?.review = nil
            self?.render()
        }
        let save = ClosureButton(title: "Confirm & Save") { [weak self] in self?.save(r.edit) }
        save.keyEquivalent = "\r"
        back.isEnabled = !busy
        save.isEnabled = !busy
        let row = NSStackView(views: [NSView(), back, save])
        row.widthAnchor.constraint(equalToConstant: inner).isActive = true
        content.addArrangedSubview(row)
        resize()
    }

    private func save(_ edit: Go.snippets.Edit) {
        guard !busy else { return }
        busy = true
        Task {
            do {
                SnippetStore.shared.set(try await Services.snippets.ApplyEdit(edit, owner.activePane?.cwd ?? ""))
                close()
                let n = edit.snippet.name.trimmingCharacters(in: .whitespaces)
                Toast.show(editing != nil ? "Saved “\(n)”" : "Added “\(n)”", in: owner.rootView)
            } catch {
                self.error = error.localizedDescription
                review = nil
                busy = false
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

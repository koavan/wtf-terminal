import AppKit
import WtfCore

/// The Findings tab: security scanners' SARIF results for the focused
/// shell's repository, by severity then rule. Read when the tab opens,
/// when the shell leaves the folder, on refresh, and when a scan finishes;
/// never polled. Like FindingsView.svelte.
@MainActor
final class FindingsView: NSView, SidePaneTab, NSSearchFieldDelegate {
    private unowned let owner: WindowController
    private let header = PaneHeader("Findings")
    private let filter = NSSearchField()
    private let list = SideList()
    private var note: NoteView?
    private var noteButton: NSButton?
    private var refresh: ToolButton?
    private var report: Go.findings.Report?
    private var error: String?
    private var loading = false
    private var folder: String?
    private var seq = 0
    private var folded = Set<String>()
    private var openRules = Set<String>()
    private var items: [String: Finding] = [:]

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        header.tool("play.fill", "Run a security scan") { [weak self] in
            guard let self else { return }
            ScanDialog.show(in: owner)
        }
        refresh = header.tool("arrow.clockwise", "Read the results again") { [weak self] in
            guard let self, !cwd.isEmpty else { return }
            load(cwd)
        }
        addSubview(header)
        filter.placeholderString = "Filter by rule, file, message, tool"
        filter.delegate = self
        filter.sendsSearchStringImmediately = true
        filter.refusesFirstResponder = true
        addSubview(filter)
        list.onClick = { [weak self] id in self?.clicked(id) }
        list.menuProvider = { [weak self] ids in ids.first.flatMap { self?.menu($0) } }
        addSubview(list)
        owner.records.observeScans(self) { v in v.folder.map { v.load($0) } }
        if !cwd.isEmpty { load(cwd) }
        rebuild()
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    private var total: Int { report?.findings.count ?? 0 }

    override func layout() {
        super.layout()
        header.frame = NSRect(x: 0, y: 0, width: bounds.width, height: 30)
        var y: CGFloat = 32
        filter.isHidden = total == 0
        if !filter.isHidden {
            filter.frame = NSRect(x: 8, y: y, width: bounds.width - 16, height: 24)
            y += 30
        }
        if let note {
            let h = note.height(for: bounds.width)
            note.frame = NSRect(x: 0, y: y, width: bounds.width, height: h)
            y += h
            if let b = noteButton {
                b.frame = NSRect(x: NoteView.inset, y: y - 4, width: b.fittingSize.width + 8, height: 24)
                y += 26
            }
        }
        list.frame = NSRect(x: 0, y: y, width: bounds.width, height: max(0, bounds.height - y))
    }

    private var cwd: String { owner.activePane?.cwd ?? "" }

    func paneChanged() {
        if !cwd.isEmpty, Findings.stale(cwd, root: report?.root, loaded: folder) { load(cwd) }
        refresh?.isEnabled = !loading && !cwd.isEmpty
    }

    func debugSummary() -> String { "findings: \(total) in \(report?.root ?? "-"), error \(error ?? "none")" }

    func controlTextDidChange(_ obj: Notification) { rebuild() }

    private func load(_ f: String) {
        folder = f
        seq += 1
        let mine = seq
        loading = true
        refresh?.isEnabled = false
        Task {
            do {
                let r = try await Services.findings.Load(f)
                guard mine == seq else { return }
                report = r
                error = nil
            } catch {
                guard mine == seq else { return }
                report = nil
                self.error = error.localizedDescription
            }
            loading = false
            refresh?.isEnabled = !cwd.isEmpty
            rebuild()
        }
    }

    private func rebuild() {
        let root = report?.root ?? ""
        header.text = root.isEmpty ? "Findings" : (WindowModel.prettyPath(root) as NSString).lastPathComponent
        header.tooltip = root.isEmpty ? nil : "Findings for \(root)"
        items = [:]
        note?.removeFromSuperview()
        noteButton?.removeFromSuperview()
        note = nil
        noteButton = nil
        var sections: [SideList.Section] = []
        if let error {
            note = paneNote(nil, error)
        } else if let report {
            if !report.sources.isEmpty {
                sections.append(.init(title: "Results", rows: report.sources.enumerated().map { i, s in
                    .init(id: "s:\(i)", symbol: s.fromScan ? "shield.lefthalf.filled" : "doc.text", tint: .secondaryLabelColor,
                          title: s.tool + (s.version.isEmpty ? "" : " \(s.version)"),
                          detail: s.error.isEmpty ? "\(s.count) \(s.count == 1 ? "finding" : "findings") · \(Findings.relative(s.modified))" : "",
                          alert: s.error,
                          tip: WindowModel.prettyPath(s.path) + (s.fromScan ? "\nFrom a scan run in wtf (kept in its cache)" : ""))
                }))
            }
            if report.truncated > 0 {
                sections.append(.init(title: nil, rows: [], empty: "Showing the first \(total) findings; \(report.truncated) more are in the files."))
            }
            let groups = Findings.group(report.findings, filter: filter.stringValue)
            for g in groups {
                var rows: [SideList.Row] = []
                for r in g.rules {
                    let ruleKey = g.severity + "\n" + r.key
                    let open = openRules.contains(ruleKey)
                    let tipDesc = r.items.first.map { $0.description.isEmpty || $0.description == r.title ? "" : "\n\($0.description)" } ?? ""
                    rows.append(.init(id: "r:" + ruleKey, symbol: open ? "chevron.down" : "chevron.right", tint: .secondaryLabelColor,
                                      title: r.title, detail: (r.ruleId != r.title && !r.ruleId.isEmpty ? "\(r.ruleId) · " : "") + r.tool,
                                      tip: r.ruleId + tipDesc + "\n\(r.tool)",
                                      columns: [.init(text: "\(r.items.count)", width: 32)]))
                    if open {
                        for (i, f) in r.items.enumerated() {
                            let id = "f:\(ruleKey)\n\(i)"
                            items[id] = f
                            let firstLine = f.message.split(separator: "\n").first.map(String.init) ?? ""
                            rows.append(.init(id: id, symbol: f.path.hasPrefix("/") ? "doc" : "link", tint: .secondaryLabelColor,
                                              title: Findings.where(f, root: root).isEmpty ? "no location" : Findings.where(f, root: root),
                                              mono: true, detail: firstLine == r.title ? "" : firstLine,
                                              tip: f.message + (f.score != 0 ? "\nScore \(f.score)" : ""), depth: 1))
                        }
                    }
                }
                let sev = g.severity
                sections.append(.init(title: Findings.labels[sev] ?? sev.capitalized, rows: rows,
                                      toggle: { [weak self] in
                                          guard let self else { return }
                                          if folded.contains(sev) { folded.remove(sev) } else { folded.insert(sev) }
                                          rebuild()
                                      }, open: !folded.contains(sev), badge: "\(g.count)"))
            }
            if groups.isEmpty {
                if !filter.stringValue.trimmingCharacters(in: .whitespaces).isEmpty {
                    note = paneNote(nil, "No findings match “\(filter.stringValue)”.")
                } else if !report.sources.isEmpty {
                    note = paneNote("No findings", "The scanners found nothing in \(WindowModel.prettyPath(root)).")
                } else {
                    note = paneNote("No scan results for \(WindowModel.prettyPath(root))",
                                    "Run a scanner — Trivy, Semgrep, Checkov, Gitleaks or Grype — and its findings show here, grouped by severity. A .sarif file at the top of the folder (from CI, say) is read too.")
                    let b = ClosureButton(title: "Run Scan…") { [weak self] in
                        guard let self else { return }
                        ScanDialog.show(in: owner)
                    }
                    b.bezelColor = .controlAccentColor
                    noteButton = b
                }
            }
        } else {
            note = paneNote(nil, cwd.isEmpty ? "Focus a terminal to see findings for its folder." : "Reading results…")
        }
        list.set(sections)
        if let note { addSubview(note) }
        if let noteButton { addSubview(noteButton) }
        needsLayout = true
    }

    private func clicked(_ id: String) {
        if id.hasPrefix("r:") {
            let key = String(id.dropFirst(2))
            if openRules.contains(key) { openRules.remove(key) } else { openRules.insert(key) }
            rebuild()
        } else if let f = items[id], f.path.hasPrefix("/") {
            openInEditor(f)
        }
    }

    /// The file in $EDITOR, in a split next to the shell, at its line.
    private func openInEditor(_ f: Finding) {
        let dir = (f.path as NSString).deletingLastPathComponent
        owner.model.split(.row, cwd: dir, command: "${EDITOR:-vi} " + (f.line > 0 ? "+\(f.line) " : "") + Shell.quote(f.path))
    }

    private func menu(_ id: String) -> NSMenu? {
        let m = NSMenu()
        m.autoenablesItems = false
        func add(_ t: String, _ run: @escaping () -> Void) { m.addItem(ClosureMenuItem(title: t, keyEquivalent: "", action: run)) }
        let o = owner
        func copy(_ s: String, _ toast: String) {
            Clipboard.copy(s)
            Toast.show(toast, in: o.rootView)
        }
        func ruleItems(_ f: Finding) {
            if !f.ruleId.isEmpty { add("Copy Rule ID") { copy(f.ruleId, "Copied rule ID") } }
            if f.helpUri.range(of: "^https?://", options: .regularExpression) != nil, let u = URL(string: f.helpUri) {
                add("Open Rule Documentation") { NSWorkspace.shared.open(u) }
            }
        }
        if let f = items[id] {
            let root = report?.root ?? ""
            if f.path.hasPrefix("/") {
                add(f.line > 0 ? "Open in Editor at Line \(f.line)" : "Open in Editor") { [weak self] in self?.openInEditor(f) }
                add("Reveal in Finder") {
                    Task {
                        do { try await Services.files.Reveal(f.path) } catch {
                            Toast.show("Couldn't reveal it: \(error.localizedDescription)", in: o.rootView)
                        }
                    }
                }
                m.addItem(.separator())
                add("Copy Path") { copy(f.path, "Copied path") }
                if f.line > 0 {
                    let w = Findings.where(f, root: root)
                    add("Copy \(w)") { copy(w, "Copied path and line") }
                }
            } else if !f.path.isEmpty {
                add("Copy Location") { copy(f.path, "Copied location") }
            }
            if m.numberOfItems > 0 { m.addItem(.separator()) }
            add("Copy Message") { copy(f.message, "Copied message") }
            ruleItems(f)
            return m
        }
        if id.hasPrefix("r:") {
            let key = String(id.dropFirst(2))
            add(openRules.contains(key) ? "Hide Locations" : "Show Locations") { [weak self] in self?.clicked(id) }
            m.addItem(.separator())
            if let f = report?.findings.first(where: { key == $0.severity + "\n" + $0.tool + "\n" + $0.ruleId }) { ruleItems(f) }
            return m
        }
        if id.hasPrefix("s:"), let i = Int(id.dropFirst(2)), let s = report?.sources[safe: i] {
            add("Reveal in Finder") { Task { try? await Services.files.Reveal(s.path) } }
            add("Copy Path") { copy(s.path, "Copied path") }
            if s.fromScan {
                m.addItem(.separator())
                add("Remove These Results…") { [weak self] in
                    Confirm.show(in: o.window, title: "Remove the \(s.tool) results?",
                                 message: "Deletes \(WindowModel.prettyPath(s.path)) from wtf's cache. Run the scan again to get them back.",
                                 button: "Remove") {
                        Task {
                            do {
                                try await Services.findings.Forget(s.path)
                                if let f = self?.folder { self?.load(f) }
                            } catch {
                                Toast.show("Couldn't remove them: \(error.localizedDescription)", in: o.rootView)
                            }
                        }
                    }
                }
            }
            return m
        }
        return nil
    }
}

extension Array {
    subscript(safe i: Int) -> Element? { indices.contains(i) ? self[i] : nil }
}

/// Run Security Scan: pick an installed scanner, see the exact command, and
/// run it in the focused shell; its SARIF goes to wtf's cache, not the repo.
/// Like ScanDialog.svelte.
@MainActor
final class ScanDialog: NSObject {
    private static var current: ScanDialog?
    /// The last scanner run, for this app session.
    private static var lastChoice = "trivy"

    private unowned let owner: WindowController
    private let paneId: Int?
    private let cwd: String
    private let sheet: FormSheet
    private let body = NSStackView()
    private var scanners: [Go.findings.Scanner]?
    private var choice = ""
    private var scan: Go.findings.Scan?
    private var error: String?

    static func show(in w: WindowController) {
        let d = ScanDialog(w)
        current = d
        d.sheet.present(in: w.window)
    }

    private init(_ w: WindowController) {
        owner = w
        paneId = w.activePane?.id
        cwd = w.activePane?.cwd ?? ""
        sheet = FormSheet(title: "Run Security Scan", subtitle: "Scans the focused shell’s repository.", width: 520)
        super.init()
        body.orientation = .vertical
        body.alignment = .leading
        body.spacing = 8
        body.widthAnchor.constraint(equalToConstant: 472).isActive = true
        sheet.add(body)
        sheet.buttons([("Run Scan", true, { [weak self] in self?.run() })], left: [])
        sheet.onClose = { Self.current = nil }
        render()
        guard !cwd.isEmpty else { return }
        let path = w.activePane?.env?["PATH"] ?? ""
        Task {
            do {
                let list = try await Services.findings.Scanners(path)
                scanners = list
                choice = list.contains { $0.id == Self.lastChoice } ? Self.lastChoice : (list.first { $0.found } ?? list.first)?.id ?? ""
                fetchCommand()
            } catch {
                self.error = error.localizedDescription
                scanners = []
            }
            render()
        }
    }

    private var chosen: Go.findings.Scanner? { scanners?.first { $0.id == choice } }

    private func fetchCommand() {
        scan = nil
        let id = choice
        Task {
            do {
                let s = try await Services.findings.ScanCommand(cwd, id)
                guard choice == id else { return }
                scan = s
                error = nil
            } catch {
                self.error = error.localizedDescription
            }
            render()
        }
    }

    private func label(_ s: String, size: CGFloat = 12, muted: Bool = false, mono: Bool = false) -> NSTextField {
        let l = NSTextField(wrappingLabelWithString: s)
        l.font = mono ? .monospacedSystemFont(ofSize: 11.5, weight: .regular) : .systemFont(ofSize: size)
        if muted { l.textColor = .secondaryLabelColor }
        l.preferredMaxLayoutWidth = 472
        l.isSelectable = mono
        return l
    }

    private func render() {
        body.arrangedSubviews.forEach { $0.removeFromSuperview() }
        if let scan { sheet.setSubtitle("Scans \(WindowModel.prettyPath(scan.root)). Results show in the Findings tab when it finishes.") }
        if cwd.isEmpty {
            let e = label("Focus a terminal first: the scan runs in its folder.")
            e.textColor = .systemRed
            body.addArrangedSubview(e)
        } else if let scanners {
            for s in scanners {
                let radio = NSButton(radioButtonWithTitle: s.name + (s.found ? "" : " · not installed"), target: nil, action: nil)
                radio.state = s.id == choice ? .on : .off
                let w = PopupWatcher { [weak self] in
                    self?.choice = s.id
                    self?.fetchCommand()
                    self?.render()
                }
                radio.target = w
                radio.action = #selector(PopupWatcher.changed)
                objc_setAssociatedObject(radio, "watch", w, .OBJC_ASSOCIATION_RETAIN)
                let about = label(s.about, size: 11, muted: true)
                let col = NSStackView(views: [radio, about])
                col.orientation = .vertical
                col.alignment = .leading
                col.spacing = 1
                body.addArrangedSubview(col)
            }
            if let note = chosen?.note, !note.isEmpty { body.addArrangedSubview(label(note, size: 11, muted: true)) }
            if let scan {
                body.addArrangedSubview(label(scan.command, mono: true))
                body.addArrangedSubview(label("Runs in your shell (a new tab if it’s busy). The SARIF goes to \(WindowModel.prettyPath(scan.output)), not into the repository.", size: 11, muted: true))
            }
            if let c = chosen, !c.found {
                body.addArrangedSubview(label("\(c.name) isn't installed. Install it (for example with Homebrew: brew install \(c.tool)), then open this again.", size: 11, muted: true))
            }
        } else {
            body.addArrangedSubview(label("Looking for scanners…", muted: true))
        }
        if let error {
            let e = label(error)
            e.textColor = .systemRed
            body.addArrangedSubview(e)
        }
        sheet.button("Run Scan")?.isEnabled = scan != nil && chosen?.found == true
    }

    private func run() {
        guard let scan, chosen?.found == true else { return }
        Self.lastChoice = choice
        sheet.close()
        let o = owner
        if let id = paneId, let p = o.model.panes[id], p.isShell, !p.running, let term = o.terminals[id] {
            term.run(scan.command)
        } else {
            o.model.open(cwd: o.model.panes[paneId ?? -1]?.cwd ?? cwd, command: scan.command)
            Toast.show("Scanning in a new tab: that shell was busy", in: o.rootView)
        }
        o.sidebar.tab = .findings
        o.sidebar.open = true
        o.layoutSidebar()
    }
}

import AppKit
import WtfCore

/// The command palette (⌘⇧P): commands, tabs, groups and workspaces in one
/// fuzzy list; or themes (previewed as you move) or workspaces alone.
/// Like Palette.svelte. One per window.
@MainActor
final class Palette: NSView, NSTextFieldDelegate {
    enum Mode: Equatable { case commands, themes, workspaces, snippets }

    struct Entry {
        enum Section: Int { case commands, tabs, groups, workspaces, snippets, themes }
        let section: Section
        let label: String
        var detail = ""
        var hint = ""
        var dot: NSColor?
        var symbol: String?
        var theme: Theme?
        let run: () -> Void
    }

    private static var shown: [ObjectIdentifier: Palette] = [:]

    static func toggle(_ mode: Mode, in w: WindowController) {
        if let p = shown[ObjectIdentifier(w)] {
            let same = p.mode == mode
            p.dismiss()
            if same { return }
        }
        TabOverview.close(in: w)
        let p = Palette(mode: mode, window: w)
        shown[ObjectIdentifier(w)] = p
        p.frame = w.rootView.bounds
        p.autoresizingMask = [.width, .height]
        w.rootView.addSubview(p)
        p.window?.makeFirstResponder(p.field)
    }

    static func close(in w: WindowController) { shown[ObjectIdentifier(w)]?.dismiss() }

    private let mode: Mode
    private unowned let owner: WindowController
    private let panel = NSVisualEffectView()
    private let field = NSTextField()
    private let list = PaletteList()
    private let scroll = NSScrollView()
    private var entries: [Entry] = []
    private var rows: [(entry: Entry, match: Fuzzy.Match?)] = []
    private var selected = 0
    private var committed = false

    init(mode: Mode, window: WindowController) {
        self.mode = mode
        owner = window
        super.init(frame: .zero)
        wantsLayer = true
        layer?.backgroundColor = NSColor.black.withAlphaComponent(0.18).cgColor

        panel.material = .popover
        panel.state = .active
        panel.wantsLayer = true
        panel.layer?.cornerRadius = 14
        panel.layer?.masksToBounds = true
        addSubview(panel)
        field.placeholderString = switch mode {
        case .commands: "Search commands, tabs, snippets and workspaces…"
        case .themes: "Search themes…"
        case .workspaces: "Search workspaces…"
        case .snippets: "Search snippets…"
        }
        field.font = .systemFont(ofSize: 15)
        field.isBordered = false
        field.drawsBackground = false
        field.focusRingType = .none
        field.delegate = self
        let icon = NSImageView(image: NSImage(systemSymbolName: "magnifyingglass", accessibilityDescription: nil)!)
        icon.contentTintColor = .secondaryLabelColor
        icon.frame = NSRect(x: 16, y: 16, width: 18, height: 18)
        panel.addSubview(icon)
        panel.addSubview(field)
        let line = NSBox()
        line.boxType = .separator
        line.identifier = .init("line")
        panel.addSubview(line)
        scroll.documentView = list
        scroll.drawsBackground = false
        scroll.hasVerticalScroller = true
        panel.addSubview(scroll)
        list.palette = self
        setAccessibilityLabel("Command palette")
        load()
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    override func layout() {
        super.layout()
        let w = min(600, bounds.width - 32)
        let rowsHeight = CGFloat(max(1, rows.count + headerCount)) * 34 + 12
        let h = min(460, bounds.height - 110, 50 + rowsHeight)
        panel.frame = NSRect(x: (bounds.width - w) / 2, y: 72, width: w, height: h)
        field.frame = NSRect(x: 42, y: 13, width: w - 58, height: 24)
        panel.subviews.first { $0.identifier?.rawValue == "line" }?.frame = NSRect(x: 0, y: 49, width: w, height: 1)
        scroll.frame = NSRect(x: 0, y: 50, width: w, height: h - 50)
        list.frame.size.width = w
        list.rebuild()
    }

    // Clicks on the scrim close it.
    override func mouseDown(with event: NSEvent) {
        if !panel.frame.contains(convert(event.locationInWindow, from: nil)) { dismiss() }
    }

    // MARK: Entries

    private var headerCount: Int {
        guard field.stringValue.trimmingCharacters(in: .whitespaces).isEmpty, mode == .commands else { return 0 }
        return Set(rows.map(\.entry.section)).count
    }

    private func load() {
        switch mode {
        case .commands:
            entries = tabEntries() + groupEntries() + commandEntries()
            loadWorkspaces(trailing: false)
            loadSnippets(trailing: false)
        case .workspaces:
            entries = []
            loadWorkspaces(trailing: true)
        case .snippets:
            entries = []
            loadSnippets(trailing: true)
        case .themes:
            entries = []
            Task {
                let themes = (try? await Backend.call("config", "Themes", as: [ThemeInfo].self)) ?? []
                setThemes(themes)
            }
        }
        refilter()
    }

    struct ThemeInfo: Decodable {
        let name: String
        let dark: Bool
        let builtin: Bool
        let theme: Theme

        init(from decoder: Decoder) throws {
            theme = try Theme(from: decoder)
            let c = try decoder.container(keyedBy: Keys.self)
            name = try c.decode(String.self, forKey: .name)
            dark = (try? c.decode(Bool.self, forKey: .dark)) ?? false
            builtin = (try? c.decode(Bool.self, forKey: .builtin)) ?? false
        }

        enum Keys: String, CodingKey { case name, dark, builtin }
    }

    private func setThemes(_ themes: [ThemeInfo]) {
        let darkMode = Settings.shared.isDark
        let autoTheme = themes.first { $0.name == (darkMode ? "wtf Dark" : "wtf Light") }?.theme
        entries = [Entry(section: .themes, label: "Auto", detail: "wtf Dark or wtf Light, following the system", theme: autoTheme) {
            Palette.setTheme("auto", in: self.owner)
        }]
        for t in themes {
            entries.append(Entry(section: .themes, label: t.name, detail: (t.dark ? "Dark" : "Light") + (t.builtin ? "" : " · from themes folder"),
                                 theme: t.theme) { Palette.setTheme(t.name, in: self.owner) })
        }
        refilter()
        // Start on the theme in use.
        let spec = (Settings.shared.config?.theme ?? "").trimmingCharacters(in: .whitespaces).lowercased()
        let current = Settings.shared.theme?.name
        selected = spec.isEmpty || spec == "auto" ? 0 : (rows.firstIndex { $0.entry.label == current } ?? 0)
        selectionChanged()
    }

    private static func setTheme(_ spec: String, in w: WindowController) {
        Task {
            do {
                try await Backend.run("config", "SetTheme", [spec])
            } catch {
                Settings.shared.preview(nil)
                Toast.show("Couldn't save theme: \(error.localizedDescription)", in: w.rootView)
            }
        }
    }

    private func tabEntries() -> [Entry] {
        owner.model.tabs.map { t in
            let g = t.groupId.flatMap(owner.model.group)
            let pane = owner.model.focusedPane(t)
            let detail = [g.map { $0.name } ?? "", WindowModel.prettyPath(pane?.cwd ?? "")].filter { !$0.isEmpty }.joined(separator: " · ")
            return Entry(section: .tabs, label: Redaction.shared.text(owner.model.title(t)), detail: detail,
                         dot: nil, symbol: TabStripView.icon(pane)) { [owner] in owner.model.activate(t.id) }
        }
    }

    private func groupEntries() -> [Entry] {
        let dark = Settings.shared.theme?.dark ?? true
        return owner.model.groups.map { g in
            let name = g.name.isEmpty ? "unnamed group" : g.name
            return Entry(section: .groups, label: "\(g.collapsed ? "Expand" : "Collapse") \(name)",
                         detail: "\(owner.model.tabsIn(g.id).count) tabs", dot: NSColor(hex: g.color.hex(dark: dark))) { [owner] in
                owner.model.toggleCollapsed(g.id)
            }
        }
    }

    private func commandEntries() -> [Entry] {
        Commands.all.filter { $0.id != "palette" && $0.available(owner) }.map { c in
            Entry(section: .commands, label: c.label, hint: c.key, symbol: "command") { [owner] in c.run(owner) }
        }
    }

    /// Workspaces from the snippets files, read afresh (they're hand-edited).
    private func loadWorkspaces(trailing: Bool) {
        let cwd = owner.activePane?.cwd ?? ""
        Task {
            guard let list = try? await Backend.call("snippets", "List", [cwd], as: SnippetList.self) else { return }
            let ws = list.workspaces.map { w in
                Entry(section: .workspaces, label: w.name,
                      detail: w.description.isEmpty ? (w.tabs.count == 1 ? "1 tab" : "\(w.tabs.count) tabs") : w.description,
                      hint: [w.window == "new" ? "new window" : "", w.project ? "project" : ""].filter { !$0.isEmpty }.joined(separator: " · "),
                      symbol: "square.stack.3d.up") { [owner] in WorkspaceOpen.show(w, in: owner) }
            }
            entries += ws
            if trailing {
                entries.append(Entry(section: .workspaces, label: "Save Window as Workspace…", detail: "Its tabs, groups, splits, folders and running commands",
                                     symbol: "plus") { [owner] in WorkspaceEditor.show(.capture(owner.model.captureWorkspace()), in: owner) })
            }
            refilter()
        }
    }

    /// Snippets from the snippets files, read afresh; the Snippets tab follows.
    private func loadSnippets(trailing: Bool) {
        let cwd = owner.activePane?.cwd ?? ""
        Task {
            guard let list = try? await Services.snippets.List(cwd) else { return }
            SnippetStore.shared.set(list, cwd: cwd)
            entries += list.snippets.map { s in
                Entry(section: .snippets, label: s.name,
                      detail: !s.description.isEmpty ? s.description : s.isRunbook ? "\(s.steps.count) steps" : s.command,
                      hint: [s.isRunbook ? "runbook" : "", s.project ? "project" : ""].filter { !$0.isEmpty }.joined(separator: " · "),
                      symbol: "terminal") { [owner] in owner.openSnippet(s) }
            }
            if trailing {
                entries.append(Entry(section: .snippets, label: "Open Snippets File…", detail: "Add your own; examples included",
                                     symbol: "doc.text") { [owner] in owner.openSnippetsFile() })
            }
            refilter()
        }
    }

    // MARK: Filtering

    private func refilter() {
        let q = field.stringValue
        if q.trimmingCharacters(in: .whitespaces).isEmpty {
            let order: [Entry.Section: Int] = [.commands: 0, .tabs: 1, .groups: 2, .workspaces: 3, .snippets: 4, .themes: 5]
            rows = entries.enumerated().sorted {
                mode == .themes ? $0.offset < $1.offset
                    : (order[$0.element.section]!, $0.offset) < (order[$1.element.section]!, $1.offset)
            }.map { ($0.element, nil) }
        } else {
            rows = Fuzzy.rank(q, entries) { $0.label }.map { ($0.item, $0.match) }
        }
        selected = min(selected, max(0, rows.count - 1))
        needsLayout = true
        list.rebuild()
    }

    func controlTextDidChange(_ note: Notification) {
        selected = 0
        refilter()
        selectionChanged()
    }

    func control(_ control: NSControl, textView: NSTextView, doCommandBy selector: Selector) -> Bool {
        switch selector {
        case #selector(NSResponder.moveDown(_:)): move(1)
        case #selector(NSResponder.moveUp(_:)): move(-1)
        case #selector(NSResponder.insertNewline(_:)): runSelected()
        case #selector(NSResponder.cancelOperation(_:)): dismiss()
        default: return false
        }
        return true
    }

    private func move(_ d: Int) {
        guard !rows.isEmpty else { return }
        selected = (selected + d + rows.count) % rows.count
        selectionChanged()
    }

    fileprivate func select(_ i: Int) {
        guard i != selected, i < rows.count else { return }
        selected = i
        selectionChanged()
    }

    private func selectionChanged() {
        list.rebuild()
        list.scrollToSelected()
        if mode == .themes { Settings.shared.preview(selected < rows.count ? rows[selected].entry.theme : nil) }
    }

    fileprivate func runSelected() { run(selected) }

    fileprivate func run(_ i: Int) {
        guard i < rows.count else { return }
        let e = rows[i].entry
        if mode == .themes { committed = true }
        dismiss()
        e.run()
    }

    @objc func dismiss() {
        Self.shown[ObjectIdentifier(owner)] = nil
        // Leaving without choosing puts the configured theme back.
        if mode == .themes, !committed { Settings.shared.preview(nil) }
        removeFromSuperview()
        owner.focusActive()
    }

    fileprivate var visibleRows: [(header: String?, entry: Entry, match: Fuzzy.Match?)] {
        let showHeaders = headerCount > 0
        var out: [(String?, Entry, Fuzzy.Match?)] = []
        var last: Entry.Section?
        for r in rows {
            var header: String?
            if showHeaders, r.entry.section != last {
                header = switch r.entry.section {
                case .commands: "Commands"
                case .tabs: "Tabs"
                case .groups: "Groups"
                case .workspaces: "Workspaces"
                case .snippets: "Snippets"
                case .themes: "Themes"
                }
            }
            last = r.entry.section
            out.append((header, r.entry, r.match))
        }
        return out
    }

    fileprivate var selectedIndex: Int { selected }
}

/// The palette's rows, drawn in one view.
@MainActor
private final class PaletteList: NSView {
    weak var palette: Palette?
    private var rowRects: [NSRect] = []

    override var isFlipped: Bool { true }

    func rebuild() {
        guard let p = palette else { return }
        var y: CGFloat = 6
        rowRects = []
        for r in p.visibleRows {
            if r.header != nil { y += 26 }
            rowRects.append(NSRect(x: 6, y: y, width: frame.width - 12, height: 34))
            y += 34
        }
        if rowRects.isEmpty { y += 50 }
        frame.size.height = max(y + 6, enclosingScrollView?.contentSize.height ?? 0)
        updateTrackingAreas()
        needsDisplay = true
    }

    func scrollToSelected() {
        guard let p = palette, p.selectedIndex < rowRects.count else { return }
        scrollToVisible(rowRects[p.selectedIndex].insetBy(dx: 0, dy: -4))
    }

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        trackingAreas.forEach(removeTrackingArea)
        addTrackingArea(NSTrackingArea(rect: bounds, options: [.mouseMoved, .activeInKeyWindow, .inVisibleRect], owner: self))
    }

    override func mouseMoved(with event: NSEvent) {
        let pt = convert(event.locationInWindow, from: nil)
        if let i = rowRects.firstIndex(where: { $0.contains(pt) }) { palette?.select(i) }
    }

    override func mouseUp(with event: NSEvent) {
        let pt = convert(event.locationInWindow, from: nil)
        if let i = rowRects.firstIndex(where: { $0.contains(pt) }) { palette?.run(i) }
    }

    override func draw(_ dirty: NSRect) {
        guard let p = palette else { return }
        let rows = p.visibleRows
        if rows.isEmpty {
            let s = NSAttributedString(string: "No matches", attributes: [.font: NSFont.systemFont(ofSize: 13), .foregroundColor: NSColor.secondaryLabelColor])
            s.draw(at: NSPoint(x: (bounds.width - s.size().width) / 2, y: 18))
            return
        }
        for (i, r) in rows.enumerated() {
            let rect = rowRects[i]
            if let h = r.header {
                NSAttributedString(string: h, attributes: [.font: NSFont.systemFont(ofSize: 11, weight: .semibold), .foregroundColor: NSColor.secondaryLabelColor])
                    .draw(at: NSPoint(x: rect.minX + 10, y: rect.minY - 20))
            }
            let sel = i == p.selectedIndex
            if sel {
                NSColor.controlAccentColor.setFill()
                NSBezierPath(roundedRect: rect, xRadius: 8, yRadius: 8).fill()
            }
            let fg: NSColor = sel ? .white : .labelColor
            // Glyph
            let g = NSRect(x: rect.minX + 10, y: rect.midY - 8, width: 16, height: 16)
            if let t = r.entry.theme {
                let sw = NSRect(x: g.minX - 1, y: g.minY + 1, width: 18, height: 14)
                NSColor(hex: t.background).setFill()
                NSBezierPath(roundedRect: sw, xRadius: 4, yRadius: 4).fill()
                for (k, idx) in [1, 2, 4].enumerated() where idx < t.palette.count {
                    NSColor(hex: t.palette[idx]).setFill()
                    NSBezierPath(ovalIn: NSRect(x: sw.minX + 3 + CGFloat(k) * 4.5, y: sw.midY - 1.5, width: 3, height: 3)).fill()
                }
            } else if let dot = r.entry.dot {
                dot.setFill()
                NSBezierPath(ovalIn: NSRect(x: g.midX - 4.5, y: g.midY - 4.5, width: 9, height: 9)).fill()
            } else if let sym = r.entry.symbol, let img = NSImage(systemSymbolName: sym, accessibilityDescription: nil)?
                .withSymbolConfiguration(.init(pointSize: 12, weight: .regular)) {
                let tinted = img.tinted(sel ? .white : .secondaryLabelColor)
                tinted.draw(in: NSRect(x: g.midX - img.size.width / 2, y: g.midY - img.size.height / 2, width: img.size.width, height: img.size.height),
                            from: .zero, operation: .sourceOver, fraction: 1, respectFlipped: true, hints: nil)
            }
            // Label, matched characters marked.
            let label = NSMutableAttributedString(string: r.entry.label, attributes: [.font: NSFont.systemFont(ofSize: 13), .foregroundColor: fg])
            let chars = Array(r.entry.label)
            for idx in r.match?.indices ?? [] where idx < chars.count {
                let start = String(chars[..<idx]).utf16.count
                let range = NSRange(location: start, length: String(chars[idx]).utf16.count)
                label.addAttributes(sel ? [.underlineStyle: NSUnderlineStyle.single.rawValue]
                                        : [.foregroundColor: NSColor.controlAccentColor, .font: NSFont.systemFont(ofSize: 13, weight: .semibold)], range: range)
            }
            let hintWidth: CGFloat = r.entry.hint.isEmpty ? 0 : NSAttributedString(string: r.entry.hint, attributes: [.font: NSFont.systemFont(ofSize: 11)]).size().width + 16
            let labelMax = (rect.width - 46 - hintWidth) * 0.6
            let lsize = label.size()
            let lw = min(lsize.width, labelMax)
            label.draw(with: NSRect(x: rect.minX + 36, y: rect.midY - lsize.height / 2, width: lw, height: lsize.height),
                       options: [.usesLineFragmentOrigin, .truncatesLastVisibleLine])
            if !r.entry.detail.isEmpty {
                let d = NSAttributedString(string: r.entry.detail, attributes: [.font: NSFont.systemFont(ofSize: 12),
                                                                                .foregroundColor: sel ? NSColor.white.withAlphaComponent(0.75) : NSColor.secondaryLabelColor])
                let x = rect.minX + 36 + lw + 10
                d.draw(with: NSRect(x: x, y: rect.midY - d.size().height / 2, width: max(0, rect.maxX - hintWidth - 10 - x), height: d.size().height),
                       options: [.usesLineFragmentOrigin, .truncatesLastVisibleLine])
            }
            if !r.entry.hint.isEmpty {
                let h = NSAttributedString(string: r.entry.hint, attributes: [.font: NSFont.systemFont(ofSize: 11), .foregroundColor: sel ? NSColor.white : NSColor.secondaryLabelColor])
                let hs = h.size()
                let cap = NSRect(x: rect.maxX - hs.width - 18, y: rect.midY - 10, width: hs.width + 10, height: 20)
                (sel ? NSColor.white.withAlphaComponent(0.2) : NSColor.labelColor.withAlphaComponent(0.07)).setFill()
                NSBezierPath(roundedRect: cap, xRadius: 5, yRadius: 5).fill()
                h.draw(at: NSPoint(x: cap.minX + 5, y: cap.midY - hs.height / 2))
            }
        }
    }
}

extension NSImage {
    func tinted(_ c: NSColor) -> NSImage {
        let img = copy() as! NSImage
        img.isTemplate = false
        return NSImage(size: img.size, flipped: false) { r in
            img.draw(in: r)
            c.set()
            r.fill(using: .sourceAtop)
            return true
        }
    }
}

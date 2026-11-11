import AppKit
import WebKit
import WtfCore

/// The manual: the Markdown pages in manual/, rendered in
/// Swift, shown in a WKWebView with JavaScript off. Pages are listed and
/// searched natively on the left. Made on first open and let go on close
/// (a web view costs a process). Like internal/gtkui/manual.go.
@MainActor
final class ManualWindow: NSObject, NSWindowDelegate, WKNavigationDelegate, NSSearchFieldDelegate,
    NSTableViewDataSource, NSTableViewDelegate {
    private static var shared: ManualWindow?

    /// Opens the manual at a page (the context's help when nil).
    static func show(_ topic: String? = nil, from w: WindowController? = nil) {
        let id = topic ?? w.map { Manual.helpTopic($0.sidebar) } ?? "getting-started"
        let m = shared ?? ManualWindow()
        shared = m
        m.go(id, push: m.current != nil && m.current != id)
        m.window.makeKeyAndOrderFront(nil)
    }

    private let window: NSWindow
    private let web: WKWebView
    private let search = NSSearchField()
    private let table = NSTableView()
    private let back = NSButton()
    private let crumb = NSTextField(labelWithString: "")
    private var current: String?
    private var trail: [String] = []
    private var rows: [Row] = []
    private static var sources: [String: String] = [:]
    private var plain: [(Manual.Topic, String)]?
    private var appearance: NSKeyValueObservation?
    private var keys: Any?

    private enum Row { case group(String), topic(Manual.Topic) }

    private override init() {
        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 980, height: 720), styleMask: [.titled, .closable, .resizable, .miniaturizable],
                          backing: .buffered, defer: false)
        let cfg = WKWebViewConfiguration()
        cfg.defaultWebpagePreferences.allowsContentJavaScript = false
        web = WKWebView(frame: .zero, configuration: cfg)
        super.init()
        window.title = "wtf Manual"
        window.isReleasedWhenClosed = false
        window.delegate = self
        window.setFrameAutosaveName("wtfManual")
        window.minSize = NSSize(width: 640, height: 420)
        web.navigationDelegate = self
        web.setValue(false, forKey: "drawsBackground")

        search.placeholderString = "Search the manual"
        search.delegate = self
        search.sendsSearchStringImmediately = true
        let col = NSTableColumn(identifier: .init("t"))
        table.addTableColumn(col)
        table.headerView = nil
        table.style = .sourceList
        table.dataSource = self
        table.delegate = self
        table.target = self
        table.action = #selector(picked)
        let scroll = NSScrollView()
        scroll.documentView = table
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        let nav = NSStackView(views: [search, scroll])
        nav.orientation = .vertical
        nav.alignment = .leading
        nav.spacing = 8
        nav.edgeInsets = NSEdgeInsets(top: 10, left: 10, bottom: 0, right: 6)
        search.translatesAutoresizingMaskIntoConstraints = false
        scroll.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            search.widthAnchor.constraint(equalTo: nav.widthAnchor, constant: -16),
            scroll.widthAnchor.constraint(equalTo: nav.widthAnchor, constant: -10),
        ])
        let navBG = NSVisualEffectView()
        navBG.material = .sidebar
        navBG.addSubview(nav)
        nav.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            nav.leadingAnchor.constraint(equalTo: navBG.leadingAnchor), nav.trailingAnchor.constraint(equalTo: navBG.trailingAnchor),
            nav.topAnchor.constraint(equalTo: navBG.topAnchor), nav.bottomAnchor.constraint(equalTo: navBG.bottomAnchor),
            navBG.widthAnchor.constraint(equalToConstant: 230),
        ])

        back.image = NSImage(systemSymbolName: "chevron.left", accessibilityDescription: "Back")
        back.bezelStyle = .accessoryBarAction
        back.toolTip = "Back (⌘[)"
        back.target = self
        back.action = #selector(goBack)
        back.keyEquivalent = "["
        back.keyEquivalentModifierMask = .command
        crumb.textColor = .secondaryLabelColor
        crumb.font = .systemFont(ofSize: 11.5)
        let bar = NSStackView(views: [back, crumb])
        bar.edgeInsets = NSEdgeInsets(top: 6, left: 10, bottom: 6, right: 10)
        let right = NSStackView(views: [bar, web])
        right.orientation = .vertical
        right.spacing = 0
        right.alignment = .leading
        web.translatesAutoresizingMaskIntoConstraints = false
        bar.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            web.widthAnchor.constraint(equalTo: right.widthAnchor),
            bar.widthAnchor.constraint(equalTo: right.widthAnchor),
        ])
        let split = NSStackView(views: [navBG, right])
        split.spacing = 0
        split.alignment = .top
        navBG.translatesAutoresizingMaskIntoConstraints = false
        right.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            navBG.heightAnchor.constraint(equalTo: split.heightAnchor),
            right.heightAnchor.constraint(equalTo: split.heightAnchor),
        ])
        window.contentView = split
        window.center()
        window.initialFirstResponder = search
        appearance = NSApp.observe(\.effectiveAppearance) { _, _ in
            Task { @MainActor in ManualWindow.shared?.reload() }
        }
        refilter()
        // Anywhere in the window: Esc clears the search, then closes; ⌘F searches.
        keys = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] e in
            guard let self, e.window === self.window else { return e }
            if e.keyCode == 53 {
                if self.search.stringValue.isEmpty { self.window.performClose(nil) } else {
                    self.search.stringValue = ""
                    self.refilter()
                }
                return nil
            }
            if e.modifierFlags.intersection(.deviceIndependentFlagsMask) == .command, e.charactersIgnoringModifiers == "f" {
                self.window.makeFirstResponder(self.search)
                return nil
            }
            return e
        }
    }

    // MARK: Pages

    /// The pages bundled in the app, or the repo's while developing.
    static var folder: URL {
        if let u = Bundle.main.resourceURL?.appendingPathComponent("manual"), FileManager.default.fileExists(atPath: u.path) { return u }
        return URL(fileURLWithPath: #filePath).deletingLastPathComponent().appendingPathComponent("../../../manual").standardized
    }

    static func source(_ id: String) -> String {
        if let s = sources[id] { return s }
        let s = (try? String(contentsOf: folder.appendingPathComponent(id + ".md"), encoding: .utf8)) ?? "# \(id)\n\nThis page is missing from the app."
        sources[id] = s
        return s
    }

    static let context = Manual.Context(
        label: { id in Commands.all.first { $0.id == id }?.label },
        shortcut: { id in Commands.all.first { $0.id == id }.flatMap { $0.key.isEmpty ? nil : $0.key } })

    /// Every page's unknown command ids (debug self-check: should be none).
    static func missingCommands() -> [String: [String]] {
        var out: [String: [String]] = [:]
        for t in Manual.topics {
            let r = Manual.render(source(t.id), context)
            if !r.missing.isEmpty { out[t.id] = r.missing }
        }
        return out
    }

    private func go(_ id: String, push: Bool) {
        let id = Manual.topics.contains { $0.id == id } ? id : Manual.topics[0].id
        if push, let c = current, c != id { trail.append(c) }
        current = id
        reload()
        table.reloadData()
        if let i = rows.firstIndex(where: { if case .topic(let t) = $0 { t.id == id } else { false } }) {
            table.selectRowIndexes([i], byExtendingSelection: false)
            table.scrollRowToVisible(i)
        }
    }

    private func reload() {
        guard let id = current, let topic = Manual.topics.first(where: { $0.id == id }) else { return }
        var body = Manual.render(Self.source(id), Self.context).html
        if id == "shortcuts" {
            let rows = Commands.all.sorted { $0.label.localizedCompare($1.label) == .orderedAscending }.map { c in
                "<tr><td>\(Manual.escape(c.label))</td><td>" + (c.key.isEmpty ? "<span class=\"muted\">palette only</span>" : "<kbd>\(Manual.escape(c.key))</kbd>") + "</td></tr>"
            }.joined()
            body += "\n<table><thead><tr><th>Command</th><th>Shortcut</th></tr></thead><tbody>\(rows)</tbody></table>"
        }
        crumb.stringValue = topic.group
        back.isEnabled = !trail.isEmpty
        web.loadHTMLString(Self.page(body), baseURL: nil)
    }

    private static func css(_ c: NSColor) -> String {
        guard let s = c.usingColorSpace(.sRGB) else { return "#888" }
        return String(format: "rgba(%d,%d,%d,%.3f)", Int(s.redComponent * 255), Int(s.greenComponent * 255), Int(s.blueComponent * 255), s.alphaComponent)
    }

    static func page(_ body: String) -> String {
        var text = "", muted = "", accent = "", line = ""
        NSApp.effectiveAppearance.performAsCurrentDrawingAppearance {
            text = css(.labelColor)
            muted = css(.secondaryLabelColor)
            accent = css(.controlAccentColor)
            line = css(.separatorColor)
        }
        return """
        <!doctype html><html><head><meta charset="utf-8"><style>
        :root { color-scheme: light dark; --text: \(text); --muted: \(muted); --accent: \(accent); --line: \(line); }
        body { font: 13.5px/1.6 -apple-system, system-ui, sans-serif; color: var(--text); background: transparent;
               max-width: 680px; margin: 0; padding: 0 32px 40px; -webkit-user-select: text; }
        h1 { font-size: 22px; font-weight: 700; margin: 8px 0 12px; }
        h2 { font-size: 15.5px; font-weight: 600; margin: 22px 0 6px; }
        h3 { font-size: 13.5px; font-weight: 600; margin: 16px 0 4px; }
        code { font: 12px "JetBrains Mono", ui-monospace, Menlo, monospace; background: color-mix(in srgb, var(--text) 8%, transparent);
               padding: 1px 5px; border-radius: 5px; }
        pre { background: color-mix(in srgb, var(--text) 6%, transparent); padding: 10px 12px; border-radius: 8px; overflow-x: auto; }
        pre code { background: none; padding: 0; }
        kbd { font: 600 11.5px -apple-system, system-ui; border: 1px solid var(--line); border-bottom-width: 2px; border-radius: 5px;
              padding: 0 5px; white-space: nowrap; }
        aside { border-left: 3px solid var(--accent); background: color-mix(in srgb, var(--accent) 10%, transparent);
                padding: 8px 12px; border-radius: 0 8px 8px 0; margin: 12px 0; }
        table { width: 100%; border-collapse: collapse; margin: 10px 0; }
        th, td { text-align: left; padding: 5px 8px; border-bottom: 1px solid var(--line); vertical-align: top; }
        th { color: var(--muted); font-weight: 600; }
        a { color: var(--accent); text-decoration: none; } a:hover { text-decoration: underline; }
        .muted { color: var(--muted); }
        </style></head><body>
        \(body)
        </body></html>
        """
    }

    // MARK: Links

    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                 decisionHandler: @escaping @MainActor @Sendable (WKNavigationActionPolicy) -> Void) {
        guard let url = action.request.url, action.navigationType == .linkActivated else {
            decisionHandler(.allow)
            return
        }
        decisionHandler(.cancel)
        if url.scheme == "wtf-manual" {
            go(url.absoluteString.replacingOccurrences(of: "wtf-manual:", with: ""), push: true)
        } else if url.scheme == "https" {
            NSWorkspace.shared.open(url)
        }
    }

    @objc private func goBack() {
        guard let prev = trail.popLast() else { return }
        go(prev, push: false)
    }

    // MARK: Navigation list and search

    private func refilter() {
        let q = search.stringValue
        let shown: [Manual.Topic]
        if q.trimmingCharacters(in: .whitespaces).isEmpty {
            shown = Manual.topics
        } else {
            if plain == nil { plain = Manual.topics.map { ($0, Manual.plainText(Self.source($0.id))) } }
            shown = Manual.search(q, pages: plain!)
        }
        rows = []
        var group = ""
        for t in shown {
            if t.group != group {
                group = t.group
                rows.append(.group(group))
            }
            rows.append(.topic(t))
        }
        table.reloadData()
        if let c = current, let i = rows.firstIndex(where: { if case .topic(let t) = $0 { t.id == c } else { false } }) {
            table.selectRowIndexes([i], byExtendingSelection: false)
        }
    }

    func controlTextDidChange(_ obj: Notification) { refilter() }

    func control(_ control: NSControl, textView: NSTextView, doCommandBy sel: Selector) -> Bool {
        if sel == #selector(NSResponder.insertNewline(_:)) {
            if let first = rows.first(where: { if case .topic = $0 { true } else { false } }), case .topic(let t) = first { go(t.id, push: true) }
            return true
        }
        if sel == #selector(NSResponder.cancelOperation(_:)) {
            if search.stringValue.isEmpty { window.performClose(nil) } else {
                search.stringValue = ""
                refilter()
            }
            return true
        }
        return false
    }

    func numberOfRows(in tableView: NSTableView) -> Int {
        rows.isEmpty ? 1 : rows.count
    }

    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        if rows.isEmpty {
            let l = NSTextField(wrappingLabelWithString: "Nothing matches “\(search.stringValue)”.")
            l.textColor = .secondaryLabelColor
            return l
        }
        switch rows[row] {
        case .group(let g):
            let l = NSTextField(labelWithString: g.uppercased())
            l.font = .systemFont(ofSize: 10.5, weight: .bold)
            l.textColor = .secondaryLabelColor
            return l
        case .topic(let t):
            let l = NSTextField(labelWithString: t.title)
            l.font = .systemFont(ofSize: 12.5)
            l.lineBreakMode = .byTruncatingTail
            return l
        }
    }

    func tableView(_ tableView: NSTableView, heightOfRow row: Int) -> CGFloat {
        if rows.isEmpty { return 36 }
        if case .group = rows[row] { return 26 }
        return 22
    }

    func tableView(_ tableView: NSTableView, shouldSelectRow row: Int) -> Bool {
        guard !rows.isEmpty else { return false }
        if case .topic = rows[row] { return true }
        return false
    }

    @objc private func picked() {
        let i = table.clickedRow
        guard i >= 0, i < rows.count, case .topic(let t) = rows[i] else { return }
        go(t.id, push: true)
    }

    /// Renders the page shown to a PNG (debug self-checks).
    static func debugSnapshot(to path: String, done: @escaping () -> Void) {
        guard let m = shared else { return done() }
        m.web.takeSnapshot(with: nil) { image, _ in
            MainActor.assumeIsolated {
                if let image, let tiff = image.tiffRepresentation, let rep = NSBitmapImageRep(data: tiff) {
                    try? rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: path))
                }
                if let v = m.window.contentView, let rep = v.bitmapImageRepForCachingDisplay(in: v.bounds) {
                    v.cacheDisplay(in: v.bounds, to: rep)
                    try? rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: path.replacingOccurrences(of: ".png", with: "-nav.png")))
                }
                done()
            }
        }
    }

    // MARK: Closing

    func windowWillClose(_ notification: Notification) {
        // Let the web view (and its process) go until next time.
        appearance = nil
        if let keys { NSEvent.removeMonitor(keys) }
        keys = nil
        Self.shared = nil
        WindowManager.shared.keyWindow?.focusActive()
    }
}

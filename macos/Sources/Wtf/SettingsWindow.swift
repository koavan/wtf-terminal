import AppKit
import WtfCore

/// Settings (⌘,): one window for the app, with a section per toolbar
/// button. AI holds the providers — added, edited and tested here, written
/// to config.toml; General leads to the theme and config.toml for the rest.
/// Status reads the keychain, so it loads when the window opens, after an
/// action and when the config changes; never on a timer. Made on first
/// open and let go on close, like the manual.
@MainActor
final class SettingsWindow: NSObject, NSWindowDelegate, NSToolbarDelegate {
    enum Section: String, CaseIterable {
        case ai, general
        var title: String { self == .ai ? "AI" : "General" }
        var symbol: String { self == .ai ? "sparkles" : "gearshape" }
    }

    private static var shared: SettingsWindow?
    private static let width: CGFloat = 680

    /// Opens Settings (at a section), or brings it to the front.
    static func show(_ section: Section? = nil, from w: WindowController? = nil) {
        let s = shared ?? SettingsWindow()
        shared = s
        if let w { s.from = w }
        if let section, section != s.section { s.select(section) }
        s.window.makeKeyAndOrderFront(nil)
        s.load()
    }

    private struct Test {
        let request: AIRequest
        var text = ""
        var done: AIDone?
    }

    private let window: NSWindow
    private let scroll = NSScrollView()
    private let doc = FlippedView()
    private let content = NSStackView()
    /// The terminal window it was opened from: its focused shell's
    /// variables go with requests, so command providers are found on its PATH.
    private weak var from: WindowController?
    private var section = Section.ai
    private var status: Go.ai.Status?
    private var error: String?
    private var presets: [Go.ai.Preset]?
    private var tests: [String: Test] = [:]
    /// The streaming reply's label, updated in place as chunks arrive.
    private var replyLabels: [String: NSTextField] = [:]
    private var rowViews: [String: NSView] = [:]
    /// A provider to scroll to once the list shows it (just saved).
    private var reveal: String?
    private var seen: Config?
    private var keys: Any?

    private var inner: CGFloat { Self.width - 48 }
    private var shell: WindowController? { from ?? WindowManager.shared.keyWindow ?? WindowManager.shared.windows.first }

    private override init() {
        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: Self.width, height: 560),
                          styleMask: [.titled, .closable, .miniaturizable, .resizable], backing: .buffered, defer: false)
        super.init()
        window.title = "Settings"
        window.isReleasedWhenClosed = false
        window.delegate = self
        // As wide as the rows are laid out; as tall as you like.
        window.contentMinSize = NSSize(width: Self.width, height: 320)
        window.contentMaxSize = NSSize(width: Self.width, height: 4000)
        let bar = NSToolbar(identifier: "wtf.settings")
        bar.delegate = self
        bar.displayMode = .iconAndLabel
        window.toolbar = bar
        window.toolbarStyle = .preference
        bar.selectedItemIdentifier = .init(section.rawValue)

        content.orientation = .vertical
        content.alignment = .leading
        content.spacing = 10
        content.edgeInsets = NSEdgeInsets(top: 20, left: 24, bottom: 20, right: 24)
        content.translatesAutoresizingMaskIntoConstraints = false
        doc.addSubview(content)
        NSLayoutConstraint.activate([
            content.topAnchor.constraint(equalTo: doc.topAnchor),
            content.leadingAnchor.constraint(equalTo: doc.leadingAnchor),
            content.widthAnchor.constraint(equalToConstant: Self.width),
        ])
        scroll.documentView = doc
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        window.contentView = scroll
        window.center()
        // The menus' ⌘W closes a pane of the terminal window, not this one.
        keys = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] e in
            guard let self, e.window === self.window, e.modifierFlags.intersection(.deviceIndependentFlagsMask) == .command,
                  e.charactersIgnoringModifiers == "w" else { return e }
            self.window.performClose(nil)
            return nil
        }
        seen = Settings.shared.config
        // Providers saved here (or by hand) come back as config:changed.
        Settings.shared.observe(self) { s in
            guard Settings.shared.config != s.seen else { return }
            s.seen = Settings.shared.config
            s.section == .ai ? s.load() : s.render()
        }
        render()
    }

    func windowWillClose(_ notification: Notification) {
        for t in tests.values { t.request.cancel() }
        if let keys { NSEvent.removeMonitor(keys) }
        keys = nil
        Self.shared = nil
        WindowManager.shared.keyWindow?.focusActive()
    }

    // MARK: Toolbar

    func toolbarAllowedItemIdentifiers(_ toolbar: NSToolbar) -> [NSToolbarItem.Identifier] { Section.allCases.map { .init($0.rawValue) } }
    func toolbarDefaultItemIdentifiers(_ toolbar: NSToolbar) -> [NSToolbarItem.Identifier] { toolbarAllowedItemIdentifiers(toolbar) }
    func toolbarSelectableItemIdentifiers(_ toolbar: NSToolbar) -> [NSToolbarItem.Identifier] { toolbarAllowedItemIdentifiers(toolbar) }

    func toolbar(_ toolbar: NSToolbar, itemForItemIdentifier id: NSToolbarItem.Identifier, willBeInsertedIntoToolbar flag: Bool) -> NSToolbarItem? {
        guard let s = Section(rawValue: id.rawValue) else { return nil }
        let item = NSToolbarItem(itemIdentifier: id)
        item.label = s.title
        item.image = NSImage(systemSymbolName: s.symbol, accessibilityDescription: s.title)
        item.target = self
        item.action = #selector(pick(_:))
        return item
    }

    @objc private func pick(_ item: NSToolbarItem) {
        guard let s = Section(rawValue: item.itemIdentifier.rawValue), s != section else { return }
        select(s)
        if s == .ai { load() }
    }

    private func select(_ s: Section) {
        section = s
        window.toolbar?.selectedItemIdentifier = .init(s.rawValue)
        render()
    }

    // MARK: Rendering

    private func load() {
        guard section == .ai else { return }
        Task {
            do {
                status = try await Services.ai.Status()
                error = nil
            } catch {
                self.error = error.localizedDescription
            }
            render()
        }
    }

    private func render() {
        content.arrangedSubviews.forEach { $0.removeFromSuperview() }
        replyLabels = [:]
        rowViews = [:]
        switch section {
        case .ai: renderAI()
        case .general: renderGeneral()
        }
        content.layoutSubtreeIfNeeded()
        doc.frame = NSRect(x: 0, y: 0, width: Self.width, height: content.fittingSize.height)
        if let name = reveal, let v = rowViews[name] {
            reveal = nil
            v.scrollToVisible(v.bounds)
        }
    }

    private func label(_ s: String, size: CGFloat = 12.5, weight: NSFont.Weight = .regular, color: NSColor = .labelColor, mono: Bool = false) -> NSTextField {
        let l = NSTextField(wrappingLabelWithString: s)
        l.font = mono ? .monospacedSystemFont(ofSize: size - 1, weight: weight) : .systemFont(ofSize: size, weight: weight)
        l.textColor = color
        l.preferredMaxLayoutWidth = inner
        l.isSelectable = mono
        return l
    }

    private func heading(_ t: String, _ sub: String) {
        content.addArrangedSubview(label(t, size: 15, weight: .semibold))
        content.addArrangedSubview(label(sub, size: 12, color: .secondaryLabelColor))
    }

    private func bar(_ views: [NSView]) {
        let row = NSStackView(views: views)
        row.translatesAutoresizingMaskIntoConstraints = false
        row.widthAnchor.constraint(equalToConstant: inner).isActive = true
        content.addArrangedSubview(row)
    }

    private func spacer() -> NSView {
        let v = NSView()
        v.setContentHuggingPriority(.init(1), for: .horizontal)
        return v
    }

    private func separator() {
        let line = NSBox()
        line.boxType = .separator
        content.addArrangedSubview(line)
        line.widthAnchor.constraint(equalToConstant: inner).isActive = true
    }

    private func openConfig() {
        Task {
            do { try await Services.config.OpenConfigFile() } catch {
                Toast.show("Couldn't open config.toml: \(error.localizedDescription)", in: window.contentView)
            }
        }
    }

    // MARK: General

    private func renderGeneral() {
        heading("General", "Most settings are in config.toml for now: fonts, the shell, scrollback, Hide Secrets at startup and the rest, each documented there. Changes apply when you save the file.")
        let theme = Settings.shared.config?.theme ?? ""
        let name = NSTextField(labelWithString: "Theme")
        name.font = .systemFont(ofSize: 12.5, weight: .semibold)
        let current = NSTextField(labelWithString: theme.isEmpty || theme.lowercased() == "auto" ? "Auto (follows the system)" : theme)
        current.textColor = .secondaryLabelColor
        let change = ClosureButton(title: "Change Theme…") { [weak self] in
            guard let w = self?.shell else { return }
            w.window?.makeKeyAndOrderFront(nil)
            Palette.toggle(.themes, in: w)
        }
        change.toolTip = "Pick a theme in the palette, previewed as you move"
        bar([name, current, spacer(), change])
        separator()
        let file = ClosureButton(title: "Open config.toml") { [weak self] in self?.openConfig() }
        let themes = ClosureButton(title: "Open Themes Folder") { [weak self] in
            Task {
                do { try await Services.config.OpenThemesFolder() } catch {
                    Toast.show("Couldn't open folder: \(error.localizedDescription)", in: self?.window.contentView)
                }
            }
        }
        themes.toolTip = "Add your own themes as files here"
        bar([file, themes, spacer()])
    }

    // MARK: AI

    private func renderAI() {
        heading("AI", "The AI tools wtf’s AI features ask: the Claude API, any OpenAI-compatible endpoint, or a command-line tool. Secrets are hidden before anything is sent, and the first request to each provider shows exactly what will be sent.")
        let add = ClosureButton(title: "Add Provider…") { [weak self] in self?.addProvider() }
        add.bezelColor = .controlAccentColor
        let help = ClosureButton(title: "Help") { ManualWindow.show("ai") }
        bar([add, spacer(), help])
        if let status, status.providers.isEmpty {
            content.addArrangedSubview(label("No AI provider is set up yet, so AI features are hidden. Add one: a cloud API with your key, a model server on this computer (Ollama, LM Studio), or a command-line tool you already use (claude -p, codex exec, gemini, llm).", color: .secondaryLabelColor))
        } else if let status {
            for p in status.providers {
                separator()
                let r = row(p)
                rowViews[p.name] = r
                content.addArrangedSubview(r)
            }
        } else if error == nil {
            content.addArrangedSubview(label("Loading…", color: .secondaryLabelColor))
        }
        if let error { content.addArrangedSubview(label(error, color: .systemRed)) }
        separator()
        let file = ClosureButton(title: "Open config.toml") { [weak self] in self?.openConfig() }
        file.controlSize = .small
        bar([label("Providers are saved in config.toml as [ai.providers.<name>]; editing it by hand works too.", size: 11.5, color: .secondaryLabelColor), spacer(), file])
    }

    private static func keyState(_ p: Go.ai.ProviderInfo) -> (String, NSColor) {
        if p.kind == "command" { return ("Uses the tool’s own login", .secondaryLabelColor) }
        if p.keyFrom.hasPrefix("$") { return p.hasKey ? ("Key from \(p.keyFrom)", .secondaryLabelColor) : ("\(p.keyFrom) is empty", .systemOrange) }
        if p.hasKey { return ("Key in keychain", .secondaryLabelColor) }
        if !p.needsKey { return ("No key needed", .secondaryLabelColor) }
        return ("Needs an API key", .systemOrange)
    }

    private func row(_ p: Go.ai.ProviderInfo) -> NSView {
        func small(_ title: String, tip: String, _ run: @escaping () -> Void) -> NSButton {
            let b = ClosureButton(title: title, run)
            b.controlSize = .small
            b.toolTip = tip
            return b
        }
        let name = NSTextField(labelWithString: p.name)
        name.font = .systemFont(ofSize: 13, weight: .bold)
        var head: [NSView] = [name]
        func tag(_ s: String, tip: String) {
            let t = NSTextField(labelWithString: s)
            t.font = .systemFont(ofSize: 10.5, weight: .semibold)
            t.textColor = .controlAccentColor
            t.toolTip = tip
            head.append(t)
        }
        if p.default { tag("DEFAULT", tip: "ai-default: most AI features ask this one") }
        if p.fast { tag("QUICK", tip: "ai-fast: quick inline features ask this one") }
        head.append(spacer())
        head.append(small("Edit…", tip: "Change \(p.name)’s kind, model, URL, key or command") { [weak self] in self?.editProvider(p) })
        head.append(small("Delete…", tip: "Remove \(p.name) from config.toml") { [weak self] in self?.deleteProvider(p) })
        let headRow = NSStackView(views: head)
        headRow.spacing = 8
        headRow.translatesAutoresizingMaskIntoConstraints = false
        headRow.widthAnchor.constraint(equalToConstant: inner).isActive = true

        var lines: [NSView] = [headRow]
        func muted(_ s: String, mono: Bool = false, color: NSColor = .secondaryLabelColor) {
            let l = label(s, size: mono ? 12 : 11.5, color: color, mono: mono)
            if mono { l.textColor = .secondaryLabelColor }
            lines.append(l)
        }
        muted([p.kind, p.model].filter { !$0.isEmpty }.joined(separator: " · ") + (p.local ? " · on this machine" : ""))
        muted(p.destination, mono: true)
        let (key, keyColor) = Self.keyState(p)
        muted(key, color: keyColor)
        let keyLine = lines[lines.count - 1]

        var acts: [NSView] = []
        let testing = tests[p.name] != nil && tests[p.name]?.done == nil
        if testing {
            acts.append(small("Stop", tip: "Stop the test request") { [weak self] in self?.tests[p.name]?.request.cancel() })
        } else {
            acts.append(small("Test", tip: "Send a tiny request to check that \(p.name) answers") { [weak self] in self?.test(p) })
        }
        acts.append(small("What’s Sent", tip: "Show exactly what the test request sends, without sending it") { [weak self] in
            guard let self else { return }
            AIRequest.showPreview(testRequest(p), in: window)
        })
        if p.kind != "command", !p.keyFrom.hasPrefix("$") {
            acts.append(small(p.hasKey ? "Change Key…" : "Set Key…", tip: "Store \(p.name)’s API key in the keychain") { [weak self] in self?.setKey(p) })
        }
        if p.keyFrom == "keychain" {
            acts.append(small("Remove Key", tip: "Delete \(p.name)’s API key from the keychain") { [weak self] in self?.removeKey(p) })
        }
        if !p.default {
            acts.append(small("Use as Default", tip: "Set ai-default in config.toml: most AI features ask this one") { [weak self] in self?.use(p, fast: false) })
        }
        if !p.fast {
            acts.append(small("Use for Quick Features", tip: "Set ai-fast in config.toml: quick inline features ask this one") { [weak self] in self?.use(p, fast: true) })
        }
        let actRow = NSStackView(views: acts)
        actRow.spacing = 6
        lines.append(actRow)
        if let t = tests[p.name] { lines += testViews(p.name, t) }

        let col = NSStackView(views: lines)
        col.orientation = .vertical
        col.alignment = .leading
        col.spacing = 3
        col.setCustomSpacing(8, after: keyLine)
        col.setCustomSpacing(6, after: headRow)
        return col
    }

    /// The test's reply as it streams, then its time and usage, or the error.
    private func testViews(_ name: String, _ t: Test) -> [NSView] {
        let reply = label(t.text, size: 12.5, mono: true)
        reply.maximumNumberOfLines = 6
        reply.isHidden = t.text.isEmpty
        guard let d = t.done else {
            replyLabels[name] = reply
            // Spins only while the request runs.
            let spin = NSProgressIndicator()
            spin.style = .spinning
            spin.controlSize = .small
            spin.startAnimation(nil)
            let waiting = NSStackView(views: [spin, label("Waiting for \(name)…", size: 11.5, color: .secondaryLabelColor)])
            waiting.spacing = 6
            return [waiting, reply]
        }
        var views: [NSView] = t.text.isEmpty ? [] : [reply]
        if d.cancelled {
            views.append(label("Stopped", size: 11.5, color: .secondaryLabelColor))
        } else if !d.error.isEmpty {
            views.append(label(d.error, size: 11.5, color: .systemRed))
        } else {
            views.append(label((t.text.isEmpty ? "Answered with no text" : "Answered") + " · " + d.summary, size: 11.5, color: .systemGreen))
        }
        return views
    }

    private func testRequest(_ p: Go.ai.ProviderInfo) -> Go.ai.Request {
        Go.ai.Request(provider: p.name, system: "You check that a connection works.",
                      messages: [Go.ai.Message(role: "user", content: "Reply with the single word OK.")],
                      maxTokens: 20, env: shell?.activePane?.env ?? [:])
    }

    private func test(_ p: Go.ai.ProviderInfo) {
        let req = AIRequest(testRequest(p))
        req.onChunk = { [weak self] text in
            guard let self, var t = tests[p.name], t.request === req else { return }
            t.text += text
            tests[p.name] = t
            if let l = replyLabels[p.name] {
                l.stringValue = t.text
                l.isHidden = false
            } else {
                render()
            }
        }
        req.onDone = { [weak self] d in
            guard let self, var t = tests[p.name], t.request === req else { return }
            t.done = d
            if !d.text.isEmpty { t.text = d.text }
            tests[p.name] = t
            render()
        }
        tests[p.name] = Test(request: req)
        render()
        req.start(in: window)
    }

    // MARK: Actions

    private func addProvider() {
        Task {
            if presets == nil {
                do { presets = try await Services.ai.Presets() } catch {
                    self.error = error.localizedDescription
                    return render()
                }
            }
            ProviderForm.show(old: nil, info: nil, presets: presets ?? [], existing: status?.providers.map(\.name) ?? [], in: window) { name in
                self.saved(name)
            }
        }
    }

    private func editProvider(_ p: Go.ai.ProviderInfo) {
        ProviderForm.show(old: p.name, info: p, presets: [], existing: status?.providers.map(\.name) ?? [], in: window) { [weak self] name in
            self?.saved(name)
        }
    }

    private func saved(_ name: String) {
        reveal = name
        load()
    }

    private func deleteProvider(_ p: Go.ai.ProviderInfo) {
        var message = "Its [ai.providers.\(p.name)] table is removed from config.toml"
        message += p.kind == "command" ? "." : ", and its API key is removed from the keychain."
        if p.default { message += " The first remaining provider becomes the default." }
        Confirm.show(in: window, title: "Delete \(p.name)?", message: message, button: "Delete") { [weak self] in
            Task {
                do {
                    try await Services.config.DeleteAIProvider(p.name)
                    if p.kind != "command" { try await Services.ai.DeleteKey(p.name) }
                } catch {
                    self?.error = error.localizedDescription
                }
                self?.tests[p.name] = nil
                self?.load()
            }
        }
    }

    private func setKey(_ p: Go.ai.ProviderInfo) {
        let f = FormSheet(title: "API Key for \(p.name)", subtitle: "Stored in the keychain under “wtf AI”, never in config.toml. Requests to \(p.destination) send it.")
        let field = NSSecureTextField()
        field.placeholderString = p.kind == "anthropic" ? "sk-ant-…" : "API key"
        f.field("Key", field)
        f.buttons([("Save", true, { [weak self, weak f] in
            guard let f else { return }
            f.button("Save")?.isEnabled = false
            let key = field.stringValue
            Task {
                do {
                    try await Services.ai.SetKey(p.name, key)
                    f.close()
                    self?.load()
                } catch {
                    f.error = error.localizedDescription
                    f.button("Save")?.isEnabled = true
                }
            }
        })])
        f.present(in: window, focus: field)
    }

    private func removeKey(_ p: Go.ai.ProviderInfo) {
        Confirm.show(in: window, title: "Remove \(p.name)’s API key?",
                     message: "It’s deleted from the keychain. \(p.name) can’t be used until a key is set again.", button: "Remove Key") { [weak self] in
            Task {
                do {
                    try await Services.ai.DeleteKey(p.name)
                } catch {
                    self?.error = error.localizedDescription
                }
                self?.load()
            }
        }
    }

    /// Writes ai-default or ai-fast; config:changed then reloads the list.
    private func use(_ p: Go.ai.ProviderInfo, fast: Bool) {
        Task {
            do {
                try await Services.config.SetAIProvider(fast, p.name)
            } catch {
                self.error = error.localizedDescription
                render()
            }
        }
    }
}

// MARK: - Provider form

/// Add Provider (from a preset) and Edit: one [ai.providers.<name>] table
/// as a form. The key goes to the keychain, never config.toml; leaving it
/// empty keeps the saved one.
@MainActor
final class ProviderForm {
    private static let kinds = [("anthropic", "Claude API (anthropic)"), ("openai", "OpenAI-compatible (openai)"), ("command", "Command-line tool (command)")]
    private static var open: [ProviderForm] = []

    /// saved(name) runs once it's written; the form then closes.
    static func show(old: String?, info: Go.ai.ProviderInfo?, presets: [Go.ai.Preset], existing: [String], in window: NSWindow, saved: @escaping (String) -> Void) {
        let f = ProviderForm(old: old ?? "", info: info, presets: presets, existing: existing, saved: saved)
        open.append(f)
        f.form.onClose = { open.removeAll { $0 === f } }
        f.form.present(in: window, focus: f.name)
        f.fit()
    }

    private let form: FormSheet
    /// The saved name; "" until the first save of a new one.
    private var old: String
    private let info: Go.ai.ProviderInfo?
    private let presets: [Go.ai.Preset]
    private let existing: [String]
    private let saved: (String) -> Void

    private let name = NSTextField()
    private let preset = NSPopUpButton()
    private let kind = NSPopUpButton()
    private let baseURL = NSTextField()
    private let model = NSComboBox()
    private let fetching = NSProgressIndicator()
    private let fetchNote = NSTextField(wrappingLabelWithString: "")
    private let key = NSSecureTextField()
    private let envToggle = NSButton(checkboxWithTitle: "Read the key from an environment variable instead", target: nil, action: nil)
    private let keyEnv = NSTextField()
    private let headersToggle = NSButton(checkboxWithTitle: "Send extra headers", target: nil, action: nil)
    private let headersScroll = NSTextView.scrollableTextView()
    private var headers: NSTextView { headersScroll.documentView as! NSTextView }
    private var headersRow: NSView?
    private let command = NSTextField()
    private var apiRows: [NSView] = []
    private var envRow: NSView?
    private var commandRow: NSView?
    private var watchers: [PopupWatcher] = []
    private var busy = false

    private init(old: String, info: Go.ai.ProviderInfo?, presets: [Go.ai.Preset], existing: [String], saved: @escaping (String) -> Void) {
        self.old = old
        self.info = info
        self.presets = presets
        self.existing = existing
        self.saved = saved
        form = FormSheet(title: old.isEmpty ? "Add AI Provider" : "Edit \(old)",
                         subtitle: old.isEmpty ? nil : "Saved in config.toml as [ai.providers.\(old)].", width: 560)
        build()
        if old.isEmpty {
            if let i = presets.indices.first { choose(i) }
        } else if let p = Settings.shared.config?.ai?.providers[old] {
            fill(name: old, p)
        }
        applyKind()
    }

    private func watch(_ control: NSControl, _ run: @escaping () -> Void) {
        let w = PopupWatcher(run)
        watchers.append(w)
        control.target = w
        control.action = #selector(PopupWatcher.changed)
    }

    private func build() {
        if old.isEmpty, !presets.isEmpty {
            // Grouped: Cloud API, On this computer, Command-line tool, Other.
            let menu = NSMenu()
            var group = ""
            for (i, p) in presets.enumerated() {
                if p.group != group {
                    group = p.group
                    menu.addItem(.sectionHeader(title: group))
                }
                let item = NSMenuItem(title: p.label, action: nil, keyEquivalent: "")
                item.tag = i
                menu.addItem(item)
            }
            preset.menu = menu
            watch(preset) { [weak self] in
                guard let self, let i = preset.selectedItem?.tag else { return }
                choose(i)
                applyKind()
            }
            form.field("Start from", preset)
        }
        name.placeholderString = "e.g. claude"
        form.field("Name", name, help: "What the palette and config.toml call it: letters, digits, - and _.")
        kind.addItems(withTitles: Self.kinds.map(\.1))
        watch(kind) { [weak self] in self?.applyKind() }
        form.field("Kind", kind)

        apiRows.append(form.field("Base URL", baseURL, help: "Leave empty for the vendor’s own API."))
        model.completes = true
        model.numberOfVisibleItems = 12
        let fetchButton = ClosureButton(title: "Fetch Models") { [weak self] in self?.fetchModels() }
        fetchButton.toolTip = "Ask the provider which models it has (uses the key below, or the saved one)"
        fetching.style = .spinning
        fetching.controlSize = .small
        fetching.isDisplayedWhenStopped = false
        let modelLine = NSStackView(views: [model, fetching, fetchButton])
        modelLine.spacing = 6
        model.setContentHuggingPriority(.init(1), for: .horizontal)
        apiRows.append(form.field("Model", modelLine))
        fetchNote.font = .systemFont(ofSize: 11)
        fetchNote.textColor = .secondaryLabelColor
        fetchNote.preferredMaxLayoutWidth = 512
        fetchNote.isHidden = true
        form.add(fetchNote)
        apiRows.append(fetchNote)
        fetchButtonRef = fetchButton

        if info?.keyFrom == "keychain" {
            key.placeholderString = "Saved in the Keychain: leave empty to keep it"
        } else {
            key.placeholderString = "Paste the key (not needed for a server on this computer)"
        }
        apiRows.append(form.field("API key", key, help: "Kept in the keychain, never in config.toml."))
        watch(envToggle) { [weak self] in self?.applyKind() }
        form.add(envToggle)
        apiRows.append(envToggle)
        keyEnv.placeholderString = "e.g. OPENAI_API_KEY"
        envRow = form.field("Environment variable", keyEnv, help: "wtf reads the key from this variable (as wtf was started) instead of the keychain.")
        watch(headersToggle) { [weak self] in self?.applyKind() }
        headersToggle.toolTip = "For gateways and keys that need more, e.g. Anthropic’s anthropic-workspace-id"
        form.add(headersToggle)
        apiRows.append(headersToggle)
        headersScroll.borderType = .bezelBorder
        headersScroll.heightAnchor.constraint(equalToConstant: 56).isActive = true
        headers.font = .monospacedSystemFont(ofSize: 12, weight: .regular)
        headers.isRichText = false
        headers.isAutomaticQuoteSubstitutionEnabled = false
        headers.isAutomaticDashSubstitutionEnabled = false
        headers.isAutomaticTextReplacementEnabled = false
        headers.textContainerInset = NSSize(width: 4, height: 4)
        headersRow = form.field("Extra headers", headersScroll, help: "One per line, as Name: value — e.g. anthropic-workspace-id: wrkspc_… Not for keys.")

        command.placeholderString = "e.g. claude -p"
        command.font = .monospacedSystemFont(ofSize: 12, weight: .regular)
        commandRow = form.field("Command", command, help: "wtf runs it in an empty folder with the prompt on stdin; the reply is what it prints. It’s found on the focused shell’s PATH. Quote arguments with spaces as in a shell.")

        form.buttons([("Save", true, { [weak self] in self?.save() })])
    }

    private var fetchButtonRef: NSButton?
    private var fetchingModels = false

    private var kindID: String { Self.kinds[max(0, kind.indexOfSelectedItem)].0 }

    /// Fills the form from a preset.
    private func choose(_ i: Int) {
        guard presets.indices.contains(i) else { return }
        let p = presets[i]
        preset.selectItem(withTag: i)
        var n = p.name
        // "ollama" taken: "ollama-2".
        if !n.isEmpty, existing.contains(n) {
            var k = 2
            while existing.contains("\(n)-\(k)") { k += 1 }
            n = "\(n)-\(k)"
        }
        fill(name: n, Go.config.AIProvider(kind: p.kind, model: p.model, baseUrl: p.baseUrl, command: p.command))
        form.setSubtitle(p.help)
        fit()
    }

    private func fill(name n: String, _ p: Go.config.AIProvider) {
        name.stringValue = n
        kind.selectItem(at: Self.kinds.firstIndex { $0.0 == p.kind } ?? 0)
        baseURL.stringValue = p.baseUrl
        model.removeAllItems()
        model.stringValue = p.model
        keyEnv.stringValue = p.apiKeyEnv
        envToggle.state = p.apiKeyEnv.isEmpty ? .off : .on
        headers.string = ""
        headersToggle.state = p.headers.isEmpty ? .off : .on
        if !p.headers.isEmpty {
            Task { headers.string = (try? await Services.config.FormatHeaders(p.headers)) ?? "" }
        }
        fetchNote.isHidden = true
        command.stringValue = ""
        if !p.command.isEmpty {
            Task { command.stringValue = (try? await Services.config.JoinCommand(p.command)) ?? p.command.joined(separator: " ") }
        }
    }

    /// Shows the fields the kind uses.
    private func applyKind() {
        let k = kindID
        let api = k != "command"
        for v in apiRows where v !== fetchNote { v.isHidden = !api }
        if !api { fetchNote.isHidden = true }
        envRow?.isHidden = !api || envToggle.state != .on
        headersRow?.isHidden = !api || headersToggle.state != .on
        commandRow?.isHidden = api
        baseURL.placeholderString = k == "anthropic" ? "https://api.anthropic.com" : "https://api.openai.com/v1"
        fit()
    }

    /// The sheet's height follows the fields shown.
    private func fit() {
        guard let v = form.panel.contentView else { return }
        v.layoutSubtreeIfNeeded()
        form.panel.setContentSize(NSSize(width: 560, height: v.fittingSize.height))
    }

    private func trimmed(_ f: NSTextField) -> String { f.stringValue.trimmingCharacters(in: .whitespacesAndNewlines) }

    /// The extra headers' text: none while the box is unticked.
    private var headersText: String { headersToggle.state == .on ? headers.string : "" }

    /// The provider as the form says (the command and headers parsed separately).
    private var provider: Go.config.AIProvider {
        Go.config.AIProvider(kind: kindID, model: trimmed(model), baseUrl: trimmed(baseURL),
                             apiKeyEnv: envToggle.state == .on ? trimmed(keyEnv) : "")
    }

    private func fetchModels() {
        guard !fetchingModels else { return }
        fetchingModels = true
        var p = provider
        let text = headersText
        let typed = trimmed(key)
        // A saved key lives under the saved name.
        let lookup = old.isEmpty ? trimmed(name) : old
        fetchButtonRef?.isEnabled = false
        fetching.startAnimation(nil)
        fetchNote.isHidden = true
        Task {
            do {
                p.headers = try await Services.config.ParseHeaders(text)
                let list = try await Services.ai.Models(lookup, p, typed)
                model.removeAllItems()
                model.addItems(withObjectValues: list)
                fetchNote.textColor = .secondaryLabelColor
                fetchNote.stringValue = list.isEmpty ? "The provider listed no models." : "\(list.count) \(list.count == 1 ? "model" : "models"): pick one from the Model list."
            } catch {
                fetchNote.textColor = .systemRed
                fetchNote.stringValue = error.localizedDescription
            }
            fetching.stopAnimation(nil)
            fetchingModels = false
            fetchButtonRef?.isEnabled = true
            fetchNote.isHidden = false
            fit()
        }
    }

    private func save() {
        guard !busy else { return }
        busy = true
        form.button("Save")?.isEnabled = false
        form.error = nil
        let n = trimmed(name)
        var p = provider
        let api = p.kind != "command"
        let typedKey = trimmed(key)
        let line = command.stringValue
        let text = headersText
        Task {
            do {
                if api {
                    p.command = []
                    p.headers = try await Services.config.ParseHeaders(text)
                } else {
                    p = Go.config.AIProvider(kind: "command", command: try await Services.config.SplitCommand(line))
                }
                try await Services.config.SaveAIProvider(old, n, p)
                let was = old
                // Saved: a retry after a key error edits it rather than adding it twice.
                old = n
                if !was.isEmpty, was != n { try await Services.ai.MoveKey(was, n) }
                if api, !typedKey.isEmpty { try await Services.ai.SetKey(n, typedKey) }
                form.close()
                saved(n)
            } catch {
                form.error = error.localizedDescription
                form.button("Save")?.isEnabled = true
                busy = false
                fit()
            }
        }
    }
}

import AppKit
import SwiftTerm
import WtfCore

/// One terminal view attached to one backend shell session: its look
/// (config.toml), command blocks (OSC 133), secret covers, find bar and
/// menus. Like TerminalPane.svelte.
@MainActor
final class TerminalController: NSObject, @MainActor TerminalViewDelegate, OutputSink, NSMenuDelegate {
    let view: WtfTerminalView
    let container: TerminalContainer
    private var overlay: TerminalOverlay { container.overlay }
    /// The window (backend name) this pane is in; changes when its tab moves.
    var windowName: String
    /// The pane it shows; changes when its tab moves to another window.
    var paneId: Int
    /// The backend session, once started.
    private(set) var id: String?
    private var tracker: PromptTracker!
    private var searchBar: SearchBar?
    /// What runs in the shell now, from terminal:info.
    var info: TerminalInfo?
    private var closed = false

    // What the window learns from this pane's shell.
    var onPrompt: (() -> Void)?
    var onCommandStart: ((_ text: String, _ block: Int) -> Void)?
    var onCommandEnd: ((_ exitCode: Int, _ durationMs: Double) -> Void)?
    var onEnv: ((String) -> Void)?
    /// Output arrived (for the background-tab dot).
    var onOutput: (() -> Void)?
    /// The terminal took keyboard focus (a click into the pane).
    var onFocus: (() -> Void)?
    /// Broadcast input: what's typed here, for the other panes.
    var fanOut: ((ArraySlice<UInt8>) -> Void)?
    /// Panes a paste goes to (1 unless broadcasting).
    var broadcastCount: (() -> Int)?

    init(pane: Int, window: String, frame: NSRect = NSRect(x: 0, y: 0, width: 800, height: 500)) {
        paneId = pane
        windowName = window
        view = WtfTerminalView(frame: frame, font: NSFont.monospacedSystemFont(ofSize: 13, weight: .regular))
        container = TerminalContainer(terminal: view)
        super.init()
        view.terminalDelegate = self
        view.controller = self
        // Links open with ⌘-click, as in the Wails build.
        view.linkReporting = .implicit
        // CoreText beat Metal in the M0 benchmark; Metal stays opt-in.
        if ProcessInfo.processInfo.environment["WTF_NATIVE_RENDERER"] == "metal" {
            do { try view.setUseMetal(true) } catch { NSLog("metal unavailable: \(error)") }
        }

        tracker = PromptTracker(terminal: view.getTerminal())
        tracker.onChange = { [weak self] in self?.refreshOverlay() }
        tracker.onPrompt = { [weak self] in self?.onPrompt?() }
        tracker.onCommandStart = { [weak self] text, block in
            Debug.log("command: \(Redaction.shared.text(text))")
            self?.onCommandStart?(text, block)
        }
        tracker.onCommandEnd = { [weak self] code, ms in
            Debug.log("command done: exit \(code) in \(Format.duration(ms: ms))")
            self?.onCommandEnd?(code, ms)
        }
        tracker.onEnv = { [weak self] in self?.onEnv?($0) }
        view.confirmPaste = { [weak self] in self?.confirmPaste($0) }
        view.contextMenu = { [weak self] in self?.contextMenu(for: $0) }
        view.onFocus = { [weak self] in self?.onFocus?() }
        view.onCommandClick = { [weak self] in self?.commandClick($0) ?? false }
        overlay.onMarkClick = { [weak self] id, event in self?.showBlockMenu(id, event) }

        Settings.shared.observe(self) { $0.applySettings() }
        Redaction.shared.observe(self) { $0.refreshOverlay() }
        applySettings()
    }

    /// Starts a shell in a folder ("" for home), then types the git identity
    /// switch and the first command, if any.
    func start(cwd: String, identity: String?, command: String?) async throws -> String {
        let term = view.getTerminal()
        let id = try await Backend.call("terminal", "Create", [windowName, term.cols, term.rows, cwd], as: String.self)
        self.id = id
        if closed {
            close()
            return id
        }
        try Backend.attach(id, to: self)
        // The tty holds these until the shell reads them.
        if let identity { type(await Self.identityCommand(identity, in: view) + "\r") }
        if let command, !command.isEmpty { type(command + "\r") }
        return id
    }

    /// The command that switches a shell to a saved git identity; empty when
    /// it no longer exists.
    private static func identityCommand(_ name: String, in view: NSView) async -> String {
        let list = (try? await Backend.call("sshhosts", "GitIdentities", as: [Shell.GitIdentity].self)) ?? []
        if let i = list.first(where: { $0.name == name }) { return Shell.useIdentity(i) }
        Toast.show("Git identity “\(name)” no longer exists: this shell uses your own git and ssh config", in: view)
        return ""
    }

    /// The tab moved to another window: the shell now belongs to it.
    func moved(to window: String) {
        windowName = window
        if let id { Task { try? await Backend.run("terminal", "Adopt", [id, window]) } }
    }

    /// Hangs up the shell (its exit event follows).
    func close() {
        closed = true
        if let id { Task { try? await Backend.run("terminal", "Close", [id]) } }
    }

    func type(_ text: String) {
        if let id, !text.isEmpty { Backend.write(id, Array(text.utf8)[...]) }
    }

    /// Ctrl-U, then the command and Return: for an idle prompt.
    func run(_ command: String) {
        type(Shell.clearLine + command + "\r")
        view.window?.makeFirstResponder(view)
    }

    func output(_ bytes: ArraySlice<UInt8>) {
        view.feed(byteArray: bytes)
        // Covers go on before the frame is drawn; marks can wait a moment.
        if Redaction.shared.on { refreshCovers() }
        scheduleMarks()
        scheduleTints()
        scheduleFind()
        onOutput?()
    }

    // MARK: Blocks, for history, pins and exports

    func block(_ id: Int) -> PromptTracker.Block? { tracker.info(id) }
    func blockOutput(_ id: Int) -> String? { tracker.output(id) }
    var lastFinishedBlock: Int? { tracker.lastFinished() }
    func transcript() -> [Export.Block] { tracker.transcript() }

    /// Scrolls a command's prompt into view and flashes its block; false once
    /// it has left the scrollback.
    @discardableResult
    func revealCommand(_ id: Int) -> Bool {
        let t = view.getTerminal()
        guard !t.isCurrentBufferAlternate, let row = tracker.promptRow(id) else { return false }
        view.scrollTo(row: row - t.buffer.totalLinesTrimmed)
        refreshOverlay()
        highlight(id)
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.2) { [weak self] in self?.overlay.highlight = nil }
        return true
    }

    /// The whole scrollback as text: soft-wrapped rows joined, trailing blank lines dropped.
    func plainText() -> String {
        let t = view.getTerminal()
        var lines: [String] = []
        let last = Rows.last(t)
        var row = t.buffer.totalLinesTrimmed
        while row <= last {
            guard let line = t.getScrollInvariantLine(row: row) else { row += 1; continue }
            var text = line.translateToString(trimRight: true)
            while row + 1 <= last, let next = t.getScrollInvariantLine(row: row + 1), next.isWrapped {
                text += next.translateToString(trimRight: true)
                row += 1
            }
            lines.append(text)
            row += 1
        }
        while lines.last?.trimmingCharacters(in: .whitespaces).isEmpty == true { lines.removeLast() }
        return lines.joined(separator: "\n")
    }

    /// Puts text at the prompt the way a paste would (bracketed when the shell asks).
    func insert(_ text: String) {
        view.pasteText(text)
        view.window?.makeFirstResponder(view)
    }

    var owner: WindowController? { WindowManager.shared.windows.first { $0.name == windowName } }

    /// The last rows on screen as text, for the tab overview's preview.
    func snapshot(rows: Int) -> [String] {
        let t = view.getTerminal()
        var end = Rows.cursor(t) + 1
        while end > t.buffer.totalLinesTrimmed, Rows.text(t, end - 1).trimmingCharacters(in: .whitespaces).isEmpty { end -= 1 }
        let lines = (max(t.buffer.totalLinesTrimmed, end - rows)..<max(end, t.buffer.totalLinesTrimmed)).map { Rows.text(t, $0) }
        return Redaction.shared.on ? Redaction.shared.text(lines.joined(separator: "\n")).components(separatedBy: "\n") : lines
    }

    func focus() { view.window?.makeFirstResponder(view) }

    // MARK: Look

    /// A font by family ("JetBrains Mono") or by name ("JetBrainsMono-Regular").
    static func font(_ name: String, _ size: CGFloat) -> NSFont? {
        guard !name.isEmpty else { return nil }
        return NSFontManager.shared.font(withFamily: name, traits: [], weight: 5, size: size) ?? NSFont(name: name, size: size)
    }

    private func applySettings() {
        let s = Settings.shared
        guard let cfg = s.config, let theme = s.theme else { return }
        let size = CGFloat(s.fontSize)
        let font = [cfg.fontFamily, "JetBrains Mono", "Menlo"].lazy.compactMap { Self.font($0, size) }.first
            ?? .monospacedSystemFont(ofSize: size, weight: .regular)
        if view.font != font {
            view.font = font
            Debug.log("terminal font: \(font.fontName) \(font.pointSize)")
        }
        view.lineSpacing = CGFloat(max(1, cfg.lineHeight))
        view.optionAsMetaKey = cfg.optionAsMeta
        view.changeScrollback(cfg.scrollback)
        let blink = cfg.cursorBlink
        let style: CursorStyle = switch cfg.cursorStyle {
        case "bar": blink ? .blinkBar : .steadyBar
        case "underline": blink ? .blinkUnderline : .steadyUnderline
        default: blink ? .blinkBlock : .steadyBlock
        }
        view.getTerminal().setCursorStyle(style)

        let palette = theme.palette.compactMap { RGB(hex: $0) }
        if palette.count == 16 { view.installColors(palette.map(Self.termColor)) }
        let bg = Self.ns(theme.background) ?? .black
        view.nativeBackgroundColor = bg
        view.nativeForegroundColor = Self.ns(theme.foreground) ?? .white
        view.caretColor = Self.ns(theme.cursor) ?? .gray
        view.caretTextColor = Self.ns(theme.cursorText)
        if let sel = Self.ns(theme.selectionBackground) { view.selectedTextBackgroundColor = sel }
        container.layer?.backgroundColor = bg.cgColor

        overlay.dark = theme.dark
        let colors = Logs.Colors(palette: theme.palette, foreground: theme.foreground, background: theme.background)
        if colors != logColors {
            logColors = colors
            container.tints.runs = []
        }
        container.tints.dark = theme.dark
        scheduleTints()
        overlay.coverColor = Self.ns(theme.coverColor.hex) ?? .gray
        overlay.accent = Self.ns(theme.cursor) ?? .controlAccentColor
        container.needsLayout = true
        refreshOverlay()
    }

    private static func ns(_ hex: String) -> NSColor? {
        guard let c = RGB(hex: hex) else { return nil }
        return NSColor(srgbRed: c.r / 255, green: c.g / 255, blue: c.b / 255, alpha: c.a)
    }

    private static func termColor(_ c: RGB) -> SwiftTerm.Color {
        SwiftTerm.Color(red: UInt16(c.r * 257), green: UInt16(c.g * 257), blue: UInt16(c.b * 257))
    }

    // MARK: Overlay

    private var marksScheduled = false

    /// Marks for what's on screen, once per pass of the main loop however
    /// many output frames arrived.
    /// Find's matches that are on screen, for the overlay: read from the
    /// screen as it is now, so they follow output that moved or replaced
    /// lines since the search.
    private func refreshFound() {
        guard let bar = searchBar else {
            overlay.found = []
            overlay.foundCurrent = nil
            return
        }
        let t = view.getTerminal()
        let top = t.buffer.totalLinesTrimmed + t.buffer.yDisp
        overlay.found = Find.visibleMatches(in: t, bar.query, caseSensitive: bar.caseSensitive)
            .map { .init(row: $0.row, col: $0.col, width: $0.width) }
        overlay.foundCurrent = bar.current.map { bar.found[$0] }
            .flatMap { $0.row >= top && $0.row < top + t.rows ? .init(row: $0.row - top, col: $0.col, width: $0.width) : nil }
    }

    private var foundScheduled = false
    private var findScheduled = false

    /// The highlights follow output and scrolling once per pass of the main
    /// loop, however many lines went by.
    private func scheduleFound() {
        guard searchBar != nil, !foundScheduled else { return }
        foundScheduled = true
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            foundScheduled = false
            refreshFound()
        }
    }

    /// Output arrived while Find is open: the count and the selected match
    /// catch up a moment later, one search however much arrives meanwhile.
    private func scheduleFind() {
        scheduleFound()
        guard searchBar != nil, !findScheduled else { return }
        findScheduled = true
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) { [weak self] in
            guard let self else { return }
            findScheduled = false
            searchBar?.refresh()
        }
    }

    // MARK: Log colors

    private var tintsScheduled = false
    private var logColors: Logs.Colors?

    /// Colors what's on screen 120ms after output, a scroll or a resize: one
    /// scan however much arrives meanwhile, none while highlighting is off.
    func scheduleTints() {
        guard Settings.shared.config?.logHighlighting ?? true else {
            container.tints.runs = []
            return
        }
        guard !tintsScheduled else { return }
        tintsScheduled = true
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.12) { [weak self] in
            guard let self else { return }
            tintsScheduled = false
            // A hidden pane is colored when it shows again.
            guard let colors = logColors, view.window != nil, !container.isHiddenOrHasHiddenAncestor else { return }
            container.tints.runs = Logs.tints(view.getTerminal(), colors: colors)
        }
    }

    private func scheduleMarks() {
        guard !marksScheduled else { return }
        marksScheduled = true
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            marksScheduled = false
            refreshMarks()
        }
    }

    private func refreshMarks() {
        let t = view.getTerminal()
        if t.isCurrentBufferAlternate {
            overlay.marks = []
            return
        }
        let top = Rows.top(t)
        overlay.marks = tracker.blocks(from: top, to: top + t.rows - 1).map { .init(row: $0.row - top, block: $0.block) }
    }

    private func refreshCovers() {
        let t = view.getTerminal()
        let covers: [TerminalOverlay.Cover] = Redaction.shared.on
            ? SecretCovers.scan(t).map { .init(row: $0.row, col: $0.col, width: $0.width) } : []
        if covers != overlay.covers, ProcessInfo.processInfo.environment["WTF_NATIVE_DEBUG"] != nil {
            NSLog("covers: \(covers.map { "\($0.row):\($0.col)+\($0.width)" })")
        }
        overlay.covers = covers
    }

    /// Marks and secret covers for what's on screen now.
    private func refreshOverlay() {
        refreshMarks()
        refreshCovers()
    }

    // MARK: Paste

    private func confirmPaste(_ text: String) {
        guard let window = view.window else { return }
        PasteConfirm.show(text, bracketed: view.getTerminal().bracketedPasteMode, panes: broadcastCount?() ?? 1, in: window) { [weak self] text in
            guard let self else { return }
            view.window?.makeFirstResponder(view)
            view.pasteText(text)
        }
    }

    // MARK: Menus

    /// "kubectl get pods · ✗ exit 1 · 3.2s": what the command was and how it went.
    private static func summary(_ b: PromptTracker.Block) -> String {
        let text = b.text.count > 48 ? String(b.text.prefix(47)) + "…" : b.text.isEmpty ? "command" : b.text
        guard let code = b.exitCode else { return "\(text) · running" }
        return [text, code == 0 ? "✓" : "✗ exit \(code)", b.durationMs.map { Format.duration(ms: $0) } ?? ""]
            .filter { !$0.isEmpty }.joined(separator: " · ")
    }

    private func item(_ title: String, key: String = "", mods: NSEvent.ModifierFlags = .command,
                      enabled: Bool = true, _ action: @escaping () -> Void) -> NSMenuItem {
        let item = ClosureMenuItem(title: title, keyEquivalent: key, action: action)
        item.keyEquivalentModifierMask = mods
        item.isEnabled = enabled
        return item
    }

    /// A command block's actions.
    private func blockItems(_ id: Int) -> [NSMenuItem] {
        guard let b = tracker.info(id) else { return [] }
        let heading = NSMenuItem(title: Redaction.shared.text(Self.summary(b)), action: nil, keyEquivalent: "")
        heading.isEnabled = false
        var items = [heading]
        if !b.text.isEmpty { items.append(item("Copy Command") { [weak self] in self?.copy(b.text, "command") }) }
        if b.hasOutput {
            items.append(item("Copy Output") { [weak self] in self?.copy(self?.tracker.output(id) ?? "", "output") })
            if !b.text.isEmpty {
                items.append(item("Copy Command and Output") { [weak self] in
                    let out = self?.tracker.output(id) ?? ""
                    self?.copy(out.isEmpty ? "$ \(b.text)" : "$ \(b.text)\n\(out)", "command and output")
                })
            }
            items.append(item("Select Output") { [weak self] in self?.selectOutput(id) })
            items.append(item(b.running ? "Pin Output (follows it)" : "Pin Output") { [weak self] in
                guard let self else { return }
                owner?.pin(block: id, in: self)
            })
        }
        if !b.running, b.hasOutput, Review.reviewable(b.text) {
            items.append(.separator())
            items.append(item("Review Changes…") { [weak self] in
                guard let self, let o = owner else { return }
                ReviewSheet.show(block: id, in: self, owner: o)
            })
        }
        if let code = b.exitCode, code != 0, ExplainSheet.available {
            items.append(.separator())
            items.append(item("Explain Failure…") { [weak self] in
                guard let self, let o = owner else { return }
                ExplainSheet.show(block: id, in: self, owner: o)
            })
        }
        if !b.text.isEmpty, !b.running {
            items.append(.separator())
            items.append(item("Run Again") { [weak self] in self?.runAgain(b.text) })
        }
        if !b.text.isEmpty {
            items.append(item("Save as Snippet…") { [weak self] in
                guard let self, let w = WindowManager.shared.windows.first(where: { $0.name == self.windowName }) else { return }
                SnippetEditor.show(.new(command: b.text), in: w)
            })
        }
        return items
    }

    /// Copy, paste and the rest.
    private func generalItems() -> [NSMenuItem] {
        var items: [NSMenuItem] = []
        if view.selectedText != nil { items.append(item("Copy", key: "c") { [weak self] in self?.view.copy(self as Any) }) }
        items += [
            item("Paste", key: "v") { [weak self] in self?.view.paste(self as Any) },
            item("Select All", key: "a") { [weak self] in self?.view.selectAll() },
            .separator(),
            item("Find…", key: "f") { [weak self] in self?.showFind() },
            item("Clear", key: "k") { [weak self] in self?.clearBuffer() },
            .separator(),
        ]
        if ExplainSheet.available, !view.getTerminal().isCurrentBufferAlternate {
            items.append(item("Command from Plain English…", key: "i") { [weak self] in
                guard let o = self?.owner else { return }
                AskBar.toggle(in: o)
            })
        }
        items += [
            item(Redaction.shared.on ? "Show Secrets" : "Hide Secrets", key: "r", mods: [.command, .option]) {
                [weak self] in self?.toggleSecrets()
            },
        ]
        if let sel = view.selectedText, !sel.isEmpty {
            items.insert(item("Pin Selection") { [weak self] in
                guard let self else { return }
                owner?.pinSelection(sel, in: self)
            }, at: 1)
        }
        let recording = owner?.isRecording(pane: paneId) == true
        items.append(item(recording ? "Stop Recording" : "Start Recording…", key: "s", mods: [.command, .option]) { [weak self] in
            guard let self, let o = owner else { return }
            o.toggleRecording(pane: o.model.panes[paneId])
        })
        return items
    }

    private func menu(_ items: [NSMenuItem]) -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        m.delegate = self
        items.forEach(m.addItem)
        return m
    }

    /// The right-click menu: the command block under the mouse, then the general items.
    private func contextMenu(for event: NSEvent) -> NSMenu? {
        var items: [NSMenuItem] = []
        let t = view.getTerminal()
        let cell = view.cell(at: event)
        let inspect = cell.map { inspectItems(row: Rows.top(t) + $0.row, col: $0.col) } ?? inspectItems(row: nil, col: nil)
        if !inspect.isEmpty { items = inspect + [.separator()] }
        if let cell, !t.isCurrentBufferAlternate, let id = tracker.blockAt(Rows.top(t) + cell.row) {
            items += blockItems(id) + [.separator()]
            highlight(id)
        }
        return menu(items + generalItems())
    }

    /// The inspector entries over the first row holding some text (debug self-checks).
    func debugInspectTitles(at text: String, offset: Int = 0) -> [String] {
        let t = view.getTerminal()
        for row in stride(from: Rows.last(t), through: t.buffer.totalLinesTrimmed, by: -1) {
            guard let line = t.getScrollInvariantLine(row: row)?.translateToString(trimRight: true),
                  let r = line.range(of: text), !line.contains("echo"), !line.contains("printf") else { continue }
            let col = line.distance(from: line.startIndex, to: r.lowerBound) + offset
            return inspectItems(row: row, col: col).map { $0.isSeparatorItem ? "—" : $0.title }
        }
        return ["(not found: \(text))"]
    }

    /// The soft-wrapped line holding an absolute row, and the index of a column in it.
    private func logicalLine(row: Int, col: Int) -> (text: String, index: Int)? {
        let t = view.getTerminal()
        var start = row
        while start > t.buffer.totalLinesTrimmed, t.getScrollInvariantLine(row: start)?.isWrapped == true { start -= 1 }
        var text = "", index = 0, r = start
        let last = Rows.last(t)
        while r <= last, let line = t.getScrollInvariantLine(row: r) {
            let next = r + 1 <= last ? t.getScrollInvariantLine(row: r + 1) : nil
            let continues = next?.isWrapped == true
            if r == row { index = (text as NSString).length + col }
            text += line.translateToString(trimRight: !continues)
            guard continues else { break }
            r += 1
        }
        return r >= row ? (text, index) : nil
    }

    /// The token at a cell (absolute row), as the menus use it: none for a JWT
    /// while Hide Secrets is on; a path only when the file is on this computer
    /// (made absolute).
    private func token(row: Int, col: Int) -> Logs.Token? {
        guard var tok = Logs.token(view.getTerminal(), row: row, col: col) else { return nil }
        switch tok.kind {
        case .jwt where Redaction.shared.on: return nil
        case .path(let p, let line, let c):
            guard let abs = resolve(p) else { return nil }
            tok.kind = .path(path: abs, line: line, col: c)
        default: break
        }
        return tok
    }

    /// Paths found lately, per folder and name (5s; cleared past 200).
    private var resolved: [String: (path: String?, at: Date)] = [:]

    private func resolve(_ name: String) -> String? {
        let cwd = owner?.model.panes[paneId]?.cwd ?? ""
        let key = cwd + "\u{0}" + name
        if let r = resolved[key], Date().timeIntervalSince(r.at) < 5 { return r.path }
        if resolved.count > 200 { resolved = [:] }
        let p = Logs.resolveFile(name, cwd: cwd)
        resolved[key] = (p, Date())
        return p
    }

    /// The cluster the shell reported at its last prompt (not the Kubernetes tab's).
    private var kubeTarget: Kube.Target {
        let p = owner?.model.panes[paneId]
        return Kube.Target(context: p?.infra?.kubeContext ?? "", namespace: p?.infra?.kubeNamespace ?? "", env: p?.env ?? [:])
    }

    /// ⌘-click: a URL opens (SwiftTerm does that); other tokens show their menu.
    func commandClick(_ event: NSEvent) -> Bool {
        let t = view.getTerminal()
        guard !t.isCurrentBufferAlternate, let cell = view.cell(at: event),
              let raw = Logs.token(t, row: Rows.top(t) + cell.row, col: cell.col) else { return false }
        if case .url = raw.kind { return false }
        guard Logs.isLink(raw.kind) else { return false }
        if case .jwt = raw.kind, Redaction.shared.on {
            Toast.show("Hide Secrets is on: turn it off to inspect tokens", in: view)
            return true
        }
        guard let tok = token(row: Rows.top(t) + cell.row, col: cell.col) else { return false }
        let items = menuItems(Logs.menu(tok, target: kubeTarget))
        guard !items.isEmpty else { return false }
        NSMenu.popUpContextMenu(menu(items), with: event, for: view)
        return true
    }

    /// A token menu as menu items, each running its action.
    private func menuItems(_ entries: [Logs.Entry]) -> [NSMenuItem] {
        entries.map { e in
            switch e {
            case .heading(let h):
                let i = NSMenuItem(title: h, action: nil, keyEquivalent: "")
                i.isEnabled = false
                return i
            case .separator:
                return .separator()
            case .item(let label, let hint, let action):
                let i = item(label) { [weak self] in self?.run(action) }
                i.toolTip = hint
                return i
            }
        }
    }

    private func run(_ a: Logs.Action) {
        guard let o = owner else { return }
        let cwd = o.model.panes[paneId]?.cwd
        switch a {
        case .openUrl(let u): if let url = URL(string: u) { NSWorkspace.shared.open(url) }
        case .copy(let text, let what): copy(text, what)
        case .copySecret(let text, let what): Clipboard.copySecret(text, what: what, in: view)
        case .runInTab(let cmd): o.model.open(cwd: cwd, command: cmd)
        case .runHere(let cmd): o.runInPane(paneId, cmd)
        case .describe(let kind, let name):
            // At the prompt, the shell has its own variables; a new tab needs them in front.
            var bare = kubeTarget
            bare.env = [:]
            if let p = o.model.panes[paneId], p.isShell, !p.running {
                o.runInPane(paneId, Kube.describe(bare, kind, name))
            } else {
                o.model.open(cwd: cwd, command: Kube.describe(kubeTarget, kind, name))
            }
        case .openInEditor(let path, let line): FilesView.openInEditor(path, line: line, in: o)
        case .openFile(let path):
            Task {
                do { try await Services.files.Open(path) } catch { Toast.show("Couldn't open it: \(error.localizedDescription)", in: view) }
            }
        case .reveal(let path):
            Task {
                do { try await Services.files.Reveal(path) } catch { Toast.show("Couldn't reveal it: \(error.localizedDescription)", in: view) }
            }
        case .showSecret(let name): SecretViewer.show(name: name, target: kubeTarget, in: o)
        case .inspectJwt(let t): JwtViewer(token: t, owner: o).show()
        case .inspectTls(let u): CertViewer(.host(u), owner: o).show()
        }
    }

    /// Inspector entries: the URL or token under the mouse, what the
    /// selection can be inspected as, a YAML base64 value, JSON in the line.
    /// Like outputEntries in TerminalPane.svelte.
    private func inspectItems(row: Int?, col: Int?) -> [NSMenuItem] {
        guard let owner else { return [] }
        let hide = Redaction.shared.on
        var groups: [[NSMenuItem]] = []
        let logical = row.flatMap { r in col.flatMap { logicalLine(row: r, col: $0) } }
        let selection = view.selectedText.flatMap { $0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? nil : $0 }

        // The token under the mouse, and its menu.
        if let r = row, let c = col, let tok = token(row: r, col: c) {
            let g = menuItems(Logs.menu(tok, target: kubeTarget))
            if !g.isEmpty { groups.append(g) }
        }
        // What the selection can be inspected as.
        if let selection {
            let entries = Inspect.selectionEntries(selection, secrets: !hide)
            if !entries.isEmpty { groups.append(entries.map { e in item(e.label) { Viewer.open(e.action, in: owner) } }) }
        } else if !hide, let l = logical, let value = Inspect.yamlBase64At(l.text, l.index) {
            groups.append([item("Decode Base64…") { DecodedViewer(text: value, owner: owner).show() }])
        }
        // JSON in the selection, or in the line.
        // (Right-click selects the word under it: the line is the fallback.)
        if let json = [selection, logical?.text].compactMap({ $0 }).lazy.compactMap({ OrderedJSON.find(Redaction.shared.text($0)) }).first {
            groups.append([item("Pretty-print JSON") { JsonViewer(json: json, owner: owner).show() }])
        }
        return groups.enumerated().flatMap { i, g in i == 0 ? g : [.separator()] + g }
    }

    private func showBlockMenu(_ id: Int, _ event: NSEvent) {
        let items = blockItems(id)
        guard !items.isEmpty else { return }
        highlight(id)
        NSMenu.popUpContextMenu(menu(items), with: event, for: view)
    }

    private func highlight(_ id: Int) {
        let t = view.getTerminal()
        guard let rows = tracker.blockRows(id) else { return }
        let top = Rows.top(t)
        let from = max(0, rows.lowerBound - top), to = min(t.rows - 1, rows.upperBound - top)
        overlay.highlight = from <= to ? from...to : nil
    }

    func menuDidClose(_ menu: NSMenu) { overlay.highlight = nil }

    // MARK: Actions

    private func copy(_ text: String, _ what: String) {
        let text = Redaction.shared.text(text)
        Clipboard.copy(text)
        let lines = text.isEmpty ? 0 : text.split(separator: "\n", omittingEmptySubsequences: false).count
        Toast.show(lines > 1 ? "Copied \(what) (\(lines) lines)" : "Copied \(what)", in: view)
    }

    private func selectOutput(_ id: Int) {
        let t = view.getTerminal()
        guard let rows = tracker.outputRows(id) else {
            Toast.show("Its output has left the scrollback", in: view)
            return
        }
        let base = t.buffer.totalLinesTrimmed
        view.selection.setSelection(start: Position(col: 0, row: rows.lowerBound - base),
                                    end: Position(col: t.cols - 1, row: rows.upperBound - base))
    }

    private func runAgain(_ text: String) {
        if let info, !info.isShell {
            Toast.show("The shell is busy — finish the running program first", in: view)
            return
        }
        // Ctrl-U clears anything half-typed first.
        type("\u{15}\(text)\r")
        view.window?.makeFirstResponder(view)
    }

    func copyLastOutput() {
        guard let id = tracker.lastFinished(), let out = tracker.output(id) else {
            Toast.show("No finished command yet", in: view)
            return
        }
        Clipboard.copy(Redaction.shared.text(out))
        let lines = out.isEmpty ? 0 : out.components(separatedBy: "\n").count
        Toast.show(lines == 0 ? "Command had no output" : "Copied \(lines) \(lines == 1 ? "line" : "lines") of output", in: view)
    }

    func toggleSecrets() {
        Redaction.shared.on.toggle()
        Toast.show(Redaction.shared.on ? "Secrets hidden — on screen and when copied" : "Secrets shown", in: view)
    }

    /// Scrolls the previous (-1) or next (1) prompt to the top of the view.
    func jumpPrompt(_ direction: Int) {
        let t = view.getTerminal()
        guard !t.isCurrentBufferAlternate else { return }
        if let row = tracker.jump(direction, from: Rows.top(t)) {
            view.scrollTo(row: row - t.buffer.totalLinesTrimmed)
        } else if direction > 0 {
            view.scrollDown(lines: Int.max / 2)
        }
        refreshOverlay()
    }

    /// Clears the scrollback and screen, keeping the line the cursor is on
    /// (the prompt) as the new first line, like xterm.js's clear().
    func clearBuffer() {
        let t = view.getTerminal()
        guard !t.isCurrentBufferAlternate else { return }
        let y = t.getCursorLocation().y
        if y > 0 { view.feed(text: "\u{1b}[\(y)S\u{1b}[\(y)A") }
        t.buffer.clearScrollback()
        view.scrollTo(row: 0)
        tracker.relocate()
        view.needsDisplay = true
    }

    func showFind() {
        if let bar = searchBar {
            bar.focus()
            return
        }
        let bar = SearchBar(terminal: view)
        bar.onFound = { [weak self] in self?.refreshFound() }
        bar.translatesAutoresizingMaskIntoConstraints = false
        container.addSubview(bar)
        NSLayoutConstraint.activate([
            bar.topAnchor.constraint(equalTo: container.topAnchor, constant: 8),
            bar.trailingAnchor.constraint(equalTo: container.trailingAnchor, constant: -16),
        ])
        bar.onClose = { [weak self] in
            guard let self else { return }
            searchBar?.removeFromSuperview()
            searchBar = nil
            refreshFound()
            view.window?.makeFirstResponder(view)
        }
        searchBar = bar
        bar.focus()
    }

    func findNext() { if let searchBar { searchBar.next() } else { showFind() } }
    func findPrevious() { if let searchBar { searchBar.previous() } else { showFind() } }

    // MARK: TerminalViewDelegate

    func send(source: TerminalView, data: ArraySlice<UInt8>) {
        guard let id, !data.isEmpty else { return }
        Backend.write(id, data)
        fanOut?(data)
    }

    /// Bytes straight to the shell, bypassing the emulator (broadcast receivers).
    func write(_ bytes: ArraySlice<UInt8>) {
        if let id { Backend.write(id, bytes) }
    }

    func sizeChanged(source: TerminalView, newCols: Int, newRows: Int) {
        tracker?.relocate()
        scheduleTints()
        guard let id else { return }
        Task { try? await Backend.run("terminal", "Resize", [id, newCols, newRows]) }
    }

    // Called for every line output scrolls by: coalesced like output.
    func scrolled(source: TerminalView, position: Double) {
        if Redaction.shared.on { refreshCovers() }
        scheduleMarks()
        scheduleTints()
        scheduleFound()
    }

    // A program's own title (OSC 0/2: vim, htop, a remote shell) names the
    // tab while it runs, as in the Wails build.
    func setTerminalTitle(source: TerminalView, title: String) {
        guard let o = owner, let p = o.model.panes[paneId], p.oscTitle != title else { return }
        p.oscTitle = title
        o.paneTitleChanged()
    }
    func hostCurrentDirectoryUpdate(source: TerminalView, directory: String?) {}
    func requestOpenLink(source: TerminalView, link: String, params: [String: String]) {
        if let url = URL(string: link), ["http", "https", "mailto", "file"].contains(url.scheme?.lowercased() ?? "") {
            NSWorkspace.shared.open(url)
        }
    }
    // Silent, as in the Wails build (a beep on every completion miss).
    func bell(source: TerminalView) {}
    // Programs (remote ones too, over ssh) can't set the clipboard (OSC 52),
    // as in the Wails build.
    func clipboardCopy(source: TerminalView, content: Data) {}
    func clipboardRead(source: TerminalView) -> Data? { nil }
    func iTermContent(source: TerminalView, content: ArraySlice<UInt8>) {}
    func rangeChanged(source: TerminalView, startY: Int, endY: Int) {}
}

/// A menu item that runs a closure.
final class ClosureMenuItem: NSMenuItem {
    private let run: () -> Void

    init(title: String, keyEquivalent: String, action: @escaping () -> Void) {
        run = action
        super.init(title: title, action: #selector(fire), keyEquivalent: keyEquivalent)
        target = self
        // Destructive items are red, as in the Wails build's menus.
        if Self.danger.contains(where: { title.hasPrefix($0) }) {
            attributedTitle = NSAttributedString(string: title, attributes: [.foregroundColor: NSColor.systemRed, .font: NSFont.menuFont(ofSize: 0)])
        }
    }

    static let danger = ["Delete", "End ", "Force Quit", "Remove These", "Close Tab…", "Unpin All", "Forget Stored"]

    required init(coder: NSCoder) { fatalError() }

    @objc private func fire() { run() }
}

/// The app menu's terminal commands reach the focused terminal through the
/// responder chain.
extension WtfTerminalView {
    @objc func wtfFind(_ sender: Any?) {
        switch (sender as? NSMenuItem)?.tag ?? Int(NSFindPanelAction.showFindPanel.rawValue) {
        case Int(NSFindPanelAction.next.rawValue): controller?.findNext()
        case Int(NSFindPanelAction.previous.rawValue): controller?.findPrevious()
        default: controller?.showFind()
        }
    }

    @objc func clearBuffer(_ sender: Any?) { controller?.clearBuffer() }
    @objc func copyLastOutput(_ sender: Any?) { controller?.copyLastOutput() }
    @objc func toggleSecrets(_ sender: Any?) { controller?.toggleSecrets() }
    @objc func previousPrompt(_ sender: Any?) { controller?.jumpPrompt(-1) }
    @objc func nextPrompt(_ sender: Any?) { controller?.jumpPrompt(1) }
    @objc func biggerText(_ sender: Any?) { Settings.shared.biggerText() }
    @objc func smallerText(_ sender: Any?) { Settings.shared.smallerText() }
    @objc func actualSize(_ sender: Any?) { Settings.shared.actualSize() }
}

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
    private let window: String
    private(set) var id: String?
    private var tracker: PromptTracker!
    private var searchBar: SearchBar?
    /// What runs in the shell now, from terminal:info.
    private var info: TerminalInfo?
    /// Called with the title the window should show.
    var onTitle: ((String) -> Void)?
    var onExit: ((Int) -> Void)?

    init(window: String, frame: NSRect) {
        self.window = window
        view = WtfTerminalView(frame: frame, font: NSFont.monospacedSystemFont(ofSize: 13, weight: .regular))
        container = TerminalContainer(terminal: view)
        super.init()
        container.autoresizingMask = [.width, .height]
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
        if ProcessInfo.processInfo.environment["WTF_NATIVE_DEBUG"] != nil {
            tracker.onCommandStart = { NSLog("command: \(Redaction.shared.text($0)) (#\($1))") }
            tracker.onCommandEnd = { NSLog("command done: exit \($0) in \(Format.duration(ms: $1))") }
        }
        view.confirmPaste = { [weak self] in self?.confirmPaste($0) }
        view.contextMenu = { [weak self] in self?.contextMenu(for: $0) }
        overlay.onMarkClick = { [weak self] id, event in self?.showBlockMenu(id, event) }

        Settings.shared.observe { [weak self] in self?.applySettings() }
        Redaction.shared.observe { [weak self] in self?.refreshOverlay() }
        applySettings()
    }

    /// Starts a shell in the user's home directory.
    func start() async throws {
        let term = view.getTerminal()
        let id = try await Backend.call("terminal", "Create", [window, term.cols, term.rows, ""], as: String.self)
        self.id = id
        Backend.on("terminal:info", TerminalInfo.self) { [weak self] info in
            guard let self, info.id == self.id else { return }
            self.info = info
            self.onTitle?(Redaction.shared.text(Self.title(info)))
        }
        Backend.on("terminal:exit", TerminalExit.self) { [weak self] exit in
            guard let self, exit.id == self.id else { return }
            self.onExit?(exit.code)
        }
        try Backend.attach(id, to: self)
    }

    func type(_ text: String) {
        if let id { Backend.write(id, Array(text.utf8)[...]) }
    }

    func output(_ bytes: ArraySlice<UInt8>) {
        view.feed(byteArray: bytes)
        // Covers go on before the frame is drawn; marks can wait a moment.
        if Redaction.shared.on { refreshCovers() }
        scheduleMarks()
    }

    private static func title(_ info: TerminalInfo) -> String {
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let dir = info.cwd.hasPrefix(home) ? "~" + info.cwd.dropFirst(home.count) : info.cwd
        return info.isShell ? dir : "\(info.process) — \(dir)"
    }

    // MARK: Look

    private func applySettings() {
        let s = Settings.shared
        guard let cfg = s.config, let theme = s.theme else { return }
        let size = CGFloat(s.fontSize)
        let font = [cfg.fontFamily, "JetBrains Mono", "Menlo"].lazy.compactMap { NSFont(name: $0, size: size) }.first
            ?? .monospacedSystemFont(ofSize: size, weight: .regular)
        if view.font != font { view.font = font }
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
        PasteConfirm.show(text, bracketed: view.getTerminal().bracketedPasteMode, in: window) { [weak self] text in
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
        }
        if !b.text.isEmpty, !b.running {
            items.append(.separator())
            items.append(item("Run Again") { [weak self] in self?.runAgain(b.text) })
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
            item(Redaction.shared.on ? "Show Secrets" : "Hide Secrets", key: "r", mods: [.command, .option]) {
                [weak self] in self?.toggleSecrets()
            },
        ]
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
        if let cell = view.cell(at: event), !t.isCurrentBufferAlternate,
           let id = tracker.blockAt(Rows.top(t) + cell.row) {
            items = blockItems(id) + [.separator()]
            highlight(id)
        }
        return menu(items + generalItems())
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
            Toast.show("No finished command output to copy", in: view)
            return
        }
        copy(out, "output")
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
            view.window?.makeFirstResponder(view)
        }
        searchBar = bar
        bar.focus()
    }

    func findNext() { if let searchBar { searchBar.next() } else { showFind() } }
    func findPrevious() { if let searchBar { searchBar.previous() } else { showFind() } }

    // MARK: TerminalViewDelegate

    func send(source: TerminalView, data: ArraySlice<UInt8>) {
        if let id, !data.isEmpty { Backend.write(id, data) }
    }

    func sizeChanged(source: TerminalView, newCols: Int, newRows: Int) {
        tracker?.relocate()
        guard let id else { return }
        Task { try? await Backend.run("terminal", "Resize", [id, newCols, newRows]) }
    }

    // Called for every line output scrolls by: coalesced like output.
    func scrolled(source: TerminalView, position: Double) {
        if Redaction.shared.on { refreshCovers() }
        scheduleMarks()
    }

    // The shell's own title (OSC 0/2) is ignored, as in the Wails build:
    // the title comes from terminal:info.
    func setTerminalTitle(source: TerminalView, title: String) {}
    func hostCurrentDirectoryUpdate(source: TerminalView, directory: String?) {}
    func requestOpenLink(source: TerminalView, link: String, params: [String: String]) {
        if let url = URL(string: link), ["http", "https", "mailto", "file"].contains(url.scheme?.lowercased() ?? "") {
            NSWorkspace.shared.open(url)
        }
    }
    func bell(source: TerminalView) { NSSound.beep() }
    func clipboardCopy(source: TerminalView, content: Data) {
        if let s = String(data: content, encoding: .utf8) { Clipboard.copy(s) }
    }
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
    }

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

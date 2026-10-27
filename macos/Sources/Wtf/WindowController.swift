import AppKit
import WtfCore

/// One window: its tabs and panes (WindowModel), the tab strip, the pane
/// area and a terminal per pane. Menu commands for the focused window land
/// here through the responder chain. Like App.svelte with tabs.svelte.ts.
@MainActor
final class WindowController: NSWindowController, NSWindowDelegate, NSMenuItemValidation {
    let name: String
    let model = WindowModel()
    private let strip: TabStripView
    private let panes = PaneArea()
    private let root = RootView()
    private let address = AddressBar()
    private let broadcastBar = BroadcastBar()
    private let middle = MiddleStack()
    /// A terminal for every pane, kept while its tab is in the background.
    private(set) var terminals: [Int: TerminalController] = [:]
    /// A request editor for every API pane.
    private(set) var apis: [Int: ApiController] = [:]
    /// Panes whose shells went to another window: not hung up when they leave.
    private var detached = Set<Int>()
    /// Commands run, pins, recordings: in memory for this window.
    let records = WindowRecords()
    private var recTimer: Timer?
    private var saveTimer: Timer?
    private var lastSaved = ""
    private var closing = false
    private var sessionExtra: [String: JSONValue] = [:]
    /// The side pane: whether it's open, how wide, which tab (saved per window).
    var sidebar = SidebarState()
    private var sidebarView: Sidebar?

    init(name: String) {
        self.name = name
        strip = TabStripView(model: model)
        let window = WtfWindow(contentRect: NSRect(x: 0, y: 0, width: 1100, height: 700),
                              styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
                              backing: .buffered, defer: false)
        window.titlebarAppearsTransparent = true
        window.titleVisibility = .hidden
        window.minSize = NSSize(width: 480, height: 300)
        window.isReleasedWhenClosed = false
        window.tabbingMode = .disallowed
        super.init(window: window)
        window.delegate = self
        root.strip = strip
        root.panes = panes
        root.addSubview(panes)
        root.addSubview(strip)
        middle.address = address
        middle.broadcast = broadcastBar
        root.middle = middle
        window.contentView = root
        window.strip = strip
        window.onFirstResponder = { [weak self] in self?.firstResponderChanged($0) }
        address.onShowSecrets = { Redaction.shared.on = false }
        address.onShowUsage = { [weak self] in self.map { UsageDialog.show(in: $0) } }
        address.onStopRecording = { [weak self] in self?.stopRecording(pane: $0) }
        broadcastBar.onStop = { [weak self] in
            guard let self else { return }
            Broadcast.shared.stopAll(in: self)
        }
        Redaction.shared.observe(self) { $0.sync() }

        model.onChange = { [weak self] in self?.sync() }
        model.onEmpty = { [weak self] in self?.closeForGood() }
        strip.onActivate = { [weak self] in self?.model.activate($0) }
        strip.onClose = { [weak self] in self?.closeTabs([$0]) }
        strip.onNewTab = { [weak self] in self?.model.open() }
        strip.onNewTabMenu = { [weak self] in
            let m = NSMenu()
            m.addItem(ClosureMenuItem(title: "New Tab", keyEquivalent: "") { self?.model.open() })
            m.addItem(ClosureMenuItem(title: "New API Tab", keyEquivalent: "") { self?.model.openApi() })
            return m
        }
        strip.onMenu = { [weak self] id, _ in self?.tabMenu(id) }
        strip.onChip = { [weak self] g, right, r in
            guard let self else { return }
            if right { showGroupEditor(g, at: r) } else { model.toggleCollapsed(g) }
        }
        strip.onTearOff = { [weak self] id, at in self?.moveToNewWindow(id, at: at) }
        strip.onRenamed = { [weak self] in self?.focusActive() }
        strip.onToggleSidebar = { [weak self] in self?.toggleSidebar(nil) }
        strip.broadcasting = { Broadcast.shared.isBroadcasting(tab: $0, in: self) }
        strip.recording = { [unowned self] tab in model.tab(tab)?.root.leaves.contains { records.recording[$0] != nil } ?? false }
        panes.onRatio = { [weak self] path, r in
            guard let self, let t = model.active else { return }
            model.setRatio(tab: t.id, path: path, r)
        }
        Settings.shared.observe(self) { $0.applyTheme() }
        applyTheme()
        if !SshStore.shared.identitiesLoaded { SshStore.shared.loadIdentities() }
    }

    required init?(coder: NSCoder) { fatalError() }

    // MARK: Startup

    /// Opens what the window starts with: a saved session, a tab from
    /// another window, or one fresh tab.
    func bootstrap(session: SavedSession?, tab: (payload: SavedTab.Payload, terminals: [String: TerminalController])?, plan: Plan? = nil) {
        if let tab {
            adopt(tab.payload, terminals: tab.terminals)
        } else if let plan, model.open(plan: plan) != nil {
            // A workspace opened in this new window.
        } else if let session, model.restore(session) {
            sessionExtra = session.extra
            sidebar = SidebarState(json: session.extra["sidebar"])
            NetStore.shared.terminalsOnly = sidebar.networkScope == "terminals"
            ProcStore.shared.terminalsOnly = sidebar.processScope == "terminals"
            ProcStore.shared.sort = Procs.Sort(rawValue: sidebar.processSort) ?? .cpu
        } else {
            model.open(cwd: "")
        }
        sync()
        layoutSidebar()
        lastSaved = sessionJSON()
    }

    // MARK: Side pane

    /// Shows, hides or resizes the side pane to match `sidebar`.
    func layoutSidebar() {
        if sidebar.open {
            if sidebarView == nil {
                let v = Sidebar(window: self)
                sidebarView = v
                root.sidebar = v
            }
            sidebarView?.update()
        } else if sidebarView != nil {
            // Closed means gone: nothing in it keeps running.
            root.sidebar = nil
            sidebarView = nil
        }
        root.sidebarWidth = CGFloat(sidebar.width)
        root.needsLayout = true
        strip.sidebarOpen = sidebar.open
        scheduleSave()
    }

    func sidebarChanged() { layoutSidebar() }

    @objc func toggleSidebar(_ sender: Any?) {
        sidebar.open.toggle()
        layoutSidebar()
    }

    /// A tab's shortcut: opens the pane on it, or closes the pane when it's
    /// already showing it (as VS Code does).
    func showSidebar(_ tab: SidebarTab) {
        if sidebar.open, sidebar.tab == tab {
            sidebar.open = false
        } else {
            sidebar.tab = tab
            sidebar.open = true
        }
        Debug.log("showSidebar(\(tab)) → open \(sidebar.open), tab \(sidebar.tab)")
        layoutSidebar()
    }

    var sidePane: SidePaneTab? { sidebarView?.content }

    // MARK: Model → views

    /// Brings the views in line with the model: terminals for new panes,
    /// closed panes hung up, the active tab laid out and focused.
    private func sync() {
        let live = Set(model.panes.keys)
        for (id, term) in terminals where !live.contains(id) {
            terminals[id] = nil
            let moved = detached.remove(id) != nil
            if !moved { term.close() }
            recordsPaneGone(id, moved: moved)
            term.container.removeFromSuperview()
        }
        for (id, api) in apis where !live.contains(id) {
            apis[id] = nil
            api.close()
            api.container.removeFromSuperview()
        }
        for (id, pane) in model.panes where terminals[id] == nil && apis[id] == nil {
            if pane.kind == .api { startApi(pane) } else { startTerminal(pane) }
        }

        if let t = model.active {
            var views: [Int: NSView] = [:]
            for id in t.root.leaves {
                if let term = terminals[id] { views[id] = term.container } else if let api = apis[id] { views[id] = api.container }
            }
            panes.dimAmount = Settings.shared.config?.unfocusedSplitDim ?? 0.35
            panes.show(t.root, focused: t.focusedPaneId, zoomed: t.zoomedPaneId, views: views)
            for id in t.root.leaves { terminals[id]?.scheduleTints() }
        }
        strip.refresh()
        Broadcast.shared.prune(in: self)
        refreshBars()
        window?.title = model.active.map { Redaction.shared.text(model.title($0)) } ?? "wtf"
        // The keyboard moves only when another pane comes to the front:
        // renaming a group or a tab, or any other edit, keeps it where it is.
        sidePane?.paneChanged()
        let front = model.active.map { [$0.id, $0.focusedPaneId] }
        if front != lastFront {
            lastFront = front
            focusActive()
        }
        scheduleSave()
    }

    /// The active tab and its focused pane at the last sync.
    private var lastFront: [Int]?

    /// The REC pill and the tabs' red dots, and the 1s clock only while something records.
    func refreshRecordingIndicators() {
        let on = !records.recording.isEmpty
        if on, recTimer == nil {
            recTimer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in
                MainActor.assumeIsolated { self?.address.updateRecording() }
            }
        } else if !on {
            recTimer?.invalidate()
            recTimer = nil
        }
        strip.refresh()
        refreshBars()
    }

    private func refreshBars() {
        address.recording = model.activePane.flatMap { p in records.recording[p.id].map { (p.id, $0.path, $0.since) } }
        address.show(pane: model.activePane, group: model.active?.groupId.flatMap(model.group))
        let on = Broadcast.shared.isOn(in: self)
        broadcastBar.isHidden = !on
        if on { broadcastBar.set(Broadcast.shared.describe(in: self)) }
        for (id, term) in terminals { term.container.outlined = on && Broadcast.shared.includes(id, in: self) }
        root.needsLayout = true
    }

    /// A program set its pane's title: the tab and window show it.
    func paneTitleChanged() {
        strip.refresh()
        window?.title = model.active.map { Redaction.shared.text(model.title($0)) } ?? "wtf"
    }

    func broadcastChanged() {
        strip.refresh()
        refreshBars()
    }

    /// A pane's context badges were resolved.
    func paneContextChanged() {
        refreshBars()
        // The side pane follows it too (the AWS profile in use, the cluster).
        sidePane?.paneChanged()
    }

    /// Re-resolves every pane's context (an AWS account lookup finished, or
    /// badges were turned on).
    func refreshContexts() {
        for p in model.panes.values {
            if Settings.shared.config?.contextBadges == false { p.infra = nil } else { AddressBarModel.changed(pane: p, in: self) }
        }
        refreshBars()
    }

    /// Gives the keyboard to the active tab's focused pane, unless a text
    /// field (rename, find) has it.
    func focusActive() {
        guard let w = window, let t = model.active else { return }
        if let api = apis[t.focusedPaneId] {
            if let r = w.firstResponder as? NSView, r.isDescendant(of: strip) || r.isDescendant(of: api.container) { return }
            api.focus()
            return
        }
        guard let term = terminals[t.focusedPaneId] else { return }
        // Renaming a tab, or typing in this pane's find bar, keeps the keyboard.
        if let r = w.firstResponder as? NSView, r is NSTextView || r is NSTextField,
           r.isDescendant(of: strip) || r.isDescendant(of: term.container) { return }
        if w.firstResponder !== term.view { w.makeFirstResponder(term.view) }
    }

    private func startTerminal(_ pane: Pane) {
        let term = TerminalController(pane: pane.id, window: name)
        wire(term)
        terminals[pane.id] = term
        let identity = pane.gitIdentity, command = pane.initialCommand
        pane.initialCommand = nil
        Task {
            do {
                let id = try await term.start(cwd: pane.initialCwd, identity: identity, command: command)
                pane.sessionId = id
                if model.panes[pane.id] == nil { term.close() }
                if let info = try? await Backend.call("terminal", "Info", [id], as: TerminalInfo.self) {
                    apply(info)
                }
            } catch {
                Toast.show("Couldn't start a shell: \(error.localizedDescription)", in: root)
            }
        }
    }

    private func startApi(_ pane: Pane) {
        let api = ApiController(pane: pane.id, request: pane.api ?? Api.blank)
        let id = pane.id
        api.onFocus = { [weak self] in self?.model.focusPane(id) }
        api.onChange = { [weak self, weak api] in
            guard let self, let api, let p = model.panes[id] else { return }
            p.api = api.request
            paneTitleChanged()
            scheduleSave()
        }
        api.onRunInShell = { [weak self] in self?.runFromApi(id, $0) }
        apis[id] = api
    }

    /// The keyboard went into an API pane: it becomes the focused pane.
    private func firstResponderChanged(_ r: NSResponder?) {
        guard let v = r as? NSView, let (id, _) = apis.first(where: { v.isDescendant(of: $0.value.container) }) else { return }
        model.focusPane(id)
    }

    /// Run in Shell from an API pane: typed at an idle shell in the same
    /// tab, else in a new tab.
    private func runFromApi(_ apiPane: Int, _ command: String) {
        let idle = model.tabOf(pane: apiPane)?.root.leaves.first { id in
            guard let p = model.panes[id], terminals[id] != nil else { return false }
            return p.isShell && !p.running
        }
        if let id = idle, let term = terminals[id] {
            model.focusPane(id)
            term.run(command)
        } else {
            model.open(cwd: model.panes[apiPane]?.cwd, command: command)
        }
    }

    /// Hooks a pane's terminal to the model.
    private func wire(_ term: TerminalController) {
        let id = term.paneId
        term.fanOut = { [weak self, weak term] bytes in
            guard let self, let term, Broadcast.shared.isOn(in: self), term.view.window?.firstResponder === term.view else { return }
            if TerminalReport.matches(String(decoding: bytes, as: UTF8.self)) { return }
            for target in Broadcast.shared.targets(of: term.paneId, in: self) { terminals[target]?.write(bytes) }
        }
        term.broadcastCount = { [weak self, weak term] in
            guard let self, let term else { return 1 }
            return Broadcast.shared.targets(of: term.paneId, in: self).count + 1
        }
        term.onFocus = { [weak self] in self?.model.focusPane(id) }
        term.onOutput = { [weak self] in
            guard let self, let t = model.tabOf(pane: id) else { return }
            let visible = t.id == model.activeId && (t.zoomedPaneId == nil || t.zoomedPaneId == id)
            if !visible { model.markActivity(pane: id) }
        }
        term.onPrompt = { [weak self] in
            guard let p = self?.model.panes[id], !p.integrated else { return }
            p.integrated = true
        }
        term.onCommandStart = { [weak self] text, block in
            guard let self, let p = model.panes[id] else { return }
            records.history.started(pane: id, id: block, command: text, cwd: p.cwd)
            records.runningBlock[id] = block
            records.changed()
            p.running = true
            p.command = text
            strip.refresh()
            refreshBars()
        }
        term.onCommandEnd = { [weak self] code, ms in
            guard let self, let p = model.panes[id] else { return }
            p.lastCommand = (p.command, code, ms)
            SshStore.shared.commandFinished(p.command)
            records.history.finished(pane: id, exitCode: code, durationMs: ms)
            // A pin following this command gets its final output.
            if let b = records.runningBlock.removeValue(forKey: id), let term = terminals[id] {
                records.pins.update(pane: id, block: b, text: term.blockOutput(b) ?? "", done: (code, ms))
            }
            records.changed()
            if Findings.isScan(p.command) { records.scanFinished() }
            p.running = false
            p.command = ""
            model.markFinished(pane: id, ok: code == 0)
            if id == model.active?.focusedPaneId { sidePane?.commandFinished() }
            notifyIfSlow(p, code, ms)
            strip.refresh()
            refreshBars()
        }
        term.onEnv = { [weak self] text in
            guard let self, let p = model.panes[id] else { return }
            p.env = Env.parse(text)
            AddressBarModel.changed(pane: p, in: self)
            scheduleSave()
        }
    }

    // MARK: Backend events (routed here by the window manager)

    func owns(session: String) -> Bool { model.pane(session: session) != nil }

    func apply(_ info: TerminalInfo) {
        model.applyInfo(session: info.id, process: info.process, cwd: info.cwd, isShell: info.isShell)
        if let p = model.pane(session: info.id) { terminals[p.id]?.info = info }
    }

    func sessionExited(_ session: String) { model.sessionExited(session) }

    /// notify-after: a long command finished while you weren't looking.
    private func notifyIfSlow(_ p: Pane, _ code: Int, _ ms: Double) {
        let after = Settings.shared.config?.notifyAfter ?? 10
        guard after > 0, ms >= after * 1000 else { return }
        let visible = model.active?.root.leaves.contains(p.id) == true && window?.isKeyWindow == true && NSApp.isActive
        guard !visible else { return }
        let dur = Format.duration(ms: ms)
        let title = code == 0 ? "✓ Finished in \(dur)" : "✗ Failed (exit \(code)) after \(dur)"
        let body = Redaction.shared.text(p.lastCommand?.text ?? p.process)
        Task { try? await Backend.run("notify", "Notify", [title, WindowModel.prettyPath(p.cwd), body]) }
    }

    // MARK: Look

    private func applyTheme() {
        guard let theme = Settings.shared.theme else { return }
        let chrome = NSColor(theme.chrome)
        let surface = NSColor(hex: theme.background)
        window?.backgroundColor = chrome
        window?.appearance = NSAppearance(named: theme.dark ? .darkAqua : .aqua)
        root.layer?.backgroundColor = chrome.cgColor
        strip.dark = theme.dark
        strip.chrome = chrome
        strip.surface = surface
        strip.accent = NSColor(theme.accent)
        strip.ok = NSColor(hex: theme.dark ? "#5bb98c" : "#2f9e6a")
        strip.danger = NSColor(hex: theme.dark ? "#e5484d" : "#d93036")
        panes.surface = surface
        panes.accent = NSColor(theme.accent)
        panes.hairline = theme.dark ? NSColor(white: 1, alpha: 0.12) : NSColor(white: 0, alpha: 0.14)
        panes.layer?.backgroundColor = surface.cgColor
        panes.dimAmount = Settings.shared.config?.unfocusedSplitDim ?? 0.35
        panes.needsLayout = true
        address.dark = theme.dark
        address.accent = NSColor(theme.accent)
        address.ok = strip.ok
        address.danger = strip.danger
        address.omniboxColor = theme.dark ? NSColor(white: 1, alpha: 0.06) : NSColor(white: 0, alpha: 0.05)
        address.identityColor = theme.dark ? NSColor(white: 1, alpha: 0.08) : NSColor(white: 0, alpha: 0.06)
        strip.refresh()
        refreshBars()
    }

    // MARK: Saving

    private func sessionJSON() -> String {
        var extra = sessionExtra
        sidebar.networkScope = NetStore.shared.terminalsOnly ? "terminals" : "all"
        sidebar.processScope = ProcStore.shared.terminalsOnly ? "terminals" : "all"
        sidebar.processSort = ProcStore.shared.sort.rawValue
        extra["sidebar"] = sidebar.json
        let s = model.snapshot(fontSize: Settings.shared.fontSize, extra: extra)
        let enc = JSONEncoder()
        enc.outputFormatting = .sortedKeys
        return (try? enc.encode(s)).map { String(decoding: $0, as: UTF8.self) } ?? ""
    }

    /// Saves 400ms after the last change, and only what changed.
    func scheduleSave() {
        guard !closing else { return }
        saveTimer?.invalidate()
        saveTimer = Timer.scheduledTimer(withTimeInterval: 0.4, repeats: false) { [weak self] _ in
            MainActor.assumeIsolated { self?.saveNow() }
        }
    }

    func saveNow() {
        guard !closing else { return }
        let json = sessionJSON()
        guard json != lastSaved, !model.tabs.isEmpty else { return }
        lastSaved = json
        let name = name
        Task {
            do { try await Backend.run("state", "SaveWindow", [name, json]) } catch { NSLog("saving \(name): \(error)") }
        }
    }

    // MARK: Closing

    /// Asks first when programs would be ended; runs `action` once it's fine.
    func confirmIfBusy(_ paneIds: [Int], title: String, button: String, quitting: Bool = false, _ action: @escaping () -> Void) {
        let busy = paneIds.compactMap { model.panes[$0] }.filter(\.busy)
        guard !busy.isEmpty, let window else { return action() }
        let alert = NSAlert()
        alert.messageText = title
        let n = busy.count
        let verb = quitting ? "Quitting" : "Closing"
        alert.informativeText = (n == 1 ? "A program is still running. \(verb) will end it." : "\(n) programs are still running. \(verb) will end them.")
            + "\n\n" + busy.prefix(6).map { p in
                "\(p.running && !p.command.isEmpty ? Redaction.shared.text(p.command) : p.process) — \(WindowModel.prettyPath(p.cwd))"
            }.joined(separator: "\n") + (n > 6 ? "\nand \(n - 6) more" : "")
        alert.addButton(withTitle: button)
        alert.addButton(withTitle: "Cancel")
        alert.buttons[0].hasDestructiveAction = true
        alert.beginSheetModal(for: window) { r in
            if r == .alertFirstButtonReturn { action() }
        }
    }

    func closeTabs(_ ids: [Int]) {
        let panes = ids.compactMap(model.tab).flatMap(\.root.leaves)
        let n = ids.count
        confirmIfBusy(panes, title: n == 1 ? "Close this tab?" : "Close \(n) tabs?", button: n == 1 ? "Close Tab" : "Close Tabs") { [weak self] in
            self?.model.closeMany(ids)
        }
    }

    private func closePane(_ id: Int) {
        guard let t = model.tabOf(pane: id) else { return }
        let last = t.root.leaves.count == 1
        confirmIfBusy([id], title: last ? "Close this tab?" : "Close this pane?", button: last ? "Close Tab" : "Close Pane") { [weak self] in
            self?.model.closePane(id)
        }
    }

    func windowShouldClose(_ sender: NSWindow) -> Bool {
        if closing || WindowManager.shared.quitting { return true }
        confirmIfBusy(Array(model.panes.keys), title: "Close this window?", button: "Close Window") { [weak self] in
            self?.closeForGood()
        }
        return false
    }

    /// Closes the window and forgets its session.
    func closeForGood() {
        guard !closing else { return }
        closing = true
        saveTimer?.invalidate()
        for (id, term) in terminals where !detached.contains(id) { term.close() }
        terminals.removeAll()
        apis.values.forEach { $0.close() }
        apis.removeAll()
        let name = name
        Task { try? await Backend.run("session", "Forget", [name]) }
        window?.close()
        WindowManager.shared.closed(self)
    }

    /// The app quits: the session is saved now and stays for next launch.
    func quitting() {
        saveTimer?.invalidate()
        let json = sessionJSON()
        if json != lastSaved, !model.tabs.isEmpty {
            do { try Backend.runNow("state", "SaveWindow", [name, json]) } catch { NSLog("saving \(name): \(error)") }
        }
        closing = true
    }

    func windowDidBecomeKey(_ note: Notification) {
        Backend.setFocused(window: name, focused: true)
        focusActive()
    }

    func windowDidResignKey(_ note: Notification) { Backend.setFocused(window: name, focused: false) }

    func windowWillClose(_ note: Notification) { Backend.forget(window: name) }

    // MARK: Moving tabs between windows

    private func detach(_ id: Int) -> (SavedTab.Payload, [String: TerminalController])? {
        guard let t = model.tab(id) else { return nil }
        var moving: [String: TerminalController] = [:]
        for p in t.root.leaves {
            guard let term = terminals[p], let s = model.panes[p]?.sessionId else { continue }
            moving[s] = term
            detached.insert(p)
        }
        guard let payload = model.detach(id, buffers: { _ in "" }) else { return nil }
        return (payload, moving)
    }

    /// Takes in a tab; its panes reuse the terminals that came with it, so
    /// their scrollback and running programs carry over.
    func adopt(_ payload: SavedTab.Payload, terminals moving: [String: TerminalController]) {
        let before = Set(model.panes.keys)
        guard model.adopt(payload) else { return }
        for (id, pane) in model.panes where !before.contains(id) {
            guard let s = pane.sessionId, let term = moving[s] else { continue }
            pane.restoreBuffer = nil
            term.moved(to: name)
            term.paneId = id
            wire(term)
            terminals[id] = term
            if let info = term.info { apply(info) }
            syncRecording(pane: id)
        }
        sync()
        window?.makeKeyAndOrderFront(nil)
    }

    func moveToNewWindow(_ id: Int, at point: NSPoint? = nil) {
        guard model.tabs.count > 1 else {
            Toast.show("This is the only tab in the window", in: root)
            return
        }
        guard let (payload, moving) = detach(id) else { return }
        WindowManager.shared.open(tab: (payload, moving), at: point)
    }

    func moveTab(_ id: Int, to other: WindowController) {
        guard let (payload, moving) = detach(id) else { return }
        other.adopt(payload, terminals: moving)
    }

    // MARK: Menus

    private func tabMenu(_ id: Int) -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        func add(_ title: String, key: String = "", mods: NSEvent.ModifierFlags = [.command], _ action: @escaping () -> Void) {
            let item = ClosureMenuItem(title: title, keyEquivalent: key, action: action)
            item.keyEquivalentModifierMask = mods
            m.addItem(item)
        }
        guard let t = model.tab(id) else { return m }
        add("New Tab to the Right") { [weak self] in self?.model.openNextTo(id) }
        add("New API Tab to the Right") { [weak self] in
            guard let self, let i = model.index(id) else { return }
            model.openApi(at: i + 1, group: model.tab(id)?.groupId)
        }
        m.addItem(.separator())
        add("Add Tab to New Group", key: "g", mods: [.command, .option]) { [weak self] in self?.newGroup(id) }
        let others = model.groups.filter { $0.id != t.groupId }
        if !others.isEmpty {
            let heading = NSMenuItem(title: "Add Tab to Group", action: nil, keyEquivalent: "")
            heading.isEnabled = false
            m.addItem(heading)
            for g in others {
                let item = ClosureMenuItem(title: g.name.isEmpty ? "Unnamed group" : g.name, keyEquivalent: "") { [weak self] in
                    self?.model.addToGroup(id, g.id)
                }
                item.image = Self.dot(NSColor(hex: g.color.hex(dark: Settings.shared.theme?.dark ?? true)))
                item.indentationLevel = 1
                m.addItem(item)
            }
        }
        if let g = t.groupId {
            add("Remove from Group") { [weak self] in self?.model.removeFromGroup(id) }
            let on = Broadcast.shared.group(g, in: self)
            add(on ? "Stop Broadcasting Input" : "Broadcast Input to Group", key: "b", mods: [.command, .option]) { [weak self] in
                guard let self else { return }
                Broadcast.shared.toggle(group: g, in: self)
            }
        }
        if t.root.isSplit, !(t.groupId.map { Broadcast.shared.group($0, in: self) } ?? false) {
            let on = Broadcast.shared.panes(of: id, in: self)
            add(on ? "Stop Broadcasting to Panes" : "Broadcast Input to Panes") { [weak self] in
                guard let self else { return }
                Broadcast.shared.toggle(panesOf: id, in: self)
            }
        }
        // Recording and saving the tab's focused pane.
        if let p = model.focusedPane(t), p.kind == .terminal {
            m.addItem(.separator())
            add(isRecording(pane: p.id) ? "Stop Recording" : "Start Recording…", key: "s", mods: [.command, .option]) { [weak self] in
                self?.toggleRecording(pane: p)
            }
            add("Save Output as Text…") { [weak self] in self?.saveOutputAsText(pane: p) }
            if p.integrated { add("Save Commands as Markdown…") { [weak self] in self?.saveTranscript(pane: p) } }
        }
        m.addItem(.separator())
        // Git identities: switch this tab's shell to one, or back.
        let identities = SshStore.shared.identities
        let current = model.focusedPane(t)?.identity
        if identities.isEmpty {
            add("Git Identities…") { [weak self] in self?.showGitIdentities() }
        } else {
            let heading = NSMenuItem(title: current.map { "Git identity: \($0)" } ?? "Git identity", action: nil, keyEquivalent: "")
            heading.isEnabled = false
            m.addItem(heading)
            for i in identities where i.name != current {
                let item = ClosureMenuItem(title: "Use \(i.name)", keyEquivalent: "") { [weak self] in
                    guard let self, let p = model.tab(id)?.focusedPaneId else { return }
                    runInPane(p, Shell.useIdentity(.init(name: i.name, authorName: i.authorName, authorEmail: i.authorEmail, keyPath: i.keyPath)))
                }
                item.indentationLevel = 1
                m.addItem(item)
            }
            if current != nil {
                add("Stop Using Identity") { [weak self] in
                    guard let self, let p = model.tab(id)?.focusedPaneId else { return }
                    runInPane(p, "unset GIT_SSH_COMMAND GIT_AUTHOR_NAME GIT_AUTHOR_EMAIL GIT_COMMITTER_NAME GIT_COMMITTER_EMAIL WTF_GIT_IDENTITY")
                }
            }
            add("Git Identities…") { [weak self] in self?.showGitIdentities() }
        }
        m.addItem(.separator())
        add("Duplicate") { [weak self] in self?.model.openNextTo(id, duplicate: true) }
        let otherWindows = WindowManager.shared.windows.filter { $0 !== self }
        if model.tabs.count > 1 || !otherWindows.isEmpty { m.addItem(.separator()) }
        if model.tabs.count > 1 {
            add("Move to New Window") { [weak self] in self?.moveToNewWindow(id) }
        }
        for other in otherWindows {
            add("Move to “\(other.window?.title ?? other.name)”") { [weak self] in self?.moveTab(id, to: other) }
        }
        m.addItem(.separator())
        add("Rename Tab…") { [weak self] in self?.strip.startRename(id) }
        m.items.last?.toolTip = "Or double-click the tab"
        if t.customTitle != nil {
            add("Use Automatic Name") { [weak self] in self?.model.rename(id, "") }
        }
        m.addItem(.separator())
        add("Close Tab", key: "w", mods: [.command, .option]) { [weak self] in self?.closeTabs([id]) }
        if model.tabs.count > 1 {
            add("Close Other Tabs") { [weak self] in
                guard let self else { return }
                closeTabs(model.tabs.map(\.id).filter { $0 != id })
            }
        }
        if let i = model.index(id), i < model.tabs.count - 1 {
            add("Close Tabs to the Right") { [weak self] in
                guard let self else { return }
                closeTabs(Array(model.tabs[(i + 1)...].map(\.id)))
            }
        }
        return m
    }

    /// The SSH tab's Git section.
    func showGitIdentities() {
        sidebar.sshSection = "git"
        sidebar.tab = .ssh
        sidebar.open = true
        layoutSidebar()
        sidebarView?.update()
    }

    static func dot(_ c: NSColor) -> NSImage {
        NSImage(size: NSSize(width: 10, height: 10), flipped: false) { r in
            c.setFill()
            NSBezierPath(ovalIn: r.insetBy(dx: 1, dy: 1)).fill()
            return true
        }
    }

    private func newGroup(_ id: Int) {
        guard let g = model.createGroup(id) else { return }
        strip.refresh()
        for case .chip(let gid, let r) in strip.items where gid == g { showGroupEditor(g, at: r) }
    }

    private func showGroupEditor(_ g: Int, at rect: NSRect) {
        GroupEditor.show(group: g, in: self, from: strip, at: rect)
    }

    // MARK: Commands (menus, palette)

    var activePane: Pane? { model.activePane }
    var activeTerminal: TerminalController? { model.active.flatMap { terminals[$0.focusedPaneId] } }
    var activeApi: ApiController? { model.active.flatMap { apis[$0.focusedPaneId] } }

    @objc func newTab(_ sender: Any?) { model.open() }
    @objc func splitRight(_ sender: Any?) { model.split(.row) }
    @objc func splitDown(_ sender: Any?) { model.split(.column) }
    @objc func duplicateTab(_ sender: Any?) { model.duplicate() }
    @objc func closePaneOrTab(_ sender: Any?) { if let p = model.active?.focusedPaneId { closePane(p) } }
    @objc func closeTab(_ sender: Any?) { if let id = model.activeId { closeTabs([id]) } }
    @objc func reopenTab(_ sender: Any?) { model.reopen() }
    @objc func closeWindowAction(_ sender: Any?) { _ = windowShouldClose(window!) }
    @objc func moveTabToNewWindow(_ sender: Any?) { if let id = model.activeId { moveToNewWindow(id) } }
    @objc func nextTab(_ sender: Any?) { model.cycle(1) }
    @objc func previousTab(_ sender: Any?) { model.cycle(-1) }
    @objc func moveTabRight(_ sender: Any?) { model.moveActive(1) }
    @objc func moveTabLeft(_ sender: Any?) { model.moveActive(-1) }
    @objc func addTabToNewGroup(_ sender: Any?) { if let id = model.activeId { newGroup(id) } }
    @objc func selectTab(_ sender: Any?) { model.selectNumber((sender as? NSMenuItem)?.tag ?? 1) }
    @objc func renameTab(_ sender: Any?) { if let id = model.activeId { strip.startRename(id) } }
    @objc func toggleZoom(_ sender: Any?) { model.toggleZoom() }
    @objc func nextPane(_ sender: Any?) { model.cyclePane(1) }
    @objc func previousPane(_ sender: Any?) { model.cyclePane(-1) }
    @objc func paneLeft(_ sender: Any?) { model.focusDirection(.left) }
    @objc func paneRight(_ sender: Any?) { model.focusDirection(.right) }
    @objc func paneUp(_ sender: Any?) { model.focusDirection(.up) }
    @objc func paneDown(_ sender: Any?) { model.focusDirection(.down) }
    @objc func showAllTabs(_ sender: Any?) { TabOverview.toggle(in: self) }
    @objc func toggleBroadcast(_ sender: Any?) { Broadcast.shared.toggleActive(in: self) }

    func validateMenuItem(_ item: NSMenuItem) -> Bool {
        let split = model.active?.root.isSplit == true
        switch item.action {
        case #selector(toggleZoom(_:)), #selector(nextPane(_:)), #selector(previousPane(_:)),
             #selector(paneLeft(_:)), #selector(paneRight(_:)), #selector(paneUp(_:)), #selector(paneDown(_:)):
            return split
        case #selector(moveTabToNewWindow(_:)):
            return model.tabs.count > 1
        case #selector(toggleBroadcast(_:)):
            item.title = Broadcast.shared.isBroadcasting(tab: model.activeId ?? -1, in: self) ? "Stop Broadcasting Input" : "Broadcast Input"
            return true
        default:
            return true
        }
    }

    /// Runs a command in a pane: typed at its prompt when idle, else in a new tab.
    func runInPane(_ paneId: Int, _ command: String) {
        guard let p = model.panes[paneId], let term = terminals[paneId], p.isShell, !p.running else {
            model.open(cwd: model.panes[paneId]?.cwd, command: command)
            return
        }
        term.run(command)
    }

    /// The terminals of a tab's panes, for broadcasting.
    func terminals(ofTab id: Int) -> [TerminalController] {
        model.tab(id)?.root.leaves.compactMap { terminals[$0] } ?? []
    }

    var rootView: NSView { root }
}

/// The window's content: the tab strip over the panes.
@MainActor
final class RootView: NSView {
    weak var strip: TabStripView?
    weak var panes: PaneArea?
    /// Between the strip and the panes (the address bar).
    var middle: NSView? {
        didSet {
            oldValue?.removeFromSuperview()
            if let middle { addSubview(middle) }
            needsLayout = true
        }
    }

    override var isFlipped: Bool { true }

    override init(frame: NSRect) {
        super.init(frame: frame)
        wantsLayer = true
    }

    required init?(coder: NSCoder) { fatalError() }

    /// The side pane, left of the terminals and under the tab strip.
    var sidebar: NSView? {
        didSet {
            oldValue?.removeFromSuperview()
            if let sidebar { addSubview(sidebar) }
            needsLayout = true
        }
    }
    var sidebarWidth: CGFloat = 300

    override func layout() {
        super.layout()
        let h = TabStripView.height
        strip?.frame = NSRect(x: 0, y: 0, width: bounds.width, height: h)
        var x: CGFloat = 0
        if let sidebar {
            let w = min(sidebarWidth, max(0, bounds.width - 240))
            sidebar.frame = NSRect(x: 0, y: h, width: w, height: max(0, bounds.height - h))
            x = w + 4
        }
        var y = h
        if let m = middle {
            let mh = m.fittingSize.height > 0 ? m.fittingSize.height : 34
            m.frame = NSRect(x: x, y: y, width: bounds.width - x, height: mh)
            y += mh
        }
        panes?.frame = NSRect(x: x, y: y, width: bounds.width - x, height: max(0, bounds.height - y))
    }
}

/// The address bar, and the broadcast bar under it while input is broadcast.
@MainActor
final class MiddleStack: NSView {
    var address: AddressBar? { didSet { oldValue?.removeFromSuperview(); address.map(addSubview) } }
    var broadcast: BroadcastBar? { didSet { oldValue?.removeFromSuperview(); broadcast.map(addSubview) } }

    override var isFlipped: Bool { true }
    override var fittingSize: NSSize {
        NSSize(width: 0, height: 40 + (broadcast?.isHidden == false ? 34 : 0))
    }

    override func layout() {
        super.layout()
        address?.frame = NSRect(x: 0, y: 0, width: bounds.width, height: 40)
        broadcast?.frame = NSRect(x: 8, y: 40, width: bounds.width - 16, height: 30)
    }
}

/// The window. Double-clicks on the tab strip go to the strip alone: the
/// system title bar under it would otherwise zoom the window as well (a
/// double-click on a tab renames it; on empty space the strip zooms itself).
@MainActor
final class WtfWindow: NSWindow {
    weak var strip: TabStripView?
    /// The keyboard moved to another view.
    var onFirstResponder: ((NSResponder?) -> Void)?

    override func makeFirstResponder(_ responder: NSResponder?) -> Bool {
        let ok = super.makeFirstResponder(responder)
        if ok { onFirstResponder?(responder) }
        return ok
    }

    override func sendEvent(_ event: NSEvent) {
        if event.type == .leftMouseDown, event.clickCount >= 2, let strip, let content = contentView,
           strip.frame.contains(content.convert(event.locationInWindow, from: nil)),
           !trafficLights.contains(where: { $0.contains(event.locationInWindow) }) {
            strip.mouseDown(with: event)
            return
        }
        super.sendEvent(event)
    }

    /// The close, minimise and zoom buttons, in window coordinates.
    private var trafficLights: [NSRect] {
        [.closeButton, .miniaturizeButton, .zoomButton].compactMap { kind in
            standardWindowButton(kind).map { $0.convert($0.bounds, to: nil) }
        }
    }
}

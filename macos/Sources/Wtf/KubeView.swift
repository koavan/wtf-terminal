import AppKit
import WtfCore

/// The Kubernetes tab: Resources (what the focused shell's context holds)
/// and Contexts (the kubeconfigs). Only the shown view exists. Like
/// internal/gtkui/kubetab.go.
@MainActor
final class KubeView: NSView, SidePaneTab {
    private unowned let owner: WindowController
    private let segments = NSSegmentedControl(labels: ["Resources", "Contexts"], trackingMode: .selectOne, target: nil, action: nil)
    private var body: NSView?

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        segments.segmentDistribution = .fillEqually
        segments.target = self
        segments.action = #selector(switched)
        segments.refusesFirstResponder = true
        addSubview(segments)
        show(window.sidebar.kubeView)
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    @objc private func switched() {
        let v = segments.selectedSegment == 1 ? "contexts" : "cluster"
        owner.sidebar.kubeView = v
        owner.sidebarChanged()
        show(v)
    }

    func show(_ v: String) {
        segments.selectedSegment = v == "contexts" ? 1 : 0
        body?.removeFromSuperview()
        let b: NSView = v == "contexts" ? KubeContextsSection(window: owner, parent: self) : KubeResources(window: owner)
        body = b
        addSubview(b)
        needsLayout = true
    }

    func paneChanged() { (body as? SidePaneTab)?.paneChanged() }
    func commandFinished() { (body as? SidePaneTab)?.commandFinished() }

    /// Clicks section headings as the mouse would (debug self-checks).
    func debugClick(_ titles: [String]) -> [Bool] {
        guard let r = body as? KubeResources else { return [] }
        return titles.map { r.clickHeading($0) }
    }

    func debugSummary() -> String {
        let s = owner.kubeState
        return "kube: " + (s.target.map { "context \($0.context), namespace \($0.namespace)" } ?? "no target: \(s.targetError)") + ", open: \(s.open.sorted())"
    }

    override func layout() {
        super.layout()
        segments.frame = NSRect(x: 6, y: 4, width: bounds.width - 12, height: 24)
        body?.frame = NSRect(x: 0, y: 32, width: bounds.width, height: max(0, bounds.height - 32))
    }
}

/// The Resources view's state, per window: kept while the window lives so
/// open sections come back as they were.
@MainActor
final class KubeState {
    final class Section {
        var items: Any?
        var error = ""
        var loading = false
        var updatedAt: Date?
        var seq = 0
    }

    var target: Go.kube.Target?
    var targetError = ""
    var targetSeq = 0
    var sections: [String: Section] = [:]
    var open = Set<String>()
    /// Namespace picked here per context; absent follows the shell.
    var namespaces: [String: String] = [:]
    var namespaceList: [String]?
    var namespacesFor = ""
    var deployment: Go.kube.Deployment?

    func section(_ id: String) -> Section {
        if let s = sections[id] { return s }
        let s = Section()
        sections[id] = s
        return s
    }

    func clear() {
        for s in sections.values {
            s.items = nil
            s.error = ""
            s.loading = false
            s.updatedAt = nil
            s.seq += 1
        }
        deployment = nil
    }
}

@MainActor
private var kubeStates: [ObjectIdentifier: KubeState] = [:]

extension WindowController {
    var kubeState: KubeState {
        if let s = kubeStates[ObjectIdentifier(self)] { return s }
        let s = KubeState()
        kubeStates[ObjectIdentifier(self)] = s
        return s
    }
}

// MARK: - Resources

@MainActor
final class KubeResources: NSView, SidePaneTab, NSComboBoxDelegate, NSSearchFieldDelegate {
    static let groups: [(String, [String])] = [
        ("Workloads", ["deployments", "statefulsets", "daemonsets", "jobs", "cronjobs", "pods"]),
        ("Network", ["services", "ingresses"]),
        ("Config", ["configmaps", "secrets"]),
        ("Storage", ["pvcs"]),
        ("Scaling", ["hpas"]),
        ("Cluster", ["nodes", "events"]),
        ("Access", ["permissions"]),
    ]

    /// Read when opened and when the context or namespace changes, never on the poll.
    static let unpolled: Set<String> = ["permissions"]

    private unowned let owner: WindowController
    private var state: KubeState { owner.kubeState }
    private let header = PaneHeader("Kubernetes")
    private let nsLabel = NSTextField(labelWithString: "Namespace")
    private let ns = NSComboBox()
    private let paused = NSTextField(labelWithString: "Paused")
    private let filter = NSSearchField()
    private let list = SideList()
    private var noteLink: NSButton?
    private var note: NoteView?
    private var timer: Timer?
    private var lastShell: Kube.Target?

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        header.refreshControls(in: self) { [weak self] in self?.resolve() }
        addSubview(header)
        nsLabel.font = .systemFont(ofSize: 11.5)
        nsLabel.textColor = .secondaryLabelColor
        addSubview(nsLabel)
        ns.font = .monospacedSystemFont(ofSize: 11.5, weight: .regular)
        ns.refusesFirstResponder = true
        ns.toolTip = "Shows another namespace here; the shell keeps its own"
        ns.delegate = self
        ns.target = self
        ns.action = #selector(namespaceCommitted)
        addSubview(ns)
        paused.font = .systemFont(ofSize: 11)
        paused.textColor = .secondaryLabelColor
        addSubview(paused)
        filter.placeholderString = "Filter by name, status, node"
        filter.delegate = self
        filter.sendsSearchStringImmediately = true
        // The side pane takes the keyboard only when clicked into.
        filter.refusesFirstResponder = true
        addSubview(filter)
        list.onClick = { [weak self] id in
            guard let self, id.hasPrefix("deployments:"), let d = deployments.first(where: { "deployments:\($0.name)" == id }) else { return }
            select(d)
        }
        list.onDoubleClick = { [weak self] id in self?.doubleClicked(id) }
        list.menuProvider = { [weak self] ids in ids.first.flatMap { self?.menu($0) } }
        addSubview(list)
        Settings.shared.observe(self) { $0.startPolling() }
        resolve()
        startPolling()
    }

    required init?(coder: NSCoder) { fatalError() }

    deinit { MainActor.assumeIsolated { timer?.invalidate() } }

    override var isFlipped: Bool { true }

    override func removeFromSuperview() {
        timer?.invalidate()
        timer = nil
        super.removeFromSuperview()
    }

    override func layout() {
        super.layout()
        var y: CGFloat = 0
        header.frame = NSRect(x: 0, y: y, width: bounds.width, height: 30)
        y += 30
        let showNs = state.target != nil
        nsLabel.isHidden = !showNs
        ns.isHidden = !showNs
        if showNs {
            nsLabel.frame = NSRect(x: 10, y: y + 4, width: 70, height: 16)
            ns.frame = NSRect(x: 80, y: y, width: bounds.width - 88, height: 24)
            y += 28
        }
        let anyOpen = !state.open.isEmpty
        let auto = Settings.shared.config?.sidebarAutoRefresh ?? true
        paused.isHidden = !(anyOpen && !auto)
        if !paused.isHidden {
            paused.stringValue = Poller.pausedText(state.open.compactMap { state.section($0).updatedAt }.max())
            paused.frame = NSRect(x: 10, y: y, width: bounds.width - 20, height: 16)
            y += 18
        }
        filter.isHidden = !anyOpen
        if anyOpen {
            filter.frame = NSRect(x: 8, y: y + 2, width: bounds.width - 16, height: 24)
            y += 30
        }
        if let note {
            note.frame = NSRect(x: 0, y: y, width: bounds.width, height: note.height(for: bounds.width))
            y += note.frame.height
            if let l = noteLink {
                l.frame = NSRect(x: NoteView.inset - 4, y: y - 6, width: l.fittingSize.width, height: 22)
                y += 20
            }
        }
        list.frame = NSRect(x: 0, y: y, width: bounds.width, height: max(0, bounds.height - y))
    }

    // MARK: Data

    private var shell: Kube.Target {
        let p = owner.activePane
        return Kube.Target(context: p?.infra?.kubeContext ?? "", namespace: p?.infra?.kubeNamespace ?? "", env: p?.env ?? [:])
    }

    /// The target as commands use it: its context and namespace, the shell's variables.
    private var commandTarget: Kube.Target? {
        guard let t = state.target else { return nil }
        return Kube.Target(context: t.context, namespace: t.namespace, env: shell.env)
    }

    func paneChanged() {
        let s = shell
        if s != lastShell { resolve() }
    }

    /// Works out which context and namespace to show, then reads the open sections.
    func resolve() {
        let s = shell
        lastShell = s
        state.targetSeq += 1
        let mine = state.targetSeq
        let req = Go.kube.Request(env: s.env, context: s.context, namespace: state.namespaces[s.context] ?? "")
        Task {
            do {
                let t = try await Services.kube.Target(req)
                guard mine == state.targetSeq else { return }
                if t.context != state.target?.context || t.namespace != state.target?.namespace { state.clear() }
                if state.namespacesFor != t.context { state.namespaceList = nil }
                state.target = t
                state.targetError = ""
                refreshOpen()
            } catch {
                guard mine == state.targetSeq else { return }
                state.target = nil
                state.targetError = error.localizedDescription
                state.clear()
            }
            rebuild()
        }
    }

    /// Reads the open sections; `poll` leaves out the ones only read on request.
    private func refreshOpen(poll: Bool = false) {
        for id in state.open where !(poll && Self.unpolled.contains(id) && state.section(id).items != nil) { refresh(id) }
        rebuild()
    }

    private func refresh(_ id: String) {
        guard let t = state.target else { return }
        // The click self-check opens sections without reading the cluster.
        if ProcessInfo.processInfo.environment["WTF_NATIVE_DEBUG"] == "kubeclicks" {
            Debug.log("would read \(id) from \(t.context)")
            return
        }
        let s = state.section(id)
        guard !s.loading else { return }
        s.seq += 1
        s.loading = true
        let mine = s.seq
        let req = Go.kube.Request(env: shell.env, context: t.context, namespace: t.namespace)
        let selector = state.deployment?.selector ?? [:]
        Task {
            do {
                let items: Any = switch id {
                case "deployments": try await Services.kube.Deployments(req)
                case "pods": try await Services.kube.Pods(req, selector)
                case "events": try await Services.kube.Events(req)
                case "permissions": try await Services.kube.Permissions(req)
                default: try await Services.kube.Table(req, id)
                }
                guard mine == s.seq else { return }
                s.items = items
                s.error = ""
                s.updatedAt = Date()
            } catch {
                guard mine == s.seq else { return }
                s.error = error.localizedDescription
            }
            s.loading = false
            rebuild()
        }
    }

    private func toggle(_ id: String) {
        if state.open.contains(id) {
            state.open.remove(id)
            let s = state.section(id)
            s.seq += 1
            s.loading = false
        } else {
            state.open.insert(id)
            refresh(id)
        }
        rebuild()
    }

    private func select(_ d: Go.kube.Deployment?) {
        state.deployment = d != nil && state.deployment?.name != d?.name ? d : nil
        let pods = state.section("pods")
        pods.items = nil
        pods.seq += 1
        pods.loading = false
        state.open.insert("pods")
        refresh("pods")
        rebuild()
    }

    /// Refreshes open sections every max(setting, 10s), only while the window is in front.
    private func startPolling() {
        timer?.invalidate()
        timer = nil
        let cfg = Settings.shared.config
        guard cfg?.sidebarAutoRefresh ?? true else { rebuild(); return }
        let interval = max(10, max(1, cfg?.sidebarRefreshInterval ?? 3))
        timer = Timer.scheduledTimer(withTimeInterval: interval, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated {
                guard let self, self.window?.isKeyWindow == true, NSApp.isActive else { return }
                self.refreshOpen(poll: true)
            }
        }
        rebuild()
    }

    // MARK: Building

    private var deployments: [Go.kube.Deployment] { state.section("deployments").items as? [Go.kube.Deployment] ?? [] }

    private var query: String { filter.stringValue.trimmingCharacters(in: .whitespaces).lowercased() }

    private func matches(_ text: String) -> Bool { query.isEmpty || text.lowercased().contains(query) }

    private func rebuild() {
        let t = state.target
        header.text = t.map { Badges.shortClusterName($0.cluster.isEmpty ? $0.context : $0.cluster) } ?? (shell.context.isEmpty ? "Kubernetes" : shell.context)
        header.tooltip = t.map { "Context: \($0.context)\nCluster: \($0.cluster)\nFollows the focused shell's context" }
        if let t, ns.currentEditor() == nil { ns.stringValue = t.namespace }
        if let l = state.namespaceList, ns.numberOfItems != l.count {
            ns.removeAllItems()
            ns.addItems(withObjectValues: l)
        }
        ns.toolTip = shell.namespace.isEmpty ? "The namespace to show" : "The namespace to show. The shell’s is " + shell.namespace
        note?.removeFromSuperview()
        note = nil
        noteLink?.removeFromSuperview()
        noteLink = nil
        if !state.targetError.isEmpty {
            note = paneNote("Can’t find a Kubernetes context", state.targetError)
            noteLink = owner.installLink(for: state.targetError)
            if let noteLink { addSubview(noteLink) }
        } else if t != nil, state.open.isEmpty {
            note = paneNote(nil, "Open a section to load it. Only open sections are read from the cluster, and refreshed.")
        }
        if let note { addSubview(note) }
        guard t != nil else {
            list.set([])
            needsLayout = true
            return
        }
        let dark = Settings.shared.theme?.dark ?? true
        func dot(_ tone: Kube.Tone?) -> NSColor? {
            switch tone {
            case .ok: NSColor(hex: dark ? "#5bb98c" : "#2f9e6a")
            case .wait: NSColor(hex: dark ? "#fdd663" : "#c98a00")
            case .bad: NSColor(hex: dark ? "#e5484d" : "#d93036")
            case .done: NSColor.secondaryLabelColor.withAlphaComponent(0.5)
            case nil: nil
            }
        }
        var sections: [SideList.Section] = []
        for (group, ids) in Self.groups {
            for (i, id) in ids.enumerated() {
                let s = state.section(id)
                let open = state.open.contains(id)
                let label = Self.label(id)
                let note = Kube.tableKinds.first { $0.id == id }?.note ?? ""
                var rows: [SideList.Row] = []
                if !s.error.isEmpty {
                    let tool = Tools.missingTool(s.error)
                    rows.append(.init(id: "\(id):!error", symbol: "exclamationmark.triangle", tint: .systemRed, title: s.error, tip: s.error,
                                      button: tool.map { t in ("wrench.and.screwdriver", "Show how to install it", { [weak self] in
                                          guard let self else { return }
                                          ToolsDialog.show(focus: t, in: owner)
                                      }) }))
                }
                switch id {
                case "deployments":
                    for d in deployments where matches(d.name) {
                        // One red line per problem.
                        var row = SideList.Row(id: "deployments:\(d.name)", symbol: "", title: d.name, mono: true, detail: Kube.deploymentDetail(d),
                                               alert: d.problems.first ?? "",
                                               tip: (d.images + d.problems).joined(separator: "\n"), dot: dot(Kube.tone(d)),
                                               highlighted: state.deployment?.name == d.name)
                        row.more = d.problems.dropFirst().map { (text: $0, color: NSColor.systemRed) }
                        rows.append(row)
                    }
                case "pods":
                    for p in s.items as? [Go.kube.Pod] ?? [] where matches("\(p.name) \(p.status) \(p.node)") {
                        rows.append(.init(id: "pods:\(p.name)", symbol: "", title: p.name, mono: true, detail: Kube.podDetail(p),
                                          tip: Kube.podTip(p), dot: dot(Kube.tone(p))))
                    }
                case "events":
                    for (n, e) in (s.items as? [Go.kube.Event] ?? []).enumerated() where matches("\(e.object) \(e.reason) \(e.message)") {
                        let warn = e.type == "Warning"
                        rows.append(.init(id: "events:\(n)", symbol: warn ? "exclamationmark.triangle" : "info.circle",
                                          tint: warn ? .systemRed : .secondaryLabelColor,
                                          title: "\(e.reason) \(e.object)\(e.count > 1 ? " ×\(e.count)" : "") · \(Kube.age(e.last))",
                                          detail: e.message, tip: "\(e.type) · \(e.last)"))
                    }
                case "permissions":
                    guard let p = s.items as? Go.kube.Permissions else { break }
                    if !p.user.isEmpty, matches(p.user) {
                        rows.append(.init(id: "permissions:!user", symbol: "person.crop.circle", tint: .secondaryLabelColor, title: "You: \(p.user)",
                                          detail: p.groups.isEmpty ? "" : "\(p.groups.count) group\(p.groups.count == 1 ? "" : "s")",
                                          tip: ([p.user] + p.groups.map { "group: \($0)" }).joined(separator: "\n")))
                    }
                    if p.admin {
                        rows.append(.init(id: "permissions:!admin", symbol: "exclamationmark.shield", tint: .systemOrange,
                                          title: "Full access: every verb on every resource", tip: "A rule allows * on *.* in this namespace"))
                    }
                    for (n, r) in p.rules.enumerated() where matches("\(r.resource) \(r.verbs.joined(separator: " ")) \(r.names.joined(separator: " "))") {
                        rows.append(.init(id: "permissions:\(n)", symbol: r.nonResource ? "link" : "cube", tint: .secondaryLabelColor,
                                          title: r.resource, mono: true, detail: Kube.ruleDetail(r),
                                          tip: "\(r.nonResource ? "URL" : "Resource"): \(r.resource)\nVerbs: \(r.verbs.joined(separator: ", "))"
                                              + (r.names.isEmpty ? "" : "\nOnly: \(r.names.joined(separator: ", "))")))
                    }
                default:
                    let table = s.items as? Go.kube.Table
                    for r in table?.rows ?? [] {
                        var cells: [String: String] = [:]
                        for (ci, col) in (table?.columns ?? []).enumerated() where ci < r.count { cells[col] = r[ci] }
                        let name = r.first ?? ""
                        guard matches("\(name) \(r.joined(separator: " "))") else { continue }
                        let tip = (table?.columns ?? []).compactMap { c in cells[c].flatMap { $0.isEmpty || $0 == "<none>" ? nil : "\(c): \($0)" } }
                        rows.append(.init(id: "\(id):\(name)", symbol: "", title: name, mono: true, detail: Kube.rowSummary(id, cells),
                                          tip: tip.joined(separator: "\n"), dot: dot(Kube.rowTone(id, cells))))
                    }
                }
                let count = rows.filter { !$0.id.hasSuffix(":!error") && !$0.id.hasSuffix(":!filter") }.count
                let loaded = s.items != nil
                var badge = ""
                if open { badge = loaded ? "\(count)" : (s.loading ? "…" : "") }
                let title = label
                // Pods of one deployment: a row says so, with a way back to all.
                if id == "pods", open, let d = state.deployment {
                    rows.insert(.init(id: "pods:!filter", symbol: "line.3.horizontal.decrease.circle", tint: .controlAccentColor,
                                      title: "Pods of deployment \(d.name)", tip: "Click × to show all pods",
                                      button: ("xmark.circle.fill", "Show all pods", { [weak self] in self?.select(nil) })), at: 0)
                }
                let emptyText: String? = loaded && count == 0
                    ? (id == "events" ? "No recent events" : id == "permissions" ? (query.isEmpty ? "Nothing allowed in this namespace" : "No permissions match") : "No \(label.lowercased())\(query.isEmpty && !(id == "pods" && state.deployment != nil) ? "" : " match")")
                    : nil
                sections.append(.init(title: title, tip: (open ? "Collapse \(label)" : "Show \(label)") + (note.isEmpty ? "" : "\n\(note)"),
                                      rows: rows, toggle: { [weak self] in self?.toggle(id) }, open: open, badge: badge,
                                      group: i == 0 ? group : nil, empty: emptyText))
            }
        }
        list.set(sections)
        needsLayout = true
    }

    func clickHeading(_ title: String) -> Bool { list.clickHeading(title) }

    static func label(_ id: String) -> String {
        switch id {
        case "deployments": "Deployments"
        case "pods": "Pods"
        case "events": "Events"
        case "permissions": "Permissions"
        default: Kube.tableKinds.first { $0.id == id }?.label ?? id
        }
    }

    // MARK: Namespace

    func comboBoxWillPopUp(_ note: Notification) { loadNamespaces() }
    func controlTextDidBeginEditing(_ obj: Notification) { if obj.object as? NSComboBox === ns { loadNamespaces() } }
    func controlTextDidChange(_ obj: Notification) { if obj.object as? NSSearchField === filter { rebuild() } }

    private func loadNamespaces() {
        guard let t = state.target, state.namespacesFor != t.context else { return }
        state.namespacesFor = t.context
        Task {
            state.namespaceList = try? await Services.kube.Namespaces(Go.kube.Request(env: shell.env, context: t.context, namespace: t.namespace))
            rebuild()
        }
    }

    @objc private func namespaceCommitted() {
        let v = ns.stringValue.trimmingCharacters(in: .whitespaces)
        guard let t = state.target, !v.isEmpty, v != t.namespace else {
            ns.stringValue = state.target?.namespace ?? ""
            return
        }
        let ctx = t.context
        if v == shell.namespace, ctx == shell.context { state.namespaces[ctx] = nil } else { state.namespaces[ctx] = v }
        resolve()
    }

    // MARK: Actions

    private func newTab(_ cmd: String) { owner.model.open(command: cmd) }

    /// Typed into the focused shell without the variables (it has them), or
    /// a new tab with them.
    private func here(_ build: (Kube.Target) -> String) {
        guard let ct = commandTarget else { return }
        var bare = ct
        bare.env = [:]
        if let p = owner.activePane, p.isShell, !p.running, owner.terminals[p.id] != nil {
            owner.runInPane(p.id, build(bare))
        } else {
            owner.model.open(cwd: owner.activePane?.cwd, command: build(ct))
        }
    }

    private func copy(_ text: String, _ toast: String) {
        Clipboard.copy(text)
        Toast.show(toast, in: self)
    }

    private var whereText: String {
        guard let t = state.target else { return "" }
        return "Context \(t.context)\(t.cluster.isEmpty ? "" : " (cluster \(Badges.shortClusterName(t.cluster)))"), namespace \(t.namespace)."
    }

    /// Confirms a command that changes the cluster, showing it, then runs it in a new tab.
    private func confirm(_ title: String, _ button: String, _ command: String) {
        Confirm.show(in: window, title: title, message: "\(whereText) This runs in a new tab:", items: [command], button: button) { [weak self] in
            self?.newTab(command)
        }
    }

    private func doubleClicked(_ id: String) {
        guard let ct = commandTarget else { return }
        if id.hasPrefix("pods:"), let p = pod(id) {
            newTab(Kube.logs(ct, pod: p.name, container: p.containers.count > 1 ? p.containers[0].name : nil))
        } else if id.hasPrefix("secrets:") {
            SecretViewer.show(name: String(id.dropFirst("secrets:".count)), target: ct, in: owner)
        }
    }

    private func pod(_ id: String) -> Go.kube.Pod? {
        (state.section("pods").items as? [Go.kube.Pod])?.first { "pods:\($0.name)" == id }
    }

    private func menu(_ id: String) -> NSMenu? {
        guard let ct = commandTarget, let colon = id.firstIndex(of: ":") else { return nil }
        let section = String(id[..<colon]), name = String(id[id.index(after: colon)...])
        let m = NSMenu()
        m.autoenablesItems = false
        func add(_ t: String, _ run: @escaping () -> Void) { m.addItem(ClosureMenuItem(title: t, keyEquivalent: "", action: run)) }
        func heading(_ t: String) {
            let i = NSMenuItem(title: t, action: nil, keyEquivalent: "")
            i.isEnabled = false
            m.addItem(i)
        }
        func portForward(_ resource: String, _ port: String = "") {
            var d = TunnelDraft()
            d.kind = "kubectl"
            d.name = String(resource.split(separator: "/").last ?? "")
            d.context = ct.context
            d.namespace = ct.namespace
            d.resource = resource
            d.listenPort = port
            d.destPort = port
            d.env = TunnelDraft.kubectlEnv(ct.env)
            TunnelEditor.show(nil, host: nil, prefill: d, in: owner)
        }
        switch section {
        case "permissions":
            guard let p = state.section("permissions").items as? Go.kube.Permissions else { return nil }
            if name == "!user" {
                add("Copy User Name") { [weak self] in self?.copy(p.user, "Copied user name") }
                return m
            }
            guard let n = Int(name), n < p.rules.count else { return nil }
            let r = p.rules[n]
            add(r.nonResource ? "Copy URL" : "Copy Resource") { [weak self] in self?.copy(r.resource, "Copied \(r.resource)") }
            return m
        case "pods":
            guard let p = pod(id) else { return nil }
            let many = p.containers.count > 1
            if many {
                heading("Logs")
                for c in p.containers { add(c.name) { [weak self] in self?.newTab(Kube.logs(ct, pod: p.name, container: c.name)) } }
                heading("Shell")
                for c in p.containers { add(c.name) { [weak self] in self?.here { Kube.exec($0, pod: p.name, container: c.name) } } }
            } else {
                add("Logs") { [weak self] in self?.newTab(Kube.logs(ct, pod: p.name)) }
                add("Shell (exec)") { [weak self] in self?.here { Kube.exec($0, pod: p.name) } }
            }
            for c in p.containers where c.restarts > 0 {
                add(many ? "Logs Before Last Restart: \(c.name)" : "Logs Before Last Restart") { [weak self] in
                    self?.newTab(Kube.logs(ct, pod: p.name, container: many ? c.name : nil, previous: true))
                }
            }
            m.addItem(.separator())
            add("Describe") { [weak self] in self?.here { Kube.describe($0, "pod", p.name) } }
            add("Port Forward…") { portForward("pod/\(p.name)") }
            m.addItem(.separator())
            add("Copy Name") { [weak self] in self?.copy(p.name, "Copied pod name") }
            if !p.ip.isEmpty { add("Copy IP (\(p.ip))") { [weak self] in self?.copy(p.ip, "Copied pod IP") } }
            m.addItem(.separator())
            add("Delete Pod…") { [weak self] in self?.confirm("Delete pod \(p.name)?", "Delete Pod", Kube.deletePod(ct, p.name)) }
        case "deployments":
            guard let d = deployments.first(where: { $0.name == name }) else { return nil }
            add(state.deployment?.name == d.name ? "Show All Pods" : "Show Its Pods") { [weak self] in self?.select(d) }
            add("Logs (all pods)") { [weak self] in self?.newTab(Kube.workloadLogs(ct, "deployment", name)) }
            add("Describe") { [weak self] in self?.here { Kube.describe($0, "deployment", name) } }
            add("View YAML") { [weak self] in self?.newTab(Kube.yaml(ct, "deployment", name)) }
            add("Rollout Status") { [weak self] in self?.newTab(Kube.rolloutStatus(ct, "deployment", name)) }
            add("Rollout History") { [weak self] in self?.newTab(Kube.rolloutHistory(ct, "deployment", name)) }
            add("Port Forward…") { portForward("deployment/\(name)") }
            m.addItem(.separator())
            add("Restart…") { [weak self] in self?.confirm("Restart \(name)?", "Restart", Kube.restart(ct, "deployment", name)) }
            add("Scale…") { [weak self] in self?.scale("deployment", name, d.desired) }
            m.addItem(.separator())
            add("Copy Name") { [weak self] in self?.copy(name, "Copied deployment name") }
        case "events":
            return nil
        default:
            guard let kind = Kube.tableKinds.first(where: { $0.id == section }) else { return nil }
            let k = kind.kind
            let table = state.section(section).items as? Go.kube.Table
            let row = table?.rows.first { $0.first == name } ?? []
            var cells: [String: String] = [:]
            for (ci, col) in (table?.columns ?? []).enumerated() where ci < row.count { cells[col] = row[ci] }
            switch section {
            case "statefulsets", "daemonsets":
                add("Logs (all pods)") { [weak self] in self?.newTab(Kube.workloadLogs(ct, k, name)) }
                add("Rollout Status") { [weak self] in self?.newTab(Kube.rolloutStatus(ct, k, name)) }
                m.addItem(.separator())
                add("Restart…") { [weak self] in self?.confirm("Restart \(name)?", "Restart", Kube.restart(ct, k, name)) }
                if section == "statefulsets" {
                    let current = Int(cells["READY"]?.split(separator: "/").last ?? "") ?? 0
                    add("Scale…") { [weak self] in self?.scale(k, name, current) }
                }
                m.addItem(.separator())
            case "jobs":
                add("Logs") { [weak self] in self?.newTab(Kube.workloadLogs(ct, "job", name)) }
                m.addItem(.separator())
            case "cronjobs":
                add("Run Now…") { [weak self] in self?.confirm("Run \(name) now?", "Create Job", Kube.triggerCronJob(ct, name)) }
                m.addItem(.separator())
            case "services":
                add("Port Forward…") { portForward("service/\(name)", Kube.firstPort(cells["PORT(S)"] ?? "")) }
                m.addItem(.separator())
            case "ingresses":
                let hosts = (cells["HOSTS"] ?? "").split(separator: ",").map(String.init).filter { !$0.isEmpty && $0 != "*" }
                for h in hosts { add("Open https://\(h)") { NSWorkspace.shared.open(URL(string: "https://\(h)")!) } }
                if !hosts.isEmpty { m.addItem(.separator()) }
            case "secrets":
                add("View Decoded Values…") { [weak self] in
                    guard let self else { return }
                    SecretViewer.show(name: name, target: ct, in: owner)
                }
                add("Decode in Terminal") { [weak self] in self?.newTab(Kube.decodeSecret(ct, name)) }
                m.addItem(.separator())
            default: break
            }
            add("Describe") { [weak self] in self?.here { Kube.describe($0, k, name) } }
            if section != "secrets" { add("View YAML") { [weak self] in self?.newTab(Kube.yaml(ct, k, name)) } }
            m.addItem(.separator())
            add("Copy Name") { [weak self] in self?.copy(name, "Copied \(k) name") }
        }
        return m
    }

    /// Scale: asks how many replicas, shows the command, runs it in a new tab.
    private func scale(_ kind: String, _ name: String, _ current: Int) {
        guard let ct = commandTarget else { return }
        let s = FormSheet(title: "Scale \(name)", subtitle: "\(whereText) It has \(current) \(current == 1 ? "replica" : "replicas") now.")
        let field = s.text(String(current))
        let preview = s.note("", mono: true)
        func command() -> String? {
            let v = field.stringValue.trimmingCharacters(in: .whitespaces)
            guard v.range(of: #"^\d+$"#, options: .regularExpression) != nil, let n = Int(v), n <= 1000 else {
                s.error = "Enter a number of replicas, 0 or more"
                return nil
            }
            if n == current {
                s.error = "It already has \(current)"
                return nil
            }
            s.error = nil
            return Kube.scale(ct, kind, name, n)
        }
        let watch = FieldWatcher {
            preview.stringValue = command().map { "\($0)\nRuns in a new tab." } ?? ""
            s.button("Scale")?.isEnabled = command() != nil
        }
        field.delegate = watch
        s.field("Replicas", field)
        s.add(preview)
        s.buttons([("Scale", true, { [weak self] in
            _ = watch
            guard let c = command() else { return }
            s.close()
            self?.newTab(c)
        })])
        s.present(in: window, focus: field)
        s.button("Scale")?.isEnabled = false
    }
}

// MARK: - Contexts

@MainActor
final class KubeContextsSection: NSView, SidePaneTab, NSSearchFieldDelegate {
    private unowned let owner: WindowController
    private weak var parent: KubeView?
    private let filter = NSSearchField()
    private let list = SideList()
    private var note: NoteView?
    private var configs = Go.kube.ConfigList()
    private var loaded = false
    /// Test Connection results, kept per window while it lives (switching views keeps them).
    private static var checksByWindow: [ObjectIdentifier: [String: (busy: Bool, result: String, error: String)]] = [:]
    private var checks: [String: (busy: Bool, result: String, error: String)] {
        get { Self.checksByWindow[ObjectIdentifier(owner)] ?? [:] }
        set { Self.checksByWindow[ObjectIdentifier(owner)] = newValue }
    }
    private var lastEnv: [String: String]?

    init(window: WindowController, parent: KubeView) {
        owner = window
        self.parent = parent
        super.init(frame: .zero)
        filter.placeholderString = "Filter contexts"
        filter.delegate = self
        filter.sendsSearchStringImmediately = true
        // The side pane takes the keyboard only when clicked into.
        filter.refusesFirstResponder = true
        addSubview(filter)
        let add = ToolButton(symbol: "plus", tip: "Add a cluster") { [weak self] in self?.showAddMenu() }
        add.identifier = .init("add")
        let reload = ToolButton(symbol: "arrow.clockwise", tip: "Reload") { [weak self] in self?.load() }
        reload.identifier = .init("reload")
        addSubview(add)
        addSubview(reload)
        list.onDoubleClick = { [weak self] id in self?.context(id).map { self?.use($0) } }
        list.menuProvider = { [weak self] ids in ids.first.flatMap { self?.context($0) }.flatMap { self?.menu($0) } }
        addSubview(list)
        load()
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    override func layout() {
        super.layout()
        filter.frame = NSRect(x: 8, y: 4, width: bounds.width - 70, height: 24)
        subviews.first { $0.identifier?.rawValue == "add" }?.frame = NSRect(x: bounds.width - 58, y: 4, width: 24, height: 24)
        subviews.first { $0.identifier?.rawValue == "reload" }?.frame = NSRect(x: bounds.width - 32, y: 4, width: 24, height: 24)
        list.frame = NSRect(x: 0, y: 34, width: bounds.width, height: max(0, bounds.height - 34))
        note?.frame = NSRect(x: 0, y: 34, width: bounds.width, height: note?.height(for: bounds.width) ?? 0)
    }

    private var env: [String: String] { owner.activePane?.env ?? [:] }

    func paneChanged() { if env != lastEnv { load() } }

    func commandFinished() {
        if loaded, let c = owner.activePane?.lastCommand?.text, Kube.changesKubeconfig(c) { load() }
    }

    func load() {
        lastEnv = env
        Task {
            do { configs = try await Services.kube.Contexts(env) } catch { configs.problems = [error.localizedDescription] }
            loaded = true
            rebuild()
        }
    }

    func controlTextDidChange(_ obj: Notification) { rebuild() }

    private func key(_ c: Go.kube.KubeContext) -> String { c.file + "\n" + c.name }
    private func context(_ id: String) -> Go.kube.KubeContext? { configs.contexts.first { key($0) == id } }

    private func rebuild() {
        let q = filter.stringValue.trimmingCharacters(in: .whitespaces).lowercased()
        let shown = configs.contexts.filter { q.isEmpty || "\($0.name) \($0.cluster) \($0.server) \($0.namespace) \($0.eksCluster)".lowercased().contains(q) }
        let inUse = owner.activePane?.infra?.kubeContext ?? ""
        func row(_ c: Go.kube.KubeContext) -> SideList.Row {
            var pills: [SideList.Pill] = []
            if c.active, c.name == inUse { pills.append(.init(text: "in use", color: .controlAccentColor, tip: "The focused shell uses this context")) }
            if c.current { pills.append(.init(text: "default", tip: "The kubeconfig's current-context: what new shells use")) }
            var issues = c.problems
            if c.auth == "exec", !c.pluginFound { issues.append("\(c.execCommand) isn’t on this shell’s PATH") }
            let detail = Kube.contextDetail(c)
            // A test's answer is a line of its own, under the context's details.
            var answer = "", answerColor = NSColor.secondaryLabelColor
            if let ch = checks[key(c)] {
                if ch.busy { answer = "Asking the cluster…" } else if !ch.result.isEmpty { answer = ch.result; answerColor = .systemGreen } else if !ch.error.isEmpty { issues.append(ch.error) }
            }
            var tip = (c.active ? "" : "From \(WindowModel.prettyPath(c.file))\n") + "Cluster: \(c.cluster)\(c.server.isEmpty ? "" : " (\(c.server))")\nUser: \(c.user)"
            if !c.execCommand.isEmpty { tip += "\nLogs in with: \(c.execCommand) \(c.execArgs.joined(separator: " "))" }
            if !c.execEnv.isEmpty { tip += "\nWith: \(c.execEnv.joined(separator: ", "))" }
            return .init(id: key(c), symbol: "helm", tint: c.active && c.name == inUse ? .controlAccentColor : .secondaryLabelColor,
                         title: c.name, mono: true, detail: detail, alert: issues.isEmpty ? answer : issues.joined(separator: " · "),
                         alertColor: issues.isEmpty ? answerColor : .systemRed, pills: pills, tip: tip,
                         button: ("play.fill", "Use in this shell", { [weak self] in self?.use(c) }))
        }
        var sections: [SideList.Section] = []
        let active = shown.filter(\.active)
        if !active.isEmpty {
            sections.append(.init(title: "Used by this shell · " + configs.files.map { WindowModel.prettyPath($0) }.joined(separator: ", "),
                                  tip: "The files kubectl reads in the focused shell (its KUBECONFIG, or ~/.kube/config)", rows: active.map(row)))
        }
        for f in configs.otherFiles {
            let rows = shown.filter { !$0.active && $0.file == f }
            if !rows.isEmpty {
                sections.append(.init(title: WindowModel.prettyPath(f), tip: "\(f)\nNot read by the focused shell; using one of its contexts points the shell at this file only",
                                      rows: rows.map(row)))
            }
        }
        if !configs.problems.isEmpty {
            sections.append(.init(title: "Problems", rows: configs.problems.map { .init(id: "p:\($0)", symbol: "exclamationmark.triangle", tint: .systemRed, title: $0) }))
        }
        list.set(sections)
        note?.removeFromSuperview()
        note = nil
        if loaded, shown.isEmpty {
            note = q.isEmpty
                ? paneNote("No Kubernetes contexts", "Contexts from your kubeconfig ($KUBECONFIG or ~/.kube/config) show here. Press + to add an EKS cluster or merge a kubeconfig file.")
                : paneNote(nil, "No contexts match “\(filter.stringValue)”.")
            addSubview(note!)
        }
        needsLayout = true
    }

    /// Points the shell at a context with an overlay file; ~/.kube/config stays as it is.
    func use(_ c: Go.kube.KubeContext, newTab: Bool = false) {
        Task {
            do {
                let command = try await Services.kube.ShellConfig(env, c.name, c.file, "")
                if !newTab, let p = owner.activePane, p.isShell, !p.running, let t = owner.terminals[p.id] {
                    t.type(command + "\r")
                    t.focus()
                    Toast.show("This shell now uses \(c.name)", in: self)
                } else {
                    owner.model.open(command: command)
                    Toast.show("Opened a tab using \(c.name)", in: owner.rootView)
                }
            } catch {
                Toast.show(error.localizedDescription, in: self)
            }
        }
    }

    private func confirmEdit(_ op: Go.kube.Op, _ title: String, _ button: String, note: String = "") {
        Task { [weak self] in
            guard let self else { return }
            do {
                let steps = try await Services.kube.PlanEdit(env, op)
                Confirm.show(in: window, title: title, message: "wtf will make these changes\(note). A copy of the kubeconfig is kept first (kube-backups).",
                             items: steps, button: button, limit: .max) { [weak self] in self?.apply(op) }
            } catch {
                Toast.show(error.localizedDescription, in: self)
            }
        }
    }

    private func apply(_ op: Go.kube.Op, sheet: FormSheet? = nil) {
        Task {
            do {
                configs = try await Services.kube.ApplyEdit(env, op, configs.versions)
                rebuild()
                if let sheet { sheet.close() } else { Toast.show("Kubeconfig updated", in: self) }
            } catch {
                if let sheet { sheet.error = error.localizedDescription } else { Toast.show(error.localizedDescription, in: self) }
                load()
            }
        }
    }

    /// Rename… or Set Default Namespace…: shows the kubectl command, then wtf runs it.
    private func prompt(_ c: Go.kube.KubeContext, rename: Bool) {
        let s = FormSheet(title: rename ? "Rename \(c.name)" : "Default Namespace of \(c.name)",
                          subtitle: rename ? "In \(WindowModel.prettyPath(c.file)). Shells using it by name keep the old name until they switch again."
                              : "Commands in this context use it unless they name another (-n). Now: \(c.namespace.isEmpty ? "default" : c.namespace).")
        let field = s.text(rename ? c.name : c.namespace, placeholder: rename ? "" : "default")
        let preview = s.note("", mono: true)
        func value() -> String? {
            let v = field.stringValue.trimmingCharacters(in: .whitespaces)
            if v.isEmpty { s.error = rename ? "Enter a name" : "Enter a namespace"; return nil }
            if v == (rename ? c.name : c.namespace) { s.error = rename ? "That’s its name now" : "That’s its namespace now"; return nil }
            s.error = nil
            return v
        }
        let label = rename ? "Rename" : "Set Namespace"
        let watch = FieldWatcher {
            if let v = value() {
                preview.stringValue = (rename ? "kubectl config rename-context \(Shell.quote(c.name)) \(Shell.quote(v))"
                    : "kubectl config set-context \(Shell.quote(c.name)) --namespace=\(Shell.quote(v))") + "\nwtf runs this, after keeping a copy of the kubeconfig."
            } else {
                preview.stringValue = ""
            }
            s.button(label)?.isEnabled = value() != nil
        }
        field.delegate = watch
        s.field(rename ? "New name" : "Namespace", field)
        s.add(preview)
        s.buttons([(label, true, { [weak self] in
            _ = watch
            guard let v = value() else { return }
            self?.apply(Go.kube.Op(kind: rename ? "rename" : "namespace", name: c.name, value: v, file: c.file), sheet: s)
        })])
        s.present(in: window, focus: field)
        s.button(label)?.isEnabled = false
    }

    private func menu(_ c: Go.kube.KubeContext) -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        func add(_ t: String, _ run: @escaping () -> Void) { m.addItem(ClosureMenuItem(title: t, keyEquivalent: "", action: run)) }
        if let ch = checks[key(c)], let tool = Tools.missingTool(ch.error) {
            let o = owner
            add(tool == "aws" ? "Install the AWS CLI…" : "Install \(tool)…") { ToolsDialog.show(focus: tool, in: o) }
            m.addItem(.separator())
        }
        add("Use in This Shell") { [weak self] in self?.use(c) }
        add("Open New Tab with Context") { [weak self] in self?.use(c, newTab: true) }
        add("Show Its Resources") { [weak self] in
            self?.use(c)
            self?.owner.sidebar.kubeView = "cluster"
            self?.owner.sidebarChanged()
            self?.parent?.show("cluster")
        }
        m.addItem(.separator())
        add("Test Connection") { [weak self] in
            guard let self else { return }
            let k = key(c)
            checks[k] = (true, "", "")
            rebuild()
            Task {
                do {
                    let r = try await Services.kube.TestConnection(env, c.name, c.file)
                    checks[k] = (false, "Connected · \(r.version)\(r.username.isEmpty ? "" : " · as \(r.username)")", "")
                } catch {
                    checks[k] = (false, "", error.localizedDescription)
                }
                rebuild()
            }
        }
        if c.execCommand.hasSuffix("aws-iam-authenticator") {
            add("Log In with aws eks get-token…") { [weak self] in
                self?.confirmEdit(Go.kube.Op(kind: "fixAuth", name: c.name, file: c.file), "Switch \(c.name) to aws eks get-token?", "Switch",
                                  note: ", so it needs only the aws CLI")
            }
        }
        m.addItem(.separator())
        add("Set Default Namespace…") { [weak self] in self?.prompt(c, rename: false) }
        add("Rename…") { [weak self] in self?.prompt(c, rename: true) }
        m.addItem(.separator())
        add("Copy Name") { [weak self] in
            Clipboard.copy(c.name)
            if let self { Toast.show("Copied context name", in: self) }
        }
        if !c.server.isEmpty {
            add("Copy Server URL") { [weak self] in
                Clipboard.copy(c.server)
                if let self { Toast.show("Copied server URL", in: self) }
            }
        }
        add("Reveal Kubeconfig in Finder") { Task { try? await Services.files.Reveal(c.file) } }
        m.addItem(.separator())
        add("Delete…") { [weak self] in
            self?.confirmEdit(Go.kube.Op(kind: "delete", name: c.name, file: c.file), "Delete \(c.name)?", "Delete Context")
        }
        return m
    }

    private func showAddMenu() {
        let m = NSMenu()
        m.addItem(ClosureMenuItem(title: "Add EKS Cluster…", keyEquivalent: "") { [weak self] in self?.addEks() })
        m.addItem(ClosureMenuItem(title: "Merge a Kubeconfig File…", keyEquivalent: "") { [weak self] in self?.merge() })
        let anchor = subviews.first { $0.identifier?.rawValue == "add" } ?? self
        m.popUp(positioning: nil, at: NSPoint(x: 0, y: anchor.bounds.height + 4), in: anchor)
    }

    private func merge() {
        let p = NSOpenPanel()
        p.title = "Merge a Kubeconfig"
        p.prompt = "Merge…"
        p.canChooseFiles = true
        p.canChooseDirectories = false
        p.showsHiddenFiles = true
        guard let w = window else { return }
        p.beginSheetModal(for: w) { [weak self] r in
            guard r == .OK, let path = p.url?.path else { return }
            self?.confirmEdit(Go.kube.Op(kind: "merge", value: path), "Merge \(Paths.base(path))?", "Merge", note: " to your kubeconfig")
        }
    }

    /// Add EKS Cluster: aws eks update-kubeconfig in a new tab, after a backup.
    private func addEks() {
        let s = FormSheet(title: "Add EKS Cluster", subtitle: "Adds a context for an EKS cluster to your kubeconfig, using one of your AWS profiles")
        let profile = s.popup(["default credentials"])
        let region = NSComboBox()
        region.addItems(withObjectValues: Kube.regions)
        region.stringValue = "us-east-1"
        let cluster = NSComboBox()
        cluster.placeholderString = "cluster name"
        let found = s.note("")
        let alias = s.text(placeholder: "the cluster's ARN, if empty")
        let preview = s.note("", mono: true)
        let file = env["KUBECONFIG"] != nil ? configs.files.first : nil
        var regionsByProfile: [String: String] = [:]
        func command() -> String? {
            let c = cluster.stringValue.trimmingCharacters(in: .whitespaces), r = region.stringValue.trimmingCharacters(in: .whitespaces)
            guard !c.isEmpty, !r.isEmpty else { return nil }
            let p = profile.indexOfSelectedItem > 0 ? profile.titleOfSelectedItem ?? "" : ""
            return Kube.eksAdd(cluster: c, region: r, profile: p, alias: alias.stringValue.trimmingCharacters(in: .whitespaces), file: file)
        }
        func refresh() {
            preview.stringValue = command().map { "\($0)\nRuns in a new tab. wtf keeps a copy of your kubeconfig first (kube-backups). It logs in with aws eks get-token." } ?? ""
            s.button("Add in Terminal")?.isEnabled = command() != nil
        }
        let watch = FieldWatcher { refresh() }
        [region, cluster].forEach { $0.delegate = watch }
        alias.delegate = watch
        let pw = PopupWatcher {
            if let r = regionsByProfile[profile.titleOfSelectedItem ?? ""], !r.isEmpty { region.stringValue = r }
            refresh()
        }
        profile.target = pw
        profile.action = #selector(PopupWatcher.changed)
        weak var listButtonRef: NSButton?
        let listButton = ClosureButton(title: "List Clusters") { [weak self] in
            guard let self else { return }
            let r = region.stringValue.trimmingCharacters(in: .whitespaces)
            let p = profile.indexOfSelectedItem > 0 ? profile.titleOfSelectedItem ?? "" : ""
            guard !r.isEmpty else {
                found.stringValue = "Choose a region first"
                return
            }
            found.stringValue = "Asking…"
            listButtonRef?.title = "Asking…"
            listButtonRef?.isEnabled = false
            Task {
                defer {
                    listButtonRef?.title = "List Clusters"
                    listButtonRef?.isEnabled = true
                }
                do {
                    let names = try await Services.kube.EKSClusters(env, p, r)
                    cluster.removeAllItems()
                    cluster.addItems(withObjectValues: names)
                    if names.count == 1, cluster.stringValue.isEmpty { cluster.stringValue = names[0] }
                    found.stringValue = names.isEmpty ? "No clusters in \(r) for this profile" : "\(names.count) in \(r): \(names.joined(separator: ", "))"
                    found.textColor = .secondaryLabelColor
                } catch {
                    found.stringValue = error.localizedDescription
                    found.textColor = .systemRed
                    if Tools.missingTool(error.localizedDescription) != nil {
                        found.stringValue += " — Command-Line Tools (wtf menu) shows how to install it"
                    }
                }
                refresh()
            }
        }
        listButtonRef = listButton
        listButton.toolTip = "Asks AWS (eks list-clusters)"
        s.field("AWS profile", profile)
        s.field("Region", region)
        s.field("Cluster", NSStackView(views: [cluster, listButton]))
        s.add(found)
        s.field("Context name", alias)
        s.add(preview)
        s.buttons([("Add in Terminal", true, { [weak self] in
            _ = (watch, pw)
            guard let self, let c = command() else { return }
            Task {
                try? await Services.kube.BackupConfigs(env)
                s.close()
                owner.model.open(command: c)
            }
        })])
        s.present(in: window, focus: cluster)
        refresh()
        // Profiles from the local AWS files (no network).
        Task {
            if let l = try? await Services.awsprofiles.List() {
                for p in l.profiles { regionsByProfile[p.name] = p.region }
                profile.addItems(withTitles: l.profiles.map(\.name))
            }
        }
    }
}

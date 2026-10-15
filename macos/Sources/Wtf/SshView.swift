import AppKit
import WtfCore

/// What the SSH tab shows, shared by every window: loaded when a section
/// first appears, never polled. Tunnels follow the ssh:tunnels event.
@MainActor
final class SshStore {
    static let shared = SshStore()

    var list = Go.sshhosts.List()
    var hostsLoaded = false
    var keyFiles: [String] = []
    var keys = Go.sshhosts.KeyList()
    var keysLoaded = false
    var tunnels: [Go.sshhosts.TunnelStatus] = []
    var tunnelsLoaded = false
    var identities: [Go.sshhosts.GitIdentity] = []
    var identitiesLoaded = false
    var identityError = ""
    private var observers = Observers()
    private var listening = false

    func observe<T: AnyObject>(_ owner: T, _ fn: @escaping (T) -> Void) { observers.add(owner, fn) }
    func changed() {
        observers.notify()
        let ready = waiting.filter { (!$0.keys || keysLoaded) && (!$0.hosts || hostsLoaded) }
        waiting.removeAll { w in ready.contains { $0.id == w.id } }
        ready.forEach { $0.run() }
    }

    private var waiting: [(id: UUID, keys: Bool, hosts: Bool, run: () -> Void)] = []

    /// Runs once the keys and/or hosts are loaded (loading them if needed),
    /// so dialogs built from them never open with empty lists.
    func whenLoaded(keys: Bool = false, hosts: Bool = false, _ run: @escaping () -> Void) {
        if (!keys || keysLoaded), (!hosts || hostsLoaded) { return run() }
        waiting.append((UUID(), keys, hosts, run))
        if keys, !keysLoaded { loadKeys() }
        if hosts, !hostsLoaded { loadHosts() }
    }

    func loadHosts() {
        Task {
            do { list = try await Services.sshhosts.List() } catch { list.problems = [error.localizedDescription] }
            hostsLoaded = true
            changed()
            keyFiles = (try? await Services.sshhosts.Keys()) ?? []
        }
    }

    func loadKeys() {
        Task {
            do { keys = try await Services.sshhosts.KeyList() } catch { keys.problems = [error.localizedDescription] }
            keysLoaded = true
            changed()
        }
    }

    func loadTunnels() {
        if !listening {
            listening = true
            Backend.on("ssh:tunnels", [Go.sshhosts.TunnelStatus]?.self) { [weak self] in
                self?.tunnels = $0 ?? []
                self?.changed()
            }
        }
        Task {
            tunnels = (try? await Services.sshhosts.Tunnels()) ?? []
            tunnelsLoaded = true
            changed()
        }
    }

    func loadIdentities() {
        Task {
            do {
                identities = try await Services.sshhosts.GitIdentities()
                identityError = ""
            } catch {
                identityError = error.localizedDescription
            }
            identitiesLoaded = true
            changed()
        }
    }

    /// ssh-keygen, ssh-add and the like finished in a shell: keys and hosts may have changed.
    func commandFinished(_ command: String) {
        guard Ssh.isKeyTool(command) else { return }
        if keysLoaded { loadKeys() }
        if hostsLoaded { loadHosts() }
    }

    func runningTunnels(kubectl: Bool) -> Int { tunnels.filter { Tunnels.isOn($0) && ($0.kind == "kubectl") == kubectl }.count }
}

/// The SSH tab: Hosts, Keys, Git identities and Tunnels. Only the shown
/// section exists. Like SshView.svelte.
@MainActor
final class SshView: NSView, SidePaneTab {
    private unowned let owner: WindowController
    private let segments = NSSegmentedControl(labels: ["Hosts", "Keys", "Git", "Tunnels"], trackingMode: .selectOne, target: nil, action: nil)
    private var section: NSView?
    private static let ids = ["hosts", "keys", "git", "tunnels"]

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        segments.segmentDistribution = .fillEqually
        segments.target = self
        segments.action = #selector(switched)
        segments.refusesFirstResponder = true
        addSubview(segments)
        // Tunnels load with the tab, for the running count.
        if !SshStore.shared.tunnelsLoaded { SshStore.shared.loadTunnels() }
        SshStore.shared.observe(self) { $0.updateBadge() }
        show(window.sidebar.sshSection)
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    private func updateBadge() {
        let n = SshStore.shared.runningTunnels(kubectl: false)
        segments.setLabel(n > 0 ? "Tunnels \(n)" : "Tunnels", forSegment: 3)
        segments.setToolTip(n > 0 ? "\(n) running" : nil, forSegment: 3)
    }

    @objc private func switched() {
        let id = Self.ids[max(0, segments.selectedSegment)]
        owner.sidebar.sshSection = id
        owner.sidebarChanged()
        show(id)
    }

    private func show(_ id: String) {
        segments.selectedSegment = Self.ids.firstIndex(of: id) ?? 0
        section?.removeFromSuperview()
        let v: NSView = switch id {
        case "keys": SshKeysSection(window: owner)
        case "git": GitIdentitiesSection(window: owner)
        case "tunnels": TunnelsSection(window: owner)
        default: SshHostsSection(window: owner)
        }
        section = v
        addSubview(v)
        updateBadge()
        needsLayout = true
    }

    func paneChanged() { (section as? SidePaneTab)?.paneChanged() }

    func debugSummary() -> String {
        let st = SshStore.shared
        return "ssh: \(st.list.hosts.filter { !$0.known }.count) configured, \(st.list.hosts.filter(\.known).count) known hosts, \(st.list.problems.count) problems, \(st.tunnels.count) tunnels"
    }

    override func layout() {
        super.layout()
        segments.frame = NSRect(x: 6, y: 4, width: bounds.width - 12, height: 24)
        section?.frame = NSRect(x: 0, y: 32, width: bounds.width, height: max(0, bounds.height - 32))
    }
}

// MARK: - Hosts

@MainActor
final class SshHostsSection: NSView, NSSearchFieldDelegate {
    private unowned let owner: WindowController
    private let filter = NSSearchField()
    private let list = SideList()
    private let footer = NSStackView()
    private let footerLabel = NSTextField(labelWithString: "")
    private var note: NoteView?
    private var shown: [SshHost] = []
    private var store: SshStore { .shared }

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        filter.placeholderString = "Filter hosts"
        filter.delegate = self
        filter.sendsSearchStringImmediately = true
        // The side pane takes the keyboard only when clicked into.
        filter.refusesFirstResponder = true
        addSubview(filter)
        let add = ToolButton(symbol: "plus", tip: "Add host") { [weak self] in
            guard let self else { return }
            HostEditor.show(nil, in: owner)
        }
        let reload = ToolButton(symbol: "arrow.clockwise", tip: "Reload ~/.ssh/config") { SshStore.shared.loadHosts() }
        add.identifier = .init("add")
        reload.identifier = .init("reload")
        addSubview(add)
        addSubview(reload)
        list.allowsMultiple = true
        list.onDoubleClick = { [weak self] id in self?.open([id], .here) }
        list.onReturn = { [weak self] ids in self?.open(ids, ids.count > 1 ? .group : .here) }
        list.menuProvider = { [weak self] ids in self?.menu(ids) }
        list.onSelection = { [weak self] _ in self?.updateFooter() }
        list.onDrop = { [weak self] id, path in
            guard let self, let h = host(id) else { return }
            TransferSheet.show(host: h, upload: true, files: [path], in: owner)
        }
        addSubview(list)
        footerLabel.font = .systemFont(ofSize: 11.5)
        let openGroup = ClosureButton(title: "Open as Tab Group") { [weak self] in
            guard let self else { return }
            open(list.selectedIds, .group)
        }
        openGroup.controlSize = .small
        footer.setViews([footerLabel, NSView(), openGroup], in: .leading)
        footer.edgeInsets = NSEdgeInsets(top: 4, left: 10, bottom: 4, right: 8)
        footer.isHidden = true
        addSubview(footer)
        store.observe(self) { $0.rebuild() }
        store.loadHosts()
        rebuild()
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    override func layout() {
        super.layout()
        filter.frame = NSRect(x: 8, y: 4, width: bounds.width - 70, height: 24)
        subviews.first { $0.identifier?.rawValue == "add" }?.frame = NSRect(x: bounds.width - 58, y: 4, width: 24, height: 24)
        subviews.first { $0.identifier?.rawValue == "reload" }?.frame = NSRect(x: bounds.width - 32, y: 4, width: 24, height: 24)
        let footH: CGFloat = footer.isHidden ? 0 : 32
        list.frame = NSRect(x: 0, y: 34, width: bounds.width, height: max(0, bounds.height - 34 - footH))
        note?.frame = NSRect(x: 0, y: 34, width: bounds.width, height: note?.height(for: bounds.width) ?? 0)
        footer.frame = NSRect(x: 0, y: bounds.height - footH, width: bounds.width, height: footH)
    }

    private func host(_ id: String) -> SshHost? { store.list.hosts.first { Ssh.key($0) == id } }

    func controlTextDidChange(_ note: Notification) { rebuild() }

    func control(_ control: NSControl, textView: NSTextView, doCommandBy selector: Selector) -> Bool {
        switch selector {
        case #selector(NSResponder.insertNewline(_:)):
            let ids = list.selectedIds
            if !ids.isEmpty { open(ids, ids.count > 1 ? .group : .here) }
            return true
        case #selector(NSResponder.cancelOperation(_:)):
            list.clearSelection()
            return true
        case #selector(NSResponder.moveDown(_:)):
            list.focus()
            return true
        default:
            return false
        }
    }

    private func rebuild() {
        shown = store.list.hosts.filter { Ssh.matches($0, filter.stringValue) }
        var sections: [SideList.Section] = []
        let configured = shown.filter { !$0.known }
        var files: [String] = []
        for h in configured where !files.contains(h.source) { files.append(h.source) }
        for f in files {
            sections.append(.init(title: files.count == 1 ? "SSH config" : WindowModel.prettyPath(f), tip: WindowModel.prettyPath(f),
                                  rows: configured.filter { $0.source == f }.map(row)))
        }
        let known = shown.filter(\.known)
        if !known.isEmpty {
            sections.append(.init(title: "Known hosts", tip: "Servers in ~/.ssh/known_hosts that your config doesn’t name", rows: known.map(row)))
        }
        if !store.list.problems.isEmpty {
            sections.append(.init(title: "Problems", rows: store.list.problems.map {
                .init(id: "p:\($0)", symbol: "exclamationmark.triangle", tint: .systemRed, title: $0)
            }))
        }
        list.set(sections)
        note?.removeFromSuperview()
        note = nil
        if store.hostsLoaded, shown.isEmpty {
            note = filter.stringValue.isEmpty
                ? paneNote("No SSH hosts", "Hosts from ~/.ssh/config (and the files it includes) show here, along with servers you’ve connected to before. Press + to add one.")
                : paneNote(nil, "No hosts match “\(filter.stringValue)”.")
            addSubview(note!)
        }
        updateFooter()
        needsLayout = true
    }

    private func row(_ h: SshHost) -> SideList.Row {
        var tip = Ssh.command(h)
        if !h.source.isEmpty { tip += "\nfrom \(WindowModel.prettyPath(h.source))" }
        tip += "\nDrop a file here to upload it"
        return SideList.Row(
            id: Ssh.key(h), symbol: "server.rack", tint: h.known ? .secondaryLabelColor : .controlAccentColor,
            title: h.alias, mono: true, detail: Ssh.detail(h),
            pills: Ssh.facts(h).map { .init(text: $0.label, tip: $0.title) }, tip: tip,
            button: ("play.fill", "Connect in this shell (a new tab if it’s busy)", { [weak self] in self?.owner.open(hosts: [h], .here) }))
    }

    private func updateFooter() {
        let n = list.selectedIds.count
        let wasHidden = footer.isHidden
        footer.isHidden = n < 2
        if n > 1 {
            let hosts = list.selectedIds.compactMap(host)
            footerLabel.stringValue = "\(n) selected"
            footer.toolTip = "Opens each host in a tab, grouped as “\(Ssh.groupName(hosts))”"
        }
        if wasHidden != footer.isHidden { needsLayout = true }
    }

    private func open(_ ids: [String], _ where_: WindowController.OpenWhere) {
        let hosts = ids.compactMap(host)
        guard !hosts.isEmpty else { return }
        list.clearSelection()
        owner.open(hosts: hosts, where_)
    }

    private func menu(_ ids: [String]) -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        func add(_ t: String, _ hint: String? = nil, _ run: @escaping () -> Void) {
            let i = ClosureMenuItem(title: t, keyEquivalent: "", action: run)
            i.toolTip = hint
            m.addItem(i)
        }
        let hosts = ids.compactMap(host)
        if hosts.count > 1 {
            let n = hosts.count
            add("Open \(n) Hosts in a Tab Group", "↩") { [weak self] in self?.open(ids, .group) }
            add("Open \(n) Hosts in a Tab Group with Broadcast Input") { [weak self] in self?.open(ids, .broadcast) }
            add("Open \(n) Hosts in Tabs") { [weak self] in self?.open(ids, .tab) }
            m.addItem(.separator())
            add("Copy ssh Commands") { [weak self] in
                Clipboard.copy(hosts.map(Ssh.command).joined(separator: "\n"))
                if let self { Toast.show("Copied ssh commands", in: self) }
            }
            return m
        }
        guard let h = hosts.first else { return m }
        let o = owner
        add("Open Here", "double-click") { [weak self] in self?.open(ids, .here) }
        add("Open in New Tab") { [weak self] in self?.open(ids, .tab) }
        add("Open in Split Right") { [weak self] in self?.open(ids, .right) }
        add("Open in Split Down") { [weak self] in self?.open(ids, .down) }
        add("Open sftp Session") { o.runInShell(Ssh.sftp(h)) }
        m.addItem(.separator())
        add("Upload Files…") { TransferSheet.show(host: h, upload: true, files: [], in: o) }
        add("Download…") { TransferSheet.show(host: h, upload: false, files: [], in: o) }
        add("New Tunnel…") { TunnelEditor.show(nil, host: h, in: o) }
        m.addItem(.separator())
        add("Install Public Key…") { KeyInstallSheet.show(key: nil, host: h, in: o) }
        add("Host Keys…") { HostKeysSheet.show(h, in: o) }
        m.addItem(.separator())
        add("Copy ssh Command") { [weak self] in
            Clipboard.copy(Ssh.command(h))
            if let self { Toast.show("Copied ssh command", in: self) }
        }
        add("Copy Host Name") { [weak self] in
            Clipboard.copy(h.hostName.isEmpty ? h.alias : h.hostName)
            if let self { Toast.show("Copied host name", in: self) }
        }
        m.addItem(.separator())
        if h.known {
            add("Save as Host…") {
                var d = HostDraft()
                d.name = h.alias
                d.hostName = h.alias
                d.port = h.port
                HostEditor.show(nil, prefill: d, in: o)
            }
        } else if !h.editable {
            let i = NSMenuItem(title: "Managed by \(WindowModel.prettyPath(h.source))", action: nil, keyEquivalent: "")
            i.isEnabled = false
            m.addItem(i)
        } else {
            add("Edit Host…") { HostEditor.show(h, in: o) }
            add("Delete Host…") { HostEditor.confirmDelete(h, in: o) }
        }
        return m
    }
}

// MARK: - Keys

@MainActor
final class SshKeysSection: NSView {
    private unowned let owner: WindowController
    private let header = PaneHeader("~/.ssh")
    private let list = SideList()
    private let footer = NSTextField(labelWithString: "")
    private var unloadAll: ClosureButton?
    private var note: NoteView?
    private var store: SshStore { .shared }

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        header.tool("plus", "New key") { [weak self] in
            guard let self else { return }
            owner.showGenerator("key")
        }
        header.tool("arrow.clockwise", "Reload") { SshStore.shared.loadKeys() }
        addSubview(header)
        list.onDoubleClick = { [weak self] id in
            guard let self, let k = key(id) else { return }
            if k.path.isEmpty { copyFingerprint(k) } else { copyPublicKey(k) }
        }
        list.menuProvider = { [weak self] ids in ids.first.flatMap { self?.key($0) }.flatMap { self?.menu($0) } }
        addSubview(list)
        footer.font = .systemFont(ofSize: 11.5)
        addSubview(footer)
        store.observe(self) { $0.rebuild() }
        store.loadKeys()
        rebuild()
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    override func layout() {
        super.layout()
        header.frame = NSRect(x: 0, y: 0, width: bounds.width, height: 30)
        list.frame = NSRect(x: 0, y: 30, width: bounds.width, height: max(0, bounds.height - 30 - 30))
        note?.frame = NSRect(x: 0, y: 30, width: bounds.width, height: note?.height(for: bounds.width) ?? 0)
        footer.frame = NSRect(x: 10, y: bounds.height - 24, width: bounds.width - 100, height: 18)
        unloadAll?.frame = NSRect(x: bounds.width - 90, y: bounds.height - 28, width: 84, height: 24)
    }

    private func key(_ id: String) -> Go.sshhosts.Key? { store.keys.keys.first { ($0.path.isEmpty ? "a:" + $0.fingerprint : $0.path) == id } }

    private func name(_ k: Go.sshhosts.Key) -> String {
        k.path.isEmpty ? k.name : String(WindowModel.prettyPath(k.path).replacingOccurrences(of: "~/.ssh/", with: ""))
    }

    private func rebuild() {
        let accent = NSColor.controlAccentColor
        func row(_ k: Go.sshhosts.Key) -> SideList.Row {
            var pills: [SideList.Pill] = []
            if !k.unprotected.isEmpty { pills.append(.init(text: k.unprotected, color: .systemRed, tip: "ssh refuses keys other users can read")) }
            if k.inAgent { pills.append(.init(text: "agent", color: accent)) }
            if !k.usedBy.isEmpty { pills.append(.init(text: "\(k.usedBy.count) \(k.usedBy.count == 1 ? "host" : "hosts")")) }
            var tip = [k.fingerprint]
            if !k.path.isEmpty {
                tip.append(k.encrypted ? "Protected by a passphrase" : "No passphrase (or not recorded in the file)")
                if let d = ISO8601DateFormatter().date(from: k.modified) {
                    tip.append("File written \(d.formatted(date: .abbreviated, time: .omitted)): when the key was made, unless its passphrase was changed since")
                }
            }
            if !k.usedBy.isEmpty { tip.append("IdentityFile for \(k.usedBy.joined(separator: ", "))") }
            if !k.unprotected.isEmpty { tip.append("Readable by other users (\(k.unprotected)): ssh won’t use it until it’s made private") }
            return .init(id: k.path.isEmpty ? "a:" + k.fingerprint : k.path, symbol: "key", tint: k.inAgent ? accent : .secondaryLabelColor,
                         title: name(k), mono: true, detail: Ssh.keyDetails(k), pills: pills, tip: tip.joined(separator: "\n"))
        }
        let files = store.keys.keys.filter { !$0.path.isEmpty }
        let agentOnly = store.keys.keys.filter { $0.path.isEmpty }
        var sections = [SideList.Section(title: nil, rows: files.map(row))]
        if !agentOnly.isEmpty {
            sections.append(.init(title: "Agent only", tip: "Keys ssh-agent holds that aren’t files in ~/.ssh — from a password manager or a security key, say",
                                  rows: agentOnly.map(row)))
        }
        if !store.keys.problems.isEmpty {
            sections.append(.init(title: "Problems", rows: store.keys.problems.map { .init(id: "p:\($0)", symbol: "exclamationmark.triangle", tint: .systemRed, title: $0) }))
        }
        list.set(sections)
        note?.removeFromSuperview()
        note = nil
        if store.keysLoaded, files.isEmpty, agentOnly.isEmpty {
            note = paneNote("No keys in ~/.ssh", "A key lets you log in without a password. Press + to make one, then install it on your servers.")
            addSubview(note!)
        }
        // The agent, in the footer.
        let inAgent = store.keys.keys.filter(\.inAgent).count
        if !store.keys.agentError.isEmpty {
            footer.stringValue = "ssh-agent: \(store.keys.agentError)"
            footer.textColor = .systemRed
        } else {
            footer.stringValue = inAgent == 0 ? "ssh-agent: no keys loaded" : "ssh-agent: \(inAgent) \(inAgent == 1 ? "key" : "keys") loaded"
            footer.textColor = .secondaryLabelColor
        }
        footer.toolTip = store.keys.agentSocket.isEmpty ? "SSH_AUTH_SOCK isn’t set" : "SSH_AUTH_SOCK=\(store.keys.agentSocket)"
        unloadAll?.removeFromSuperview()
        unloadAll = nil
        if inAgent > 0, store.keys.agentError.isEmpty {
            let b = ClosureButton(title: "Unload All") { [weak self] in
                guard let self else { return }
                Confirm.show(in: window, title: "Unload all keys from ssh-agent?",
                             message: "The key files stay in ~/.ssh. Connections already open keep working.", button: "Unload All") { [weak self] in
                    self?.act("Unloaded every key") { try await Services.sshhosts.AgentRemoveAll() }
                }
            }
            b.controlSize = .small
            b.bezelStyle = .accessoryBarAction
            addSubview(b)
            unloadAll = b
        }
        needsLayout = true
    }

    /// A quick backend action: its toast, or the error; keys reloaded after.
    private func act(_ success: String, _ run: @escaping () async throws -> Void) {
        Task {
            do {
                try await run()
                Toast.show(success, in: self)
            } catch {
                Toast.show(error.localizedDescription, in: self)
            }
            SshStore.shared.loadKeys()
        }
    }

    private func copyPublicKey(_ k: Go.sshhosts.Key) {
        Task {
            do {
                Clipboard.copy(try await Services.sshhosts.PublicKey(k.path))
                Toast.show("Copied \(WindowModel.prettyPath(k.path)).pub", in: self)
            } catch {
                Toast.show(error.localizedDescription, in: self)
            }
        }
    }

    private func copyFingerprint(_ k: Go.sshhosts.Key) {
        Clipboard.copy(k.fingerprint)
        Toast.show("Copied fingerprint", in: self)
    }

    private func menu(_ k: Go.sshhosts.Key) -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        func add(_ t: String, _ run: @escaping () -> Void) { m.addItem(ClosureMenuItem(title: t, keyEquivalent: "", action: run)) }
        let o = owner
        let n = name(k)
        let quoted = Ssh.quotePath(k.path)
        let file = !k.path.isEmpty
        if file {
            add("Copy Public Key") { [weak self] in self?.copyPublicKey(k) }
            add("Install on Host…") { KeyInstallSheet.show(key: k, host: nil, in: o) }
            m.addItem(.separator())
        }
        if file, !k.inAgent {
            add("Load into Agent") { o.runInShell(Ssh.agentAdd(quoted, keychain: false)) }
            add("Load into Agent and Keychain") { o.runInShell(Ssh.agentAdd(quoted, keychain: true)) }
        }
        if k.inAgent {
            add("Unload from Agent") { [weak self] in self?.act("Unloaded \(n)") { try await Services.sshhosts.AgentRemove(k.fingerprint) } }
        }
        if file {
            add(k.encrypted ? "Change Passphrase…" : "Set Passphrase…") { o.runInShell("ssh-keygen -p -f \(quoted)") }
            if !k.unprotected.isEmpty {
                add("Make Private (chmod 600)") { [weak self] in self?.act("\(n) is now private") { try await Services.sshhosts.ProtectKey(k.path) } }
            }
            if !k.hasPub {
                add("Create .pub File") { [weak self] in self?.act("Wrote \(n).pub") { try await Services.sshhosts.WritePublicKey(k.path) } }
            }
        }
        m.addItem(.separator())
        add("Copy Fingerprint") { [weak self] in self?.copyFingerprint(k) }
        if file {
            add("Reveal in Finder") { Task { try? await Services.files.Reveal(k.path) } }
            m.addItem(.separator())
            add("Delete Key…") { [weak self] in
                guard let self else { return }
                Confirm.show(in: window, title: "Delete \(n)?",
                             message: "Moves the key\(k.hasPub ? " and its .pub" : "") out of ~/.ssh into wtf’s cache folder (ssh-backups/keys), where you can still get it back. Servers that only accept this key will refuse you."
                                 + (k.inAgent ? " The agent keeps its copy until you unload it." : ""),
                             items: k.usedBy.map { "Used by \($0)" }, button: "Delete Key") { [weak self] in
                    self?.act("Moved \(n) to wtf’s cache folder") { try await Services.sshhosts.DeleteKey(k.path) }
                }
            }
        }
        return m
    }
}

// MARK: - Tunnels

@MainActor
final class TunnelsSection: NSView {
    private unowned let owner: WindowController
    private let header = PaneHeader("Port forwards")
    private let list = SideList()
    private var note: NoteView?

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        header.tool("plus", "New tunnel") { [weak self] in
            guard let self else { return }
            TunnelEditor.show(nil, host: nil, in: owner)
        }
        addSubview(header)
        list.onDoubleClick = { [weak self] id in
            guard let self, let t = tunnel(id) else { return }
            TunnelEditor.show(t, host: nil, in: owner)
        }
        list.menuProvider = { [weak self] ids in ids.first.flatMap { self?.tunnel($0) }.flatMap { self?.menu($0) } }
        addSubview(list)
        SshStore.shared.observe(self) { $0.rebuild() }
        if !SshStore.shared.tunnelsLoaded { SshStore.shared.loadTunnels() }
        rebuild()
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    override func layout() {
        super.layout()
        header.frame = NSRect(x: 0, y: 0, width: bounds.width, height: 30)
        list.frame = NSRect(x: 0, y: 30, width: bounds.width, height: max(0, bounds.height - 30))
        note?.frame = NSRect(x: 0, y: 30, width: bounds.width, height: note?.height(for: bounds.width) ?? 0)
    }

    private func tunnel(_ id: String) -> Go.sshhosts.TunnelStatus? { SshStore.shared.tunnels.first { $0.id == id } }

    private func rebuild() {
        let list = SshStore.shared.tunnels.filter { $0.kind != "kubectl" }
        self.list.set([.init(title: nil, rows: list.map { Self.row($0, window: owner) })])
        note?.removeFromSuperview()
        note = nil
        if SshStore.shared.tunnelsLoaded, list.isEmpty {
            note = paneNote("No tunnels", "Forward a port through ssh: reach a database behind a bastion on localhost, expose a local dev server to a server, or browse through a SOCKS proxy. Press + to add one, or right-click a host.")
            addSubview(note!)
        }
        needsLayout = true
    }

    /// A tunnel's row, shared with the Kubernetes forwards.
    static func row(_ t: Go.sshhosts.TunnelStatus, window: WindowController) -> SideList.Row {
        let dark = Settings.shared.theme?.dark ?? true
        let color: NSColor = switch t.state {
        case "up": NSColor(hex: dark ? "#5bb98c" : "#2f9e6a")
        case "connecting", "retrying": NSColor(hex: dark ? "#fdd663" : "#c98a00")
        case "failed": NSColor(hex: dark ? "#e5484d" : "#d93036")
        default: NSColor.secondaryLabelColor.withAlphaComponent(0.5)
        }
        let desc = Tunnels.describe(t)
        let on = Tunnels.isOn(t)
        return .init(id: t.id, symbol: "", title: t.name.isEmpty ? desc : t.name,
                     detail: (t.name.isEmpty ? "" : desc + " · ") + Tunnels.stateLabel(t.state),
                     alert: t.state == "failed" || t.state == "retrying" ? t.error : "",
                     tip: t.command + (t.autoStart ? "\nStarts when wtf opens" : ""), dot: color,
                     button: (on ? "stop.fill" : "play.fill", on ? "Stop" : "Start", { [weak window] in
                         Task {
                             do {
                                 SshStore.shared.tunnels = on ? try await Services.sshhosts.StopTunnel(t.id) : try await Services.sshhosts.StartTunnel(t.id)
                                 SshStore.shared.changed()
                             } catch {
                                 if let v = window?.rootView { Toast.show(error.localizedDescription, in: v) }
                             }
                         }
                     }))
    }

    /// A tunnel's menu, shared with the Kubernetes forwards.
    static func menu(_ t: Go.sshhosts.TunnelStatus, window w: WindowController) -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        func add(_ title: String, _ run: @escaping () -> Void) { m.addItem(ClosureMenuItem(title: title, keyEquivalent: "", action: run)) }
        let on = Tunnels.isOn(t)
        add(on ? "Stop" : "Start") {
            Task {
                do {
                    SshStore.shared.tunnels = try await on ? Services.sshhosts.StopTunnel(t.id) : Services.sshhosts.StartTunnel(t.id)
                    SshStore.shared.changed()
                } catch {
                    Toast.show(error.localizedDescription, in: w.rootView, seconds: 6)
                }
            }
        }
        if let port = Tunnels.localPort(t) {
            if t.kind == "local" || t.kind == "kubectl" {
                add("Open localhost:\(port) in Browser") { NSWorkspace.shared.open(URL(string: "http://localhost:\(port)")!) }
            }
            add("Copy localhost:\(port)") {
                Clipboard.copy("localhost:\(port)")
                Toast.show("Copied localhost:\(port)", in: w.rootView)
            }
        }
        add("Copy Command") {
            Clipboard.copy(t.command)
            Toast.show("Copied command", in: w.rootView)
        }
        m.addItem(.separator())
        add("Edit…") { TunnelEditor.show(t, host: nil, in: w) }
        add("Delete") {
            Confirm.show(in: w.window, title: "Delete \(t.name.isEmpty ? "this port forward" : t.name)?",
                         message: Tunnels.describe(t) + (on ? ". It stops now." : "."), button: "Delete") {
                Task {
                    do {
                        SshStore.shared.tunnels = try await Services.sshhosts.DeleteTunnel(t.id)
                        SshStore.shared.changed()
                    } catch {
                        Toast.show(error.localizedDescription, in: w.rootView, seconds: 6)
                    }
                }
            }
        }
        return m
    }

    private func menu(_ t: Go.sshhosts.TunnelStatus) -> NSMenu { Self.menu(t, window: owner) }
}

// MARK: - Git identities

@MainActor
final class GitIdentitiesSection: NSView, SidePaneTab {
    private unowned let owner: WindowController
    private let header = PaneHeader("Git identities")
    private let current = NSStackView()
    private let currentLabel = NSTextField(labelWithString: "")
    private let list = SideList()
    private var note: NoteView?

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        header.tool("plus", "New identity") { [weak self] in
            guard let self else { return }
            GitIdentityEditor.show(nil, in: owner)
        }
        addSubview(header)
        let stop = ClosureButton(title: "Stop Using") { [weak self] in self?.stopUsing() }
        stop.bezelStyle = .accessoryBarAction
        stop.controlSize = .small
        currentLabel.font = .systemFont(ofSize: 11.5)
        current.setViews([currentLabel, NSView(), stop], in: .leading)
        current.edgeInsets = NSEdgeInsets(top: 0, left: 10, bottom: 0, right: 8)
        addSubview(current)
        list.onDoubleClick = { [weak self] id in self?.identity(id).map { self?.use($0) } }
        list.menuProvider = { [weak self] ids in ids.first.flatMap { self?.identity($0) }.flatMap { self?.menu($0) } }
        addSubview(list)
        SshStore.shared.observe(self) { $0.rebuild() }
        SshStore.shared.loadIdentities()
        if !SshStore.shared.keysLoaded { SshStore.shared.loadKeys() }
        rebuild()
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    func paneChanged() { rebuild() }

    private var currentName: String? { owner.activePane?.env?[Shell.identityVar].flatMap { $0.isEmpty ? nil : $0 } }

    override func layout() {
        super.layout()
        header.frame = NSRect(x: 0, y: 0, width: bounds.width, height: 30)
        var y: CGFloat = 30
        if !current.isHidden {
            current.frame = NSRect(x: 0, y: y, width: bounds.width, height: 26)
            y += 28
        }
        list.frame = NSRect(x: 0, y: y, width: bounds.width, height: max(0, bounds.height - y))
        note?.frame = NSRect(x: 0, y: y, width: bounds.width, height: note?.height(for: bounds.width) ?? 0)
    }

    private func identity(_ id: String) -> Go.sshhosts.GitIdentity? { SshStore.shared.identities.first { $0.id == id } }

    private func author(_ i: Go.sshhosts.GitIdentity) -> String {
        if !i.authorName.isEmpty, !i.authorEmail.isEmpty { return "\(i.authorName) <\(i.authorEmail)>" }
        return [i.authorName, i.authorEmail].first { !$0.isEmpty } ?? "author from git config"
    }

    private func keyName(_ i: Go.sshhosts.GitIdentity) -> String { WindowModel.prettyPath(i.keyPath).replacingOccurrences(of: "~/.ssh/", with: "") }

    private func rebuild() {
        let cur = currentName
        current.isHidden = cur == nil
        if let cur {
            let s = NSMutableAttributedString(string: "This tab: ", attributes: [.font: NSFont.systemFont(ofSize: 11.5)])
            s.append(NSAttributedString(string: cur, attributes: [.font: NSFont.systemFont(ofSize: 11.5, weight: .bold)]))
            currentLabel.attributedStringValue = s
        }
        let rows = SshStore.shared.identities.map { i -> SideList.Row in
            var pills: [SideList.Pill] = []
            if !i.githubUser.isEmpty { pills.append(.init(text: "@\(i.githubUser)", tip: "The GitHub account this key logged in as at the last test")) }
            if i.name == cur { pills.append(.init(text: "this tab", color: .controlAccentColor)) }
            let tested = i.githubUser.isEmpty ? "GitHub account: not tested yet (right-click → Test GitHub Login)"
                : "GitHub account: \(i.githubUser), as of the last test (\(Badges.relativeTime(Double(i.testedAt), now: Date().timeIntervalSince1970 * 1000)))"
            return .init(id: i.id, symbol: "key", title: i.name, detail: "\(author(i)) · \(keyName(i))", pills: pills,
                         tip: "Commits as: \(author(i))\nSSH key: \(WindowModel.prettyPath(i.keyPath)) (offered alone: IdentitiesOnly)\n\(tested)",
                         highlighted: i.name == cur)
        }
        var sections = [SideList.Section(title: nil, rows: rows)]
        if !SshStore.shared.identityError.isEmpty {
            sections.append(.init(title: "Problems", rows: [.init(id: "err", symbol: "exclamationmark.triangle", tint: .systemRed, title: SshStore.shared.identityError)]))
        }
        list.set(sections)
        note?.removeFromSuperview()
        note = nil
        if SshStore.shared.identitiesLoaded, rows.isEmpty {
            note = paneNote("Work on GitHub as more than one user", "An identity pairs an SSH key with the name and email to commit as. Use one in a tab and git in that tab pushes and commits as that user, while other tabs stay as they are. Press + to add one.")
            addSubview(note!)
        }
        needsLayout = true
    }

    func use(_ i: Go.sshhosts.GitIdentity) {
        if !owner.runInShell(Shell.useIdentity(.init(name: i.name, authorName: i.authorName, authorEmail: i.authorEmail, keyPath: i.keyPath))) {
            Toast.show("Opened a new tab as \(i.name): that one was busy", in: self)
        }
    }

    private func stopUsing() {
        owner.runInShell("unset GIT_SSH_COMMAND GIT_AUTHOR_NAME GIT_AUTHOR_EMAIL GIT_COMMITTER_NAME GIT_COMMITTER_EMAIL WTF_GIT_IDENTITY")
    }

    private func menu(_ i: Go.sshhosts.GitIdentity) -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        func add(_ t: String, _ run: @escaping () -> Void) { m.addItem(ClosureMenuItem(title: t, keyEquivalent: "", action: run)) }
        let o = owner
        let isCurrent = i.name == currentName
        add(isCurrent ? "Use Again in This Tab" : "Use in This Tab") { [weak self] in self?.use(i) }
        add("Open New Tab as \(i.name)") {
            o.model.open(command: Shell.useIdentity(.init(name: i.name, authorName: i.authorName, authorEmail: i.authorEmail, keyPath: i.keyPath)))
        }
        if isCurrent { add("Stop Using in This Tab") { [weak self] in self?.stopUsing() } }
        m.addItem(.separator())
        add("Test GitHub Login") { [weak self] in
            Task {
                guard let self else { return }
                do {
                    let r = try await Services.sshhosts.TestGitIdentity(i.id)
                    Toast.show(r.ok ? "\(i.name) logs in to GitHub as \(r.user)" : "\(i.name): \(r.message)", in: self)
                    if r.ok { SshStore.shared.loadIdentities() }
                } catch {
                    Toast.show(error.localizedDescription, in: self)
                }
            }
        }
        add("Add Key to GitHub…") { [weak self] in
            Task {
                guard let self else { return }
                do {
                    Clipboard.copy(try await Services.sshhosts.PublicKey(i.keyPath))
                    Toast.show("Copied \(self.keyName(i)).pub. Paste it on GitHub while signed in as the account for \(i.name)", in: self)
                    NSWorkspace.shared.open(URL(string: "https://github.com/settings/ssh/new")!)
                } catch {
                    Toast.show(error.localizedDescription, in: self)
                }
            }
        }
        add("Copy Public Key") { [weak self] in
            Task {
                guard let self else { return }
                do {
                    Clipboard.copy(try await Services.sshhosts.PublicKey(i.keyPath))
                    Toast.show("Copied \(self.keyName(i)).pub", in: self)
                } catch {
                    Toast.show(error.localizedDescription, in: self)
                }
            }
        }
        m.addItem(.separator())
        add("Edit…") { GitIdentityEditor.show(i, in: o) }
        add("Delete…") { [weak self] in
            Confirm.show(in: self?.window, title: "Delete the identity \(i.name)?",
                         message: "wtf forgets it. The key stays in \(WindowModel.prettyPath(i.keyPath)), and GitHub keeps it. Tabs using it keep their variables until they change them or close.",
                         button: "Delete Identity") {
                Task {
                    do {
                        SshStore.shared.identities = try await Services.sshhosts.DeleteGitIdentity(i.id)
                        SshStore.shared.changed()
                    } catch {
                        Toast.show(error.localizedDescription, in: o.rootView)
                    }
                }
            }
        }
        return m
    }
}

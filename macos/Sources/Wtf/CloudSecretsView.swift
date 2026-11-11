import AppKit
import WtfCore

// The Cloud Secrets tab (#18): AWS Secrets Manager, Parameter Store and
// Vault, as the focused shell sees them. Lists hold names only; a value is
// read when its dialog opens and dropped when it closes. Every write is
// planned, confirmed, and checked against the version the user saw
// (internal/cloudsecrets). Also the AWS permissions dialog (#15).

/// What one window's Cloud Secrets tab shows; kept while the window is open,
/// so switching tabs doesn't read everything again.
@MainActor
final class SecretsState {
    /// A region picked in the tab; "" follows the shell.
    var region = ""
    /// The Vault folder shown; "" lists the mounts.
    var vaultPath = ""
    /// Each Vault mount's KV version, from the mounts list.
    var kv: [String: Int] = [:]
    var target: Go.cloudsecrets.Target?
    var items: [Go.cloudsecrets.Item]?
    var mounts: [Go.cloudsecrets.Mount]?
    var error = ""
    var loading = false
    var seq = 0
    /// What the list was read for (store, shell, region, path).
    var loadedFor = ""

    /// The KV version of the mount a path is in (2 when unknown).
    func kvVersion(_ path: String) -> Int {
        kv.filter { path.hasPrefix($0.key) }.max { $0.key.count < $1.key.count }?.value ?? 2
    }
}

@MainActor
private var secretsStates: [ObjectIdentifier: SecretsState] = [:]

extension WindowController {
    var secretsState: SecretsState {
        if let s = secretsStates[ObjectIdentifier(self)] { return s }
        let s = SecretsState()
        secretsStates[ObjectIdentifier(self)] = s
        return s
    }
}

@MainActor
final class CloudSecretsView: NSView, SidePaneTab, NSSearchFieldDelegate, NSComboBoxDelegate {
    static let sources = [("secretsmanager", "Secrets Manager"), ("ssm", "Parameters"), ("vault", "Vault")]
    /// The shell variables that change what the tab shows (not PWD).
    private static let watched = ["AWS_PROFILE", "AWS_DEFAULT_PROFILE", "AWS_VAULT", "AWS_REGION", "AWS_DEFAULT_REGION",
                                  "AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE", "WTF_AWS_ENV_KEYS", "VAULT_ADDR", "VAULT_NAMESPACE", "VAULT_CACERT"]

    private unowned let owner: WindowController
    private var state: SecretsState { owner.secretsState }
    private let segments = NSSegmentedControl(labels: sources.map(\.1), trackingMode: .selectOne, target: nil, action: nil)
    private let header = PaneHeader("Cloud Secrets")
    private let where_ = NSTextField(labelWithString: "")
    private let region = NSComboBox()
    private let path = NSTextField()
    private var up: ToolButton!
    private let filter = NSSearchField()
    private let list = SideList()
    private var note: NoteView?
    private var noteLink: NSButton?
    private var reloadButton: ToolButton?

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        segments.segmentDistribution = .fillEqually
        segments.target = self
        segments.action = #selector(switched)
        segments.refusesFirstResponder = true
        addSubview(segments)
        header.tool("plus", "New Secret…") { [weak self] in self?.create() }
        reloadButton = header.tool("arrow.clockwise", "Read again") { [weak self] in self?.load(force: true) }
        addSubview(header)
        where_.font = .systemFont(ofSize: 11.5)
        where_.textColor = .secondaryLabelColor
        where_.lineBreakMode = .byTruncatingMiddle
        addSubview(where_)
        region.font = .monospacedSystemFont(ofSize: 11.5, weight: .regular)
        region.addItems(withObjectValues: Kube.regions)
        region.completes = true
        region.refusesFirstResponder = true
        region.toolTip = "The region to show. Clear it to follow the shell's"
        region.delegate = self
        region.target = self
        region.action = #selector(regionCommitted)
        addSubview(region)
        path.font = .monospacedSystemFont(ofSize: 11.5, weight: .regular)
        path.placeholderString = "Mounts · type a path, like secret/app"
        path.refusesFirstResponder = true
        path.target = self
        path.action = #selector(pathCommitted)
        addSubview(path)
        up = ToolButton(symbol: "arrow.up", tip: "Up one folder") { [weak self] in self?.goUp() }
        addSubview(up)
        filter.placeholderString = "Filter by name"
        filter.delegate = self
        filter.sendsSearchStringImmediately = true
        filter.refusesFirstResponder = true
        addSubview(filter)
        list.onDoubleClick = { [weak self] id in self?.open(id) }
        list.menuProvider = { [weak self] ids in ids.first.flatMap { self?.menu($0) } }
        addSubview(list)
        Redaction.shared.observe(self) { $0.rebuild() }
        segments.selectedSegment = Self.sources.firstIndex { $0.0 == window.sidebar.secretsSource } ?? 0
        load()
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    private var source: String { owner.sidebar.secretsSource }
    private var isVault: Bool { source == "vault" }

    @objc private func switched() {
        owner.sidebar.secretsSource = Self.sources[max(0, segments.selectedSegment)].0
        owner.sidebarChanged()
        load()
    }

    private var env: [String: String] { owner.activePane?.env ?? [:] }

    private var request: Go.cloudsecrets.Request { Go.cloudsecrets.Request(env: env, region: state.region) }

    private var key: String {
        let vars = Self.watched.map { "\($0)=\(env[$0] ?? "")" }.joined(separator: "\n")
        return [source, vars, state.region, isVault ? state.vaultPath : ""].joined(separator: "\n")
    }

    /// The focused shell changed: read again only if what it points at did.
    func paneChanged() { load() }

    func debugSummary() -> String {
        "secrets: \(source), \(state.items?.count ?? state.mounts?.count ?? -1) rows, error \(state.error)"
    }

    /// Reads the list for the store shown, unless it's already read for the same shell.
    func load(force: Bool = false) {
        let k = key
        guard force || k != state.loadedFor else { return rebuild() }
        state.loadedFor = k
        state.seq += 1
        let mine = state.seq
        state.loading = true
        state.error = ""
        state.items = nil
        state.mounts = nil
        rebuild()
        let req = request, src = source, vaultPath = state.vaultPath
        Task {
            var items: [Go.cloudsecrets.Item]?
            var mounts: [Go.cloudsecrets.Mount]?
            var failure = ""
            let target = try? await Services.cloudsecrets.Target(req)
            do {
                switch src {
                case "vault":
                    if vaultPath.isEmpty { mounts = try await Services.cloudsecrets.VaultMounts(req) } else { items = try await Services.cloudsecrets.VaultList(req, vaultPath) }
                default:
                    if let e = target?.awsError, !e.isEmpty { throw CloudSecretsError(e) }
                    items = src == "ssm" ? try await Services.cloudsecrets.ListParameters(req) : try await Services.cloudsecrets.ListSecrets(req)
                }
            } catch {
                failure = error.localizedDescription
            }
            guard mine == state.seq else { return }
            state.target = target
            state.items = items
            state.mounts = mounts
            for m in mounts ?? [] { state.kv[m.path] = m.kv }
            state.error = failure
            state.loading = false
            rebuild()
        }
    }

    /// How long ago an AWS or Vault time was; nil if it can't be read (the
    /// CLI's microseconds are cut to milliseconds first).
    static func age(_ iso: String) -> String? {
        var t = iso
        if let r = t.range(of: #"\.(\d{3})\d+"#, options: .regularExpression) { t.replaceSubrange(r, with: "." + t[r].dropFirst().prefix(3)) }
        let a = Kube.age(t)
        return a.isEmpty ? nil : a
    }

    // MARK: Building

    private var query: String { filter.stringValue.trimmingCharacters(in: .whitespaces).lowercased() }

    private func rebuild() {
        reloadButton?.isEnabled = !state.loading
        let t = state.target
        if isVault {
            let addr = t?.vaultAddr ?? ""
            where_.stringValue = (addr.isEmpty ? "VAULT_ADDR not set in this shell" : addr) + ((t?.vaultNamespace ?? "").isEmpty ? "" : " · namespace \(t!.vaultNamespace)")
            where_.toolTip = "The focused shell's VAULT_ADDR and VAULT_NAMESPACE. The token is the vault CLI's own (vault login)."
            if path.currentEditor() == nil { path.stringValue = state.vaultPath }
            up.isEnabled = !state.vaultPath.isEmpty
        } else if let t {
            where_.stringValue = t.profile.isEmpty ? "No AWS profile" : t.profile + (t.account.isEmpty ? "" : " · \(t.account)")
            where_.toolTip = "The focused shell's AWS profile and account"
            if region.currentEditor() == nil { region.stringValue = t.region }
        } else {
            where_.stringValue = ""
        }
        note?.removeFromSuperview()
        note = nil
        noteLink?.removeFromSuperview()
        noteLink = nil
        let q = query
        var rows: [SideList.Row] = []
        if let mounts = state.mounts {
            for m in mounts where q.isEmpty || m.path.lowercased().contains(q) {
                rows.append(.init(id: "m:\(m.path)", symbol: "externaldrive", tint: .secondaryLabelColor, title: m.path, mono: true,
                                  detail: "KV \(m.kv)" + (m.description.isEmpty ? "" : " · \(m.description)"), tip: "Double-click to open"))
            }
        }
        for it in state.items ?? [] where q.isEmpty || it.name.lowercased().contains(q) || it.description.lowercased().contains(q) {
            rows.append(row(it))
        }
        list.set([.init(title: nil, rows: rows)])
        if state.loading {
            note = paneNote(nil, "Reading…")
        } else if !state.error.isEmpty {
            let hint = isVault && state.vaultPath.isEmpty ? " Type a path above (like secret/app) to open it directly." : ""
            note = paneNote(isVault ? "Vault didn’t answer" : "Can’t list secrets", state.error + hint)
            noteLink = owner.installLink(for: state.error)
            if let noteLink { addSubview(noteLink) }
        } else if rows.isEmpty, state.items != nil || state.mounts != nil {
            note = q.isEmpty
                ? paneNote(nil, isVault ? (state.vaultPath.isEmpty ? "No key/value mounts." : "This folder is empty.") : "None in this account and region. Press + to add one.")
                : paneNote(nil, "Nothing matches “\(filter.stringValue)”.")
        }
        if let note { addSubview(note) }
        needsLayout = true
    }

    private func row(_ it: Go.cloudsecrets.Item) -> SideList.Row {
        if isVault {
            return .init(id: "i:\(it.name)", symbol: it.folder ? "folder" : "key", tint: .secondaryLabelColor, title: it.name, mono: true,
                         tip: it.folder ? "Double-click to open" : "Double-click to see its value")
        }
        var detail: [String] = []
        if source == "ssm" { detail.append("\(it.type) · v\(it.version)") }
        if let a = Self.age(it.changed) { detail.append("changed \(a) ago") }
        if it.rotation { detail.append("rotation on") }
        var tip = [it.name]
        if !it.description.isEmpty { tip.append(it.description) }
        if !it.changed.isEmpty { tip.append("Changed \(it.changed)") }
        if !it.accessed.isEmpty { tip.append("Last read \(it.accessed)") }
        return .init(id: "i:\(it.name)", symbol: source == "ssm" ? "slider.horizontal.3" : "key", tint: .secondaryLabelColor, title: it.name, mono: true,
                     detail: detail.joined(separator: " · "), tip: tip.joined(separator: "\n"))
    }

    override func layout() {
        super.layout()
        var y: CGFloat = 4
        segments.frame = NSRect(x: 6, y: y, width: bounds.width - 12, height: 24)
        y += 28
        header.frame = NSRect(x: 0, y: y, width: bounds.width, height: 30)
        y += 30
        where_.frame = NSRect(x: 10, y: y, width: bounds.width - 20, height: 16)
        y += 18
        region.isHidden = isVault
        path.isHidden = !isVault
        up.isHidden = !isVault
        if isVault {
            up.frame = NSRect(x: 6, y: y, width: 24, height: 24)
            path.frame = NSRect(x: 32, y: y, width: bounds.width - 40, height: 24)
        } else {
            region.frame = NSRect(x: 8, y: y, width: bounds.width - 16, height: 24)
        }
        y += 28
        filter.frame = NSRect(x: 8, y: y + 2, width: bounds.width - 16, height: 24)
        y += 30
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

    func controlTextDidChange(_ obj: Notification) { if obj.object as? NSSearchField === filter { rebuild() } }

    func comboBoxSelectionDidChange(_ notification: Notification) {
        DispatchQueue.main.async { [weak self] in self?.regionCommitted() }
    }

    @objc private func regionCommitted() {
        let r = region.stringValue.trimmingCharacters(in: .whitespaces)
        guard r != state.region, r != state.target?.region || r.isEmpty else { return }
        state.region = r
        load()
    }

    @objc private func pathCommitted() {
        var p = path.stringValue.trimmingCharacters(in: .whitespaces)
        while p.hasPrefix("/") { p.removeFirst() }
        if !p.isEmpty, !p.hasSuffix("/") { p += "/" }
        go(p)
    }

    private func go(_ p: String) {
        state.vaultPath = p
        path.stringValue = p
        load()
    }

    private func goUp() {
        var parts = state.vaultPath.split(separator: "/").map(String.init)
        guard !parts.isEmpty else { return }
        parts.removeLast()
        go(parts.isEmpty ? "" : parts.joined(separator: "/") + "/")
    }

    // MARK: Actions

    /// The full name of a row: a Vault path, or the secret's name.
    private func fullName(_ name: String) -> String { isVault ? state.vaultPath + name : name }

    private func item(_ id: String) -> Go.cloudsecrets.Item? {
        guard id.hasPrefix("i:") else { return nil }
        return state.items?.first { $0.name == String(id.dropFirst(2)) }
    }

    private func open(_ id: String) {
        if id.hasPrefix("m:") { return go(String(id.dropFirst(2))) }
        guard let it = item(id) else { return }
        if it.folder { return go(state.vaultPath + it.name) }
        show(it, edit: false)
    }

    private func show(_ it: Go.cloudsecrets.Item, edit: Bool) {
        let name = fullName(it.name)
        CloudSecretViewer(store: source, name: name, kv: state.kvVersion(name), request: request, where_: whereText, owner: owner,
                          editNow: edit) { [weak self] in self?.load(force: true) }.show()
    }

    private var whereText: String {
        guard let t = state.target else { return "" }
        if isVault { return (t.vaultAddr.isEmpty ? "Vault" : t.vaultAddr) + (t.vaultNamespace.isEmpty ? "" : " · namespace \(t.vaultNamespace)") }
        return "\(t.profile)\(t.account.isEmpty ? "" : " · \(t.account)") · \(t.region)"
    }

    private func create() {
        if !isVault, let e = state.target?.awsError, !e.isEmpty { return Toast.show(e, in: owner.rootView) }
        if Redaction.shared.on { return Toast.show(CloudSecretViewer.hiddenNote, in: owner.rootView) }
        CloudSecretEditor.show(store: source, name: isVault ? state.vaultPath : "", value: nil, kv: state.kvVersion(state.vaultPath),
                               request: request, where_: whereText, in: owner) { [weak self] in self?.load(force: true) }
    }

    private func menu(_ id: String) -> NSMenu? {
        let m = NSMenu()
        m.autoenablesItems = false
        func add(_ t: String, _ run: @escaping () -> Void) { m.addItem(ClosureMenuItem(title: t, keyEquivalent: "", action: run)) }
        let o = owner
        if id.hasPrefix("m:") {
            let p = String(id.dropFirst(2))
            add("Open") { [weak self] in self?.go(p) }
            add("Copy Path") { Clipboard.copy(p); Toast.show("Copied \(p)", in: o.rootView) }
            return m
        }
        guard let it = item(id) else { return nil }
        let name = fullName(it.name)
        if it.folder {
            add("Open") { [weak self] in self?.go(name) }
        } else {
            add("Show Value…") { [weak self] in self?.show(it, edit: false) }
            add("Edit Value…") { [weak self] in self?.show(it, edit: true) }
        }
        m.addItem(.separator())
        add(isVault ? "Copy Path" : "Copy Name") { Clipboard.copy(name); Toast.show("Copied \(name)", in: o.rootView) }
        return m
    }
}

struct CloudSecretsError: LocalizedError {
    let message: String
    init(_ m: String) { message = m }
    var errorDescription: String? { message }
}

// MARK: - A secret's value

@MainActor
final class CloudSecretViewer: Viewer {
    static let hiddenNote = "Hide Secrets is on: turn it off to see or change values"

    private let store: String
    private let name: String
    private let kv: Int
    private let request: Go.cloudsecrets.Request
    private let whereText: String
    private let saved: () -> Void
    private var value: Go.cloudsecrets.Value?
    private var revealed = Set<String>()
    private var error: String?

    /// `editNow` goes straight to the editor once the value is read.
    init(store: String, name: String, kv: Int, request: Go.cloudsecrets.Request, where_: String, owner: WindowController,
         editNow: Bool = false, saved: @escaping () -> Void) {
        self.store = store
        self.name = name
        self.kv = kv
        self.request = request
        self.whereText = where_
        self.saved = saved
        super.init(owner: owner, width: 580)
        onLosingFocus { [weak self] in
            guard let self, !revealed.isEmpty else { return }
            revealed.removeAll()
            render()
        }
        render()
        Task {
            do {
                switch store {
                case "ssm": value = try await Services.cloudsecrets.ReadParameter(request, name)
                case "vault": value = try await Services.cloudsecrets.VaultRead(request, name, kv)
                default: value = try await Services.cloudsecrets.ReadSecret(request, name)
                }
            } catch {
                self.error = error.localizedDescription
            }
            render()
            if editNow, value != nil { edit() }
        }
    }

    override func closed() {
        value = nil
        revealed = []
    }

    /// One entry per field, or the whole value as one.
    private var entries: [(key: String, value: String)] {
        guard let v = value else { return [] }
        return v.fields.isEmpty ? [("value", v.text)] : v.fields.map { ($0.key, $0.value) }
    }

    private func render() {
        clear()
        var sub = [whereText]
        if let v = value {
            if !v.type.isEmpty { sub.append(v.type) }
            if !v.version.isEmpty { sub.append("version \(v.version.count > 12 ? String(v.version.prefix(8)) + "…" : v.version)") }
            if let a = CloudSecretsView.age(v.changed) { sub.append("changed \(a) ago") }
        }
        title(name, sub.filter { !$0.isEmpty }.joined(separator: " · ") + ". Values hide again when wtf loses focus.")
        let hidden = Redaction.shared.on
        if let error {
            content.addArrangedSubview(value(error, color: .systemRed))
        } else if value == nil {
            content.addArrangedSubview(value("Reading…", color: .secondaryLabelColor))
        } else {
            if hidden { content.addArrangedSubview(value(Self.hiddenNote + ".", color: .systemOrange)) }
            let list = NSStackView()
            list.orientation = .vertical
            list.alignment = .leading
            list.spacing = 8
            let o = owner
            for e in entries {
                let key = NSTextField(labelWithString: e.key)
                key.font = NSFontManager.shared.convert(Self.monoFont(12), toHaveTrait: .boldFontMask)
                key.lineBreakMode = .byTruncatingTail
                key.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
                let meta = NSTextField(labelWithString: (value?.binary == true ? "binary (base64) · " : "") + "\(e.value.count) characters")
                meta.font = .systemFont(ofSize: 11)
                meta.textColor = .secondaryLabelColor
                let shown = revealed.contains(e.key) && !hidden
                let eye = ToolButton(symbol: shown ? "eye.slash" : "eye", tip: shown ? "Hide" : "Reveal") { [weak self] in
                    guard let self else { return }
                    if Redaction.shared.on { return Toast.show(Self.hiddenNote, in: o.rootView) }
                    if shown { revealed.remove(e.key) } else { revealed.insert(e.key) }
                    render()
                }
                eye.setAccessibilityLabel((shown ? "Hide " : "Reveal ") + e.key)
                let copy = ToolButton(symbol: "doc.on.doc", tip: "Copy") {
                    if Redaction.shared.on { return Toast.show(Self.hiddenNote, in: o.rootView) }
                    Clipboard.copySecret(e.value, what: e.key, in: o.rootView)
                }
                copy.setAccessibilityLabel("Copy \(e.key)")
                let head = NSStackView(views: [key, meta, spacer(), eye, copy])
                head.translatesAutoresizingMaskIntoConstraints = false
                head.widthAnchor.constraint(equalToConstant: inner - 14).isActive = true
                list.addArrangedSubview(head)
                if shown {
                    list.addArrangedSubview(textBox(Self.mono(e.value, size: 11.5), maxHeight: maxBoxHeight * 0.3, wrap: true))
                } else {
                    let dots = NSTextField(labelWithString: "••••••••")
                    dots.textColor = .secondaryLabelColor
                    dots.setAccessibilityLabel("hidden")
                    list.addArrangedSubview(dots)
                }
            }
            let scroll = NSScrollView()
            let doc = FlippedView()
            doc.addSubview(list)
            let h = Self.fit(list, width: inner - 14)
            doc.frame = list.frame
            scroll.documentView = doc
            scroll.hasVerticalScroller = true
            scroll.autohidesScrollers = true
            scroll.drawsBackground = false
            scroll.translatesAutoresizingMaskIntoConstraints = false
            scroll.widthAnchor.constraint(equalToConstant: inner).isActive = true
            scroll.heightAnchor.constraint(equalToConstant: min(h, maxBoxHeight * 0.6)).isActive = true
            content.addArrangedSubview(scroll)
        }
        var left: [NSView] = []
        if value != nil {
            let all = revealed.count == entries.count && !hidden
            left.append(ClosureButton(title: all ? "Hide All" : "Reveal All") { [weak self] in
                guard let self else { return }
                if Redaction.shared.on { return Toast.show(Self.hiddenNote, in: owner.rootView) }
                revealed = all ? [] : Set(entries.map(\.key))
                render()
            })
            if value?.binary == false {
                left.append(ClosureButton(title: "Edit…") { [weak self] in self?.edit() })
            }
        }
        done(left)
    }

    private func edit() {
        guard let v = value else { return }
        if Redaction.shared.on { return Toast.show(Self.hiddenNote, in: owner.rootView) }
        if v.binary { return Toast.show("Binary secrets can't be edited here", in: owner.rootView) }
        let (o, s, n, k, r, w, done) = (owner, store, name, kv, request, whereText, saved)
        close()
        CloudSecretEditor.show(store: s, name: n, value: v, kv: k, request: r, where_: w, in: o, saved: done)
    }
}

// MARK: - New value or new secret

@MainActor
enum CloudSecretEditor {
    static let types = ["SecureString", "String", "StringList"]

    /// Edits `value` when given, else creates a secret (`name` prefills it).
    static func show(store: String, name: String, value: Go.cloudsecrets.Value?, kv: Int, request: Go.cloudsecrets.Request,
                     where_: String, in w: WindowController, saved: @escaping () -> Void) {
        let creating = value == nil
        let vault = store == "vault"
        let noun = vault ? "Vault Secret" : store == "ssm" ? "Parameter" : "Secret"
        let form = FormSheet(title: creating ? "New \(noun)" : "New Value for \(name)", subtitle: where_, width: 560)
        let nameField = form.text(name, placeholder: vault ? "secret/app/db" : store == "ssm" ? "/app/prod/db-password" : "prod/app/db")
        nameField.font = .monospacedSystemFont(ofSize: 12, weight: .regular)
        let typePopup = form.popup(types)
        let desc = form.text("", placeholder: "Optional")
        if creating {
            form.field(vault ? "Path" : "Name", nameField, help: vault ? "The mount and path, like secret/app/db. KV version \(kv) mount." : nil)
            if store == "ssm" { form.field("Type", typePopup, help: "SecureString is encrypted with the account's default key (aws/ssm).") }
            if !vault { form.field("Description", desc) }
        }
        var text = value?.text ?? (vault ? "{\n  \"key\": \"\"\n}" : "")
        let editor = BoundTextView(text) { text = $0 }
        editor.isAutomaticTextReplacementEnabled = false
        editor.placeholder = store == "secretsmanager" ? "A value, or JSON like {\"user\": \"…\", \"password\": \"…\"}" : "The value"
        let scroll = NSScrollView()
        scroll.documentView = editor
        scroll.hasVerticalScroller = true
        scroll.borderType = .bezelBorder
        scroll.translatesAutoresizingMaskIntoConstraints = false
        scroll.heightAnchor.constraint(equalToConstant: 180).isActive = true
        editor.frame = NSRect(x: 0, y: 0, width: 560 - 48 - 4, height: 180)
        form.field("Value", scroll, help: vault ? "A JSON object of keys and values." : "Shown only here. The plan you confirm next never shows it.")
        var busy = false
        func change() -> Go.cloudsecrets.Change {
            Go.cloudsecrets.Change(store: store, name: creating ? nameField.stringValue.trimmingCharacters(in: .whitespaces) : name,
                                   create: creating, value: text, type: store == "ssm" && creating ? typePopup.titleOfSelectedItem ?? "SecureString" : "",
                                   description: creating && !vault ? desc.stringValue : "", version: value?.version ?? "", kv: kv)
        }
        form.buttons([("Review…", true, {
            guard !busy else { return }
            busy = true
            form.error = nil
            let c = change()
            Task {
                do {
                    let steps = try await Services.cloudsecrets.PlanChange(request, c)
                    busy = false
                    Confirm.show(in: form.panel, title: creating ? "Create \(c.name)?" : "Save a new value for \(c.name)?",
                                 message: "wtf will make this change, then check nothing else changed it first.", items: steps,
                                 button: creating ? "Create" : "Save", limit: .max) {
                        busy = true
                        Task {
                            do {
                                try await Services.cloudsecrets.ApplyChange(request, c)
                                text = ""
                                form.close()
                                Toast.show(creating ? "Created \(c.name)" : "Saved a new value for \(c.name)", in: w.rootView)
                                saved()
                            } catch {
                                form.error = error.localizedDescription
                            }
                            busy = false
                        }
                    }
                } catch {
                    busy = false
                    form.error = error.localizedDescription
                }
            }
        })])
        form.onClose = { text = "" }
        form.present(in: w.window, focus: creating ? nameField : editor)
    }
}

// MARK: - AWS permissions (#15)

@MainActor
final class AwsPermissionsViewer: Viewer {
    private let profile: String
    private var perms: Go.awsprofiles.Permissions?
    private var error: String?
    /// Policies whose document is shown: key → document, nil while loading.
    private var docs: [String: String?] = [:]
    private var docErrors: [String: String] = [:]

    static func show(_ profile: String, in w: WindowController) {
        AwsPermissionsViewer(profile: profile, owner: w).show()
    }

    private init(profile: String, owner: WindowController) {
        self.profile = profile
        super.init(owner: owner, width: 620)
        render()
        Task {
            do { perms = try await Services.awsprofiles.Permissions(profile) } catch { self.error = error.localizedDescription }
            render()
        }
    }

    private func key(_ p: Go.awsprofiles.Policy) -> String { "\(p.via)\n\(p.owner)\n\(p.name)\n\(p.arn)" }

    private static func kindText(_ k: String) -> String {
        switch k {
        case "user": "IAM user"
        case "role": "Assumed role"
        case "root": "Root user"
        default: "Other"
        }
    }

    private func render() {
        clear()
        title("Permissions of \(profile)", "The IAM policies attached to these credentials. This lists them, it doesn't evaluate them: organization SCPs, resource policies and session policies can still allow less.")
        if let error {
            content.addArrangedSubview(value(error, color: .systemRed))
            if let link = owner.installLink(for: error) { content.addArrangedSubview(link) }
            return done()
        }
        guard let p = perms else {
            content.addArrangedSubview(value("Asking AWS…", color: .secondaryLabelColor))
            return done()
        }
        var facts: [(String, NSView)] = [("ARN", value(p.identity.arn, mono: true)), ("Account", value(p.identity.account, mono: true)),
                                         ("Kind", value(Self.kindText(p.kind)))]
        if !p.name.isEmpty { facts.append((p.kind == "role" ? "Role" : "User", value(p.name, mono: true))) }
        if !p.session.isEmpty { facts.append(("Session", value(p.session, mono: true))) }
        if !p.groups.isEmpty { facts.append(("Groups", value(p.groups.joined(separator: ", "), mono: true))) }
        content.addArrangedSubview(self.facts(facts))
        if p.kind == "root" {
            content.addArrangedSubview(value("The root user can do anything in the account; IAM policies don't limit it.", color: .systemOrange))
        } else if p.kind == "other" {
            content.addArrangedSubview(value("wtf can't list policies for this kind of identity.", color: .secondaryLabelColor))
        }
        let list = NSStackView()
        list.orientation = .vertical
        list.alignment = .leading
        list.spacing = 6
        var groups: [(String, [Go.awsprofiles.Policy])] = []
        for pol in p.policies {
            let g = switch pol.via {
            case "boundary": "Permissions boundary"
            case "group": "Group \(pol.owner)"
            case "role": "Attached to role \(pol.owner)"
            default: "Attached to user \(pol.owner)"
            }
            if let i = groups.firstIndex(where: { $0.0 == g }) { groups[i].1.append(pol) } else { groups.append((g, [pol])) }
        }
        if p.policies.isEmpty, p.kind == "user" || p.kind == "role" {
            list.addArrangedSubview(value("No policies found.", color: .secondaryLabelColor))
        }
        let o = owner
        for (g, pols) in groups {
            list.addArrangedSubview(heading(g))
            for pol in pols {
                let k = key(pol)
                let name = NSTextField(labelWithString: pol.name)
                name.font = Self.monoFont(12)
                name.lineBreakMode = .byTruncatingMiddle
                name.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
                name.toolTip = pol.arn.isEmpty ? pol.name : pol.arn
                var views: [NSView] = [name]
                if pol.awsManaged { views.append(tag("AWS managed")) }
                if pol.inline { views.append(tag("inline")) }
                views.append(spacer())
                let shown = docs.keys.contains(k)
                views.append(ClosureButton(title: shown ? "Hide" : "Show") { [weak self] in self?.toggle(pol) })
                if !pol.arn.isEmpty {
                    let copy = ToolButton(symbol: "doc.on.doc", tip: "Copy ARN") {
                        Clipboard.copy(pol.arn)
                        Toast.show("Copied \(pol.arn)", in: o.rootView)
                    }
                    views.append(copy)
                }
                let row = NSStackView(views: views)
                row.translatesAutoresizingMaskIntoConstraints = false
                row.widthAnchor.constraint(equalToConstant: inner - 14).isActive = true
                list.addArrangedSubview(row)
                if let e = docErrors[k] {
                    list.addArrangedSubview(value(e, color: .systemRed))
                } else if shown {
                    if let doc = docs[k] ?? nil {
                        list.addArrangedSubview(textBox(JsonViewer.colored(doc), maxHeight: maxBoxHeight * 0.35, wrap: false))
                    } else {
                        list.addArrangedSubview(value("Reading the policy…", color: .secondaryLabelColor))
                    }
                }
            }
        }
        if !p.problems.isEmpty {
            list.addArrangedSubview(heading("IAM didn’t say"))
            for pr in p.problems { list.addArrangedSubview(value(pr, color: .systemRed)) }
        }
        let scroll = NSScrollView()
        let doc = FlippedView()
        doc.addSubview(list)
        let h = Self.fit(list, width: inner - 14)
        doc.frame = list.frame
        scroll.documentView = doc
        scroll.hasVerticalScroller = true
        scroll.autohidesScrollers = true
        scroll.drawsBackground = false
        scroll.translatesAutoresizingMaskIntoConstraints = false
        scroll.widthAnchor.constraint(equalToConstant: inner).isActive = true
        scroll.heightAnchor.constraint(equalToConstant: min(max(h, 1), maxBoxHeight * 0.55)).isActive = true
        content.addArrangedSubview(scroll)
        done()
    }

    private func toggle(_ pol: Go.awsprofiles.Policy) {
        let k = key(pol)
        if docs.keys.contains(k) || docErrors[k] != nil {
            docs.removeValue(forKey: k)
            docErrors.removeValue(forKey: k)
            return render()
        }
        docs[k] = .some(nil)
        render()
        Task {
            do {
                let d = try await Services.awsprofiles.PolicyDocument(profile, pol)
                guard docs.keys.contains(k) else { return }
                docs[k] = d
            } catch {
                docs.removeValue(forKey: k)
                docErrors[k] = error.localizedDescription
            }
            render()
        }
    }
}

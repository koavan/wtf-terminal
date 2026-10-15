import AppKit
import WtfCore

/// The AWS profiles, shared by every window: read when the tab first shows
/// and after each change, never polled. Checks run only when asked and are
/// kept per profile while the app runs. Like lib/aws.svelte.ts.
@MainActor
final class AwsStore {
    static let shared = AwsStore()

    struct Check {
        var busy = false
        var identity: Go.awsprofiles.Identity?
        var identityError: String?
        var keys: [Go.awsprofiles.AccessKey]?
        var keysError: String?
    }

    var list = Go.awsprofiles.List()
    var loaded = false
    var loading = false
    var checks: [String: Check] = [:]
    private var observers = Observers()

    func observe<T: AnyObject>(_ owner: T, _ fn: @escaping (T) -> Void) { observers.add(owner, fn) }
    func changed() { observers.notify() }

    func load() {
        loading = true
        changed()
        Task {
            do { list = try await Services.awsprofiles.List() } catch { list.problems = [error.localizedDescription] }
            loading = false
            loaded = true
            changed()
        }
    }

    func apply(_ l: Go.awsprofiles.List) {
        list = l
        loaded = true
        changed()
    }

    func verify(_ name: String) {
        checks[name, default: Check()].busy = true
        checks[name]?.identityError = nil
        changed()
        Task {
            do {
                let id = try await Services.awsprofiles.Verify(name)
                checks[name]?.identity = id
                checks[name]?.identityError = nil
            } catch {
                checks[name]?.identity = nil
                checks[name]?.identityError = error.localizedDescription
            }
            checks[name]?.busy = false
            changed()
        }
    }

    func checkKeys(_ name: String) {
        checks[name, default: Check()].busy = true
        changed()
        Task {
            do {
                checks[name]?.keys = try await Services.awsprofiles.AccessKeys(name)
                checks[name]?.keysError = nil
            } catch {
                checks[name]?.keys = nil
                checks[name]?.keysError = error.localizedDescription
            }
            checks[name]?.busy = false
            changed()
        }
    }
}

/// The AWS Profiles tab: the profiles in ~/.aws/credentials and
/// ~/.aws/config. Use one in the focused shell, check it with AWS, add,
/// edit, rotate, copy to default, delete — every write planned and
/// confirmed first. Like AwsView.svelte.
@MainActor
final class AwsView: NSView, SidePaneTab, NSSearchFieldDelegate {
    private unowned let owner: WindowController
    private let header = PaneHeader("AWS Profiles")
    private let filter = NSSearchField()
    private let list = SideList()
    private var note: NoteView?
    private var reloadButton: ToolButton?
    private var store: AwsStore { .shared }

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        header.tool("plus", "Add profile") { [weak self] in
            guard let self else { return }
            AwsProfileEditor.show(nil, in: owner)
        }
        reloadButton = header.tool("arrow.clockwise", "Reload") { AwsStore.shared.load() }
        addSubview(header)
        filter.placeholderString = "Filter profiles"
        filter.delegate = self
        filter.sendsSearchStringImmediately = true
        filter.refusesFirstResponder = true
        addSubview(filter)
        list.onDoubleClick = { [weak self] id in self?.profile(id).map { self?.use($0) } }
        list.menuProvider = { [weak self] ids in ids.first.flatMap { self?.profile($0) }.flatMap { self?.menu($0) } }
        addSubview(list)
        store.observe(self) { $0.rebuild() }
        if !store.loaded { store.load() }
        rebuild()
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    override func layout() {
        super.layout()
        header.frame = NSRect(x: 0, y: 0, width: bounds.width, height: 30)
        filter.frame = NSRect(x: 8, y: 32, width: bounds.width - 16, height: 24)
        list.frame = NSRect(x: 0, y: 62, width: bounds.width, height: max(0, bounds.height - 62))
        note?.frame = NSRect(x: 0, y: 62, width: bounds.width, height: note?.height(for: bounds.width) ?? 0)
    }

    func controlTextDidChange(_ obj: Notification) { rebuild() }

    /// The in-use mark follows the focused shell.
    func paneChanged() { rebuild() }

    func debugSummary() -> String { "aws: \(store.list.profiles.count) profiles, problems \(store.list.problems)" }

    private func profile(_ name: String) -> AwsProfile? { store.list.profiles.first { $0.name == name } }

    private func rebuild() {
        reloadButton?.isEnabled = !store.loading
        let inUse = Aws.inUse(owner.activePane)
        let shown = store.list.profiles.filter { Aws.matches($0, filter.stringValue) }
        var sections: [SideList.Section] = [.init(title: nil, rows: shown.map { row($0, inUse: inUse) })]
        if !store.list.problems.isEmpty {
            sections.append(.init(title: "Problems", rows: store.list.problems.map {
                .init(id: "p:\($0)", symbol: "exclamationmark.triangle", tint: .systemRed, title: $0, tip: $0)
            }))
        }
        list.set(sections)
        note?.removeFromSuperview()
        note = nil
        if store.loaded, shown.isEmpty {
            let q = filter.stringValue.trimmingCharacters(in: .whitespaces)
            note = q.isEmpty
                ? paneNote("No AWS profiles", "Profiles from ~/.aws/credentials and ~/.aws/config show here. Press + to add one.")
                : paneNote(nil, "No profiles match “\(filter.stringValue)”.")
            addSubview(note!)
        }
        needsLayout = true
    }

    private func row(_ p: AwsProfile, inUse: String) -> SideList.Row {
        var r = SideList.Row(id: p.name, symbol: "cloud", tint: p.name == inUse ? .controlAccentColor : .secondaryLabelColor,
                             title: p.name, mono: true, detail: Aws.detail(p),
                             pills: p.name == inUse ? [.init(text: "in use", color: .controlAccentColor, tip: "The focused shell uses this profile")] : [],
                             tip: Aws.tooltip(p, credentialsFile: store.list.credentialsFile, configFile: store.list.configFile, pretty: { WindowModel.prettyPath($0) }),
                             button: ("play.fill", "Use in this shell", { [weak self] in self?.use(p) }))
        // A check's result shows as the second line; all of it is in the tooltip.
        guard let c = store.checks[p.name] else { return r }
        var lines: [(String, NSColor)] = []
        if c.busy { lines.append(("Asking AWS…", .secondaryLabelColor)) }
        if let id = c.identity {
            lines.append((Aws.shortArn(id.arn), .systemGreen))
            r.tip = (r.tip ?? "") + "\n\n\(id.arn)"
        } else if let e = c.identityError {
            lines.append((Self.withHint(e), .systemRed))
        }
        if let keys = c.keys {
            lines += keys.map { (Aws.keyLine($0), NSColor.secondaryLabelColor) }
            r.tip = (r.tip ?? "") + "\n\n" + keys.map { "\(Aws.keyLine($0))\nCreated \($0.created)" + ($0.lastUsed.isEmpty ? "" : "\nLast used \($0.lastUsed)") }
                .joined(separator: "\n")
        } else if let e = c.keysError {
            lines.append((Self.withHint(e), .systemRed))
        }
        // Each result on its own line, as in the Wails build.
        if let first = lines.first {
            r.alert = first.0
            r.alertColor = first.1
            r.more = lines.dropFirst().map { (text: $0.0, color: $0.1) }
        }
        return r
    }

    /// A missing aws CLI points at its Install… (in the row's menu).
    private static func withHint(_ e: String) -> String {
        Tools.missingTool(e) == nil ? e : e + " — right-click to install it"
    }

    /// Exports AWS_PROFILE in the focused shell (no Ctrl-U first), or opens a tab with it.
    private func use(_ p: AwsProfile) {
        let cmd = Aws.useCommand(p.name)
        if let pane = owner.activePane, let term = owner.terminals[pane.id], pane.isShell, !pane.running {
            term.type(cmd + "\r")
            term.focus()
            Toast.show("This shell now uses \(p.name)", in: owner.rootView)
        } else {
            owner.model.open(command: cmd)
            Toast.show("Opened a tab using \(p.name)", in: owner.rootView)
        }
    }

    private func menu(_ p: AwsProfile) -> NSMenu {
        let m = NSMenu()
        m.autoenablesItems = false
        func add(_ t: String, _ run: @escaping () -> Void) { m.addItem(ClosureMenuItem(title: t, keyEquivalent: "", action: run)) }
        let o = owner
        func copy(_ text: String, _ toast: String) {
            Clipboard.copy(text)
            Toast.show(toast, in: o.rootView)
        }
        if let c = AwsStore.shared.checks[p.name], let tool = Tools.missingTool(c.identityError) ?? Tools.missingTool(c.keysError) {
            add("Install the AWS CLI…") { ToolsDialog.show(focus: tool, in: o) }
            m.addItem(.separator())
        }
        add("Use in This Shell") { [weak self] in self?.use(p) }
        add("Open New Tab with Profile") { o.model.open(command: Aws.useCommand(p.name)) }
        if p.kind == "sso" { add("Log In with SSO") { o.model.open(command: "aws sso login --profile \(Shell.quote(p.name))") } }
        m.addItem(.separator())
        add("Check Identity (sts)") { AwsStore.shared.verify(p.name) }
        if p.kind == "keys" { add("Check Access Keys (iam)") { AwsStore.shared.checkKeys(p.name) } }
        m.addItem(.separator())
        add("Edit…") { AwsProfileEditor.show(p, in: o) }
        if p.kind == "keys", p.accessKeyId.hasPrefix("AKIA") { add("Rotate Access Key…") { AwsRotate.show(p, in: o) } }
        if p.name != "default" {
            add("Copy to Default…") { [weak self] in
                self?.confirmChange(title: "Copy \(p.name) to default?", button: "Copy to Default",
                                    note: " — the CLI uses [default] when no profile is chosen",
                                    plan: { try await Services.awsprofiles.PlanMakeDefault(p.name, $0) },
                                    run: { try await Services.awsprofiles.MakeDefault(p.name, $0) },
                                    done: "default now has \(p.name)’s settings")
            }
        }
        m.addItem(.separator())
        if !p.accessKeyId.isEmpty { add("Copy Access Key ID") { copy(p.accessKeyId, "Copied access key ID") } }
        if !p.account.isEmpty { add("Copy Account ID (\(p.account))") { copy(p.account, "Copied account ID") } }
        add("Copy Name") { copy(p.name, "Copied profile name") }
        m.addItem(.separator())
        add("Delete…") { [weak self] in
            self?.confirmChange(title: "Delete \(p.name)?", button: "Delete Profile",
                                note: p.accessKeyId.isEmpty ? "" : " (the key stays valid in AWS; this only removes it from this computer)",
                                plan: { try await Services.awsprofiles.PlanDelete(p.name, $0) },
                                run: { try await Services.awsprofiles.Delete(p.name, $0) },
                                done: "Deleted \(p.name)")
        }
        return m
    }

    /// Plans a change, shows every line of it, and makes it once confirmed.
    private func confirmChange(title: String, button: String, note: String,
                               plan: @escaping ([String: String]) async throws -> [String],
                               run: @escaping ([String: String]) async throws -> Go.awsprofiles.List, done: String) {
        let o = owner
        let versions = store.list.versions
        Task {
            let steps: [String]
            do { steps = try await plan(versions) } catch {
                Toast.show(error.localizedDescription, in: o.rootView)
                return
            }
            guard !steps.isEmpty else {
                Toast.show("Nothing to change", in: o.rootView)
                return
            }
            Confirm.show(in: o.window, title: title,
                         message: "wtf will make these changes\(note). A copy of each file is kept in wtf’s cache folder (aws-backups).",
                         items: steps, button: button, limit: .max) {
                Task {
                    do {
                        AwsStore.shared.apply(try await run(versions))
                        Toast.show(done, in: o.rootView)
                    } catch {
                        Toast.show(error.localizedDescription, in: o.rootView)
                    }
                }
            }
        }
    }
}

/// Add or edit a profile: a form, then the exact changes to the two files,
/// written only once confirmed. Secrets are write-only. Like AwsProfileEditor.svelte.
@MainActor
final class AwsProfileEditor: NSObject {
    private static var open: [AwsProfileEditor] = []
    private static let kinds = [("keys", "Access keys"), ("role", "Assume role"), ("sso", "SSO"), ("process", "Credential process")]

    private unowned let owner: WindowController
    private let editing: AwsProfile?
    private let panel: NSPanel
    private let content = NSStackView()
    private var d: Aws.Draft
    private var plan: [String]?
    private var error: String?
    private var busy = false
    private let width: CGFloat = 470

    static func show(_ p: AwsProfile?, in w: WindowController) {
        let e = AwsProfileEditor(p, w)
        open.append(e)
        w.window?.beginSheet(e.panel)
    }

    private init(_ p: AwsProfile?, _ w: WindowController) {
        owner = w
        editing = p
        d = p.map(Aws.draft(of:)) ?? Aws.Draft()
        panel = NSPanel(contentRect: NSRect(x: 0, y: 0, width: 470, height: 400), styleMask: [.titled, .docModalWindow], backing: .buffered, defer: false)
        super.init()
        content.orientation = .vertical
        content.alignment = .leading
        content.spacing = 8
        content.edgeInsets = NSEdgeInsets(top: 18, left: 22, bottom: 16, right: 22)
        content.translatesAutoresizingMaskIntoConstraints = false
        content.widthAnchor.constraint(equalToConstant: width).isActive = true
        panel.contentView = content
        render()
    }

    private var inner: CGFloat { width - 44 }

    private func label(_ s: String, bold: Bool = false, size: CGFloat = 12, muted: Bool = false) -> NSTextField {
        let l = NSTextField(wrappingLabelWithString: s)
        l.font = bold ? .systemFont(ofSize: size, weight: .semibold) : .systemFont(ofSize: size)
        if muted { l.textColor = .secondaryLabelColor }
        l.preferredMaxLayoutWidth = inner
        return l
    }

    private func text(_ value: String, _ placeholder: String, secure: Bool = false, mono: Bool = false, _ set: @escaping (String) -> Void) -> NSTextField {
        let f: NSTextField = secure ? BoundSecureField(value, set) : BoundField(value, set)
        f.placeholderString = placeholder
        if mono { f.font = .monospacedSystemFont(ofSize: 12, weight: .regular) }
        f.widthAnchor.constraint(equalToConstant: inner).isActive = true
        return f
    }

    private func labelled(_ title: String, _ control: NSView, note: String? = nil) {
        var views: [NSView] = [label(title, bold: true), control]
        if let note { views.append(label(note, size: 11, muted: true)) }
        let s = NSStackView(views: views)
        s.orientation = .vertical
        s.alignment = .leading
        s.spacing = 3
        content.addArrangedSubview(s)
    }

    private func watch(_ control: NSControl, _ run: @escaping () -> Void) {
        let w = PopupWatcher(run)
        control.target = w
        control.action = #selector(PopupWatcher.changed)
        objc_setAssociatedObject(control, "watch", w, .OBJC_ASSOCIATION_RETAIN)
    }

    private func render() {
        content.arrangedSubviews.forEach { $0.removeFromSuperview() }
        let list = AwsStore.shared.list
        if let plan {
            content.addArrangedSubview(label("Review Changes", bold: true, size: 15))
            content.addArrangedSubview(label("Nothing is written until you confirm.", muted: true))
            let lines = label(plan.map { "• " + $0 }.joined(separator: "\n"))
            lines.font = .monospacedSystemFont(ofSize: 11.5, weight: .regular)
            lines.isSelectable = true
            content.addArrangedSubview(lines)
            content.addArrangedSubview(label(plan.isEmpty ? "Nothing to change." : "A copy of each file is kept in wtf’s cache folder (aws-backups) before it changes.",
                                             size: 11, muted: true))
            addError()
            let back = ClosureButton(title: "Back") { [weak self] in
                self?.plan = nil
                self?.render()
            }
            back.isEnabled = !busy
            back.keyEquivalent = "\u{1b}"
            var row: [NSView] = [NSView(), back]
            if !plan.isEmpty {
                let save = ClosureButton(title: "Confirm & Save") { [weak self] in self?.save() }
                save.keyEquivalent = "\r"
                save.isEnabled = !busy
                row.append(save)
            }
            buttons(row)
            return
        }
        content.addArrangedSubview(label(editing.map { "Edit \($0.name)" } ?? "New AWS Profile", bold: true, size: 15))
        content.addArrangedSubview(label("In \(WindowModel.prettyPath(list.credentialsFile)) and \(WindowModel.prettyPath(list.configFile))", muted: true))
        let name = text(d.name, "prod") { [weak self] in
            self?.d.name = $0
            self?.updateReview()
        }
        labelled("Name", name, note: (editing?.sourceFor.isEmpty ?? true) ? nil : "Source profile for \(editing!.sourceFor.joined(separator: ", ")): they follow a rename.")
        let kind = NSSegmentedControl(labels: Self.kinds.map(\.1), trackingMode: .selectOne, target: nil, action: nil)
        kind.selectedSegment = Self.kinds.firstIndex { $0.0 == d.kind } ?? -1
        kind.setAccessibilityLabel("Credentials")
        watch(kind) { [weak self, weak kind] in
            guard let self, let kind, kind.selectedSegment >= 0 else { return }
            d.kind = Self.kinds[kind.selectedSegment].0
            render()
        }
        content.addArrangedSubview(kind)
        if d.kind == "keys" {
            labelled("Access key ID", text(d.accessKeyId, "AKIA…", mono: true) { [weak self] in self?.d.accessKeyId = $0 })
            let keeps = editing?.hasSecret == true && d.accessKeyId.trimmingCharacters(in: .whitespaces) == editing?.accessKeyId
            labelled("Secret access key", text(d.secretAccessKey, keeps ? "unchanged" : "", secure: true) { [weak self] in self?.d.secretAccessKey = $0 },
                     note: (keeps ? "Leave empty to keep the current secret. " : "") + "wtf never shows a saved secret again.")
            let token = text(d.sessionToken, editing?.hasSessionToken == true ? "unchanged" : "only for temporary (ASIA…) keys", secure: true) { [weak self] in self?.d.sessionToken = $0 }
            token.isEnabled = !d.removeSessionToken
            labelled("Session token", token)
            if editing?.hasSessionToken == true {
                let rm = NSButton(checkboxWithTitle: "Remove the session token", target: nil, action: nil)
                rm.state = d.removeSessionToken ? .on : .off
                watch(rm) { [weak self, weak rm, weak token] in
                    self?.d.removeSessionToken = rm?.state == .on
                    token?.isEnabled = rm?.state != .on
                }
                content.addArrangedSubview(rm)
            }
        } else {
            for f in Aws.kindFields[d.kind] ?? [] {
                if f.key == "source_profile" {
                    let combo = BoundCombo(d.fields[f.key] ?? "") { [weak self] in self?.d.fields[f.key] = $0 }
                    combo.addItems(withObjectValues: list.profiles.map(\.name).filter { $0 != editing?.name })
                    combo.placeholderString = f.placeholder
                    combo.widthAnchor.constraint(equalToConstant: inner).isActive = true
                    labelled(f.label, combo)
                } else {
                    labelled(f.label, text(d.fields[f.key] ?? "", f.placeholder) { [weak self] in self?.d.fields[f.key] = $0 })
                }
            }
        }
        let region = BoundCombo(d.region) { [weak self] in self?.d.region = $0 }
        region.addItems(withObjectValues: Aws.regions)
        region.placeholderString = "us-east-1"
        region.widthAnchor.constraint(equalToConstant: 200).isActive = true
        let output = NSPopUpButton()
        output.addItems(withTitles: Aws.outputs.map { $0.isEmpty ? "default (json)" : $0 })
        output.selectItem(at: Aws.outputs.firstIndex(of: d.output) ?? 0)
        watch(output) { [weak self, weak output] in self?.d.output = Aws.outputs[max(0, output?.indexOfSelectedItem ?? 0)] }
        let pair = NSStackView(views: [label("Region", bold: true), region, label("Output", bold: true), output])
        content.addArrangedSubview(pair)
        let more = BoundTextView(d.more) { [weak self] in self?.d.more = $0 }
        more.placeholder = "duration_seconds = 3600"
        let scroll = NSScrollView()
        scroll.documentView = more
        scroll.hasVerticalScroller = true
        scroll.borderType = .bezelBorder
        scroll.translatesAutoresizingMaskIntoConstraints = false
        scroll.widthAnchor.constraint(equalToConstant: inner).isActive = true
        scroll.heightAnchor.constraint(equalToConstant: 44).isActive = true
        more.frame = NSRect(x: 0, y: 0, width: inner - 4, height: 44)
        let locked = editing?.locked ?? []
        labelled("More settings", scroll, note: "Other ~/.aws/config settings, one “key = value” per line." + (locked.isEmpty ? "" : " Nested settings (\(locked.joined(separator: ", "))) are kept as they are."))
        if let other = editing?.credentialsOther, !other.isEmpty {
            content.addArrangedSubview(label("Also in the credentials file, kept as written: \(other.map { "\($0.key) = \($0.value)" }.joined(separator: ", ")).", size: 11, muted: true))
        }
        addError()
        let cancel = ClosureButton(title: "Cancel") { [weak self] in self?.close() }
        cancel.keyEquivalent = "\u{1b}"
        let review = ClosureButton(title: "Review…") { [weak self] in self?.review() }
        review.keyEquivalent = "\r"
        reviewButton = review
        updateReview()
        buttons([NSView(), cancel, review])
        panel.initialFirstResponder = name
    }

    private weak var reviewButton: NSButton?
    private func updateReview() { reviewButton?.isEnabled = !busy && !d.name.trimmingCharacters(in: .whitespaces).isEmpty }

    private func addError() {
        guard let error else { return }
        let e = label(error)
        e.textColor = .systemRed
        content.addArrangedSubview(e)
    }

    private func buttons(_ views: [NSView]) {
        let row = NSStackView(views: views)
        row.widthAnchor.constraint(equalToConstant: inner).isActive = true
        content.addArrangedSubview(row)
        content.layoutSubtreeIfNeeded()
        panel.setContentSize(NSSize(width: width, height: content.fittingSize.height))
    }

    private func goDraft() throws -> Go.awsprofiles.Draft {
        try Aws.goDraft(d, original: editing?.name ?? "", versions: AwsStore.shared.list.versions)
    }

    private func review() {
        guard !busy, !d.name.trimmingCharacters(in: .whitespaces).isEmpty else {
            error = "Give the profile a name"
            render()
            return
        }
        let draft: Go.awsprofiles.Draft
        do { draft = try goDraft() } catch {
            self.error = error.localizedDescription
            render()
            return
        }
        busy = true
        Task {
            do {
                plan = try await Services.awsprofiles.PlanSave(draft)
                error = nil
            } catch {
                self.error = error.localizedDescription
            }
            busy = false
            render()
        }
    }

    private func save() {
        guard !busy, let draft = try? goDraft() else { return }
        busy = true
        render()
        Task {
            do {
                AwsStore.shared.apply(try await Services.awsprofiles.Save(draft))
                close()
                Toast.show(editing != nil ? "Saved \(draft.name)" : "Added \(draft.name)", in: owner.rootView)
            } catch {
                self.error = error.localizedDescription
                plan = nil
                busy = false
                render()
            }
        }
    }

    private func close() {
        owner.window?.endSheet(panel)
        Self.open.removeAll { $0 === self }
        owner.focusActive()
    }
}

/// Rotate an IAM user's access key: check, show the plan, then create,
/// write, verify, deactivate and delete — undoing what it can on failure.
/// The new secret never reaches the UI. Like AwsRotate.svelte.
@MainActor
final class AwsRotate: NSObject {
    private static var open: [AwsRotate] = []
    enum Phase { case checking, ready, rotating, done, failed }

    private unowned let owner: WindowController
    private let profile: AwsProfile
    private let panel: NSPanel
    private let content = NSStackView()
    private var phase = Phase.checking
    private var deleteOld = true
    private var lines: [String] = []
    private var error: String?
    private let width: CGFloat = 480

    static func show(_ p: AwsProfile, in w: WindowController) {
        let r = AwsRotate(p, w)
        open.append(r)
        w.window?.beginSheet(r.panel)
        r.check()
    }

    private init(_ p: AwsProfile, _ w: WindowController) {
        owner = w
        profile = p
        panel = NSPanel(contentRect: NSRect(x: 0, y: 0, width: 480, height: 260), styleMask: [.titled, .docModalWindow], backing: .buffered, defer: false)
        super.init()
        content.orientation = .vertical
        content.alignment = .leading
        content.spacing = 10
        content.edgeInsets = NSEdgeInsets(top: 18, left: 22, bottom: 16, right: 22)
        content.translatesAutoresizingMaskIntoConstraints = false
        content.widthAnchor.constraint(equalToConstant: width).isActive = true
        panel.contentView = content
        render()
    }

    private func label(_ s: String, bold: Bool = false, size: CGFloat = 12, muted: Bool = false) -> NSTextField {
        let l = NSTextField(wrappingLabelWithString: s)
        l.font = bold ? .systemFont(ofSize: size, weight: .semibold) : .systemFont(ofSize: size)
        if muted { l.textColor = .secondaryLabelColor }
        l.preferredMaxLayoutWidth = width - 44
        return l
    }

    private func check() {
        phase = .checking
        error = nil
        render()
        Task {
            do {
                lines = try await Services.awsprofiles.PlanRotate(profile.name, deleteOld)
                phase = .ready
            } catch {
                self.error = error.localizedDescription
                phase = .failed
            }
            render()
        }
    }

    private func rotate() {
        phase = .rotating
        render()
        Task {
            do {
                let res = try await Services.awsprofiles.Rotate(profile.name, deleteOld, AwsStore.shared.list.versions)
                lines = res.steps
                AwsStore.shared.checks[profile.name] = nil
                AwsStore.shared.apply(res.list)
                phase = .done
                Toast.show("\(profile.name) now uses \(res.newKeyId)", in: owner.rootView)
            } catch {
                self.error = error.localizedDescription
                phase = .failed
                AwsStore.shared.load()
            }
            render()
        }
    }

    private func render() {
        content.arrangedSubviews.forEach { $0.removeFromSuperview() }
        content.addArrangedSubview(label("Rotate Access Key — \(profile.name)", bold: true, size: 15))
        content.addArrangedSubview(label("Replaces \(profile.accessKeyId) with a new key for the same IAM user", muted: true))
        switch phase {
        case .checking: content.addArrangedSubview(label("Checking the key with AWS…", muted: true))
        case .rotating: content.addArrangedSubview(label("Rotating — this takes up to half a minute while AWS takes the new key on…", muted: true))
        case .ready, .done:
            content.addArrangedSubview(label(lines.enumerated().map { "\($0 + 1). \($1)" }.joined(separator: "\n")))
            if phase == .ready {
                let box = NSButton(checkboxWithTitle: "Delete the old key afterwards (otherwise it’s left deactivated; AWS allows two keys per user)", target: nil, action: nil)
                box.state = deleteOld ? .on : .off
                let w = PopupWatcher { [weak self, weak box] in
                    self?.deleteOld = box?.state == .on
                    self?.check()
                }
                box.target = w
                box.action = #selector(PopupWatcher.changed)
                objc_setAssociatedObject(box, "watch", w, .OBJC_ASSOCIATION_RETAIN)
                content.addArrangedSubview(box)
                content.addArrangedSubview(label("If AWS doesn’t accept the new key, wtf puts the old one back and deletes the new one.", size: 11, muted: true))
            }
        case .failed: break
        }
        if let error {
            let e = label(error)
            e.textColor = .systemRed
            content.addArrangedSubview(e)
        }
        var row: [NSView] = [NSView()]
        if phase == .ready {
            let cancel = ClosureButton(title: "Cancel") { [weak self] in self?.close() }
            cancel.keyEquivalent = "\u{1b}"
            let go = ClosureButton(title: "Rotate Key") { [weak self] in self?.rotate() }
            go.keyEquivalent = "\r"
            row += [cancel, go]
        } else {
            let b = ClosureButton(title: phase == .done ? "Done" : "Close") { [weak self] in self?.close() }
            b.keyEquivalent = "\r"
            b.isEnabled = phase != .rotating
            row.append(b)
        }
        let r = NSStackView(views: row)
        r.widthAnchor.constraint(equalToConstant: width - 44).isActive = true
        content.addArrangedSubview(r)
        content.layoutSubtreeIfNeeded()
        panel.setContentSize(NSSize(width: width, height: content.fittingSize.height))
    }

    private func close() {
        owner.window?.endSheet(panel)
        Self.open.removeAll { $0 === self }
        owner.focusActive()
    }
}

/// A secure text field that reports every edit.
@MainActor
final class BoundSecureField: NSSecureTextField, NSTextFieldDelegate {
    private let set: (String) -> Void

    init(_ value: String, _ set: @escaping (String) -> Void) {
        self.set = set
        super.init(frame: .zero)
        stringValue = value
        delegate = self
    }

    required init?(coder: NSCoder) { fatalError() }

    func controlTextDidChange(_ obj: Notification) { set(stringValue) }
}

/// A combo box (text with suggestions) that reports every edit and pick.
@MainActor
final class BoundCombo: NSComboBox, NSComboBoxDelegate {
    private let set: (String) -> Void

    init(_ value: String, _ set: @escaping (String) -> Void) {
        self.set = set
        super.init(frame: .zero)
        stringValue = value
        completes = true
        delegate = self
    }

    required init?(coder: NSCoder) { fatalError() }

    func controlTextDidChange(_ obj: Notification) { set(stringValue) }
    func comboBoxSelectionDidChange(_ notification: Notification) {
        if indexOfSelectedItem >= 0, let v = itemObjectValue(at: indexOfSelectedItem) as? String { set(v) }
    }
}

/// A plain multi-line text view that reports every edit.
@MainActor
final class BoundTextView: NSTextView, NSTextViewDelegate {
    private let set: (String) -> Void

    init(_ value: String, _ set: @escaping (String) -> Void) {
        self.set = set
        super.init(frame: .zero, textContainer: nil)
        string = value
        font = .monospacedSystemFont(ofSize: 12, weight: .regular)
        isRichText = false
        isAutomaticQuoteSubstitutionEnabled = false
        isAutomaticDashSubstitutionEnabled = false
        isContinuousSpellCheckingEnabled = false
        isVerticallyResizable = true
        autoresizingMask = [.width]
        delegate = self
    }

    override init(frame: NSRect, textContainer: NSTextContainer?) {
        set = { _ in }
        super.init(frame: frame, textContainer: textContainer)
    }

    required init?(coder: NSCoder) { fatalError() }

    func textDidChange(_ notification: Notification) {
        set(string)
        needsDisplay = true
    }

    /// Shown, muted, while it's empty.
    var placeholder = ""

    override func draw(_ dirty: NSRect) {
        super.draw(dirty)
        guard string.isEmpty, !placeholder.isEmpty else { return }
        NSAttributedString(string: placeholder, attributes: [.font: font ?? .systemFont(ofSize: 12), .foregroundColor: NSColor.placeholderTextColor])
            .draw(at: NSPoint(x: textContainerInset.width + 5, y: textContainerInset.height))
    }
}

import AppKit
import WtfCore

/// The Generate tab: a new SSH key (made by wtf, with a passphrase if you
/// like) or a random password. Nothing loads until the key form shows.
/// Like GenerateView.svelte.
@MainActor
final class GenerateView: NSView, SidePaneTab {
    private unowned let owner: WindowController
    private let header = PaneHeader("Generate")
    private let section = NSSegmentedControl(labels: ["SSH Key", "Password"], trackingMode: .selectOne, target: nil, action: nil)
    private let scroll = NSScrollView()
    private var body: NSView?

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        addSubview(header)
        section.target = self
        section.action = #selector(switched)
        section.refusesFirstResponder = true
        section.controlSize = .small
        section.setAccessibilityLabel("Generate")
        addSubview(section)
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        scroll.autohidesScrollers = true
        addSubview(scroll)
        show()
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    override func layout() {
        super.layout()
        header.frame = NSRect(x: 0, y: 0, width: bounds.width, height: 30)
        section.frame = NSRect(x: 8, y: 32, width: bounds.width - 16, height: 22)
        scroll.frame = NSRect(x: 0, y: 60, width: bounds.width, height: max(0, bounds.height - 60))
        fitBody()
    }

    func debugSummary() -> String { "generate: \(owner.sidebar.generateSection)" }

    @objc private func switched() {
        owner.sidebar.generateSection = section.selectedSegment == 1 ? "password" : "key"
        owner.scheduleSave()
        show()
    }

    /// Shows the remembered section (the palette's Generate commands set it first).
    func show() {
        let pw = owner.sidebar.generateSection == "password"
        section.selectedSegment = pw ? 1 : 0
        let v: NSView = pw ? PasswordGenerator(window: owner) : KeyGenForm(window: owner)
        body = v
        let doc = FlippedView()
        doc.addSubview(v)
        scroll.documentView = doc
        (v as? Refitting)?.onResize = { [weak self] in self?.fitBody() }
        needsLayout = true
    }

    private func fitBody() {
        guard let body, let doc = scroll.documentView else { return }
        let w = scroll.contentSize.width
        body.frame.size.width = w
        body.layoutSubtreeIfNeeded()
        let h = max(body.fittingSize.height, 10)
        body.frame = NSRect(x: 0, y: 0, width: w, height: h)
        doc.frame = NSRect(x: 0, y: 0, width: w, height: h)
    }
}

extension WindowController {
    /// Opens the Generate tab on a section (never closes the pane).
    func showGenerator(_ section: String) {
        sidebar.generateSection = section
        sidebar.tab = .generate
        sidebar.open = true
        layoutSidebar()
        (sidePane as? GenerateView)?.show()
    }
}

/// A form whose height changes as it's used.
@MainActor
protocol Refitting: AnyObject {
    var onResize: (() -> Void)? { get set }
}

/// A side-pane form: a vertical stack padded 14pt, every row as wide as the pane.
@MainActor
class PaneForm: NSStackView, Refitting {
    var onResize: (() -> Void)?

    init() {
        super.init(frame: .zero)
        orientation = .vertical
        alignment = .leading
        spacing = 8
        edgeInsets = NSEdgeInsets(top: 6, left: 14, bottom: 16, right: 14)
    }

    required init?(coder: NSCoder) { fatalError() }

    func clear() { arrangedSubviews.forEach { $0.removeFromSuperview() } }

    func add(_ v: NSView, full: Bool = true) {
        addArrangedSubview(v)
        if full {
            v.translatesAutoresizingMaskIntoConstraints = false
            v.widthAnchor.constraint(equalTo: widthAnchor, constant: -28).isActive = true
        }
    }

    func label(_ s: String, bold: Bool = false, size: CGFloat = 12, color: NSColor = .labelColor) -> NSTextField {
        let l = NSTextField(wrappingLabelWithString: s)
        l.font = bold ? .systemFont(ofSize: size, weight: .semibold) : .systemFont(ofSize: size)
        l.textColor = color
        return l
    }

    func note(_ s: String) -> NSTextField { label(s, size: 11, color: .secondaryLabelColor) }

    func link(_ title: String, _ run: @escaping () -> Void) -> NSButton {
        let b = ClosureButton(title: title, run)
        b.isBordered = false
        b.contentTintColor = .controlAccentColor
        b.font = .systemFont(ofSize: 11.5, weight: .medium)
        b.refusesFirstResponder = true
        return b
    }

    func watch(_ control: NSControl, _ run: @escaping () -> Void) {
        let w = PopupWatcher(run)
        control.target = w
        control.action = #selector(PopupWatcher.changed)
        objc_setAssociatedObject(control, "watch", w, .OBJC_ASSOCIATION_RETAIN)
    }

    func resized() { onResize?() }
}

/// Makes a key pair in ~/.ssh through Go: the passphrase is used once, in
/// memory, and never stored. Then shows the public key and what to do
/// with it. Like KeyGenForm.svelte.
@MainActor
final class KeyGenForm: PaneForm {
    private unowned let owner: WindowController
    private var type = "ed25519"
    private var name = ""
    private var nameTouched = false
    private var comment = ""
    private var passphrase = ""
    private var confirm = ""
    private var showPass = false
    private var addToAgent = true
    private var error: String?
    private var busy = false
    private var result: Go.sshhosts.KeyGenResult?
    private let saved = NSTextField(wrappingLabelWithString: "")
    private let problem = NSTextField(wrappingLabelWithString: "")
    private var submit: NSButton?
    private var copyLink: NSButton?

    init(window: WindowController) {
        owner = window
        super.init()
        let store = SshStore.shared
        if !store.keysLoaded { store.loadKeys() }
        store.observe(self) { f in
            if !f.nameTouched, f.result == nil { f.suggestName() }
        }
        suggestName()
    }

    required init?(coder: NSCoder) { fatalError() }

    private func suggestName() {
        let taken = Set(SshStore.shared.keys.keys.map { WindowModel.prettyPath($0.path) })
        name = Ssh.freeKeyName(Ssh.keyTypes.first { $0.id == type }?.file ?? "id_ed25519", taken: taken)
        render()
    }

    private func render() {
        clear()
        if let result {
            renderResult(result)
            resized()
            return
        }
        let typePop = NSPopUpButton()
        typePop.addItems(withTitles: Ssh.keyTypes.map(\.label))
        typePop.selectItem(at: Ssh.keyTypes.firstIndex { $0.id == type } ?? 0)
        watch(typePop) { [weak self, weak typePop] in
            guard let self, let typePop else { return }
            type = Ssh.keyTypes[max(0, typePop.indexOfSelectedItem)].id
            if !nameTouched { suggestName() }
        }
        add(label("Type", bold: true))
        add(typePop, full: false)
        add(label("File name", bold: true))
        add(BoundField(name) { [weak self] v in
            self?.name = v
            self?.nameTouched = true
            self?.refresh()
        })
        saved.font = .systemFont(ofSize: 11)
        saved.textColor = .secondaryLabelColor
        add(saved)
        add(label("Comment", bold: true))
        let c = BoundField(comment) { [weak self] in self?.comment = $0 }
        c.placeholderString = "you@this-computer"
        add(c)
        add(note("A label stored with the key, e.g. which account or machine it's for."))

        add(label("Passphrase", bold: true))
        let p1: NSTextField = showPass ? BoundField(passphrase) { [weak self] in self?.passphrase = $0; self?.refresh() }
            : BoundSecureField(passphrase) { [weak self] in self?.passphrase = $0; self?.refresh() }
        p1.placeholderString = "none"
        p1.setAccessibilityLabel("Passphrase")
        let eye = ToolButton(symbol: showPass ? "eye.slash" : "eye", tip: showPass ? "Hide" : "Show") { [weak self] in
            guard let self else { return }
            showPass.toggle()
            render()
        }
        eye.setAccessibilityLabel(showPass ? "Hide passphrase" : "Show passphrase")
        let row = NSStackView(views: [p1, eye])
        add(row)
        let p2: NSTextField = showPass ? BoundField(confirm) { [weak self] in self?.confirm = $0; self?.refresh() }
            : BoundSecureField(confirm) { [weak self] in self?.confirm = $0; self?.refresh() }
        p2.placeholderString = "again"
        p2.setAccessibilityLabel("Passphrase again")
        add(p2)
        add(note("Encrypts the key file, so a copy of it is useless without the passphrase. Leave both empty for none."))
        let copy = link("Copy") { [weak self] in
            guard let self else { return }
            Clipboard.copySecret(passphrase, what: "passphrase", in: owner.rootView)
        }
        copyLink = copy
        add(NSStackView(views: [link("Suggest one") { [weak self] in self?.suggestPassphrase() }, copy]), full: false)
        problem.font = .systemFont(ofSize: 11.5)
        problem.textColor = .systemRed
        add(problem)

        let agent = NSButton(checkboxWithTitle: "Load it into ssh-agent", target: nil, action: nil)
        agent.state = addToAgent ? .on : .off
        watch(agent) { [weak self, weak agent] in self?.addToAgent = agent?.state == .on }
        add(agent, full: false)
        if let error {
            let e = label(error, color: .systemRed)
            add(e)
        }
        let go = ClosureButton(title: busy ? "Generating…" : "Generate Key") { [weak self] in self?.generate() }
        go.keyEquivalent = "\r"
        go.bezelColor = .controlAccentColor
        submit = go
        add(go, full: false)
        add(note("The passphrase is used once, to encrypt the file, and isn't stored. Prefer it never pass through wtf?"))
        add(link("Generate in a terminal instead") { [weak self] in
            guard let self else { return }
            KeyGenSheet.show(in: owner)
        }, full: false)
        refresh()
        resized()
    }

    /// The bits that follow typing: the file note, mismatch, the button.
    private func refresh() {
        let n = name.trimmingCharacters(in: .whitespaces)
        saved.stringValue = "Saved as ~/.ssh/\(n.isEmpty ? "…" : n) and .pub. Existing files are never replaced."
        let differ = !passphrase.isEmpty && !confirm.isEmpty && passphrase != confirm
        let short = !passphrase.isEmpty && passphrase.count < 5
        problem.stringValue = differ ? "The two passphrases differ." : short ? "Use at least 5 characters, or none." : ""
        problem.isHidden = problem.stringValue.isEmpty
        copyLink?.isHidden = passphrase.isEmpty || differ
        submit?.isEnabled = !n.isEmpty && !busy && passphrase == confirm && !short
    }

    private func suggestPassphrase() {
        var o = Passwords.Options()
        o.symbols = false
        o.avoidAmbiguous = true
        passphrase = Passwords.generate(o)
        confirm = passphrase
        showPass = !Redaction.shared.on
        if Redaction.shared.on {
            Toast.show("Passphrase filled in. Hidden while Hide Secrets is on: copy it to your password manager", in: owner.rootView)
        }
        render()
    }

    private func generate() {
        guard submit?.isEnabled == true else { return }
        busy = true
        error = nil
        render()
        let req = Go.sshhosts.KeyGenRequest(type: type, name: name.trimmingCharacters(in: .whitespaces),
                                            comment: comment.trimmingCharacters(in: .whitespaces), passphrase: passphrase, addToAgent: addToAgent)
        Task {
            do {
                let r = try await Services.sshhosts.GenerateKey(req)
                passphrase = ""
                confirm = ""
                showPass = false
                result = r
                SshStore.shared.loadKeys()
                Toast.show("Created \(WindowModel.prettyPath(r.path))", in: owner.rootView)
            } catch {
                self.error = error.localizedDescription
            }
            busy = false
            render()
        }
    }

    private func renderResult(_ r: Go.sshhosts.KeyGenResult) {
        let o = owner
        let pretty = WindowModel.prettyPath(r.path)
        add(label("✓ Created \(pretty) and .pub", bold: true, color: .systemGreen))
        let agentLine = r.inAgent ? "Loaded into ssh-agent until it restarts." : r.agentError.isEmpty ? "" : "Not loaded into ssh-agent: \(r.agentError)"
        let fp = label(r.fingerprint + (agentLine.isEmpty ? "" : "\n" + agentLine), size: 11, color: .secondaryLabelColor)
        fp.font = .monospacedSystemFont(ofSize: 11, weight: .regular)
        fp.isSelectable = true
        add(fp)
        add(label("Public key: this is what servers and GitHub need", bold: true))
        let pub = label(r.publicKey, size: 11)
        pub.font = .monospacedSystemFont(ofSize: 11, weight: .regular)
        pub.isSelectable = true
        pub.lineBreakMode = .byCharWrapping
        add(pub)
        func copyPublic() {
            Clipboard.copy(r.publicKey)
            Toast.show("Copied the public key", in: o.rootView)
        }
        let copy = ClosureButton(title: "Copy Public Key") { copyPublic() }
        copy.bezelColor = .controlAccentColor
        add(copy, full: false)
        add(ClosureButton(title: "Add to GitHub…") {
            copyPublic()
            NSWorkspace.shared.open(URL(string: "https://github.com/settings/ssh/new")!)
        }, full: false)
        add(ClosureButton(title: "Install on Host…") {
            KeyInstallSheet.show(key: SshStore.shared.keys.keys.first { $0.path == r.path }, host: nil, in: o)
        }, full: false)
        add(ClosureButton(title: "Use for a Git Identity…") { GitIdentityEditor.show(nil, keyPath: r.path, in: o) }, full: false)
        let keychain = ClosureButton(title: "Load into Agent and Keychain") {
            o.runInShell(Ssh.agentAdd("~/.ssh/" + Shell.quote(URL(fileURLWithPath: r.path).lastPathComponent), keychain: true))
        }
        keychain.toolTip = "Runs ssh-add in your shell, which asks for the passphrase and keeps it in the Keychain"
        add(keychain, full: false)
        add(link("Make another key") { [weak self] in
            guard let self else { return }
            result = nil
            nameTouched = false
            comment = ""
            suggestName()
        }, full: false)
    }
}

/// Random passwords from the system's secure source: never saved, never
/// sent. Options last while the app runs. Like PasswordGenerator.svelte.
@MainActor
final class PasswordGenerator: PaneForm, NSTextFieldDelegate {
    private static var options = Passwords.Options()

    private unowned let owner: WindowController
    private var password = ""
    private var revealed = false
    private let output = NSTextField(wrappingLabelWithString: "")
    private let bits = NSTextField(labelWithString: "")
    private let lengthLabel = NSTextField(labelWithString: "")
    private let slider = NSSlider(value: 24, minValue: Double(Passwords.minLength), maxValue: 64, target: nil, action: nil)
    private let number = NSTextField(string: "")
    private var copyButton: NSButton?
    private var newButton: NSButton?
    private var showButton: NSButton?

    init(window: WindowController) {
        owner = window
        super.init()
        build()
        regenerate()
        // Turning Hide Secrets on covers the password again.
        Redaction.shared.observe(self) { g in
            if Redaction.shared.on { g.revealed = false }
            g.update()
        }
    }

    required init?(coder: NSCoder) { fatalError() }

    private var o: Passwords.Options {
        get { Self.options }
        set { Self.options = newValue }
    }

    private func build() {
        output.font = .monospacedSystemFont(ofSize: 14, weight: .regular)
        output.lineBreakMode = .byCharWrapping
        output.wantsLayer = true
        output.layer?.cornerRadius = 6
        output.drawsBackground = true
        output.backgroundColor = NSColor.labelColor.withAlphaComponent(0.05)
        add(output)
        let copy = ClosureButton(title: "Copy") { [weak self] in
            guard let self, !password.isEmpty else { return }
            Clipboard.copySecret(password, what: "password", in: owner.rootView)
        }
        copy.bezelColor = .controlAccentColor
        let new = ClosureButton(title: "New") { [weak self] in self?.regenerate() }
        new.toolTip = "Make another"
        let show = ClosureButton(title: "Show") { [weak self] in
            self?.revealed = true
            self?.update()
        }
        copyButton = copy
        newButton = new
        showButton = show
        add(NSStackView(views: [copy, new, show]), full: false)
        bits.font = .systemFont(ofSize: 11)
        bits.textColor = .secondaryLabelColor
        add(bits)
        lengthLabel.font = .systemFont(ofSize: 12, weight: .semibold)
        slider.target = self
        slider.action = #selector(slid)
        number.delegate = self
        number.widthAnchor.constraint(equalToConstant: 50).isActive = true
        number.alignment = .right
        add(lengthLabel)
        add(NSStackView(views: [slider, number]))
        let boxes: [(String, WritableKeyPath<Passwords.Options, Bool>)] = [
            ("Lowercase a–z", \.lower), ("Uppercase A–Z", \.upper), ("Digits 0–9", \.digits),
            ("Symbols !#$%&*+-.:;=?@^_~ …", \.symbols), ("Leave out look-alikes I l 1 O 0 o |", \.avoidAmbiguous),
        ]
        for (title, key) in boxes {
            let b = NSButton(checkboxWithTitle: title, target: nil, action: nil)
            b.state = o[keyPath: key] ? .on : .off
            watch(b) { [weak self, weak b] in
                self?.o[keyPath: key] = b?.state == .on
                self?.regenerate()
            }
            add(b, full: false)
        }
        add(note("Made on this computer from the system's secure random source. Never saved or sent anywhere. No quotes, spaces or backslashes, so it pastes into shells and config files as-is."))
    }

    @objc private func slid() {
        o.length = Int(slider.doubleValue.rounded())
        regenerate()
    }

    func controlTextDidEndEditing(_ obj: Notification) {
        let n = Double(number.stringValue.trimmingCharacters(in: .whitespaces))
        o.length = n.map { $0.isFinite ? Passwords.clamp(Int($0.rounded())) : 24 } ?? 24
        regenerate()
    }

    private func regenerate() {
        password = Passwords.generate(o)
        revealed = false
        update()
    }

    private var masked: Bool { Redaction.shared.on && !revealed }

    private func update() {
        let none = Passwords.classes(o).isEmpty
        if none {
            output.stringValue = "Choose at least one kind of character"
            output.textColor = .secondaryLabelColor
            output.isSelectable = false
            output.toolTip = nil
        } else if masked {
            output.stringValue = String(repeating: "•", count: min(password.count, 32))
            output.textColor = .labelColor
            output.isSelectable = false
            output.toolTip = "Hidden while Hide Secrets is on"
        } else {
            output.stringValue = password
            output.textColor = .labelColor
            output.isSelectable = true
            output.toolTip = nil
        }
        copyButton?.isEnabled = !password.isEmpty
        newButton?.isEnabled = !none
        showButton?.isHidden = !masked || none
        bits.stringValue = none ? "" : "About \(Passwords.entropyBits(o)) bits of randomness"
        lengthLabel.stringValue = "Length: \(o.length)"
        slider.doubleValue = Double(min(64, o.length))
        number.stringValue = "\(o.length)"
        resized()
    }
}

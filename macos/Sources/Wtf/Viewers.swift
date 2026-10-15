import AppKit
import WtfCore

/// The token and certificate inspector's dialogs: JSON, JWT, decoded
/// base64, certificates and Kubernetes secrets. One at a time: opening one
/// closes the last. Decoded values live only while the dialog is open.
/// Like JsonViewer, JwtViewer, DecodedViewer, CertViewer and SecretViewer.svelte.
@MainActor
class Viewer: PagedSheet {
    private static weak var current: Viewer?
    private var observers: [NSObjectProtocol] = []

    /// Shows a viewer, closing whatever viewer was open.
    func show() {
        Self.current?.close()
        Self.current = self
        present()
    }

    override func close() {
        observers.forEach(NotificationCenter.default.removeObserver)
        observers = []
        closed()
        super.close()
    }

    /// Drop decoded values here.
    func closed() {}

    private var focusLost: (() -> Void)?

    /// Runs when wtf stops being in front, or another of its windows comes forward.
    func onLosingFocus(_ run: @escaping () -> Void) {
        focusLost = run
        let nc = NotificationCenter.default
        observers.append(nc.addObserver(forName: NSApplication.didResignActiveNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.focusLost?() }
        })
        observers.append(nc.addObserver(forName: NSWindow.didResignKeyNotification, object: owner.window, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated {
                // The window hands the keyboard to its own sheet: that's not leaving.
                guard let self, let key = NSApp.keyWindow, key !== self.panel, key !== self.owner.window else { return }
                self.focusLost?()
            }
        })
    }

    var maxBoxHeight: CGFloat { (owner.window?.contentView?.bounds.height ?? 700) }

    /// A selectable monospace box: wrapping, or not (with a horizontal scroller).
    func textBox(_ text: NSAttributedString, maxHeight: CGFloat, wrap: Bool) -> NSView {
        let tv = NSTextView()
        tv.textStorage?.setAttributedString(text)
        tv.isEditable = false
        tv.isSelectable = true
        tv.drawsBackground = false
        tv.textContainerInset = NSSize(width: 8, height: 8)
        let scroll = NSScrollView()
        scroll.documentView = tv
        scroll.hasVerticalScroller = true
        scroll.hasHorizontalScroller = !wrap
        scroll.autohidesScrollers = true
        scroll.borderType = .noBorder
        scroll.drawsBackground = true
        scroll.backgroundColor = NSColor.labelColor.withAlphaComponent(0.05)
        scroll.wantsLayer = true
        scroll.layer?.cornerRadius = 8
        tv.minSize = NSSize(width: 0, height: 0)
        tv.maxSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
        tv.isVerticallyResizable = true
        if wrap {
            tv.frame = NSRect(x: 0, y: 0, width: inner, height: 10)
            tv.textContainer?.widthTracksTextView = true
            tv.autoresizingMask = [.width]
        } else {
            tv.isHorizontallyResizable = true
            tv.textContainer?.widthTracksTextView = false
            tv.textContainer?.containerSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
            tv.frame = NSRect(x: 0, y: 0, width: inner, height: 10)
        }
        tv.layoutManager?.ensureLayout(for: tv.textContainer!)
        let used = tv.layoutManager?.usedRect(for: tv.textContainer!) ?? .zero
        if !wrap { tv.frame.size.width = max(inner, used.width + 20) }
        tv.frame.size.height = used.height + 16
        let h = min(maxHeight, used.height + 16 + (wrap ? 0 : 14))
        scroll.translatesAutoresizingMaskIntoConstraints = false
        scroll.widthAnchor.constraint(equalToConstant: inner).isActive = true
        scroll.heightAnchor.constraint(equalToConstant: max(28, h)).isActive = true
        return scroll
    }

    /// Lays out a stack at a width and gives its height (for a scroll view's document).
    static func fit(_ stack: NSStackView, width: CGFloat) -> CGFloat {
        stack.translatesAutoresizingMaskIntoConstraints = false
        let w = stack.widthAnchor.constraint(equalToConstant: width)
        w.isActive = true
        stack.layoutSubtreeIfNeeded()
        let h = stack.fittingSize.height
        stack.translatesAutoresizingMaskIntoConstraints = true
        w.isActive = false
        stack.frame = NSRect(x: 0, y: 0, width: width, height: h)
        stack.layoutSubtreeIfNeeded()
        return h
    }

    static func mono(_ s: String, size: CGFloat = 12) -> NSAttributedString {
        NSAttributedString(string: s, attributes: [.font: monoFont(size), .foregroundColor: NSColor.labelColor])
    }

    static func monoFont(_ size: CGFloat) -> NSFont {
        TerminalController.font("JetBrains Mono", size) ?? .monospacedSystemFont(ofSize: size, weight: .regular)
    }

    /// A small rounded tag (expired, server, CA…).
    func tag(_ text: String, color: NSColor = .secondaryLabelColor, strong: Bool = false) -> NSView {
        let p = Pill(bg: color.withAlphaComponent(strong ? 0.16 : 0.1), radius: 5, inset: 6)
        let l = NSTextField(labelWithString: text)
        l.font = .systemFont(ofSize: 11, weight: .semibold)
        l.textColor = color
        p.set([l])
        return p
    }

    /// Label-value rows.
    func facts(_ rows: [(String, NSView)]) -> NSView {
        let grid = NSGridView(views: rows.map { k, v in
            let l = NSTextField(labelWithString: k)
            l.font = .systemFont(ofSize: 12.5)
            l.textColor = .secondaryLabelColor
            return [l, v]
        })
        grid.columnSpacing = 12
        grid.rowSpacing = 5
        for r in 0..<grid.numberOfRows { grid.row(at: r).yPlacement = .top }
        grid.column(at: 0).width = 104
        grid.column(at: 1).xPlacement = .leading
        return grid
    }

    func value(_ s: String, mono: Bool = false, size: CGFloat = 12.5, color: NSColor = .labelColor) -> NSTextField {
        let l = NSTextField(wrappingLabelWithString: s)
        l.font = mono ? Self.monoFont(size - 0.5) : .systemFont(ofSize: size)
        l.textColor = color
        l.isSelectable = true
        l.preferredMaxLayoutWidth = inner - 160
        l.lineBreakMode = .byCharWrapping
        return l
    }

    func heading(_ s: String) -> NSTextField {
        let l = NSTextField(labelWithString: s.uppercased())
        l.font = .systemFont(ofSize: 11, weight: .bold)
        l.textColor = .secondaryLabelColor
        return l
    }

    func done(_ left: [NSView] = []) {
        let d = ClosureButton(title: "Done") { [weak self] in self?.close() }
        d.keyEquivalent = "\r"
        buttons(left + [spacer(), d])
    }

    /// The viewer an inspect action opens.
    static func open(_ action: Inspect.Action, in w: WindowController) {
        switch action {
        case .inspectJwt(let t): JwtViewer(token: t, owner: w).show()
        case .decodeBase64(let t): DecodedViewer(text: t, owner: w).show()
        case .inspectCertText(let t): CertViewer(.text(t), owner: w).show()
        case .inspectTls(let h): CertViewer(.host(h), owner: w).show()
        }
    }
}

// MARK: - JSON

@MainActor
final class JsonViewer: Viewer {
    private let json: String

    init(json: String, owner: WindowController) {
        self.json = json
        super.init(owner: owner, width: 640)
        let lines = json.components(separatedBy: "\n").count
        title("JSON", "\(lines) lines, pretty-printed from the terminal's output.")
        content.addArrangedSubview(textBox(Self.colored(json), maxHeight: maxBoxHeight * 0.6, wrap: false))
        let o = owner
        done([ClosureButton(title: "Copy") {
            Clipboard.copy(json)
            Toast.show("Copied JSON", in: o.rootView)
        }])
    }

    /// Keys blue, strings green, literals purple, numbers orange.
    static func colored(_ json: String) -> NSAttributedString {
        let out = NSMutableAttributedString(attributedString: mono(json))
        let re = try! NSRegularExpression(pattern: #"("(?:[^"\\]|\\.)*")(\s*:)?|\b(true|false|null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)"#)
        for m in re.matches(in: json, range: NSRange(location: 0, length: (json as NSString).length)) {
            if m.range(at: 1).location != NSNotFound {
                out.addAttribute(.foregroundColor, value: m.range(at: 2).location != NSNotFound ? NSColor.systemBlue : NSColor.systemGreen, range: m.range(at: 1))
            } else if m.range(at: 3).location != NSNotFound {
                out.addAttribute(.foregroundColor, value: NSColor.systemPurple, range: m.range(at: 3))
            } else if m.range(at: 4).location != NSNotFound {
                out.addAttribute(.foregroundColor, value: NSColor.systemOrange, range: m.range(at: 4))
            }
        }
        return out
    }
}

// MARK: - JWT

@MainActor
final class JwtViewer: Viewer {
    private var jwt: Inspect.Jwt?

    init(token: String, owner: WindowController) {
        jwt = Inspect.decodeJwt(token)
        super.init(owner: owner, width: 600)
        render()
    }

    override func closed() { jwt = nil }

    static let timeLabels = ["exp": "Expires", "nbf": "Not valid before", "iat": "Issued", "auth_time": "Signed in"]

    private func render() {
        title("JWT", "Decoded from the terminal's output. The signature isn't verified: wtf has no key to check it with.")
        guard let j = jwt else {
            content.addArrangedSubview(value("This isn't a JWT wtf can read.", color: .systemRed))
            done()
            return
        }
        let now = Date()
        let exp = j.times.first { $0.claim == "exp" }?.date, nbf = j.times.first { $0.claim == "nbf" }?.date
        let status: NSView = if let exp, exp <= now { tag("expired", color: .systemRed, strong: true) }
            else if let nbf, nbf > now { tag("not yet valid", color: .systemRed, strong: true) }
            else if exp != nil { tag("not expired", color: .systemGreen) }
            else { tag("no expiry") }
        var rows: [(String, NSView)] = [("Status", status)]
        let f = DateFormatter()
        f.dateStyle = .medium
        f.timeStyle = .medium
        for t in j.times {
            rows.append((Self.timeLabels[t.claim] ?? t.claim, value("\(f.string(from: t.date)) · \(Badges.relativeTime(t.date.timeIntervalSince1970 * 1000, now: now.timeIntervalSince1970 * 1000))")))
        }
        if let iss = j.claims["iss"]?.string { rows.append(("Issuer", value(iss, mono: true))) }
        if let sub = j.claims["sub"]?.string { rows.append(("Subject", value(sub, mono: true))) }
        if let aud = j.claims["aud"] { rows.append(("Audience", value(Self.audience(aud), mono: true))) }
        rows.append(("Algorithm", value((j.alg.isEmpty ? "none" : j.alg) + (j.signed ? "" : " · unsigned"), mono: true)))
        content.addArrangedSubview(facts(rows))
        content.addArrangedSubview(heading("Claims"))
        content.addArrangedSubview(textBox(Self.mono(j.claims.pretty()), maxHeight: maxBoxHeight * 0.32, wrap: true))
        content.addArrangedSubview(heading("Header"))
        content.addArrangedSubview(textBox(Self.mono(j.header.pretty()), maxHeight: maxBoxHeight * 0.2, wrap: true))
        let o = owner
        let claims = j.claims.pretty()
        done([ClosureButton(title: "Copy Claims") {
            Clipboard.copy(claims)
            Toast.show("Copied claims", in: o.rootView)
        }])
    }

    /// `aud` as JavaScript's [aud].flat().join(", ") prints it.
    static func audience(_ v: OrderedJSON) -> String {
        func str(_ v: OrderedJSON) -> String {
            switch v {
            case .string(let s): s
            case .number(let n): OrderedJSON.jsNumber(n)
            case .bool(let b): b ? "true" : "false"
            case .null: ""
            case .array(let a): a.map(str).joined(separator: ",")
            case .object: "[object Object]"
            }
        }
        if case .array(let a) = v { return a.map(str).joined(separator: ", ") }
        return str(v)
    }
}

// MARK: - Decoded base64

@MainActor
final class DecodedViewer: Viewer {
    private var decoded: Inspect.Decoded?
    private var shown = false

    init(text: String, owner: WindowController) {
        decoded = Inspect.decodeBase64(text)
        super.init(owner: owner, width: 600)
        onLosingFocus { [weak self] in
            guard let self, shown else { return }
            shown = false
            render()
        }
        render()
    }

    override func closed() { decoded = nil }

    static let kindLabels: [Inspect.Kind: String] = [.certificate: "PEM certificate", .jwt: "JWT", .json: "JSON", .text: "Text", .binary: "Binary data"]

    private func render() {
        clear()
        title("Decoded Base64", "Hidden until you reveal it, and hidden again when wtf loses focus.")
        guard let d = decoded else {
            content.addArrangedSubview(value("This isn't base64 wtf can decode.", color: .systemRed))
            done()
            return
        }
        let kind = NSTextField(labelWithString: Self.kindLabels[d.kind] ?? "")
        kind.font = .systemFont(ofSize: 12.5, weight: .semibold)
        let meta = NSTextField(labelWithString: Inspect.size(d.size) + (d.kind == .binary && d.size > 256 ? " · first 256 shown" : ""))
        meta.font = .systemFont(ofSize: 11.5)
        meta.textColor = .secondaryLabelColor
        let eye = ToolButton(symbol: shown ? "eye.slash" : "eye", tip: shown ? "Hide" : "Reveal") { [weak self] in self?.toggle() }
        let head = NSStackView(views: [kind, meta, spacer(), eye])
        head.translatesAutoresizingMaskIntoConstraints = false
        head.widthAnchor.constraint(equalToConstant: inner).isActive = true
        content.addArrangedSubview(head)
        if shown {
            content.addArrangedSubview(textBox(Self.mono(d.text), maxHeight: maxBoxHeight * 0.5, wrap: d.kind != .binary))
        } else {
            let mask = ClosureButton(title: "••••••••  Reveal") { [weak self] in self?.toggle() }
            mask.bezelStyle = .regularSquare
            mask.contentTintColor = .secondaryLabelColor
            mask.translatesAutoresizingMaskIntoConstraints = false
            mask.widthAnchor.constraint(equalToConstant: inner).isActive = true
            mask.heightAnchor.constraint(equalToConstant: 36).isActive = true
            content.addArrangedSubview(mask)
        }
        var left: [NSView] = []
        let o = owner
        if d.kind == .certificate {
            left.append(ClosureButton(title: "Inspect Certificate…") { CertViewer(.text(d.text), owner: o).show() })
        }
        if d.kind == .jwt {
            left.append(ClosureButton(title: "Inspect JWT…") { JwtViewer(token: Inspect.firstJwt(d.text) ?? "", owner: o).show() })
        }
        if d.kind != .binary {
            left.append(ClosureButton(title: "Copy") { Clipboard.copySecret(d.text, what: "decoded text", in: o.rootView) })
        }
        done(left)
    }

    private func toggle() {
        shown.toggle()
        render()
    }
}

// MARK: - Certificates

@MainActor
final class CertViewer: Viewer, NSTextFieldDelegate {
    enum Source { case host(String), file(String), text(String) }

    private let source: Source
    private var host = ""
    private var report: Go.certs.Report?
    private var error: String?
    private var busy = false
    private var now = Date()
    private var hostField: NSTextField?

    init(_ source: Source, owner: WindowController) {
        self.source = source
        if case .host(let h) = source { host = h }
        super.init(owner: owner, width: 620)
        render()
        if case .host = source, host.trimmingCharacters(in: .whitespaces).isEmpty { return }
        inspect()
    }

    private var hostMode: Bool { if case .host = source { true } else { false } }

    private func inspect() {
        let h = host.trimmingCharacters(in: .whitespaces)
        if hostMode, h.isEmpty { return }
        error = nil
        report = nil
        busy = true
        render()
        Task {
            do {
                switch source {
                case .file(let p): report = try await Services.certs.InspectFile(p)
                case .text(let t): report = try await Services.certs.InspectText(t)
                case .host: report = try await Services.certs.InspectHost(h)
                }
                now = Date()
            } catch {
                self.error = error.localizedDescription
            }
            busy = false
            render()
        }
    }

    func controlTextDidChange(_ obj: Notification) {
        host = hostField?.stringValue ?? ""
        (content.arrangedSubviews.compactMap { $0 as? NSStackView }.first?.arrangedSubviews.last as? NSButton)?.isEnabled =
            !busy && !host.trimmingCharacters(in: .whitespaces).isEmpty
    }

    func control(_ control: NSControl, textView: NSTextView, doCommandBy sel: Selector) -> Bool {
        guard sel == #selector(NSResponder.insertNewline(_:)) else { return false }
        inspect()
        return true
    }

    private func render() {
        clear()
        switch source {
        case .host: title("TLS Certificate", "Connects to the server and shows the certificates it presents. Nothing is sent but the TLS handshake.")
        case .file(let p): title("Certificate", WindowModel.prettyPath(p))
        case .text: title("Certificate", "From the selected text.")
        }
        if hostMode {
            let f = NSTextField(string: host)
            f.placeholderString = "example.com, host:8443 or https://…"
            f.font = Self.monoFont(12)
            f.delegate = self
            hostField = f
            let go = ClosureButton(title: "Inspect") { [weak self] in self?.inspect() }
            go.isEnabled = !busy && !host.trimmingCharacters(in: .whitespaces).isEmpty
            let row = NSStackView(views: [f, go])
            row.translatesAutoresizingMaskIntoConstraints = false
            row.widthAnchor.constraint(equalToConstant: inner).isActive = true
            content.addArrangedSubview(row)
            if host.isEmpty { panel.initialFirstResponder = f }
        }
        if let error {
            content.addArrangedSubview(value(error, color: .systemRed))
        } else if busy {
            content.addArrangedSubview(value(hostMode ? "Connecting to \(host.trimmingCharacters(in: .whitespaces))…" : "Reading…", color: .secondaryLabelColor))
        } else if let r = report {
            if hostMode {
                let hostName = host.trimmingCharacters(in: .whitespaces)
                    .replacingOccurrences(of: #"^\w+://"#, with: "", options: .regularExpression)
                    .replacingOccurrences(of: #"[:/].*$"#, with: "", options: .regularExpression)
                let trust: NSView
                if r.verified {
                    let row = NSStackView(views: [tag("verifies", color: .systemGreen), value("against this computer's trusted roots, for \(hostName)")])
                    trust = row
                } else {
                    trust = NSStackView(views: [tag("doesn't verify", color: .systemRed, strong: true), value(r.verifyError, mono: true)])
                }
                content.addArrangedSubview(facts([
                    ("Connected to", value(r.address, mono: true)),
                    ("Protocol", value("\(r.protocol) · \(r.cipher)" + (r.alpn.isEmpty ? "" : " · \(r.alpn)"), mono: true)),
                    ("Trust", trust),
                ]))
            }
            for n in r.notes { content.addArrangedSubview(value(n, color: .secondaryLabelColor)) }
            let list = NSStackView(views: r.certificates.enumerated().map { card($0.offset, $0.element) })
            list.orientation = .vertical
            list.alignment = .leading
            list.spacing = 10
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
            scroll.heightAnchor.constraint(equalToConstant: min(h, maxBoxHeight * 0.56)).isActive = true
            content.addArrangedSubview(scroll)
        }
        done()
    }

    private func card(_ i: Int, _ c: Go.certs.Cert) -> NSView {
        let subject = value(c.subject.isEmpty ? "(no subject)" : c.subject)
        subject.font = .systemFont(ofSize: 12.5, weight: .semibold)
        var tags: [NSView] = [subject]
        if hostMode { tags.append(tag(i == 0 ? "server" : c.selfSigned ? "root" : "intermediate")) }
        if c.isCA { tags.append(tag("CA")) }
        if c.selfSigned { tags.append(tag("self-signed")) }
        let f = DateFormatter()
        f.dateStyle = .medium
        f.timeStyle = .short
        let nb = Date(timeIntervalSince1970: Double(c.notBefore) / 1000), na = Date(timeIntervalSince1970: Double(c.notAfter) / 1000)
        let rel = { (d: Date) in Badges.relativeTime(d.timeIntervalSince1970 * 1000, now: self.now.timeIntervalSince1970 * 1000) }
        let validity: NSView = na <= now ? tag("expired \(rel(na))", color: .systemRed, strong: true)
            : nb > now ? tag("not valid until \(rel(nb))", color: .systemRed, strong: true) : tag("expires \(rel(na))")
        var rows: [(String, NSView)] = [("Valid", NSStackView(views: [value("\(f.string(from: nb)) – \(f.string(from: na))"), validity]))]
        if !c.names.isEmpty { rows.append(("Names", value(c.names.joined(separator: ", "), mono: true))) }
        rows.append(("Issuer", value(c.issuer)))
        rows.append(("Key", value("\(c.key) · signed with \(c.signatureAlgorithm)")))
        rows.append(("Serial", value(c.serial, mono: true, size: 11.5)))
        let o = owner
        let copy = link("Copy") {
            Clipboard.copy(c.sha256)
            Toast.show("Copied fingerprint", in: o.rootView)
        }
        let fp = value(c.sha256, mono: true, size: 11.5)
        fp.preferredMaxLayoutWidth = inner - 200
        rows.append(("SHA-256", NSStackView(views: [fp, copy])))
        let stack = NSStackView(views: [NSStackView(views: tags), facts(rows)])
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 6
        stack.edgeInsets = NSEdgeInsets(top: 8, left: 10, bottom: 8, right: 10)
        // A plain bordered card: unlike NSBox, it takes its contents' height.
        stack.wantsLayer = true
        stack.layer?.cornerRadius = 8
        stack.layer?.borderWidth = 1
        stack.layer?.borderColor = NSColor.separatorColor.cgColor
        stack.layer?.backgroundColor = NSColor.labelColor.withAlphaComponent(0.03).cgColor
        stack.translatesAutoresizingMaskIntoConstraints = false
        stack.widthAnchor.constraint(equalToConstant: inner - 14).isActive = true
        return stack
    }
}

// MARK: - Kubernetes secrets

@MainActor
final class SecretViewer: Viewer {
    private var entries: [Go.kube.SecretEntry] = []
    private var revealed = Set<String>()
    private var loading = true
    private var error: String?
    private let name: String
    private let target: Kube.Target

    static func show(name: String, target t: Kube.Target, in w: WindowController) {
        SecretViewer(name: name, target: t, owner: w).show()
    }

    private init(name: String, target: Kube.Target, owner: WindowController) {
        self.name = name
        self.target = target
        super.init(owner: owner, width: 560)
        onLosingFocus { [weak self] in
            guard let self, !revealed.isEmpty else { return }
            revealed.removeAll()
            render()
        }
        render()
        Task {
            do {
                entries = try await Services.kube.SecretValues(Go.kube.Request(env: target.env, context: target.context, namespace: target.namespace), name)
            } catch {
                self.error = error.localizedDescription
            }
            loading = false
            render()
        }
    }

    override func closed() {
        entries = []
        revealed = []
    }

    private func render() {
        clear()
        title("Secret \(name)", "Context \(target.context), namespace \(target.namespace). Values hide again when wtf loses focus.")
        if let error {
            content.addArrangedSubview(value(error, color: .systemRed))
        } else if loading {
            content.addArrangedSubview(value("Reading the secret…", color: .secondaryLabelColor))
        } else if entries.isEmpty {
            content.addArrangedSubview(value("This secret has no data.", color: .secondaryLabelColor))
        } else {
            let list = NSStackView()
            list.orientation = .vertical
            list.alignment = .leading
            list.spacing = 8
            let o = owner
            for e in entries {
                let key = NSTextField(labelWithString: e.key)
                key.font = Self.monoFont(12)
                key.font = NSFontManager.shared.convert(key.font!, toHaveTrait: .boldFontMask)
                key.lineBreakMode = .byTruncatingTail
                key.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
                let meta = NSTextField(labelWithString: (e.binary ? "binary · " : "") + Inspect.size(e.size, bytes: "B"))
                meta.font = .systemFont(ofSize: 11)
                meta.textColor = .secondaryLabelColor
                let shown = revealed.contains(e.key)
                let eye = ToolButton(symbol: shown ? "eye.slash" : "eye", tip: shown ? "Hide" : "Reveal") { [weak self] in
                    guard let self else { return }
                    if shown { revealed.remove(e.key) } else { revealed.insert(e.key) }
                    render()
                }
                eye.setAccessibilityLabel((shown ? "Hide " : "Reveal ") + e.key)
                let copy = ToolButton(symbol: "doc.on.doc", tip: e.binary ? "Copy (base64)" : "Copy") {
                    Clipboard.copySecret(e.value, what: e.binary ? "\(e.key) (base64)" : e.key, in: o.rootView)
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
        if !entries.isEmpty {
            let all = revealed.count == entries.count
            left.append(ClosureButton(title: all ? "Hide All" : "Reveal All") { [weak self] in
                guard let self else { return }
                revealed = all ? [] : Set(entries.map(\.key))
                render()
            })
        }
        done(left)
    }
}

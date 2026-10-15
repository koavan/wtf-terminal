import AppKit
import WtfCore

/// A sheet whose content is rebuilt as it changes: a vertical stack with
/// a title, subtitle, body and a button row. For dialogs with pages.
@MainActor
class PagedSheet: NSObject {
    unowned let owner: WindowController
    let panel: NSPanel
    let content = NSStackView()
    let width: CGFloat
    private static var open: [PagedSheet] = []

    init(owner: WindowController, width: CGFloat) {
        self.owner = owner
        self.width = width
        let p = EscPanel(contentRect: NSRect(x: 0, y: 0, width: width, height: 300), styleMask: [.titled, .docModalWindow], backing: .buffered, defer: false)
        panel = p
        super.init()
        p.onCancel = { [weak self] in self?.close() }
        content.orientation = .vertical
        content.alignment = .leading
        content.spacing = 10
        content.edgeInsets = NSEdgeInsets(top: 20, left: 24, bottom: 18, right: 24)
        content.translatesAutoresizingMaskIntoConstraints = false
        content.widthAnchor.constraint(equalToConstant: width).isActive = true
        panel.contentView = content
    }

    var inner: CGFloat { width - 48 }

    func present() {
        Self.open.append(self)
        owner.window?.beginSheet(panel)
    }

    func close() {
        owner.window?.endSheet(panel)
        Self.open.removeAll { $0 === self }
        owner.focusActive()
    }

    func clear() { content.arrangedSubviews.forEach { $0.removeFromSuperview() } }

    func label(_ s: String, size: CGFloat = 12.5, weight: NSFont.Weight = .regular, color: NSColor = .labelColor, mono: Bool = false) -> NSTextField {
        let l = NSTextField(wrappingLabelWithString: s)
        l.font = mono ? .monospacedSystemFont(ofSize: size - 1, weight: weight) : .systemFont(ofSize: size, weight: weight)
        l.textColor = color
        l.preferredMaxLayoutWidth = inner
        l.isSelectable = mono
        return l
    }

    func title(_ t: String, _ sub: String?) {
        content.addArrangedSubview(label(t, size: 15, weight: .semibold))
        if let sub, !sub.isEmpty { content.addArrangedSubview(label(sub, size: 12, color: .secondaryLabelColor)) }
    }

    func link(_ title: String, tip: String? = nil, _ run: @escaping () -> Void) -> NSButton {
        let b = ClosureButton(title: title, run)
        b.isBordered = false
        b.contentTintColor = .controlAccentColor
        b.toolTip = tip
        return b
    }

    /// A monospace box, scrolling past a height.
    func codeBox(_ text: String, maxHeight: CGFloat) -> NSView {
        let tv = NSTextView()
        tv.string = text
        tv.isEditable = false
        tv.font = .monospacedSystemFont(ofSize: 11.5, weight: .regular)
        tv.drawsBackground = false
        tv.textContainerInset = NSSize(width: 6, height: 6)
        let scroll = NSScrollView()
        scroll.documentView = tv
        scroll.hasVerticalScroller = true
        scroll.borderType = .bezelBorder
        tv.frame = NSRect(x: 0, y: 0, width: inner - 4, height: 10)
        tv.isVerticallyResizable = true
        tv.textContainer?.widthTracksTextView = true
        tv.layoutManager?.ensureLayout(for: tv.textContainer!)
        let h = min(maxHeight, (tv.layoutManager?.usedRect(for: tv.textContainer!).height ?? 16) + 16)
        scroll.translatesAutoresizingMaskIntoConstraints = false
        scroll.widthAnchor.constraint(equalToConstant: inner).isActive = true
        scroll.heightAnchor.constraint(equalToConstant: h).isActive = true
        return scroll
    }

    func buttons(_ views: [NSView]) {
        let row = NSStackView(views: views)
        row.translatesAutoresizingMaskIntoConstraints = false
        row.widthAnchor.constraint(equalToConstant: inner).isActive = true
        content.addArrangedSubview(row)
        content.layoutSubtreeIfNeeded()
        panel.setContentSize(NSSize(width: width, height: content.fittingSize.height))
    }

    func spacer() -> NSView {
        let v = NSView()
        v.setContentHuggingPriority(.init(1), for: .horizontal)
        return v
    }
}

/// A sheet panel that closes on Esc.
@MainActor
final class EscPanel: NSPanel {
    var onCancel: (() -> Void)?
    override func cancelOperation(_ sender: Any?) { onCancel?() }
}

// MARK: - Command-line tools

/// Command-Line Tools: which programs the side pane runs are there, and
/// installing the missing ones — the exact command or script is shown
/// first, then run in a new tab (never by wtf itself). Like ToolsDialog.svelte.
@MainActor
final class ToolsDialog: PagedSheet {
    private var list: [Go.tools.Status]?
    private var error: String?
    private var checking = false
    private var welcome: [String]
    private var focus: String?
    private var installing: Go.tools.Status?
    private var method = 0
    private var busy = false

    static func show(focus: String? = nil, welcome: [String] = [], in w: WindowController) {
        guard w.window?.attachedSheet == nil else { return }
        let d = ToolsDialog(owner: w, focus: focus, welcome: welcome)
        d.present()
        d.check()
    }

    private init(owner: WindowController, focus: String?, welcome: [String]) {
        self.focus = focus
        self.welcome = welcome
        super.init(owner: owner, width: 600)
        render()
    }

    private var shellPATH: String { owner.activePane?.env?["PATH"] ?? "" }

    private func check() {
        checking = true
        render()
        Task {
            do {
                list = try await Services.tools.Check(shellPATH)
                error = nil
                if let f = focus, let t = list?.first(where: { $0.id == f }), !t.methods.isEmpty {
                    installing = t
                    method = 0
                }
                focus = nil
            } catch {
                self.error = error.localizedDescription
            }
            checking = false
            render()
        }
    }

    private func render() {
        clear()
        if let t = installing {
            renderInstall(t)
            return
        }
        title("Command-Line Tools", "Programs side-pane features run. wtf looks on the focused shell's PATH first, then where installers put them.")
        if !welcome.isEmpty {
            let names = welcome.joined(separator: ", ")
            let w = label("Welcome to wtf. \(names) \(welcome.count == 1 ? "isn't" : "aren't") installed yet. The terminal works without them; the features below that use them say so when you open them. Install any now, or later from the palette (Command-Line Tools) or About wtf.")
            w.drawsBackground = true
            w.backgroundColor = NSColor.controlAccentColor.withAlphaComponent(0.12)
            content.addArrangedSubview(w)
        }
        if let list {
            let stack = NSStackView()
            stack.orientation = .vertical
            stack.alignment = .leading
            stack.spacing = 10
            for (i, t) in list.enumerated() {
                if i > 0 {
                    let line = NSBox()
                    line.boxType = .separator
                    stack.addArrangedSubview(line)
                    line.widthAnchor.constraint(equalToConstant: inner - 16).isActive = true
                }
                stack.addArrangedSubview(row(t))
            }
            let scroll = NSScrollView()
            let doc = FlippedView()
            doc.addSubview(stack)
            stack.frame.size.width = inner - 16
            stack.layoutSubtreeIfNeeded()
            let h = stack.fittingSize.height
            stack.frame = NSRect(x: 0, y: 0, width: inner - 16, height: h)
            doc.frame = stack.frame
            scroll.documentView = doc
            scroll.hasVerticalScroller = true
            scroll.drawsBackground = false
            scroll.translatesAutoresizingMaskIntoConstraints = false
            scroll.widthAnchor.constraint(equalToConstant: inner).isActive = true
            scroll.heightAnchor.constraint(equalToConstant: min(h, min(420, (owner.window?.frame.height ?? 700) * 0.6))).isActive = true
            content.addArrangedSubview(scroll)
        } else {
            content.addArrangedSubview(label(error ?? "Checking…", color: error == nil ? .secondaryLabelColor : .systemRed))
        }
        if list != nil, let error { content.addArrangedSubview(label(error, color: .systemRed)) }
        let manual = ClosureButton(title: "Manual") { [weak self] in
            self?.close()
            ManualWindow.show("tools")
        }
        let again = ClosureButton(title: checking ? "Checking…" : "Check Again") { [weak self] in self?.check() }
        again.isEnabled = !checking
        let done = ClosureButton(title: "Done") { [weak self] in self?.close() }
        done.keyEquivalent = "\u{1b}"
        done.bezelColor = .controlAccentColor
        buttons([manual, spacer(), again, done])
    }

    private func row(_ t: Go.tools.Status) -> NSView {
        let ok = t.found && t.missing.isEmpty
        let dot = Dot(color: ok ? .systemGreen : .tertiaryLabelColor, size: 8)
        let name = NSTextField(labelWithString: t.name)
        name.font = .systemFont(ofSize: 13, weight: .bold)
        let state = NSTextField(labelWithString: t.found ? t.version : "not installed")
        state.font = t.found ? .monospacedSystemFont(ofSize: 11.5, weight: .regular) : NSFontManager.shared.convert(.systemFont(ofSize: 11.5), toHaveTrait: .italicFontMask)
        state.textColor = .secondaryLabelColor
        state.isSelectable = t.found
        state.lineBreakMode = .byTruncatingTail
        state.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        var lines: [NSView] = [NSStackView(views: [name, state])]
        func muted(_ s: String, italic: Bool = false, mono: Bool = false, tip: String? = nil) {
            let l = NSTextField(wrappingLabelWithString: s)
            l.font = mono ? .monospacedSystemFont(ofSize: 11, weight: .regular)
                : italic ? NSFontManager.shared.convert(.systemFont(ofSize: 11.5), toHaveTrait: .italicFontMask) : .systemFont(ofSize: 11.5)
            l.textColor = .secondaryLabelColor
            l.toolTip = tip
            l.preferredMaxLayoutWidth = inner - 150
            lines.append(l)
        }
        if t.found {
            muted(WindowModel.prettyPath(t.path), mono: true, tip: t.path)
            if !t.missing.isEmpty { muted("Not found: " + t.missing.joined(separator: ", "), italic: true) }
        }
        muted("Used by: " + t.usedBy.joined(separator: " · "))
        if !t.found, t.methods.isEmpty {
            muted(t.builtIn.isEmpty ? "Install it with your system's package manager." : "Part of \(t.builtIn); it may have been removed.")
        }
        let text = NSStackView(views: lines)
        text.orientation = .vertical
        text.alignment = .leading
        text.spacing = 2
        var right: [NSView] = []
        if !t.methods.isEmpty {
            let b = ClosureButton(title: "Install…") { [weak self] in
                self?.installing = t
                self?.method = 0
                self?.error = nil
                self?.render()
            }
            b.controlSize = .small
            b.bezelColor = .controlAccentColor
            right.append(b)
        }
        if !t.homepage.isEmpty, let u = URL(string: t.homepage) {
            right.append(link("Website", tip: t.homepage) { NSWorkspace.shared.open(u) })
        }
        let col = NSStackView(views: right)
        col.orientation = .vertical
        col.alignment = .trailing
        let row = NSStackView(views: [dot, text, spacer(), col])
        row.alignment = .top
        row.translatesAutoresizingMaskIntoConstraints = false
        row.widthAnchor.constraint(equalToConstant: inner - 16).isActive = true
        return row
    }

    private func renderInstall(_ t: Go.tools.Status) {
        title("Install \(t.name)", t.about)
        let methods = t.methods
        if methods.count > 1 {
            for (i, m) in methods.enumerated() {
                let r = NSButton(radioButtonWithTitle: m.label + (i == 0 ? "  (recommended)" : ""), target: nil, action: nil)
                r.state = i == method ? .on : .off
                let w = PopupWatcher { [weak self] in
                    self?.method = i
                    self?.render()
                }
                r.target = w
                r.action = #selector(PopupWatcher.changed)
                objc_setAssociatedObject(r, "watch", w, .OBJC_ASSOCIATION_RETAIN)
                content.addArrangedSubview(r)
            }
        }
        let m = methods[min(method, methods.count - 1)]
        if !m.script.isEmpty {
            content.addArrangedSubview(label("wtf writes this script to its cache folder and runs it with sh in a new tab:", size: 12, color: .secondaryLabelColor))
            content.addArrangedSubview(codeBox(m.script, maxHeight: 240))
        } else {
            content.addArrangedSubview(label("Runs in a new tab:", size: 12, color: .secondaryLabelColor))
            content.addArrangedSubview(codeBox(m.command, maxHeight: 120))
        }
        var facts: [(String, String)] = [("Installs to", m.dest), ("Checked", m.verify)]
        if m.sudo { facts.append(("Password", "Uses sudo; it asks for your password in the tab.")) }
        for (k, v) in facts {
            let key = NSTextField(labelWithString: k)
            key.font = .systemFont(ofSize: 12, weight: .semibold)
            key.textColor = .secondaryLabelColor
            key.widthAnchor.constraint(equalToConstant: 84).isActive = true
            let val = label(v, size: 12)
            val.preferredMaxLayoutWidth = inner - 96
            let r = NSStackView(views: [key, val])
            r.alignment = .firstBaseline
            content.addArrangedSubview(r)
        }
        if !m.note.isEmpty {
            let n = label(m.note, size: 12, color: .secondaryLabelColor)
            n.isSelectable = true
            content.addArrangedSubview(n)
        }
        if let error { content.addArrangedSubview(label(error, color: .systemRed)) }
        let backB = ClosureButton(title: "Back") { [weak self] in
            self?.installing = nil
            self?.error = nil
            self?.render()
        }
        let cancel = ClosureButton(title: "Cancel") { [weak self] in self?.close() }
        cancel.keyEquivalent = "\u{1b}"
        let run = ClosureButton(title: "Run in New Tab") { [weak self] in self?.install(t, m) }
        run.keyEquivalent = "\r"
        run.isEnabled = !busy
        buttons([backB, spacer(), cancel, run])
    }

    private func install(_ t: Go.tools.Status, _ m: Go.tools.Method) {
        guard !busy else { return }
        busy = true
        let o = owner
        Task {
            do {
                let cmd = try await Services.tools.Install(t.id, m.id, shellPATH)
                close()
                o.model.open(cwd: o.activePane?.cwd, command: cmd)
                Toast.show("Installing \(t.name) — check again from Command-Line Tools when it's done", in: o.rootView, seconds: 4)
            } catch {
                self.error = error.localizedDescription
                busy = false
                render()
            }
        }
    }
}

extension WindowController {
    /// Once ever (per state folder), after the window opens: says which tools aren't installed.
    func toolsFirstRun() {
        Task {
            guard let names = try? await Services.tools.FirstRun(""), !names.isEmpty, window?.attachedSheet == nil else { return }
            ToolsDialog.show(welcome: names, in: self)
        }
    }

    /// An "Install…" link for an error that says a tool is missing, or nil.
    func installLink(for error: String?) -> NSButton? {
        guard let id = Tools.missingTool(error) else { return nil }
        let b = ClosureButton(title: "Install…") { [weak self] in
            guard let self else { return }
            ToolsDialog.show(focus: id, in: self)
        }
        b.isBordered = false
        b.contentTintColor = .controlAccentColor
        b.font = .systemFont(ofSize: 12, weight: .semibold)
        b.toolTip = "Show how to install it"
        b.refusesFirstResponder = true
        return b
    }
}

// MARK: - About

/// About wtf: version, what it is, where its files are, and the way to the
/// manual and the tools. Like AboutDialog.svelte.
@MainActor
final class AboutDialog: PagedSheet {
    private var info: Go.about.Info?

    static let chips: [(String, String)] = [
        ("Tabs, groups & splits", "tabs"), ("Command blocks", "shell-integration"), ("Log-friendly output", "logs"),
        ("Pinned output", "pins"), ("Hide Secrets", "secrets"), ("Recording", "recording"), ("SSH", "ssh-hosts"),
        ("AWS profiles", "aws"), ("Kubernetes", "kubernetes"), ("Snippets & runbooks", "snippets"),
        ("Workspaces", "workspaces"), ("Port forwards", "network"), ("Broadcast input", "broadcast"),
    ]

    static func show(in w: WindowController) {
        guard w.window?.attachedSheet == nil else { return }
        let d = AboutDialog(owner: w, width: 520)
        d.render()
        d.present()
        Task {
            d.info = try? await Services.about.Info()
            d.render()
        }
    }

    /// Diagnostics for a bug report: no file paths but the shell's name.
    static func diagnostics(_ i: Go.about.Info) -> String {
        [
            "wtf \(i.version) (\(i.commit)" + (i.buildDate.isEmpty ? "" : ", built \(i.buildDate)") + ")",
            "\(i.os) · \(i.arch)",
            "Go \(i.goVersion) · native macOS (AppKit, SwiftTerm)",
            "Shell: \((i.shell as NSString).lastPathComponent), shell integration \(i.shellIntegration ? "on" : "off")",
        ].joined(separator: "\n")
    }

    private func render() {
        clear()
        let icon = NSImageView(image: NSApp.applicationIconImage)
        icon.widthAnchor.constraint(equalToConstant: 72).isActive = true
        icon.heightAnchor.constraint(equalToConstant: 72).isActive = true
        var hero: [NSView] = [label("wtf", size: 24, weight: .bold), label("A terminal for all the What-The-Fuck moments", color: .secondaryLabelColor)]
        if let i = info {
            let v = label("Version \(i.version) · \(i.commit)" + (i.buildDate.isEmpty ? "" : " · built \(i.buildDate)"), size: 11.5, color: .secondaryLabelColor)
            v.toolTip = "The git commit this build was made from"
            hero.append(v)
        }
        let heroText = NSStackView(views: hero)
        heroText.orientation = .vertical
        heroText.alignment = .leading
        heroText.spacing = 2
        content.addArrangedSubview(NSStackView(views: [icon, heroText]))
        content.addArrangedSubview(label("A browser-like terminal for DevOps work: tabs you can group, split and move between windows, an address bar that says which cluster and AWS account each shell points at, and a side pane for your files, SSH hosts, AWS profiles, Kubernetes clusters, snippets, workspaces, port forwards and processes."))

        // Feature chips, wrapped into rows.
        var rows: [NSStackView] = [NSStackView()]
        var used: CGFloat = 0
        for (title, topic) in Self.chips {
            let b = ClosureButton(title: title) { [weak self] in
                self?.close()
                ManualWindow.show(topic)
            }
            b.bezelStyle = .accessoryBarAction
            b.controlSize = .small
            b.toolTip = "Open the manual at \(title)"
            let w = b.fittingSize.width + 6
            if used + w > inner, !(rows.last!.arrangedSubviews.isEmpty) {
                rows.append(NSStackView())
                used = 0
            }
            rows.last!.addArrangedSubview(b)
            rows.last!.spacing = 4
            used += w + 4
        }
        let chips = NSStackView(views: rows)
        chips.orientation = .vertical
        chips.alignment = .leading
        chips.spacing = 4
        content.addArrangedSubview(chips)

        if let i = info {
            let grid = NSGridView()
            grid.columnSpacing = 12
            grid.rowSpacing = 4
            func key(_ s: String) -> NSTextField {
                let l = NSTextField(labelWithString: s)
                l.font = .systemFont(ofSize: 12, weight: .semibold)
                l.textColor = .secondaryLabelColor
                return l
            }
            func reveal(_ path: String, tip: String) -> NSView {
                link(WindowModel.prettyPath(path), tip: tip) { [weak self] in
                    Task {
                        do { try await Services.files.Reveal(path) } catch {
                            if let v = self?.owner.rootView { Toast.show("\(WindowModel.prettyPath(path)) doesn’t exist yet", in: v) }
                        }
                    }
                }
            }
            let shell = NSTextField(labelWithString: "\((i.shell as NSString).lastPathComponent) (\(WindowModel.prettyPath(i.shell))) · shell integration \(i.shellIntegration ? "on" : "off")")
            grid.addRow(with: [key("Shell"), shell])
            grid.addRow(with: [key("System"), NSTextField(labelWithString: "\(i.os) · \(i.arch)")])
            grid.addRow(with: [key("Built with"), NSTextField(labelWithString: "Go \(i.goVersion.replacingOccurrences(of: "go", with: "", options: .anchored)) · AppKit and SwiftTerm")])
            grid.addRow(with: [key("Settings"), reveal(i.configFile, tip: "Show in Finder")])
            grid.addRow(with: [key("Snippets"), reveal(i.snippetsFile, tip: "Show in Finder")])
            grid.addRow(with: [key("Session"), reveal(i.stateDir, tip: "Saved windows and tabs, port forwards")])
            grid.addRow(with: [key("Backups"), reveal(i.cacheDir, tip: "Copies of ssh, AWS and kubeconfig files kept before wtf changed them, and deleted SSH keys")])
            for r in 0..<grid.numberOfRows { grid.cell(atColumnIndex: 0, rowIndex: r).yPlacement = .center }
            grid.column(at: 0).width = 80
            grid.column(at: 1).xPlacement = .leading
            content.addArrangedSubview(grid)
        }
        content.addArrangedSubview(label("Native macOS build: AppKit and SwiftTerm over wtf’s Go backend. Icons: SF Symbols. Font: JetBrains Mono (OFL).", size: 11.5, color: .secondaryLabelColor))

        let o = owner
        let diag = ClosureButton(title: "Copy Diagnostics") { [weak self] in
            guard let i = self?.info else { return }
            Clipboard.copy(Self.diagnostics(i))
            Toast.show("Copied version and system details", in: o.rootView)
        }
        diag.isEnabled = info != nil
        diag.toolTip = "Version and system details for a bug report (no file paths)"
        let tools = ClosureButton(title: "Tools") { [weak self] in
            self?.close()
            ToolsDialog.show(in: o)
        }
        tools.toolTip = "kubectl, the AWS CLI, git and OpenSSH: found or not, and installing them"
        var row: [NSView] = [diag, tools, spacer()]
        if let repo = info?.repository, let u = URL(string: repo) {
            row.append(ClosureButton(title: "GitHub") { NSWorkspace.shared.open(u) })
        }
        row.append(ClosureButton(title: "Shortcuts") { [weak self] in
            self?.close()
            ManualWindow.show("shortcuts")
        })
        let manual = ClosureButton(title: "Manual") { [weak self] in
            self?.close()
            ManualWindow.show("getting-started")
        }
        manual.keyEquivalent = "\r"
        row.append(manual)
        buttons(row)
    }
}

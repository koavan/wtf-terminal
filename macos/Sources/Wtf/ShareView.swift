import AppKit
import WtfCore

/// The Share & Record tab: Hide Secrets, recording the focused terminal,
/// saving its output as text or Markdown, and what was saved from this
/// window. Like ShareView.svelte.
@MainActor
final class ShareView: NSView, SidePaneTab {
    private unowned let owner: WindowController
    private let header = PaneHeader("Share & Record")
    private let scroll = NSScrollView()
    private let form = PaneForm()
    private var timer: Timer?

    init(window: WindowController) {
        owner = window
        super.init(frame: .zero)
        addSubview(header)
        let doc = FlippedView()
        doc.addSubview(form)
        scroll.documentView = doc
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        addSubview(scroll)
        owner.records.observe(self) { $0.rebuild() }
        Redaction.shared.observe(self) { $0.rebuild() }
        Settings.shared.observe(self) { $0.rebuild() }
        rebuild()
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        if window == nil {
            timer?.invalidate()
            timer = nil
        }
    }

    override func layout() {
        super.layout()
        header.frame = NSRect(x: 0, y: 0, width: bounds.width, height: 30)
        scroll.frame = NSRect(x: 0, y: 30, width: bounds.width, height: max(0, bounds.height - 30))
        fit()
    }

    private func fit() {
        let w = scroll.contentSize.width
        form.frame.size.width = w
        form.layoutSubtreeIfNeeded()
        let h = form.fittingSize.height
        form.frame = NSRect(x: 0, y: 0, width: w, height: h)
        scroll.documentView?.frame = NSRect(x: 0, y: 0, width: w, height: h)
    }

    func paneChanged() { rebuild() }

    func debugSummary() -> String { "share: recording \(owner.records.recording.count), saved \(owner.records.saved.count)" }

    private func heading(_ symbol: String, _ text: String) -> NSView {
        let i = NSImageView(image: NSImage(systemSymbolName: symbol, accessibilityDescription: nil)!)
        i.symbolConfiguration = .init(pointSize: 11, weight: .semibold)
        i.contentTintColor = .secondaryLabelColor
        let l = NSTextField(labelWithString: text.uppercased())
        l.font = .systemFont(ofSize: 10.5, weight: .bold)
        l.textColor = .secondaryLabelColor
        let s = NSStackView(views: [i, l])
        s.spacing = 5
        return s
    }

    private func spacer(_ h: CGFloat) -> NSView {
        let v = NSView()
        v.heightAnchor.constraint(equalToConstant: h).isActive = true
        return v
    }

    private func rebuild() {
        form.clear()
        let o = owner
        let records = o.records

        // Hide Secrets
        form.add(heading("checkmark.shield", "Hide Secrets"), full: false)
        let on = Redaction.shared.on
        let toggle = NSSwitch()
        toggle.state = on ? .on : .off
        form.watch(toggle) {
            Redaction.shared.on.toggle()
            Toast.show(Redaction.shared.on ? "Secrets hidden — on screen and when copied" : "Secrets shown", in: o.rootView)
        }
        let state = form.label(on ? "Secrets are hidden" : "Secrets are shown", bold: true, color: on ? .systemGreen : .labelColor)
        let sub = form.note(on ? "Covered on screen, [REDACTED] when copied or saved" : "Turn on before you share your screen")
        let texts = NSStackView(views: [state, sub])
        texts.orientation = .vertical
        texts.alignment = .leading
        texts.spacing = 1
        form.add(NSStackView(views: [toggle, texts]))
        let startup = NSButton(checkboxWithTitle: "Turn on whenever wtf starts", target: nil, action: nil)
        startup.state = Settings.shared.config?.redactSecrets == true ? .on : .off
        form.watch(startup) { [weak startup] in
            let v = startup?.state == .on
            Task {
                do { try await Services.config.SetRedactSecrets(v) } catch {
                    Toast.show("Couldn't save the setting: \(error.localizedDescription)", in: o.rootView)
                }
            }
        }
        form.add(startup, full: false)
        form.add(form.note("Access keys, tokens, JWTs, passwords and private keys in every terminal. Best effort — it can miss a secret it doesn't recognize. ⌥⌘R"))
        form.add(form.link("What it covers") { ManualWindow.show("secrets") }, full: false)
        form.add(spacer(8))

        // Record this terminal
        form.add(heading("play.circle", "Record this terminal"), full: false)
        var anyRecording = !records.recording.isEmpty
        if let p = o.activePane {
            let target = form.label(o.label(pane: p.id), bold: true)
            target.toolTip = WindowModel.prettyPath(p.cwd)
            form.add(target)
            if let rec = records.recording[p.id] {
                let line = form.label("● Recording · \(Export.elapsed(Int(Date().timeIntervalSince(rec.since))))", bold: true, color: .systemRed)
                form.add(line)
                let path = form.note(WindowModel.prettyPath(rec.path))
                path.toolTip = rec.path
                form.add(path)
                let stop = ClosureButton(title: "Stop Recording") { o.stopRecording(pane: p.id) }
                stop.bezelColor = .systemRed
                form.add(stop, full: false)
            } else {
                let start = ClosureButton(title: "● Start Recording…") { o.toggleRecording(pane: p) }
                start.bezelColor = .controlAccentColor
                form.add(start, full: false)
            }
            form.add(form.note("An asciinema (.cast) file of what the terminal shows from now on, with its timing — not what you type. Kept as is: Hide Secrets doesn't apply to it. ⌥⌘S"))
            form.add(spacer(4))
            form.add(heading("square.and.arrow.down", "Save what's there"), full: false)
            form.add(ClosureButton(title: "Save Output as Text…") { o.saveOutputAsText(pane: p) }, full: false)
            let md = ClosureButton(title: "Save Commands as Markdown…") { o.saveTranscript(pane: p) }
            md.isEnabled = p.integrated
            if !p.integrated { md.toolTip = "Needs shell integration (bash or zsh)" }
            form.add(md, full: false)
            form.add(form.note("Markdown: each command with its exit status, start time, duration and output — for a postmortem or a ticket."))
        } else {
            form.add(form.note("Open a terminal to record it."))
        }

        // Also recording
        let others = records.recording.filter { $0.key != o.activePane?.id }.sorted { $0.key < $1.key }
        if !others.isEmpty {
            form.add(spacer(8))
            form.add(heading("record.circle", "Also recording"), full: false)
            for (pane, rec) in others {
                let go = ClosureButton(title: "\(o.label(pane: pane)) — \(Export.elapsed(Int(Date().timeIntervalSince(rec.since)))) · \((rec.path as NSString).lastPathComponent)") {
                    o.show(pane: pane)
                }
                go.isBordered = false
                go.toolTip = "Show this terminal"
                go.contentTintColor = .systemRed
                let stop = ToolButton(symbol: "stop.fill", tip: "Stop recording") { o.stopRecording(pane: pane) }
                form.add(NSStackView(views: [go, stop]), full: false)
            }
        }

        // Saved in this window
        if !records.saved.isEmpty {
            form.add(spacer(8))
            form.add(heading("folder", "Saved in this window"), full: false)
            let f = DateFormatter()
            f.dateFormat = DateFormatter.dateFormat(fromTemplate: "jmm", options: 0, locale: .current)
            for s in records.saved {
                let open = ClosureButton(title: "\((s.path as NSString).lastPathComponent) — \(s.kind.rawValue) · \(f.string(from: s.at))") {
                    Task {
                        do { try await Services.files.Reveal(s.path) } catch {
                            Toast.show("\(WindowModel.prettyPath(s.path)) isn't there any more", in: o.rootView)
                        }
                    }
                }
                open.isBordered = false
                open.image = NSImage(systemSymbolName: s.kind == .recording ? "play.rectangle" : "doc.text", accessibilityDescription: nil)
                open.imagePosition = .imageLeading
                open.toolTip = "\(s.path)\nShow in Finder"
                let copy = ToolButton(symbol: "doc.on.doc", tip: "Copy path") {
                    Clipboard.copy(s.path)
                    Toast.show("Copied path", in: o.rootView)
                }
                form.add(NSStackView(views: [open, copy]), full: false)
            }
        }

        // The elapsed clocks tick only while something records.
        anyRecording = !records.recording.isEmpty
        if anyRecording, timer == nil {
            timer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in
                MainActor.assumeIsolated { self?.rebuild() }
            }
        } else if !anyRecording {
            timer?.invalidate()
            timer = nil
        }
        needsLayout = true
    }
}

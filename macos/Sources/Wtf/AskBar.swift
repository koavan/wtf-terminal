import AppKit
import WtfCore

/// Plain English to command (#25), ⌘I: a bar over the window where you
/// describe a command ("delete evicted pods in all namespaces"). The quick
/// AI gets it with the focused shell's facts (Go's suggest service) and
/// answers with one command and a sentence about it. Return types the
/// command at the prompt without running it; Explain Flags asks what each
/// part does. One per window.
@MainActor
final class AskBar: NSView, NSTextFieldDelegate {
    private static var shown: [ObjectIdentifier: AskBar] = [:]

    static func toggle(in w: WindowController) {
        if let b = shown[ObjectIdentifier(w)] {
            b.dismiss()
            return
        }
        guard ExplainSheet.available else {
            Toast.show("Set up an AI provider in Settings first (⌘,)", in: w.rootView)
            return
        }
        guard let term = w.activeTerminal, w.activePane?.isShell == true else {
            Toast.show("Focus a terminal pane first", in: w.rootView)
            return
        }
        Palette.close(in: w)
        TabOverview.close(in: w)
        let b = AskBar(window: w, terminal: term)
        shown[ObjectIdentifier(w)] = b
        b.frame = w.rootView.bounds
        b.autoresizingMask = [.width, .height]
        w.rootView.addSubview(b)
        b.window?.makeFirstResponder(b.field)
    }

    static func close(in w: WindowController) { shown[ObjectIdentifier(w)]?.dismiss() }

    private unowned let owner: WindowController
    private weak var terminal: TerminalController?
    private let panel = NSVisualEffectView()
    private let field = NSTextField()
    private let stack = NSStackView()
    private let status = NSTextField(wrappingLabelWithString: "")
    private let command = NSTextField(wrappingLabelWithString: "")
    private let note = NSTextField(wrappingLabelWithString: "")
    private let flags = NSTextField(wrappingLabelWithString: "")
    private var insertButton: ClosureButton!
    private var flagsButton: ClosureButton!
    private var sentButton: ClosureButton!
    private let buttonRow = NSStackView()

    private var request: AIRequest?
    /// What was asked, and the request that asked it (for What's Sent and
    /// the flags follow-up).
    private var asked = ""
    private var aiRequest: Go.ai.Request?
    private var reply = ""
    private var suggestion: Go.suggest.Suggestion?
    private var flagsAsked = false

    private init(window: WindowController, terminal: TerminalController) {
        owner = window
        self.terminal = terminal
        super.init(frame: .zero)
        wantsLayer = true
        layer?.backgroundColor = NSColor.black.withAlphaComponent(0.18).cgColor

        panel.material = .popover
        panel.state = .active
        panel.wantsLayer = true
        panel.layer?.cornerRadius = 14
        panel.layer?.masksToBounds = true
        panel.translatesAutoresizingMaskIntoConstraints = false
        addSubview(panel)

        field.placeholderString = "Describe a command, e.g. delete evicted pods in all namespaces"
        field.font = .systemFont(ofSize: 15)
        field.isBordered = false
        field.drawsBackground = false
        field.focusRingType = .none
        field.delegate = self
        field.cell?.isScrollable = true
        let icon = NSImageView(image: NSImage(systemSymbolName: "sparkles", accessibilityDescription: nil)!)
        icon.contentTintColor = .secondaryLabelColor
        let top = NSStackView(views: [icon, field])
        top.spacing = 8
        top.edgeInsets = NSEdgeInsets(top: 13, left: 16, bottom: 13, right: 16)
        let line = NSBox()
        line.boxType = .separator

        status.font = .systemFont(ofSize: 11.5)
        status.textColor = .secondaryLabelColor
        command.font = .monospacedSystemFont(ofSize: 13, weight: .medium)
        command.isSelectable = true
        note.font = .systemFont(ofSize: 12.5)
        flags.font = .monospacedSystemFont(ofSize: 11.5, weight: .regular)
        flags.textColor = .secondaryLabelColor
        flags.isSelectable = true

        sentButton = ClosureButton(title: "What’s Sent") { [weak self] in self?.showSent() }
        flagsButton = ClosureButton(title: "Explain Flags") { [weak self] in self?.explainFlags() }
        insertButton = ClosureButton(title: "Insert at Prompt") { [weak self] in self?.insert() }
        for b in [sentButton!, flagsButton!, insertButton!] { b.controlSize = .small }
        buttonRow.setViews([sentButton, flagsButton, NSView(), insertButton], in: .leading)

        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 8
        stack.edgeInsets = NSEdgeInsets(top: 10, left: 16, bottom: 12, right: 16)
        for v in [command, note, flags, status, buttonRow] { stack.addArrangedSubview(v) }

        let outer = NSStackView(views: [top, line, stack])
        outer.orientation = .vertical
        outer.alignment = .leading
        outer.spacing = 0
        outer.translatesAutoresizingMaskIntoConstraints = false
        panel.addSubview(outer)
        NSLayoutConstraint.activate([
            panel.topAnchor.constraint(equalTo: topAnchor, constant: 72),
            panel.centerXAnchor.constraint(equalTo: centerXAnchor),
            panel.widthAnchor.constraint(equalToConstant: 620),
            outer.topAnchor.constraint(equalTo: panel.topAnchor),
            outer.leadingAnchor.constraint(equalTo: panel.leadingAnchor),
            outer.trailingAnchor.constraint(equalTo: panel.trailingAnchor),
            outer.bottomAnchor.constraint(equalTo: panel.bottomAnchor),
            top.widthAnchor.constraint(equalTo: outer.widthAnchor),
            line.widthAnchor.constraint(equalTo: outer.widthAnchor),
            stack.widthAnchor.constraint(equalTo: outer.widthAnchor),
            buttonRow.widthAnchor.constraint(equalTo: stack.widthAnchor, constant: -32),
        ])
        for l in [status, command, note, flags] { l.preferredMaxLayoutWidth = 588 }
        setAccessibilityLabel("Command from plain English")
        show(.idle)
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    // Clicks on the scrim close it.
    override func mouseDown(with event: NSEvent) {
        if !panel.frame.contains(convert(event.locationInWindow, from: nil)) { dismiss() }
    }

    private enum State { case idle, asking, answered, failed(String) }

    private func show(_ s: State) {
        let hasCommand = !(suggestion?.command.isEmpty ?? true)
        switch s {
        case .idle:
            status.stringValue = "Return asks the quick AI with this shell’s folder and context. Nothing runs: the command is typed at the prompt for you."
        case .asking:
            status.stringValue = "Asking…  Esc stops."
        case .answered:
            status.stringValue = hasCommand ? "Return types it at the prompt (not run) · edit the request and Return to ask again" : "No command for that"
        case .failed(let e):
            status.stringValue = e
        }
        command.isHidden = !hasCommand
        note.isHidden = note.stringValue.isEmpty
        flags.isHidden = flags.stringValue.isEmpty
        insertButton.isEnabled = hasCommand
        flagsButton.isEnabled = hasCommand && !flagsAsked && request == nil
        sentButton.isEnabled = aiRequest != nil
        buttonRow.isHidden = { if case .idle = s { return true } else { return false } }()
    }

    // MARK: Asking

    private func ask() {
        let text = field.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, request == nil else { return }
        asked = text
        suggestion = nil
        reply = ""
        flagsAsked = false
        command.stringValue = ""
        note.stringValue = ""
        flags.stringValue = ""
        show(.asking)
        let pane = owner.model.panes[terminal?.paneId ?? -1]
        let a = Go.suggest.Ask(text: text, env: pane?.env ?? [:], cwd: pane?.cwd ?? "")
        Task {
            do {
                let r = try await Services.suggest.Request(a)
                guard superview != nil else { return }
                aiRequest = r
                send(r, flags: false)
            } catch {
                show(.failed(error.localizedDescription))
            }
        }
    }

    private func send(_ r: Go.ai.Request, flags isFlags: Bool) {
        let req = AIRequest(r)
        request = req
        var streamed = ""
        req.onChunk = { [weak self] chunk in
            guard let self else { return }
            streamed += chunk
            if isFlags { flags.stringValue = Redaction.shared.text(streamed) } else { note.stringValue = Redaction.shared.text(streamed) }
            show(.asking)
        }
        req.onDone = { [weak self] d in self?.done(d, flags: isFlags) }
        show(.asking)
        req.start(in: owner.window)
    }

    private func done(_ d: AIDone, flags isFlags: Bool) {
        request = nil
        if d.cancelled {
            if isFlags { flagsAsked = false; flags.stringValue = "" }
            show(suggestion == nil ? .idle : .answered)
            return
        }
        if !d.error.isEmpty {
            if isFlags { flagsAsked = false }
            show(.failed("The AI couldn’t answer: \(d.error)"))
            return
        }
        if isFlags {
            flags.stringValue = Redaction.shared.text(d.text.trimmingCharacters(in: .whitespacesAndNewlines))
            show(.answered)
            return
        }
        reply = d.text
        Task {
            let s = (try? await Services.suggest.Parse(d.text)) ?? Go.suggest.Suggestion(note: d.text)
            suggestion = s
            command.stringValue = s.command.isEmpty ? "" : "$ " + s.command
            note.stringValue = Redaction.shared.text(s.note)
            show(.answered)
            status.stringValue += " · " + d.summary
        }
    }

    private func explainFlags() {
        guard let r = aiRequest, suggestion?.command.isEmpty == false, !flagsAsked, request == nil else { return }
        flagsAsked = true
        Task {
            guard let f = try? await Services.suggest.FlagsRequest(r, reply) else { return }
            send(f, flags: true)
        }
    }

    private func showSent() {
        guard let r = aiRequest else { return }
        AIRequest.showPreview(r, in: owner.window)
    }

    private func insert() {
        guard let cmd = suggestion?.command, !cmd.isEmpty, let term = terminal else { return }
        switch term.typeAtPrompt(cmd) {
        case .busy:
            Toast.show("The shell is busy — finish the running program first", in: owner.rootView)
        case .copied:
            dismiss()
            Toast.show("The command has several lines and this shell would run them as typed: copied instead", in: owner.rootView)
        case .typed:
            dismiss()
        }
    }

    // MARK: Keys

    func control(_ control: NSControl, textView: NSTextView, doCommandBy sel: Selector) -> Bool {
        switch sel {
        case #selector(NSResponder.insertNewline(_:)):
            let text = field.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
            if text == asked, suggestion?.command.isEmpty == false, request == nil {
                insert()
            } else {
                ask()
            }
            return true
        case #selector(NSResponder.cancelOperation(_:)):
            if let r = request { r.cancel() } else { dismiss() }
            return true
        default:
            return false
        }
    }

    func dismiss() {
        request?.cancel()
        Self.shown[ObjectIdentifier(owner)] = nil
        removeFromSuperview()
        owner.focusActive()
    }
}

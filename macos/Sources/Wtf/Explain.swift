import AppKit
import WtfCore

/// Explain this failure (#24): a failed command block's facts go to the AI
/// (Go builds them: the command, exit code, output tail, folder, context and
/// tool versions), and the reply streams into a sheet. Its fix command is
/// typed at the prompt for the user to read and run; nothing runs it.
@MainActor
final class ExplainSheet {
    /// Open sheets, held until they close.
    private static var open: [ExplainSheet] = []

    private weak var owner: WindowController?
    private weak var terminal: TerminalController?
    private let paneId: Int
    private let sheet: FormSheet
    private let text: NSTextView
    private let status: NSTextField
    private let fixLabel: NSTextField
    private var request: AIRequest?
    private var reply = ""
    private var fix = ""
    private var aiRequest: Go.ai.Request?

    /// Whether Explain is offered: an AI provider is set up.
    static var available: Bool { !(Settings.shared.config?.aiProviderNames.isEmpty ?? true) }

    /// Explains block id of a pane's terminal.
    static func show(block id: Int, in term: TerminalController, owner w: WindowController) {
        guard let b = term.block(id), let code = b.exitCode else { return }
        let pane = w.model.panes[term.paneId]
        let failure = Go.explain.Failure(command: b.text, exitCode: code, durationMs: b.durationMs ?? 0,
                                         output: term.blockOutput(id) ?? "", env: pane?.env ?? [:], cwd: pane?.cwd ?? "")
        let s = ExplainSheet(command: b.text, exitCode: code, paneId: term.paneId, terminal: term, owner: w)
        open.append(s)
        s.start(failure)
    }

    private init(command: String, exitCode: Int, paneId: Int, terminal: TerminalController, owner: WindowController) {
        self.owner = owner
        self.terminal = terminal
        self.paneId = paneId
        let shown = Redaction.shared.text(command.count > 60 ? String(command.prefix(59)) + "…" : command)
        let provider = Settings.shared.config?.aiDefault ?? ""
        sheet = FormSheet(title: "Explain: \(shown)",
                          subtitle: "✗ exit \(exitCode)" + (provider.isEmpty ? "" : " · asking \(provider)"), width: 620)

        let scroll = NSTextView.scrollableTextView()
        scroll.borderType = .bezelBorder
        text = scroll.documentView as! NSTextView
        text.isEditable = false
        text.textContainerInset = NSSize(width: 8, height: 8)
        text.font = .systemFont(ofSize: 13)
        scroll.translatesAutoresizingMaskIntoConstraints = false
        scroll.widthAnchor.constraint(equalToConstant: 572).isActive = true
        scroll.heightAnchor.constraint(equalToConstant: 260).isActive = true
        sheet.add(scroll)

        fixLabel = sheet.note("", mono: true)
        fixLabel.isHidden = true
        sheet.add(fixLabel)
        status = sheet.note("Gathering the command’s facts…")
        sheet.add(status)

        sheet.buttons([("Type Fix at Prompt", true, { [weak self] in self?.typeFix() })], cancel: "Close", left: [
            ("What’s Sent", { [weak self] in self?.showSent() }),
            ("Copy", { [weak self] in self?.copy() }),
        ])
        sheet.button("Type Fix at Prompt")?.isEnabled = false
        sheet.button("Copy")?.isEnabled = false
        sheet.onClose = { [weak self] in self?.closed() }
        sheet.present(in: owner.window)
    }

    private func start(_ f: Go.explain.Failure) {
        Task {
            do {
                let r = try await Services.explain.Request(f)
                guard Self.open.contains(where: { $0 === self }) else { return }
                aiRequest = r
                send(r)
            } catch {
                status.stringValue = "Couldn’t gather the facts: \(error.localizedDescription)"
            }
        }
    }

    private func send(_ r: Go.ai.Request) {
        let req = AIRequest(r)
        request = req
        status.stringValue = "Waiting for the reply…  Close stops it."
        req.onChunk = { [weak self] chunk in
            guard let self else { return }
            reply += chunk
            show(reply)
        }
        req.onDone = { [weak self] d in self?.done(d) }
        req.start(in: sheet.panel)
    }

    private func show(_ s: String) {
        text.string = Redaction.shared.on ? Redaction.shared.text(s) : s
        text.scrollToEndOfDocument(nil)
    }

    private func done(_ d: AIDone) {
        request = nil
        if d.cancelled {
            // Cancelled in the first-send preview: nothing was asked.
            if reply.isEmpty { sheet.close() } else { status.stringValue = "Stopped." }
            return
        }
        if !d.error.isEmpty {
            status.stringValue = "The AI couldn’t answer: \(d.error)"
            status.textColor = .systemRed
            return
        }
        reply = d.text
        sheet.button("Copy")?.isEnabled = !reply.isEmpty
        Task {
            let parsed = (try? await Services.explain.Parse(d.text)) ?? Go.explain.Reply(cause: d.text)
            show(parsed.cause)
            fix = parsed.fix
            if !fix.isEmpty {
                fixLabel.stringValue = "$ " + fix
                fixLabel.isHidden = false
                sheet.button("Type Fix at Prompt")?.isEnabled = true
            }
            status.stringValue = d.summary + (fix.isEmpty ? " · no fix command suggested" : " · the fix is typed, not run")
        }
    }

    /// Puts the fix at the pane's prompt, without Return.
    private func typeFix() {
        guard !fix.isEmpty, let w = owner, let term = terminal else { return }
        guard let p = w.model.panes[paneId], p.isShell, !p.running else {
            Toast.show("The shell is busy — finish the running program first", in: sheet.panel.contentView)
            return
        }
        // Without bracketed paste, a newline would run the lines.
        if fix.contains("\n"), !term.view.getTerminal().bracketedPasteMode {
            Clipboard.copy(fix)
            Toast.show("The fix has several lines and this shell would run them as typed: copied instead", in: w.rootView)
            sheet.close()
            return
        }
        sheet.close()
        term.type(Shell.clearLine)
        term.insert(fix)
    }

    private func copy() {
        let all = fix.isEmpty ? text.string : text.string + "\n\n" + fix
        Clipboard.copy(all)
        Toast.show("Copied the explanation", in: sheet.panel.contentView)
    }

    private func showSent() {
        guard let r = aiRequest else { return }
        AIRequest.showPreview(r, in: sheet.panel)
    }

    private func closed() {
        request?.cancel()
        Self.open.removeAll { $0 === self }
    }
}

extension WindowController {
    /// Explains the focused pane's last command, when it failed.
    func explainLastFailure() {
        guard ExplainSheet.available else {
            Toast.show("Set up an AI provider in Settings first (⌘,)", in: rootView)
            return
        }
        guard let term = activeTerminal, let id = term.lastFinishedBlock, let b = term.block(id) else {
            Toast.show("No finished command to explain yet", in: rootView)
            return
        }
        guard b.exitCode != 0 else {
            Toast.show("The last command succeeded: nothing to explain", in: rootView)
            return
        }
        ExplainSheet.show(block: id, in: term, owner: self)
    }
}

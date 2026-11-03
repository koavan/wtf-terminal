import AppKit
import WtfCore

/// Change reviewer (#26): what a terraform plan, kubectl diff or helm diff
/// (a command block or a saved plan file) will do, parsed by Go's review
/// service and listed as facts. Summarize with Claude asks the Claude pane
/// (/wtf:review) for a plain-English summary. Nothing is applied.
@MainActor
final class ReviewSheet {
    private static var open: [ReviewSheet] = []

    private weak var owner: WindowController?
    private let sheet: FormSheet
    private let facts: NSTextView
    private let status: NSTextField
    private var review: Go.review.Review?
    /// The shell it's about, and what /wtf:review gets.
    private var paneId: Int?
    private var block: Int?
    private var file: String?

    /// Reviews block id of a pane's terminal.
    static func show(block id: Int, in term: TerminalController, owner w: WindowController) {
        guard let b = term.block(id) else { return }
        let shown = Redaction.shared.text(b.text.count > 60 ? String(b.text.prefix(59)) + "…" : b.text)
        let s = ReviewSheet(title: "Review: \(shown)", owner: w)
        s.paneId = term.paneId
        s.block = id
        let output = term.blockOutput(id) ?? ""
        s.load { try await Services.review.Parse(b.text, output) }
    }

    /// Reviews a saved plan file.
    static func show(file path: String, owner w: WindowController) {
        let s = ReviewSheet(title: "Review: \((path as NSString).lastPathComponent)", owner: w)
        s.paneId = w.model.active?.focusedPaneId
        s.file = path
        let env = w.activePane?.env ?? [:]
        s.status.stringValue = "Reading the plan…"
        s.load { try await Services.review.ParseFile(path, env) }
    }

    private init(title: String, owner w: WindowController) {
        owner = w
        sheet = FormSheet(title: title, subtitle: "Reading…", width: 680)
        facts = Self.textBox(in: sheet, height: 240, mono: true)
        status = sheet.note("")
        sheet.add(status)
        sheet.buttons([("Summarize with Claude", true, { [weak self] in self?.summarize() })], cancel: "Close", left: [
            ("Copy", { [weak self] in self?.copy() }),
        ])
        sheet.button("Summarize with Claude")?.isEnabled = false
        sheet.button("Copy")?.isEnabled = false
        sheet.onClose = { [weak self] in self?.closed() }
        sheet.present(in: w.window)
        Self.open.append(self)
    }

    private static func textBox(in sheet: FormSheet, height: CGFloat, mono: Bool) -> NSTextView {
        let scroll = NSTextView.scrollableTextView()
        scroll.borderType = .bezelBorder
        let tv = scroll.documentView as! NSTextView
        tv.isEditable = false
        tv.textContainerInset = NSSize(width: 8, height: 8)
        tv.font = mono ? .monospacedSystemFont(ofSize: 11.5, weight: .regular) : .systemFont(ofSize: 13)
        scroll.translatesAutoresizingMaskIntoConstraints = false
        scroll.widthAnchor.constraint(equalToConstant: 632).isActive = true
        scroll.heightAnchor.constraint(equalToConstant: height).isActive = true
        sheet.add(scroll)
        return tv
    }

    private func load(_ parse: @escaping () async throws -> Go.review.Review) {
        Task {
            do {
                let r = try await parse()
                guard Self.open.contains(where: { $0 === self }) else { return }
                show(r)
            } catch {
                sheet.setSubtitle("")
                status.stringValue = "Couldn’t read it: \(error.localizedDescription)"
                status.textColor = .systemRed
            }
        }
    }

    private func show(_ r: Go.review.Review) {
        review = r
        status.stringValue = ""
        guard !r.tool.isEmpty else {
            sheet.setSubtitle("")
            facts.string = "This output isn’t terraform plan, kubectl diff or helm diff output, so there’s nothing to review."
            return
        }
        var sub = "\(r.tool) · \(r.countText)"
        if !r.plan.isEmpty { sub += "\n\(r.tool) says: \(r.plan)" }
        sheet.setSubtitle(sub)
        facts.string = Redaction.shared.text(r.noChanges && r.lines.isEmpty ? "No changes." : r.lines.joined(separator: "\n"))
        sheet.button("Copy")?.isEnabled = true
        sheet.button("Summarize with Claude")?.isEnabled = !r.lines.isEmpty
    }

    /// Closes the sheet and asks the shell's Claude pane.
    private func summarize() {
        guard let w = owner else { return }
        if let id = paneId, w.model.panes[id] != nil { w.model.focusPane(id) }
        let block = block, file = file
        sheet.close()
        ClaudePane.review(block: block, file: file, in: w)
    }

    private func copy() {
        guard let r = review else { return }
        Clipboard.copy(Redaction.shared.text("\(r.tool): \(r.countText)\n" + r.lines.joined(separator: "\n")))
        Toast.show("Copied the review", in: sheet.panel.contentView)
    }

    private func closed() {
        Self.open.removeAll { $0 === self }
    }
}

extension WindowController {
    /// Reviews the focused pane's last finished command.
    func reviewLastCommand() {
        guard let term = activeTerminal, let id = term.lastFinishedBlock else {
            Toast.show("No finished command to review yet", in: rootView)
            return
        }
        ReviewSheet.show(block: id, in: term, owner: self)
    }
}

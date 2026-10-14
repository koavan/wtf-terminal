import AppKit
import WtfCore

/// Open “<workspace>”: asks for its placeholders, shows what will open,
/// then opens it here or in a new window. Like WorkspaceOpenDialog.svelte.
@MainActor
final class WorkspaceOpen: NSObject, NSTextFieldDelegate {
    private static var current: WorkspaceOpen?

    private let workspace: Workspace
    private unowned let owner: WindowController
    private let sheet: NSPanel
    private var inputs: [(field: Field, control: NSControl)] = []
    private let planView = NSTextField(wrappingLabelWithString: "")
    private var buttons: [NSButton] = []

    static func show(_ w: Workspace, in owner: WindowController) {
        let o = WorkspaceOpen(workspace: w, owner: owner)
        current = o
        owner.window?.beginSheet(o.sheet)
    }

    init(workspace: Workspace, owner: WindowController) {
        self.workspace = workspace
        self.owner = owner
        sheet = NSPanel(contentRect: NSRect(x: 0, y: 0, width: 560, height: 300), styleMask: [.titled, .docModalWindow], backing: .buffered, defer: false)
        super.init()
        build()
    }

    private func build() {
        let title = NSTextField(labelWithString: "Open “\(workspace.name)”")
        title.font = .systemFont(ofSize: 15, weight: .semibold)
        let subtitle = NSTextField(wrappingLabelWithString: [workspace.description, workspace.source.isEmpty ? "" : "From \(WindowModel.prettyPath(workspace.source))"]
            .filter { !$0.isEmpty }.joined(separator: " · "))
        subtitle.textColor = .secondaryLabelColor
        var views: [NSView] = [title, subtitle]

        let facts = ShellFacts(pane: owner.activePane)
        for f in workspace.fields {
            let label = NSTextField(labelWithString: f.description.isEmpty ? f.name : "\(f.name) — \(f.description)")
            label.font = .systemFont(ofSize: 12, weight: .medium)
            let value = Placeholders.resolve(f.default, facts)
            let control: NSControl
            if !f.choices.isEmpty {
                let pop = NSPopUpButton()
                pop.addItems(withTitles: f.choices)
                if f.choices.contains(value) { pop.selectItem(withTitle: value) }
                pop.target = self
                pop.action = #selector(changed)
                control = pop
            } else {
                let t = NSTextField(string: value)
                t.placeholderString = f.default
                t.delegate = self
                control = t
            }
            control.widthAnchor.constraint(equalToConstant: 300).isActive = true
            let row = NSStackView(views: [label, control])
            row.distribution = .fill
            views.append(row)
            inputs.append((f, control))
        }
        planView.font = .systemFont(ofSize: 12)
        planView.preferredMaxLayoutWidth = 512
        views.append(planView)

        let cancel = NSButton(title: "Cancel", target: self, action: #selector(cancel))
        cancel.keyEquivalent = "\u{1b}"
        let newWindow = NSButton(title: "Open in New Window", target: self, action: #selector(openNewWindow))
        var row: [NSView] = [NSView(), cancel, newWindow]
        if workspace.window == "new" {
            newWindow.keyEquivalent = "\r"
        } else {
            let here = NSButton(title: "Open Here", target: self, action: #selector(openHere))
            here.keyEquivalent = "\r"
            row.append(here)
            buttons.append(here)
        }
        buttons.append(newWindow)
        let buttonRow = NSStackView(views: row)
        views.append(buttonRow)

        let stack = NSStackView(views: views)
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 12
        stack.edgeInsets = NSEdgeInsets(top: 20, left: 24, bottom: 18, right: 24)
        buttonRow.widthAnchor.constraint(equalTo: stack.widthAnchor, constant: -48).isActive = true
        stack.widthAnchor.constraint(equalToConstant: 560).isActive = true
        sheet.contentView = stack
        changed()
        if let first = inputs.first?.control as? NSTextField { sheet.initialFirstResponder = first }
    }

    private var values: [String: String] {
        var v: [String: String] = [:]
        for (f, c) in inputs {
            v[f.name] = (c as? NSPopUpButton)?.titleOfSelectedItem ?? (c as? NSTextField)?.stringValue ?? ""
        }
        return v
    }

    /// The plan as it stands, redrawn on every edit.
    @objc private func changed() {
        do {
            let plan = try workspace.plan(values)
            let text = NSMutableAttributedString()
            for (i, t) in plan.tabs.enumerated() {
                var head = t.title.isEmpty ? "Tab \(i + 1)" : t.title
                if !t.group.isEmpty { head += "   [\(t.group)]" }
                if t.panes.count > 1 { head += "   [\(t.panes.count) panes, \(t.layout == "column" ? "stacked" : "side by side")]" }
                text.append(NSAttributedString(string: head + "\n", attributes: [.font: NSFont.systemFont(ofSize: 12, weight: .semibold)]))
                for p in t.panes {
                    text.append(NSAttributedString(string: "    " + (p.cwd.isEmpty ? "current folder" : WindowModel.prettyPath(p.cwd)),
                                                   attributes: [.font: NSFont.systemFont(ofSize: 12), .foregroundColor: NSColor.secondaryLabelColor]))
                    if !p.command.isEmpty {
                        text.append(NSAttributedString(string: "  " + p.command, attributes: [.font: NSFont.monospacedSystemFont(ofSize: 11.5, weight: .regular)]))
                    }
                    text.append(NSAttributedString(string: "\n"))
                }
            }
            let n = plan.tabs.count
            text.append(NSAttributedString(string: (n == 1 ? "Opens 1 tab" : "Opens \(n) tabs") + "; each startup command is typed into its new shell and run.",
                                           attributes: [.font: NSFont.systemFont(ofSize: 11.5), .foregroundColor: NSColor.secondaryLabelColor]))
            planView.attributedStringValue = text
            buttons.forEach { $0.isEnabled = true }
        } catch {
            planView.attributedStringValue = NSAttributedString(string: error.localizedDescription, attributes: [.foregroundColor: NSColor.systemRed])
            buttons.forEach { $0.isEnabled = false }
        }
    }

    func controlTextDidChange(_ note: Notification) { changed() }

    private func finish() {
        owner.window?.endSheet(sheet)
        Self.current = nil
    }

    @objc private func cancel() {
        finish()
        owner.focusActive()
    }

    @objc private func openHere() { open(newWindow: false) }
    @objc private func openNewWindow() { open(newWindow: true) }

    private func open(newWindow: Bool) {
        let plan: Plan
        do { plan = try workspace.plan(values) } catch { return }
        finish()
        if newWindow {
            WindowManager.shared.open(plan: plan)
        } else {
            owner.model.open(plan: plan)
        }
    }
}

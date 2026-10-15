import AppKit
import WtfCore

/// A dialog as a sheet on the window: title, subtitle, labelled fields with
/// help text, an error line and a row of buttons. Esc cancels; Return in a
/// text field runs the primary button. The dialogs of the side pane are
/// built from it.
@MainActor
final class FormSheet: NSObject {
    private static var open: [FormSheet] = []

    let panel: NSPanel
    private let stack = NSStackView()
    private let errorLabel = NSTextField(wrappingLabelWithString: "")
    private let subtitleLabel = NSTextField(wrappingLabelWithString: "")
    private var buttonRow = NSStackView()
    private var buttons: [String: NSButton] = [:]
    private weak var parent: NSWindow?
    private let width: CGFloat
    /// Runs when the sheet closes.
    var onClose: (() -> Void)?

    init(title: String, subtitle: String? = nil, width: CGFloat = 480) {
        self.width = width
        panel = SubmitPanel(contentRect: NSRect(x: 0, y: 0, width: width, height: 200), styleMask: [.titled, .docModalWindow],
                        backing: .buffered, defer: false)
        super.init()
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 10
        stack.edgeInsets = NSEdgeInsets(top: 20, left: 24, bottom: 18, right: 24)
        let t = NSTextField(labelWithString: title)
        t.font = .systemFont(ofSize: 15, weight: .semibold)
        stack.addArrangedSubview(t)
        subtitleLabel.stringValue = subtitle ?? ""
        subtitleLabel.textColor = .secondaryLabelColor
        subtitleLabel.preferredMaxLayoutWidth = width - 48
        subtitleLabel.isHidden = (subtitle ?? "").isEmpty
        stack.addArrangedSubview(subtitleLabel)
        errorLabel.textColor = .systemRed
        errorLabel.preferredMaxLayoutWidth = width - 48
        errorLabel.isHidden = true
    }

    private var inner: CGFloat { width - 48 }

    func setSubtitle(_ s: String) {
        subtitleLabel.stringValue = s
        subtitleLabel.isHidden = s.isEmpty
    }

    /// A labelled control, with optional help under it.
    @discardableResult
    func field(_ label: String, _ control: NSView, help: String? = nil) -> NSView {
        let l = NSTextField(labelWithString: label)
        l.font = .systemFont(ofSize: 12, weight: .medium)
        var views: [NSView] = [l, control]
        control.translatesAutoresizingMaskIntoConstraints = false
        if let help {
            let h = NSTextField(wrappingLabelWithString: help)
            h.font = .systemFont(ofSize: 11)
            h.textColor = .secondaryLabelColor
            h.preferredMaxLayoutWidth = inner
            views.append(h)
        }
        let s = NSStackView(views: views)
        s.orientation = .vertical
        s.alignment = .leading
        s.spacing = 4
        add(s)
        control.widthAnchor.constraint(equalToConstant: inner).isActive = control is NSButton ? false : true
        return s
    }

    /// Any view, full width.
    func add(_ v: NSView) {
        stack.addArrangedSubview(v)
        v.translatesAutoresizingMaskIntoConstraints = false
        if !(v is NSButton) { v.widthAnchor.constraint(lessThanOrEqualToConstant: inner).isActive = true }
    }

    func text(_ value: String = "", placeholder: String = "") -> NSTextField {
        let f = NSTextField(string: value)
        f.placeholderString = placeholder
        return f
    }

    func note(_ text: String, mono: Bool = false) -> NSTextField {
        let n = NSTextField(wrappingLabelWithString: text)
        n.font = mono ? .monospacedSystemFont(ofSize: 11.5, weight: .regular) : .systemFont(ofSize: 11.5)
        n.textColor = mono ? .labelColor : .secondaryLabelColor
        n.preferredMaxLayoutWidth = inner
        n.isSelectable = mono
        return n
    }

    func popup(_ items: [String], selected: Int = 0) -> NSPopUpButton {
        let p = NSPopUpButton()
        p.addItems(withTitles: items)
        if selected >= 0, selected < items.count { p.selectItem(at: selected) }
        return p
    }

    var error: String? {
        get { errorLabel.isHidden ? nil : errorLabel.stringValue }
        set {
            errorLabel.stringValue = newValue ?? ""
            errorLabel.isHidden = newValue == nil
        }
    }

    /// Buttons, left to right after a spacer; the primary is the default (Return).
    func buttons(_ list: [(title: String, primary: Bool, run: () -> Void)], cancel: String = "Cancel", left: [(String, () -> Void)] = []) {
        var views: [NSView] = []
        for (title, run) in left {
            let b = ClosureButton(title: title, run)
            buttons[title] = b
            views.append(b)
        }
        views.append(NSView())
        let c = ClosureButton(title: cancel) { [weak self] in self?.close() }
        c.keyEquivalent = "\u{1b}"
        views.append(c)
        for b in list {
            let btn = ClosureButton(title: b.title, b.run)
            if b.primary { btn.keyEquivalent = "\r" }
            buttons[b.title] = btn
            views.append(btn)
        }
        buttonRow = NSStackView(views: views)
        buttonRow.translatesAutoresizingMaskIntoConstraints = false
    }

    func button(_ title: String) -> NSButton? { buttons[title] }

    func present(in window: NSWindow?, focus: NSView? = nil) {
        guard let window else { return }
        stack.addArrangedSubview(errorLabel)
        stack.addArrangedSubview(buttonRow)
        buttonRow.widthAnchor.constraint(equalToConstant: inner).isActive = true
        stack.widthAnchor.constraint(equalToConstant: width).isActive = true
        panel.contentView = stack
        panel.initialFirstResponder = focus
        parent = window
        Self.open.append(self)
        window.beginSheet(panel)
    }

    func close() {
        onClose?()
        onClose = nil
        parent?.endSheet(panel)
        Self.open.removeAll { $0 === self }
        WindowManager.shared.keyWindow?.focusActive()
    }
}

/// A button that runs a closure.
@MainActor
final class ClosureButton: NSButton {
    private let run: () -> Void

    init(title: String, _ run: @escaping () -> Void) {
        self.run = run
        super.init(frame: .zero)
        self.title = title
        bezelStyle = .rounded
        target = self
        action = #selector(fire)
    }

    required init?(coder: NSCoder) { fatalError() }

    @objc private func fire() { run() }
}

@MainActor
enum Confirm {
    /// "Delete …?" with a destructive button; runs `action` when confirmed.
    static func show(in window: NSWindow?, title: String, message: String, items: [String] = [], button: String,
                     limit: Int = 6, _ action: @escaping () -> Void) {
        guard let window else { return }
        let alert = NSAlert()
        alert.messageText = title
        var text = message
        if !items.isEmpty {
            text += "\n\n" + items.prefix(limit).map { "• \($0)" }.joined(separator: "\n") + (items.count > limit ? "\nand \(items.count - limit) more" : "")
        }
        alert.informativeText = text
        alert.addButton(withTitle: button)
        alert.addButton(withTitle: "Cancel")
        alert.buttons[0].hasDestructiveAction = true
        alert.beginSheetModal(for: window) { if $0 == .alertFirstButtonReturn { action() } }
    }
}

extension WindowController {
    /// Runs a command in the focused shell when it's at a prompt, else in a
    /// new tab in its folder.
    @discardableResult
    func runInShell(_ command: String) -> Bool {
        guard let p = activePane else {
            model.open(command: command)
            return false
        }
        let here = p.isShell && !p.running && terminals[p.id] != nil
        runInPane(p.id, command)
        return here
    }

    enum OpenWhere { case here, right, down, tab, group, broadcast }

    /// Opens ssh hosts: here, in splits, tabs, or a tab group (with broadcast).
    func open(hosts: [SshHost], _ where_: OpenWhere) {
        guard let first = hosts.first else { return }
        switch where_ {
        case .here: runInShell(Ssh.command(first))
        case .right: model.split(.row, command: Ssh.command(first))
        case .down: model.split(.column, command: Ssh.command(first))
        case .tab: for h in hosts { model.open(command: Ssh.command(h)) }
        case .group, .broadcast:
            let ids = hosts.map { model.open(command: Ssh.command($0)) }
            guard ids.count > 1, let g = model.createGroup(ids[0]) else { return }
            model.setGroup(g, name: Ssh.groupName(hosts))
            for id in ids.dropFirst() { model.addToGroup(id, g) }
            if where_ == .broadcast { Broadcast.shared.toggle(group: g, in: self) }
            model.activate(ids[0])
        }
    }
}

/// A sheet where ⌘↩ presses the default button from anywhere, even a
/// multi-line field (where ↩ makes a new line).
@MainActor
final class SubmitPanel: NSPanel {
    override func performKeyEquivalent(with event: NSEvent) -> Bool {
        if event.modifierFlags.intersection(.deviceIndependentFlagsMask) == .command, event.keyCode == 36 || event.keyCode == 76,
           let b = Self.defaultButton(in: contentView), b.isEnabled {
            b.performClick(nil)
            return true
        }
        return super.performKeyEquivalent(with: event)
    }

    private static func defaultButton(in v: NSView?) -> NSButton? {
        guard let v else { return nil }
        if let b = v as? NSButton, b.keyEquivalent == "\r", b.keyEquivalentModifierMask.isEmpty, !b.isHidden { return b }
        for s in v.subviews { if let b = defaultButton(in: s) { return b } }
        return nil
    }
}

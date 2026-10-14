import AppKit
import SwiftTerm

/// Find in the terminal's output: as you type, Return for the next match,
/// Shift-Return for the previous, Escape to close. Like SearchBar.svelte.
@MainActor
final class SearchBar: NSView, NSSearchFieldDelegate {
    private let field = NSSearchField()
    private let status = NSTextField(labelWithString: "")
    private let caseButton = NSButton(title: "Aa", target: nil, action: nil)
    private unowned let terminal: TerminalView
    var onClose: (() -> Void)?

    init(terminal: TerminalView) {
        self.terminal = terminal
        super.init(frame: .zero)
        let effect = NSVisualEffectView()
        effect.material = .popover
        effect.state = .active
        effect.wantsLayer = true
        effect.layer?.cornerRadius = 8
        effect.translatesAutoresizingMaskIntoConstraints = false
        addSubview(effect)

        field.placeholderString = "Find"
        field.delegate = self
        field.sendsSearchStringImmediately = true
        field.target = self
        field.action = #selector(changed)
        status.font = .systemFont(ofSize: 11)
        status.textColor = .secondaryLabelColor
        caseButton.setButtonType(.pushOnPushOff)
        caseButton.bezelStyle = .accessoryBarAction
        caseButton.toolTip = "Match case"
        caseButton.target = self
        caseButton.action = #selector(changed)
        let prev = NSButton(image: NSImage(systemSymbolName: "chevron.up", accessibilityDescription: "Previous")!,
                            target: self, action: #selector(previous))
        let next = NSButton(image: NSImage(systemSymbolName: "chevron.down", accessibilityDescription: "Next")!,
                            target: self, action: #selector(self.next))
        let close = NSButton(image: NSImage(systemSymbolName: "xmark", accessibilityDescription: "Close")!,
                             target: self, action: #selector(closeBar))
        for b in [prev, next, close] { b.bezelStyle = .accessoryBarAction; b.isBordered = false }
        prev.toolTip = "Previous (⇧⌘G)"
        next.toolTip = "Next (⌘G)"
        close.toolTip = "Close (Esc)"

        let stack = NSStackView(views: [field, status, caseButton, prev, next, close])
        stack.spacing = 6
        stack.edgeInsets = NSEdgeInsets(top: 6, left: 8, bottom: 6, right: 6)
        stack.translatesAutoresizingMaskIntoConstraints = false
        effect.addSubview(stack)
        NSLayoutConstraint.activate([
            effect.leadingAnchor.constraint(equalTo: leadingAnchor),
            effect.trailingAnchor.constraint(equalTo: trailingAnchor),
            effect.topAnchor.constraint(equalTo: topAnchor),
            effect.bottomAnchor.constraint(equalTo: bottomAnchor),
            stack.leadingAnchor.constraint(equalTo: effect.leadingAnchor),
            stack.trailingAnchor.constraint(equalTo: effect.trailingAnchor),
            stack.topAnchor.constraint(equalTo: effect.topAnchor),
            stack.bottomAnchor.constraint(equalTo: effect.bottomAnchor),
            field.widthAnchor.constraint(equalToConstant: 200),
        ])
    }

    required init?(coder: NSCoder) { fatalError() }

    func focus() { window?.makeFirstResponder(field) }

    private var options: SearchOptions {
        SearchOptions(caseSensitive: caseButton.state == .on, regex: false, wholeWord: false)
    }

    private var query: String { field.stringValue }

    /// Search as you type: from the top of the matches each time.
    @objc private func changed() {
        guard !query.isEmpty else {
            terminal.clearSearch()
            status.stringValue = ""
            return
        }
        report(terminal.findNext(query, options: options))
    }

    @objc func next() {
        if !query.isEmpty { report(terminal.findNext(query, options: options)) }
    }

    @objc func previous() {
        if !query.isEmpty { report(terminal.findPrevious(query, options: options)) }
    }

    private func report(_ found: Bool) {
        status.stringValue = found ? "" : "No results"
    }

    @objc func closeBar() {
        terminal.clearSearch()
        onClose?()
    }

    func control(_ control: NSControl, textView: NSTextView, doCommandBy selector: Selector) -> Bool {
        switch selector {
        case #selector(NSResponder.cancelOperation(_:)):
            closeBar()
            return true
        case #selector(NSResponder.insertNewline(_:)):
            if NSEvent.modifierFlags.contains(.shift) { previous() } else { next() }
            return true
        default:
            return false
        }
    }
}

import AppKit
import SwiftTerm
import WtfCore

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

    /// Every match in the scrollback, and the one selected.
    private(set) var found: [Find.Match] = []
    private(set) var current: Int?
    /// The matches changed: the overlay highlights the ones on screen.
    var onFound: (() -> Void)?

    /// Search as you type: the match at or after the current one, so typing
    /// more refines it rather than jumping back to the top.
    @objc private func changed() { search(scroll: true) }

    /// Searches again for output that arrived since, keeping the selected
    /// match and the scroll position.
    func refresh() {
        guard !query.isEmpty else { return }
        search(scroll: false)
    }

    var query: String { field.stringValue }
    var caseSensitive: Bool { caseButton.state == .on }

    private func search(scroll: Bool) {
        let from = current.map { found[$0] }
        guard !query.isEmpty else {
            found = []
            current = nil
            terminal.selection.selectNone()
            status.stringValue = ""
            onFound?()
            return
        }
        let t = terminal.getTerminal()
        found = Find.matches(in: t, query, caseSensitive: caseSensitive)
        if found.isEmpty {
            current = nil
        } else if let from {
            current = found.firstIndex { ($0.row, $0.col) >= (from.row, from.col) } ?? 0
        } else {
            // The first match from the bottom of the screen up, as a terminal search reads.
            let bottom = t.buffer.totalLinesTrimmed + t.buffer.yDisp + t.rows
            current = found.lastIndex { $0.row < bottom } ?? 0
        }
        show(scroll: scroll)
    }

    @objc func next() { step(1) }
    @objc func previous() { step(-1) }

    private func step(_ d: Int) {
        guard !query.isEmpty else { return }
        if found.isEmpty { changed() }
        guard !found.isEmpty else { return }
        current = ((current ?? -d) + d + found.count) % found.count
        show()
    }

    /// Selects the current match, scrolls to it, and says where it is.
    /// Rows in `found` count from the first line ever written; the buffer's
    /// rows start after the lines trimmed off the top.
    private func show(scroll: Bool = true) {
        status.stringValue = Find.status(found, current: current)
        status.textColor = found.isEmpty ? .systemOrange : .secondaryLabelColor
        if let i = current {
            let m = found[i]
            let t = terminal.getTerminal()
            let row = m.row - t.buffer.totalLinesTrimmed
            // A refresh leaves the selection alone (it may be the user's);
            // the overlay outlines the current match.
            if !scroll {
                onFound?()
                return
            }
            terminal.selection.setSelection(start: Position(col: m.col, row: row), end: Position(col: m.col + m.width, row: row))
            if row < t.buffer.yDisp || row >= t.buffer.yDisp + t.rows {
                terminal.scrollTo(row: max(0, row - t.rows / 2))
            }
        } else if scroll {
            terminal.selection.selectNone()
        }
        onFound?()
    }

    @objc func closeBar() {
        found = []
        current = nil
        onFound?()
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

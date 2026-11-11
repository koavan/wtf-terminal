import AppKit
import SwiftTerm
import WtfCore

/// SwiftTerm's view with wtf's behaviour around the clipboard: copies leave
/// secrets out while Hide Secrets is on, copy-on-select, and pastes that need
/// a look first are shown before they reach the shell.
@MainActor
final class WtfTerminalView: TerminalView {
    weak var controller: TerminalController?
    /// Asks before pasting (see PasteConfirm); the controller sets it.
    var confirmPaste: ((String) -> Void)?
    /// Builds the right-click menu for a point in the view.
    var contextMenu: ((NSEvent) -> NSMenu?)?
    var onSelectionChanged: (() -> Void)?
    var onFocus: (() -> Void)?
    /// ⌘-click on a token (timestamp, address, path, resource, JWT): true when it took the click.
    var onCommandClick: ((NSEvent) -> Bool)?

    /// A click into the pane focuses it (keyboard moves go through the model).
    override func mouseDown(with event: NSEvent) {
        onFocus?()
        let mods = event.modifierFlags.intersection(.deviceIndependentFlagsMask)
        if mods == .command, onCommandClick?(event) == true { return }
        // ⌥-drag selects even in programs that take the mouse (vim, htop, tmux).
        if mods.contains(.option), allowMouseReporting {
            allowMouseReporting = false
            optionSelecting = true
        }
        super.mouseDown(with: event)
    }

    private var optionSelecting = false

    override func mouseUp(with event: NSEvent) {
        super.mouseUp(with: event)
        if optionSelecting {
            optionSelecting = false
            allowMouseReporting = true
        }
    }

    var selectedText: String? {
        guard selectionActive else { return nil }
        return getSelection()
    }

    override func copy(_ sender: Any) {
        guard let text = selectedText else { return }
        Clipboard.copy(Redaction.shared.text(text))
    }

    override func paste(_ sender: Any) {
        Debug.log("paste: \(Clipboard.text?.count ?? -1) characters on the clipboard")
        guard let text = Clipboard.text, !text.isEmpty else { return }
        let ask = Settings.shared.config?.confirmPaste ?? true
        let facts = PasteFacts(text)
        Debug.log("paste: \(facts.lines) lines, final newline \(facts.finalNewline), controls \(facts.controls), confirm-paste \(ask), handler \(confirmPaste != nil)")
        if let confirm = confirmPaste, ask, facts.needsConfirm {
            confirm(text)
        } else {
            pasteText(text)
        }
    }

    /// Sends text as a paste: bracketed when the program asked for it, and
    /// with line breaks as Return, as xterm.js does.
    func pasteText(_ text: String) {
        let body = text.replacingOccurrences(of: "\r\n", with: "\r").replacingOccurrences(of: "\n", with: "\r")
        if getTerminal().bracketedPasteMode {
            send(txt: "\u{1b}[200~" + body + "\u{1b}[201~")
        } else {
            send(txt: body)
        }
    }

    override func selectionChanged(source: Terminal) {
        super.selectionChanged(source: source)
        if Settings.shared.config?.copyOnSelect == true, let text = selectedText, !text.isEmpty {
            Clipboard.copy(Redaction.shared.text(text))
        }
        onSelectionChanged?()
    }

    override func menu(for event: NSEvent) -> NSMenu? {
        // Right-click selects the word under the mouse (unless it's in the
        // selection already), so the menu can copy or inspect it.
        let t = getTerminal()
        if !t.isCurrentBufferAlternate, let c = cell(at: event) {
            let at = Position(col: c.col, row: t.buffer.yDisp + c.row)
            if !(selectionActive && Self.contains(selection.start, selection.end, at)) {
                selection.selectWordOrExpression(at: at, in: t.buffer)
            }
        }
        return contextMenu?(event) ?? super.menu(for: event)
    }

    private static func contains(_ a: Position, _ b: Position, _ p: Position) -> Bool {
        let (s, e) = Position.compare(a, b) == .after ? (b, a) : (a, b)
        return Position.compare(p, s) != .before && Position.compare(p, e) == .before
    }

    override func validateUserInterfaceItem(_ item: NSValidatedUserInterfaceItem) -> Bool {
        if item.action == #selector(toggleSecrets(_:)), let m = item as? NSMenuItem {
            m.title = Redaction.shared.on ? "Show Secrets" : "Hide Secrets"
            return true
        }
        let ours: [Selector] = [#selector(wtfFind(_:)), #selector(clearBuffer(_:)), #selector(copyLastOutput(_:)),
                                #selector(previousPrompt(_:)), #selector(nextPrompt(_:)), #selector(biggerText(_:)),
                                #selector(smallerText(_:)), #selector(actualSize(_:))]
        if let action = item.action, ours.contains(action) { return true }
        return super.validateUserInterfaceItem(item)
    }

    /// The row on screen and column under a point in the window, if inside.
    func cell(at event: NSEvent) -> (row: Int, col: Int)? {
        let p = convert(event.locationInWindow, from: nil)
        let t = getTerminal()
        guard let px = cellSizeInPixels(source: t) else { return nil }
        let scale = window?.backingScaleFactor ?? 2
        let w = CGFloat(px.width) / scale, h = CGFloat(px.height) / scale
        let row = Int((bounds.height - p.y) / h), col = Int(p.x / w)
        guard row >= 0, row < t.rows, col >= 0, col < t.cols else { return nil }
        return (row, col)
    }
}

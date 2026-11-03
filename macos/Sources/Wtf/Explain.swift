import AppKit
import WtfCore

// Explain this failure (#24) goes through the Claude pane's /wtf:explain
// (ClaudePane.explain). Typing at a prompt is also what the bridge's
// type_at_prompt does.

extension TerminalController {
    enum TypeResult { case typed, busy, copied }

    /// Types a suggested command at this pane's prompt without Return: Ctrl-U
    /// first, then the text as a paste (bracketed when the shell asks). A
    /// multi-line one is copied instead when the shell would run each line;
    /// nothing is typed while a program runs.
    func typeAtPrompt(_ text: String) -> TypeResult {
        guard let p = owner?.model.panes[paneId], p.isShell, !p.running else { return .busy }
        if text.contains("\n"), !view.getTerminal().bracketedPasteMode {
            Clipboard.copy(text)
            return .copied
        }
        type(Shell.clearLine)
        insert(text)
        return .typed
    }
}

extension WindowController {
    /// Explains the focused pane's last command, when it failed.
    func explainLastFailure() {
        guard let term = activeTerminal, let id = term.lastFinishedBlock, let b = term.block(id) else {
            Toast.show("No finished command to explain yet", in: rootView)
            return
        }
        guard b.exitCode != 0 else {
            Toast.show("The last command succeeded: nothing to explain", in: rootView)
            return
        }
        model.focusPane(term.paneId)
        ClaudePane.explain(block: id, in: self)
    }
}

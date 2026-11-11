import AppKit
import WtfCore

/// A paste with more than one line, a final newline or control characters,
/// shown as it would arrive before anything reaches the shell. Like
/// internal/gtkui/paste.go.
@MainActor
enum PasteConfirm {
    static func show(_ text: String, bracketed: Bool, panes: Int = 1, in window: NSWindow, paste: @escaping (String) -> Void) {
        let f = PasteFacts(text)
        let alert = NSAlert()
        alert.messageText = f.lines > 1 ? "Paste \(f.lines) lines?"
            : f.controls > 0 ? "Paste text with control characters?" : "Paste with a newline?"
        var has: [String] = []
        if f.controls > 0 { has.append("\(f.controls) control \(f.controls == 1 ? "character" : "characters") (shown as ␛ and the like)") }
        if f.finalNewline { has.append("a newline at the end") }
        alert.informativeText = [
            has.isEmpty ? "" : "It has \(has.joined(separator: " and ")).",
            bracketed ? "The program here takes it as one paste and runs nothing until you press Enter."
                : "The program here hasn’t asked for bracketed paste: a shell runs each line as it arrives.",
            panes > 1 ? "Broadcast input is on: it goes to \(panes) panes." : "",
        ].filter { !$0.isEmpty }.joined(separator: " ")

        // The text as it would arrive, control characters made visible.
        let scroll = NSTextView.scrollableTextView()
        scroll.frame = NSRect(x: 0, y: 0, width: 560, height: min(320, CGFloat(f.lines) * 18 + 24))
        scroll.borderType = .bezelBorder
        let view = scroll.documentView as! NSTextView
        view.isEditable = false
        view.font = .monospacedSystemFont(ofSize: 12, weight: .regular)
        view.textContainerInset = NSSize(width: 6, height: 6)
        view.string = Paste.showControls(Redaction.shared.text(text))
        alert.accessoryView = scroll

        alert.addButton(withTitle: "Paste")
        if f.finalNewline { alert.addButton(withTitle: "Paste Without Final Newline") }
        alert.addButton(withTitle: "Cancel")
        alert.addButton(withTitle: "Don’t Ask Again")
        alert.buttons.last?.toolTip = "Turns off confirm-paste. Toggle Paste Confirmation in the palette turns it back on."

        Debug.log("paste confirm shown: \(alert.messageText)")
        alert.beginSheetModal(for: window) { response in
            let index = response.rawValue - NSApplication.ModalResponse.alertFirstButtonReturn.rawValue
            let title = index >= 0 && index < alert.buttons.count ? alert.buttons[index].title : "Cancel"
            switch title {
            case "Paste":
                paste(text)
            case "Paste Without Final Newline":
                paste(Paste.withoutFinalNewline(text))
            case "Don’t Ask Again":
                paste(text)
                Task {
                    do {
                        try await Backend.run("config", "SetConfirmPaste", [false])
                        Toast.show("Pastes go straight in now. Toggle Paste Confirmation in the palette to change it", in: window.contentView)
                    } catch {
                        Toast.show("Couldn't save the setting: \(error.localizedDescription)", in: window.contentView)
                    }
                }
            default:
                break
            }
        }
    }
}

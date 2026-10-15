import AppKit

@MainActor
enum Clipboard {
    /// The general pasteboard; debug runs use a private one so they never
    /// touch the user's clipboard.
    static var board = NSPasteboard.general

    static func copy(_ text: String) {
        board.clearContents()
        board.setString(text, forType: .string)
    }

    static var text: String? { board.string(forType: .string) }

    /// Copies a secret wtf revealed (a Kubernetes secret's value, a decoded
    /// token, a generated password) and empties the clipboard after
    /// `clipboard-clear` seconds, unless something else was copied meanwhile.
    /// Like copySecret in lib/clipboard.ts.
    static func copySecret(_ text: String, what: String, in view: NSView?) {
        copy(text)
        let seconds = Settings.shared.config?.clipboardClear ?? 30
        if seconds > 0 {
            // The change count moves with every copy, ours or anyone's.
            let count = board.changeCount
            DispatchQueue.main.asyncAfter(deadline: .now() + seconds) {
                if board.changeCount == count { board.clearContents() }
            }
            let s = Int(seconds)
            Toast.show("Copied \(what). The clipboard clears in \(s) \(s == 1 ? "second" : "seconds")", in: view)
        } else {
            Toast.show("Copied \(what)", in: view)
        }
    }
}

/// A short message at the bottom of the window that fades after a moment.
@MainActor
enum Toast {
    static func show(_ message: String, in view: NSView?, seconds: Double = 2.4) {
        guard let content = view?.window?.contentView else { return }
        content.subviews.filter { $0.identifier == id }.forEach { $0.removeFromSuperview() }

        let label = NSTextField(labelWithString: message)
        label.font = .systemFont(ofSize: 12, weight: .medium)
        label.textColor = .labelColor
        let box = NSVisualEffectView()
        box.identifier = id
        box.material = .hudWindow
        box.state = .active
        box.wantsLayer = true
        box.layer?.cornerRadius = 8
        box.translatesAutoresizingMaskIntoConstraints = false
        label.translatesAutoresizingMaskIntoConstraints = false
        box.addSubview(label)
        content.addSubview(box)
        NSLayoutConstraint.activate([
            label.leadingAnchor.constraint(equalTo: box.leadingAnchor, constant: 12),
            label.trailingAnchor.constraint(equalTo: box.trailingAnchor, constant: -12),
            label.topAnchor.constraint(equalTo: box.topAnchor, constant: 7),
            label.bottomAnchor.constraint(equalTo: box.bottomAnchor, constant: -7),
            box.centerXAnchor.constraint(equalTo: content.centerXAnchor),
            box.bottomAnchor.constraint(equalTo: content.bottomAnchor, constant: -16),
        ])
        DispatchQueue.main.asyncAfter(deadline: .now() + seconds) {
            NSAnimationContext.runAnimationGroup({ $0.duration = 0.25; box.animator().alphaValue = 0 }) {
                MainActor.assumeIsolated { box.removeFromSuperview() }
            }
        }
    }

    private static let id = NSUserInterfaceItemIdentifier("wtf.toast")
}

/// Logging for debug runs (WTF_NATIVE_DEBUG).
enum Debug {
    static let on = ProcessInfo.processInfo.environment["WTF_NATIVE_DEBUG"] != nil
    static func log(_ s: @autoclosure () -> String) { if on { NSLog("%@", s()) } }
}

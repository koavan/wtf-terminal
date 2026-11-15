import AppKit
import WtfCore

/// A newer wtf on GitHub (internal/update): the daily check's notice, and
/// Check for Updates…. Nothing is downloaded: Download opens the release page.
@MainActor
enum Updates {
    static func listen() {
        Backend.on("update:available", Go.update.Release.self) { r in
            guard let w = target() else { return }
            UpdateNotice.show(r, in: w)
        }
    }

    /// Check for Updates…: asks now, and says what it found.
    static func check(in w: WindowController) {
        Toast.show("Checking for updates…", in: w.rootView)
        Task {
            do {
                let st = try await Services.update.Check()
                if let r = st.available {
                    UpdateNotice.show(r, in: w)
                } else {
                    Toast.show("wtf \(st.current) is the latest version", in: w.rootView, seconds: 3)
                }
            } catch {
                Toast.show("Couldn’t check for updates: \(error.localizedDescription)", in: w.rootView, seconds: 4)
            }
        }
    }

    private static func target() -> WindowController? {
        let m = WindowManager.shared
        if let k = m.keyWindow, !k.background { return k }
        return m.windows.first { !$0.background && $0.window?.isVisible == true }
    }
}

/// The notice in a window's lower right corner: stays until dismissed.
@MainActor
enum UpdateNotice {
    private static let id = NSUserInterfaceItemIdentifier("wtf.update")

    static func show(_ r: Go.update.Release, in w: WindowController) {
        guard let content = w.window?.contentView else { return }
        for window in WindowManager.shared.windows {
            window.window?.contentView?.subviews.filter { $0.identifier == id }.forEach { $0.removeFromSuperview() }
        }

        let box = NSVisualEffectView()
        box.identifier = id
        box.material = .hudWindow
        box.state = .active
        box.wantsLayer = true
        box.layer?.cornerRadius = 10
        box.translatesAutoresizingMaskIntoConstraints = false

        let heading = NSTextField(labelWithString: "wtf \(r.version) is available")
        heading.font = .systemFont(ofSize: 13, weight: .semibold)
        let detail = NSTextField(wrappingLabelWithString: r.title)
        detail.font = .systemFont(ofSize: 11.5)
        detail.textColor = .secondaryLabelColor
        detail.maximumNumberOfLines = 2
        detail.preferredMaxLayoutWidth = 280

        let close = { box.removeFromSuperview() }
        let download = ClosureButton(title: "Download…") {
            if let u = URL(string: r.url) { NSWorkspace.shared.open(u) }
            close()
        }
        download.keyEquivalent = ""
        download.bezelColor = .controlAccentColor
        download.toolTip = "Open the release page on GitHub: what's new, and the .dmg"
        let skip = ClosureButton(title: "Skip This Version") {
            Task { try? await Services.update.Skip(r.version) }
            close()
        }
        skip.toolTip = "Don't tell me about \(r.version) again (Check for Updates… still shows it)"
        let later = ClosureButton(title: "Later") { close() }
        later.toolTip = "Ask again the next time wtf starts"
        for b in [download, skip, later] { b.controlSize = .small }

        let buttons = NSStackView(views: [download, skip, later])
        buttons.spacing = 6
        let stack = NSStackView(views: [heading, detail, buttons])
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 6
        stack.translatesAutoresizingMaskIntoConstraints = false
        box.addSubview(stack)
        content.addSubview(box)
        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: box.leadingAnchor, constant: 14),
            stack.trailingAnchor.constraint(equalTo: box.trailingAnchor, constant: -14),
            stack.topAnchor.constraint(equalTo: box.topAnchor, constant: 12),
            stack.bottomAnchor.constraint(equalTo: box.bottomAnchor, constant: -12),
            box.trailingAnchor.constraint(equalTo: content.trailingAnchor, constant: -16),
            box.bottomAnchor.constraint(equalTo: content.bottomAnchor, constant: -16),
            box.widthAnchor.constraint(lessThanOrEqualToConstant: 340),
        ])
    }
}

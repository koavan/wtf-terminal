import AppKit
import WtfCore

/// Settings (⌘,): one window for the app, with a section per toolbar
/// button. General leads to the theme, config.toml for the rest, and Ask
/// Claude's auto mode. Made on first open and let go on close, like the
/// manual.
@MainActor
final class SettingsWindow: NSObject, NSWindowDelegate, NSToolbarDelegate {
    enum Section: String, CaseIterable {
        case general
        var title: String { "General" }
        var symbol: String { "gearshape" }
    }

    private static var shared: SettingsWindow?
    private static let width: CGFloat = 680

    /// Opens Settings (at a section), or brings it to the front.
    static func show(_ section: Section? = nil, from w: WindowController? = nil) {
        let s = shared ?? SettingsWindow()
        shared = s
        if let w { s.from = w }
        if let section, section != s.section { s.select(section) }
        s.window.makeKeyAndOrderFront(nil)
    }

    private let window: NSWindow
    private let scroll = NSScrollView()
    private let doc = FlippedView()
    private let content = NSStackView()
    /// The terminal window it was opened from, for the palette (Change Theme).
    private weak var from: WindowController?
    private var section = Section.general
    private var seen: Config?
    private var keys: Any?

    private var inner: CGFloat { Self.width - 48 }
    private var shell: WindowController? { from ?? WindowManager.shared.keyWindow ?? WindowManager.shared.windows.first }

    private override init() {
        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: Self.width, height: 560),
                          styleMask: [.titled, .closable, .miniaturizable, .resizable], backing: .buffered, defer: false)
        super.init()
        window.title = "Settings"
        window.isReleasedWhenClosed = false
        window.delegate = self
        // As wide as the rows are laid out; as tall as you like.
        window.contentMinSize = NSSize(width: Self.width, height: 320)
        window.contentMaxSize = NSSize(width: Self.width, height: 4000)
        let bar = NSToolbar(identifier: "wtf.settings")
        bar.delegate = self
        bar.displayMode = .iconAndLabel
        window.toolbar = bar
        window.toolbarStyle = .preference
        bar.selectedItemIdentifier = .init(section.rawValue)

        content.orientation = .vertical
        content.alignment = .leading
        content.spacing = 10
        content.edgeInsets = NSEdgeInsets(top: 20, left: 24, bottom: 20, right: 24)
        content.translatesAutoresizingMaskIntoConstraints = false
        doc.addSubview(content)
        NSLayoutConstraint.activate([
            content.topAnchor.constraint(equalTo: doc.topAnchor),
            content.leadingAnchor.constraint(equalTo: doc.leadingAnchor),
            content.widthAnchor.constraint(equalToConstant: Self.width),
        ])
        scroll.documentView = doc
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        window.contentView = scroll
        window.center()
        // The menus' ⌘W closes a pane of the terminal window, not this one.
        keys = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] e in
            guard let self, e.window === self.window, e.modifierFlags.intersection(.deviceIndependentFlagsMask) == .command,
                  e.charactersIgnoringModifiers == "w" else { return e }
            self.window.performClose(nil)
            return nil
        }
        seen = Settings.shared.config
        Settings.shared.observe(self) { s in
            guard Settings.shared.config != s.seen else { return }
            s.seen = Settings.shared.config
            s.render()
        }
        render()
    }

    func windowWillClose(_ notification: Notification) {
        if let keys { NSEvent.removeMonitor(keys) }
        keys = nil
        Self.shared = nil
        WindowManager.shared.keyWindow?.focusActive()
    }

    // MARK: Toolbar

    func toolbarAllowedItemIdentifiers(_ toolbar: NSToolbar) -> [NSToolbarItem.Identifier] { Section.allCases.map { .init($0.rawValue) } }
    func toolbarDefaultItemIdentifiers(_ toolbar: NSToolbar) -> [NSToolbarItem.Identifier] { toolbarAllowedItemIdentifiers(toolbar) }
    func toolbarSelectableItemIdentifiers(_ toolbar: NSToolbar) -> [NSToolbarItem.Identifier] { toolbarAllowedItemIdentifiers(toolbar) }

    func toolbar(_ toolbar: NSToolbar, itemForItemIdentifier id: NSToolbarItem.Identifier, willBeInsertedIntoToolbar flag: Bool) -> NSToolbarItem? {
        guard let s = Section(rawValue: id.rawValue) else { return nil }
        let item = NSToolbarItem(itemIdentifier: id)
        item.label = s.title
        item.image = NSImage(systemSymbolName: s.symbol, accessibilityDescription: s.title)
        item.target = self
        item.action = #selector(pick(_:))
        return item
    }

    @objc private func pick(_ item: NSToolbarItem) {
        guard let s = Section(rawValue: item.itemIdentifier.rawValue), s != section else { return }
        select(s)
    }

    private func select(_ s: Section) {
        section = s
        window.toolbar?.selectedItemIdentifier = .init(s.rawValue)
        render()
    }

    // MARK: Rendering

    private func render() {
        content.arrangedSubviews.forEach { $0.removeFromSuperview() }
        switch section {
        case .general: renderGeneral()
        }
        content.layoutSubtreeIfNeeded()
        doc.frame = NSRect(x: 0, y: 0, width: Self.width, height: content.fittingSize.height)
    }

    private func label(_ s: String, size: CGFloat = 12.5, weight: NSFont.Weight = .regular, color: NSColor = .labelColor, mono: Bool = false) -> NSTextField {
        let l = NSTextField(wrappingLabelWithString: s)
        l.font = mono ? .monospacedSystemFont(ofSize: size - 1, weight: weight) : .systemFont(ofSize: size, weight: weight)
        l.textColor = color
        l.preferredMaxLayoutWidth = inner
        l.isSelectable = mono
        return l
    }

    private func heading(_ t: String, _ sub: String) {
        content.addArrangedSubview(label(t, size: 15, weight: .semibold))
        content.addArrangedSubview(label(sub, size: 12, color: .secondaryLabelColor))
    }

    private func bar(_ views: [NSView]) {
        let row = NSStackView(views: views)
        row.translatesAutoresizingMaskIntoConstraints = false
        row.widthAnchor.constraint(equalToConstant: inner).isActive = true
        content.addArrangedSubview(row)
    }

    private func spacer() -> NSView {
        let v = NSView()
        v.setContentHuggingPriority(.init(1), for: .horizontal)
        return v
    }

    private func separator() {
        let line = NSBox()
        line.boxType = .separator
        content.addArrangedSubview(line)
        line.widthAnchor.constraint(equalToConstant: inner).isActive = true
    }

    private func openConfig() {
        Task {
            do { try await Services.config.OpenConfigFile() } catch {
                Toast.show("Couldn't open config.toml: \(error.localizedDescription)", in: window.contentView)
            }
        }
    }

    // MARK: General

    private func renderGeneral() {
        heading("General", "Most settings are in config.toml for now: fonts, the shell, scrollback, Hide Secrets at startup and the rest, each documented there. Changes apply when you save the file.")
        let theme = Settings.shared.config?.theme ?? ""
        let name = NSTextField(labelWithString: "Theme")
        name.font = .systemFont(ofSize: 12.5, weight: .semibold)
        let current = NSTextField(labelWithString: theme.isEmpty || theme.lowercased() == "auto" ? "Auto (follows the system)" : theme)
        current.textColor = .secondaryLabelColor
        let change = ClosureButton(title: "Change Theme…") { [weak self] in
            guard let w = self?.shell else { return }
            w.window?.makeKeyAndOrderFront(nil)
            Palette.toggle(.themes, in: w)
        }
        change.toolTip = "Pick a theme in the palette, previewed as you move"
        bar([name, current, spacer(), change])
        separator()
        let file = ClosureButton(title: "Open config.toml") { [weak self] in self?.openConfig() }
        let themes = ClosureButton(title: "Open Themes Folder") { [weak self] in
            Task {
                do { try await Services.config.OpenThemesFolder() } catch {
                    Toast.show("Couldn't open folder: \(error.localizedDescription)", in: self?.window.contentView)
                }
            }
        }
        themes.toolTip = "Add your own themes as files here"
        bar([file, themes, spacer()])
        separator()
        let claude = NSTextField(labelWithString: "Ask Claude")
        claude.font = .systemFont(ofSize: 12.5, weight: .semibold)
        content.addArrangedSubview(claude)
        content.addArrangedSubview(label("Ask Claude (⌘I) runs your own Claude Code in a split beside the shell, with wtf’s tools for that shell. wtf makes Claude Code ask before every command it runs or types in your shells and every file it changes, even in its auto mode or after “don’t ask again”.", size: 12, color: .secondaryLabelColor))
        let auto = NSButton(checkboxWithTitle: "Let Claude Code’s own permission mode decide instead (claude-auto-mode)", target: self, action: #selector(toggleClaudeAuto(_:)))
        auto.state = Settings.shared.config?.claudeAutoMode == true ? .on : .off
        auto.toolTip = "Applies to Claude panes opened from now on"
        content.addArrangedSubview(auto)
    }

    @objc private func toggleClaudeAuto(_ sender: NSButton) {
        let on = sender.state == .on
        Task {
            do { try await Services.config.SetClaudeAutoMode(on) } catch {
                sender.state = on ? .off : .on
                Toast.show("Couldn't change the setting: \(error.localizedDescription)", in: window.contentView)
            }
        }
    }
}

import AppKit
import WtfCore

/// The popover under a group chip: name, colour, and the group's actions.
/// Like GroupEditor.svelte.
@MainActor
final class GroupEditor: NSViewController, NSTextFieldDelegate, NSPopoverDelegate {
    private static var open: NSPopover?

    private let groupId: Int
    private unowned let window: WindowController
    private let name = NSTextField()
    private var swatches: [NSButton] = []

    static func show(group: Int, in w: WindowController, from view: NSView, at rect: NSRect) {
        open?.close()
        let p = NSPopover()
        let editor = GroupEditor(group: group, window: w)
        p.contentViewController = editor
        p.behavior = .transient
        p.delegate = editor
        p.show(relativeTo: NSRect(x: rect.minX, y: rect.minY, width: 1, height: rect.height), of: view, preferredEdge: .maxY)
        open = p
    }

    init(group: Int, window: WindowController) {
        groupId = group
        self.window = window
        super.init(nibName: nil, bundle: nil)
    }

    required init?(coder: NSCoder) { fatalError() }

    private var group: TabGroup? { window.model.group(groupId) }

    override func loadView() {
        let dark = Settings.shared.theme?.dark ?? true
        name.placeholderString = "Name this group"
        name.stringValue = group?.name ?? ""
        name.delegate = self
        name.bezelStyle = .roundedBezel

        swatches = GroupColor.allCases.map { c in
            let b = NSButton(image: WindowController.dot(NSColor(hex: c.hex(dark: dark))), target: self, action: #selector(pick(_:)))
            b.isBordered = false
            b.imageScaling = .scaleProportionallyUpOrDown
            b.toolTip = c.rawValue.capitalized
            b.identifier = .init(c.rawValue)
            b.widthAnchor.constraint(equalToConstant: 20).isActive = true
            b.heightAnchor.constraint(equalToConstant: 20).isActive = true
            b.wantsLayer = true
            b.layer?.cornerRadius = 10
            return b
        }
        let row = NSStackView(views: swatches)
        row.spacing = 4

        let broadcasting = Broadcast.shared.group(groupId, in: window)
        let actions = [
            ("New tab in group", #selector(newTab)),
            (broadcasting ? "Stop broadcasting input" : "Broadcast input to group", #selector(broadcast)),
            ("Ungroup", #selector(ungroup)),
            ("Close group", #selector(closeGroup)),
        ].map { title, action -> NSButton in
            let b = NSButton(title: title, target: self, action: action)
            b.bezelStyle = .accessoryBarAction
            b.alignment = .left
            if action == #selector(closeGroup) { b.contentTintColor = .systemRed }
            return b
        }
        let sep = NSBox()
        sep.boxType = .separator
        let stack = NSStackView(views: [name, row, sep] + actions)
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 8
        stack.edgeInsets = NSEdgeInsets(top: 12, left: 12, bottom: 10, right: 12)
        name.widthAnchor.constraint(equalToConstant: 212).isActive = true
        sep.widthAnchor.constraint(equalTo: name.widthAnchor).isActive = true
        view = stack
        markSelected()
    }

    override func viewDidAppear() {
        super.viewDidAppear()
        view.window?.makeFirstResponder(name)
        name.currentEditor()?.selectAll(nil)
    }

    private func markSelected() {
        for b in swatches {
            let on = b.identifier?.rawValue == group?.color.rawValue
            b.layer?.borderWidth = on ? 2 : 0
            b.layer?.borderColor = NSColor.labelColor.cgColor
        }
    }

    // Edits apply as you type.
    func controlTextDidChange(_ note: Notification) { window.model.setGroup(groupId, name: name.stringValue) }

    func control(_ control: NSControl, textView: NSTextView, doCommandBy selector: Selector) -> Bool {
        if selector == #selector(NSResponder.insertNewline(_:)) {
            close()
            return true
        }
        return false
    }

    @objc private func pick(_ b: NSButton) {
        guard let c = b.identifier.flatMap({ GroupColor(rawValue: $0.rawValue) }) else { return }
        window.model.setGroup(groupId, color: c)
        markSelected()
    }

    private func close() { Self.open?.close() }

    @objc private func newTab() { close(); window.model.newTabInGroup(groupId) }
    @objc private func broadcast() { close(); Broadcast.shared.toggle(group: groupId, in: window) }
    @objc private func ungroup() { close(); window.model.ungroup(groupId) }

    @objc private func closeGroup() {
        close()
        let tabs = window.model.tabsIn(groupId)
        let n = tabs.count
        let title = group.map { $0.name.isEmpty ? "Close this group?" : "Close the “\($0.name)” group?" } ?? "Close this group?"
        window.confirmIfBusy(tabs.flatMap(\.root.leaves), title: title, button: n == 1 ? "Close Tab" : "Close Tabs") { [weak window, groupId] in
            window?.model.closeGroup(groupId)
        }
    }

    func popoverDidClose(_ note: Notification) {
        Self.open = nil
        window.focusActive()
    }
}

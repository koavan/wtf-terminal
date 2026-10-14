import AppKit
import WtfCore

/// The tab strip, which is also the window's title bar: group chips, tabs
/// and the + button, drawn in one view (cheap however many tabs). Like
/// TabStrip.svelte: click to activate, middle-click or × to close,
/// double-click to rename, right-click for the menu, drag to reorder, drag
/// off the strip for a new window. Empty space moves the window.
@MainActor
final class TabStripView: NSView, NSTextFieldDelegate {
    static let height: CGFloat = 52
    static let tabHeight: CGFloat = 32

    enum Item {
        case chip(group: Int, rect: NSRect)
        case tab(id: Int, rect: NSRect)
    }

    unowned let model: WindowModel
    /// Callbacks to the window.
    var onActivate: ((Int) -> Void)?
    var onClose: ((Int) -> Void)?
    var onNewTab: (() -> Void)?
    var onMenu: ((_ tab: Int, _ event: NSEvent) -> NSMenu?)?
    var onChip: ((_ group: Int, _ rightClick: Bool, _ rect: NSRect) -> Void)?
    var onTearOff: ((_ tab: Int, _ screenPoint: NSPoint) -> Void)?
    var onRenamed: (() -> Void)?
    /// Whether the tab's input is broadcast, and whether it records.
    var broadcasting: (Int) -> Bool = { _ in false }
    var recording: (Int) -> Bool = { _ in false }

    private(set) var items: [Item] = []
    private var plusRect = NSRect.zero
    private var hover: Int?
    private var hoverClose = false
    private var press: (tab: Int, at: NSPoint, offset: CGFloat)?
    private var drag: (tab: Int, x: CGFloat, tearing: Bool)?
    private var renaming: (tab: Int, field: NSTextField)?

    // Colours, from the theme.
    var dark = true
    var chrome = NSColor.windowBackgroundColor
    var surface = NSColor.textBackgroundColor
    var accent = NSColor.controlAccentColor
    var ok = NSColor.systemGreen
    var danger = NSColor.systemRed

    init(model: WindowModel) {
        self.model = model
        super.init(frame: .zero)
        wantsLayer = true
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }
    override var mouseDownCanMoveWindow: Bool { false }
    override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }

    /// Room for the traffic lights, except in full screen.
    private var leftInset: CGFloat { window?.styleMask.contains(.fullScreen) == true ? 12 : 84 }

    func refresh() {
        layoutItems()
        needsDisplay = true
        window?.invalidateCursorRects(for: self)
    }

    override func setFrameSize(_ size: NSSize) {
        super.setFrameSize(size)
        refresh()
    }

    private func groupColor(_ g: TabGroup) -> NSColor { NSColor(hex: g.color.hex(dark: dark)) }

    private func chipWidth(_ g: TabGroup, count: Int) -> CGFloat {
        if g.name.isEmpty && !g.collapsed { return 14 }
        let text = (g.name.isEmpty ? "" : g.name) as NSString
        var w = text.size(withAttributes: [.font: Self.chipFont]).width + 16
        if g.collapsed { w += 18 + (g.name.isEmpty ? 0 : 4) }
        return min(160, max(22, w))
    }

    static let titleFont = NSFont.systemFont(ofSize: 12.5, weight: .medium)
    static let chipFont = NSFont.systemFont(ofSize: 11.5, weight: .semibold)

    /// Places chips and tabs: tabs share what the chips leave, 220pt each at
    /// most and 44pt at least.
    private func layoutItems() {
        var plan: [(chip: TabGroup?, tab: Tab?)] = []
        var lastGroup: Int?
        for t in model.tabs {
            if let g = t.groupId, g != lastGroup, let group = model.group(g) { plan.append((group, nil)) }
            lastGroup = t.groupId
            if !model.isHidden(t) { plan.append((nil, t)) }
        }
        let gap: CGFloat = 4
        let chips = plan.compactMap(\.chip)
        let chipSpace = chips.reduce(0) { $0 + chipWidth($1, count: model.tabsIn($1.id).count) + gap }
        let tabCount = CGFloat(plan.filter { $0.tab != nil }.count)
        let available = bounds.width - leftInset - 12 - 36 - chipSpace
        let tabWidth = tabCount > 0 ? min(220, max(44, (available - gap * tabCount) / tabCount)) : 0
        var x = leftInset
        let y = (Self.height - Self.tabHeight) / 2
        items = plan.map { entry in
            if let g = entry.chip {
                let w = chipWidth(g, count: model.tabsIn(g.id).count)
                let h: CGFloat = g.name.isEmpty && !g.collapsed ? 14 : 22
                defer { x += w + gap }
                return .chip(group: g.id, rect: NSRect(x: x, y: (Self.height - h) / 2, width: w, height: h))
            }
            defer { x += tabWidth + gap }
            return .tab(id: entry.tab!.id, rect: NSRect(x: x, y: y, width: tabWidth, height: Self.tabHeight))
        }
        plusRect = NSRect(x: x + 2, y: (Self.height - 28) / 2, width: 28, height: 28)
    }

    private func rect(of tab: Int) -> NSRect? {
        for case .tab(let id, let r) in items where id == tab { return r }
        return nil
    }

    private func closeRect(_ r: NSRect) -> NSRect { NSRect(x: r.maxX - 24, y: r.midY - 9, width: 18, height: 18) }

    // MARK: Drawing

    override func draw(_ dirty: NSRect) {
        chrome.setFill()
        bounds.fill()

        // Group underlines: from the chip's middle under its last member.
        for case .chip(let gid, let chip) in items {
            guard let g = model.group(gid), !g.collapsed else { continue }
            let members = model.tabsIn(gid).compactMap { t -> NSRect? in
                drag?.tab == t.id ? nil : rect(of: t.id)
            }
            guard let last = members.last else { continue }
            groupColor(g).setFill()
            let line = NSRect(x: chip.midX, y: last.maxY + 6, width: last.maxX - chip.midX - 4, height: 2)
            NSBezierPath(roundedRect: line, xRadius: 1, yRadius: 1).fill()
        }

        for item in items {
            switch item {
            case .chip(let gid, let r):
                guard let g = model.group(gid) else { continue }
                drawChip(g, r)
            case .tab(let id, var r):
                guard let t = model.tab(id) else { continue }
                if let d = drag, d.tab == id { continue }
                if renaming?.tab == id { r = r.insetBy(dx: 0, dy: 0) }
                drawTab(t, r, lifted: false)
            }
        }
        // The dragged tab, on top and following the mouse.
        if let d = drag, let t = model.tab(d.tab), let r = rect(of: d.tab) {
            var moved = r
            moved.origin.x = d.x
            drawTab(t, moved, lifted: true)
        }

        // The + button.
        let plus = NSImage(systemSymbolName: "plus", accessibilityDescription: "New Tab")!
            .withSymbolConfiguration(.init(pointSize: 13, weight: .medium))!
        if hover == -1 {
            NSColor.labelColor.withAlphaComponent(0.08).setFill()
            NSBezierPath(roundedRect: plusRect, xRadius: 7, yRadius: 7).fill()
        }
        draw(symbol: plus, in: plusRect, tint: .secondaryLabelColor)
    }

    private func drawChip(_ g: TabGroup, _ r: NSRect) {
        let color = groupColor(g)
        color.setFill()
        NSBezierPath(roundedRect: r, xRadius: r.height / 2, yRadius: r.height / 2).fill()
        guard r.height > 14 else { return }
        let ink = dark ? NSColor(hex: "#17181c") : .white
        var text = g.name
        if g.collapsed { text += (text.isEmpty ? "" : "  ") + "\(model.tabsIn(g.id).count)" }
        let s = NSAttributedString(string: text, attributes: [.font: Self.chipFont, .foregroundColor: ink])
        let size = s.size()
        s.draw(with: NSRect(x: r.minX + 8, y: r.midY - size.height / 2, width: r.width - 16, height: size.height),
               options: [.usesLineFragmentOrigin, .truncatesLastVisibleLine])
    }

    private func drawTab(_ t: Tab, _ r: NSRect, lifted: Bool) {
        let active = t.id == model.activeId
        let group = t.groupId.flatMap(model.group)
        let path = NSBezierPath(roundedRect: r, xRadius: 9, yRadius: 9)
        if active || lifted {
            NSGraphicsContext.saveGraphicsState()
            let shadow = NSShadow()
            shadow.shadowColor = .black.withAlphaComponent(lifted ? 0.35 : 0.18)
            shadow.shadowBlurRadius = lifted ? 10 : 3
            shadow.shadowOffset = NSSize(width: 0, height: lifted ? -3 : -1)
            shadow.set()
            surface.setFill()
            path.fill()
            NSGraphicsContext.restoreGraphicsState()
            if let group, active {
                groupColor(group).withAlphaComponent(0.14).setFill()
                path.fill()
            }
        } else if hover == t.id {
            NSColor.labelColor.withAlphaComponent(0.07).setFill()
            path.fill()
        }

        // Icon, then the title fading out at the end.
        let pane = model.focusedPane(t)
        let iconColor = active ? (group.map(groupColor) ?? accent) : NSColor.secondaryLabelColor
        let iconRect = NSRect(x: r.minX + 11, y: r.midY - 7, width: 14, height: 14)
        if let img = NSImage(systemSymbolName: Self.icon(pane), accessibilityDescription: nil)?
            .withSymbolConfiguration(.init(pointSize: 11, weight: .medium)) {
            draw(symbol: img, in: iconRect, tint: iconColor)
        }
        // Background dots: a command finished (green/red) or output arrived.
        if !active, t.finished != nil || t.activity {
            let dot = NSRect(x: iconRect.maxX - 3, y: iconRect.minY - 3, width: 6, height: 6)
            chrome.setFill()
            NSBezierPath(ovalIn: dot.insetBy(dx: -2, dy: -2)).fill()
            (t.finished.map { $0 ? ok : danger } ?? accent).setFill()
            NSBezierPath(ovalIn: dot).fill()
        }
        var x = iconRect.maxX + 6
        if broadcasting(t.id), let img = NSImage(systemSymbolName: "dot.radiowaves.left.and.right", accessibilityDescription: "Broadcasting")?
            .withSymbolConfiguration(.init(pointSize: 10, weight: .semibold)) {
            draw(symbol: img, in: NSRect(x: x, y: r.midY - 6, width: 14, height: 12), tint: .systemOrange)
            x += 17
        }
        if recording(t.id) {
            NSColor.systemRed.setFill()
            NSBezierPath(ovalIn: NSRect(x: x, y: r.midY - 3.5, width: 7, height: 7)).fill()
            x += 11
        }
        guard renaming?.tab != t.id else { return }
        let showClose = active || hover == t.id
        let end = r.maxX - (showClose ? 28 : 8)
        let title = model.title(t)
        let color = active ? NSColor.labelColor : NSColor.secondaryLabelColor
        let s = NSAttributedString(string: Redaction.shared.text(title), attributes: [.font: Self.titleFont, .foregroundColor: color])
        let size = s.size()
        let box = NSRect(x: x, y: r.midY - size.height / 2, width: max(0, end - x), height: size.height)
        NSGraphicsContext.saveGraphicsState()
        NSBezierPath(rect: box).addClip()
        s.draw(at: box.origin)
        NSGraphicsContext.restoreGraphicsState()
        // Fade the last 18pt rather than cutting with an ellipsis.
        if size.width > box.width, box.width > 18 {
            let fadeBase = active || lifted ? surface : (hover == t.id ? chrome.blended(withFraction: 0.07, of: .labelColor) ?? chrome : chrome)
            NSGradient(starting: fadeBase.withAlphaComponent(0), ending: fadeBase)?
                .draw(in: NSRect(x: box.maxX - 18, y: box.minY, width: 18, height: box.height), angle: 0)
        }
        if showClose, !lifted {
            let cr = closeRect(r)
            if hoverClose, hover == t.id {
                NSColor.labelColor.withAlphaComponent(0.12).setFill()
                NSBezierPath(roundedRect: cr, xRadius: 5, yRadius: 5).fill()
            }
            if let x = NSImage(systemSymbolName: "xmark", accessibilityDescription: "Close Tab")?
                .withSymbolConfiguration(.init(pointSize: 9, weight: .semibold)) {
                draw(symbol: x, in: cr, tint: .secondaryLabelColor)
            }
        }
    }

    /// An SF Symbol, centred and tinted.
    private func draw(symbol: NSImage, in r: NSRect, tint: NSColor) {
        let img = symbol.copy() as! NSImage
        img.isTemplate = false
        let tinted = NSImage(size: img.size, flipped: false) { rect in
            img.draw(in: rect)
            tint.set()
            rect.fill(using: .sourceAtop)
            return true
        }
        let s = img.size
        tinted.draw(in: NSRect(x: r.midX - s.width / 2, y: r.midY - s.height / 2, width: s.width, height: s.height),
                    from: .zero, operation: .sourceOver, fraction: 1, respectFlipped: true, hints: nil)
    }

    /// The tab icon for what runs in its focused pane.
    static func icon(_ p: Pane?) -> String {
        guard let p, !p.isShell, !p.process.isEmpty else { return "terminal" }
        switch p.process {
        case "vim", "nvim", "vi", "nano", "hx", "emacs", "micro", "helix": return "pencil"
        case "ssh", "mosh", "telnet": return "globe"
        case "top", "htop", "btop", "btm": return "waveform.path.ecg"
        case "git", "lazygit", "tig": return "arrow.triangle.branch"
        case "docker", "kubectl", "k9s", "podman": return "shippingbox"
        default: return "play"
        }
    }

    // MARK: Mouse

    private func item(at p: NSPoint) -> Item? {
        items.first {
            switch $0 {
            case .chip(_, let r), .tab(_, let r): r.insetBy(dx: -2, dy: -4).contains(p)
            }
        }
    }

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        trackingAreas.forEach(removeTrackingArea)
        addTrackingArea(NSTrackingArea(rect: bounds, options: [.mouseMoved, .mouseEnteredAndExited, .activeAlways, .inVisibleRect],
                                       owner: self))
    }

    override func mouseMoved(with event: NSEvent) {
        let p = convert(event.locationInWindow, from: nil)
        var h: Int?
        var onClose = false
        if case .tab(let id, let r) = item(at: p) {
            h = id
            onClose = closeRect(r).contains(p)
        } else if plusRect.contains(p) {
            h = -1
        }
        if h != hover || onClose != hoverClose {
            hover = h
            hoverClose = onClose
            needsDisplay = true
        }
        if case .tab(let id, _) = item(at: p), let t = model.tab(id), let pane = model.focusedPane(t) {
            let tip = pane.oscTitle.isEmpty ? WindowModel.prettyPath(pane.cwd) : Redaction.shared.text(pane.oscTitle)
            if toolTip != tip { toolTip = tip }
        } else if case .chip(let gid, _) = item(at: p), let g = model.group(gid) {
            let n = model.tabsIn(gid).count
            toolTip = "\(g.name.isEmpty ? "Unnamed group" : g.name) — \(n) \(n == 1 ? "tab" : "tabs")"
        } else if plusRect.contains(p) {
            toolTip = "New Tab (⌘T)"
        } else {
            toolTip = nil
        }
    }

    override func mouseExited(with event: NSEvent) {
        hover = nil
        hoverClose = false
        needsDisplay = true
    }

    override func mouseDown(with event: NSEvent) {
        let p = convert(event.locationInWindow, from: nil)
        if renaming != nil { finishRename(save: true) }
        switch item(at: p) {
        case .tab(let id, let r):
            if closeRect(r).contains(p), id == model.activeId || hover == id {
                onClose?(id)
                return
            }
            if event.clickCount == 2 {
                startRename(id)
                return
            }
            onActivate?(id)
            press = (id, p, p.x - r.minX)
        case .chip(let gid, let r):
            onChip?(gid, false, r)
        case nil:
            if plusRect.contains(p) {
                onNewTab?()
            } else if event.clickCount == 2 {
                // As the system's title bar does: zoom or minimise, per the user's setting.
                let action = UserDefaults.standard.string(forKey: "AppleActionOnDoubleClick") ?? "Maximize"
                if action == "Minimize" { window?.miniaturize(nil) } else if action != "None" { window?.zoom(nil) }
            } else {
                window?.performDrag(with: event)
            }
        }
    }

    override func mouseDragged(with event: NSEvent) {
        guard let press else { return }
        let p = convert(event.locationInWindow, from: nil)
        if drag == nil {
            guard abs(p.x - press.at.x) >= 4 || abs(p.y - press.at.y) >= 4 else { return }
            drag = (press.tab, p.x - press.offset, false)
        }
        guard var d = drag else { return }
        let inWindow = window.map { NSPointInRect(event.locationInWindow, $0.contentView!.bounds) } ?? true
        d.tearing = model.tabs.count > 1 && (!inWindow || p.y > bounds.maxY + 56)
        d.x = min(max(leftInset, p.x - press.offset), bounds.width - 60)
        drag = d
        if !d.tearing, let r = rect(of: d.tab) {
            // Reorder live: before the first tab whose centre is right of ours.
            let centre = d.x + r.width / 2
            let target = items.compactMap { item -> (Int, CGFloat)? in
                if case .tab(let id, let rr) = item, id != d.tab { return (id, rr.midX) }
                return nil
            }.first { $0.1 > centre }?.0
            let order = model.tabs.map(\.id)
            let current = order.firstIndex(of: d.tab).map { $0 + 1 < order.count ? order[$0 + 1] : nil } ?? nil
            if target != current {
                model.moveBefore(d.tab, target)
                layoutItems()
            }
        }
        NSCursor.closedHand.set()
        needsDisplay = true
    }

    override func mouseUp(with event: NSEvent) {
        defer {
            press = nil
            drag = nil
            NSCursor.arrow.set()
            refresh()
        }
        if let d = drag, d.tearing {
            let screen = window?.convertPoint(toScreen: event.locationInWindow) ?? NSEvent.mouseLocation
            onTearOff?(d.tab, NSPoint(x: screen.x - (press?.offset ?? 0) - 90, y: screen.y + 20))
        }
    }

    override func otherMouseDown(with event: NSEvent) {
        // Middle-click closes a tab.
        if event.buttonNumber == 2, case .tab(let id, _) = item(at: convert(event.locationInWindow, from: nil)) { onClose?(id) }
    }

    override func rightMouseDown(with event: NSEvent) {
        let p = convert(event.locationInWindow, from: nil)
        switch item(at: p) {
        case .tab(let id, _):
            if let menu = onMenu?(id, event) { NSMenu.popUpContextMenu(menu, with: event, for: self) }
        case .chip(let gid, let r):
            onChip?(gid, true, r)
        case nil:
            break
        }
    }

    // MARK: Rename

    func startRename(_ id: Int) {
        onActivate?(id)
        refresh()
        guard let r = rect(of: id), let t = model.tab(id) else { return }
        renaming?.field.removeFromSuperview()
        let f = NSTextField(string: model.title(t))
        f.font = Self.titleFont
        f.focusRingType = .none
        f.isBordered = false
        f.drawsBackground = true
        f.backgroundColor = surface
        f.wantsLayer = true
        f.layer?.cornerRadius = 7
        f.layer?.borderWidth = 1.5
        f.layer?.borderColor = accent.cgColor
        f.placeholderString = model.focusedPane(t).map { WindowModel.paneTitle($0) }
        f.setAccessibilityLabel("Tab name (empty for automatic)")
        f.delegate = self
        f.frame = NSRect(x: r.minX + 30, y: r.midY - 10, width: r.width - 36, height: 20)
        addSubview(f)
        renaming = (id, f)
        window?.makeFirstResponder(f)
        f.currentEditor()?.selectAll(nil)
        needsDisplay = true
    }

    func finishRename(save: Bool) {
        guard let r = renaming else { return }
        renaming = nil
        if save { model.rename(r.tab, String(r.field.stringValue.prefix(WindowModel.maxTitle))) }
        r.field.removeFromSuperview()
        refresh()
        onRenamed?()
    }

    func control(_ control: NSControl, textView: NSTextView, doCommandBy selector: Selector) -> Bool {
        switch selector {
        case #selector(NSResponder.insertNewline(_:)):
            finishRename(save: true)
            return true
        case #selector(NSResponder.cancelOperation(_:)):
            finishRename(save: false)
            return true
        default:
            return false
        }
    }

    func controlTextDidEndEditing(_ note: Notification) {
        if renaming != nil { finishRename(save: true) }
    }
}

extension NSColor {
    /// "#rrggbb" (alpha optional), sRGB.
    convenience init(hex: String) {
        let c = RGB(hex: hex) ?? RGB(128, 128, 128)
        self.init(srgbRed: c.r / 255, green: c.g / 255, blue: c.b / 255, alpha: c.a)
    }

    convenience init(_ c: RGB) {
        self.init(srgbRed: c.r / 255, green: c.g / 255, blue: c.b / 255, alpha: c.a)
    }
}

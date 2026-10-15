import AppKit
import SwiftTerm
import WtfCore

/// Holds a terminal view with the padding the Wails build has (the left
/// padding is the gutter for command marks), and the overlay drawn over it.
@MainActor
final class TerminalContainer: NSView {
    static let padding = NSEdgeInsets(top: 10, left: 14, bottom: 6, right: 6)

    let terminal: TerminalView
    let overlay: TerminalOverlay
    let tints = TintView()

    init(terminal: TerminalView) {
        self.terminal = terminal
        overlay = TerminalOverlay(terminal: terminal)
        super.init(frame: terminal.frame)
        wantsLayer = true
        terminal.autoresizingMask = []
        overlay.autoresizingMask = [.width, .height]
        tints.overlay = overlay
        addSubview(terminal)
        addSubview(tints)
        addSubview(overlay)
        registerForDraggedTypes([FilesView.pathType])
    }

    // A path dragged from the Files tab: pasted at the prompt. The pane
    // shows it'll take the drop.
    override func draggingEntered(_ sender: NSDraggingInfo) -> NSDragOperation {
        let ok = sender.draggingPasteboard.availableType(from: [FilesView.pathType]) != nil
        dropTarget = ok
        return ok ? .copy : []
    }

    override func draggingExited(_ sender: NSDraggingInfo?) { dropTarget = false }
    override func draggingEnded(_ sender: NSDraggingInfo) { dropTarget = false }

    private var dropTarget = false {
        didSet {
            guard dropTarget != oldValue else { return }
            layer?.sublayers?.filter { $0.name == "drop" }.forEach { $0.removeFromSuperlayer() }
            guard dropTarget else { return }
            let l = CALayer()
            l.name = "drop"
            l.backgroundColor = NSColor.controlAccentColor.withAlphaComponent(0.12).cgColor
            l.borderColor = NSColor.controlAccentColor.cgColor
            l.borderWidth = 2
            l.cornerRadius = 6
            l.frame = bounds.insetBy(dx: 2, dy: 2)
            layer?.addSublayer(l)
        }
    }

    override func performDragOperation(_ sender: NSDraggingInfo) -> Bool {
        dropTarget = false
        guard let text = sender.draggingPasteboard.string(forType: .string),
              let view = terminal as? WtfTerminalView else { return false }
        window?.makeFirstResponder(view)
        view.onFocus?()
        view.pasteText(text + " ")
        return true
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    override func layout() {
        super.layout()
        let p = Self.padding
        terminal.frame = NSRect(x: p.left, y: p.top, width: max(0, bounds.width - p.left - p.right),
                                height: max(0, bounds.height - p.top - p.bottom))
        overlay.frame = bounds
        overlay.needsDisplay = true
        tints.frame = bounds
        tints.needsDisplay = true
        layer?.sublayers?.filter { $0.name == "broadcast" }.forEach { $0.frame = bounds.insetBy(dx: 2, dy: 2) }
    }

    /// The orange outline of a pane that receives broadcast input.
    var outlined = false {
        didSet {
            guard outlined != oldValue else { return }
            if outlined {
                let l = CALayer()
                l.name = "broadcast"
                l.borderColor = NSColor.systemOrange.cgColor
                l.borderWidth = 1.5
                l.cornerRadius = 8
                l.frame = bounds.insetBy(dx: 2, dy: 2)
                layer?.addSublayer(l)
            } else {
                layer?.sublayers?.filter { $0.name == "broadcast" }.forEach { $0.removeFromSuperlayer() }
            }
        }
    }

    /// Clicks in the padding focus the terminal.
    override func mouseDown(with event: NSEvent) { window?.makeFirstResponder(terminal) }
}

/// Draws over a terminal: each command's mark in the left gutter (click it
/// for the command's actions), how long slow commands took, the highlight of
/// the block a menu is about, and covers over secrets while Hide Secrets is
/// on. Positions are in rows and columns of the terminal; the drawing
/// follows scrolling because the owner redraws on every scroll and output.
@MainActor
final class TerminalOverlay: NSView, NSViewToolTipOwner {
    struct Cover: Equatable { let row: Int; let col: Int; let width: Int }
    struct Mark: Equatable { let row: Int; let block: PromptTracker.Block }

    private unowned let terminal: TerminalView
    /// Visible rows (0 at the top of the view).
    // Each change redraws; nothing redraws while nothing changes, and the
    // overlay is hidden while it has nothing to draw, so streaming output
    // costs it nothing.
    var covers: [Cover] = [] { didSet { if covers != oldValue { changed() } } }
    /// Find's matches on screen, and the selected one.
    var found: [Cover] = [] { didSet { if found != oldValue { changed() } } }
    var foundCurrent: Cover? { didSet { if foundCurrent != oldValue { changed() } } }
    var marks: [Mark] = [] {
        didSet {
            guard marks != oldValue else { return }
            updateTooltips()
            changed()
        }
    }
    /// Visible rows of the highlighted block.
    var highlight: ClosedRange<Int>? { didSet { if highlight != oldValue { changed() } } }

    private func changed() {
        isHidden = covers.isEmpty && marks.isEmpty && highlight == nil && found.isEmpty
        needsDisplay = true
    }
    var coverColor = NSColor.gray
    var accent = NSColor.systemBlue
    var dark = true
    var onMarkClick: ((_ id: Int, _ event: NSEvent) -> Void)?

    init(terminal: TerminalView) {
        self.terminal = terminal
        super.init(frame: .zero)
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    /// Width and height of a cell, in points.
    var cell: CGSize {
        let px = terminal.cellSizeInPixels(source: terminal.getTerminal()) ?? (8, 16)
        let scale = window?.backingScaleFactor ?? 2
        return CGSize(width: CGFloat(px.width) / scale, height: CGFloat(px.height) / scale)
    }

    /// A row's rectangle in this view, from column `col` for `width` cells.
    func rect(row: Int, col: Int = 0, width: Int = 1) -> NSRect {
        let c = cell
        let origin = terminal.frame.origin
        return NSRect(x: origin.x + CGFloat(col) * c.width, y: origin.y + CGFloat(row) * c.height,
                      width: CGFloat(width) * c.width, height: c.height)
    }

    private func markRect(_ row: Int) -> NSRect {
        let r = rect(row: row)
        return NSRect(x: terminal.frame.minX - 13, y: r.minY, width: 12, height: r.height)
    }

    override func draw(_ dirty: NSRect) {
        let cols = terminal.getTerminal().cols
        if let h = highlight {
            let r = rect(row: h.lowerBound, width: cols).union(rect(row: h.upperBound, width: cols))
            accent.withAlphaComponent(dark ? 0.16 : 0.12).setFill()
            NSBezierPath(roundedRect: r, xRadius: 4, yRadius: 4).fill()
        }

        let ok = dark ? NSColor(srgbRed: 0x5b / 255, green: 0xb9 / 255, blue: 0x8c / 255, alpha: 0.75)
            : NSColor(srgbRed: 0x2f / 255, green: 0x9e / 255, blue: 0x6a / 255, alpha: 0.75)
        let fail = dark ? NSColor(srgbRed: 0xe5 / 255, green: 0x48 / 255, blue: 0x4d / 255, alpha: 0.95)
            : NSColor(srgbRed: 0xd9 / 255, green: 0x30 / 255, blue: 0x36 / 255, alpha: 0.95)
        let muted = dark ? NSColor(white: 0.92, alpha: 0.45) : NSColor(white: 0.24, alpha: 0.5)
        let font = NSFont.monospacedSystemFont(ofSize: max(9, cell.height * 0.6), weight: .regular)
        for m in marks {
            guard let code = m.block.exitCode else { continue }
            let r = markRect(m.row)
            (code == 0 ? ok : fail).setFill()
            let bar = NSRect(x: r.minX + 4, y: r.minY + r.height * 0.22, width: 3, height: r.height * 0.56)
            NSBezierPath(roundedRect: bar, xRadius: 1.5, yRadius: 1.5).fill()

            // How long a slow command took, at the end of its prompt line, when there's room.
            guard let ms = m.block.durationMs, ms >= PromptTracker.durationLabelMs else { continue }
            let label = Format.duration(ms: ms)
            let width = label.count + 2
            let used = rowText(m.row).count
            guard used + width + 2 <= cols else { continue }
            let at = rect(row: m.row, col: cols - width, width: width)
            let s = NSAttributedString(string: label, attributes: [.font: font, .foregroundColor: muted])
            let size = s.size()
            s.draw(at: NSPoint(x: at.maxX - size.width - cell.width, y: at.midY - size.height / 2))
        }

        // Every match on screen, the selected one outlined.
        accent.withAlphaComponent(dark ? 0.28 : 0.22).setFill()
        for f in found { NSBezierPath(roundedRect: rect(row: f.row, col: f.col, width: f.width), xRadius: 2, yRadius: 2).fill() }
        if let c = foundCurrent {
            accent.setStroke()
            let p = NSBezierPath(roundedRect: rect(row: c.row, col: c.col, width: c.width).insetBy(dx: 0.5, dy: 0.5), xRadius: 2, yRadius: 2)
            p.lineWidth = 1.5
            p.stroke()
        }

        coverColor.setFill()
        for c in covers { rect(row: c.row, col: c.col, width: c.width).fill() }
    }

    private func rowText(_ row: Int) -> String {
        terminal.getTerminal().getLine(row: row)?.translateToString(trimRight: true) ?? ""
    }

    // Only the marks take the mouse; everything else goes to the terminal.
    override func hitTest(_ point: NSPoint) -> NSView? {
        let p = convert(point, from: superview)
        return marks.contains { $0.block.exitCode != nil && markRect($0.row).contains(p) } ? self : nil
    }

    override func mouseDown(with event: NSEvent) {
        let p = convert(event.locationInWindow, from: nil)
        if let m = marks.first(where: { markRect($0.row).contains(p) }) { onMarkClick?(m.block.id, event) }
    }

    override func resetCursorRects() {
        for m in marks where m.block.exitCode != nil { addCursorRect(markRect(m.row), cursor: .pointingHand) }
    }

    /// The mark's tooltip, like the Wails build's.
    func tooltip(at point: NSPoint) -> String? {
        guard let m = marks.first(where: { markRect($0.row).contains(point) }), let code = m.block.exitCode else { return nil }
        var s = code == 0 ? "Succeeded" : "Failed with exit code \(code)"
        if let ms = m.block.durationMs { s += " · took \(Format.duration(ms: ms))" }
        if let at = m.block.finishedAt { s += " · finished \(at.formatted(date: .omitted, time: .standard))" }
        return s + "\nClick for this command's actions"
    }

    func view(_ view: NSView, stringForToolTip tag: NSView.ToolTipTag, point: NSPoint, userData: UnsafeMutableRawPointer?) -> String {
        tooltip(at: point) ?? ""
    }

    private func updateTooltips() {
        removeAllToolTips()
        for m in marks where m.block.exitCode != nil { addToolTip(markRect(m.row), owner: self, userData: nil) }
        window?.invalidateCursorRects(for: self)
    }
}

/// The log colors: runs of cells blended over the text below. On a dark
/// theme the darker of text and tint shows (light text takes the tint, the
/// dark background stays); on a light theme the lighter. The buffer isn't
/// touched, so copying, recordings and a program's own colors are exact.
@MainActor
final class TintView: NSView {
    weak var overlay: TerminalOverlay?
    var runs: [Logs.Tint] = [] {
        didSet {
            guard runs != oldValue else { return }
            isHidden = runs.isEmpty
            needsDisplay = true
        }
    }
    var dark = true {
        didSet {
            guard dark != oldValue || compositingFilter == nil else { return }
            compositingFilter = CIFilter(name: dark ? "CIDarkenBlendMode" : "CILightenBlendMode")
        }
    }

    override init(frame: NSRect) {
        super.init(frame: frame)
        wantsLayer = true
        compositingFilter = CIFilter(name: "CIDarkenBlendMode")
        autoresizingMask = [.width, .height]
        isHidden = true
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }
    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    override func draw(_ dirty: NSRect) {
        guard let overlay else { return }
        for t in runs {
            NSColor(hex: t.color).setFill()
            overlay.rect(row: t.row, col: t.col, width: t.width).fill()
        }
    }
}

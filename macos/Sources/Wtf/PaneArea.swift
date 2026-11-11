import AppKit
import WtfCore

/// Lays out the active tab's panes from its split tree: one terminal per
/// leaf, a divider per split (drag to resize, double-click for half and
/// half), unfocused panes dimmed, and the zoomed pane alone with a pill to
/// say so. Terminals are kept, not re-created, as the
/// layout changes: only their frames move.
@MainActor
final class PaneArea: NSView {
    struct Divider {
        let path: [Bool]
        let dir: SplitDir
        /// The split's whole area, in view coordinates.
        let area: NSRect
        let ratio: Double
        /// Where the line is drawn, and the 9pt band that grabs the mouse.
        var line: NSRect {
            switch dir {
            case .row: NSRect(x: area.minX + area.width * ratio - 0.5, y: area.minY, width: 1, height: area.height)
            case .column: NSRect(x: area.minX, y: area.minY + area.height * ratio - 0.5, width: area.width, height: 1)
            }
        }
        var grab: NSRect { line.insetBy(dx: dir == .row ? -4 : 0, dy: dir == .column ? -4 : 0) }
    }

    /// The panes on screen, by pane id.
    private(set) var shown: [Int: NSView] = [:]
    private var dims: [Int: NSView] = [:]
    private var dividers: [Divider] = []
    private var hot: Int?
    private var dragging: Int?
    private let pill = NSTextField(labelWithString: "Zoomed · ⌘⇧↩ to restore")
    private let pillBox = NSVisualEffectView()

    var layoutTree: Layout?
    var focused: Int?
    var zoomed: Int?
    var dimAmount = 0.35
    var surface = NSColor.black
    var accent = NSColor.controlAccentColor
    var hairline = NSColor.separatorColor
    /// A divider was dragged: the split at `path` gets `ratio`.
    var onRatio: ((_ path: [Bool], _ ratio: Double) -> Void)?

    override var isFlipped: Bool { true }

    override init(frame: NSRect) {
        super.init(frame: frame)
        wantsLayer = true
        pill.font = .systemFont(ofSize: 11.5, weight: .medium)
        pill.textColor = .secondaryLabelColor
        pillBox.material = .hudWindow
        pillBox.state = .active
        pillBox.wantsLayer = true
        pillBox.layer?.cornerRadius = 11
        pill.translatesAutoresizingMaskIntoConstraints = false
        pillBox.addSubview(pill)
        NSLayoutConstraint.activate([
            pill.leadingAnchor.constraint(equalTo: pillBox.leadingAnchor, constant: 12),
            pill.trailingAnchor.constraint(equalTo: pillBox.trailingAnchor, constant: -12),
            pill.centerYAnchor.constraint(equalTo: pillBox.centerYAnchor),
        ])
        pillBox.isHidden = true
        addSubview(pillBox)
    }

    required init?(coder: NSCoder) { fatalError() }

    /// Shows a tab: `views` holds a view for each of its panes.
    func show(_ tree: Layout, focused: Int, zoomed: Int?, views: [Int: NSView]) {
        layoutTree = tree
        self.focused = focused
        self.zoomed = zoomed
        for (id, v) in shown where views[id] !== v { v.removeFromSuperview(); dims[id]?.removeFromSuperview(); dims[id] = nil }
        for (id, v) in views where v.superview !== self {
            v.removeFromSuperview()
            addSubview(v, positioned: .below, relativeTo: pillBox)
            shown[id] = v
        }
        shown = views
        needsLayout = true
        needsDisplay = true
    }

    override func layout() {
        super.layout()
        guard let tree = layoutTree else { return }
        let frames = tree.frames()
        let split = tree.isSplit && zoomed == nil
        for (id, v) in shown {
            if let z = zoomed {
                v.isHidden = id != z
                v.frame = bounds
            } else if let f = frames[id] {
                v.isHidden = false
                // Whole points, so neighbouring panes don't blur or overlap.
                v.frame = NSRect(x: (f.x * bounds.width).rounded(), y: (f.y * bounds.height).rounded(),
                                 width: (f.w * bounds.width).rounded(), height: (f.h * bounds.height).rounded())
            }
            // Unfocused panes of a split sit behind a veil of the background colour.
            let wantDim = split && id != focused && dimAmount > 0
            if wantDim {
                let d = dims[id] ?? {
                    let d = DimView()
                    d.wantsLayer = true
                    dims[id] = d
                    return d
                }()
                if d.superview !== self { addSubview(d, positioned: .above, relativeTo: v) }
                d.frame = v.frame
                d.layer?.backgroundColor = surface.withAlphaComponent(min(0.8, dimAmount)).cgColor
            } else if let d = dims.removeValue(forKey: id) {
                d.removeFromSuperview()
            }
        }
        dividers = split ? tree.splits().map {
            Divider(path: $0.path, dir: $0.dir,
                    area: NSRect(x: $0.rect.x * bounds.width, y: $0.rect.y * bounds.height,
                                 width: $0.rect.w * bounds.width, height: $0.rect.h * bounds.height),
                    ratio: $0.ratio)
        } : []
        pillBox.isHidden = zoomed == nil
        let size = pill.intrinsicContentSize
        pillBox.frame = NSRect(x: (bounds.width - size.width - 24) / 2, y: bounds.height - 34, width: size.width + 24, height: 22)
        window?.invalidateCursorRects(for: self)
        needsDisplay = true
    }

    override func draw(_ dirty: NSRect) {
        for (i, d) in dividers.enumerated() {
            if i == hot || i == dragging {
                accent.setFill()
                d.line.insetBy(dx: d.dir == .row ? -0.5 : 0, dy: d.dir == .column ? -0.5 : 0).fill()
            } else {
                hairline.setFill()
                d.line.fill()
            }
        }
    }

    // Dividers sit between panes: only the bands take the mouse.
    override func hitTest(_ point: NSPoint) -> NSView? {
        let p = convert(point, from: superview)
        if dividers.contains(where: { $0.grab.contains(p) }) { return self }
        return super.hitTest(point)
    }

    override func resetCursorRects() {
        for d in dividers { addCursorRect(d.grab, cursor: d.dir == .row ? .resizeLeftRight : .resizeUpDown) }
    }

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        trackingAreas.forEach(removeTrackingArea)
        addTrackingArea(NSTrackingArea(rect: bounds, options: [.mouseMoved, .mouseEnteredAndExited, .activeInKeyWindow, .inVisibleRect],
                                       owner: self))
    }

    override func mouseMoved(with event: NSEvent) {
        let p = convert(event.locationInWindow, from: nil)
        let h = dividers.firstIndex { $0.grab.contains(p) }
        if h != hot { hot = h; needsDisplay = true }
    }

    override func mouseExited(with event: NSEvent) {
        if hot != nil, dragging == nil { hot = nil; needsDisplay = true }
    }

    override func mouseDown(with event: NSEvent) {
        let p = convert(event.locationInWindow, from: nil)
        guard let i = dividers.firstIndex(where: { $0.grab.contains(p) }) else { return }
        if event.clickCount == 2 {
            onRatio?(dividers[i].path, 0.5)
            return
        }
        dragging = i
        needsDisplay = true
    }

    override func mouseDragged(with event: NSEvent) {
        guard let i = dragging, i < dividers.count else { return }
        let d = dividers[i]
        let p = convert(event.locationInWindow, from: nil)
        let r = d.dir == .row ? (p.x - d.area.minX) / max(1, d.area.width) : (p.y - d.area.minY) / max(1, d.area.height)
        onRatio?(d.path, Layout.clamp(r))
    }

    override func mouseUp(with event: NSEvent) {
        dragging = nil
        needsDisplay = true
    }
}

/// The veil over an unfocused pane: clicks go through to the terminal.
private final class DimView: NSView {
    override func hitTest(_ point: NSPoint) -> NSView? { nil }
}

import AppKit
import WtfCore

/// The side pane's lists: sections of rows with an icon, a title, a detail
/// line, pills and an optional trailing button; single and multiple
/// selection, double-click, right-click menus, and drops. Built on a view-
/// based table so long lists stay cheap.
@MainActor
final class SideList: NSScrollView, NSTableViewDataSource, NSTableViewDelegate {
    struct Pill {
        var text: String
        var color: NSColor = .secondaryLabelColor
        var tip: String? = nil
    }

    struct Row {
        var id: String
        var symbol: String
        var tint: NSColor = .controlAccentColor
        var title: String
        var mono = false
        var detail = ""
        /// A second, red line (an error).
        var alert = ""
        /// The alert line's colour (green for a check that passed).
        var alertColor: NSColor = .systemRed
        var pills: [Pill] = []
        var tip: String? = nil
        /// A dot instead of an icon (tunnel and resource states).
        var dot: NSColor? = nil
        var highlighted = false
        var button: (symbol: String, tip: String, run: () -> Void)? = nil
        /// Right-aligned number columns (pid, CPU, memory), left to right.
        var columns: [Column] = []
        /// Tree depth: indents the row and shows an elbow.
        var depth = 0
        /// A dimmed title (shells in the process tree).
        var muted = false
    }

    struct Column {
        var text: String
        var width: CGFloat
        var color: NSColor = .secondaryLabelColor
        var bold = false
    }

    struct Section {
        var title: String?
        var tip: String? = nil
        var rows: [Row]
        /// A collapsible section: its heading toggles it, and shows a count.
        var toggle: (() -> Void)? = nil
        var open = true
        var badge = ""
        /// A small group label above the heading (Workloads, Network…).
        var group: String? = nil
        /// A note shown in place of rows (empty states).
        var empty: String? = nil
        /// A plain heading that runs this when clicked (Go to tab).
        var action: (() -> Void)? = nil
        /// A heading shown even with no rows.
        var always = false
    }

    private enum Item {
        case header(String, String?, (() -> Void)?)
        case group(String)
        case heading(title: String, tip: String?, open: Bool, badge: String, toggle: () -> Void)
        case note(String, red: Bool)
        case row(Row)
    }

    private let table = SideTable()
    private var items: [Item] = []
    var onDoubleClick: ((String) -> Void)?
    var onReturn: (([String]) -> Void)?
    var onEscape: (() -> Void)?
    var menuProvider: (([String]) -> NSMenu?)?
    var onSelection: (([String]) -> Void)?
    /// Accepts drops of paths from the Files tab onto a row.
    var onDrop: ((_ row: String, _ path: String) -> Void)? {
        didSet { if onDrop != nil { table.registerForDraggedTypes([FilesView.pathType]) } }
    }
    var allowsMultiple: Bool {
        get { table.allowsMultipleSelection }
        set { table.allowsMultipleSelection = newValue }
    }

    override init(frame: NSRect) {
        super.init(frame: frame)
        let col = NSTableColumn(identifier: .init("c"))
        table.addTableColumn(col)
        table.headerView = nil
        table.style = .plain
        table.backgroundColor = .clear
        table.intercellSpacing = NSSize(width: 0, height: 0)
        table.usesAutomaticRowHeights = false
        table.dataSource = self
        table.delegate = self
        table.target = self
        table.doubleAction = #selector(doubled)
        table.action = #selector(clicked)
        table.list = self
        documentView = table
        drawsBackground = false
        hasVerticalScroller = true
        autohidesScrollers = true
    }

    required init?(coder: NSCoder) { fatalError() }

    func set(_ sections: [Section]) {
        let selected = Set(selectedIds)
        items = []
        for s in sections {
            if let g = s.group { items.append(.group(g)) }
            if let toggle = s.toggle {
                items.append(.heading(title: s.title ?? "", tip: s.tip, open: s.open, badge: s.badge, toggle: toggle))
                guard s.open else { continue }
            } else if let t = s.title, !s.rows.isEmpty || s.always {
                items.append(.header(t, s.tip, s.action))
            }
            items += s.rows.map { .row($0) }
            if s.rows.isEmpty, let e = s.empty { items.append(.note(e, red: false)) }
        }
        table.reloadData()
        let keep = IndexSet(items.indices.filter { if case .row(let r) = items[$0] { selected.contains(r.id) } else { false } })
        table.selectRowIndexes(keep, byExtendingSelection: false)
        table.tableColumns.first?.width = contentSize.width
    }

    var selectedIds: [String] {
        table.selectedRowIndexes.compactMap { i in
            guard i < items.count, case .row(let r) = items[i] else { return nil }
            return r.id
        }
    }

    func clearSelection() { table.deselectAll(nil) }
    func focus() { window?.makeFirstResponder(table) }

    // MARK: Table

    func numberOfRows(in tableView: NSTableView) -> Int { items.count }

    /// Notes wrap to the list's width.
    private var noteWidth: CGFloat { max(80, contentSize.width - 38) }

    private static func noteHeight(_ text: String, width: CGFloat) -> CGFloat {
        let l = NSTextField(wrappingLabelWithString: text)
        l.font = .systemFont(ofSize: 11.5)
        let h = l.cell?.cellSize(forBounds: NSRect(x: 0, y: 0, width: width, height: 10_000)).height ?? 16
        return max(22, ceil(h) + 6)
    }

    override func setFrameSize(_ newSize: NSSize) {
        let changed = newSize.width != frame.width
        super.setFrameSize(newSize)
        if changed, items.contains(where: { if case .note = $0 { true } else { false } }) {
            table.tableColumns.first?.width = contentSize.width
            table.reloadData()
        }
    }

    func tableView(_ tableView: NSTableView, heightOfRow row: Int) -> CGFloat {
        switch items[row] {
        case .header, .group: return 24
        case .heading: return 26
        case .note(let t, _): return Self.noteHeight(t, width: noteWidth)
        case .row(let r): return r.detail.isEmpty && r.alert.isEmpty ? 26 : (r.alert.isEmpty ? 38 : 52)
        }
    }

    // Headings are ordinary rows that can't be selected: group rows would
    // float and draw a bar behind them.
    func tableView(_ tableView: NSTableView, isGroupRow row: Int) -> Bool { false }

    func tableView(_ tableView: NSTableView, shouldSelectRow row: Int) -> Bool { if case .row = items[row] { true } else { false } }

    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        switch items[row] {
        case .header(let t, let tip, let action):
            let l = NSTextField(labelWithString: action == nil ? t.uppercased() : t)
            l.font = action == nil ? .systemFont(ofSize: 10.5, weight: .bold) : .systemFont(ofSize: 11.5, weight: .semibold)
            l.textColor = action == nil ? .secondaryLabelColor : .controlAccentColor
            l.toolTip = tip
            let v = NSView()
            l.frame = NSRect(x: 8, y: 4, width: 280, height: 16)
            l.autoresizingMask = [.width]
            v.addSubview(l)
            return v
        case .group(let g):
            let l = NSTextField(labelWithString: g)
            l.font = .systemFont(ofSize: 10.5, weight: .semibold)
            l.textColor = .tertiaryLabelColor
            let v = NSView()
            l.frame = NSRect(x: 8, y: 4, width: 280, height: 16)
            v.addSubview(l)
            return v
        case .heading(let title, let tip, let open, let badge, let toggle):
            let b = HeadingButton(title: title, open: open, badge: badge, toggle: toggle)
            b.toolTip = tip
            return b
        case .note(let t, let red):
            let l = NSTextField(wrappingLabelWithString: t)
            l.font = .systemFont(ofSize: 11.5)
            l.textColor = red ? .systemRed : .secondaryLabelColor
            let v = NSView()
            l.frame = NSRect(x: 30, y: 2, width: noteWidth, height: Self.noteHeight(t, width: noteWidth) - 4)
            l.autoresizingMask = [.width]
            v.addSubview(l)
            return v
        case .row(let r):
            let cell = tableView.makeView(withIdentifier: .init("row"), owner: self) as? SideRowView ?? SideRowView()
            cell.identifier = .init("row")
            cell.set(r)
            return cell
        }
    }

    func tableViewSelectionDidChange(_ note: Notification) { onSelection?(selectedIds) }

    /// A single click on a row (deployments select their pods).
    var onClick: ((String) -> Void)?

    /// The table takes clicks to track selection and doesn't pass them to
    /// the views in its rows, so headings toggle from here.
    @objc private func clicked() {
        let i = table.clickedRow
        guard i >= 0, i < items.count else { return }
        switch items[i] {
        case .heading(_, _, _, _, let toggle): toggle()
        case .header(_, _, let action?): action()
        case .row(let r): onClick?(r.id)
        default: break
        }
    }

    /// Clicks a heading by title, as the mouse would (debug self-checks).
    func clickHeading(_ title: String) -> Bool {
        guard let i = items.firstIndex(where: { if case .heading(let t, _, _, _, _) = $0 { t == title } else { false } }),
              let cell = table.view(atColumn: 0, row: i, makeIfNecessary: true) else { return false }
        let p = cell.convert(NSPoint(x: 20, y: cell.bounds.midY), to: nil)
        guard let w = window else { return false }
        for type in [NSEvent.EventType.leftMouseDown, .leftMouseUp] {
            if let e = NSEvent.mouseEvent(with: type, location: p, modifierFlags: [], timestamp: ProcessInfo.processInfo.systemUptime,
                                          windowNumber: w.windowNumber, context: nil, eventNumber: 0, clickCount: 1, pressure: 1) {
                w.sendEvent(e)
            }
        }
        return true
    }

    @objc private func doubled() {
        let i = table.clickedRow
        guard i >= 0, i < items.count, case .row(let r) = items[i] else { return }
        onDoubleClick?(r.id)
    }

    fileprivate func rowId(at point: NSPoint) -> String? {
        let i = table.row(at: point)
        guard i >= 0, i < items.count, case .row(let r) = items[i] else { return nil }
        return r.id
    }

    fileprivate func menuFor(event: NSEvent) -> NSMenu? {
        let p = table.convert(event.locationInWindow, from: nil)
        let i = table.row(at: p)
        guard i >= 0, case .row = items[i] else { return nil }
        if !table.selectedRowIndexes.contains(i) { table.selectRowIndexes([i], byExtendingSelection: false) }
        return menuProvider?(selectedIds)
    }

    fileprivate func key(_ code: UInt16) -> Bool {
        switch code {
        case 36, 76:
            if !selectedIds.isEmpty { onReturn?(selectedIds); return true }
        case 53:
            clearSelection()
            onEscape?()
            return true
        default: break
        }
        return false
    }

    // MARK: Drops (paths from the Files tab)

    func tableView(_ tableView: NSTableView, validateDrop info: NSDraggingInfo, proposedRow row: Int,
                   proposedDropOperation op: NSTableView.DropOperation) -> NSDragOperation {
        guard onDrop != nil, row < items.count, case .row = items[row] else { return [] }
        tableView.setDropRow(row, dropOperation: .on)
        return .copy
    }

    func tableView(_ tableView: NSTableView, acceptDrop info: NSDraggingInfo, row: Int, dropOperation: NSTableView.DropOperation) -> Bool {
        guard row < items.count, case .row(let r) = items[row],
              let path = info.draggingPasteboard.string(forType: FilesView.pathType) else { return false }
        onDrop?(r.id, path)
        return true
    }
}

@MainActor
private final class SideTable: NSTableView {
    weak var list: SideList?

    override func menu(for event: NSEvent) -> NSMenu? { list?.menuFor(event: event) }

    override func keyDown(with event: NSEvent) {
        if list?.key(event.keyCode) != true { super.keyDown(with: event) }
    }
}

@MainActor
private final class SideRowView: NSTableCellView {
    private let icon = NSImageView()
    private let dot = NSView()
    private let title = NSTextField(labelWithString: "")
    private let detail = NSTextField(labelWithString: "")
    private let alert = NSTextField(labelWithString: "")
    private let pills = NSStackView()
    private var button: ToolButton?
    private var columns: [NSTextField] = []
    private var columnWidths: [CGFloat] = []
    private var depth = 0
    private let elbow = NSTextField(labelWithString: "└")

    init() {
        super.init(frame: .zero)
        icon.symbolConfiguration = .init(pointSize: 12, weight: .regular)
        dot.wantsLayer = true
        dot.layer?.cornerRadius = 3.5
        title.lineBreakMode = .byTruncatingTail
        detail.font = .systemFont(ofSize: 11)
        detail.textColor = .secondaryLabelColor
        detail.lineBreakMode = .byTruncatingTail
        alert.font = .systemFont(ofSize: 11)
        alert.textColor = .systemRed
        alert.lineBreakMode = .byTruncatingTail
        pills.spacing = 4
        elbow.font = .systemFont(ofSize: 11)
        elbow.textColor = .tertiaryLabelColor
        [icon, dot, title, detail, alert, pills, elbow].forEach(addSubview)
    }

    required init?(coder: NSCoder) { fatalError() }

    func set(_ r: SideList.Row) {
        icon.image = NSImage(systemSymbolName: r.symbol, accessibilityDescription: nil)
        icon.contentTintColor = r.tint
        icon.isHidden = r.dot != nil
        dot.isHidden = r.dot == nil
        dot.layer?.backgroundColor = r.dot?.cgColor
        title.font = r.mono ? .monospacedSystemFont(ofSize: 12, weight: .regular) : .systemFont(ofSize: 12.5)
        title.stringValue = r.title
        title.textColor = r.muted ? .secondaryLabelColor : .labelColor
        depth = r.depth
        elbow.isHidden = r.depth == 0
        columns.forEach { $0.removeFromSuperview() }
        columns = r.columns.map { c in
            let l = NSTextField(labelWithString: c.text)
            l.font = .monospacedDigitSystemFont(ofSize: 11.5, weight: c.bold ? .semibold : .regular)
            l.textColor = c.color
            l.alignment = .right
            addSubview(l)
            return l
        }
        columnWidths = r.columns.map(\.width)
        detail.stringValue = r.detail
        detail.isHidden = r.detail.isEmpty
        alert.stringValue = r.alert
        alert.textColor = r.alertColor
        alert.isHidden = r.alert.isEmpty
        pills.arrangedSubviews.forEach { $0.removeFromSuperview() }
        for p in r.pills {
            let pill = Pill(bg: p.color.withAlphaComponent(0.14), radius: 5, inset: 5)
            let l = NSTextField(labelWithString: p.text)
            l.font = .systemFont(ofSize: 10.5, weight: .medium)
            l.textColor = p.color
            pill.set([l])
            pill.toolTip = p.tip
            pills.addArrangedSubview(pill)
        }
        button?.removeFromSuperview()
        button = nil
        if let b = r.button {
            let tb = ToolButton(symbol: b.symbol, tip: b.tip, action: b.run)
            addSubview(tb)
            button = tb
        }
        toolTip = r.tip
        wantsLayer = true
        layer?.backgroundColor = r.highlighted ? NSColor.controlAccentColor.withAlphaComponent(0.12).cgColor : nil
        layer?.cornerRadius = 6
        needsLayout = true
    }

    override func layout() {
        super.layout()
        let h = bounds.height
        let lines = [title, detail, alert].filter { !$0.isHidden }
        let lineH: CGFloat = 16
        var y = (h - CGFloat(lines.count) * lineH) / 2
        let indent = CGFloat(depth) * 14
        elbow.frame = NSRect(x: 8 + indent - 12, y: (h - 16) / 2, width: 12, height: 16)
        icon.frame = NSRect(x: 8 + indent, y: (h - 16) / 2, width: 16, height: 16)
        dot.frame = NSRect(x: 12 + indent, y: (h - 7) / 2, width: 7, height: 7)
        var trailing: CGFloat = button == nil ? 6 : 30
        for (l, w) in zip(columns, columnWidths).reversed() {
            l.frame = NSRect(x: bounds.width - trailing - w, y: (h - 16) / 2, width: w, height: 16)
            trailing += w + 4
        }
        let pillsW = pills.fittingSize.width
        button?.frame = NSRect(x: bounds.width - 28, y: (h - 24) / 2, width: 24, height: 24)
        pills.frame = NSRect(x: bounds.width - trailing - pillsW, y: (h - 16) / 2, width: pillsW, height: 16)
        let textX = (icon.image == nil && dot.isHidden ? 10 : 30) + indent
        let textW = max(0, bounds.width - textX - 2 - trailing - pillsW - 6)
        // The table is flipped; rows lay out top-down.
        for l in lines {
            l.frame = NSRect(x: textX, y: y, width: textW, height: lineH)
            y += lineH
        }
    }

    override var isFlipped: Bool { true }
}

/// A collapsible section's heading: chevron, title and count.
@MainActor
private final class HeadingButton: NSView {
    private let toggle: () -> Void

    init(title: String, open: Bool, badge: String, toggle: @escaping () -> Void) {
        self.toggle = toggle
        super.init(frame: .zero)
        let chevron = NSImageView(image: NSImage(systemSymbolName: open ? "chevron.down" : "chevron.right", accessibilityDescription: nil)!)
        chevron.symbolConfiguration = .init(pointSize: 9, weight: .semibold)
        chevron.contentTintColor = .secondaryLabelColor
        let l = NSTextField(labelWithString: title)
        l.font = .systemFont(ofSize: 12.5, weight: .medium)
        let b = NSTextField(labelWithString: badge)
        b.font = .monospacedDigitSystemFont(ofSize: 11, weight: .medium)
        b.textColor = .secondaryLabelColor
        let row = NSStackView(views: [chevron, l, b])
        row.spacing = 6
        row.frame = NSRect(x: 8, y: 3, width: 280, height: 20)
        row.autoresizingMask = [.width]
        addSubview(row)
    }

    required init?(coder: NSCoder) { fatalError() }

    // Clicks go to the table, which runs the toggle (SideList.clicked).
    override func hitTest(_ point: NSPoint) -> NSView? { nil }
}

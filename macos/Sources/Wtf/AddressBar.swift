import AppKit
import WtfCore

/// Resolving each pane's context from what its shell reported: off the main
/// thread, newest request per pane wins, a failure keeps the last answer.
/// Like resolveContext in lib/infra.ts.
@MainActor
enum AddressBarModel {
    private static var seq = 0
    private static var latest: [ObjectIdentifier: Int] = [:]

    /// The pane's shell reported its variables (at a prompt).
    static func changed(pane: Pane, in w: WindowController) {
        guard let env = pane.env, Settings.shared.config?.contextBadges ?? true else { return }
        seq += 1
        let mine = seq, key = ObjectIdentifier(pane)
        latest[key] = mine
        Task { [weak w] in
            let ctx = try? await Backend.call("infra", "Resolve", [env], as: InfraContext.self)
            guard latest[key] == mine else { return }
            latest[key] = nil
            guard let ctx else { return }
            if pane.infra != ctx {
                pane.infra = ctx
                w?.paneContextChanged()
            }
        }
    }
}

/// The bar under the tab strip: what the focused pane runs, where, its
/// context badges and how the last command went. Clicking it never takes the
/// keyboard from the terminal. Like AddressBar.svelte.
@MainActor
final class AddressBar: NSView {
    private let omnibox = NSView()
    private let stack = NSStackView()
    private let end = NSStackView()
    private var clockTimer: Timer?
    private var current: (pane: Pane, group: TabGroup?)?
    var onShowSecrets: (() -> Void)?
    var dark = true
    var accent = NSColor.controlAccentColor
    var ok = NSColor.systemGreen
    var danger = NSColor.systemRed
    var omniboxColor = NSColor.labelColor.withAlphaComponent(0.06) {
        didSet { omnibox.layer?.backgroundColor = omniboxColor.cgColor }
    }
    var identityColor = NSColor.labelColor.withAlphaComponent(0.08)

    override var isFlipped: Bool { true }
    override var mouseDownCanMoveWindow: Bool { false }
    override var fittingSize: NSSize { NSSize(width: 0, height: 40) }

    override init(frame: NSRect) {
        super.init(frame: frame)
        wantsLayer = true
        omnibox.wantsLayer = true
        omnibox.layer?.cornerRadius = 8
        addSubview(omnibox)
        stack.orientation = .horizontal
        stack.spacing = 10
        stack.alignment = .centerY
        stack.edgeInsets = NSEdgeInsets(top: 0, left: 8, bottom: 0, right: 6)
        omnibox.addSubview(stack)
        end.spacing = 6
        end.alignment = .centerY
    }

    required init?(coder: NSCoder) { fatalError() }

    override func layout() {
        super.layout()
        omnibox.frame = NSRect(x: 8, y: 6, width: bounds.width - 16, height: 28)
        stack.frame = omnibox.bounds
    }

    // Clicks never move the keyboard here.
    override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }

    func show(pane: Pane?, group: TabGroup?) {
        current = pane.map { ($0, group) }
        rebuild()
    }

    func rebuild() {
        stack.arrangedSubviews.forEach { $0.removeFromSuperview() }
        end.arrangedSubviews.forEach { $0.removeFromSuperview() }
        clockTimer?.invalidate()
        clockTimer = nil
        guard let (pane, group) = current else {
            addEnd()
            return
        }
        omnibox.toolTip = pane.cwd

        if let group {
            let color = NSColor(hex: group.color.hex(dark: dark))
            stack.addArrangedSubview(chip(text: group.name.isEmpty ? "Group" : group.name, dot: color, weight: .semibold, bg: nil, fg: .labelColor))
        }
        let busy = !pane.isShell
        stack.addArrangedSubview(chip(symbol: TabStripView.icon(pane), text: pane.process.isEmpty ? "shell" : pane.process,
                                      weight: .semibold, bg: busy ? accent.withAlphaComponent(0.16) : identityColor,
                                      fg: busy ? accent : .labelColor))
        let path = crumbs(WindowModel.prettyPath(pane.cwd))
        path.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        stack.addArrangedSubview(path)

        let spacer = NSView()
        spacer.setContentHuggingPriority(.init(1), for: .horizontal)
        spacer.setContentCompressionResistancePriority(.init(1), for: .horizontal)
        stack.addArrangedSubview(spacer)

        if Settings.shared.config?.contextBadges ?? true, let ctx = pane.infra {
            let badges = Badges.of(ctx)
            for b in badges { stack.addArrangedSubview(badge(b)) }
            // The clocks move on their own: refresh every 15s while any is shown.
            if !(ctx.expiries ?? []).isEmpty {
                clockTimer = Timer.scheduledTimer(withTimeInterval: 15, repeats: true) { [weak self] _ in
                    MainActor.assumeIsolated { self?.rebuild() }
                }
            }
        }
        if let status = statusChip(pane) { stack.addArrangedSubview(status) }
        addEnd()
    }

    private func addEnd() {
        if Redaction.shared.on {
            let b = PillButton(symbol: "checkmark.shield", text: "Secrets hidden", fg: ok, bg: ok.withAlphaComponent(0.16))
            b.toolTip = "Secrets in the output are covered, and replaced with [REDACTED] when copied. Click to show them (⌥⌘R)"
            b.onClick = { [weak self] in self?.onShowSecrets?() }
            end.addArrangedSubview(b)
        }
        if !end.arrangedSubviews.isEmpty { stack.addArrangedSubview(end) }
    }

    // MARK: Pieces

    private func crumbs(_ path: String) -> NSView {
        var parts = path.split(separator: "/").map(String.init)
        if path.hasPrefix("/") { parts.insert("/", at: 0) }
        let s = NSMutableAttributedString()
        let muted: [NSAttributedString.Key: Any] = [.font: NSFont.systemFont(ofSize: 12.5), .foregroundColor: NSColor.secondaryLabelColor]
        for (i, p) in parts.enumerated() {
            if i > 0, parts[i - 1] != "/" {
                s.append(NSAttributedString(string: " › ", attributes: [.font: NSFont.systemFont(ofSize: 11),
                                                                         .foregroundColor: NSColor.secondaryLabelColor.withAlphaComponent(0.45)]))
            } else if i > 0 {
                s.append(NSAttributedString(string: " ", attributes: muted))
            }
            let last = i == parts.count - 1
            s.append(NSAttributedString(string: p, attributes: last
                ? [.font: NSFont.systemFont(ofSize: 12.5, weight: .medium), .foregroundColor: NSColor.labelColor] : muted))
        }
        let f = NSTextField(labelWithAttributedString: s)
        f.lineBreakMode = .byClipping
        f.cell?.truncatesLastVisibleLine = false
        return f
    }

    private func chip(symbol: String? = nil, text: String, dot: NSColor? = nil, weight: NSFont.Weight, bg: NSColor?, fg: NSColor) -> NSView {
        let v = Pill(bg: bg)
        var views: [NSView] = []
        if let dot { views.append(Dot(color: dot)) }
        if let symbol, let img = NSImage(systemSymbolName: symbol, accessibilityDescription: nil) {
            let i = NSImageView(image: img)
            i.symbolConfiguration = .init(pointSize: 10, weight: .semibold)
            i.contentTintColor = fg
            views.append(i)
        }
        let l = NSTextField(labelWithString: text)
        l.font = .systemFont(ofSize: 11.5, weight: weight)
        l.textColor = fg
        l.lineBreakMode = .byTruncatingTail
        views.append(l)
        v.set(views)
        v.setContentCompressionResistancePriority(.required, for: .horizontal)
        return v
    }

    private func badge(_ b: Badge) -> NSView {
        let v = Pill(bg: identityColor)
        let kind = NSTextField(labelWithString: b.kindLabel)
        kind.font = .systemFont(ofSize: 10, weight: .bold)
        kind.textColor = .secondaryLabelColor
        let value = NSTextField(labelWithString: b.label)
        value.font = .systemFont(ofSize: 11.5, weight: .medium)
        value.lineBreakMode = .byTruncatingTail
        value.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        var views: [NSView] = [kind, value]
        if let clock = b.clock {
            let (text, color, tint): (String, NSColor, NSColor) = switch clock {
            case .ok(let l): (l, .secondaryLabelColor, NSColor.secondaryLabelColor.withAlphaComponent(0.14))
            case .soon(let l): (l, .labelColor, NSColor.systemYellow.withAlphaComponent(0.26))
            case .expired: ("expired", danger, danger.withAlphaComponent(0.18))
            }
            let c = Pill(bg: tint, radius: 4, inset: 4)
            let icon = NSImageView(image: NSImage(systemSymbolName: "clock", accessibilityDescription: nil)!)
            icon.symbolConfiguration = .init(pointSize: 9, weight: .semibold)
            icon.contentTintColor = color
            let l = NSTextField(labelWithString: text)
            l.font = .monospacedDigitSystemFont(ofSize: 10.5, weight: .semibold)
            l.textColor = color
            c.set([icon, l])
            views.append(c)
        }
        v.set(views)
        v.toolTip = b.title
        v.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        return v
    }

    private func statusChip(_ p: Pane) -> NSView? {
        if p.running, !p.command.isEmpty {
            let v = Pill(bg: accent.withAlphaComponent(0.16))
            let dot = Dot(color: accent, size: 6)
            dot.pulseThreeTimes()
            let l = NSTextField(labelWithString: Redaction.shared.text(p.command))
            l.font = .monospacedSystemFont(ofSize: 11.5, weight: .regular)
            l.textColor = accent
            l.lineBreakMode = .byTruncatingTail
            l.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
            v.set([dot, l])
            v.toolTip = Redaction.shared.text(p.command)
            v.widthAnchor.constraint(lessThanOrEqualTo: omnibox.widthAnchor, multiplier: 0.4).isActive = true
            return v
        }
        guard let last = p.lastCommand else { return nil }
        let dur = Format.duration(ms: last.durationMs)
        let cmd = Redaction.shared.text(last.text)
        if last.exitCode == 0 {
            let v = chip(symbol: "checkmark", text: dur, weight: .medium, bg: identityColor, fg: ok)
            v.toolTip = "\(cmd) — succeeded"
            return v
        }
        let v = chip(symbol: "xmark", text: "exit \(last.exitCode)  \(dur)", weight: .medium, bg: danger.withAlphaComponent(0.14), fg: danger)
        v.toolTip = "\(cmd) — exit code \(last.exitCode)"
        return v
    }
}

/// A 20pt rounded pill holding a row of views.
@MainActor
final class Pill: NSView {
    private let row = NSStackView()

    init(bg: NSColor?, radius: CGFloat = 6, inset: CGFloat = 7) {
        super.init(frame: .zero)
        wantsLayer = true
        layer?.cornerRadius = radius
        layer?.backgroundColor = bg?.cgColor
        row.spacing = 5
        row.alignment = .centerY
        row.edgeInsets = NSEdgeInsets(top: 0, left: inset, bottom: 0, right: inset)
        row.translatesAutoresizingMaskIntoConstraints = false
        addSubview(row)
        NSLayoutConstraint.activate([
            row.leadingAnchor.constraint(equalTo: leadingAnchor),
            row.trailingAnchor.constraint(equalTo: trailingAnchor),
            row.topAnchor.constraint(equalTo: topAnchor),
            row.bottomAnchor.constraint(equalTo: bottomAnchor),
            heightAnchor.constraint(equalToConstant: inset == 7 ? 20 : 16),
        ])
    }

    required init?(coder: NSCoder) { fatalError() }

    func set(_ views: [NSView]) { views.forEach(row.addArrangedSubview) }
}

/// A pill that acts on a click without taking keyboard focus.
@MainActor
final class PillButton: NSView {
    var onClick: (() -> Void)?

    init(symbol: String, text: String, fg: NSColor, bg: NSColor) {
        super.init(frame: .zero)
        let pill = Pill(bg: bg)
        let i = NSImageView(image: NSImage(systemSymbolName: symbol, accessibilityDescription: nil)!)
        i.symbolConfiguration = .init(pointSize: 10, weight: .semibold)
        i.contentTintColor = fg
        let l = NSTextField(labelWithString: text)
        l.font = .systemFont(ofSize: 11.5, weight: .semibold)
        l.textColor = fg
        pill.set([i, l])
        pill.translatesAutoresizingMaskIntoConstraints = false
        addSubview(pill)
        NSLayoutConstraint.activate([
            pill.leadingAnchor.constraint(equalTo: leadingAnchor), pill.trailingAnchor.constraint(equalTo: trailingAnchor),
            pill.topAnchor.constraint(equalTo: topAnchor), pill.bottomAnchor.constraint(equalTo: bottomAnchor),
        ])
        setAccessibilityRole(.button)
        setAccessibilityLabel(text)
    }

    required init?(coder: NSCoder) { fatalError() }

    override func hitTest(_ point: NSPoint) -> NSView? { frame.contains(point) ? self : nil }
    override func mouseDown(with event: NSEvent) {}
    override func mouseUp(with event: NSEvent) { onClick?() }
    override func resetCursorRects() { addCursorRect(bounds, cursor: .pointingHand) }
}

/// A coloured dot.
@MainActor
final class Dot: NSView {
    init(color: NSColor, size: CGFloat = 8) {
        super.init(frame: NSRect(x: 0, y: 0, width: size, height: size))
        wantsLayer = true
        layer?.backgroundColor = color.cgColor
        layer?.cornerRadius = size / 2
        widthAnchor.constraint(equalToConstant: size).isActive = true
        heightAnchor.constraint(equalToConstant: size).isActive = true
    }

    required init?(coder: NSCoder) { fatalError() }

    /// Pulses three times, then holds still (no endless animations).
    func pulseThreeTimes() {
        let a = CABasicAnimation(keyPath: "opacity")
        a.fromValue = 1
        a.toValue = 0.25
        a.duration = 0.7
        a.autoreverses = true
        a.repeatCount = 3
        a.timingFunction = CAMediaTimingFunction(name: .easeInEaseOut)
        layer?.add(a, forKey: "pulse")
    }
}

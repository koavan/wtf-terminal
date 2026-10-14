import AppKit
import WtfCore

/// wtf's CPU and memory, sampled every 2s only while a readout or the
/// dialog is on screen and a wtf window is in front. Like usage.svelte.ts.
@MainActor
final class UsageStore {
    static let shared = UsageStore()

    var current: Go.usage.Usage?
    var error: String?
    private var watchers = Set<ObjectIdentifier>()
    private var poller: Poller?
    private var busy = false
    private var observers = Observers()

    func observe<T: AnyObject>(_ owner: T, _ fn: @escaping (T) -> Void) { observers.add(owner, fn) }

    func watch(_ who: AnyObject) {
        watchers.insert(ObjectIdentifier(who))
        if poller == nil { poller = Poller(window: nil, interval: 2) { UsageStore.shared.sample() } }
    }

    func unwatch(_ who: AnyObject) {
        watchers.remove(ObjectIdentifier(who))
        if watchers.isEmpty {
            poller?.stop()
            poller = nil
        }
    }

    private func sample() {
        guard !busy else { return }
        busy = true
        Task {
            do {
                current = try await Services.usage.Sample()
                error = nil
            } catch {
                self.error = error.localizedDescription
            }
            busy = false
            observers.notify()
        }
    }

    /// The palette's toggle: the readout in every address bar.
    static func toggle(in w: WindowController) {
        let on = !(Settings.shared.config?.showUsage ?? false)
        Task {
            do {
                try await Services.config.SetShowUsage(on)
                Toast.show(on ? "wtf’s CPU and memory show in the address bar" : "Resource usage hidden", in: w.rootView)
            } catch {
                Toast.show("Couldn't save the setting: \(error.localizedDescription)", in: w.rootView)
            }
        }
    }
}

/// The address bar's "CPU · memory" readout; click for the breakdown.
@MainActor
final class UsageReadout: NSView {
    private let label = NSTextField(labelWithString: "…")
    private let icon = NSImageView(image: NSImage(systemSymbolName: "waveform.path.ecg", accessibilityDescription: nil)!)
    var onClick: (() -> Void)?
    private var watching = false

    override init(frame: NSRect) {
        super.init(frame: frame)
        icon.symbolConfiguration = .init(pointSize: 10, weight: .medium)
        icon.contentTintColor = .secondaryLabelColor
        label.font = .monospacedDigitSystemFont(ofSize: 11.5, weight: .regular)
        label.textColor = .secondaryLabelColor
        let row = NSStackView(views: [icon, label])
        row.spacing = 4
        row.translatesAutoresizingMaskIntoConstraints = false
        addSubview(row)
        NSLayoutConstraint.activate([
            row.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 4), row.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -4),
            row.topAnchor.constraint(equalTo: topAnchor), row.bottomAnchor.constraint(equalTo: bottomAnchor),
            heightAnchor.constraint(equalToConstant: 20),
        ])
        toolTip = "wtf's CPU (% of one core) and memory: the app and its windows, not the programs you run in it. Click for the breakdown."
        setAccessibilityRole(.button)
        setAccessibilityLabel("Resource usage")
        UsageStore.shared.observe(self) { $0.update() }
        update()
    }

    required init?(coder: NSCoder) { fatalError() }

    /// Samples only while shown.
    func setWatching(_ on: Bool) {
        guard on != watching else { return }
        watching = on
        on ? UsageStore.shared.watch(self) : UsageStore.shared.unwatch(self)
    }

    private func update() {
        guard let u = UsageStore.shared.current else { return }
        let t = Usage.total(u)
        label.stringValue = "\(Usage.cpu(t.cpu)) · \(Usage.bytes(t.memory))"
    }

    override func hitTest(_ point: NSPoint) -> NSView? { frame.contains(point) ? self : nil }
    override func mouseDown(with event: NSEvent) {}
    override func mouseUp(with event: NSEvent) { onClick?() }
    override func resetCursorRects() { addCursorRect(bounds, cursor: .pointingHand) }
}

/// The Resource Usage dialog: wtf in all, then the app and your programs,
/// process by process, live while open. Like UsageDialog.svelte.
@MainActor
final class UsageDialog: NSObject {
    private static var shown: UsageDialog?
    private let sheet: FormSheet
    private let body = NSStackView()
    private let subtitle: NSTextField

    static func show(in w: WindowController) {
        guard shown == nil else { return }
        let d = UsageDialog()
        shown = d
        d.sheet.present(in: w.window)
    }

    private override init() {
        sheet = FormSheet(title: "Resource Usage", width: 560)
        subtitle = sheet.note("")
        super.init()
        sheet.add(subtitle)
        body.orientation = .vertical
        body.alignment = .leading
        body.spacing = 2
        body.widthAnchor.constraint(equalToConstant: 512).isActive = true
        sheet.add(body)
        let show = NSButton(checkboxWithTitle: "Show in the address bar", target: self, action: #selector(toggled(_:)))
        show.state = Settings.shared.config?.showUsage ?? false ? .on : .off
        sheet.add(show)
        sheet.buttons([], cancel: "Done")
        sheet.onClose = { [weak self] in
            guard let self else { return }
            UsageStore.shared.unwatch(self)
            Self.shown = nil
        }
        UsageStore.shared.observe(self) { $0.update() }
        UsageStore.shared.watch(self)
        update()
    }

    @objc private func toggled(_ b: NSButton) {
        let on = b.state == .on
        Task {
            do { try await Services.config.SetShowUsage(on) } catch {
                if let v = WindowManager.shared.keyWindow?.rootView { Toast.show("Couldn't save the setting: \(error.localizedDescription)", in: v) }
            }
        }
    }

    private func update() {
        let store = UsageStore.shared
        let u = store.current
        let cores = u.map { $0.cores } ?? 0
        subtitle.stringValue = "CPU is a percentage of one core (\(cores > 0 ? "\(cores)" : "?") cores = \(cores * 100)%), over the last 2 seconds. "
            + (u?.memoryKind == "rss" ? "Memory is resident memory (RSS); pages shared between processes count in each." : "Memory is the footprint Activity Monitor shows.")
        body.arrangedSubviews.forEach { $0.removeFromSuperview() }
        if let e = store.error {
            let l = NSTextField(wrappingLabelWithString: e)
            l.textColor = .systemRed
            body.addArrangedSubview(l)
        }
        guard let u else {
            body.addArrangedSubview(sheet.note("Measuring…"))
            return
        }
        let t = Usage.total(u)
        body.addArrangedSubview(line("wtf in all", Usage.cpu(t.cpu), Usage.bytes(t.memory), size: 13, weight: .bold, highlight: true))
        let measured = u.interval > 0
        var groups: [(String, String, Go.usage.Group)] = [
            ("wtf", "The app — its windows, terminals and backend — and what it runs itself (tunnels, git and kubectl checks)", u.app),
        ]
        // The native app draws its own windows; a group only shows if something landed there.
        if !u.window.procs.isEmpty { groups.append(("Windows", "Helper processes that draw wtf’s windows", u.window)) }
        groups.append(("Your programs", "Shells and what runs in them — not counted in wtf’s total", u.programs))
        for (title, note, g) in groups {
            let spacer = NSView()
            spacer.heightAnchor.constraint(equalToConstant: 8).isActive = true
            body.addArrangedSubview(spacer)
            let head = line(title, Usage.cpu(measured ? g.cpu : -1), Usage.bytes(g.memory), size: 12.5, weight: .semibold)
            head.toolTip = note
            body.addArrangedSubview(head)
            let n = NSTextField(labelWithString: note)
            n.font = .systemFont(ofSize: 11)
            n.textColor = .secondaryLabelColor
            n.lineBreakMode = .byTruncatingTail
            n.widthAnchor.constraint(lessThanOrEqualToConstant: 512).isActive = true
            body.addArrangedSubview(n)
            let procs = g.procs
            if procs.isEmpty {
                let none = NSTextField(labelWithString: "None")
                none.textColor = .secondaryLabelColor
                none.font = .systemFont(ofSize: 12)
                body.addArrangedSubview(indent(none))
            }
            for p in procs {
                let r = line("\(p.name)  \(p.pid)", Usage.cpu(p.cpu), Usage.bytes(p.memory), size: 12, weight: .regular, mono: true, inset: 18)
                r.toolTip = "\(p.name) (pid \(p.pid))"
                body.addArrangedSubview(r)
            }
        }
    }

    private func indent(_ v: NSView) -> NSView {
        let s = NSStackView(views: [v])
        s.edgeInsets = NSEdgeInsets(top: 0, left: 18, bottom: 0, right: 0)
        return s
    }

    /// A row of the grid: name | CPU (64pt) | memory (76pt), numbers right-aligned.
    private func line(_ name: String, _ cpu: String, _ mem: String, size: CGFloat, weight: NSFont.Weight,
                      highlight: Bool = false, mono: Bool = false, inset: CGFloat = 6) -> NSView {
        let n = NSTextField(labelWithString: name)
        n.font = mono ? .monospacedSystemFont(ofSize: size - 0.5, weight: weight) : .systemFont(ofSize: size, weight: weight)
        n.lineBreakMode = .byTruncatingTail
        n.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        let c = NSTextField(labelWithString: cpu), m = NSTextField(labelWithString: mem)
        for (l, w) in [(c, 64.0), (m, 76.0)] {
            l.font = .monospacedDigitSystemFont(ofSize: size, weight: weight)
            l.alignment = .right
            l.widthAnchor.constraint(equalToConstant: w).isActive = true
        }
        let spacer = NSView()
        spacer.setContentHuggingPriority(.init(1), for: .horizontal)
        let row = NSStackView(views: [n, spacer, c, m])
        row.edgeInsets = NSEdgeInsets(top: 3, left: inset, bottom: 3, right: 6)
        row.translatesAutoresizingMaskIntoConstraints = false
        row.widthAnchor.constraint(equalToConstant: 512).isActive = true
        if highlight {
            row.wantsLayer = true
            row.layer?.cornerRadius = 6
            row.layer?.backgroundColor = NSColor.labelColor.withAlphaComponent(0.06).cgColor
        }
        return row
    }
}

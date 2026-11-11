import AppKit
import WtfCore

/// The config from the backend, the system appearance and the text size,
/// for every terminal.
@MainActor
final class Settings {
    static let shared = Settings()

    static let minFontSize = 6.0
    static let maxFontSize = 72.0

    private(set) var resolved: ResolvedConfig?
    /// The size in use: config.toml's, until changed with Bigger/Smaller Text.
    private(set) var fontSize = 13.0
    private var observers = Observers()
    private var appearance: NSKeyValueObservation?

    var config: Config? { resolved?.config }
    var isDark: Bool { NSApp.effectiveAppearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua }
    var theme: Theme? { previewing ?? resolved?.theme(dark: isDark) }
    /// A theme shown while browsing themes in the palette.
    private var previewing: Theme?

    func preview(_ t: Theme?) {
        guard t != previewing else { return }
        previewing = t
        changed()
    }

    /// Loads the config, then follows its changes and the system appearance.
    func start() async throws {
        apply(try await Backend.call("config", "Current", as: ResolvedConfig.self), initial: true)
        Backend.on("config:changed", ResolvedConfig.self) { [weak self] in self?.apply($0, initial: false) }
        appearance = NSApp.observe(\.effectiveAppearance) { _, _ in
            Task { @MainActor in Settings.shared.changed() }
        }
    }

    private func apply(_ r: ResolvedConfig, initial: Bool) {
        let sizeChanged = !initial && r.config.fontSize != resolved?.config.fontSize
        if initial {
            Redaction.shared.on = r.config.redactSecrets
        } else if r.config.redactSecrets, resolved?.config.redactSecrets == false {
            Redaction.shared.on = true
        }
        resolved = r
        // A saved theme replaces the one previewed.
        previewing = nil
        if initial || sizeChanged { fontSize = r.config.fontSize }
        // Problems show once each time they change: at launch too (once a window is up).
        if r.problems != shownProblems {
            shownProblems = r.problems
            if !r.problems.isEmpty { showProblems() }
        }
        changed()
    }

    private var shownProblems: [String] = []

    /// "Settings: <first problem> (+N more)" in the front window, now or when it opens.
    func showProblems() {
        guard !shownProblems.isEmpty else { return }
        guard let w = WindowManager.shared.keyWindow ?? WindowManager.shared.windows.first else { return }
        let p = shownProblems
        Toast.show("Settings: \(p[0])" + (p.count > 1 ? " (+\(p.count - 1) more)" : ""), in: w.rootView, seconds: 6)
    }

    /// Runs whenever the config, theme or text size changes, while `owner` lives.
    func observe<T: AnyObject>(_ owner: T, _ fn: @escaping (T) -> Void) { observers.add(owner, fn) }

    private func changed() { observers.notify() }

    func setFontSize(_ size: Double) {
        guard size.isFinite else { return }
        fontSize = min(Self.maxFontSize, max(Self.minFontSize, size.rounded()))
        changed()
    }

    func biggerText() { setFontSize(fontSize + 1) }
    func smallerText() { setFontSize(fontSize - 1) }
    func actualSize() { setFontSize(config?.fontSize ?? 13) }
}

/// Hide Secrets, for sharing your screen: while on, secrets in the output are
/// covered on screen, and text that leaves a terminal has them replaced with
/// [REDACTED]. The output itself is untouched. One switch for every terminal;
/// `redact-secrets` in config.toml sets it at startup.
@MainActor
final class Redaction {
    static let shared = Redaction()
    private var observers = Observers()

    var on = false {
        didSet { if on != oldValue { observers.notify() } }
    }

    func observe<T: AnyObject>(_ owner: T, _ fn: @escaping (T) -> Void) { observers.add(owner, fn) }

    /// The text as it may be shown or copied now.
    func text(_ s: String) -> String { on && !s.isEmpty ? Secrets.redact(s) : s }
}

/// Callbacks that end with their owner: a closed pane stops listening.
@MainActor
struct Observers {
    private var list: [(owner: () -> AnyObject?, fn: (AnyObject) -> Void)] = []

    mutating func add<T: AnyObject>(_ owner: T, _ fn: @escaping (T) -> Void) {
        list.append(({ [weak owner] in owner }, { fn($0 as! T) }))
    }

    mutating func notify() {
        list.removeAll { $0.owner() == nil }
        for o in list { if let owner = o.owner() { o.fn(owner) } }
    }
}

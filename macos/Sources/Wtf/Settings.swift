import AppKit
import WtfCore

/// The config from the backend, the system appearance and the text size,
/// for every terminal. Like lib/settings.svelte.ts.
@MainActor
final class Settings {
    static let shared = Settings()

    static let minFontSize = 6.0
    static let maxFontSize = 72.0

    private(set) var resolved: ResolvedConfig?
    /// The size in use: config.toml's, until changed with Bigger/Smaller Text.
    private(set) var fontSize = 13.0
    private var observers: [() -> Void] = []
    private var appearance: NSKeyValueObservation?

    var config: Config? { resolved?.config }
    var isDark: Bool { NSApp.effectiveAppearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua }
    var theme: Theme? { resolved?.theme(dark: isDark) }

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
        if initial || sizeChanged { fontSize = r.config.fontSize }
        changed()
    }

    /// Runs whenever the config, theme or text size changes.
    func observe(_ fn: @escaping () -> Void) { observers.append(fn) }

    private func changed() { observers.forEach { $0() } }

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
/// `redact-secrets` in config.toml sets it at startup. Like
/// lib/redaction.svelte.ts.
@MainActor
final class Redaction {
    static let shared = Redaction()
    private var observers: [() -> Void] = []

    var on = false {
        didSet { if on != oldValue { observers.forEach { $0() } } }
    }

    func observe(_ fn: @escaping () -> Void) { observers.append(fn) }

    /// The text as it may be shown or copied now.
    func text(_ s: String) -> String { on && !s.isEmpty ? Secrets.redact(s) : s }
}

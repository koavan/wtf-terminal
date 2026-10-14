import AppKit
import WtfCore

/// Runs `refresh` now, then every `interval` seconds while the window is in
/// front (key, visible, app active), and again as soon as it comes back to
/// the front. An interval of 0 loads once. Like lib/poller.ts.
@MainActor
final class Poller {
    private var timer: Timer?
    private var focus: NSObjectProtocol?
    private weak var window: NSWindow?
    private let refresh: () -> Void
    private(set) var interval: TimeInterval

    /// The side pane's interval: the setting while auto-refresh is on, else 0.
    static var sidebarInterval: TimeInterval {
        let c = Settings.shared.config
        return c?.sidebarAutoRefresh ?? true ? max(1, c?.sidebarRefreshInterval ?? 3) : 0
    }

    /// `window` nil: any wtf window in front counts (shared samplers).
    init(window: NSWindow?, interval: TimeInterval, _ refresh: @escaping () -> Void) {
        self.window = window
        self.interval = interval
        self.refresh = refresh
        start()
    }

    private var inFront: Bool {
        guard NSApp.isActive, let w = window ?? NSApp.keyWindow else { return false }
        return w.isKeyWindow && w.occlusionState.contains(.visible) && !w.isMiniaturized
    }

    private func start() {
        refresh()
        guard interval > 0 else { return }
        timer = Timer.scheduledTimer(withTimeInterval: interval, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated { self?.tick() }
        }
        timer?.tolerance = interval * 0.1
        focus = NotificationCenter.default.addObserver(forName: NSWindow.didBecomeKeyNotification, object: window, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.tick() }
        }
    }

    private func tick() { if inFront { refresh() } }

    func stop() {
        timer?.invalidate()
        timer = nil
        if let focus { NotificationCenter.default.removeObserver(focus) }
        focus = nil
    }

    /// Restarts with a new interval (refreshing now) only if it changed.
    func update(interval new: TimeInterval) {
        guard new != interval else { return }
        stop()
        interval = new
        start()
    }

    /// "Paused · updated 3:04:05 PM", for the note under a paused tab's header.
    static func pausedText(_ updated: Date?) -> String {
        guard let updated else { return "Paused" }
        let f = DateFormatter()
        f.dateFormat = DateFormatter.dateFormat(fromTemplate: "jmmss", options: 0, locale: .current)
        return "Paused · updated \(f.string(from: updated))"
    }
}

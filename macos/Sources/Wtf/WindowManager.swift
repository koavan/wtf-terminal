import AppKit
import WtfCore

/// The app's windows: reopening the saved ones at launch, naming new ones
/// ("main", then w2, w3, …), routing backend events to the window a shell
/// belongs to, and asking before quitting ends running programs. What
/// internal/windows does for the Wails build.
@MainActor
final class WindowManager {
    static let shared = WindowManager()

    private(set) var windows: [WindowController] = []
    /// Window names in the session file, not to be reused for new windows.
    private var savedNames = Set<String>()
    /// Quitting was confirmed: windows close without asking and keep their sessions.
    private(set) var quitting = false

    struct SavedWindow: Decodable {
        let name: String
        let session: SavedSession?
    }

    /// Opens the saved windows, or one fresh window.
    func start() async {
        Backend.on("terminal:info", TerminalInfo.self) { [weak self] info in
            self?.windows.first { $0.owns(session: info.id) }?.apply(info)
        }
        Backend.on("terminal:exit", TerminalExit.self) { [weak self] e in
            self?.windows.first { $0.owns(session: e.id) }?.sessionExited(e.id)
        }
        Backend.on("infra:changed", String.self) { [weak self] _ in self?.windows.forEach { $0.refreshContexts() } }
        Settings.shared.observe(self) { m in
            // Badges turned on or off.
            let on = Settings.shared.config?.contextBadges ?? true
            if on != m.badgesOn {
                m.badgesOn = on
                m.windows.forEach { $0.refreshContexts() }
            }
        }
        badgesOn = Settings.shared.config?.contextBadges ?? true

        let saved = (try? await Backend.call("session", "Windows", as: [SavedWindow].self)) ?? []
        savedNames = Set(saved.map(\.name))
        if let size = saved.first?.session?.fontSize { Settings.shared.setFontSize(size) }
        if saved.isEmpty {
            open(name: "main", session: nil)
        } else {
            for w in saved { open(name: w.name, session: w.session) }
        }
        // Problems in config.toml found at launch.
        Settings.shared.showProblems()
        // Once ever: which tools the side pane needs aren't installed.
        if ProcessInfo.processInfo.environment["WTF_NATIVE_DEBUG"] == nil { keyWindow?.toolsFirstRun() ?? windows.first?.toolsFirstRun() }
    }

    private var badgesOn = true

    @discardableResult
    private func open(name: String, session: SavedSession?, tab: (SavedTab.Payload, [String: TerminalController])? = nil,
                      plan: Plan? = nil, at point: NSPoint? = nil) -> WindowController {
        let w = WindowController(name: name)
        windows.append(w)
        w.bootstrap(session: session, tab: tab.map { (payload: $0.0, terminals: $0.1) }, plan: plan)
        if let point, let win = w.window {
            win.setFrameTopLeftPoint(point)
        } else if let last = windows.dropLast().last?.window {
            w.window?.setFrameTopLeftPoint(w.window!.cascadeTopLeft(from: NSPoint(x: last.frame.minX, y: last.frame.maxY)))
        } else {
            w.window?.center()
        }
        w.showWindow(nil)
        w.window?.makeKeyAndOrderFront(nil)
        return w
    }

    /// The lowest w<i> (i ≥ 2) not open and not in the session file.
    private func newName() -> String {
        let taken = Set(windows.map(\.name)).union(savedNames)
        var i = 2
        while taken.contains("w\(i)") { i += 1 }
        return "w\(i)"
    }

    /// ⌘N.
    func newWindow() { open(name: newName(), session: nil) }

    /// A tab torn off, or moved to a new window.
    func open(tab: (SavedTab.Payload, [String: TerminalController]), at point: NSPoint?) {
        open(name: newName(), session: nil, tab: tab, at: point)
    }

    /// A workspace opened in a new window.
    func open(plan: Plan) {
        let w = open(name: newName(), session: nil, plan: plan)
        _ = w
    }

    func closed(_ w: WindowController) {
        windows.removeAll { $0 === w }
        savedNames.remove(w.name)
    }

    // MARK: Quitting

    struct Running: Decodable {
        let window: String
        let process: String
        let cwd: String
    }

    /// ⌘Q: asks first when programs are running in any window.
    func shouldTerminate() -> NSApplication.TerminateReply {
        if quitting { return .terminateNow }
        Task {
            let running = (try? await Backend.call("terminal", "RunningProcesses", as: [Running].self)) ?? []
            if running.isEmpty {
                confirmQuit()
            } else {
                askToQuit(running)
            }
        }
        return .terminateCancel
    }

    private func askToQuit(_ running: [Running]) {
        let target = NSApp.keyWindow ?? windows.first?.window
        let alert = NSAlert()
        alert.messageText = "Quit wtf?"
        let n = running.count
        alert.informativeText = (n == 1 ? "A program is still running. Quitting will end it." : "\(n) programs are still running. Quitting will end them.")
            + "\n\n" + running.prefix(6).map { "\($0.process) — \(WindowModel.prettyPath($0.cwd))" }.joined(separator: "\n")
            + (n > 6 ? "\nand \(n - 6) more" : "")
        alert.addButton(withTitle: "Quit")
        alert.addButton(withTitle: "Cancel")
        alert.buttons[0].hasDestructiveAction = true
        let done: (NSApplication.ModalResponse) -> Void = { [weak self] r in
            if r == .alertFirstButtonReturn { self?.confirmQuit() }
        }
        if let target { alert.beginSheetModal(for: target, completionHandler: done) } else { done(alert.runModal()) }
    }

    private func confirmQuit() {
        quitting = true
        // Sessions are kept: every window comes back next launch.
        windows.forEach { $0.quitting() }
        NSApp.terminate(nil)
    }

    var keyWindow: WindowController? {
        windows.first { $0.window?.isKeyWindow == true } ?? windows.first { $0.window?.isMainWindow == true }
    }
}

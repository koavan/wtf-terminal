// wtf, native macOS UI. M1: one window, one terminal, on the full Go
// backend (cmd/libwtf).
//
// Environment:
//   WTF_NATIVE_RENDERER=metal   use SwiftTerm's Metal renderer (default CoreText)
//   WTF_NATIVE_RUN="cmd"        typed into the shell once it starts (benchmarks)
//   WTF_NATIVE_DEBUG=1          log title changes
import AppKit

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate {
    private let windowName = "main"
    private var window: NSWindow!
    private var term: TerminalController!

    func applicationDidFinishLaunching(_ note: Notification) {
        do {
            let (state, cache) = try Self.profileDirs()
            let info = Bundle.main.infoDictionary ?? [:]
            try Backend.start(
                version: info["CFBundleShortVersionString"] as? String ?? "dev",
                commit: info["WtfCommit"] as? String ?? "",
                buildDate: info["WtfBuildDate"] as? String ?? "",
                stateDir: state, cacheDir: cache)
        } catch {
            fail("Couldn't start the backend", error)
            return
        }

        let frame = NSRect(x: 0, y: 0, width: 1100, height: 700)
        window = NSWindow(contentRect: frame, styleMask: [.titled, .closable, .miniaturizable, .resizable],
                          backing: .buffered, defer: false)
        window.title = "wtf"
        window.minSize = NSSize(width: 480, height: 300)
        window.delegate = self
        term = TerminalController(window: windowName, frame: frame)
        let debug = ProcessInfo.processInfo.environment["WTF_NATIVE_DEBUG"] != nil
        term.onTitle = { [weak self] title in
            if debug { NSLog("title: \(title)") }
            self?.window.title = title
        }
        term.onExit = { _ in NSApp.terminate(nil) }
        window.contentView = term.view
        window.center()
        window.makeKeyAndOrderFront(nil)
        window.makeFirstResponder(term.view)
        NSApp.activate()

        Task {
            do {
                try await term.start()
                let about = try await Backend.call("about", "Info", as: AboutInfo.self)
                window.subtitle = "\(about.version) · \(about.shell)\(about.shellIntegration ? " · integrated" : "")"
                NSLog("wtf \(about.version) (\(about.commit)) on \(about.os) \(about.arch), state \(about.stateDir)")
                if let cmd = ProcessInfo.processInfo.environment["WTF_NATIVE_RUN"], !cmd.isEmpty {
                    try await Task.sleep(for: .seconds(1.5))
                    term.type(cmd + "\r")
                }
            } catch {
                fail("Couldn't start a shell", error)
            }
        }
    }

    /// Until the native app replaces the Wails one (M7), it keeps state and
    /// caches apart from it, so the two can run side by side without
    /// clearing each other's files or starting the same tunnels.
    private static func profileDirs() throws -> (state: String, cache: String) {
        let fm = FileManager.default
        let support = try fm.url(for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
        let caches = try fm.url(for: .cachesDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
        let state = support.appendingPathComponent("wtf-native", isDirectory: true)
        let cache = caches.appendingPathComponent("wtf-native", isDirectory: true)
        try fm.createDirectory(at: state, withIntermediateDirectories: true)
        try fm.createDirectory(at: cache, withIntermediateDirectories: true)
        return (state.path, cache.path)
    }

    private func fail(_ what: String, _ error: Error) {
        let alert = NSAlert()
        alert.messageText = what
        alert.informativeText = error.localizedDescription
        alert.runModal()
        NSApp.terminate(nil)
    }

    func windowDidBecomeKey(_ note: Notification) { Backend.setFocused(window: windowName, focused: true) }
    func windowDidResignKey(_ note: Notification) { Backend.setFocused(window: windowName, focused: false) }

    func applicationShouldTerminateAfterLastWindowClosed(_ app: NSApplication) -> Bool { true }
    func applicationWillTerminate(_ note: Notification) { Backend.shutdown() }
}

@MainActor
func buildMenu() -> NSMenu {
    let menu = NSMenu()
    let appItem = NSMenuItem()
    appItem.submenu = NSMenu()
    appItem.submenu!.addItem(withTitle: "Quit wtf", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
    menu.addItem(appItem)
    let editItem = NSMenuItem()
    editItem.submenu = NSMenu(title: "Edit")
    editItem.submenu!.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
    editItem.submenu!.addItem(withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
    editItem.submenu!.addItem(withTitle: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
    menu.addItem(editItem)
    return menu
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.setActivationPolicy(.regular)
app.mainMenu = buildMenu()
app.run()

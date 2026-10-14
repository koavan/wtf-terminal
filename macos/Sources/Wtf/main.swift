// wtf, native macOS UI: one window, one terminal (tabs and splits come in
// M3), on the full Go backend (cmd/libwtf).
//
// Environment:
//   WTF_NATIVE_RENDERER=metal   use SwiftTerm's Metal renderer (default CoreText)
//   WTF_NATIVE_RUN="cmd"        typed into the shell once it starts (benchmarks)
//   WTF_NATIVE_DEBUG=1          log titles, commands and secret covers
//                               (=secrets also turns Hide Secrets on)
import AppKit
import SwiftTerm

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

        Task {
            do {
                try await Settings.shared.start()
            } catch {
                fail("Couldn't load the config", error)
                return
            }
            if ProcessInfo.processInfo.environment["WTF_NATIVE_DEBUG"] == "secrets" { Redaction.shared.on = true }
            openWindow()
        }
    }

    private func openWindow() {
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
        window.contentView = term.container
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
                if ProcessInfo.processInfo.environment["WTF_NATIVE_DEBUG"] == "paste" {
                    // Pastes two lines from a private pasteboard through the
                    // responder chain, as ⌘V does, then quits.
                    Clipboard.board = NSPasteboard(name: .init("wtf.debug"))
                    Clipboard.copy("echo one\necho two\n")
                    try await Task.sleep(for: .seconds(1))
                    NSLog("first responder: \(String(describing: window.firstResponder))")
                    NSLog("pasteboard holds: \(String(describing: Clipboard.text))")
                    // A ⌘V key press, through the menu's key equivalent like a real one.
                    NSLog("key window: \(String(describing: NSApp.keyWindow)), active \(NSApp.isActive)")
                    window.makeKey()
                    let v = NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: .command, timestamp: 0,
                                             windowNumber: window.windowNumber, context: nil, characters: "v",
                                             charactersIgnoringModifiers: "v", isARepeat: false, keyCode: 9)!
                    NSApp.sendEvent(v)
                    NSLog("menu paste item enabled: \(String(describing: NSApp.mainMenu?.item(at: 1)?.submenu?.item(withTitle: "Paste")?.isEnabled))")
                    try await Task.sleep(for: .seconds(1))
                    NSLog("sheet: \(String(describing: window.attachedSheet))")
                    // Cancel it, or the open sheet would keep the app from quitting.
                    if let sheet = window.attachedSheet { window.endSheet(sheet, returnCode: .cancel) }
                    NSApp.terminate(nil)
                }
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
    func submenu(_ title: String, _ items: [NSMenuItem]) {
        let top = NSMenuItem()
        top.submenu = NSMenu(title: title)
        items.forEach(top.submenu!.addItem)
        menu.addItem(top)
    }
    func item(_ title: String, _ action: Selector, _ key: String = "", _ mods: NSEvent.ModifierFlags = .command, tag: Int = 0) -> NSMenuItem {
        let i = NSMenuItem(title: title, action: action, keyEquivalent: key)
        i.keyEquivalentModifierMask = mods
        i.tag = tag
        return i
    }
    let find = #selector(WtfTerminalView.wtfFind(_:))
    submenu("wtf", [item("Quit wtf", #selector(NSApplication.terminate(_:)), "q")])
    // Shortcuts as in the Wails build's menu (main.go).
    submenu("Edit", [
        item("Copy", #selector(NSText.copy(_:)), "c"),
        item("Paste", #selector(NSText.paste(_:)), "v"),
        item("Select All", #selector(NSText.selectAll(_:)), "a"),
        .separator(),
        item("Find…", find, "f", tag: Int(NSFindPanelAction.showFindPanel.rawValue)),
        item("Find Next", find, "g", tag: Int(NSFindPanelAction.next.rawValue)),
        item("Find Previous", find, "g", [.command, .shift], tag: Int(NSFindPanelAction.previous.rawValue)),
        .separator(),
        item("Copy Last Command Output", #selector(WtfTerminalView.copyLastOutput(_:)), "c", [.command, .option]),
        item("Clear Buffer", #selector(WtfTerminalView.clearBuffer(_:)), "k"),
    ])
    submenu("View", [
        item("Hide Secrets", #selector(WtfTerminalView.toggleSecrets(_:)), "r", [.command, .option]),
        .separator(),
        item("Previous Prompt", #selector(WtfTerminalView.previousPrompt(_:)), String(UnicodeScalar(NSUpArrowFunctionKey)!)),
        item("Next Prompt", #selector(WtfTerminalView.nextPrompt(_:)), String(UnicodeScalar(NSDownArrowFunctionKey)!)),
        .separator(),
        item("Bigger Text", #selector(WtfTerminalView.biggerText(_:)), "="),
        item("Smaller Text", #selector(WtfTerminalView.smallerText(_:)), "-"),
        item("Actual Size", #selector(WtfTerminalView.actualSize(_:)), "0"),
    ])
    return menu
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.setActivationPolicy(.regular)
app.mainMenu = buildMenu()
app.run()

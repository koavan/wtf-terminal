// wtf, native macOS UI: windows of tabs and split panes on the Go backend
// (cmd/libwtf).
//
// Environment:
//   WTF_NATIVE_RENDERER=metal   use SwiftTerm's Metal renderer (default CoreText)
//   WTF_NATIVE_RUN="cmd"        typed into the first shell once it starts (benchmarks)
//   WTF_NATIVE_DEBUG=1          log titles, commands and secret covers
//                               (=secrets also turns Hide Secrets on;
//                                =paste checks paste confirmation, then quits)
import AppKit
import SwiftTerm
import WtfCore

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
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
            await WindowManager.shared.start()
            NSApp.activate()
            await debugRuns()
        }
    }

    /// Self-checks and benchmark runs driven by the environment.
    private func debugRuns() async {
        guard let w = WindowManager.shared.windows.first else { return }
        let env = ProcessInfo.processInfo.environment
        if let about = try? await Backend.call("about", "Info", as: AboutInfo.self) {
            NSLog("wtf \(about.version) (\(about.commit)) on \(about.os) \(about.arch), state \(about.stateDir)")
        }
        try? await Task.sleep(for: .seconds(1.5))
        if env["WTF_NATIVE_DEBUG"] == "paste" {
            // Pastes two lines from a private pasteboard through the
            // responder chain, as ⌘V does, then quits.
            Clipboard.board = NSPasteboard(name: .init("wtf.debug"))
            Clipboard.copy("echo one\necho two\n")
            w.window?.makeKey()
            NSLog("pasted: \(w.window?.firstResponder?.tryToPerform(#selector(NSText.paste(_:)), with: nil) ?? false)")
            try? await Task.sleep(for: .seconds(1))
            NSLog("sheet: \(String(describing: w.window?.attachedSheet))")
            if let sheet = w.window?.attachedSheet { w.window?.endSheet(sheet, returnCode: .cancel) }
            NSApp.terminate(nil)
        }
        if env["WTF_NATIVE_DEBUG"] == "tabs" {
            // Builds tabs, a split, a named group and a custom title, then
            // quits the normal way (which saves the session).
            let m = w.model
            m.open(cwd: "/tmp")
            m.split(.row)
            let g = m.createGroup(m.activeId!)!
            m.setGroup(g, name: "debug", color: .green)
            m.open(cwd: "/usr")
            m.rename(m.activeId!, "renamed")
            try? await Task.sleep(for: .seconds(1.5))
            NSLog("tabs: \(m.tabs.map { m.title($0) }) groups: \(m.groups.map(\.name)) panes: \(m.panes.count)")
            NSApp.terminate(nil)
        }
        if env["WTF_NATIVE_DEBUG"] == "snapshot", let dir = env["WTF_NATIVE_SNAPSHOT_DIR"] {
            // Renders the window with each side-pane tab to PNGs, for checking
            // the layout without screen recording. Then quits.
            func snap(_ name: String) {
                guard let v = w.window?.contentView, let rep = v.bitmapImageRepForCachingDisplay(in: v.bounds) else { return }
                v.cacheDisplay(in: v.bounds, to: rep)
                try? rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: "\(dir)/\(name).png"))
                NSLog("snapshot \(name)")
            }
            w.activeTerminal?.type("cd ~/Documents\r")
            for (tab, name) in [(SidebarTab.files, "files"), (.kube, "kube"), (.ssh, "ssh")] {
                w.sidebar.tab = tab
                w.sidebar.open = true
                w.layoutSidebar()
                try? await Task.sleep(for: .seconds(2.5))
                snap(name)
            }
            NSApp.terminate(nil)
        }
        if env["WTF_NATIVE_DEBUG"] == "m5", let dir = env["WTF_NATIVE_SNAPSHOT_DIR"] {
            // Runs a few commands and a local web server, then renders every
            // M5 side-pane tab and the Resource Usage dialog. Read-only.
            func snap(_ name: String, _ v: NSView?) {
                guard let v, let rep = v.bitmapImageRepForCachingDisplay(in: v.bounds) else { return }
                v.cacheDisplay(in: v.bounds, to: rep)
                try? rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: "\(dir)/\(name).png"))
                NSLog("snapshot \(name): \(w.sidePane?.debugSummary() ?? "-")")
            }
            func tab(_ t: SidebarTab, _ name: String, wait: Double = 2.5) async {
                w.sidebar.tab = t
                w.sidebar.open = true
                w.layoutSidebar()
                try? await Task.sleep(for: .seconds(wait))
                snap(name, w.window?.contentView)
            }
            w.window?.makeKey()
            w.activeTerminal?.type("cd /tmp\recho hello from wtf\rfalse\rls -1 / | head -5\r")
            try? await Task.sleep(for: .seconds(2.5))
            w.pinLastOutput()
            w.activeTerminal?.type("python3 -m http.server 8765 --bind 127.0.0.1\r")
            try? await Task.sleep(for: .seconds(2))
            await tab(.network, "network")
            await tab(.processes, "processes")
            ProcStore.shared.terminalsOnly = false
            ProcStore.shared.changed()
            try? await Task.sleep(for: .seconds(0.5))
            snap("processes-all", w.window?.contentView)
            ProcStore.shared.terminalsOnly = true
            await tab(.history, "history", wait: 1)
            w.sidebar.historyScope = "all"
            await tab(.pins, "pins", wait: 1)
            await tab(.history, "history-all", wait: 1)
            await tab(.share, "share", wait: 1)
            await tab(.snippets, "snippets")
            await tab(.aws, "aws")
            await tab(.findings, "findings")
            w.sidebar.generateSection = "key"
            await tab(.generate, "generate-key")
            w.showGenerator("password")
            try? await Task.sleep(for: .seconds(1))
            snap("generate-password", w.window?.contentView)
            UsageDialog.show(in: w)
            try? await Task.sleep(for: .seconds(4.5))
            snap("usage", w.window?.attachedSheet?.contentView)
            if let sheet = w.window?.attachedSheet { w.window?.endSheet(sheet) }
            w.activeTerminal?.type("\u{03}")
            try? await Task.sleep(for: .seconds(0.5))
            NSApp.terminate(nil)
        }
        if env["WTF_NATIVE_DEBUG"] == "kubeclicks" {
            // Clicks Kubernetes section headings through the real mouse path
            // (without reading the cluster), then quits.
            w.window?.makeKey()
            w.sidebar.tab = .kube
            w.sidebar.kubeView = "cluster"
            w.sidebar.open = true
            w.layoutSidebar()
            try? await Task.sleep(for: .seconds(2.5))
            NSLog("before: \(w.sidePane?.debugSummary() ?? "-")")
            let clicked = (w.sidePane as? KubeView)?.debugClick(["Deployments", "Pods"]) ?? []
            try? await Task.sleep(for: .seconds(0.5))
            NSLog("clicked \(clicked); after: \(w.sidePane?.debugSummary() ?? "-")")
            NSApp.terminate(nil)
        }
        if env["WTF_NATIVE_DEBUG"] == "commands" {
            // Runs commands so the address bar shows running, ✓ and ✗, with
            // the Files tab following the cd. Then quits.
            w.showSidebar(.files)
            w.activeTerminal?.type("cd /tmp\rsleep 1\rfalse\r")
            try? await Task.sleep(for: .seconds(4))
            NSLog("\(w.sidePane?.debugSummary() ?? "no side pane")")
            NSLog("last command: \(w.activePane.map { "\($0.lastCommand?.text ?? "-") exit \($0.lastCommand?.exitCode ?? -1), cwd \($0.cwd)" } ?? "-")")
            NSApp.terminate(nil)
        }
        if env["WTF_NATIVE_DEBUG"] == "sidebar" {
            // Opens each side-pane tab (read-only views), logs what it shows, quits.
            for tab in [SidebarTab.files, .ssh, .kube] {
                w.showSidebar(tab)
                try? await Task.sleep(for: .seconds(2.5))
                NSLog("\(w.sidePane?.debugSummary() ?? "no side pane")")
            }
            NSLog("sidebar state: open \(w.sidebar.open), tab \(w.sidebar.tab), width \(w.sidebar.width)")
            NSApp.terminate(nil)
        }
        if env["WTF_NATIVE_DEBUG"] == "restore" {
            let m = w.model
            NSLog("restored windows: \(WindowManager.shared.windows.map(\.name)) tabs: \(m.tabs.map { "\(m.title($0)):\($0.root.leaves.count)" }) groups: \(m.groups.map { "\($0.name)/\($0.color)" })")
            try? await Task.sleep(for: .seconds(1))
            NSLog("sessions: \(m.panes.values.map { $0.sessionId ?? "-" }.sorted())")
            NSApp.terminate(nil)
        }
        if let cmd = env["WTF_NATIVE_RUN"], !cmd.isEmpty { w.activeTerminal?.type(cmd + "\r") }
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

    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        WindowManager.shared.shouldTerminate()
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ app: NSApplication) -> Bool { true }

    func applicationWillTerminate(_ note: Notification) { Backend.shutdown() }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows: Bool) -> Bool {
        if !hasVisibleWindows { WindowManager.shared.newWindow() }
        return true
    }
}

/// The app menu, as in the Wails build (main.go buildMenu). Items with a
/// command id run it in the key window; Copy, Paste and Select All go
/// through the responder chain so text fields get them too.
@MainActor
func buildMenu() -> NSMenu {
    let menu = NSMenu()
    func submenu(_ title: String, _ items: [NSMenuItem]) -> NSMenu {
        let top = NSMenuItem()
        let m = NSMenu(title: title)
        items.forEach(m.addItem)
        top.submenu = m
        menu.addItem(top)
        return m
    }
    func cmd(_ title: String, _ id: String, _ key: String = "", _ mods: NSEvent.ModifierFlags = .command) -> NSMenuItem {
        let i = NSMenuItem(title: title, action: #selector(CommandTarget.run(_:)), keyEquivalent: key)
        i.target = CommandTarget.shared
        i.representedObject = id
        i.keyEquivalentModifierMask = mods
        return i
    }
    func role(_ title: String, _ action: Selector, _ key: String = "", _ mods: NSEvent.ModifierFlags = .command) -> NSMenuItem {
        let i = NSMenuItem(title: title, action: action, keyEquivalent: key)
        i.keyEquivalentModifierMask = mods
        return i
    }
    let arrow = { (k: Int) in String(UnicodeScalar(k)!) }
    let up = arrow(NSUpArrowFunctionKey), down = arrow(NSDownArrowFunctionKey)
    let left = arrow(NSLeftArrowFunctionKey), right = arrow(NSRightArrowFunctionKey)

    let appMenu = submenu("wtf", [
        cmd("About wtf", "app.about"),
        .separator(),
        cmd("Settings…", "config.open", ","),
        cmd("Change Theme…", "theme.pick"),
        .separator(),
        role("Hide wtf", #selector(NSApplication.hide(_:)), "h"),
        role("Hide Others", #selector(NSApplication.hideOtherApplications(_:)), "h", [.command, .option]),
        role("Show All", #selector(NSApplication.unhideAllApplications(_:))),
        .separator(),
        role("Quit wtf", #selector(NSApplication.terminate(_:)), "q"),
    ])
    _ = appMenu
    _ = submenu("Shell", [
        cmd("New Tab", "tab.new", "t"),
        cmd("New Window", "window.new", "n"),
        cmd("Split Right", "pane.splitRight", "d"),
        cmd("Split Down", "pane.splitDown", "d", [.command, .shift]),
        cmd("Duplicate Tab", "tab.duplicate"),
        cmd("Run Snippet…", "snippets.run", "r", [.command, .shift]),
        cmd("Start or Stop Recording…", "record.toggle", "s", [.command, .option]),
        cmd("Save Output as Text…", "export.text"),
        cmd("Save Commands as Markdown…", "export.markdown"),
        cmd("Run Security Scan…", "findings.scan"),
        cmd("Open Workspace…", "workspace.open"),
        cmd("Save Window as Workspace…", "workspace.save"),
        .separator(),
        cmd("Broadcast Input", "broadcast.toggle", "b", [.command, .option]),
        cmd("Move Tab to New Window", "tab.toNewWindow"),
        .separator(),
        cmd("Close", "pane.close", "w"),
        cmd("Close Tab", "tab.close", "w", [.command, .option]),
        cmd("Reopen Closed Tab", "tab.reopen", "t", [.command, .shift]),
        .separator(),
        cmd("Close Window", "window.close", "w", [.command, .shift]),
    ])
    _ = submenu("Edit", [
        role("Copy", #selector(NSText.copy(_:)), "c"),
        role("Paste", #selector(NSText.paste(_:)), "v"),
        role("Select All", #selector(NSText.selectAll(_:)), "a"),
        .separator(),
        cmd("Find…", "find", "f"),
        cmd("Find Next", "find.next", "g"),
        cmd("Find Previous", "find.prev", "g", [.command, .shift]),
        .separator(),
        cmd("Copy Last Command Output", "term.copyOutput", "c", [.command, .option]),
        cmd("Clear Buffer", "term.clear", "k"),
    ])
    _ = submenu("View", [
        cmd("Command Palette…", "palette", "p", [.command, .shift]),
        cmd("Toggle Sidebar", "sidebar.toggle", "b"),
        cmd("Show Files", "sidebar.files", "e", [.command, .shift]),
        cmd("Show SSH Hosts", "sidebar.ssh", "s", [.command, .shift]),
        cmd("Show AWS Profiles", "sidebar.aws", "u", [.command, .shift]),
        cmd("Show Kubernetes", "sidebar.kube", "k", [.command, .shift]),
        cmd("Show Snippets", "sidebar.snippets", "y", [.command, .shift]),
        cmd("Show Workspaces", "sidebar.workspaces", "l", [.command, .shift]),
        cmd("Show Network", "sidebar.network", "o", [.command, .shift]),
        cmd("Show Processes", "sidebar.processes", "a", [.command, .shift]),
        cmd("Show Command History", "sidebar.history", "h", [.command, .shift]),
        cmd("Show Pinned Output", "sidebar.pins", "i", [.command, .shift]),
        cmd("Show Findings", "sidebar.findings", "f", [.command, .shift]),
        cmd("Show Share & Record", "sidebar.share", "j", [.command, .shift]),
        cmd("Show Generate Keys & Passwords", "sidebar.generate", "n", [.command, .shift]),
        cmd("Resource Usage…", "usage.show"),
        cmd("Hide Secrets", "secrets.toggle", "r", [.command, .option]),
        .separator(),
        cmd("Previous Prompt", "prompt.prev", up),
        cmd("Next Prompt", "prompt.next", down),
        .separator(),
        cmd("Bigger Text", "font.bigger", "="),
        cmd("Smaller Text", "font.smaller", "-"),
        cmd("Actual Size", "font.reset", "0"),
        .separator(),
        role("Enter Full Screen", #selector(NSWindow.toggleFullScreen(_:)), "f", [.command, .control]),
    ])
    _ = submenu("Tab", [
        cmd("Show All Tabs", "tab.overview", "\\", [.command, .shift]),
        .separator(),
        cmd("Show Next Tab", "tab.next", "]", [.command, .shift]),
        cmd("Show Previous Tab", "tab.prev", "[", [.command, .shift]),
        cmd("Move Tab Right", "tab.moveRight", right, [.command, .shift]),
        cmd("Move Tab Left", "tab.moveLeft", left, [.command, .shift]),
        .separator(),
        cmd("Add Tab to New Group", "tab.group", "g", [.command, .option]),
        .separator(),
    ] + (1...8).map { cmd("Select Tab \($0)", "tab.select.\($0)", "\($0)") } + [cmd("Select Last Tab", "tab.select.last", "9")])
    _ = submenu("Pane", [
        cmd("Toggle Zoom", "pane.zoom", "\r", [.command, .shift]),
        .separator(),
        cmd("Select Next Pane", "pane.next", "]"),
        cmd("Select Previous Pane", "pane.prev", "["),
        cmd("Select Pane Left", "pane.left", left, [.command, .option]),
        cmd("Select Pane Right", "pane.right", right, [.command, .option]),
        cmd("Select Pane Above", "pane.up", up, [.command, .option]),
        cmd("Select Pane Below", "pane.down", down, [.command, .option]),
    ])
    let windowMenu = submenu("Window", [
        role("Minimize", #selector(NSWindow.performMiniaturize(_:)), "m"),
        role("Zoom", #selector(NSWindow.performZoom(_:))),
        .separator(),
        role("Bring All to Front", #selector(NSApplication.arrangeInFront(_:))),
    ])
    NSApp.windowsMenu = windowMenu
    return menu
}

/// Ctrl-Tab and Ctrl-Shift-Tab cycle tabs, before the terminal sees them.
@MainActor
func installKeyMonitor() {
    NSEvent.addLocalMonitorForEvents(matching: .keyDown) { e in
        let mods = e.modifierFlags.intersection(.deviceIndependentFlagsMask)
        guard e.keyCode == 48, mods.contains(.control), !mods.contains(.command), let w = WindowManager.shared.keyWindow else { return e }
        w.model.cycle(mods.contains(.shift) ? -1 : 1)
        return nil
    }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.setActivationPolicy(.regular)
app.mainMenu = buildMenu()
installKeyMonitor()
app.run()

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
        if env["WTF_NATIVE_DEBUG"] == "api", let dir = env["WTF_NATIVE_SNAPSHOT_DIR"] {
            // Serves a JSON file on 127.0.0.1:8766 from a terminal, then
            // sends it from an API tab and renders each page. Local only.
            func snap(_ name: String) {
                guard let v = w.window?.contentView, let rep = v.bitmapImageRepForCachingDisplay(in: v.bounds) else { return }
                v.cacheDisplay(in: v.bounds, to: rep)
                try? rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: "\(dir)/\(name).png"))
                NSLog("snapshot \(name): \(w.activeApi?.debugSummary ?? "-")")
            }
            w.window?.makeKey()
            w.activeTerminal?.type("cd \(Shell.quote(dir)) && printf '%s' '{\"users\":[{\"id\":12345678901234567890,\"name\":\"ada\",\"admin\":true}],\"next\":null}' > users.json && python3 -m http.server 8766 --bind 127.0.0.1\r")
            try? await Task.sleep(for: .seconds(2))
            // A new, empty tab: each list still has its Add button.
            w.model.openApi()
            try? await Task.sleep(for: .seconds(0.5))
            snap("api-new-params")
            w.activeApi?.debugShow(request: 1)
            try? await Task.sleep(for: .seconds(0.3))
            snap("api-new-headers")
            w.model.closeActivePane()
            w.model.openApi(.init(method: "GET", url: "http://127.0.0.1:8766/users.json?page=2&limit=50", headers: [
                .init(name: "Authorization", value: "Bearer abc.def.ghi"), .init(name: "Accept", value: "application/json"),
                .init(name: "X-Debug", value: "1", off: true),
            ]))
            try? await Task.sleep(for: .seconds(0.8))
            snap("api-empty")
            w.activeApi?.send()
            try? await Task.sleep(for: .seconds(1.5))
            snap("api-params")
            w.activeApi?.debugShow(request: 1)
            try? await Task.sleep(for: .seconds(0.3))
            snap("api-headers")
            w.activeApi?.debugShow(request: 2, response: 1)
            try? await Task.sleep(for: .seconds(0.3))
            snap("api-body-resheaders")
            w.model.split(.row)
            try? await Task.sleep(for: .seconds(1))
            snap("api-split")
            w.model.closeActivePane()
            w.model.activate(w.model.tabs[0].id)
            w.activeTerminal?.type("\u{03}")
            try? await Task.sleep(for: .seconds(0.5))
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
        if env["WTF_NATIVE_DEBUG"] == "m6", let dir = env["WTF_NATIVE_SNAPSHOT_DIR"] {
            // The manual's commands, the terminal's inspector menu over real
            // output, and each M6 dialog rendered to a PNG. Read-only.
            func snapSheet(_ name: String) {
                guard let v = w.window?.attachedSheet?.contentView, let rep = v.bitmapImageRepForCachingDisplay(in: v.bounds) else {
                    NSLog("snapshot \(name): no sheet")
                    return
                }
                v.cacheDisplay(in: v.bounds, to: rep)
                try? rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: "\(dir)/\(name).png"))
                NSLog("snapshot \(name)")
            }
            func endSheet() { if let s = w.window?.attachedSheet { w.window?.endSheet(s) } }
            NSLog("manual unknown commands: \(ManualWindow.missingCommands())")
            NSLog("terminal font: \(w.activeTerminal?.view.font.fontName ?? "-")")
            w.window?.makeKey()
            let header = #"{"alg":"HS256","typ":"JWT"}"#, claims = #"{"sub":"ada","iss":"https://idp.example.com","exp":1791829800,"iat":1791826200,"aud":["api"]}"#
            func b64u(_ s: String) -> String {
                Data(s.utf8).base64EncodedString().replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_").replacingOccurrences(of: "=", with: "")
            }
            let token = b64u(header) + "." + b64u(claims) + ".c2lnbmF0dXJlLXNpZ25hdHVyZQ"
            w.activeTerminal?.type("clear; printf '%s\\n' '2026-10-15 INFO {\"user\":\"ada\",\"roles\":[\"admin\"],\"ok\":true}' 'Authorization: Bearer \(token)' '  password: cGFzc3dvcmQxMjM=' 'see https://example.com/docs'\r")
            try? await Task.sleep(for: .seconds(2))
            if let t = w.activeTerminal {
                NSLog("menu json line: \(t.debugInspectTitles(at: "INFO"))")
                NSLog("menu jwt: \(t.debugInspectTitles(at: "eyJ", offset: 3))")
                NSLog("menu yaml: \(t.debugInspectTitles(at: "cGFzc3dvcmQxMjM=", offset: 2))")
                NSLog("menu url: \(t.debugInspectTitles(at: "https://", offset: 4))")
            }
            ManualWindow.show("inspect", from: w)
            try? await Task.sleep(for: .seconds(2))
            await withCheckedContinuation { c in ManualWindow.debugSnapshot(to: "\(dir)/manual.png") { c.resume() } }
            NSApp.windows.first { $0.title == "wtf Manual" }?.close()
            w.window?.makeKey()
            try? await Task.sleep(for: .seconds(0.5))
            AboutDialog.show(in: w)
            try? await Task.sleep(for: .seconds(2.5))
            snapSheet("about")
            endSheet()
            ToolsDialog.show(in: w)
            try? await Task.sleep(for: .seconds(6))
            snapSheet("tools")
            endSheet()
            JsonViewer(json: OrderedJSON.find(#"{"user":"ada","roles":["admin"],"ok":true,"n":1.5}"#)!, owner: w).show()
            try? await Task.sleep(for: .seconds(1))
            snapSheet("json")
            JwtViewer(token: token, owner: w).show()
            try? await Task.sleep(for: .seconds(1))
            snapSheet("jwt")
            let dv = DecodedViewer(text: "cGFzc3dvcmQxMjM=", owner: w)
            dv.show()
            try? await Task.sleep(for: .seconds(1))
            snapSheet("decoded")
            CertViewer(.file("/etc/ssl/cert.pem"), owner: w).show()
            try? await Task.sleep(for: .seconds(3))
            snapSheet("cert")
            endSheet()
            try? await Task.sleep(for: .seconds(0.5))
            WorkspaceEditor.show(.capture(w.model.captureWorkspace()), in: w)
            try? await Task.sleep(for: .seconds(1.5))
            snapSheet("workspace-editor")
            endSheet()
            try? await Task.sleep(for: .seconds(0.5))
            NSApp.terminate(nil)
        }
        if env["WTF_NATIVE_DEBUG"] == "perf", let bench = env["WTF_NATIVE_BENCH"] {
            // M7 performance check: the terminal benchmark (macos/bench/workload.sh,
            // run in this window's shell), then 30s with each side-pane tab shown,
            // and the manual opened and closed. Same meter, same results.csv.
            let label = env["WTF_NATIVE_BENCH_LABEL"] ?? "native"
            let done = "\(bench)/.workload-done"
            try? FileManager.default.removeItem(atPath: done)
            w.window?.makeKey()
            w.activeTerminal?.type("cd \(Shell.quote(bench))/../.. && macos/bench/workload.sh \(label); touch \(Shell.quote(done))\r")
            for _ in 0..<180 where !FileManager.default.fileExists(atPath: done) { try? await Task.sleep(for: .seconds(2)) }
            func meter() -> (cpu: Double, mib: Double) {
                let p = Process()
                p.executableURL = URL(fileURLWithPath: "\(bench)/respcpu")
                p.arguments = ["\(ProcessInfo.processInfo.processIdentifier)"]
                let out = Pipe()
                p.standardOutput = out
                try? p.run()
                p.waitUntilExit()
                let f = String(decoding: out.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self).split(separator: " ")
                return (Double(f.first ?? "0") ?? 0, Double(f.count > 1 ? f[1] : "0") ?? 0)
            }
            func record(_ phase: String, seconds: Double) async {
                let a = meter()
                try? await Task.sleep(for: .seconds(seconds))
                let b = meter()
                let line = String(format: "%@,%@,%.2f,%.2f,%.0f,%.0f\n", label, phase, seconds, b.cpu - a.cpu, 100 * (b.cpu - a.cpu) / seconds, b.mib)
                if let h = FileHandle(forWritingAtPath: "\(bench)/results.csv") {
                    h.seekToEndOfFile()
                    h.write(Data(line.utf8))
                    h.closeFile()
                }
                NSLog("perf \(line)")
            }
            w.activeTerminal?.type("clear\r")
            for tab in [SidebarTab.files, .network, .processes, .history, .pins, .findings, .kube] {
                w.sidebar.tab = tab
                w.sidebar.open = true
                w.layoutSidebar()
                w.window?.makeKey()
                try? await Task.sleep(for: .seconds(3))
                await record("pane-\(tab)-30s", seconds: 30)
            }
            w.sidebar.open = false
            w.layoutSidebar()
            ManualWindow.show("getting-started", from: w)
            try? await Task.sleep(for: .seconds(3))
            await record("manual-open-15s", seconds: 15)
            NSApp.windows.first { $0.title == "wtf Manual" }?.close()
            w.window?.makeKey()
            try? await Task.sleep(for: .seconds(5))
            await record("manual-closed-15s", seconds: 15)
            NSApp.terminate(nil)
        }
        if env["WTF_NATIVE_DEBUG"] == "m7", let dir = env["WTF_NATIVE_SNAPSHOT_DIR"] {
            // Log colors and token menus over real output, the tab title a
            // program sets, and a window snapshot. Read-only.
            w.window?.makeKey()
            let repo = env["WTF_NATIVE_REPO"] ?? "/tmp"
            w.activeTerminal?.type("cd \(Shell.quote(repo)) && clear && printf '%s\\n' '2026-10-15T10:00:01Z INFO server started on 10.0.0.5:8080' '2026-10-15 10:00:02,120 WARN slow request pod/web-7d9f8 took 3s' 'Oct 15 10:00:03 host sshd[42]: ERROR from 192.168.1.20' 'panic at macos/build.sh:12 +0x1d' 'deployment.apps/api restarted, see https://example.com/x' && printf '\\033]0;custom title from a program\\007'\r")
            try? await Task.sleep(for: .seconds(2.5))
            if let t = w.activeTerminal {
                NSLog("tints: \(t.container.tints.runs.map { "\($0.row):\($0.col)+\($0.width) \($0.color)" })")
                NSLog("menu k8s: \(t.debugInspectTitles(at: "pod/web", offset: 5))")
                NSLog("menu timestamp: \(t.debugInspectTitles(at: "2026-10-15T10", offset: 3))")
                NSLog("menu ip: \(t.debugInspectTitles(at: "10.0.0.5", offset: 1))")
                NSLog("menu path: \(t.debugInspectTitles(at: "macos/build.sh", offset: 2))")
                NSLog("menu deployment: \(t.debugInspectTitles(at: "deployment.apps", offset: 2))")
            }
            NSLog("tab title: \(w.model.active.map { w.model.title($0) } ?? "-")")
            // The live window, blending and all, for screencapture -l from outside.
            NSLog("window id: \(w.window?.windowNumber ?? 0)")
            try? await Task.sleep(for: .seconds(4))
            if let v = w.window?.contentView, let rep = v.bitmapImageRepForCachingDisplay(in: v.bounds) {
                v.cacheDisplay(in: v.bounds, to: rep)
                try? rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: "\(dir)/logs.png"))
            }
            NSApp.terminate(nil)
        }
        if env["WTF_NATIVE_DEBUG"] == "idle" {
            // Waits (WTF_NATIVE_WAIT seconds, default 5) with whatever
            // WTF_NATIVE_RUN started, then quits the normal way.
            if let cmd = env["WTF_NATIVE_IDLE_RUN"] {
                try? await Task.sleep(for: .seconds(1.5))
                w.activeTerminal?.type(cmd + "\r")
            }
            try? await Task.sleep(for: .seconds(Double(env["WTF_NATIVE_WAIT"] ?? "") ?? 5))
            NSLog("idle: quitting with \(w.model.panes.count) panes, running: \(w.model.panes.values.filter(\.running).count)")
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

    /// Where state and caches live: wtf's own folders (~/Library/Application
    /// Support/wtf, ~/Library/Caches/wtf), shared with earlier versions, so
    /// windows, settings and port forwards carry over. Debug self-checks keep
    /// theirs apart ("wtf-native"), so a test run never touches the real session.
    private static func profileDirs() throws -> (state: String, cache: String) {
        let env = ProcessInfo.processInfo.environment
        guard env["WTF_NATIVE_DEBUG"] != nil || env["WTF_NATIVE_PROFILE"] == "separate" else { return ("", "") }
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
    func servicesItem() -> NSMenuItem {
        let item = NSMenuItem(title: "Services", action: nil, keyEquivalent: "")
        let menu = NSMenu(title: "Services")
        item.submenu = menu
        NSApp.servicesMenu = menu
        return item
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
        cmd("Command-Line Tools…", "tools.show"),
        cmd("AI Providers…", "ai.providers"),
        .separator(),
        servicesItem(),
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
        cmd("New API Tab", "tab.newApi", "t", [.command, .option]),
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
        role("Undo", Selector(("undo:")), "z"),
        role("Redo", Selector(("redo:")), "z", [.command, .shift]),
        .separator(),
        role("Cut", #selector(NSText.cut(_:)), "x"),
        role("Copy", #selector(NSText.copy(_:)), "c"),
        role("Paste", #selector(NSText.paste(_:)), "v"),
        role("Select All", #selector(NSText.selectAll(_:)), "a"),
        .separator(),
        cmd("Find…", "find", "f"),
        cmd("Find Next", "find.next", "g"),
        cmd("Find Previous", "find.prev", "g", [.command, .shift]),
        .separator(),
        cmd("Copy Last Command Output", "term.copyOutput", "c", [.command, .option]),
        cmd("Pin Last Command Output", "term.pinOutput"),
        cmd("Explain Last Failed Command…", "ai.explain"),
        cmd("Review Last Command’s Changes…", "review.last"),
        cmd("Command from Plain English…", "ai.ask", "i"),
        cmd("Investigate (AI)…", "ai.investigate"),
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
    let helpMenu = submenu("Help", [
        cmd("wtf Manual", "help.manual", "?"),
        cmd("Keyboard Shortcuts", "help.shortcuts"),
    ])
    NSApp.helpMenu = helpMenu
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

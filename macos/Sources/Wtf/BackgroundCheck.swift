// WTF_NATIVE_DEBUG=background: background agents (#34), as linux's
// selfcheck_background.go. A stand-in claude (on PATH for new shells; the
// shell refuses to run any other claude) runs in a tab; parking it keeps it
// running in the never-shown background window; Peek shows its screen and a
// reply reaches it; Open brings it back into a tab; End ends it; Start in
// Background starts one there. It parks by calling park (what "Keep Running
// in Background" does) so it never changes the user's agent-close setting.
import AppKit
import WtfCore

@MainActor
enum BackgroundCheck {
    private static var failed = false

    private static func check(_ ok: Bool, _ what: String) {
        print((ok ? "PASS " : "FAIL ") + what)
        fflush(stdout)
        if !ok { failed = true }
    }

    private static func waitUntil(_ seconds: Double, _ cond: () -> Bool) async -> Bool {
        let end = ContinuousClock.now + .seconds(seconds)
        while ContinuousClock.now < end {
            if cond() { return true }
            try? await Task.sleep(for: .milliseconds(50))
        }
        return cond()
    }

    private static func script(_ dir: String) -> String {
        let d = Shell.quote(dir)
        return """
        #!/bin/sh
        d=\(d)
        echo "$*" >> "$d/args"
        h() { printf '%s' "$1" | "$WTF_MCP" hook; }
        h '{"hook_event_name":"SessionStart"}'
        h '{"hook_event_name":"UserPromptSubmit","prompt":"background work"}'
        h '{"hook_event_name":"Notification","message":"Claude needs your permission to use Bash"}'
        echo STANDIN-SCREEN
        while IFS= read -r line; do
            echo "$line" >> "$d/typed"
            [ "$line" = quit ] && break
        done
        h '{"hook_event_name":"SessionEnd"}'

        """
    }

    static func run(_ w: WindowController) async {
        defer {
            print(failed ? "SOME FAILED" : "ALL PASSED")
            fflush(stdout)
            exit(failed ? 1 : 0)
        }
        let fm = FileManager.default
        let dir = NSTemporaryDirectory() + "wtf-background-check"
        try? fm.removeItem(atPath: dir)
        try? fm.createDirectory(atPath: dir, withIntermediateDirectories: true)
        try? script(dir).write(toFile: "\(dir)/claude", atomically: true, encoding: .utf8)
        try? fm.setAttributes([.posixPermissions: 0o755], ofItemAtPath: "\(dir)/claude")
        func read(_ f: String) -> String { (try? String(contentsOfFile: "\(dir)/\(f)", encoding: .utf8)) ?? "" }
        // New shells find the stand-in first; each also checks before running it.
        setenv("PATH", dir + ":" + (ProcessInfo.processInfo.environment["PATH"] ?? "/usr/bin:/bin"), 1)
        let q = Shell.quote(dir)
        // The user's startup files may put another claude first on PATH.
        let guarded = "export PATH=\(q):$PATH; [ \"$(/usr/bin/which claude)\" = \(q)/claude ] && "
        let wm = WindowManager.shared
        let m = w.model

        m.open(cwd: dir)
        var id = 0
        check(await waitUntil(20) {
            id = m.active?.focusedPaneId ?? 0
            return m.panes[id]?.integrated == true && m.panes[id]?.running == false
        }, "a tab with shell integration")
        guard let term = w.terminals[id], let session = m.panes[id]?.sessionId else { return }
        term.type(guarded + "claude --resume x\r")
        check(await waitUntil(15) { Agents.all[session]?.state == "waiting" }, "claude runs and needs you")
        check(read("args").contains("/claude-plugin/status-settings.json"), "with wtf's status hooks: \(read("args").debugDescription)")

        // Keep Running in Background.
        w.park([id])
        check(await waitUntil(5) { m.pane(session: session) == nil && wm.background?.model.pane(session: session) != nil },
              "parking moves it to the background window")
        check(wm.background?.window?.isVisible == false && wm.visibleWindows.count == 1, "which isn't shown")
        check(Agents.all[session] != nil, "and it keeps running")
        if !(w.sidebar.open && w.sidebar.tab == .agents) { w.showSidebar(.agents) }
        var summary = ""
        check(await waitUntil(5) { summary = w.sidePane?.debugSummary() ?? ""; return summary.contains("\(session) waiting background") },
              "the Agents tab lists it under Background: \(summary.debugDescription)")

        // Peek and reply.
        Background.peek(session)
        check(await waitUntil(5) { Background.lastPeek?.screenText.contains("STANDIN-SCREEN") == true },
              "Peek shows its screen: \((Background.lastPeek?.screenText ?? "").suffix(80).debugDescription)")
        Background.lastPeek?.reply.stringValue = "hello agent"
        Background.lastPeek?.sendReply()
        check(await waitUntil(5) { read("typed").contains("hello agent\n") }, "a reply reaches it: \(read("typed").debugDescription)")
        Background.lastPeek?.close()

        // Open: back in a tab of the shown window, the same shell.
        Agents.go(to: session)
        check(await waitUntil(5) { m.activePane?.sessionId == session && wm.background == nil },
              "Open brings it back into a tab (and the empty background window goes)")
        check(Agents.all[session] != nil, "still running")

        // Parked again, then ended.
        if let p = m.pane(session: session) { w.park([p.id]) }
        check(await waitUntil(5) { wm.background?.model.pane(session: session) != nil }, "parked again")
        if let bg = wm.background, let p = bg.model.pane(session: session) { bg.model.closePane(p.id) }
        check(await waitUntil(5) { Agents.all[session] == nil && wm.background == nil }, "End ends it")

        // Start in Background, with a first prompt.
        // The same path Start in Background takes, with the guard in front.
        check(Background.command(prompt: "fix the build") == "claude 'fix the build'", "the command it types")
        wm.startInBackground(cwd: dir, command: guarded + Background.command(prompt: "fix the build"))
        check(await waitUntil(20) { read("args").contains("fix the build") }, "Start in Background runs claude there: \(read("args").debugDescription)")
        var started: String?
        check(await waitUntil(5) {
            started = wm.background?.model.panes.values.compactMap(\.sessionId).first { Agents.all[$0] != nil }
            return started != nil
        }, "and the Agents tab has it")
        if let bg = wm.background, let s = started, let p = bg.model.pane(session: s) { bg.model.closePane(p.id) }
        check(await waitUntil(5) { wm.background == nil }, "ended")
    }
}

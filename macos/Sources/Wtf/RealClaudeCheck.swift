// WTF_NATIVE_DEBUG=realclaude WTF_REAL_CLAUDE_DIR=<a folder claude trusts>:
// the Approvals inbox (#39, #40) with the real Claude Code, by hand's
// stand-in: a claude typed in a tab (wtf's shell integration runs it with
// the status hooks) asks to run two harmless commands; the check presses
// Allow, then Deny, in the Agents tab. It uses your Claude Code login (a
// few cheap haiku turns), so it's never part of the regular checks.
// Prints PASS/FAIL lines, then quits.
import AppKit
import WtfCore

@MainActor
enum RealClaudeCheck {
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
            try? await Task.sleep(for: .milliseconds(100))
        }
        return cond()
    }

    private static func press(_ title: String, in v: NSView) -> Bool {
        if let b = v as? NSButton, b.title == title {
            b.performClick(nil)
            return true
        }
        return v.subviews.contains { press(title, in: $0) }
    }

    static func run(_ w: WindowController) async {
        defer {
            print(failed ? "SOME FAILED" : "ALL PASSED")
            fflush(stdout)
            exit(failed ? 1 : 0)
        }
        let env = ProcessInfo.processInfo.environment
        guard let dir = env["WTF_REAL_CLAUDE_DIR"] else {
            check(false, "WTF_REAL_CLAUDE_DIR is set")
            return
        }
        let snap = env["WTF_NATIVE_SNAPSHOT_DIR"]
        let allowed = NSTemporaryDirectory() + "wtf-real-allowed"
        let denied = NSTemporaryDirectory() + "wtf-real-denied"
        for f in [allowed, denied] { try? FileManager.default.removeItem(atPath: f) }

        let m = w.model
        m.open()
        var id = 0
        check(await waitUntil(20) {
            id = m.active?.focusedPaneId ?? 0
            return m.panes[id]?.integrated == true && m.panes[id]?.running == false
        }, "a new tab with shell integration")
        guard let term = w.terminals[id], let session = m.panes[id]?.sessionId else { return }
        if !(w.sidebar.open && w.sidebar.tab == .agents) { w.showSidebar(.agents) }
        let prompt = "Use the Bash tool to run exactly this one command and nothing else, then stop: "
            + "kubectl --context wtf-real-check -n shop get pods; touch \(allowed)"
        term.type("cd \(Shell.quote(dir)) && claude --model haiku --permission-mode default \(Shell.quote(prompt))\r")

        // 1. Allow, from the Agents tab.
        check(await waitUntil(120) { Agents.all[session]?.approvals.isEmpty == false }, "the real Claude Code's request shows in the inbox")
        let ap = Agents.all[session]?.approvals.first
        check(ap?.tool == "Bash" && ap?.what.contains("kubectl --context wtf-real-check -n shop get pods") == true, "with its command: \(ap?.what.debugDescription ?? "none")")
        check(ap?.facts.first?.hasPrefix("kubectl: context wtf-real-check") == true && ap?.facts.first?.contains("namespace shop (from -n)") == true
              || ap?.facts.first == "kubectl: context wtf-real-check, which isn't in the kubeconfig files (from --context)",
              "and where it would act: \(ap?.facts ?? [])")
        try? await Task.sleep(for: .seconds(1))
        if let snap, let v = w.sidePane, let rep = v.bitmapImageRepForCachingDisplay(in: v.bounds) {
            v.cacheDisplay(in: v.bounds, to: rep)
            try? rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: snap + "/real-approval.png"))
        }
        check(w.sidePane.map { press("Allow", in: $0) } == true, "Allow is pressed in the Agents tab")
        check(await waitUntil(60) { FileManager.default.fileExists(atPath: allowed) }, "the command ran")
        check(await waitUntil(60) { Agents.all[session]?.approvals.isEmpty == true && Agents.state(session: session).map { [.idle, .done].contains($0) } == true },
              "it leaves the inbox and Claude Code finishes: \(String(describing: Agents.state(session: session)))")
        func screen() -> String {
            guard let t = w.terminals[id]?.view.getTerminal() else { return "" }
            return (0..<t.rows).map { t.getLine(row: $0)?.translateToString(trimRight: true) ?? "" }.joined(separator: "\n")
        }
        check(screen().contains("Allowed by PermissionRequest hook"), "Claude Code says the hook allowed it")

        // 2. Deny, from the Agents tab.
        let second = "Use the Bash tool to run exactly this one command and nothing else: touch \(denied)"
        term.type(second)
        try? await Task.sleep(for: .milliseconds(300))
        term.type("\r")
        check(await waitUntil(120) { Agents.all[session]?.approvals.isEmpty == false }, "a second request shows")
        check(w.sidePane.map { press("Deny", in: $0) } == true, "Deny is pressed")
        check(await waitUntil(30) { Agents.all[session]?.approvals.isEmpty == true }, "it leaves the inbox")
        try? await Task.sleep(for: .seconds(3))
        check(!FileManager.default.fileExists(atPath: denied), "the command didn't run")
        check(await waitUntil(15) { Agents.state(session: session).map { [.idle, .done].contains($0) } == true },
              "Claude Code stops and waits for you: \(String(describing: Agents.state(session: session)))")

        term.type("/exit\r")
        check(await waitUntil(20) { m.panes[id]?.running == false }, "claude exits")
        m.closePane(id)
        for f in [allowed, denied] { try? FileManager.default.removeItem(atPath: f) }
    }
    /// WTF_NATIVE_DEBUG=realcodex: the same with the real Codex, whose prompt
    /// shows only once wtf's hook has returned; the inbox answers it with
    /// the prompt's keys. Run it with a CODEX_HOME of its own: it accepts
    /// Codex's folder and hook trust questions there.
    static func runCodex(_ w: WindowController) async {
        defer {
            print(failed ? "SOME FAILED" : "ALL PASSED")
            fflush(stdout)
            exit(failed ? 1 : 0)
        }
        let env = ProcessInfo.processInfo.environment
        guard let dir = env["WTF_REAL_CLAUDE_DIR"], env["CODEX_HOME"] != nil else {
            check(false, "WTF_REAL_CLAUDE_DIR and CODEX_HOME are set")
            return
        }
        let allowed = NSTemporaryDirectory() + "wtf-real-codex-allowed"
        let denied = NSTemporaryDirectory() + "wtf-real-codex-denied"
        for f in [allowed, denied] { try? FileManager.default.removeItem(atPath: f) }
        let m = w.model
        m.open()
        var id = 0
        check(await waitUntil(20) {
            id = m.active?.focusedPaneId ?? 0
            return m.panes[id]?.integrated == true && m.panes[id]?.running == false
        }, "a new tab with shell integration")
        guard let term = w.terminals[id], let session = m.panes[id]?.sessionId else { return }
        if !(w.sidebar.open && w.sidebar.tab == .agents) { w.showSidebar(.agents) }
        func screen() -> String {
            guard let t = w.terminals[id]?.view.getTerminal() else { return "" }
            return (0..<t.rows).map { t.getLine(row: $0)?.translateToString(trimRight: true) ?? "" }.joined(separator: "\n")
        }
        // Codex's trust questions, in its own CODEX_HOME: yes to both.
        func untilAsked(_ seconds: Double) async -> Bool {
            var folder = false, hooks = false
            return await waitUntil(seconds) {
                let sc = screen()
                if !folder, sc.contains("Trust and continue") { folder = true; term.type("\r") }
                if !hooks, sc.contains("Trust all and continue") { hooks = true; term.type("\u{1b}[B"); term.type("\r") }
                return Agents.all[session]?.approvals.isEmpty == false
            }
        }
        let prompt = "Run this shell command (ask for approval to escalate if the sandbox blocks it): touch \(allowed)"
        term.type("cd \(Shell.quote(dir)) && codex -a on-request -s read-only \(Shell.quote(prompt))\r")
        check(await untilAsked(180), "the real Codex's request shows in the inbox")
        let ap = Agents.all[session]?.approvals.first
        check(Agents.all[session]?.provider == "codex" && ap?.what.contains("touch \(allowed)") == true, "with its command: \(ap?.what.debugDescription ?? "none")")
        check(await waitUntil(15) { screen().contains("Would you like to run") }, "while Codex shows its own prompt")
        check(w.sidePane.map { press("Allow", in: $0) } == true, "Allow is pressed in the Agents tab")
        check(await waitUntil(60) { FileManager.default.fileExists(atPath: allowed) }, "the command ran")
        check(await waitUntil(60) { Agents.all[session]?.approvals.isEmpty == true }, "it leaves the inbox")
        check(await waitUntil(90) { Agents.state(session: session).map { [.idle, .done].contains($0) } == true }, "Codex finishes")

        term.type("Run this shell command (ask for approval to escalate if the sandbox blocks it): touch \(denied)")
        try? await Task.sleep(for: .milliseconds(300))
        term.type("\r")
        check(await untilAsked(180), "a second request shows")
        check(await waitUntil(15) { screen().contains("Would you like to run") }, "with Codex's prompt")
        check(w.sidePane.map { press("Deny", in: $0) } == true, "Deny is pressed")
        check(await waitUntil(30) { Agents.all[session]?.approvals.isEmpty == true }, "it leaves the inbox")
        try? await Task.sleep(for: .seconds(3))
        check(!FileManager.default.fileExists(atPath: denied), "the command didn't run")
        term.type("\u{3}")
        try? await Task.sleep(for: .milliseconds(500))
        term.type("\u{3}")
        check(await waitUntil(20) { m.panes[id]?.running == false }, "codex exits")
        m.closePane(id)
        for f in [allowed, denied] { try? FileManager.default.removeItem(atPath: f) }
    }
}

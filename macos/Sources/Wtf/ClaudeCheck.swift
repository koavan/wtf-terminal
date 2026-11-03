// WTF_NATIVE_DEBUG=claude: Ask Claude (#32) with a stand-in claude, as
// linux's selfcheck_claude.go. The backend runs $WTF_CLAUDE_STANDIN instead
// of claude in this mode (never the user's real one); the check writes it.
// It opens a split beside the shell running claude with the bundled
// plugin, points the plugin's tools at that shell, shows the hooks' states
// on the tab, goes back to the same Claude pane when asked again, and
// forgets it when it closes. Prints PASS/FAIL lines, then quits.
import AppKit
import WtfCore

@MainActor
enum ClaudeCheck {
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

    /// As in selfcheck_claude.go: notes its arguments, runs the plugin's
    /// hooks as Claude Code does, asks the MCP server for the target's
    /// recent commands; "go" and "quit" files move it on.
    private static func script(_ dir: String) -> String {
        let d = Shell.quote(dir)
        return """
        #!/bin/sh
        d=\(d)
        echo "$*" > "$d/args"
        h() { printf '%s' "$1" | "$WTF_MCP" hook; }
        h '{"hook_event_name":"SessionStart"}'
        h '{"hook_event_name":"PreToolUse","tool_name":"Bash"}' > "$d/pre.tmp" && mv "$d/pre.tmp" "$d/pre"
        (printf '%s\\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"check","version":"1"}}}' \\
            '{"jsonrpc":"2.0","method":"notifications/initialized"}' \\
            '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"recent_commands","arguments":{"count":5}}}'; sleep 3) | "$WTF_MCP" > "$d/mcp.out" 2> "$d/mcp.err"
        h '{"hook_event_name":"UserPromptSubmit"}'
        h '{"hook_event_name":"Notification","message":"Claude needs your permission to use Bash"}'
        touch "$d/waiting"
        while [ ! -e "$d/go" ]; do sleep 0.1; done
        h '{"hook_event_name":"Stop"}'
        touch "$d/idle"
        while IFS= read -r line; do
            echo "$line" >> "$d/typed"
            [ "$line" = quit ] && break
            h '{"hook_event_name":"UserPromptSubmit"}'
            case $line in *hold) while [ ! -e "$d/release" ]; do sleep 0.1; done ;; esac
            h '{"hook_event_name":"Stop"}'
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
        guard let standIn = ProcessInfo.processInfo.environment["WTF_CLAUDE_STANDIN"] else {
            check(false, "WTF_CLAUDE_STANDIN is set")
            return
        }
        let dir = (standIn as NSString).deletingLastPathComponent
        let fm = FileManager.default
        for f in ["args", "pre", "mcp.out", "mcp.err", "waiting", "go", "idle", "typed", "release"] { try? fm.removeItem(atPath: "\(dir)/\(f)") }
        try? fm.createDirectory(atPath: dir, withIntermediateDirectories: true)
        try? script(dir).write(toFile: standIn, atomically: true, encoding: .utf8)
        try? fm.setAttributes([.posixPermissions: 0o755], ofItemAtPath: standIn)
        func exists(_ f: String) -> Bool { fm.fileExists(atPath: "\(dir)/\(f)") }
        func read(_ f: String) -> String { (try? String(contentsOfFile: "\(dir)/\(f)", encoding: .utf8)) ?? "" }

        let m = w.model
        guard let targetId = m.active?.focusedPaneId, let target = w.terminals[targetId] else {
            check(false, "a first shell")
            return
        }
        // A restored session starts many shells at once: give the first longer.
        check(await waitUntil(20) { m.panes[targetId]?.integrated == true && m.panes[targetId]?.running == false }, "shell integration in the shell")
        target.type("echo hello-claude-target\r")
        check(await waitUntil(10) { target.lastFinishedBlock.flatMap { target.block($0) }?.text == "echo hello-claude-target" }, "the shell ran echo")
        guard let targetSession = m.panes[targetId]?.sessionId else { return }

        ClaudePane.ask(in: w)
        var claudeId = 0
        check(await waitUntil(10) {
            claudeId = m.active?.focusedPaneId ?? 0
            return claudeId != targetId && m.panes[claudeId]?.sessionId != nil
        }, "Ask Claude splits beside the shell")
        guard claudeId != targetId, let claudePane = m.panes[claudeId] else { return }
        let panes = m.panes.count
        check(await waitUntil(20) { exists("args") }, "claude starts in the split")
        let args = read("args").trimmingCharacters(in: .whitespacesAndNewlines)
        check(args.hasPrefix("--plugin-dir /") && args.hasSuffix("/Contents/Resources/claude-plugin"), "with the bundled plugin: \(args.debugDescription)")
        check(ClaudePane.isClaude(claudePane) && ClaudePane.target(of: claudePane) == targetSession,
              "its tools work on the shell it came from (→ \(ClaudePane.target(of: claudePane) ?? "none"))")
        check(await waitUntil(10) { exists("pre") } && read("pre").contains(#""permissionDecision":"ask""#), "the plugin asks before Bash: \(read("pre").debugDescription)")

        check(await waitUntil(20) { exists("waiting") }, "the hooks ran")
        let out = read("mcp.out")
        check(out.contains("echo hello-claude-target") && !out.contains("plugin-dir"),
              "recent_commands reads the target, not the Claude pane: \(String(out.prefix(160)).debugDescription) (stderr \(read("mcp.err").debugDescription))")
        func state() -> ClaudePane.State? { m.tabOf(pane: claudeId).flatMap { ClaudePane.state(tab: $0.id, in: w) } }
        check(await waitUntil(5) { state() == .waiting }, "the tab shows Claude Code needs you: \(String(describing: state()))")
        fm.createFile(atPath: "\(dir)/go", contents: nil)
        check(await waitUntil(5) { exists("idle") && state() == .idle }, "then idle: \(String(describing: state()))")

        // Asked again from the shell: back to the same Claude pane, nothing typed.
        m.focusPane(targetId)
        ClaudePane.ask(in: w)
        check(await waitUntil(5) { m.active?.focusedPaneId == claudeId } && m.panes.count == panes,
              "asking again goes to the same Claude pane (focused \(m.active?.focusedPaneId ?? 0), \(m.panes.count) panes)")
        ClaudePane.ask(in: w)
        try? await Task.sleep(for: .milliseconds(500))
        check(m.panes.count == panes, "a Claude pane doesn't get its own Claude pane (\(m.panes.count) panes)")

        // #24–#27 through the Claude pane: what each one types there.
        func typed(_ line: String) -> Bool { read("typed").split(separator: "\n").contains { $0 == line } }
        func screen() -> String {
            guard let t = w.terminals[claudeId]?.view.getTerminal() else { return "" }
            // Rows joined: a narrow pane wraps the line.
            return (0..<t.rows).map { t.getLine(row: $0)?.translateToString(trimRight: true) ?? "" }.joined()
        }
        target.type("ls /nonexistent-wtf\r")
        check(await waitUntil(10) {
            guard let id = target.lastFinishedBlock, let b = target.block(id) else { return false }
            return b.text == "ls /nonexistent-wtf" && (b.exitCode ?? 0) != 0
        }, "a command fails in the shell")
        let failed = target.lastFinishedBlock ?? 0
        m.focusPane(targetId)
        w.explainLastFailure()
        let explain = "/wtf:explain block \(failed)"
        check(await waitUntil(5) { typed(explain) }, "Explain Last Failed Command types \(explain.debugDescription) in the Claude pane, with Enter (got \(read("typed").debugDescription))")
        check(await waitUntil(5) { state() == .idle }, "and Claude Code is idle again")

        m.focusPane(targetId)
        ClaudePane.command(in: w)
        try? await Task.sleep(for: .milliseconds(500))
        check(screen().contains("/wtf:cmd") && !read("typed").contains("/wtf:cmd"), "Command from Plain English types /wtf:cmd for you to finish, without Enter")
        w.terminals[claudeId]?.type("hold\r")
        check(await waitUntil(5) { typed("/wtf:cmd hold") && state() == .busy }, "you finish it: busy")

        // While it works, a review waits for its next prompt.
        let plan = "/tmp/wtf-check-plan.json"
        m.focusPane(targetId)
        ClaudePane.review(file: plan, in: w)
        let review = "/wtf:review plan file \(plan)"
        try? await Task.sleep(for: .milliseconds(700))
        check(!typed(review), "nothing is typed while Claude Code works")
        fm.createFile(atPath: "\(dir)/release", contents: nil)
        check(await waitUntil(5) { typed(review) }, "then the review is typed: \(read("typed").debugDescription)")
        check(await waitUntil(5) { state() == .idle }, "and Claude Code is idle again")

        m.focusPane(targetId)
        ClaudePane.investigate(in: w)
        try? await Task.sleep(for: .milliseconds(500))
        check(screen().contains("/wtf:investigate"), "Investigate types /wtf:investigate for you to finish")
        w.terminals[claudeId]?.type(Shell.clearLine + "quit\r")
        check(await waitUntil(10) { m.panes[claudeId]?.running == false }, "claude exits")
        m.closePane(claudeId)
        check(await waitUntil(5) { !ClaudePane.isClaude(claudePane) && ClaudePane.target(of: claudePane) == nil }, "closing the Claude pane forgets it")
        check(m.tabOf(pane: targetId).flatMap { ClaudePane.state(tab: $0.id, in: w) } == nil, "and the tab's badge goes")
    }
}

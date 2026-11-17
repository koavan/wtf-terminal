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
        h '{"hook_event_name":"PreToolUse","tool_name":"Bash","cwd":"/tmp","tool_input":{"command":"kubectl --context wtf-check-ctx get pods"}}' > "$d/pre.tmp" && mv "$d/pre.tmp" "$d/pre"
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
        // #40: and says where the command would act, in Claude Code's dialog.
        check(read("pre").contains(#"Where it acts:\nkubectl: context wtf-check-ctx, which isn't in the kubeconfig files (from --context)"#),
              "with where it would act: \(read("pre").debugDescription)")

        check(await waitUntil(20) { exists("waiting") }, "the hooks ran")
        let out = read("mcp.out")
        check(out.contains("echo hello-claude-target") && !out.contains("plugin-dir"),
              "recent_commands reads the target, not the Claude pane: \(String(out.prefix(160)).debugDescription) (stderr \(read("mcp.err").debugDescription))")
        // Done (finished, not looked at yet) when the window isn't in front.
        func state() -> ClaudePane.State? {
            let st = m.tabOf(pane: claudeId).flatMap { ClaudePane.state(tab: $0.id, in: w) }
            return st == .done ? .idle : st
        }
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
        await agents(w)
    }

    /// A claude you start yourself in an ordinary tab, as selfcheck_claude.go's
    /// standInAgent: wtf's shell integration runs it with --settings
    /// (agent-status); it quits without SessionEnd, as a killed claude.
    private static func agentScript(_ dir: String) -> String {
        let d = Shell.quote(dir)
        return """
        #!/bin/sh
        d=\(d)
        echo "$*" > "$d/args"
        [ "$1" = --settings ] && grep -q 'WTF_MCP' "$2" && touch "$d/settings"
        h() { printf '%s' "$1" | "$WTF_MCP" hook; }
        h '{"hook_event_name":"SessionStart"}'
        h '{"hook_event_name":"UserPromptSubmit","prompt":"fix the flaky deploy test\\nand more"}'
        h '{"hook_event_name":"Notification","message":"Claude needs your permission to use Bash"}'
        touch "$d/waiting"
        while [ ! -e "$d/go" ]; do sleep 0.1; done
        h '{"hook_event_name":"PostToolUse"}'
        h '{"hook_event_name":"PostToolUse","tool_name":"Agent","tool_input":{"description":"Find the flaky test","subagent_type":"Explore"},"tool_response":{"status":"async_launched","agentId":"a1"}}'
        h '{"hook_event_name":"SubagentStart","agent_id":"a1","agent_type":"Explore"}'
        h '{"hook_event_name":"PostToolUse","agent_id":"a1","agent_type":"Explore","tool_name":"Read","tool_input":{"file_path":"/src/deploy_test.go"}}'
        h '{"hook_event_name":"Stop"}'
        touch "$d/done"
        while [ ! -e "$d/substop" ]; do sleep 0.1; done
        h '{"hook_event_name":"SubagentStop","agent_id":"a1","agent_type":"Explore","last_assistant_message":"It is TestDeploy."}'
        while [ ! -e "$d/ask" ]; do sleep 0.1; done
        h '{"hook_event_name":"UserPromptSubmit","prompt":"restart the api"}'
        h '{"hook_event_name":"PermissionRequest","cwd":"/tmp","tool_name":"Bash","tool_input":{"command":"kubectl --context wtf-check-ctx -n shop rollout restart deploy/api","description":"Restart the api"},"permission_suggestions":[{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"kubectl rollout restart:*"}],"behavior":"allow","destination":"localSettings"}]}' > "$d/answer.tmp"
        mv "$d/answer.tmp" "$d/answer"
        h '{"hook_event_name":"Stop"}'
        while [ ! -e "$d/quit" ]; do sleep 0.1; done

        """
    }

    /// The Agents tab (#33) with a claude typed in a new tab.
    private static func agents(_ w: WindowController) async {
        let fm = FileManager.default
        let dir = NSTemporaryDirectory() + "wtf-agents-check"
        try? fm.removeItem(atPath: dir)
        try? fm.createDirectory(atPath: dir, withIntermediateDirectories: true)
        try? agentScript(dir).write(toFile: "\(dir)/claude", atomically: true, encoding: .utf8)
        try? fm.setAttributes([.posixPermissions: 0o755], ofItemAtPath: "\(dir)/claude")
        func exists(_ f: String) -> Bool { fm.fileExists(atPath: "\(dir)/\(f)") }
        func read(_ f: String) -> String { ((try? String(contentsOfFile: "\(dir)/\(f)", encoding: .utf8)) ?? "").trimmingCharacters(in: .whitespacesAndNewlines) }

        let m = w.model
        m.open()
        var id = 0
        check(await waitUntil(20) {
            id = m.active?.focusedPaneId ?? 0
            return m.panes[id]?.integrated == true && m.panes[id]?.running == false
        }, "a new tab with shell integration")
        guard let term = w.terminals[id], let session = m.panes[id]?.sessionId else { return }
        // Only ever the stand-in: never the user's real claude.
        let q = Shell.quote(dir)
        term.type("export PATH=\(q):$PATH; [ \"$(/usr/bin/which claude)\" = \(q)/claude ] && claude --resume x\r")
        check(await waitUntil(15) { exists("waiting") }, "claude starts and its hooks run")
        check(exists("settings") && read("args").hasSuffix("/claude-plugin/status-settings.json --resume x"),
              "the shell runs it with wtf's status hooks: \(read("args").debugDescription)")
        func badge() -> AgentState? { m.tabOf(pane: id).flatMap { Agents.state(tab: $0.id, in: w) } }
        check(await waitUntil(5) { badge() == .waiting }, "its tab shows it needs you: \(String(describing: badge()))")
        // agent-group: its tab joined the window's "ungrouped agents" group.
        func inAgents() -> Bool {
            guard let t = m.tabOf(pane: id), let g = m.groupNamed(Agents.groupName) else { return false }
            return t.groupId == g
        }
        check(inAgents(), "its tab joins the agents group")
        let ag = Agents.all[session]
        check(ag?.message == "Claude needs your permission to use Bash" && ag?.prompt == "fix the flaky deploy test",
              "with what it asks and the prompt: \(String(describing: ag))")
        check(NSApp.dockTile.badgeLabel == "1", "the Dock icon counts it: \(NSApp.dockTile.badgeLabel ?? "none")")

        // A toggle: a restored session may already show it.
        if !(w.sidebar.open && w.sidebar.tab == .agents) { w.showSidebar(.agents) }
        var summary = ""
        // The side pane makes the tab on its next update.
        check(await waitUntil(5) { summary = w.sidePane?.debugSummary() ?? ""; return summary == "agents: \(session) waiting" },
              "the Agents tab lists it: \(summary.debugDescription)")

        fm.createFile(atPath: "\(dir)/go", contents: nil)
        check(await waitUntil(5) { exists("done") && (badge() == .done || badge() == .idle) }, "then done: \(String(describing: badge()))")
        check(NSApp.dockTile.badgeLabel == nil, "and the Dock badge goes")
        Task { try? await Services.bridge.Seen(session) }
        check(await waitUntil(5) { badge() == .idle }, "and idle once seen: \(String(describing: badge()))")

        // Its sub-agent (#35): folded under it, shown on a click, with its
        // task and latest step, then its result; its steps leave it idle.
        let sub = Agents.all[session]?.subagents.first
        check(Agents.all[session]?.subagents.count == 1 && sub?.type == "Explore" && sub?.task == "Find the flaky test"
              && sub?.doing == "Read deploy_test.go" && sub?.running == true, "its sub-agent, with its task and step: \(String(describing: sub))")
        check(badge() == .idle, "a sub-agent's step leaves it idle: \(String(describing: badge()))")
        check(await waitUntil(5) { summary = w.sidePane?.debugSummary() ?? ""; return summary == "agents: \(session) idle subagents=1" },
              "the sub-agents are folded away: \(summary.debugDescription)")
        Agents.toggleSubagents(session)
        check(await waitUntil(5) { summary = w.sidePane?.debugSummary() ?? ""; return summary == "agents: \(session) idle subagents=1 shown" },
              "a click shows them: \(summary.debugDescription)")
        fm.createFile(atPath: "\(dir)/substop", contents: nil)
        check(await waitUntil(5) { Agents.all[session]?.subagents.first.map { !$0.running && $0.result == "It is TestDeploy." } == true },
              "then done, with its result: \(String(describing: Agents.all[session]?.subagents.first))")
        Agents.toggleSubagents(session)
        check(!Agents.subagentsShown.contains(session), "another click hides them")

        // #39, #40: a permission prompt in the inbox, with where the command
        // would act; its Allow answers the waiting hook.
        fm.createFile(atPath: "\(dir)/ask", contents: nil)
        check(await waitUntil(10) { Agents.all[session]?.approvals.count == 1 }, "a permission prompt shows in the inbox")
        let ap = Agents.all[session]?.approvals.first
        check(ap?.tool == "Bash" && ap?.what == "kubectl --context wtf-check-ctx -n shop rollout restart deploy/api" && ap?.description == "Restart the api",
              "with its command: \(String(describing: ap))")
        check(ap?.facts == ["kubectl: context wtf-check-ctx, which isn't in the kubeconfig files (from --context)"],
              "and where it would act: \(String(describing: ap?.facts))")
        check(badge() == .waiting, "its tab shows it needs you: \(String(describing: badge()))")
        check(await waitUntil(5) { summary = w.sidePane?.debugSummary() ?? ""; return summary == "agents: \(session) waiting approvals=1" },
              "the Agents tab lists it: \(summary.debugDescription)")
        if let snap = ProcessInfo.processInfo.environment["WTF_NATIVE_SNAPSHOT_DIR"], let v = w.sidePane {
            v.layoutSubtreeIfNeeded()
            if let rep = v.bitmapImageRepForCachingDisplay(in: v.bounds) {
                v.cacheDisplay(in: v.bounds, to: rep)
                try? rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: snap + "/approvals.png"))
            }
        }
        // Next Approval goes to it from another tab.
        if let other = m.tabs.first(where: { !$0.root.leaves.contains(id) }) { m.activate(other.id) }
        check(m.active?.root.leaves.contains(id) == false, "another tab")
        // The floating card shows it there, over the terminal.
        check(await waitUntil(5) { !w.approvalDeck.isHidden && w.approvalDeck.shown.map(\.1.id) == [ap?.id ?? ""] },
              "a floating card shows it from another tab")
        check(w.approvalDeck.top?.id == ap?.id, "⌘⏎ would allow it")
        if let snap = ProcessInfo.processInfo.environment["WTF_NATIVE_SNAPSHOT_DIR"] {
            let v = w.rootView
            v.layoutSubtreeIfNeeded()
            if let rep = v.bitmapImageRepForCachingDisplay(in: v.bounds) {
                v.cacheDisplay(in: v.bounds, to: rep)
                try? rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: snap + "/approval-card.png"))
            }
        }
        Agents.nextApproval(from: w)
        check(await waitUntil(5) { m.active?.focusedPaneId == id }, "Next Approval goes to the agent asking")
        check(await waitUntil(5) { w.approvalDeck.isHidden }, "its card hides with the agent's own dialog in front")
        func buttons(_ v: NSView) -> [NSButton] {
            (v as? NSButton).map { [$0] } ?? v.subviews.flatMap(buttons)
        }
        let titles = (w.sidePane.map(buttons) ?? []).map(\.title).filter { ["Allow", "▾", "Deny", "Go to Agent"].contains($0) }
        check(titles == ["Allow", "▾", "Deny", "▾", "Go to Agent"], "with Allow ▾, Deny ▾ and Go to Agent: \(titles)")
        check(ap?.always == ["Always allow Bash(kubectl rollout restart:*) in this project"], "Allow's ▾ offers its rule: \(ap?.always ?? [])")
        // Deny's ▾ asks what to tell it, in a popover.
        if let deny = (w.sidePane.map(buttons) ?? []).last(where: { $0.title == "▾" }) {
            TellPopover.show(from: deny, anchor: w.rootView, title: "Deny, and tell Claude what to do instead", placeholder: "e.g. use pnpm instead",
                             send: "Deny and Send") { _ in }
            check(await waitUntil(3) { TellPopover.shown }, "Deny and Tell's popover shows")
            TellPopover.closeOpen()
        }
        ApprovalContent.allowAlways(ap?.id ?? "", 0, w.rootView)
        check(await waitUntil(5) { exists("answer") }, "Always allow answers the hook")
        check(read("answer") == #"{"hookSpecificOutput":{"decision":{"behavior":"allow","updatedPermissions":[{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"kubectl rollout restart:*"}],"behavior":"allow","destination":"localSettings"}]},"hookEventName":"PermissionRequest"}}"#,
              "with allow and its rule: \(read("answer").debugDescription)")
        check(await waitUntil(5) { Agents.all[session]?.approvals.isEmpty == true && badge() != .waiting }, "and it leaves the inbox: \(String(describing: badge()))")

        // A name you give it: the Agents tab and the picker show it.
        Task { try? await Services.bridge.Rename(session, "deploy fixer") }
        check(await waitUntil(5) { Agents.all[session].map(Agents.title) == "deploy fixer" }, "Rename… names it")

        m.open()
        var otherId = id
        check(await waitUntil(5) { otherId = m.active?.focusedPaneId ?? id; return otherId != id }, "another tab")
        Agents.go(to: session)
        check(await waitUntil(5) { m.active?.focusedPaneId == id }, "a row's click goes to its tab")
        // The check's profile keeps its session: leave no tabs behind.
        m.closePane(otherId)

        // Claude Code ends (its shell is back at its prompt): the tab leaves
        // the agents group, which goes with its last tab.
        Task { try? await Services.bridge.Forget(session) }
        check(await waitUntil(5) { !inAgents() }, "when it ends, its tab leaves the agents group")
        check(m.groupNamed(Agents.groupName) == nil, "and the empty group goes: \(m.tabs.map { "tab \($0.id) group \(String(describing: $0.groupId)) panes \($0.root.leaves)" }) agents \(Agents.all.keys.sorted())")

        // Its pane closes: nothing's left of it.
        m.closePane(id)
        check(await waitUntil(5) { Agents.all.isEmpty }, "closing its pane forgets it: \(Agents.all.count)")
        fm.createFile(atPath: "\(dir)/quit", contents: nil)
        check(await waitUntil(5) { (w.sidePane?.debugSummary() ?? "") == "agents: " }, "and the Agents tab is empty: \((w.sidePane?.debugSummary() ?? "none").debugDescription)")
    }
}

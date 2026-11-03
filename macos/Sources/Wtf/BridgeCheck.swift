// WTF_NATIVE_DEBUG=bridge: the bridge to the plugin's MCP server (#32),
// driven the way Claude Code drives it, as linux's selfcheck_bridge.go. One
// shell (the "Claude" one) starts $WTF_MCP with its own environment and
// speaks MCP to it over stdio; its tools point at a second shell (the
// target), which has a stand-in dig on its PATH. Prints PASS/FAIL lines,
// then quits; the exit status is 1 on a failure. Files go in a throwaway
// folder ($WTF_NATIVE_SNAPSHOT_DIR, else /tmp/wtf-bridge-check).
import AppKit
import WtfCore

@MainActor
enum BridgeCheck {
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

    static func run(_ w: WindowController, dir: String?) async {
        let dir = dir ?? "/tmp/wtf-bridge-check"
        let bin = dir + "/brbin"
        try? FileManager.default.removeItem(atPath: bin)
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        defer {
            print(failed ? "SOME FAILED" : "ALL PASSED")
            fflush(stdout)
            exit(failed ? 1 : 0)
        }
        let m = w.model
        guard let claudeId = m.active?.focusedPaneId, let claude = w.terminals[claudeId] else {
            check(false, "a first shell")
            return
        }
        check(await waitUntil(10) { m.panes[claudeId]?.integrated == true && m.panes[claudeId]?.running == false }, "shell integration in the first shell")
        m.split(.row)
        var targetId = 0
        check(await waitUntil(10) {
            targetId = m.active?.focusedPaneId ?? 0
            return targetId != claudeId && w.terminals[targetId] != nil && m.panes[targetId]?.sessionId != nil
        }, "a second shell split beside it")
        guard let target = w.terminals[targetId], let claudeSession = m.panes[claudeId]?.sessionId,
              let targetSession = m.panes[targetId]?.sessionId else { return }
        check(await waitUntil(10) { m.panes[targetId]?.integrated == true && m.panes[targetId]?.running == false }, "shell integration in the target")
        try? await Services.bridge.SetTarget(claudeSession, targetSession)

        // The target: a stand-in dig, and a command to read back.
        let q = Shell.quote(bin)
        target.type("mkdir -p \(q) && printf '#!/bin/sh\\necho \"$*\" >> \(bin)/calls\\necho \"ANSWER example.com 93.184.216.34\"\\n' > \(q)/dig && chmod +x \(q)/dig && export PATH=\(q):$PATH\r")
        check(await waitUntil(10) {
            guard let p = m.panes[targetId] else { return false }
            return (p.env?["PATH"] ?? "").hasPrefix(bin) && !p.running
        }, "the target reports the stand-in's PATH")
        target.type("echo hello-bridge\r")
        check(await waitUntil(10) { target.lastFinishedBlock.flatMap { target.block($0) }?.text == "echo hello-bridge" }, "the target ran echo")

        // What Claude Code sends: initialize, then requests, one JSON per
        // line; stdin stays open (sleep) until the answers are in.
        func session(_ name: String, _ calls: [String]) {
            let msgs = [
                #"{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"check","version":"1"}}}"#,
                #"{"jsonrpc":"2.0","method":"notifications/initialized"}"#,
            ] + calls
            try? (msgs.joined(separator: "\n") + "\n").write(toFile: "\(dir)/mcp-\(name).in", atomically: true, encoding: .utf8)
            try? FileManager.default.removeItem(atPath: "\(dir)/mcp-\(name).out")
            let base = Shell.quote("\(dir)/mcp-\(name)")
            claude.type("(cat \(base).in; sleep 6) | \"$WTF_MCP\" > \(base).out 2> \(base).err\r")
        }
        func answers(_ name: String, _ ids: [Int]) async -> [Int: [String: Any]] {
            var got: [Int: [String: Any]] = [:]
            _ = await waitUntil(25) {
                let text = (try? String(contentsOfFile: "\(dir)/mcp-\(name).out", encoding: .utf8)) ?? ""
                for line in text.split(separator: "\n") {
                    guard let obj = try? JSONSerialization.jsonObject(with: Data(line.utf8)) as? [String: Any],
                          let id = obj["id"] as? Int else { continue }
                    got[id] = (obj["result"] ?? obj["error"]) as? [String: Any] ?? [:]
                }
                return ids.allSatisfy { got[$0] != nil }
            }
            return got
        }
        func text(_ r: [String: Any]?) -> (String, Bool) {
            guard let r, let c = r["content"] as? [[String: Any]], let t = c.first?["text"] as? String else {
                return (String(describing: r), true)
            }
            return (t, r["isError"] as? Bool ?? false)
        }
        func idle(_ id: Int) async -> Bool { await waitUntil(15) { m.panes[id]?.running == false } }

        session("one", [
            #"{"jsonrpc":"2.0","id":2,"method":"tools/list"}"#,
            #"{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"recent_commands","arguments":{"count":3}}}"#,
            #"{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"run_readonly","arguments":{"argv":["rm","-rf","/tmp/nothing"],"purpose":"check"}}}"#,
            #"{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"pane_context","arguments":{}}}"#,
        ])
        var got = await answers("one", [2, 3, 4, 5])
        let names = ((got[2]?["tools"] as? [[String: Any]]) ?? []).compactMap { $0["name"] as? String }.sorted()
        let err = (try? String(contentsOfFile: "\(dir)/mcp-one.err", encoding: .utf8)) ?? ""
        check(names == ["findings", "log_patterns", "open_snippet_editor", "pane_context", "recent_commands", "review_changes", "run_readonly", "search_history", "session_commands", "type_at_prompt"],
              "wtf-mcp lists the app's tools: \(names) (stderr \(err.debugDescription))")
        var (t, isErr) = text(got[3])
        check(!isErr && t.contains("$ echo hello-bridge") && t.contains("hello-bridge\n"), "recent_commands reads the target's blocks: \(t.debugDescription)")
        (t, isErr) = text(got[4])
        check(isErr && t.contains("isn't on the read-only list"), "rm is refused: \(t.debugDescription)")
        (t, isErr) = text(got[5])
        check(!isErr && t.contains("State: at its prompt") && t.contains("Folder:"), "pane_context describes the target: \(t.debugDescription)")

        check(await idle(claudeId), "the first helper exits")
        session("two", [#"{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"run_readonly","arguments":{"argv":["dig","example.com"],"purpose":"check"}}}"#])
        got = await answers("two", [2])
        (t, isErr) = text(got[2])
        let calls = (try? String(contentsOfFile: "\(bin)/calls", encoding: .utf8)) ?? ""
        check(!isErr && t.contains("$ dig example.com") && t.contains("Exit code: 0") && t.contains("93.184.216.34"),
              "run_readonly runs dig in the target and returns its output: \(t.debugDescription)")
        check(calls == "example.com\n", "the target ran only dig: \(calls.debugDescription)")
        let last = target.lastFinishedBlock.flatMap { target.block($0) }?.text ?? ""
        check(last == "dig example.com", "it's a command block in the target: \(last.debugDescription)")

        check(await idle(claudeId), "the second helper exits")
        session("three", [#"{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"type_at_prompt","arguments":{"command":"echo typed-not-run"}}}"#])
        got = await answers("three", [2])
        (t, isErr) = text(got[2])
        try? await Task.sleep(for: .milliseconds(500))
        let term = target.view.getTerminal()
        // Rows joined: the line wraps at the pane's width. That it didn't
        // run is the next check (no new block).
        let screen = (0..<term.rows).map { term.getLine(row: $0)?.translateToString(trimRight: true) ?? "" }.joined()
        check(!isErr && t.contains("Nothing ran") && screen.contains("echo typed-not-run"),
              "type_at_prompt types without Enter: \(t.debugDescription), screen ends \(screen.suffix(80).debugDescription)")
        check(m.panes[targetId]?.running == false && (target.lastFinishedBlock.flatMap { target.block($0) }?.text ?? "") == "dig example.com",
              "nothing ran after typing")
        target.type(Shell.clearLine)

        // open_snippet_editor: wtf's editor opens as a sheet, filled in; nothing is saved.
        check(await idle(claudeId), "the third helper exits")
        session("four", [#"{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"open_snippet_editor","arguments":{"name":"Restart it","description":"Rolls the pods","steps":[{"title":"Restart","command":"kubectl -n {{ns}} rollout restart deploy/{{name}}"},{"title":"Wait","command":"kubectl -n {{ns}} rollout status deploy/{{name}}"}]}}}"#])
        got = await answers("four", [2])
        (t, isErr) = text(got[2])
        check(!isErr && t.contains("Nothing is saved"), "open_snippet_editor answers: \(t.debugDescription)")
        check(await waitUntil(5) { w.window?.attachedSheet != nil }, "and wtf's snippet editor opens on the window")
        if let sheet = w.window?.attachedSheet {
            let fields = sheet.contentView.map { allTextFields($0) }?.map(\.stringValue) ?? []
            check(fields.contains("Restart it") && fields.contains("Rolls the pods"), "filled in, named as given: \(fields.prefix(6))")
            w.window?.endSheet(sheet)
        }
        try? await Services.bridge.SetTarget(claudeSession, "")
    }

    private static func allTextFields(_ v: NSView) -> [NSTextField] {
        (v as? NSTextField).map { [$0] } ?? [] + v.subviews.flatMap { allTextFields($0) }
    }
}

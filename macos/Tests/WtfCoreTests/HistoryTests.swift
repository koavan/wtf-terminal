import Foundation
import Testing
@testable import WtfCore

@Test func historyStore() {
    var h = History()
    let t0 = Date(timeIntervalSince1970: 1000)
    h.started(pane: 1, id: 1, command: "ls", cwd: "/a", at: t0)
    h.finished(pane: 1, exitCode: 0, durationMs: 12, at: t0.addingTimeInterval(1))
    h.finished(pane: 1, exitCode: 9, durationMs: 99)
    h.started(pane: 1, id: 2, command: "   ", cwd: "/a")
    h.finished(pane: 2, exitCode: 1, durationMs: 5)
    #expect(h.of(1).count == 1 && h.of(1)[0].exitCode == 0 && h.of(1)[0].durationMs == 12)
    for i in 0..<1005 { h.started(pane: 3, id: i, command: "c\(i)", cwd: "", at: t0.addingTimeInterval(Double(i))) }
    #expect(h.of(3).count == 1000 && h.of(3)[0].command == "c5")
    h.started(pane: 4, id: 1, command: "early", cwd: "", at: t0.addingTimeInterval(-5))
    #expect(h.merged([1, 4]).map(\.command) == ["early", "ls"])
    h.clear(3)
    #expect(h.of(3).isEmpty)
}

@Test func pinStore() {
    var p = Pins()
    p.add(.init(title: "make", text: "a", paneId: 1, blockId: 7, cwd: "/", running: true))
    p.add(.init(title: "Selection", text: "sel", paneId: 1, blockId: nil, cwd: "/"))
    p.add(.init(title: "make", text: "b", paneId: 1, blockId: 7, cwd: "/", running: true))
    #expect(p.list.count == 2 && p.list[1].text == "b" && p.list[1].id == 1)
    p.update(pane: 1, block: 7, text: "done", done: (0, 50))
    p.update(pane: 1, block: 7, text: "later")
    #expect(p.list[1].text == "done" && !p.list[1].running && p.live.isEmpty)
    p.add(.init(title: "big", text: String(repeating: "x", count: 200_005), paneId: 2, blockId: 1, cwd: "/"))
    #expect(p.list[0].truncated && p.list[0].text.count == 200_000)
    for i in 0..<40 { p.add(.init(title: "s\(i)", text: "", paneId: 3, blockId: nil, cwd: "/")) }
    #expect(p.list.count == 30 && p.list[0].title == "s39")
}

@Test func exports() {
    var cal = Calendar(identifier: .gregorian)
    cal.timeZone = TimeZone(identifier: "UTC")!
    let at = cal.date(from: DateComponents(year: 2026, month: 10, day: 9, hour: 23, minute: 5))!
    #expect(Export.name("kubectl get pods -n shop", at: at, calendar: cal) == "wtf-kubectl-get-pods-n-shop-2026-10-09-2305")
    #expect(Export.name("~/", at: at, calendar: cal) == "wtf-terminal-2026-10-09-2305")
    #expect(Export.elapsed(65) == "1:05" && Export.elapsed(3725) == "1:02:05")
    #expect(Export.code("echo `date`") == "`` echo `date` ``")
    #expect(Export.fence("a ``` b") == "````")
    let utc = TimeZone(identifier: "UTC")!
    let started = cal.date(from: DateComponents(year: 2026, month: 10, day: 9, hour: 23, minute: 45, second: 1))!
    let md = Export.markdown([
        .init(text: "kubectl get pods", output: "NAME READY\nweb-1 1/1", startedAt: started, exitCode: 0, durationMs: 1000),
        .init(text: "false", output: "", exitCode: 1, durationMs: 0),
        .init(text: "tail -f log", output: nil),
        .init(text: "x", output: "y"),
    ], title: "ops", cwd: "~/ops", exportedAt: at, locale: Locale(identifier: "en_US"), timeZone: utc)
    #expect(md.contains("## 1. `kubectl get pods`\n\n✓ succeeded · started 23:45:01 · took 1s\n\n```text\nNAME READY\nweb-1 1/1\n```"))
    #expect(md.contains("## 2. `false`\n\n✗ exit 1 · took 0.0s\n\n_No output._"))
    #expect(md.contains("## 3. `tail -f log`\n\nstill running\n\n_Output no longer in the scrollback._"))
    #expect(md.contains("~/ops · 4 commands") && md.hasPrefix("# ops\n\nTerminal transcript from wtf, saved Oct 9, 2026 23:05:00"))
}

@Test func findingGroups() {
    let fs = [
        Finding(tool: "trivy", ruleId: "CVE-1", rule: "lib bad", severity: "high", message: "m", path: "/r/b.go", line: 3),
        Finding(tool: "trivy", ruleId: "CVE-1", rule: "lib bad", severity: "high", message: "m", path: "/r/a.go", line: 9),
        Finding(tool: "semgrep", ruleId: "x", severity: "warning", message: "eval used", path: "/r/c.py"),
        Finding(tool: "gitleaks", ruleId: "aws", severity: "critical", message: "key"),
    ]
    let g = Findings.group(fs, filter: "")
    #expect(g.map(\.severity) == ["critical", "high", "warning"])
    #expect(g[1].rules[0].items.map(\.path) == ["/r/a.go", "/r/b.go"] && g[1].count == 2)
    #expect(g[2].rules[0].title == "x")
    #expect(Findings.group(fs, filter: "EVAL c.py").map(\.severity) == ["warning"])
    #expect(Findings.where(fs[0], root: "/r") == "b.go:3" && Findings.where(fs[3], root: "/r") == "")
    #expect(Findings.stale("/r/sub", root: "/r", loaded: "/r") == false && Findings.stale("/x", root: "/r", loaded: "/r"))
    #expect(Findings.isScan("trivy fs .") && Findings.isScan("(cd /r && semgrep scan)") && !Findings.isScan("trivyx"))
    #expect(Findings.tone("medium") == .warn && Findings.tone("note") == .dim)
}

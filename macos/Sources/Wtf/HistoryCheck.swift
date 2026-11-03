// WTF_NATIVE_DEBUG=history: saved history (#17), as linux's
// selfcheck_history.go, in a throwaway store (backend.go). A shell points at
// a Kubernetes context and an AWS profile and region and runs commands, one
// with a secret; they're saved newest first with that context, without the
// secret; the Kubernetes filter and search narrow them; the History tab's
// Saved view shows them; forgetting the shell's session empties it.
import AppKit
import WtfCore

@MainActor
enum HistoryCheck {
    private static var failed = false

    private static func check(_ ok: Bool, _ what: String) {
        print((ok ? "PASS " : "FAIL ") + what)
        fflush(stdout)
        if !ok { failed = true }
    }

    private static func waitUntil(_ seconds: Double, _ cond: () async -> Bool) async -> Bool {
        let end = ContinuousClock.now + .seconds(seconds)
        while ContinuousClock.now < end {
            if await cond() { return true }
            try? await Task.sleep(for: .milliseconds(100))
        }
        return await cond()
    }

    static func run(_ w: WindowController) async {
        defer {
            print(failed ? "SOME FAILED" : "ALL PASSED")
            fflush(stdout)
            exit(failed ? 1 : 0)
        }
        let dir = NSTemporaryDirectory() + "wtf-history-kube-\(getpid())"
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        let kube = dir + "/config"
        try? "apiVersion: v1\nkind: Config\ncurrent-context: prod\ncontexts:\n- name: prod\n  context: {cluster: c1, namespace: shop}\nclusters:\n- name: c1\n  cluster: {server: https://x}\n"
            .write(toFile: kube, atomically: true, encoding: .utf8)
        let m = w.model
        guard let id = m.active?.focusedPaneId, let term = w.terminals[id] else { return check(false, "a shell") }
        check(await waitUntil(20) { m.panes[id]?.integrated == true && m.panes[id]?.running == false }, "shell integration")
        term.type("export KUBECONFIG=\(Shell.quote(kube)) AWS_PROFILE=ops AWS_REGION=eu-west-1\r")
        check(await waitUntil(10) { m.panes[id]?.env?["AWS_REGION"] == "eu-west-1" }, "the shell reports its context")
        for c in ["echo first-saved", "echo token=ghp_abcdefghijklmnopqrstuvwxyz0123456789", "false"] {
            term.type(c + "\r")
            check(await waitUntil(10) { term.lastFinishedBlock.flatMap { term.block($0) }?.text == c }, "ran \(c)")
        }
        func search(_ q: Go.cmdhistory.Query = .init()) async -> [Go.cmdhistory.Entry] {
            (try? await Services.cmdhistory.Search(q))?.entries ?? []
        }
        check(await waitUntil(5) { await search().count >= 4 }, "the commands are saved")
        let all = await search()
        if all.count >= 2 {
            let e = all[0]
            check(e.command == "false" && e.exitCode == 1, "newest first, with its exit code: \(e.command) \(e.exitCode)")
            check(e.kubeContext == "prod" && e.kubeNamespace == "shop" && e.awsProfile == "ops" && e.awsRegion == "eu-west-1",
                  "with its context: \(e.kubeContext)/\(e.kubeNamespace), \(e.awsProfile) \(e.awsRegion)")
            check(all[1].command.hasPrefix("echo token=") && !all[1].command.contains("ghp_"), "secrets are hidden before saving: \(all[1].command)")
        }
        check(await search(.init(text: "first")).map(\.command) == ["echo first-saved"], "search narrows it")
        check(await search(.init(kubeContext: "prod")).count == 3, "the prod filter: 3 (the export ran before it)")

        w.sidebar.historyScope = "saved"
        w.showSidebar(.history)
        check(await waitUntil(5) { (w.sidePane as? HistoryView)?.debugSummary().contains("saved: 4") == true },
              "History's Saved view shows them: \((w.sidePane as? HistoryView)?.debugSummary() ?? "none")")

        if let s = m.panes[id]?.sessionId { try? await Services.cmdhistory.ForgetSession(s) }
        SavedHistory.changed()
        check(await waitUntil(5) { await search().isEmpty }, "forgetting the shell's session empties it")
        check(await waitUntil(5) { (w.sidePane as? HistoryView)?.debugSummary().contains("saved: 0") == true }, "and the view")
    }
}

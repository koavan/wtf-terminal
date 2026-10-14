import Testing
@testable import WtfCore

@Test func placeholderFieldsInOrderOfFirstUse() {
    let f = Placeholders.fields(in: ["kubectl -n {{ns}} logs {{pod}}", "{{ns=prod}} {{end}} {{ else }}"],
                                params: [Param(name: "pod", description: "which pod", choices: ["a", "b"])])
    #expect(f.map(\.name) == ["ns", "pod"])
    #expect(f[0].default == "prod")
    #expect(f[1].description == "which pod" && f[1].choices == ["a", "b"])
}

@Test func fillQuotesUnlessRaw() throws {
    let fields = [Field(name: "q", description: "", default: "", choices: [], raw: false),
                  Field(name: "r", description: "", default: "", choices: [], raw: true)]
    #expect(try Placeholders.fill("grep {{q}} | {{r}} {{end}}", ["q": "a b", "r": "wc -l"], fields) == "grep 'a b' | wc -l {{end}}")
    #expect(throws: Placeholders.Empty.self) { try Placeholders.fill("x {{q}}", [:], fields) }
    #expect(try Placeholders.fillPlain("~/{{q}}", ["q": " src "]) == "~/src")
}

@Test func defaultsFromTheShell() {
    var f = ShellFacts()
    f.kubeNamespace = "api"
    f.cwd = "/w"
    #expect(Placeholders.resolve("$kube_namespace", f) == "api")
    #expect(Placeholders.resolve(" $cwd ", f) == "/w")
    #expect(Placeholders.resolve("$other", f) == "$other")
    #expect(Placeholders.resolve("literal", f) == "literal")
}

@Test func plansAndOpensAWorkspace() throws {
    let w = Workspace(name: "on call", tabs: [
        .init(title: "{{ns}} pods", group: "oncall", color: "red", layout: "row",
              panes: [.init(cwd: "~/src", command: "kubectl -n {{ns}} get pods"), .init(cwd: "", command: "k9s"), .init(cwd: "/tmp", command: "")]),
        .init(group: "oncall", cwd: "~"),
    ])
    let plan = try w.plan(["ns": "shop"], home: "/h")
    #expect(plan.tabs[0].title == "shop pods")
    #expect(plan.tabs[0].panes.map(\.cwd) == ["/h/src", "", "/tmp"])
    #expect(plan.tabs[0].panes[0].command == "kubectl -n shop get pods")
    #expect(plan.tabs[1].panes == [.init(cwd: "/h", command: "")])

    let m = WindowModel()
    m.open(cwd: "/existing")
    let first = m.open(plan: plan)!
    #expect(m.tabs.count == 3 && m.activeId == first)
    let t = m.tab(first)!
    #expect(t.root.leaves.count == 3 && t.focusedPaneId == t.root.leaves[0])
    #expect(t.customTitle == "shop pods")
    // Three panes side by side, a third each.
    let widths = t.root.frames().values.map(\.w).sorted()
    #expect(widths.allSatisfy { abs($0 - 1.0 / 3) < 1e-9 })
    // The empty folder falls back to the tab's first pane's.
    #expect(t.root.leaves.map { m.panes[$0]!.initialCwd } == ["/h/src", "/h/src", "/tmp"])
    #expect(m.panes[t.root.leaves[1]]!.initialCommand == "k9s")
    #expect(m.groups.count == 1 && m.groups[0].name == "oncall" && m.groups[0].color == .red && m.tabsIn(m.groups[0].id).count == 2)
}

@Test func capturesTheWindowAsAWorkspace() {
    let m = WindowModel()
    let a = m.open(cwd: "/h/src")
    m.split(.row, cwd: "/h/logs")
    m.activePane!.running = true
    m.activePane!.command = "tail -f app.log"
    m.rename(a, "api")
    let g = m.createGroup(a)!
    m.setGroup(g, name: "on call", color: .red)
    m.open(cwd: "/tmp")
    let d = m.captureWorkspace(home: "/h")
    #expect(d.tabs.count == 2)
    #expect(d.tabs[0].title == "api" && d.tabs[0].group == "on call" && d.tabs[0].color == "red" && d.tabs[0].layout == "row")
    #expect(d.tabs[0].panes == [.init(cwd: "~/src", command: ""), .init(cwd: "~/logs", command: "tail -f app.log")])
    #expect(d.tabs[1].title == "" && d.tabs[1].layout == "" && d.tabs[1].panes == [.init(cwd: "/tmp", command: "")])
}

@Test func convertsAListedWorkspace() {
    let g = Go.snippets.Workspace(id: "f#w0", name: "x", window: "new",
                                  tabs: [.init(title: "t", cwd: "~", panes: [])], params: [.init(name: "ns", default: "$kube_namespace")],
                                  source: "/f", project: true)
    let w = Workspace(g)
    #expect(w.name == "x" && w.window == "new" && w.project && w.source == "/f")
    #expect(w.tabs[0].allPanes == [.init(cwd: "~", command: "")])
    #expect(w.params[0].default == "$kube_namespace")
}

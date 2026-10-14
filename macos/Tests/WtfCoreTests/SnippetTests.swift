import Testing
@testable import WtfCore

@Test func snippetFields() throws {
    let s = Snippet(name: "Restart", command: "kubectl -n {{ns=$kube_namespace}} rollout restart deploy/{{name}} {{end}}",
                    params: [.init(name: "name", description: "The deployment", choices: ["web", "worker"])])
    #expect(s.fields.map(\.name) == ["ns", "name"])
    #expect(s.fields[1].description == "The deployment" && s.fields[0].default == "$kube_namespace")
    var facts = ShellFacts()
    facts.kubeNamespace = "prod-ns"
    #expect(s.fields[0].initialValue(facts) == "prod-ns")
    #expect(s.fields[1].initialValue(facts) == "web")
    #expect(try Placeholders.fill(s.command, ["ns": "a b", "name": "web"], s.fields) == "kubectl -n 'a b' rollout restart deploy/web {{end}}")
    #expect(throws: Placeholders.Empty.self) { try Placeholders.fill(s.command, ["ns": "x"], s.fields) }
}

@Test func runbooks() throws {
    let r = Snippet(name: "Drain", tags: ["k8s"], steps: [.init(title: "Cordon", command: "kubectl cordon {{node}}"),
                                                          .init(command: "kubectl drain {{node}} {{flags=--force}}")],
                    params: [.init(name: "flags", raw: true)])
    #expect(r.isRunbook && r.fields.map(\.name) == ["node", "flags"])
    #expect(r.subtitle == "Cordon → step 2")
    #expect(r.tooltip == "1. kubectl cordon {{node}}\n2. kubectl drain {{node}} {{flags=--force}}")
    #expect(try Placeholders.fill(r.steps[1].command, ["node": "my node's", "flags": "--force --grace-period=0"], r.fields)
        == "kubectl drain 'my node'\\''s' --force --grace-period=0")
    #expect(r.matches("K8S") && r.matches("drain") && !r.matches("helm"))
}

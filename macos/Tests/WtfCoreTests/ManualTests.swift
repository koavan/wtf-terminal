import Foundation
import Testing
@testable import WtfCore

var ctx: Manual.Context { Manual.Context(label: { ["sidebar.ssh": "Show SSH Hosts", "tab.close": "Close Tab"][$0] },
                         shortcut: { ["sidebar.ssh": "⌘⇧S"][$0] }) }

@Test func manualEscapes() {
    let r = Manual.render("# A <b>title</b>\n\nRun `rm -rf <dir>` & <script>alert(1)</script>", ctx)
    #expect(r.html.contains("<h1>A &lt;b&gt;title&lt;/b&gt;</h1>"))
    #expect(r.html.contains("<code>rm -rf &lt;dir&gt;</code> &amp; &lt;script&gt;"))
    #expect(!r.html.contains("<script>"))
}

@Test func manualSubset() {
    let src = ["## Lists", "- one **bold**", "- two *italic*", "  continued", "", "1. first", "2. second", "",
               "| A | B |", "|---|---|", "| `x` | y |", "", "> A tip", "", "```", "echo <hi>", "```"].joined(separator: "\n")
    let html = Manual.render(src, ctx).html
    #expect(html.contains("<ul><li>one <b>bold</b></li><li>two <i>italic</i> continued</li></ul>"))
    #expect(html.contains("<ol><li>first</li><li>second</li></ol>"))
    #expect(html.contains("<table><thead><tr><th>A</th><th>B</th></tr></thead><tbody><tr><td><code>x</code></td><td>y</td></tr></tbody></table>"))
    #expect(html.contains("<aside>A tip</aside>"))
    #expect(html.contains("<pre><code>echo &lt;hi&gt;</code></pre>"))
}

@Test func manualCommands() {
    let r = Manual.render("Open {{cmd:sidebar.ssh}}, press {{key:sidebar.ssh}}, or {{cmd:tab.close}}. {{mac:⌘-click|other:Ctrl-click}} {{cmd:nope}}", ctx)
    #expect(r.html.contains("<b>Show SSH Hosts</b> <kbd>⌘⇧S</kbd>, press <kbd>⌘⇧S</kbd>, or <b>Close Tab</b> (command palette). ⌘-click"))
    #expect(r.missing == ["nope"])
}

@Test func manualLinks() {
    let r = Manual.render("See [tabs](#tabs), [docs](https://example.com/x?a=1&b=2) and [bad](javascript:alert(1)).", ctx)
    #expect(r.links == ["tabs"])
    #expect(r.html.contains("<a href=\"wtf-manual:tabs\">tabs</a>"))
    #expect(r.html.contains("href=\"https://example.com/x?a=1&amp;b=2\""))
    #expect(!r.html.contains("javascript"))
}

@Test func manualPlainTextAndSearch() {
    #expect(Manual.plainText("# SSH **Keys**\n{{cmd:x}} use `ssh-add` [agent](#a)") == " ssh keys use ssh-add agent")
    let t = Manual.topics
    let pages = [(t[0], "open a tab"), (t[1], "groups of tabs")]
    #expect(Manual.search("TAB groups", pages: pages).map(\.id) == ["tabs"])
    #expect(Manual.search("", pages: pages).count == 2)
}

@Test func manualTopicsMatchPages() throws {
    let dir = URL(fileURLWithPath: #filePath).deletingLastPathComponent().appendingPathComponent("../../../frontend/src/manual")
    let files = try FileManager.default.contentsOfDirectory(atPath: dir.path).filter { $0.hasSuffix(".md") }.map { String($0.dropLast(3)) }
    #expect(Set(files) == Set(Manual.topics.map(\.id)))
    #expect(Set(Manual.topics.map(\.id)).count == Manual.topics.count)
    // Every page links only to real pages, and starts with its own title.
    for t in Manual.topics {
        let src = try String(contentsOf: dir.appendingPathComponent(t.id + ".md"), encoding: .utf8)
        #expect(src.trimmingCharacters(in: .whitespacesAndNewlines).hasPrefix("# "), "\(t.id)")
        let r = Manual.render(src, Manual.Context(label: { $0 }, shortcut: { _ in nil }))
        for l in r.links { #expect(Manual.topics.contains { $0.id == l }, "\(t.id) links to #\(l)") }
        #expect(r.html.range(of: #"\{\{(cmd|key|mac):"#, options: .regularExpression) == nil, "\(t.id)")
    }
}

@Test func helpTopics() {
    var s = SidebarState()
    #expect(Manual.helpTopic(s) == "getting-started")
    s.open = true
    s.tab = .ssh
    s.sshSection = "keys"
    #expect(Manual.helpTopic(s) == "ssh-keys")
    s.tab = .kube
    s.kubeView = "contexts"
    #expect(Manual.helpTopic(s) == "kube-contexts")
    s.tab = .findings
    #expect(Manual.helpTopic(s) == "findings")
    for tab in SidebarTab.allCases { #expect(Manual.topics.contains { $0.id == Manual.sidebarTopic[tab] }, "\(tab)") }
}

@Test func missingTools() {
    #expect(Tools.missingTool("kubectl isn't installed (or isn't on the shell's PATH)") == "kubectl")
    #expect(Tools.missingTool("the aws CLI isn't installed") == "aws")
    #expect(Tools.missingTool("lsof isn't installed") == "lsof")
    #expect(Tools.missingTool("permission denied") == nil && Tools.missingTool(nil) == nil)
}

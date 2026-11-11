// The in-app manual: the Markdown pages in manual/*.md (shared with the
// Linux app), rendered to HTML for a WKWebView. All text is escaped first; the only
// markup is the renderer's own. Page links become wtf-manual:<id> and
// outside links stay https://, so the web view needs no JavaScript.
import Foundation

public enum Manual {
    public struct Topic: Equatable, Sendable {
        public let group: String, id: String, title: String
    }

    public static let topics: [Topic] = [
        ("Basics", "getting-started", "Welcome to wtf"), ("Basics", "tabs", "Tabs and tab groups"),
        ("Basics", "panes", "Split panes"), ("Basics", "shell-integration", "Commands, prompts and output"),
        ("Basics", "logs", "Logs in the output"), ("Basics", "inspect", "Tokens and certificates"),
        ("Basics", "secrets", "Hide Secrets (screen sharing)"), ("Basics", "recording", "Recording and saving a terminal"),
        ("Basics", "find", "Find"), ("Basics", "context-badges", "Address bar and context badges"),
        ("Basics", "broadcast", "Broadcast input"), ("Basics", "snippets", "Snippets and runbooks"),
        ("Basics", "workspaces", "Workspaces"), ("Basics", "api", "API requests"),
        ("Side pane", "side-pane", "The side pane"), ("Side pane", "agents", "Agents: every Claude Code"), ("Side pane", "files", "Files"), ("Side pane", "network", "Network"),
        ("Side pane", "processes", "Processes"), ("Side pane", "history", "History"), ("Side pane", "pins", "Pinned output"),
        ("Side pane", "share", "Share & Record"), ("Side pane", "generate", "Generate: SSH keys and passwords"),
        ("Side pane", "findings", "Findings: security scans"),
        ("SSH", "ssh-hosts", "SSH hosts"), ("SSH", "ssh-keys", "SSH keys and ssh-agent"),
        ("SSH", "git-identities", "Git identities (more than one GitHub user)"), ("SSH", "ssh-host-keys", "Host keys (known_hosts)"),
        ("SSH", "ssh-tunnels", "SSH tunnels"), ("SSH", "ssh-files", "Copying files over SSH"),
        ("Cloud", "aws", "AWS profiles"), ("Cloud", "kubernetes", "Kubernetes: resources"), ("Cloud", "kube-contexts", "Kubernetes: contexts"),
        ("Cloud", "cloud-secrets", "Cloud secrets: Secrets Manager, Parameter Store, Vault"),
        ("Reference", "tools", "Command-line tools"), ("Reference", "ask-claude", "Ask Claude (Claude Code)"), ("Reference", "ai", "AI assistance"),
        ("Reference", "settings", "Settings and themes"),
        ("Reference", "usage", "Resource usage (wtf’s CPU and memory)"), ("Reference", "sessions", "Sessions and windows"),
        ("Reference", "shortcuts", "Keyboard shortcuts"), ("Reference", "privacy", "Privacy and safety"),
        ("Reference", "troubleshooting", "Troubleshooting"),
    ].map { Topic(group: $0.0, id: $0.1, title: $0.2) }

    /// The page for each side-pane tab, for its ? button.
    public static let sidebarTopic: [SidebarTab: String] = [
        .files: "files", .ssh: "ssh-hosts", .aws: "aws", .kube: "kubernetes", .snippets: "snippets", .workspaces: "workspaces",
        .network: "network", .processes: "processes", .history: "history", .pins: "pins", .share: "share",
        .generate: "generate", .findings: "findings", .investigate: "ai", .agents: "agents", .secrets: "cloud-secrets",
    ]

    /// Help for what's in front: the side pane's tab (and section), or the welcome page.
    public static func helpTopic(_ s: SidebarState) -> String {
        guard s.open else { return "getting-started" }
        if s.tab == .ssh {
            return ["hosts": "ssh-hosts", "keys": "ssh-keys", "git": "git-identities", "tunnels": "ssh-tunnels"][s.sshSection] ?? "ssh-hosts"
        }
        if s.tab == .kube, s.kubeView == "contexts" { return "kube-contexts" }
        return sidebarTopic[s.tab] ?? "getting-started"
    }

    public struct Context {
        public var label: (String) -> String?
        public var shortcut: (String) -> String?
        public init(label: @escaping (String) -> String?, shortcut: @escaping (String) -> String?) {
            self.label = label
            self.shortcut = shortcut
        }
    }

    public struct Rendered: Equatable, Sendable {
        public var html = ""
        /// Command ids the page mentions that don't exist (tests keep this empty).
        public var missing: [String] = []
        /// Topic ids the page links to.
        public var links: [String] = []
    }

    public static func escape(_ s: String) -> String {
        s.replacingOccurrences(of: "&", with: "&amp;").replacingOccurrences(of: "<", with: "&lt;")
            .replacingOccurrences(of: ">", with: "&gt;").replacingOccurrences(of: "\"", with: "&quot;")
    }

    /// Replaces each match of a pattern with what `with` makes of its groups.
    static func replace(_ s: String, _ pattern: String, _ with: ([String]) -> String) -> String {
        let re = try! NSRegularExpression(pattern: pattern)
        let ns = s as NSString
        var out = ""
        var last = 0
        for m in re.matches(in: s, range: NSRange(location: 0, length: ns.length)) {
            out += ns.substring(with: NSRange(location: last, length: m.range.location - last))
            let groups = (0..<m.numberOfRanges).map { i in
                m.range(at: i).location == NSNotFound ? "" : ns.substring(with: m.range(at: i))
            }
            out += with(groups)
            last = m.range.location + m.range.length
        }
        return out + ns.substring(from: last)
    }

    static func inline(_ text: String, _ ctx: Context, _ out: inout Rendered) -> String {
        // Code spans first, so nothing inside them is touched.
        var codes: [String] = []
        var s = replace(text, "`([^`]+)`") { g in
            codes.append("<code>\(escape(g[1]))</code>")
            return "\u{0}\(codes.count - 1)\u{0}"
        }
        s = escape(s)
        var missing: [String] = []
        s = replace(s, #"\{\{(cmd|key):([\w.]+)\}\}"#) { g in
            let (kind, id) = (g[1], g[2])
            guard let label = ctx.label(id) else {
                missing.append(id)
                return "<b>\(escape(id))</b>"
            }
            let key = ctx.shortcut(id).flatMap { $0.isEmpty ? nil : $0 }
            let kbd = key.map { "<kbd>\(escape($0))</kbd>" } ?? ""
            if kind == "key" { return key != nil ? kbd : "<b>\(escape(label))</b> in the command palette" }
            return key != nil ? "<b>\(escape(label))</b> \(kbd)" : "<b>\(escape(label))</b> (command palette)"
        }
        out.missing += missing
        s = replace(s, #"\{\{mac:([^|}]*)\|other:([^}]*)\}\}"#) { $0[1] }
        s = replace(s, #"\*\*([^*]+)\*\*"#) { "<b>\($0[1])</b>" }
        s = replace(s, #"(^|[\s(])\*([^*\s][^*]*)\*"#) { "\($0[1])<i>\($0[2])</i>" }
        var links: [String] = []
        s = replace(s, #"\[([^\]]+)\]\(([^)\s]+)\)"#) { g in
            let (label, href) = (g[1], g[2])
            if href.hasPrefix("#") {
                links.append(String(href.dropFirst()))
                return "<a href=\"wtf-manual:\(href.dropFirst())\">\(label)</a>"
            }
            if href.hasPrefix("https://") { return "<a href=\"\(href)\">\(label)</a>" }
            return label
        }
        out.links += links
        return replace(s, "\u{0}(\\d+)\u{0}") { codes[Int($0[1])!] }
    }

    public static func render(_ src: String, _ ctx: Context) -> Rendered {
        var out = Rendered()
        let lines = src.replacingOccurrences(of: "\r\n", with: "\n").components(separatedBy: "\n")
        var html: [String] = []
        var para: [String] = []
        func flush() {
            if !para.isEmpty { html.append("<p>\(inline(para.joined(separator: " "), ctx, &out))</p>") }
            para = []
        }
        func indented(_ l: String) -> Bool { l.hasPrefix("  ") || l.hasPrefix("\t\t") || l.hasPrefix(" \t") || l.hasPrefix("\t ") }
        let listRe = try! NSRegularExpression(pattern: #"^\s*(-|\d+\.)\s+(.*)$"#)
        func listItem(_ l: String) -> (ordered: Bool, text: String)? {
            let ns = l as NSString
            guard let m = listRe.firstMatch(in: l, range: NSRange(location: 0, length: ns.length)) else { return nil }
            return (ns.substring(with: m.range(at: 1)) != "-", ns.substring(with: m.range(at: 2)))
        }
        var i = 0
        while i < lines.count {
            let t = lines[i].trimmingCharacters(in: .whitespaces)
            if t.hasPrefix("```") {
                flush()
                var code: [String] = []
                i += 1
                while i < lines.count, !lines[i].trimmingCharacters(in: .whitespaces).hasPrefix("```") {
                    code.append(lines[i])
                    i += 1
                }
                html.append("<pre><code>\(escape(code.joined(separator: "\n")))</code></pre>")
                i += 1
                continue
            }
            if t.isEmpty {
                flush()
                i += 1
                continue
            }
            if let r = t.range(of: #"^#{1,3}\s+"#, options: .regularExpression) {
                flush()
                let level = t[r].filter { $0 == "#" }.count
                html.append("<h\(level)>\(inline(String(t[r.upperBound...]), ctx, &out))</h\(level)>")
                i += 1
                continue
            }
            if t.hasPrefix("|") {
                flush()
                var rows: [[String]] = []
                while i < lines.count, lines[i].trimmingCharacters(in: .whitespaces).hasPrefix("|") {
                    var row = lines[i].trimmingCharacters(in: .whitespaces)
                    if row.hasPrefix("|") { row.removeFirst() }
                    if row.hasSuffix("|") { row.removeLast() }
                    let cells = row.components(separatedBy: "|").map { $0.trimmingCharacters(in: .whitespaces) }
                    if !cells.allSatisfy({ $0.range(of: "^:?-+:?$", options: .regularExpression) != nil }) { rows.append(cells) }
                    i += 1
                }
                guard let head = rows.first else { continue }
                html.append("<table><thead><tr>" + head.map { "<th>\(inline($0, ctx, &out))</th>" }.joined()
                    + "</tr></thead><tbody>"
                    + rows.dropFirst().map { "<tr>" + $0.map { "<td>\(inline($0, ctx, &out))</td>" }.joined() + "</tr>" }.joined()
                    + "</tbody></table>")
                continue
            }
            if t.range(of: #"^(-|\d+\.)\s+"#, options: .regularExpression) != nil {
                flush()
                let ordered = !t.hasPrefix("-")
                var items: [String] = []
                while i < lines.count {
                    let l = lines[i]
                    if let m = listItem(l), m.ordered == ordered {
                        items.append(m.text)
                    } else if !l.trimmingCharacters(in: .whitespaces).isEmpty, indented(l), !items.isEmpty {
                        items[items.count - 1] += " " + l.trimmingCharacters(in: .whitespaces)
                    } else {
                        break
                    }
                    i += 1
                }
                let tag = ordered ? "ol" : "ul"
                html.append("<\(tag)>" + items.map { "<li>\(inline($0, ctx, &out))</li>" }.joined() + "</\(tag)>")
                continue
            }
            if t.hasPrefix(">") {
                flush()
                var tip: [String] = []
                while i < lines.count, lines[i].trimmingCharacters(in: .whitespaces).hasPrefix(">") {
                    var l = lines[i].trimmingCharacters(in: .whitespaces)
                    l.removeFirst()
                    if l.hasPrefix(" ") { l.removeFirst() }
                    tip.append(l)
                    i += 1
                }
                html.append("<aside>\(inline(tip.joined(separator: " "), ctx, &out))</aside>")
                continue
            }
            para.append(t)
            i += 1
        }
        flush()
        out.html = html.joined(separator: "\n")
        return out
    }

    /// A page's words without markup, for searching.
    public static func plainText(_ src: String) -> String {
        var s = replace(src, #"\{\{(cmd|key):[\w.]+\}\}"#) { _ in " " }
        s = replace(s, #"\{\{mac:([^|}]*)\|other:([^}]*)\}\}"#) { "\($0[1]) \($0[2])" }
        s = replace(s, "[#*`>|]") { _ in " " }
        s = replace(s, #"\[([^\]]+)\]\([^)]+\)"#) { $0[1] }
        s = replace(s, #"\s+"#) { _ in " " }
        return s.lowercased()
    }

    /// Pages holding every word of the query (all pages for an empty one).
    public static func search(_ query: String, pages: [(Topic, String)]) -> [Topic] {
        let words = query.lowercased().split(whereSeparator: \.isWhitespace).map(String.init)
        guard !words.isEmpty else { return pages.map(\.0) }
        return pages.filter { t, text in
            let hay = t.title.lowercased() + " " + text
            return words.allSatisfy { hay.contains($0) }
        }.map(\.0)
    }
}

public enum Tools {
    /// The tool an error says is missing (for its Install… link), or nil.
    public static func missingTool(_ error: String?) -> String? {
        guard let e = error else { return nil }
        let table = [(#"\bkubectl isn't installed\b"#, "kubectl"), (#"\baws CLI isn't installed\b"#, "aws"), (#"\blsof isn't installed\b"#, "lsof"),
                     (#"\bvault CLI isn't installed\b"#, "vault")]
        return table.first { e.range(of: $0.0, options: .regularExpression) != nil }?.1
    }
}

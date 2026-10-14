// Workspaces: named sets of tabs (folders, split panes, tab groups and
// startup commands) opened in one go, from the snippets files
// (internal/snippets). A workspace becomes a plan once its placeholders are
// filled in; the plan is what opens. Ports of lib/workspaces.svelte.ts and
// the placeholder helpers in lib/snippets.svelte.ts.
import Foundation

public struct Param: Decodable, Equatable, Sendable {
    public var name = "", description = "", `default` = ""
    public var choices: [String] = []
    public var raw = false

    public init(name: String, description: String = "", default d: String = "", choices: [String] = [], raw: Bool = false) {
        self.name = name
        self.description = description
        self.default = d
        self.choices = choices
        self.raw = raw
    }

    enum CodingKeys: String, CodingKey { case name, description, `default`, choices, raw }
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = (try? c.decodeIfPresent(String.self, forKey: .name)) ?? ""
        description = (try? c.decodeIfPresent(String.self, forKey: .description)) ?? ""
        self.default = (try? c.decodeIfPresent(String.self, forKey: .default)) ?? ""
        choices = (try? c.decodeIfPresent([String].self, forKey: .choices)) ?? []
        raw = (try? c.decodeIfPresent(Bool.self, forKey: .raw)) ?? false
    }
}

public struct Workspace: Decodable, Equatable, Sendable {
    public struct WPane: Decodable, Equatable, Sendable {
        public var cwd = "", command = ""
        public init(cwd: String, command: String) { self.cwd = cwd; self.command = command }
    }

    public struct WTab: Decodable, Equatable, Sendable {
        public var title = "", group = "", color = "", cwd = "", command = "", layout = ""
        public var panes: [WPane] = []

        public init(title: String = "", group: String = "", color: String = "", cwd: String = "", command: String = "",
                    layout: String = "", panes: [WPane] = []) {
            self.title = title; self.group = group; self.color = color; self.cwd = cwd; self.command = command
            self.layout = layout; self.panes = panes
        }

        /// Its panes: the pane list, or its own folder and command.
        public var allPanes: [WPane] { panes.isEmpty ? [WPane(cwd: cwd, command: command)] : panes }

        enum CodingKeys: String, CodingKey { case title, group, color, cwd, command, layout, panes }
        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            func s(_ k: CodingKeys) -> String { (try? c.decodeIfPresent(String.self, forKey: k)) ?? "" }
            title = s(.title); group = s(.group); color = s(.color); cwd = s(.cwd); command = s(.command); layout = s(.layout)
            panes = (try? c.decodeIfPresent([WPane].self, forKey: .panes)) ?? []
        }
    }

    public var id = "", name = "", description = "", window = "", source = ""
    public var project = false
    public var tabs: [WTab] = []
    public var params: [Param] = []

    public init(name: String, window: String = "", tabs: [WTab], params: [Param] = []) {
        self.name = name
        self.window = window
        self.tabs = tabs
        self.params = params
    }

    enum CodingKeys: String, CodingKey { case id, name, description, window, source, project, tabs, params }
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        func s(_ k: CodingKeys) -> String { (try? c.decodeIfPresent(String.self, forKey: k)) ?? "" }
        id = s(.id); name = s(.name); description = s(.description); window = s(.window); source = s(.source)
        project = (try? c.decodeIfPresent(Bool.self, forKey: .project)) ?? false
        tabs = (try? c.decodeIfPresent([WTab].self, forKey: .tabs)) ?? []
        params = (try? c.decodeIfPresent([Param].self, forKey: .params)) ?? []
    }
}

/// snippets.List, as far as workspaces go.
public struct SnippetList: Decodable, Sendable {
    public var workspaces: [Workspace] = []
    public var problems: [String] = []

    enum CodingKeys: String, CodingKey { case workspaces, problems }
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        workspaces = (try? c.decodeIfPresent([Workspace].self, forKey: .workspaces)) ?? []
        problems = (try? c.decodeIfPresent([String].self, forKey: .problems)) ?? []
    }
}

/// One value a workspace (or snippet) asks for.
public struct Field: Equatable, Sendable {
    public var name: String
    public var description: String
    /// As written: a literal or a $variable.
    public var `default`: String
    public var choices: [String]
    public var raw: Bool
}

/// What the focused shell is pointed at, for $variable defaults.
public struct ShellFacts: Sendable {
    public var kubeContext = "", kubeNamespace = "", awsProfile = "", awsRegion = "", cwd = ""

    public init(pane: Pane?) {
        guard let pane else { return }
        let env = pane.env ?? [:]
        let infra = pane.infra
        kubeContext = infra?.kubeContext ?? ""
        kubeNamespace = (infra?.kubeNamespace).flatMap { $0.isEmpty ? nil : $0 } ?? (kubeContext.isEmpty ? "" : "default")
        if let p = infra?.awsProfile, !p.isEmpty, p != "env credentials" { awsProfile = p } else { awsProfile = env["AWS_PROFILE"] ?? "" }
        awsRegion = [infra?.awsRegion ?? "", env["AWS_REGION"] ?? "", env["AWS_DEFAULT_REGION"] ?? ""].first { !$0.isEmpty } ?? ""
        cwd = pane.cwd
    }

    public init() {}
}

public enum Placeholders {
    static let pattern = try! NSRegularExpression(pattern: #"\{\{([A-Za-z_][A-Za-z0-9_-]*)(?:=([^}]*))?\}\}"#)
    /// Words Go templates use bare: never placeholders.
    static let templateWords: Set<String> = ["end", "else", "break", "continue", "nil", "true", "false"]

    struct Hit { let range: NSRange; let name: String; let inline: String? }

    static func hits(_ text: String) -> [Hit] {
        let ns = text as NSString
        return pattern.matches(in: text, range: NSRange(location: 0, length: ns.length)).compactMap { m in
            let name = ns.substring(with: m.range(at: 1))
            let inline = m.range(at: 2).location == NSNotFound ? nil : ns.substring(with: m.range(at: 2))
            if templateWords.contains(name), inline == nil { return nil }
            return Hit(range: m.range, name: name, inline: inline)
        }
    }

    /// The placeholders in some texts, in order of first use, with their declared settings.
    public static func fields(in texts: [String], params: [Param]) -> [Field] {
        var out: [Field] = []
        for text in texts {
            for h in hits(text) {
                if let i = out.firstIndex(where: { $0.name == h.name }) {
                    if out[i].default.isEmpty, let inline = h.inline { out[i].default = inline }
                    continue
                }
                let p = params.first { $0.name == h.name }
                out.append(Field(name: h.name, description: p?.description ?? "", default: h.inline ?? p?.default ?? "",
                                 choices: p?.choices ?? [], raw: p?.raw ?? false))
            }
        }
        return out
    }

    /// A default with its $variable worked out ("" when the shell doesn't say).
    public static func resolve(_ def: String, _ f: ShellFacts) -> String {
        let t = def.trimmingCharacters(in: .whitespaces)
        guard t.hasPrefix("$"), t.dropFirst().allSatisfy({ $0.isLetter || $0.isNumber || $0 == "_" }), t.count > 1 else { return def }
        switch t.dropFirst() {
        case "kube_context": return f.kubeContext
        case "kube_namespace": return f.kubeNamespace
        case "aws_profile": return f.awsProfile
        case "aws_region": return f.awsRegion
        case "cwd": return f.cwd
        default: return def
        }
    }

    public struct Empty: LocalizedError {
        public let name: String
        public var errorDescription: String? { "Fill in \(name)" }
    }

    private static func replace(_ text: String, _ value: (Hit) throws -> String) throws -> String {
        var out = text as NSString
        for h in hits(text).reversed() { out = out.replacingCharacters(in: h.range, with: try value(h)) as NSString }
        return out as String
    }

    /// A command with its placeholders filled: values shell-quoted unless raw.
    public static func fill(_ command: String, _ values: [String: String], _ fields: [Field]) throws -> String {
        try replace(command) { h in
            let v = (values[h.name] ?? "").trimmingCharacters(in: .whitespaces)
            if v.isEmpty { throw Empty(name: h.name) }
            return fields.first { $0.name == h.name }?.raw == true ? v : Shell.quote(v)
        }
    }

    /// Text (a folder, a title) with its placeholders filled as typed.
    public static func fillPlain(_ text: String, _ values: [String: String]) throws -> String {
        try replace(text) { h in
            let v = (values[h.name] ?? "").trimmingCharacters(in: .whitespaces)
            if v.isEmpty { throw Empty(name: h.name) }
            return v
        }
    }
}

public struct Plan: Codable, Equatable, Sendable {
    public struct PlannedPane: Codable, Equatable, Sendable { public var cwd: String; public var command: String }
    public struct PlannedTab: Codable, Equatable, Sendable {
        public var title: String, group: String, color: String
        /// For several panes: side by side (row) or stacked (column).
        public var layout: String
        public var panes: [PlannedPane]
    }
    public var name: String
    public var tabs: [PlannedTab]
}

extension Workspace {
    /// The values it asks for, from every title, group, folder and command.
    public var fields: [Field] {
        var texts: [String] = []
        for t in tabs {
            texts += [t.title, t.group]
            for p in t.allPanes { texts += [p.cwd, p.command] }
        }
        return Placeholders.fields(in: texts, params: params)
    }

    /// The tabs it opens, placeholders filled. Throws naming the first field left empty.
    public func plan(_ values: [String: String], home: String = NSHomeDirectory()) throws -> Plan {
        let fields = self.fields
        func expandHome(_ p: String) -> String {
            if p == "~" { return home }
            return p.hasPrefix("~/") ? home + p.dropFirst() : p
        }
        return Plan(name: name, tabs: try tabs.map { t in
            Plan.PlannedTab(
                title: try Placeholders.fillPlain(t.title, values), group: try Placeholders.fillPlain(t.group, values), color: t.color,
                layout: t.layout == "column" ? "column" : "row",
                panes: try t.allPanes.map {
                    Plan.PlannedPane(cwd: expandHome(try Placeholders.fillPlain($0.cwd, values)),
                                     command: $0.command.isEmpty ? "" : try Placeholders.fill($0.command, values, fields))
                })
        })
    }
}

extension WindowModel {
    /// Opens a plan's tabs in this window, after the tabs already open:
    /// splits chained off the newest pane then evened out, names, groups
    /// (matched within the plan only). Returns the first opened tab.
    @discardableResult
    public func open(plan: Plan) -> Int? {
        var groups: [String: Int] = [:]
        var first: Int?
        for t in plan.tabs {
            guard let firstPane = t.panes.first else { continue }
            let id = open(cwd: firstPane.cwd, command: firstPane.command.isEmpty ? nil : firstPane.command)
            first = first ?? id
            for p in t.panes.dropFirst() {
                split(t.layout == "column" ? .column : .row, cwd: p.cwd.isEmpty ? firstPane.cwd : p.cwd,
                      command: p.command.isEmpty ? nil : p.command)
            }
            evenOut(id)
            if let first = tab(id)?.root.leaves.first { focusPane(first) }
            if !t.title.isEmpty { rename(id, t.title) }
            if !t.group.isEmpty {
                if let g = groups[t.group] {
                    addToGroup(id, g)
                } else if let g = createGroup(id) {
                    setGroup(g, name: t.group, color: GroupColor(rawValue: t.color))
                    groups[t.group] = g
                }
            }
        }
        if let first { activate(first) }
        return first
    }
}

extension Workspace {
    /// A workspace as snippets.List returns it.
    public init(_ g: Go.snippets.Workspace) {
        self.init(name: g.name, window: g.window,
                  tabs: g.tabs.map { t in
                      WTab(title: t.title, group: t.group, color: t.color, cwd: t.cwd, command: t.command, layout: t.layout,
                           panes: t.panes.map { WPane(cwd: $0.cwd, command: $0.command) })
                  },
                  params: g.params.map { Param(name: $0.name, description: $0.description, default: $0.default, choices: $0.choices, raw: $0.raw) })
        id = g.id
        description = g.description
        source = g.source
        project = g.project
    }
}

extension WindowModel {
    /// "Save Window as Workspace": every tab in strip order with its custom
    /// title, group and colour, its panes' folders (~ for home) and what runs
    /// in them now (an ssh host, `kubectl … -w`), and the root split's
    /// direction. Nested splits flatten to one direction.
    public func captureWorkspace(home: String = NSHomeDirectory()) -> Go.snippets.WorkspaceDef {
        Go.snippets.WorkspaceDef(tabs: tabs.map { t in
            let g = t.groupId.flatMap(group)
            var layout = ""
            if case .split(let d, _, _, _) = t.root { layout = d.rawValue }
            return Go.snippets.WTab(
                title: t.customTitle ?? "", group: g?.name ?? "", color: g?.color.rawValue ?? "", layout: layout,
                panes: t.root.leaves.compactMap { panes[$0] }.map {
                    Go.snippets.WPane(cwd: WindowModel.prettyPath($0.cwd, home: home), command: $0.running ? $0.command : "")
                })
        })
    }
}

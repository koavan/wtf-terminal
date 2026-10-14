// The Files tab's logic: the folder tree from cached listings (filtered,
// expanded), git decorations, and the git menu's commands. Ports of
// frontend/src/lib/gitstatus.ts, fileActions.ts and FileTree.svelte's rules.
import Foundation

public enum Paths {
    /// `path` relative to `base` when inside it ("src/main.go"), else absolute.
    public static func relative(_ base: String, _ path: String) -> String {
        if base.isEmpty { return path }
        if path == base { return "." }
        let b = base.hasSuffix("/") ? base : base + "/"
        return path.hasPrefix(b) ? String(path.dropFirst(b.count)) : path
    }

    public static func parent(_ path: String) -> String {
        guard let i = path.lastIndex(of: "/"), i > path.startIndex else { return "/" }
        return String(path[..<i])
    }

    public static func base(_ path: String) -> String {
        path == "/" ? "/" : String(path.split(separator: "/").last ?? Substring(path))
    }

    /// Whether `path` is `dir` or inside it.
    public static func inside(_ path: String, _ dir: String) -> Bool {
        path == dir || path.hasPrefix(dir == "/" ? "/" : dir + "/")
    }
}

public enum ChangeKind: String, CaseIterable, Sendable {
    case modified, added, deleted, renamed, untracked, conflict

    /// The letter beside a changed file, as in most editors.
    public var letter: String {
        switch self {
        case .modified: "M"
        case .added: "A"
        case .deleted: "D"
        case .renamed: "R"
        case .untracked: "U"
        case .conflict: "!"
        }
    }

    public var label: String {
        switch self {
        case .modified: "Modified"
        case .added: "Added"
        case .deleted: "Deleted"
        case .renamed: "Renamed"
        case .untracked: "Untracked (new)"
        case .conflict: "Merge conflict"
        }
    }

    /// Which kind wins when a folder holds several: the one that most needs attention.
    static let rank: [ChangeKind] = [.conflict, .modified, .deleted, .renamed, .added, .untracked]
}

/// What the file tree shows of git, built once per `git status`.
public struct GitIndex: Sendable {
    public let status: Go.files.GitStatus
    private var files: [String: Go.files.GitChange] = [:]
    /// Untracked folders: everything inside them is new.
    private var newFolders: [String] = []
    /// Folders holding changes, with the kind that matters most.
    private var folders: [String: ChangeKind] = [:]

    public init(_ s: Go.files.GitStatus) {
        status = s
        for c in s.changes {
            let isDir = c.path.hasSuffix("/")
            let path = isDir ? String(c.path.dropLast()) : c.path
            if isDir { newFolders.append(path) } else { files[path] = c }
            let kind = ChangeKind(rawValue: c.status) ?? .modified
            var dir = Paths.parent(path)
            while dir.count >= s.root.count, dir != path {
                if let was = folders[dir], ChangeKind.rank.firstIndex(of: was)! <= ChangeKind.rank.firstIndex(of: kind)! {} else {
                    folders[dir] = kind
                }
                if dir == s.root || dir == "/" { break }
                dir = Paths.parent(dir)
            }
            if isDir { folders[path] = .untracked }
        }
    }

    /// A file's change, or nil when it's unchanged (or not in the repository).
    public func file(_ path: String) -> Go.files.GitChange? {
        if let c = files[path] { return c }
        return newFolders.contains { path.hasPrefix($0 + "/") }
            ? Go.files.GitChange(path: path, status: "untracked", staged: false, unstaged: true) : nil
    }

    /// The most pressing kind of change inside a folder, or nil.
    public func folder(_ path: String) -> ChangeKind? {
        folders[path] ?? (newFolders.contains { Paths.inside(path, $0) } ? .untracked : nil)
    }

    public var count: Int { status.changes.count }

    /// "main ↑2 ↓1"
    public static func branchLabel(_ s: Go.files.GitStatus) -> String {
        var parts = [s.detached ? "detached at \(s.branch)" : (s.branch.isEmpty ? "no commits yet" : s.branch)]
        if s.ahead > 0 { parts.append("↑\(s.ahead)") }
        if s.behind > 0 { parts.append("↓\(s.behind)") }
        return parts.joined(separator: " ")
    }

    /// "Modified · staged and unstaged"
    public static func summary(_ c: Go.files.GitChange) -> String {
        let kind = ChangeKind(rawValue: c.status) ?? .modified
        if kind == .untracked || kind == .conflict { return kind.label }
        return kind.label + (c.staged && c.unstaged ? " · staged and unstaged" : c.staged ? " · staged" : " · not staged")
    }

    public enum MenuItem: Equatable {
        case heading(String)
        /// A git command to run from the repository root, in a new tab.
        case run(label: String, command: String)
    }

    /// The git part of a file or folder's menu.
    public func menu(_ path: String, dir: Bool) -> [MenuItem] {
        let root = status.root
        guard !root.isEmpty, Paths.inside(path, root) else { return [] }
        let rel = Shell.quote(Paths.relative(root, path))
        var out: [MenuItem] = []
        if dir {
            if folder(path) != nil {
                out += [.heading("Git: changes inside"),
                        .run(label: "Show Status of Folder", command: "git status -- \(rel)"),
                        .run(label: "Show Changes in Folder", command: "git diff HEAD -- \(rel)")]
            }
            out.append(.run(label: "Show Folder History", command: "git log --oneline -- \(rel)"))
            return out
        }
        let c = file(path)
        if let c {
            let diff = switch c.status {
            case "untracked": "git diff --no-index -- /dev/null \(rel)"
            case "conflict": "git diff -- \(rel)"
            default: c.staged && !c.unstaged ? "git diff --cached -- \(rel)" : "git diff HEAD -- \(rel)"
            }
            out += [.heading("Git: \(Self.summary(c))"), .run(label: "Show Changes", command: diff)]
        }
        if c?.status != "untracked", c?.status != "added" {
            out.append(.run(label: "Show History", command: "git log --follow --oneline -- \(rel)"))
        }
        return out
    }
}

/// The folder tree: cached listings, which folders are open, and the rows
/// they make (folders first, as Go sorts them).
public struct FileTree {
    public struct Folder: Sendable {
        public var entries: [Go.files.Entry]?
        public var error: String?
        public var truncated = false

        public init(entries: [Go.files.Entry]?, error: String? = nil, truncated: Bool = false) {
            self.entries = entries
            self.error = error
            self.truncated = truncated
        }
    }

    public enum Row: Equatable {
        case entry(Go.files.Entry, depth: Int)
        /// A folder had more entries than were listed.
        case more(depth: Int)
    }

    public var folders: [String: Folder] = [:]
    public var expanded = Set<String>()

    public init() {}

    /// The rows under `root`. A filter keeps matching names, and the open
    /// folders leading to them.
    public func rows(_ root: String, filter: String = "") -> [Row] {
        let q = filter.trimmingCharacters(in: .whitespaces).lowercased()
        func walk(_ dir: String, _ depth: Int) -> [Row] {
            guard let f = folders[dir], let entries = f.entries else { return [] }
            var out: [Row] = []
            for e in entries {
                let below = e.dir && expanded.contains(e.path) ? walk(e.path, depth + 1) : []
                let match = q.isEmpty || e.name.lowercased().contains(q)
                let keptBelow = below.contains { if case .entry = $0 { true } else { false } }
                if match || keptBelow {
                    out.append(.entry(e, depth: depth))
                    out += below
                }
            }
            if f.truncated, q.isEmpty { out.append(.more(depth: depth)) }
            return out
        }
        return walk(root, 0)
    }

    /// Back history per key ("pinned", or "pane:<id>"), at most 50 each.
    public var history: [String: [String]] = [:]

    public mutating func visit(_ key: String, _ path: String) {
        var h = history[key] ?? []
        if h.last != path { h.append(path) }
        history[key] = Array(h.suffix(50))
    }
}

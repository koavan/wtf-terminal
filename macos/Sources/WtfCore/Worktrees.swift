// Git worktrees (#37): the bits of New Worktree… and the agent rows that
// don't need AppKit. Like internal/worktree (Slug) and internal/uicore.
import Foundation

public enum Worktrees {
    /// A branch name as a folder name: "feat/login fix" → "feat-login-fix".
    /// Must match worktree.Slug in Go.
    public static func slug(_ branch: String) -> String {
        var out = ""
        var dash = false
        for c in branch.trimmingCharacters(in: .whitespacesAndNewlines).unicodeScalars {
            let ok = (c >= "a" && c <= "z") || (c >= "A" && c <= "Z") || (c >= "0" && c <= "9") || c == "." || c == "_" || c == "-"
            if ok {
                out.unicodeScalars.append(c)
                dash = false
            } else if !dash, !out.isEmpty {
                out += "-"
                dash = true
            }
        }
        let trimmed = out.trimmingCharacters(in: CharacterSet(charactersIn: "-."))
        return trimmed.isEmpty ? "worktree" : trimmed
    }

    /// Where New Worktree… suggests putting `branch`.
    public static func suggest(_ repo: Go.worktree.Repo, branch: String) -> String {
        (repo.parent as NSString).appendingPathComponent(slug(branch))
    }

    /// The branches you can check out in a new worktree: those not checked
    /// out anywhere yet.
    public static func free(_ repo: Go.worktree.Repo) -> [String] {
        let used = Set(repo.worktrees.map(\.branch))
        return repo.branches.filter { !used.contains($0) }
    }

    /// The branch checked out where you asked from ("" when detached).
    public static func current(_ repo: Go.worktree.Repo) -> String {
        repo.worktrees.first { $0.path == repo.current }?.branch ?? ""
    }

    /// "feat/login ↑2 ↓1", "detached at 1a2b3c4"; "" outside a repository.
    public static func label(_ b: Go.worktree.Branch) -> String {
        guard !b.branch.isEmpty else { return "" }
        var parts = [b.detached ? "detached at \(b.branch)" : b.branch]
        if b.ahead > 0 { parts.append("↑\(b.ahead)") }
        if b.behind > 0 { parts.append("↓\(b.behind)") }
        return parts.joined(separator: " ")
    }

    /// A worktree in a menu or list: its branch (or commit) and folder.
    public static func title(_ w: Go.worktree.Worktree, home: String = NSHomeDirectory()) -> String {
        let name = w.branch.isEmpty ? "detached at \(w.head)" : w.branch
        var path = w.path
        if !home.isEmpty, path == home || path.hasPrefix(home + "/") { path = "~" + path.dropFirst(home.count) }
        return "\(name) — \(path)" + (w.main ? " (main checkout)" : "") + (w.prunable ? " (folder gone)" : "")
    }

    /// Whether `path` is `dir` or inside it.
    public static func inside(_ path: String, _ dir: String) -> Bool {
        path == dir || path.hasPrefix(dir.hasSuffix("/") ? dir : dir + "/")
    }

    /// The worktree `path` is in: the deepest one containing it.
    public static func containing(_ path: String, in repo: Go.worktree.Repo) -> Go.worktree.Worktree? {
        repo.worktrees.filter { inside(path, $0.path) }.max { $0.path.count < $1.path.count }
    }
}

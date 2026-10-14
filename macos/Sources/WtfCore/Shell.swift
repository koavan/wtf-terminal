// Shell quoting and the commands wtf types into shells. Ports of
// frontend/src/lib/shell.ts and useIdentityCommand in lib/gitIdentities.svelte.ts.
import Foundation

public enum Shell {
    /// Quotes for bash/zsh: as-is when safe, otherwise single-quoted.
    public static func quote(_ s: String) -> String {
        if !s.isEmpty, !s.hasPrefix("-"), s.range(of: #"^[A-Za-z0-9_@%+=:,./-]+$"#, options: .regularExpression) != nil { return s }
        return "'" + s.replacingOccurrences(of: "'", with: #"'\''"#) + "'"
    }

    /// Ctrl-U: clears a half-typed command line before wtf types one.
    public static let clearLine = "\u{15}"

    /// The variable a shell switched to a git identity reports.
    public static let identityVar = "WTF_GIT_IDENTITY"

    /// sshhosts.GitIdentity, as far as switching to it goes.
    public struct GitIdentity: Decodable, Sendable {
        public let name: String
        public let authorName: String
        public let authorEmail: String
        public let keyPath: String

        public init(name: String, authorName: String, authorEmail: String, keyPath: String) {
            self.name = name
            self.authorName = authorName
            self.authorEmail = authorEmail
            self.keyPath = keyPath
        }
    }

    /// Makes git in this shell use the identity's key, and commit as its
    /// author (blank: git config's).
    public static func useIdentity(_ i: GitIdentity) -> String {
        // git runs GIT_SSH_COMMAND through sh, so the key path is quoted inside it too.
        let ssh = "ssh -i \(quote(i.keyPath)) -o IdentitiesOnly=yes"
        var set = ["GIT_SSH_COMMAND=\(quote(ssh))"]
        var unset: [String] = []
        for (value, names) in [(i.authorName, ["GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"]),
                               (i.authorEmail, ["GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"])] {
            if value.isEmpty { unset += names } else { set += names.map { "\($0)=\(quote(value))" } }
        }
        set.append("\(identityVar)=\(quote(i.name))")
        return "export \(set.joined(separator: " "))" + (unset.isEmpty ? "" : "; unset \(unset.joined(separator: " "))")
    }
}

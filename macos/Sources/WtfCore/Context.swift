// A shell's cloud and cluster context, from the variables it reports at
// each prompt (infra.Resolve in internal/infra), and the address bar badges
// made from it. A port of frontend/src/lib/infra.ts.
import Foundation

public enum Env {
    /// "NAME=value" lines; lines without "=" (or starting with it) are skipped,
    /// later duplicates win.
    public static func parse(_ text: String) -> [String: String] {
        var env: [String: String] = [:]
        for line in text.split(separator: "\n", omittingEmptySubsequences: false) {
            guard let i = line.firstIndex(of: "="), i > line.startIndex else { continue }
            env[String(line[..<i])] = String(line[line.index(after: i)...])
        }
        return env
    }
}

/// infra.Context.
public struct InfraContext: Decodable, Equatable, Sendable {
    public struct Git: Decodable, Equatable, Sendable {
        public var root = ""
        public var branch = ""
        public var detached = false
        public var upstream = ""
        public var upstreamGone = false
        public var ahead = 0
        public var behind = 0
        public var remote = ""
        public var remoteURL = ""
        public var fetchedAt: Double = 0
        public var authorName = ""
        public var authorEmail = ""
        public var identity = ""
    }

    public struct Expiry: Decodable, Equatable, Sendable {
        public var kind: String
        public var what: String
        /// Unix milliseconds.
        public var expiresAt: Double
    }

    public var kubeContext = "", kubeCluster = "", kubeNamespace = ""
    public var awsProfile = "", awsAccount = "", awsAccountNote = "", awsRegion = ""
    public var gcpProject = "", tfWorkspace = ""
    public var git: Git?
    public var expiries: [Expiry]?

    public init() {}

    enum CodingKeys: String, CodingKey {
        case kubeContext, kubeCluster, kubeNamespace, awsProfile, awsAccount, awsAccountNote, awsRegion, gcpProject, tfWorkspace, git, expiries
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        func s(_ k: CodingKeys) -> String { (try? c.decodeIfPresent(String.self, forKey: k)) ?? "" }
        kubeContext = s(.kubeContext); kubeCluster = s(.kubeCluster); kubeNamespace = s(.kubeNamespace)
        awsProfile = s(.awsProfile); awsAccount = s(.awsAccount); awsAccountNote = s(.awsAccountNote); awsRegion = s(.awsRegion)
        gcpProject = s(.gcpProject); tfWorkspace = s(.tfWorkspace)
        git = try? c.decodeIfPresent(Git.self, forKey: .git)
        expiries = try? c.decodeIfPresent([Expiry].self, forKey: .expiries)
    }
}

extension InfraContext.Git {
    enum CodingKeys: String, CodingKey {
        case root, branch, detached, upstream, upstreamGone, ahead, behind, remote, remoteURL, fetchedAt, authorName, authorEmail, identity
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        func s(_ k: CodingKeys) -> String { (try? c.decodeIfPresent(String.self, forKey: k)) ?? "" }
        root = s(.root); branch = s(.branch); upstream = s(.upstream); remote = s(.remote); remoteURL = s(.remoteURL)
        authorName = s(.authorName); authorEmail = s(.authorEmail); identity = s(.identity)
        detached = (try? c.decodeIfPresent(Bool.self, forKey: .detached)) ?? false
        upstreamGone = (try? c.decodeIfPresent(Bool.self, forKey: .upstreamGone)) ?? false
        ahead = (try? c.decodeIfPresent(Int.self, forKey: .ahead)) ?? 0
        behind = (try? c.decodeIfPresent(Int.self, forKey: .behind)) ?? 0
        fetchedAt = (try? c.decodeIfPresent(Double.self, forKey: .fetchedAt)) ?? 0
    }
}

public struct Badge: Equatable, Sendable {
    public enum Clock: Equatable, Sendable { case ok(String), soon(String), expired }
    /// kube, aws, gcp, tf, git, ssh.
    public let kind: String
    public let label: String
    public let title: String
    public let clock: Clock?

    /// The kind as the badge shows it.
    public var kindLabel: String { kind == "kube" ? "K8S" : kind.uppercased() }
}

public enum Badges {
    static func join(_ parts: String...) -> String { parts.filter { !$0.isEmpty }.joined(separator: " · ") }

    /// The cluster part of an EKS ARN or a GKE name; others unchanged.
    public static func shortClusterName(_ s: String) -> String {
        for p in [#"^arn:aws[\w-]*:eks:[^:]*:\d+:cluster/(.+)$"#, #"^gke_[^_]+_[^_]+_(.+)$"#] {
            let re = try! NSRegularExpression(pattern: p)
            if let m = re.firstMatch(in: s, range: NSRange(s.startIndex..., in: s)), let r = Range(m.range(at: 1), in: s) {
                return String(s[r])
            }
        }
        return s
    }

    /// "just now", "5 min ago", "in 3 h".
    public static func relativeTime(_ ms: Double, now: Double) -> String {
        let diff = ms - now, a = abs(diff) / 1000
        let text: String
        if a < 45 { return "just now" }
        else if a < 3600 { text = "\(Int((a / 60).rounded())) min" }
        else if a < 86400 { text = "\(Int((a / 3600).rounded())) h" }
        else { text = "\(Int((a / 86400).rounded())) d" }
        return diff < 0 ? "\(text) ago" : "in \(text)"
    }

    /// "<1m", "42m", "3h 10m", "3h", "5d".
    public static func timeLeft(_ at: Double, now: Double) -> String {
        let m = (at - now) / 60000
        if m < 1 { return "<1m" }
        if m < 60 { return "\(Int(m))m" }
        if m < 1440 {
            let h = Int(m / 60), mm = Int(m) % 60
            return mm == 0 ? "\(h)h" : "\(h)h \(mm)m"
        }
        return "\(Int(m / 1440))d"
    }

    static func clock(_ list: [InfraContext.Expiry], now: Double) -> Badge.Clock? {
        guard let at = list.map(\.expiresAt).min() else { return nil }
        if at <= now { return .expired }
        if at - now >= 86_400_000 { return nil }
        let label = timeLeft(at, now: now)
        return at - now < 600_000 ? .soon(label) : .ok(label)
    }

    static func expiryLines(_ list: [InfraContext.Expiry], now: Double) -> [String] {
        let f = DateFormatter()
        f.dateStyle = .medium
        f.timeStyle = .short
        return list.sorted { $0.expiresAt < $1.expiresAt }.map { e in
            let date = f.string(from: Date(timeIntervalSince1970: e.expiresAt / 1000))
            return e.expiresAt > now
                ? "\(e.what) expires \(date) (in \(timeLeft(e.expiresAt, now: now)))"
                : "\(e.what) expired \(date) (\(relativeTime(e.expiresAt, now: now)))"
        }
    }

    /// The badges for a context, in order: kube, aws, gcp, tf, git, ssh.
    public static func of(_ c: InfraContext, now: Double = Date().timeIntervalSince1970 * 1000) -> [Badge] {
        let exp = c.expiries ?? []
        func kind(_ k: String) -> [InfraContext.Expiry] { exp.filter { $0.kind == k } }
        func title(_ lines: [String], _ k: String?) -> String {
            (lines + (k.map { expiryLines(kind($0), now: now) } ?? [])).filter { !$0.isEmpty }.joined(separator: "\n")
        }
        var out: [Badge] = []
        if !c.kubeContext.isEmpty {
            out.append(Badge(kind: "kube", label: join(shortClusterName(c.kubeCluster.isEmpty ? c.kubeContext : c.kubeCluster), c.kubeNamespace),
                             title: title([c.kubeCluster.isEmpty ? "" : "Cluster: \(c.kubeCluster)", "Context: \(c.kubeContext)",
                                           c.kubeNamespace.isEmpty ? "" : "Namespace: \(c.kubeNamespace)"], "kube"),
                             clock: clock(kind("kube"), now: now)))
        }
        if !c.awsProfile.isEmpty {
            let account = c.awsAccount.isEmpty ? (c.awsAccountNote.isEmpty ? "unknown" : c.awsAccountNote) : c.awsAccount
            out.append(Badge(kind: "aws", label: join(c.awsAccount, c.awsProfile, c.awsRegion),
                             title: title(["Account: \(account)", "Profile: \(c.awsProfile)", c.awsRegion.isEmpty ? "" : "Region: \(c.awsRegion)"], "aws"),
                             clock: clock(kind("aws"), now: now)))
        }
        if !c.gcpProject.isEmpty {
            out.append(Badge(kind: "gcp", label: c.gcpProject, title: "Google Cloud project: \(c.gcpProject)", clock: nil))
        }
        if !c.tfWorkspace.isEmpty {
            out.append(Badge(kind: "tf", label: c.tfWorkspace, title: "Terraform workspace: \(c.tfWorkspace)", clock: nil))
        }
        if let g = c.git { out.append(git(g, now: now)) }
        let ssh = kind("ssh")
        if !ssh.isEmpty {
            out.append(Badge(kind: "ssh", label: ssh.count == 1 ? "certificate" : "\(ssh.count) certificates",
                             title: title(["Certificates in the shell's ssh-agent"], "ssh"), clock: clock(ssh, now: now)))
        }
        return out
    }

    static func git(_ g: InfraContext.Git, now: Double) -> Badge {
        let branch = g.branch.isEmpty ? "(no commits)" : g.branch
        var upstream = g.upstream
        if let slash = upstream.firstIndex(of: "/"), upstream[upstream.index(after: slash)...] == branch {
            upstream = String(upstream[..<slash])
        }
        let diff = g.upstreamGone ? "gone" : [g.ahead > 0 ? "↑\(g.ahead)" : "", g.behind > 0 ? "↓\(g.behind)" : ""]
            .filter { !$0.isEmpty }.joined(separator: " ")
        let author = !g.identity.isEmpty ? g.identity : !g.authorName.isEmpty ? g.authorName : g.authorEmail
        let head = [branch, upstream.isEmpty ? "" : "→ \(upstream)", diff].filter { !$0.isEmpty }.joined(separator: " ")
        let label = join(author, head)

        func commits(_ n: Int, _ what: String) -> String { "\(n) \(n == 1 ? "commit" : "commits") to \(what)" }
        let sync: String
        if g.detached { sync = "Detached HEAD: not on a branch" }
        else if g.upstream.isEmpty { sync = "No upstream branch: nothing to compare with" }
        else if g.upstreamGone { sync = "\(g.upstream) is gone (deleted on the remote, or not fetched yet)" }
        else if g.ahead == 0, g.behind == 0 { sync = "Up to date with \(g.upstream)" }
        else { sync = [g.ahead > 0 ? commits(g.ahead, "push") : "", g.behind > 0 ? commits(g.behind, "pull") : ""].filter { !$0.isEmpty }.joined(separator: ", ") }
        var lines = [
            "Repository: \(g.root)",
            g.detached ? "Commit: \(branch)" : "Branch: \(branch)",
            g.upstream.isEmpty ? "" : "Upstream: \(g.upstream)",
            g.remoteURL.isEmpty ? "" : "Remote: \(g.remote) \(g.remoteURL)",
            sync,
        ]
        if !g.upstream.isEmpty, !g.upstreamGone {
            lines.append("As of the last fetch" + (g.fetchedAt > 0 ? " (\(relativeTime(g.fetchedAt, now: now)))" : ""))
        }
        if !g.identity.isEmpty { lines.append("Git identity: \(g.identity) (its SSH key only)") }
        let who = [g.authorName, g.authorEmail.isEmpty ? "" : "<\(g.authorEmail)>"].filter { !$0.isEmpty }.joined(separator: " ")
        lines.append(who.isEmpty ? "Author: not set (git config user.name)" : "Author: \(who)")
        return Badge(kind: "git", label: label, title: lines.filter { !$0.isEmpty }.joined(separator: "\n"), clock: nil)
    }
}

/// Which input broadcast leaves its pane. A port of isTerminalReport in
/// lib/broadcast.svelte.ts: answers the terminal gives a program on its own
/// (cursor position, mouse, focus, mode and device reports, OSC and DCS
/// replies) stay in their pane.
public enum TerminalReport {
    static let pattern = try! NSRegularExpression(pattern: [
        #"\e\[\d+;\d+R"#,
        #"\e\[[?>=]?[\d;]*c"#,
        #"\e\[[IO]"#,
        #"\e\[M[\s\S]{3}"#,
        #"\e\[<\d+;\d+;\d+[Mm]"#,
        #"\e\[\??[\d;]*\$y"#,
        #"\e\][\s\S]*(?:\x07|\e\\)"#,
        #"\eP[\s\S]*\e\\"#,
    ].map { "(?:\($0))" }.joined(separator: "|").replacingOccurrences(of: "^", with: "") .wrappedWhole)

    public static func matches(_ s: String) -> Bool {
        let ns = s as NSString
        guard let m = pattern.firstMatch(in: s, range: NSRange(location: 0, length: ns.length)) else { return false }
        return m.range.length == ns.length
    }
}

private extension String {
    var wrappedWhole: String { "^(?:\(self))$" }
}

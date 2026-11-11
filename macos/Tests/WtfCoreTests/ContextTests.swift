import Foundation
import Testing
@testable import WtfCore

// The cases of the Svelte build's infra tests and the broadcast report filter.

@Test func parsesEnvLines() {
    #expect(Env.parse("A=1\nB=x=y\n=bad\nnoeq\nA=2\nC=") == ["A": "2", "B": "x=y", "C": ""])
}

@Test func shortensClusterNames() {
    #expect(Badges.shortClusterName("arn:aws:eks:us-east-1:123456789012:cluster/payments") == "payments")
    #expect(Badges.shortClusterName("gke_my-proj_europe-west1-b_shop") == "shop")
    #expect(Badges.shortClusterName("minikube") == "minikube")
}

private func ctx(_ f: (inout InfraContext) -> Void) -> InfraContext {
    var c = InfraContext()
    f(&c)
    return c
}

@Test func badgesShowClusterAccountProjectWorkspace() {
    let b = Badges.of(ctx {
        $0.kubeContext = "work"; $0.kubeCluster = "arn:aws:eks:us-east-1:123456789012:cluster/payments"; $0.kubeNamespace = "api"
        $0.awsProfile = "admin"; $0.awsAccount = "210987654321"; $0.awsRegion = "us-east-1"; $0.gcpProject = "shop"; $0.tfWorkspace = "staging"
    })
    #expect(b.map { [$0.kind, $0.label] } == [["kube", "payments · api"], ["aws", "210987654321 · admin · us-east-1"], ["gcp", "shop"], ["tf", "staging"]])
    #expect(b[0].title.contains("Context: work") && b[0].title.contains("Cluster: arn:aws:eks:us-east-1:123456789012:cluster/payments"))
    #expect(b[0].kindLabel == "K8S")
}

@Test func badgesFallBackAndLeaveOutTheUnknown() {
    let b = Badges.of(ctx { $0.kubeContext = "minikube"; $0.awsProfile = "static"; $0.awsAccountNote = "asking AWS…" })
    #expect(b.map(\.label) == ["minikube", "static"])
    #expect(b[1].title.contains("Account: asking AWS…"))
    #expect(Badges.of(InfraContext()).isEmpty)
}

private func git(_ f: (inout InfraContext.Git) -> Void = { _ in }) -> InfraContext.Git {
    var g = InfraContext.Git(root: "/src/engine", branch: "main", upstream: "origin/main", remote: "origin",
                             remoteURL: "git@github.com:ada/engine.git", authorName: "Ada", authorEmail: "ada@example.com")
    f(&g)
    return g
}

@Test func gitBadge() {
    let now = 10.0 * 3_600_000
    let b = Badges.git(git { $0.ahead = 2; $0.behind = 1; $0.fetchedAt = now - 3 * 3_600_000 }, now: now)
    #expect(b.label == "Ada · main → origin ↑2 ↓1")
    #expect(b.title.contains("Remote: origin git@github.com:ada/engine.git"))
    #expect(b.title.contains("2 commits to push, 1 commit to pull"))
    #expect(b.title.contains("As of the last fetch (3 h ago)"))
    #expect(b.title.contains("Author: Ada <ada@example.com>"))
    #expect(Badges.git(git { $0.upstream = "origin/release/2.0" }, now: now).label == "Ada · main → origin/release/2.0")
    #expect(Badges.git(git { $0.upstream = "upstream/main" }, now: now).label == "Ada · main → upstream")
    #expect(Badges.git(git { $0.upstream = "" }, now: now).label == "Ada · main")
    #expect(Badges.git(git { $0.upstream = "" }, now: now).title.contains("No upstream branch"))
    #expect(Badges.git(git { $0.upstreamGone = true }, now: now).label == "Ada · main → origin gone")
    #expect(Badges.git(git { $0.upstream = ""; $0.detached = true; $0.branch = "abc1234" }, now: now).title.contains("Commit: abc1234"))
    #expect(Badges.git(git { $0.authorName = ""; $0.authorEmail = "" }, now: now).title.contains("Author: not set"))
    let w = Badges.git(git { $0.identity = "work" }, now: now)
    #expect(w.label == "work · main → origin" && w.title.contains("Git identity: work"))
    #expect(Badges.of(ctx { $0.git = git() }).map(\.kind) == ["git"])
}

@Test func expiryClocks() {
    let now = Date(timeIntervalSince1970: 1_791_806_400).timeIntervalSince1970 * 1000
    let min = 60_000.0
    #expect(Badges.timeLeft(now + 30_000, now: now) == "<1m")
    #expect(Badges.timeLeft(now + 42 * min, now: now) == "42m")
    #expect(Badges.timeLeft(now + 190 * min, now: now) == "3h 10m")
    #expect(Badges.timeLeft(now + 120 * min, now: now) == "2h")
    #expect(Badges.timeLeft(now + 5 * 1440 * min, now: now) == "5d")
    func e(_ m: Double) -> [InfraContext.Expiry] { [.init(kind: "aws", what: "x", expiresAt: now + m * min)] }
    #expect(Badges.clock([], now: now) == nil)
    #expect(Badges.clock(e(25 * 60), now: now) == nil)
    #expect(Badges.clock(e(42), now: now) == .ok("42m"))
    #expect(Badges.clock(e(9), now: now) == .soon("9m"))
    #expect(Badges.clock(e(-1), now: now) == .expired)

    let b = Badges.of(ctx {
        $0.kubeContext = "dev"; $0.awsProfile = "sso"; $0.awsAccount = "111111111111"
        $0.expiries = [
            .init(kind: "kube", what: "Kubernetes token of user dev", expiresAt: now + 300 * min),
            .init(kind: "kube", what: "Kubernetes client certificate of user dev", expiresAt: now + 30 * min),
            .init(kind: "aws", what: "AWS SSO session acme", expiresAt: now - 5 * min),
            .init(kind: "ssh", what: "SSH certificate ada@corp (ada)", expiresAt: now + 9 * 1440 * min),
        ]
    }, now: now)
    #expect(b[0].clock == .ok("30m"))
    #expect(b[0].title.range(of: #"client certificate of user dev expires .* \(in 30m\)\n.*token of user dev expires .* \(in 5h\)$"#, options: .regularExpression) != nil)
    #expect(b[1].clock == .expired)
    #expect(b[1].title.range(of: #"AWS SSO session acme expired .* \(5 min ago\)$"#, options: .regularExpression) != nil)
    #expect(b[2].kind == "ssh" && b[2].label == "certificate" && b[2].clock == nil)
    #expect(b[2].title.range(of: #"ada@corp \(ada\) expires .* \(in 9d\)$"#, options: .regularExpression) != nil)
    #expect(Badges.of(ctx { $0.awsProfile = "p" }, now: now)[0].clock == nil)
}

@Test func terminalReportsStayInTheirPane() {
    for s in ["\u{1b}[12;40R", "\u{1b}[?1;2c", "\u{1b}[>0;276;0c", "\u{1b}[I", "\u{1b}[O", "\u{1b}[M !!",
              "\u{1b}[<0;10;5M", "\u{1b}[?2004;1$y", "\u{1b}]11;rgb:0000/0000/0000\u{07}", "\u{1b}]10;rgb:ff/ff/ff\u{1b}\\",
              "\u{1b}P>|xterm\u{1b}\\"] {
        #expect(TerminalReport.matches(s), "\(s.debugDescription)")
    }
    for s in ["ls -la\r", "a", "\u{1b}[A", "\u{1b}[1;5D", "\u{03}", "\u{1b}[200~pasted\u{1b}[201~", "\u{1b}OP"] {
        #expect(!TerminalReport.matches(s), "\(s.debugDescription)")
    }
}

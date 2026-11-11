// The Kubernetes tab's rules: kubectl command lines, how rows read and what
// colour their dot is, and how contexts are described. Like
// internal/uicore/kube.go.
import Foundation

public enum Kube {
    public struct Target: Equatable, Sendable {
        public var context = "", namespace = ""
        public var env: [String: String] = [:]
        public init(context: String = "", namespace: String = "", env: [String: String] = [:]) {
            self.context = context
            self.namespace = namespace
            self.env = env
        }
    }

    /// `kubectl --context C -n N args…`, with the shell's KUBECONFIG and AWS
    /// variables in front when given (for a new tab).
    public static func kubectl(_ t: Target, _ args: String...) -> String {
        var parts: [String] = []
        for k in ["KUBECONFIG", "AWS_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION"] {
            if let v = t.env[k], !v.isEmpty { parts.append("\(k)=\(Shell.quote(v))") }
        }
        parts.append("kubectl")
        if !t.context.isEmpty { parts += ["--context", Shell.quote(t.context)] }
        if !t.namespace.isEmpty { parts += ["-n", Shell.quote(t.namespace)] }
        return (parts + args).joined(separator: " ")
    }

    private static func container(_ c: String?) -> [String] { c.map { ["-c", Shell.quote($0)] } ?? [] }

    public static func logs(_ t: Target, pod: String, container c: String? = nil, previous: Bool = false) -> String {
        let args = (previous ? ["logs", "--previous", Shell.quote(pod)] : ["logs", "-f", "--tail=200", Shell.quote(pod)]) + container(c)
        return kubectl(t, args.joined(separator: " "))
    }

    public static func exec(_ t: Target, pod: String, container c: String? = nil) -> String {
        kubectl(t, (["exec", "-it", Shell.quote(pod)] + container(c)).joined(separator: " "),
                "-- sh -c 'command -v bash >/dev/null && exec bash || exec sh'")
    }

    public static func describe(_ t: Target, _ kind: String, _ name: String) -> String { kubectl(t, "describe", kind, Shell.quote(name)) }
    public static func yaml(_ t: Target, _ kind: String, _ name: String) -> String { kubectl(t, "get", kind, Shell.quote(name), "-o yaml") }
    public static func workloadLogs(_ t: Target, _ kind: String, _ name: String) -> String {
        kubectl(t, "logs -f --tail=200 --all-containers --prefix", Shell.quote("\(kind)/\(name)"))
    }
    public static func rolloutStatus(_ t: Target, _ kind: String, _ name: String) -> String { kubectl(t, "rollout status", Shell.quote("\(kind)/\(name)")) }
    public static func rolloutHistory(_ t: Target, _ kind: String, _ name: String) -> String { kubectl(t, "rollout history", Shell.quote("\(kind)/\(name)")) }
    public static func restart(_ t: Target, _ kind: String, _ name: String) -> String { kubectl(t, "rollout restart", Shell.quote("\(kind)/\(name)")) }
    public static func scale(_ t: Target, _ kind: String, _ name: String, _ n: Int) -> String {
        kubectl(t, "scale", Shell.quote("\(kind)/\(name)"), "--replicas=\(n)")
    }
    public static func decodeSecret(_ t: Target, _ name: String) -> String {
        kubectl(t, "get secret", Shell.quote(name), "-o",
                Shell.quote(#"go-template={{range $k, $v := .data}}{{$k}}={{$v | base64decode}}{{"\n"}}{{end}}"#))
    }
    public static func triggerCronJob(_ t: Target, _ name: String, now: Double = Date().timeIntervalSince1970 * 1000) -> String {
        kubectl(t, "create job", "--from=" + Shell.quote("cronjob/\(name)"),
                Shell.quote(String(name.prefix(40)) + "-manual-" + String(Int64(now), radix: 36)))
    }
    public static func deletePod(_ t: Target, _ name: String) -> String { kubectl(t, "delete pod", Shell.quote(name)) }

    // MARK: Rows

    public enum Tone: Sendable { case ok, wait, bad, done }

    /// "45s", "12m", "5h", "3d".
    public static func age(_ iso: String, now: Date = Date()) -> String {
        let f = ISO8601DateFormatter()
        guard let d = f.date(from: iso) ?? { f.formatOptions.insert(.withFractionalSeconds); return f.date(from: iso) }() else { return "" }
        let s = max(0, Int(now.timeIntervalSince(d)))
        if s < 120 { return "\(s)s" }
        if s < 7200 { return "\(s / 60)m" }
        if s < 172_800 { return "\(s / 3600)h" }
        return "\(s / 86400)d"
    }

    public static func tone(_ d: Go.kube.Deployment) -> Tone {
        d.ready == d.desired ? .ok : (d.ready == 0 && d.desired > 0 ? .bad : .wait)
    }

    public static func deploymentDetail(_ d: Go.kube.Deployment, now: Date = Date()) -> String {
        "\(d.ready)/\(d.desired) ready" + (d.upToDate != d.desired ? " · \(d.upToDate) up to date" : "") + " · \(age(d.created, now: now))"
    }

    public static func tone(_ p: Go.kube.Pod) -> Tone {
        if p.status == "Running" { return p.ready == p.total ? .ok : .wait }
        if p.status == "Completed" || p.status == "Succeeded" { return .done }
        if p.status.range(of: "Err|BackOff|Error|OOMKilled|Failed|Evicted|ExitCode|Unknown|NotReady", options: .regularExpression) != nil { return .bad }
        return .wait
    }

    public static func podDetail(_ p: Go.kube.Pod, now: Date = Date()) -> String {
        var parts = [p.status]
        if p.total > 0 { parts.append("\(p.ready)/\(p.total) ready") }
        if p.restarts > 0 {
            var r = "\(p.restarts) \(p.restarts == 1 ? "restart" : "restarts")"
            if let reason = p.containers.first(where: { !$0.lastReason.isEmpty })?.lastReason {
                let ago = age(p.lastRestart, now: now)
                r += " (\(reason)\(ago.isEmpty ? "" : " \(ago) ago"))"
            }
            parts.append(r)
        }
        parts.append(age(p.created, now: now))
        return parts.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    public static func podTip(_ p: Go.kube.Pod) -> String {
        var lines = [p.name, "Node: \(p.node.isEmpty ? "—" : p.node)  IP: \(p.ip.isEmpty ? "—" : p.ip)"]
        if !p.owner.isEmpty { lines.append("Owner: \(p.owner)") }
        for c in p.containers {
            lines.append("\(c.name): \(c.image)" + (c.ready ? "" : " — \(c.reason.isEmpty ? "not ready" : c.reason)") + (c.restarts > 0 ? ", \(c.restarts) restarts" : ""))
        }
        return lines.joined(separator: "\n")
    }

    /// The table kinds: id, label, kubectl kind, summary columns, note.
    public struct TableKind: Sendable {
        public let id: String, label: String, kind: String
        public let columns: [String]
        public let note: String
    }

    public static let tableKinds: [TableKind] = [
        .init(id: "statefulsets", label: "StatefulSets", kind: "statefulset", columns: ["READY"], note: ""),
        .init(id: "daemonsets", label: "DaemonSets", kind: "daemonset", columns: ["DESIRED", "READY", "NODE SELECTOR"], note: ""),
        .init(id: "jobs", label: "Jobs", kind: "job", columns: ["STATUS", "COMPLETIONS", "DURATION"], note: ""),
        .init(id: "cronjobs", label: "CronJobs", kind: "cronjob", columns: ["SCHEDULE", "SUSPEND", "LAST SCHEDULE"], note: ""),
        .init(id: "services", label: "Services", kind: "service", columns: ["TYPE", "CLUSTER-IP", "EXTERNAL-IP", "PORT(S)"], note: ""),
        .init(id: "ingresses", label: "Ingresses", kind: "ingress", columns: ["HOSTS", "ADDRESS"], note: ""),
        .init(id: "configmaps", label: "ConfigMaps", kind: "configmap", columns: ["DATA"], note: ""),
        .init(id: "secrets", label: "Secrets", kind: "secret", columns: ["TYPE", "DATA"],
              note: "Listed by name, type and key count; a secret’s values are read only when you open that secret"),
        .init(id: "pvcs", label: "PersistentVolumeClaims", kind: "pvc", columns: ["STATUS", "CAPACITY", "STORAGECLASS"], note: ""),
        .init(id: "hpas", label: "HorizontalPodAutoscalers", kind: "hpa", columns: ["REFERENCE", "TARGETS", "REPLICAS"], note: ""),
        .init(id: "nodes", label: "Nodes", kind: "node", columns: ["STATUS", "ROLES", "VERSION"], note: "Cluster-wide, not in a namespace"),
    ]

    private static func ratio(_ s: String?) -> Tone? {
        guard let s, let slash = s.firstIndex(of: "/"), let a = Int(s[..<slash]), let b = Int(s[s.index(after: slash)...]) else { return nil }
        return a == b ? .ok : (a == 0 && b > 0 ? .bad : .wait)
    }

    public static func rowTone(_ kind: String, _ cells: [String: String]) -> Tone? {
        switch kind {
        case "statefulsets": return ratio(cells["READY"])
        case "daemonsets":
            guard let d = cells["DESIRED"], let r = cells["READY"] else { return nil }
            return ratio("\(r)/\(d)")
        case "jobs":
            if let s = cells["STATUS"], !s.isEmpty { return s == "Complete" ? .ok : (s == "Failed" ? .bad : .wait) }
            return ratio(cells["COMPLETIONS"])
        case "pvcs":
            guard let s = cells["STATUS"] else { return nil }
            return s == "Bound" ? .ok : (s == "Lost" ? .bad : .wait)
        case "nodes":
            guard let s = cells["STATUS"] else { return nil }
            return s == "Ready" ? .ok : (s.hasPrefix("NotReady") || s.hasPrefix("Unknown") ? .bad : .wait)
        default: return nil
        }
    }

    /// A permission rule's second line: its verbs, and the objects it's limited to.
    public static func ruleDetail(_ r: Go.kube.Rule) -> String {
        let verbs = r.verbs == ["*"] ? "all verbs" : r.verbs.joined(separator: ", ")
        return r.names.isEmpty ? verbs : "\(verbs) · only: \(r.names.joined(separator: ", "))"
    }

    public static func rowSummary(_ kind: String, _ cells: [String: String]) -> String {
        let cols = tableKinds.first { $0.id == kind }?.columns ?? []
        var parts = cols.compactMap { c -> String? in
            guard let v = cells[c], !v.isEmpty, v != "<none>" else { return nil }
            return v
        }
        if let age = cells["AGE"], !age.isEmpty { parts.append(age) }
        return parts.joined(separator: " · ")
    }

    /// The first port of PORT(S): "80/TCP,443/TCP" or "80:30080/TCP" → "80".
    public static func firstPort(_ ports: String) -> String {
        guard let r = ports.range(of: #"^\d+"#, options: .regularExpression) else { return "" }
        return String(ports[r])
    }

    // MARK: Contexts

    public static func clusterLabel(_ c: Go.kube.KubeContext) -> String {
        if !c.eksCluster.isEmpty { return "\(c.eksCluster) (EKS \(c.eksRegion))" }
        if let host = URL(string: c.server)?.host, !host.isEmpty { return host }
        return c.cluster
    }

    public static func authLabel(_ c: Go.kube.KubeContext) -> String {
        switch c.auth {
        case "exec":
            let base = Paths.base(c.execCommand)
            return base == "aws" && c.execArgs.contains("get-token") ? "aws eks get-token" : base
        case "client-cert": return "client certificate"
        case "token": return "token"
        case "auth-provider": return "auth provider"
        case "basic": return "username and password"
        default: return "no credentials"
        }
    }

    public static func contextDetail(_ c: Go.kube.KubeContext) -> String {
        [clusterLabel(c), c.namespace.isEmpty ? "" : "ns \(c.namespace)", authLabel(c), c.awsProfile.isEmpty ? "" : "profile \(c.awsProfile)"]
            .filter { !$0.isEmpty }.joined(separator: " · ")
    }

    /// Commands after which the kubeconfig may have changed.
    public static func changesKubeconfig(_ command: String) -> Bool {
        command.range(of: #"\b(aws\s+eks\s+update-kubeconfig|kubectl\s+config\s|eksctl\s+utils\s+write-kubeconfig)"#, options: .regularExpression) != nil
    }

    public static func eksAdd(cluster: String, region: String, profile: String, alias: String, file: String?) -> String {
        var s = "aws eks update-kubeconfig --name \(Shell.quote(cluster)) --region \(Shell.quote(region))"
        if !profile.isEmpty { s += " --profile \(Shell.quote(profile))" }
        if !alias.isEmpty { s += " --alias \(Shell.quote(alias))" }
        if let file, !file.isEmpty { s += " --kubeconfig \(Shell.quote(file))" }
        return s
    }

    public static let regions = ["us-east-1", "us-east-2", "us-west-1", "us-west-2", "ca-central-1", "sa-east-1", "eu-west-1", "eu-west-2",
                                 "eu-west-3", "eu-central-1", "eu-central-2", "eu-north-1", "eu-south-1", "ap-south-1", "ap-southeast-1",
                                 "ap-southeast-2", "ap-northeast-1", "ap-northeast-2", "ap-east-1", "me-central-1", "af-south-1"]
}

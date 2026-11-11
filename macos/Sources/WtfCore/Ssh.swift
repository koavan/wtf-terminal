// The SSH tab's rules: commands, host text, the host editor's fields,
// tunnels and scp. Like
// internal/uicore/ssh.go.
import Foundation

public typealias SshHost = Go.sshhosts.Host

public enum Ssh {
    /// The command that connects; config hosts go by alias so ssh applies their settings.
    public static func command(_ h: SshHost) -> String { "ssh \(portFlag(h))\(Shell.quote(h.alias))" }

    /// "-p 2222 " for a known host listed with a port; config hosts carry their own.
    public static func portFlag(_ h: SshHost, _ flag: String = "-p") -> String {
        h.known && !h.port.isEmpty && h.port != "22" ? "\(flag) \(Shell.quote(h.port)) " : ""
    }

    /// Stable across reloads: a known host and a config alias may share a name.
    public static func key(_ h: SshHost) -> String { "\(h.known ? "k" : "c"):\(h.alias):\(h.port)" }

    /// "ops@bastion.example.com:2222 via jump": what adds to the alias.
    public static func detail(_ h: SshHost) -> String {
        var target = !h.hostName.isEmpty && h.hostName != h.alias ? h.hostName : ""
        if !h.user.isEmpty { target = "\(h.user)@\(target.isEmpty ? h.alias : target)" }
        if !h.port.isEmpty, h.port != "22" { target = "\(target.isEmpty ? h.alias : target):\(h.port)" }
        return [target, h.proxyJump.isEmpty ? "" : "via \(h.proxyJump)"].filter { !$0.isEmpty }.joined(separator: " ")
    }

    /// Settings that change what a connection may do, shown as they are, never judged.
    public static func facts(_ h: SshHost) -> [(label: String, title: String)] {
        var out: [(String, String)] = []
        if !h.forwardAgent.isEmpty {
            out.append(("agent fwd", "ForwardAgent \(h.forwardAgent): the server can use \(h.forwardAgent == "yes" ? "your ssh-agent" : "that agent") while you're connected"))
        }
        let check = h.strictHostKeyChecking
        if check == "accept-new" {
            out.append(("hostkey accept-new", "StrictHostKeyChecking accept-new: a new host key is saved without asking; a changed one is refused"))
        } else if check == "no" || check == "off" {
            out.append(("hostkey \(check)", "StrictHostKeyChecking \(check): host keys are saved and connected to without asking"))
        }
        return out
    }

    public static func matches(_ h: SshHost, _ query: String) -> Bool {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        if q.isEmpty { return true }
        let text = "\(h.alias) \(h.hostName) \(h.user) \(h.proxyJump)".lowercased()
        return q.split(whereSeparator: \.isWhitespace).allSatisfy { text.contains($0) }
    }

    /// A tab group's name for hosts: their config file's, unless that's plain "config".
    public static func groupName(_ hosts: [SshHost]) -> String {
        let sources = Set(hosts.map(\.source))
        if sources.count == 1, let s = sources.first, !s.isEmpty, Paths.base(s) != "config" { return Paths.base(s) }
        return "SSH"
    }

    /// A path for a command line: ~/… stays unquoted at the front so the shell expands it.
    public static func quotePath(_ path: String, home: String = NSHomeDirectory()) -> String {
        let p = WindowModel.prettyPath(path, home: home)
        if p == "~" { return p }
        return p.hasPrefix("~/") ? "~/" + Shell.quote(String(p.dropFirst(2))) : Shell.quote(p)
    }

    public static func sftp(_ h: SshHost) -> String { "sftp \(portFlag(h, "-P"))\(Shell.quote(h.alias))" }

    public static func upload(_ h: SshHost, _ locals: [String], _ remoteDir: String, home: String = NSHomeDirectory()) -> String {
        "scp -r \(portFlag(h, "-P"))\(locals.map { quotePath($0, home: home) }.joined(separator: " ")) \(Shell.quote("\(h.alias):\(remoteDir.trimmingCharacters(in: .whitespaces))"))"
    }

    public static func download(_ h: SshHost, _ remote: String, _ localDir: String, home: String = NSHomeDirectory()) -> String {
        "scp -r \(portFlag(h, "-P"))\(Shell.quote("\(h.alias):\(remote.trimmingCharacters(in: .whitespaces))")) \(quotePath(localDir, home: home))"
    }

    public static func installKey(_ keyPath: String, _ h: SshHost, home: String = NSHomeDirectory()) -> String {
        "ssh-copy-id -i \(quotePath(keyPath + ".pub", home: home)) \(portFlag(h))\(Shell.quote(h.alias))"
    }

    // MARK: Keys

    public struct KeyType: Sendable {
        public let id: String, label: String, args: String, file: String
    }

    public static let keyTypes = [
        KeyType(id: "ed25519", label: "Ed25519 (recommended)", args: "-t ed25519", file: "id_ed25519"),
        KeyType(id: "rsa", label: "RSA 4096 (for older servers)", args: "-t rsa -b 4096", file: "id_rsa"),
        KeyType(id: "ecdsa", label: "ECDSA P-521", args: "-t ecdsa -b 521", file: "id_ecdsa"),
    ]

    /// ssh-keygen asks for the passphrase itself, in the tab.
    public static func keygen(type: String, name: String, comment: String, addToAgent: Bool, keychain: Bool) -> String {
        let t = keyTypes.first { $0.id == type } ?? keyTypes[0]
        let path = "~/.ssh/\(Shell.quote(name.trimmingCharacters(in: .whitespaces)))"
        let c = comment.trimmingCharacters(in: .whitespaces)
        let gen = "ssh-keygen \(t.args)\(c.isEmpty ? "" : " -C \(Shell.quote(c))") -f \(path)"
        return addToAgent ? "\(gen) && \(agentAdd(path, keychain: keychain))" : gen
    }

    public static func agentAdd(_ quotedPath: String, keychain: Bool) -> String {
        "ssh-add \(keychain ? "--apple-use-keychain " : "")\(quotedPath)"
    }

    /// A free file name like id_ed25519, id_ed25519_2, …
    public static func freeKeyName(_ base: String, taken: Set<String>) -> String {
        var i = 1
        while true {
            let name = i == 1 ? base : "\(base)_\(i)"
            if !taken.contains("~/.ssh/\(name)") { return name }
            i += 1
        }
    }

    /// Commands after which keys and hosts may have changed.
    public static func isKeyTool(_ command: String) -> Bool {
        command.range(of: #"^\s*(?:sudo\s+)?(?:ssh-keygen|ssh-add|ssh-copy-id|ssh-keyscan)\b"#, options: .regularExpression) != nil
    }

    /// "ED25519 · from Oct 2026 · work laptop"
    public static func keyDetails(_ k: Go.sshhosts.Key) -> String {
        var parts = [k.type + (k.type.hasPrefix("RSA") || k.type.hasPrefix("ECDSA") ? " \(k.bits)" : "")]
        if let d = ISO8601DateFormatter().date(from: k.modified) {
            let f = DateFormatter()
            f.setLocalizedDateFormatFromTemplate("MMM yyyy")
            parts.append("from \(f.string(from: d))")
        }
        if !k.comment.isEmpty, k.comment != k.name { parts.append(k.comment) }
        return parts.joined(separator: " · ")
    }
}

/// A host entry as the editor fills it in.
public struct HostDraft: Equatable, Sendable {
    /// Host line patterns, space-separated: "web1 web2".
    public var name = "", hostName = "", user = "", port = "", identityFile = "", proxyJump = ""
    /// Any other settings, one "Key value" per line.
    public var more = ""

    public init() {}

    nonisolated(unsafe) static let fields: [(WritableKeyPath<HostDraft, String>, String)] = [
        (\.hostName, "HostName"), (\.user, "User"), (\.port, "Port"), (\.identityFile, "IdentityFile"), (\.proxyJump, "ProxyJump"),
    ]

    /// An entry's own settings, split into the editor's fields.
    public init(_ h: SshHost?) {
        guard let h else { return }
        name = (h.patterns.isEmpty ? [h.alias] : h.patterns).joined(separator: " ")
        var more: [String] = []
        for o in h.options {
            func unquote(_ v: String) -> String { v.count > 1 && v.hasPrefix("\"") && v.hasSuffix("\"") ? String(v.dropFirst().dropLast()) : v }
            if let f = Self.fields.first(where: { $0.1.lowercased() == o.key.lowercased() }), self[keyPath: f.0].isEmpty {
                self[keyPath: f.0] = unquote(o.value)
            } else {
                more.append("\(o.key) \(o.value)")
            }
        }
        self.more = more.joined(separator: "\n")
    }

    public struct MissingValue: LocalizedError {
        let line: String
        public var errorDescription: String? { "\(line) needs a value, like “ForwardAgent yes”" }
    }

    /// The settings to write; empty fields are left out (and so removed).
    public func options() throws -> [Go.sshhosts.Option] {
        var out = Self.fields.compactMap { f -> Go.sshhosts.Option? in
            let v = self[keyPath: f.0].trimmingCharacters(in: .whitespaces)
            return v.isEmpty ? nil : Go.sshhosts.Option(key: f.1, value: v)
        }
        let re = try! NSRegularExpression(pattern: #"^\s*([^\s=]+)\s*(?:=\s*|\s+)(.*)$"#)
        for line in more.components(separatedBy: "\n") {
            let ns = line as NSString
            if let m = re.firstMatch(in: line, range: NSRange(location: 0, length: ns.length)) {
                let v = ns.substring(with: m.range(at: 2)).trimmingCharacters(in: .whitespaces)
                if !v.isEmpty {
                    out.append(Go.sshhosts.Option(key: ns.substring(with: m.range(at: 1)), value: v))
                    continue
                }
            }
            let t = line.trimmingCharacters(in: .whitespaces)
            if !t.isEmpty { throw MissingValue(line: t) }
        }
        return out
    }
}

/// A tunnel as the editor fills it in.
public struct TunnelDraft: Equatable, Sendable {
    public var name = "", host = "", port = "", kind = "local"
    /// local: the port here; remote: the port on the server; dynamic: the SOCKS port.
    public var listenPort = ""
    public var destHost = "localhost", destPort = ""
    public var autoStart = false
    public var context = "", namespace = "", resource = ""
    public var env: [String: String] = [:]

    public init(host: String = "", port: String = "") {
        self.host = host
        self.port = port
    }

    static let forward = try! NSRegularExpression(pattern: #"^(.*?):(\[[^\]]+\]|[^:]+):(\d+)$"#)

    static func forwardParts(_ spec: String) -> [String]? {
        let ns = spec as NSString
        guard let m = forward.firstMatch(in: spec, range: NSRange(location: 0, length: ns.length)) else { return nil }
        return (1...3).map { ns.substring(with: m.range(at: $0)) }
    }

    public var spec: String {
        switch kind {
        case "dynamic": return listenPort.trimmingCharacters(in: .whitespaces)
        case "kubectl":
            let local = listenPort.trimmingCharacters(in: .whitespaces), remote = destPort.trimmingCharacters(in: .whitespaces)
            return !remote.isEmpty && remote != local ? "\(local):\(remote)" : local
        default:
            let h = destHost.trimmingCharacters(in: .whitespaces).isEmpty ? "localhost" : destHost.trimmingCharacters(in: .whitespaces)
            let bracketed = h.contains(":") && !h.hasPrefix("[") ? "[\(h)]" : h
            return "\(listenPort.trimmingCharacters(in: .whitespaces)):\(bracketed):\(destPort.trimmingCharacters(in: .whitespaces))"
        }
    }

    /// The reverse of `spec`, for editing.
    public init(_ t: Go.sshhosts.TunnelStatus) {
        self.init(host: t.host, port: t.port)
        name = t.name
        kind = t.kind
        autoStart = t.autoStart
        switch t.kind {
        case "dynamic":
            listenPort = t.spec
        case "kubectl":
            let parts = t.spec.split(separator: ":", omittingEmptySubsequences: false).map(String.init)
            listenPort = parts[0]
            destPort = parts.count > 1 ? parts[1] : parts[0]
            context = t.context
            namespace = t.namespace
            resource = t.resource
            env = t.env
        default:
            if let p = Self.forwardParts(t.spec) {
                listenPort = p[0]
                destHost = p[1].hasPrefix("[") && p[1].hasSuffix("]") ? String(p[1].dropFirst().dropLast()) : p[1]
                destPort = p[2]
            } else {
                listenPort = t.spec
            }
        }
    }

    /// The variables a kubectl forward needs from the shell it's made in.
    public static func kubectlEnv(_ env: [String: String]) -> [String: String] {
        env.filter { ["KUBECONFIG", "AWS_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION", "PATH"].contains($0.key) && !$0.value.isEmpty }
    }
}

public enum Tunnels {
    public static func isOn(_ t: Go.sshhosts.TunnelStatus) -> Bool { ["up", "connecting", "retrying"].contains(t.state) }

    /// The local port a tunnel listens on (local, SOCKS and kubectl).
    public static func localPort(_ t: Go.sshhosts.TunnelStatus) -> String? {
        switch t.kind {
        case "remote": return nil
        case "kubectl": return t.spec.split(separator: ":").first.map(String.init)
        default:
            let listen = t.kind == "dynamic" ? t.spec : TunnelDraft.forwardParts(t.spec)?[0] ?? ""
            guard let r = listen.range(of: #"\d+$"#, options: .regularExpression) else { return nil }
            return String(listen[r])
        }
    }

    /// "localhost:5432 → db.internal:5432 via bastion"
    public static func describe(_ t: Go.sshhosts.TunnelStatus) -> String {
        if t.kind == "dynamic" { return "SOCKS proxy on localhost:\(t.spec) via \(t.host)" }
        if t.kind == "kubectl" {
            let parts = t.spec.split(separator: ":", omittingEmptySubsequences: false).map(String.init)
            return "localhost:\(parts[0]) → \(t.resource):\(parts.count > 1 ? parts[1] : parts[0]) in \(t.namespace)"
        }
        guard let m = TunnelDraft.forwardParts(t.spec) else { return "\(t.spec) via \(t.host)" }
        let listen = m[0].contains(":") ? m[0] : "localhost:\(m[0])"
        return t.kind == "local" ? "\(listen) → \(m[1]):\(m[2]) via \(t.host)" : "\(t.host):\(m[0]) → \(m[1]):\(m[2]) here"
    }

    public static func stateLabel(_ s: String) -> String {
        switch s {
        case "connecting": "connecting…"
        case "up": "on"
        case "retrying": "reconnecting…"
        case "failed": "failed"
        default: "off"
        }
    }
}

public enum HostKeyVerdict {
    /// What a scan says against the stored keys.
    public static func of(scanned: [Go.sshhosts.HostKey], stored: [Go.sshhosts.HostKey]) -> String {
        let fps = Set(stored.filter { $0.marker.isEmpty }.map(\.fingerprint))
        if scanned.isEmpty { return "The server offered no keys." }
        if fps.isEmpty { return "None of these keys are stored yet." }
        let k = scanned.filter { fps.contains($0.fingerprint) }.count
        if k == scanned.count { return "All of the server’s keys match the stored ones." }
        if k == 0 { return "None of the server’s keys match the stored ones." }
        return "\(k) of the server’s \(scanned.count) keys match the stored ones."
    }
}

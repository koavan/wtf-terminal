// The Network, Processes and Resource Usage rules: addresses, which
// connections show, how processes sort and nest, and number formats. Ports
// of frontend/src/lib/network.svelte.ts, processes.svelte.ts and usage.svelte.ts.
import Foundation

public typealias Conn = Go.netconn.Conn
public typealias Proc = Go.procs.Proc

public enum Net {
    /// "host:port" (IPv6 as "[v6]:port") → host without brackets, and port.
    public static func splitAddress(_ a: String) -> (host: String, port: String) {
        guard let i = a.lastIndex(of: ":") else { return (a, "") }
        var host = String(a[..<i])
        if host.hasPrefix("["), host.hasSuffix("]") { host = String(host.dropFirst().dropLast()) }
        return (host, String(a[a.index(after: i)...]))
    }

    /// Whether other machines can reach a listener on this host.
    public static func isExposed(_ host: String) -> Bool { !(host == "localhost" || host.hasPrefix("127.") || host == "::1") }

    /// One row per program, protocol, port and exposure: 127.0.0.1:3000 and [::1]:3000 show once.
    public static func mergeListeners(_ rows: [Conn]) -> [Conn] {
        var seen = Set<String>()
        return rows.filter { c in
            let (host, port) = splitAddress(c.local)
            return seen.insert("\(c.pid)|\(c.protocol)|\(port)|\(isExposed(host))").inserted
        }
    }

    public static func serviceName(_ port: String) -> String? {
        ["22": "ssh", "53": "dns", "80": "http", "443": "https", "993": "imaps", "587": "smtp", "3306": "mysql",
         "5432": "postgres", "6379": "redis", "27017": "mongodb", "9200": "elasticsearch", "5672": "amqp",
         "9092": "kafka", "11211": "memcached", "8080": "http-alt", "5353": "mdns"][port]
    }

    /// The rows to show: a "pid:N" filter picks one process; Terminals hides
    /// what wtf's shells didn't start (except listeners of active forwards).
    public static func visible(_ conns: [Conn], terminalsOnly: Bool, filter: String, forwardPorts: Set<String>) -> [Conn] {
        let q = filter.trimmingCharacters(in: .whitespaces).lowercased()
        if let r = q.range(of: #"^pid:\s*(\d+)$"#, options: .regularExpression),
           let pid = Int(q[r].drop { !$0.isNumber }) {
            return conns.filter { $0.pid == pid }
        }
        return conns.filter { c in
            if terminalsOnly, c.session.isEmpty, !(c.state == "LISTEN" && forwardPorts.contains(splitAddress(c.local).port)) { return false }
            return q.isEmpty || "\(c.process) \(c.local) \(c.remote) \(c.pid)".lowercased().contains(q)
        }
    }

    /// "close wait" for CLOSE_WAIT; nothing for ESTABLISHED.
    public static func stateLabel(_ s: String) -> String? {
        s.isEmpty || s == "ESTABLISHED" ? nil : s.lowercased().replacingOccurrences(of: "_", with: " ")
    }
}

public enum Procs {
    public enum Sort: String, Sendable { case cpu, memory, name }

    public static func cpu(_ c: Double) -> String { (c < 10 ? String(format: "%.1f", c) : String(format: "%.0f", c)) + "%" }

    public static func bytes(_ n: Int) -> String {
        let mib = 1024.0 * 1024, gib = mib * 1024, d = Double(n)
        if d < mib { return "\(max(1, Int((d / 1024).rounded()))) KB" }
        if d < gib { return "\(Int((d / mib).rounded())) MB" }
        return String(format: "%.1f GB", d / gib)
    }

    public static func elapsed(_ s: Int) -> String {
        if s < 60 { return "\(s)s" }
        if s < 3600 { return "\(s / 60)m" }
        if s < 86400 { return "\(s / 3600)h \(s % 3600 / 60)m" }
        return "\(s / 86400)d \(s % 86400 / 3600)h"
    }

    public static func matches(_ p: Proc, _ filter: String) -> Bool {
        let q = filter.trimmingCharacters(in: .whitespaces).lowercased()
        return q.isEmpty || "\(p.name) \(p.command) \(p.pid)".lowercased().contains(q)
    }

    public static func sorted(_ list: [Proc], by: Sort) -> [Proc] {
        list.sorted { a, b in
            switch by {
            case .cpu: a.cpu != b.cpu ? a.cpu > b.cpu : a.memory > b.memory
            case .memory: a.memory != b.memory ? a.memory > b.memory : a.cpu > b.cpu
            case .name:
                a.name.lowercased() != b.name.lowercased() ? a.name.lowercased() < b.name.lowercased() : a.pid < b.pid
            }
        }
    }

    /// One session's processes as a tree: the shell first, children by pid.
    public static func tree(_ list: [Proc]) -> [(proc: Proc, depth: Int)] {
        let pids = Set(list.map(\.pid))
        var children: [Int: [Proc]] = [:]
        for p in list { children[p.ppid, default: []].append(p) }
        for k in children.keys { children[k]!.sort { $0.pid < $1.pid } }
        let roots = list.filter { $0.shell || !pids.contains($0.ppid) }
            .sorted { ($0.shell ? 0 : 1, $0.pid) < ($1.shell ? 0 : 1, $1.pid) }
        var out: [(Proc, Int)] = []
        var seen = Set<Int>()
        func walk(_ p: Proc, _ d: Int) {
            guard seen.insert(p.pid).inserted else { return }
            out.append((p, d))
            for c in children[p.pid] ?? [] { walk(c, d + 1) }
        }
        for r in roots { walk(r, 0) }
        return out
    }
}

public enum Usage {
    /// The app's own CPU (-1 before a second sample) and memory; your programs aren't counted.
    public static func total(_ u: Go.usage.Usage) -> (cpu: Double, memory: Int) {
        (u.interval > 0 ? u.app.cpu + u.window.cpu : -1, u.app.memory + u.window.memory)
    }

    public static func cpu(_ c: Double) -> String {
        if c < 0 { return "…" }
        return c < 10 ? String(format: "%.1f%%", c) : "\(Int(c.rounded()))%"
    }

    public static func bytes(_ n: Int) -> String {
        var v = Double(n)
        var unit = 0
        let units = ["B", "KB", "MB", "GB", "TB"]
        while v >= 1024, unit < units.count - 1 {
            v /= 1024
            unit += 1
        }
        return unit >= 3 ? String(format: "%.1f %@", v, units[unit]) : "\(Int(v.rounded())) \(units[unit])"
    }
}

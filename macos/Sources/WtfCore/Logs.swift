// What a terminal's output says, as tokens: log levels, timestamps, URLs,
// IP addresses, file:line paths, Kubernetes resources and JWTs. They're
// colored on screen and get a menu on ⌘-click or right-click. Ports of
// lib/logs.ts and lib/logActions.ts. Offsets are UTF-16 units, as in the
// terminal's logical lines.
import Foundation

public enum Logs {
    public enum Level: String, Sendable { case error, warn, info, debug }

    public enum Kind: Equatable, Sendable {
        case level(Level)
        case timestamp, time, url, jwt
        case ip(address: String, port: Int?, cidr: Bool)
        case path(path: String, line: Int, col: Int?)
        case k8s(resource: String, name: String)
    }

    public struct Token: Equatable, Sendable {
        public var kind: Kind
        public var start: Int
        public var end: Int
        public var text: String
    }

    /// Longer lines aren't scanned (minified files, base64 blobs).
    public static let maxScan = 4000

    // JavaScript's \w and \d are ASCII; ICU's are Unicode.
    static func js(_ p: String) -> String {
        p.replacingOccurrences(of: #"\w"#, with: "[A-Za-z0-9_]").replacingOccurrences(of: #"\d"#, with: "[0-9]")
    }

    static func re(_ p: String, _ opts: NSRegularExpression.Options = []) -> NSRegularExpression {
        try! NSRegularExpression(pattern: js(p), options: opts)
    }

    static let month = "(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)"
    static let urlRe = re(#"\b(?:https?|ftp|file)://[^\s<>"'`]+"#)
    static let jwtRe = re(Inspect.jwtPattern)
    static let timestampRes = [
        re(#"(?<![\w.])\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}(?::\d{2}(?:[.,]\d{1,9})?)?(?: ?(?:Z|[+-]\d{2}:?\d{2})(?![\w:]))?"#),
        re(#"(?<![\w/])\d{2}/"# + month + #"/\d{4}:\d{2}:\d{2}:\d{2}(?: [+-]\d{4})?"#),
        re(#"(?<![\w])"# + month + #" [ \d]\d \d{2}:\d{2}:\d{2}(?![\w:])"#),
    ]
    static let timeRe = re(#"(?<![\w:.])\d{2}:\d{2}:\d{2}(?:[.,]\d{1,9})?(?![\w:])"#)
    static let octet = #"(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)"#
    static let ipv4Re = re(#"(?<![\w.])((?:"# + octet + #"\.){3}"# + octet + #")(?:(/\d{1,2})|:(\d{1,5}))?(?![\w]|\.\d)"#)
    static let hex = "[0-9a-fA-F]{1,4}"
    static let ipv6Re = re(#"(?<![\w:])((?:"# + hex + "(?::" + hex + ")*)?::(?:" + hex + "(?::" + hex + ")*)?|(?:" + hex + ":){7}" + hex + #")(/\d{1,3})?(?![\w:])"#)
    static let pathRe = re(#"(?<![\w/.~@+-])((?:~|\.{1,2})?/?(?:[\w.@+-]+/)*[\w@+-][\w.@+-]*\.[A-Za-z]\w{0,9}):(\d+)(?::(\d+))?(?![\w.]|:\d)"#)
    static let pyPathRe = re(#"File "([^"]+)", line (\d+)"#)
    static let k8sKinds = ["pods?", "deployments?", "deploy", "statefulsets?", "sts", "daemonsets?", "replicasets?", "jobs?", "cronjobs?",
                           "services?", "svc", "configmaps?", "secrets?", "ingress(?:es)?", "nodes?", "namespaces?",
                           "persistentvolumeclaims?", "pvc", "persistentvolumes?", "serviceaccounts?", "horizontalpodautoscalers?", "hpa",
                           "networkpolic(?:y|ies)", "endpoints", "endpointslices?", "roles?", "rolebindings?", "clusterroles?",
                           "clusterrolebindings?", "poddisruptionbudgets?", "pdb", "storageclass(?:es)?", "customresourcedefinitions?", "crd"]
    static let k8sRe = re(#"(?<![\w./-])((?:"# + k8sKinds.joined(separator: "|") + #")(?:\.[a-z0-9-]+(?:\.[a-z0-9-]+)*)?)/([a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?)(?![\w/-])"#)
    static let levelWords: [(String, Level)] = [
        ("FATAL", .error), ("PANIC", .error), ("CRITICAL", .error), ("CRIT", .error), ("EMERG", .error), ("ALERT", .error),
        ("ERROR", .error), ("ERR", .error), ("SEVERE", .error), ("WARNING", .warn), ("WARN", .warn),
        ("INFO", .info), ("NOTICE", .info), ("DEBUG", .debug), ("TRACE", .debug),
    ]
    static let levelAlt = levelWords.map(\.0).joined(separator: "|")
    static let levelRes = [
        re(#"(?<![\w-])("# + levelAlt + #")(?![\w-])"#),
        re(#"(?:\b(?:level|lvl|severity)["']?\s*[=:]\s*["']?|\[)("# + levelAlt + #")(?![\w-])"#, .caseInsensitive),
    ]

    /// A URL without the punctuation after it, keeping a closing bracket it opened.
    public static func trimUrl(_ url: String) -> String {
        var u = url
        while let last = u.last, ".,;:!?'\")]}>".contains(last) {
            let pairs: [Character: Character] = [")": "(", "]": "[", "}": "{"]
            if let open = pairs[last], u.filter({ $0 == open }).count >= u.filter({ $0 == last }).count { return u }
            u.removeLast()
        }
        return u
    }

    /// The tokens in a line of output, in order; where they'd overlap, the first found wins.
    public static func scan(_ text: String) -> [Token] {
        let ns = text as NSString
        guard ns.length > 0, ns.length <= maxScan else { return [] }
        var taken: [Token] = []
        func add(_ kind: Kind, _ start: Int, _ end: Int) {
            guard end > start, taken.allSatisfy({ end <= $0.start || start >= $0.end }) else { return }
            taken.append(Token(kind: kind, start: start, end: end, text: ns.substring(with: NSRange(location: start, length: end - start))))
        }
        let all = NSRange(location: 0, length: ns.length)
        func group(_ m: NSTextCheckingResult, _ i: Int) -> String? {
            m.range(at: i).location == NSNotFound ? nil : ns.substring(with: m.range(at: i))
        }
        for m in urlRe.matches(in: text, range: all) {
            let t = trimUrl(ns.substring(with: m.range))
            add(.url, m.range.location, m.range.location + (t as NSString).length)
        }
        for m in jwtRe.matches(in: text, range: all) { add(.jwt, m.range.location, NSMaxRange(m.range)) }
        for r in timestampRes { for m in r.matches(in: text, range: all) { add(.timestamp, m.range.location, NSMaxRange(m.range)) } }
        for m in pyPathRe.matches(in: text, range: all) {
            let p = m.range(at: 1)
            add(.path(path: ns.substring(with: p), line: Int(group(m, 2) ?? "") ?? 0, col: nil), p.location, NSMaxRange(p))
        }
        for m in pathRe.matches(in: text, range: all) {
            add(.path(path: group(m, 1) ?? "", line: Int(group(m, 2) ?? "") ?? 0, col: group(m, 3).flatMap { Int($0) }),
                m.range.location, NSMaxRange(m.range))
        }
        for m in k8sRe.matches(in: text, range: all) {
            add(.k8s(resource: group(m, 1) ?? "", name: group(m, 2) ?? ""), m.range.location, NSMaxRange(m.range))
        }
        for m in ipv4Re.matches(in: text, range: all) {
            var port: Int?
            if let p = group(m, 3) {
                guard let n = Int(p), (1...65535).contains(n) else { continue }
                port = n
            }
            add(.ip(address: group(m, 1) ?? "", port: port, cidr: group(m, 2) != nil), m.range.location, NSMaxRange(m.range))
        }
        for m in ipv6Re.matches(in: text, range: all) {
            guard let a = group(m, 1), (a as NSString).length >= 3 else { continue }
            add(.ip(address: a, port: nil, cidr: group(m, 2) != nil), m.range.location, NSMaxRange(m.range))
        }
        for m in timeRe.matches(in: text, range: all) { add(.time, m.range.location, NSMaxRange(m.range)) }
        for r in levelRes {
            for m in r.matches(in: text, range: all) {
                let w = m.range(at: 1)
                guard let level = levelWords.first(where: { $0.0 == ns.substring(with: w).uppercased() })?.1 else { continue }
                add(.level(level), w.location, NSMaxRange(w))
            }
        }
        return taken.sorted { $0.start < $1.start }
    }

    /// `deployments.apps` → `deployment`, `svc` → `service`.
    public static func k8sKind(_ resource: String) -> String {
        let base = String(resource.split(separator: ".", maxSplits: 1).first ?? "")
        let short = ["deploy": "deployment", "sts": "statefulset", "svc": "service", "pvc": "persistentvolumeclaim",
                     "hpa": "horizontalpodautoscaler", "pdb": "poddisruptionbudget", "crd": "customresourcedefinition"]
        if let s = short[base] { return s }
        if base == "endpoints" { return base }
        if base.hasSuffix("ies") { return base.dropLast(3) + "y" }
        if base.hasSuffix("sses") || base.hasSuffix("xes") { return String(base.dropLast(2)) }
        if base.hasSuffix("s"), !base.hasSuffix("ss") { return String(base.dropLast()) }
        return base
    }

    static let months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]

    /// A timestamp's moment, and whether it said its time zone (else it's read as local time).
    public static func parseTimestamp(_ text: String, now: Date = Date(), zone: TimeZone = .current) -> (date: Date, zoned: Bool)? {
        let ns = text as NSString
        let all = NSRange(location: 0, length: ns.length)
        func g(_ m: NSTextCheckingResult, _ i: Int) -> String? { m.range(at: i).location == NSNotFound ? nil : ns.substring(with: m.range(at: i)) }
        func build(_ y: Int, _ mo: Int, _ d: Int, _ h: Int, _ mi: Int, _ s: Int, _ ms: Int, _ z: String?) -> (Date, Bool)? {
            guard (0...11).contains(mo), (1...31).contains(d), h <= 23, mi <= 59, s <= 60 else { return nil }
            var cal = Calendar(identifier: .gregorian)
            var offset = 0
            var zoned = false
            if let z {
                zoned = true
                if z != "Z" {
                    let digits = z.dropFirst().filter(\.isNumber)
                    guard digits.count == 4 else { return nil }
                    offset = (Int(digits.prefix(2))! * 60 + Int(digits.suffix(2))!) * (z.hasPrefix("-") ? -1 : 1)
                }
                cal.timeZone = TimeZone(identifier: "UTC")!
            } else {
                cal.timeZone = zone
            }
            guard var date = cal.date(from: DateComponents(year: y, month: mo + 1, day: d, hour: h, minute: mi, second: s)) else { return nil }
            date += Double(ms) / 1000 - Double(offset * 60)
            return (date, zoned)
        }
        let iso = re(#"^(\d{4})-(\d{2})-(\d{2})[T ](\d{2}):(\d{2})(?::(\d{2})(?:[.,](\d{1,9}))?)? ?(Z|[+-]\d{2}:?\d{2})?$"#)
        if let m = iso.firstMatch(in: text, range: all) {
            let frac = g(m, 7).map { Int((Double("0." + $0)! * 1000).rounded()) } ?? 0
            return build(Int(g(m, 1)!)!, Int(g(m, 2)!)! - 1, Int(g(m, 3)!)!, Int(g(m, 4)!)!, Int(g(m, 5)!)!, Int(g(m, 6) ?? "0")!, frac, g(m, 8))
        }
        let access = re(#"^(\d{2})/(\w{3})/(\d{4}):(\d{2}):(\d{2}):(\d{2})(?: ([+-]\d{4}))?$"#)
        if let m = access.firstMatch(in: text, range: all) {
            guard let mo = months.firstIndex(of: g(m, 2)!) else { return nil }
            return build(Int(g(m, 3)!)!, mo, Int(g(m, 1)!)!, Int(g(m, 4)!)!, Int(g(m, 5)!)!, Int(g(m, 6)!)!, 0, g(m, 7))
        }
        let syslog = re(#"^(\w{3}) [ \d]?(\d{1,2}) (\d{2}):(\d{2}):(\d{2})$"#)
        if let m = syslog.firstMatch(in: text, range: all) {
            guard let mo = months.firstIndex(of: g(m, 1)!) else { return nil }
            var cal = Calendar(identifier: .gregorian)
            cal.timeZone = zone
            let year = cal.component(.year, from: now)
            guard let r = build(year, mo, Int(g(m, 2)!)!, Int(g(m, 3)!)!, Int(g(m, 4)!)!, Int(g(m, 5)!)!, 0, nil) else { return nil }
            // December's log read in January was last year.
            if r.0.timeIntervalSince(now) > 31 * 86400 { return build(year - 1, mo, Int(g(m, 2)!)!, Int(g(m, 3)!)!, Int(g(m, 4)!)!, Int(g(m, 5)!)!, 0, nil) }
            return r
        }
        return nil
    }

    // MARK: Menus

    public enum Action: Equatable, Sendable {
        case openUrl(String)
        case copy(String, what: String)
        case copySecret(String, what: String)
        /// A new tab running the command.
        case runInTab(String)
        /// The focused shell, or a new tab if it's busy.
        case runHere(String)
        /// kubectl describe: in the shell as is, or with the shell's variables in a new tab.
        case describe(kind: String, name: String)
        case openInEditor(path: String, line: Int)
        case openFile(String)
        case reveal(String)
        case showSecret(String)
        case inspectJwt(String)
        case inspectTls(String)
    }

    public enum Entry: Equatable, Sendable {
        case heading(String)
        case item(String, hint: String? = nil, Action)
        case separator
    }

    /// `Thu, Oct 9, 2026, 12:34:56`
    static func longTime(_ d: Date, _ zone: TimeZone) -> String {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US")
        f.timeZone = zone
        f.setLocalizedDateFormatFromTemplate("EEEdMMMyyyyHHmmss")
        return f.string(from: d)
    }

    /// The menu for a token (paths already made absolute). Levels and bare times have none.
    public static func menu(_ token: Token, target: Kube.Target, now: Date = Date(), zone: TimeZone = .current) -> [Entry] {
        let text = token.text
        switch token.kind {
        case .url:
            var out: [Entry] = [.heading(text.count > 60 ? String(text.prefix(59)) + "…" : text),
                                .item("Open in Browser", hint: "⌘-click", .openUrl(text)), .item("Copy URL", .copy(text, what: "URL"))]
            if text.lowercased().hasPrefix("https://") { out.append(.item("Inspect TLS Certificate…", .inspectTls(text))) }
            return out
        case .jwt:
            return [.heading("JWT"), .item("Inspect Token…", hint: "⌘-click", .inspectJwt(text)), .item("Copy Token", .copySecret(text, what: "token"))]
        case .timestamp:
            guard let (date, zoned) = parseTimestamp(text, now: now, zone: zone) else { return [] }
            let rel = Badges.relativeTime(date.timeIntervalSince1970 * 1000, now: now.timeIntervalSince1970 * 1000)
            var out: [Entry] = [.heading("\(longTime(date, zone)) your time · \(rel)"), .heading("\(longTime(date, TimeZone(identifier: "UTC")!)) UTC")]
            if !zoned { out.append(.heading("No time zone in the text: read as your time")) }
            let iso = ISO8601DateFormatter()
            iso.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            out += [.item("Copy as UTC (ISO 8601)", .copy(iso.string(from: date), what: "time")),
                    .item("Copy as Your Time", .copy(longTime(date, zone), what: "time")),
                    .item("Copy as Unix Time", .copy(String(Int(date.timeIntervalSince1970.rounded(.down))), what: "Unix time"))]
            return out
        case .ip(let address, let port, let cidr):
            var out: [Entry] = [.heading(text), .item("Copy Address", .copy(address, what: "address"))]
            if port != nil { out.append(.item("Copy \(text)", .copy(text, what: "address and port"))) }
            if !cidr {
                let host = address.contains(":") ? "[\(address)]" : address
                if let port { out.append(.item("Open http://\(host):\(port)", .openUrl("http://\(host):\(port)"))) }
                out.append(.item("SSH to \(address)", .runHere("ssh \(address)")))
            }
            return out
        case .path(let path, let line, _):
            return [.heading("\(path):\(line)"), .item("Open in Editor at Line \(line)", .openInEditor(path: path, line: line)),
                    .item("Open with Default App", .openFile(path)), .item("Reveal in Finder", .reveal(path)), .separator,
                    .item("Copy Path", .copy(path, what: "path")), .item("Copy Path:\(line)", .copy("\(path):\(line)", what: "path and line"))]
        case .k8s(let resource, let name):
            let kind = k8sKind(resource)
            var out: [Entry] = [.heading("\(kind)/\(name)"),
                                .heading(target.context.isEmpty ? "kubectl's current context (this shell reported none)"
                                    : "Context \(target.context)" + (target.namespace.isEmpty ? "" : " · namespace \(target.namespace)"))]
            if kind == "pod" {
                out.append(.item("Logs", .runInTab(Kube.logs(target, pod: name))))
            } else if ["deployment", "statefulset", "daemonset", "replicaset", "job"].contains(kind) {
                out.append(.item("Logs", .runInTab(Kube.workloadLogs(target, kind, name))))
            }
            out.append(.item("Describe", .describe(kind: kind, name: name)))
            // A secret's YAML would print its values in the clear.
            out.append(kind == "secret" ? .item("View Decoded Values…", .showSecret(name)) : .item("View YAML", .runInTab(Kube.yaml(target, kind, name))))
            out += [.separator, .item("Copy Name", .copy(name, what: "name")), .item("Copy \(kind)/\(name)", .copy("\(kind)/\(name)", what: "resource"))]
            return out
        case .level, .time:
            return []
        }
    }

    /// Kinds ⌘-click opens a menu for (URLs open directly; levels and times aren't links).
    public static func isLink(_ k: Kind) -> Bool {
        switch k {
        case .timestamp, .ip, .path, .k8s, .jwt: true
        default: false
        }
    }

    /// A file a path token names, if it's on this computer: `~` is home,
    /// relative paths start at the shell's folder. Like files.ResolveFile.
    public static func resolveFile(_ name: String, cwd: String, home: String = NSHomeDirectory()) -> String? {
        var p = name
        if p == "~" { p = home } else if p.hasPrefix("~/") { p = home + p.dropFirst() }
        if !p.hasPrefix("/") {
            guard cwd.hasPrefix("/") else { return nil }
            p = (cwd as NSString).appendingPathComponent(p)
        }
        p = (p as NSString).standardizingPath
        var dir: ObjCBool = false
        guard FileManager.default.fileExists(atPath: p, isDirectory: &dir), !dir.boolValue else { return nil }
        return p
    }
}

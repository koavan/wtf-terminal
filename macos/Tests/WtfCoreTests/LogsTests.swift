import Foundation
import Testing
@testable import WtfCore

func kinds(_ line: String) -> [String] {
    Logs.scan(line).map { t -> String in
        switch t.kind {
        case .level(let l): "level:\(l.rawValue):\(t.text)"
        case .timestamp: "timestamp:\(t.text)"
        case .time: "time:\(t.text)"
        case .url: "url:\(t.text)"
        case .jwt: "jwt"
        case .ip: "ip:\(t.text)"
        case .path: "path:\(t.text)"
        case .k8s: "k8s:\(t.text)"
        }
    }
}

func ips(_ line: String) -> [String] {
    Logs.scan(line).compactMap { if case .ip(let a, let p, let c) = $0.kind { "\(a)|\(p.map(String.init) ?? "-")|\(c)" } else { nil } }
}

func paths(_ line: String) -> [String] {
    Logs.scan(line).compactMap { if case .path(let p, let l, let c) = $0.kind { "\(p)|\(l)|\(c.map(String.init) ?? "-")" } else { nil } }
}

func k8s(_ line: String) -> [String] {
    Logs.scan(line).compactMap { if case .k8s(let r, let n) = $0.kind { "\(r)|\(n)" } else { nil } }
}

@Test func scansALogLine() {
    #expect(kinds("2026-10-09T12:34:56.789Z ERROR request from 10.0.3.7:52144 failed: see https://example.com/x.") ==
        ["timestamp:2026-10-09T12:34:56.789Z", "level:error:ERROR", "ip:10.0.3.7:52144", "url:https://example.com/x"])
}

@Test func levels() {
    #expect(kinds("[WARN] disk 91% full") == ["level:warn:WARN"])
    #expect(kinds(#"level=error msg="boom""#) == ["level:error:error"])
    #expect(kinds(#"{"level":"info","msg":"ok"}"#) == ["level:info:info"])
    #expect(kinds("[debug] cache hit") == ["level:debug:debug"])
    #expect(kinds("FATAL: out of memory") == ["level:error:FATAL"])
    #expect(kinds("ERROR_CODES.md INFOS warning: an error happened") == [])
}

@Test func timestamps() {
    #expect(kinds("2026-10-09 12:34:56,123 +02:00 started") == ["timestamp:2026-10-09 12:34:56,123 +02:00"])
    #expect(kinds(#"1.2.3.4 - - [09/Oct/2026:13:55:36 -0700] "GET /""#)[1] == "timestamp:09/Oct/2026:13:55:36 -0700")
    #expect(kinds("Oct  9 12:34:56 host sshd[42]: Accepted") == ["timestamp:Oct  9 12:34:56"])
    #expect(kinds("took until 12:34:56.5 today") == ["time:12:34:56.5"])
    #expect(!kinds("ether aa:bb:12:34:56:78").contains { $0.hasPrefix("time:") })
}

@Test func ipAddresses() {
    #expect(ips("peer 192.168.1.20:8443 in 10.0.0.0/16.") == ["192.168.1.20|8443|false", "10.0.0.0|-|true"])
    #expect(ips("listen [::1]:80 and fe80::1c2:3 and 2001:db8:0:0:0:0:2:1") == ["::1|-|false", "fe80::1c2:3|-|false", "2001:db8:0:0:0:0:2:1|-|false"])
    #expect(ips("version 999.1.1.1 and 1.2.3.4.5") == [])
    #expect(ips("std::vector Foo::Bar") == [])
}

@Test func filePaths() {
    #expect(paths("panic at /Users/me/app/main.go:42 +0x1d") == ["/Users/me/app/main.go|42|-"])
    #expect(paths("src/lib/app.ts:3:14 - error TS2304") == ["src/lib/app.ts|3|14"])
    #expect(paths(#"  File "app/views.py", line 88, in index"#) == ["app/views.py|88|-"])
    #expect(paths("./run.sh:7: command not found") == ["./run.sh|7|-"])
    #expect(paths("at 12:30:45 and v1.2:3") == [])
}

@Test func kubernetesResources() {
    #expect(k8s("deployment.apps/api-server restarted") == ["deployment.apps|api-server"])
    #expect(k8s("pod/web-7d9f8-x2x9z created, service/web unchanged.") == ["pod|web-7d9f8-x2x9z", "service|web"])
    #expect(k8s("src/pods/list.ts and ./secret/key and pod/UPPER") == [])
    for (r, k) in [("deploy", "deployment"), ("deployments.apps", "deployment"), ("pods", "pod"), ("ingresses", "ingress"),
                   ("networkpolicies", "networkpolicy"), ("storageclasses", "storageclass"), ("endpoints", "endpoints"), ("job.batch", "job")] {
        #expect(Logs.k8sKind(r) == k, "\(r)")
    }
}

@Test func urlsAndSpans() {
    #expect(kinds("GET http://10.1.2.3:8080/health.json:12 ok") == ["url:http://10.1.2.3:8080/health.json:12"])
    #expect(kinds("(see https://en.wikipedia.org/wiki/Foo_(bar))") == ["url:https://en.wikipedia.org/wiki/Foo_(bar)"])
    let line = "x  WARN  pod/a-1" as NSString
    for t in Logs.scan(line as String) { #expect(line.substring(with: NSRange(location: t.start, length: t.end - t.start)) == t.text) }
    #expect(Logs.scan(String(repeating: "ERROR ", count: 1000)).isEmpty)
}

@Test func parsesTimestamps() {
    let iso = ISO8601DateFormatter()
    iso.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    func utc(_ s: String) -> String? { Logs.parseTimestamp(s).map { iso.string(from: $0.date) } }
    #expect(utc("2026-10-09T12:34:56.789Z") == "2026-10-09T12:34:56.789Z")
    #expect(Logs.parseTimestamp("2026-10-09T12:34:56.789Z")?.zoned == true)
    #expect(utc("2026-10-09 14:34:56,5 +02:00") == "2026-10-09T12:34:56.500Z")
    #expect(utc("09/Oct/2026:13:55:36 -0700") == "2026-10-09T20:55:36.000Z")
    let local = TimeZone.current
    var cal = Calendar(identifier: .gregorian)
    cal.timeZone = local
    let noZone = Logs.parseTimestamp("2026-10-09 12:34")
    #expect(noZone?.zoned == false && noZone.map { cal.component(.hour, from: $0.date) } == 12)
    let oct10 = cal.date(from: DateComponents(year: 2026, month: 10, day: 10))!
    #expect(Logs.parseTimestamp("Oct  9 12:34:56", now: oct10).map { cal.component(.year, from: $0.date) } == 2026)
    let jan2 = cal.date(from: DateComponents(year: 2027, month: 1, day: 2))!
    #expect(Logs.parseTimestamp("Dec 31 23:00:00", now: jan2).map { cal.component(.year, from: $0.date) } == 2026)
    #expect(Logs.parseTimestamp("2026-13-40T10:00:00Z") == nil && Logs.parseTimestamp("12:34:56") == nil)
}

let menuTarget = Kube.Target(context: "prod", namespace: "shop")
func menu(_ line: String, _ t: Kube.Target = menuTarget, now: Date = Date()) -> [Logs.Entry] {
    guard let tok = Logs.scan(line).first(where: { if case .level = $0.kind { false } else { true } }) else { return [] }
    return Logs.menu(tok, target: t, now: now, zone: TimeZone(identifier: "UTC")!)
}
func labels(_ m: [Logs.Entry]) -> [String] {
    m.map { e in
        switch e {
        case .heading(let h): "# " + h
        case .item(let l, _, _): l
        case .separator: "—"
        }
    }
}
func action(_ m: [Logs.Entry], _ label: String) -> Logs.Action? {
    for case .item(let l, _, let a) in m where l == label { return a }
    return nil
}

@Test func kubeMenus() {
    let m = menu("pod/web-1 created")
    #expect(labels(m) == ["# pod/web-1", "# Context prod · namespace shop", "Logs", "Describe", "View YAML", "—", "Copy Name", "Copy pod/web-1"])
    #expect(action(m, "Logs") == .runInTab("kubectl --context prod -n shop logs -f --tail=200 web-1"))
    #expect(action(m, "Describe") == .describe(kind: "pod", name: "web-1"))
    let d = menu("deployment.apps/api restarted", Kube.Target())
    #expect(labels(d)[1] == "# kubectl's current context (this shell reported none)")
    #expect(action(d, "Logs") == .runInTab("kubectl logs -f --tail=200 --all-containers --prefix deployment/api"))
    let s = menu("secret/db-creds configured")
    #expect(!labels(s).contains("View YAML") && action(s, "View Decoded Values…") == .showSecret("db-creds"))
}

@Test func otherMenus() {
    let now = ISO8601DateFormatter().date(from: "2026-10-09T12:05:00Z")!
    let t = menu("2026-10-09T12:00:00Z start", now: now)
    #expect(labels(t)[0].hasSuffix("your time · 5 min ago"))
    #expect(!labels(t).contains("# No time zone in the text: read as your time"))
    #expect(action(t, "Copy as Unix Time") == .copy("1791547200", what: "Unix time"))
    #expect(action(t, "Copy as UTC (ISO 8601)") == .copy("2026-10-09T12:00:00.000Z", what: "time"))
    #expect(labels(menu("2026-10-09 12:00:00 start", now: now)).contains("# No time zone in the text: read as your time"))
    let ip = menu("10.0.0.5:8080 up")
    #expect(action(ip, "Open http://10.0.0.5:8080") == .openUrl("http://10.0.0.5:8080"))
    #expect(action(ip, "SSH to 10.0.0.5") == .runHere("ssh 10.0.0.5"))
    #expect(labels(menu("10.0.0.0/8")) == ["# 10.0.0.0/8", "Copy Address"])
    var p = Logs.scan("main.go:12")[0]
    p.kind = .path(path: "/src/main.go", line: 12, col: nil)
    #expect(action(Logs.menu(p, target: menuTarget), "Open in Editor at Line 12") == .openInEditor(path: "/src/main.go", line: 12))
    #expect(menu("ERROR").isEmpty && menu("at 10:00:00 ok").isEmpty)
    #expect(action(menu("see https://api.example.com/v1"), "Inspect TLS Certificate…") == .inspectTls("https://api.example.com/v1"))
    #expect(action(menu("see http://api.example.com/v1"), "Inspect TLS Certificate…") == nil)
    let jwt = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhZGEifQ.sig"
    let j = menu(jwt)
    #expect(labels(j) == ["# JWT", "Inspect Token…", "Copy Token"])
    #expect(action(j, "Copy Token") == .copySecret(jwt, what: "token"))
}

@Test func resolvesFiles() throws {
    let dir = FileManager.default.temporaryDirectory.appendingPathComponent("wtf-logs-\(UUID().uuidString)")
    try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: dir) }
    try "x".write(to: dir.appendingPathComponent("main.go"), atomically: true, encoding: .utf8)
    #expect(Logs.resolveFile("main.go", cwd: dir.path) == dir.appendingPathComponent("main.go").standardizedFileURL.path
        || Logs.resolveFile("main.go", cwd: dir.path)?.hasSuffix("/main.go") == true)
    #expect(Logs.resolveFile("nope.go", cwd: dir.path) == nil)
    #expect(Logs.resolveFile("main.go", cwd: "") == nil)
    #expect(Logs.resolveFile(".", cwd: dir.path) == nil)
}

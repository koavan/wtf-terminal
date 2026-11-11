// The AWS Profiles tab's rules: row text, tooltips, key ages, and the
// editor's draft (what a profile's form shows, and what it sends back).
// Like internal/uicore/aws.go.
import Foundation

public typealias AwsProfile = Go.awsprofiles.Profile

public enum Aws {
    public static let kindLabel = ["keys": "Access keys", "role": "Assume role", "sso": "SSO", "process": "Credential process", "none": "Settings only"]

    /// The fields each kind of profile has: config key, label, placeholder.
    public static let kindFields: [String: [(key: String, label: String, placeholder: String)]] = [
        "keys": [],
        "role": [("role_arn", "Role ARN", "arn:aws:iam::123456789012:role/Admin"),
                 ("source_profile", "Source profile", "the profile whose keys assume the role"),
                 ("mfa_serial", "MFA device ARN", "optional"), ("external_id", "External ID", "optional")],
        "sso": [("sso_session", "SSO session", "a [sso-session] name in ~/.aws/config"),
                ("sso_account_id", "Account ID", "123456789012"), ("sso_role_name", "Role name", "AdministratorAccess"),
                ("sso_start_url", "Start URL (without a session)", "optional"),
                ("sso_region", "SSO region (without a session)", "optional")],
        "process": [("credential_process", "Command", "/usr/local/bin/get-creds --profile x")],
    ]

    public static let regions = ["us-east-1", "us-east-2", "us-west-1", "us-west-2", "ca-central-1", "sa-east-1", "eu-west-1", "eu-west-2",
                                 "eu-west-3", "eu-central-1", "eu-central-2", "eu-north-1", "eu-south-1", "ap-south-1", "ap-southeast-1",
                                 "ap-southeast-2", "ap-northeast-1", "ap-northeast-2", "ap-east-1", "me-central-1", "af-south-1"]
    public static let outputs = ["", "json", "yaml", "yaml-stream", "text", "table"]

    public static func shortKey(_ id: String) -> String { id.count > 8 ? id.prefix(4) + "…" + id.suffix(4) : id }

    /// `AKIA…0001 · 123456789012 · us-east-1`
    public static func detail(_ p: AwsProfile) -> String {
        let kind = p.kind == "keys" ? shortKey(p.accessKeyId) + (p.hasSessionToken ? " (temporary)" : "") : kindLabel[p.kind] ?? p.kind
        return ([kind, p.account, p.region].filter { !$0.isEmpty }).joined(separator: " · ")
    }

    public static func tooltip(_ p: AwsProfile, credentialsFile: String, configFile: String, pretty: (String) -> String) -> String {
        var lines = ["Kind: \(kindLabel[p.kind] ?? p.kind)"]
        if !p.accessKeyId.isEmpty { lines.append("Access key: \(p.accessKeyId)") }
        if p.inCredentials { lines.append("In \(pretty(credentialsFile))") }
        if p.inConfig { lines.append("In \(pretty(configFile))") }
        lines += p.credentialsOther.map { "\($0.key) = \($0.value) (in the credentials file)" }
        if !p.sourceFor.isEmpty { lines.append("Source profile for \(p.sourceFor.joined(separator: ", "))") }
        return lines.joined(separator: "\n")
    }

    public static func matches(_ p: AwsProfile, _ query: String) -> Bool {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        return q.isEmpty || "\(p.name) \(p.accessKeyId) \(p.region) \(p.account)".lowercased().contains(q)
    }

    /// `123456789012:user/ci-bot` from an sts/iam ARN.
    public static func shortArn(_ arn: String) -> String {
        arn.replacingOccurrences(of: #"^arn:aws[\w-]*:(iam|sts)::"#, with: "", options: .regularExpression)
    }

    static func date(_ iso: String) -> Date? {
        let f = ISO8601DateFormatter()
        if let d = f.date(from: iso) { return d }
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f.date(from: iso)
    }

    public static func daysSince(_ iso: String, now: Date = Date()) -> Int {
        guard let d = date(iso) else { return 0 }
        return Int((now.timeIntervalSince(d) / 86400).rounded(.down))
    }

    public static func ago(_ iso: String, now: Date = Date()) -> String {
        let d = daysSince(iso, now: now)
        return d <= 0 ? "today" : d == 1 ? "yesterday" : "\(d) days ago"
    }

    /// An access key's line under its profile, after Check Access Keys.
    public static func keyLine(_ k: Go.awsprofiles.AccessKey, now: Date = Date()) -> String {
        let used = k.lastUsed.isEmpty ? "never used" : "used \(ago(k.lastUsed, now: now))" + (k.lastUsedService.isEmpty ? "" : " (\(k.lastUsedService))")
        return "\(shortKey(k.id))\(k.current ? " (this one)" : "") · \(k.status.lowercased()) · \(daysSince(k.created, now: now)) days old · \(used)"
    }

    /// The profile the focused shell uses ("" for exported keys).
    public static func inUse(_ pane: Pane?) -> String {
        let p = pane?.infra?.awsProfile ?? ""
        return p == "env credentials" ? "" : p
    }

    public static func useCommand(_ name: String) -> String { "export AWS_PROFILE=" + Shell.quote(name) }

    /// The editor's form, as typed.
    public struct Draft: Equatable, Sendable {
        public var name = "", kind = "keys"
        public var accessKeyId = "", secretAccessKey = "", sessionToken = ""
        public var removeSessionToken = false
        public var region = "", output = ""
        public var fields: [String: String] = [:]
        public var more = ""
        public init() {}
    }

    public static func draft(of p: AwsProfile) -> Draft {
        var d = Draft()
        d.name = p.name
        d.kind = p.kind
        d.accessKeyId = p.accessKeyId
        func setting(_ k: String) -> String { p.settings.first { $0.key.lowercased() == k }?.value ?? "" }
        d.region = setting("region")
        d.output = setting("output")
        let keys = (kindFields[p.kind] ?? []).map(\.key)
        for k in keys { d.fields[k] = setting(k) }
        d.more = p.settings.filter { !(["region", "output"] + keys).contains($0.key.lowercased()) }
            .map { "\($0.key) = \($0.value)" }.joined(separator: "\n")
        return d
    }

    public struct BadLine: LocalizedError {
        public let line: String
        public var errorDescription: String? { "“\(line)” should look like “key = value”" }
    }

    /// What Save and PlanSave take. Throws on a "more settings" line that isn't key = value.
    public static func goDraft(_ d: Draft, original: String, versions: [String: String]) throws -> Go.awsprofiles.Draft {
        var settings: [Go.awsprofiles.Setting] = []
        let trim = { (s: String) in s.trimmingCharacters(in: .whitespaces) }
        if !trim(d.region).isEmpty { settings.append(.init(key: "region", value: trim(d.region))) }
        if !d.output.isEmpty { settings.append(.init(key: "output", value: d.output)) }
        for f in kindFields[d.kind] ?? [] {
            let v = trim(d.fields[f.key] ?? "")
            if !v.isEmpty { settings.append(.init(key: f.key, value: v)) }
        }
        let re = try! NSRegularExpression(pattern: #"^\s*([^=\s]+)\s*=\s*(.*)$"#)
        for line in d.more.split(separator: "\n", omittingEmptySubsequences: false).map(String.init) where !trim(line).isEmpty {
            let ns = line as NSString
            guard let m = re.firstMatch(in: line, range: NSRange(location: 0, length: ns.length)) else { throw BadLine(line: trim(line)) }
            let value = trim(ns.substring(with: m.range(at: 2)))
            if value.isEmpty { throw BadLine(line: trim(line)) }
            settings.append(.init(key: ns.substring(with: m.range(at: 1)), value: value))
        }
        let keys = d.kind == "keys"
        return .init(original: original, name: trim(d.name), versions: versions, useKeys: keys,
                     accessKeyId: keys ? trim(d.accessKeyId) : "", secretAccessKey: keys ? trim(d.secretAccessKey) : "",
                     sessionToken: keys ? trim(d.sessionToken) : "", removeSessionToken: keys && d.removeSessionToken, settings: settings)
    }
}

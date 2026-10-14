// Finds secrets in text, for Hide Secrets (screen sharing): access keys,
// tokens, JWTs, passwords in URLs and in key=value settings, private keys.
// Best effort, and on the side of hiding too much: hiding a harmless value
// costs little while sharing a screen; showing a key costs a lot.
//
// A port of frontend/src/lib/secrets.ts. Both are tested against
// testdata/secrets/cases.json so they find the same things.
import Foundation

public struct SecretSpan: Equatable, Sendable {
    /// UTF-16 offsets into the text, as NSString and JavaScript count.
    public let start: Int
    public let end: Int
    /// What it looks like, e.g. "AWS access key".
    public let kind: String

    public init(start: Int, end: Int, kind: String) {
        self.start = start
        self.end = end
        self.kind = kind
    }
}

public enum Secrets {
    /// What copied text shows instead of a secret.
    public static let redacted = "[REDACTED]"

    private struct Rule: @unchecked Sendable {
        let kind: String
        let re: NSRegularExpression
        /// Which group is the secret (0: all of it).
        let group: Int
        /// Extra check on the secret found.
        let accept: (@Sendable (String) -> Bool)?

        init(_ kind: String, _ pattern: String, _ options: NSRegularExpression.Options = [], group: Int = 0,
             accept: (@Sendable (String) -> Bool)? = nil) {
            self.kind = kind
            re = try! NSRegularExpression(pattern: jsPattern(pattern), options: options)
            self.group = group
            self.accept = accept
        }
    }

    private static func has(_ v: String, _ pattern: String) -> Bool { v.range(of: jsPattern(pattern), options: .regularExpression) != nil }
    /// Upper case, lower case and digits mixed: a key or a hash, not a word.
    private static func random(_ v: String) -> Bool { has(v, "[A-Z]") && has(v, "[a-z]") && has(v, "[0-9]") }
    /// Encoded data rather than a name: random-looking, or mixed case with base64's = padding.
    private static func base64ish(_ v: String) -> Bool {
        random(v) || (v.hasSuffix("=") && has(v, "[A-Z]") && has(v, "[a-z]"))
    }

    private static let rules: [Rule] = [
        Rule("private key", #"-----BEGIN [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----[\s\S]*?(?:-----END [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----|$(?![\s\S]))"#),
        Rule("AWS access key", #"\b(?:AKIA|ASIA|ABIA|ACCA)[A-Z0-9]{16}\b"#),
        Rule("AWS secret key", #"\b(?:aws_?secret_?access_?key|secret_?access_?key)["']?\s*[:=]\s*["']?([A-Za-z0-9/+=]{40})(?![A-Za-z0-9/+=])"#,
             .caseInsensitive, group: 1),
        Rule("AWS session token", #"\b(?:aws_?session_?token|session_?token)["']?\s*[:=]\s*["']?([A-Za-z0-9/+=]{80,})"#,
             .caseInsensitive, group: 1),
        Rule("JWT", #"\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*"#),
        Rule("GitHub token", #"\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,})\b"#),
        Rule("GitLab token", #"\bglpat-[A-Za-z0-9_-]{20,}"#),
        Rule("Slack token", #"\bxox[abposr]-[A-Za-z0-9-]{10,}"#),
        Rule("Slack webhook", #"https://hooks\.slack\.com/services/[A-Za-z0-9/]+"#),
        Rule("Stripe key", #"\b[sr]k_(?:live|test)_[A-Za-z0-9]{20,}"#),
        Rule("Google API key", #"\bAIza[0-9A-Za-z_-]{35}"#),
        Rule("API key", #"\bsk-(?:ant-|proj-)?[A-Za-z0-9_-]{20,}"#),
        Rule("npm token", #"\bnpm_[A-Za-z0-9]{36}\b"#),
        // https://user:password@host
        Rule("password in URL", #"\b[a-z][a-z0-9+.-]*://[^\s:/@]+:([^\s@/]+)@"#, .caseInsensitive, group: 1),
        Rule("bearer token", #"\b(?:bearer|token)\s+([A-Za-z0-9._~+/-]{20,}=*)"#, .caseInsensitive, group: 1),
        // password=…, "client_secret": "…", API_KEY: …, export GITHUB_TOKEN=…
        Rule("secret setting",
             #"\b[\w.-]*(?:passw(?:or)?d|pwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credentials?)["']?\s*[:=]\s*(["']?)([^\s"'&,;]{6,})\1"#,
             .caseInsensitive, group: 2,
             accept: { v in
                 !has(v, #"^(?i:true|false|null|none|nil|undefined|\*+|<[^>]*>|\$\{?\w+\}?)$"#) && !has(v, "^[0-9]+$")
             }),
        // Base64 values in YAML, as in a secret's `data:` (kubectl get secret -o yaml).
        Rule("encoded value", #"^\s+[\w.-]+:\s+([A-Za-z0-9+/]{12,}={0,2})\s*$"#, .anchorsMatchLines, group: 1,
             accept: base64ish),
    ]

    /// A JavaScript pattern for ICU: \\w and \\b are ASCII-only in JavaScript
    /// but Unicode-aware in ICU, so they're spelled out.
    static func jsPattern(_ p: String) -> String {
        let word = "A-Za-z0-9_"
        var out = ""
        var inClass = false
        var chars = Array(p)[...]
        while let c = chars.popFirst() {
            if c == "\\", let next = chars.popFirst() {
                switch next {
                case "w": out += inClass ? word : "[\(word)]"
                case "b" where !inClass: out += "(?:(?<![\(word)])(?=[\(word)])|(?<=[\(word)])(?![\(word)]))"
                default: out += "\\\(next)"
                }
                continue
            }
            if c == "[" { inClass = true } else if c == "]" { inClass = false }
            out.append(c)
        }
        return out
    }

    /// The secrets in a text, in order and never overlapping.
    public static func find(_ text: String) -> [SecretSpan] {
        if text.isEmpty { return [] }
        let ns = text as NSString
        var out: [SecretSpan] = []
        for rule in rules {
            for m in rule.re.matches(in: text, range: NSRange(location: 0, length: ns.length)) where m.range.length > 0 {
                let r = m.range(at: rule.group)
                guard r.location != NSNotFound, r.length > 0 else { continue }
                if let accept = rule.accept, !accept(ns.substring(with: r)) { continue }
                let start = r.location, end = r.location + r.length
                if out.contains(where: { start < $0.end && end > $0.start }) { continue }
                out.append(SecretSpan(start: start, end: end, kind: rule.kind))
            }
        }
        return out.sorted { $0.start < $1.start }
    }

    /// The text with its secrets replaced, for copying while secrets are hidden.
    public static func redact(_ text: String) -> String {
        let spans = find(text)
        if spans.isEmpty { return text }
        let ns = text as NSString
        var out = ""
        var at = 0
        for s in spans {
            out += ns.substring(with: NSRange(location: at, length: s.start - at)) + redacted
            at = s.end
        }
        return out + ns.substring(from: at)
    }
}

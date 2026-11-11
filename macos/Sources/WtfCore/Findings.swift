// The Findings tab's grouping: by severity, then rule, filtered by words.
// Like internal/uicore/findings.go.
import Foundation

public typealias Finding = Go.findings.Finding

public enum Findings {
    public static let order = ["critical", "high", "error", "medium", "warning", "low", "note", "none"]
    public static let labels = ["critical": "Critical", "high": "High", "error": "Error", "medium": "Medium",
                                "warning": "Warning", "low": "Low", "note": "Note", "none": "None"]

    public enum Tone { case bad, warn, dim }
    public static func tone(_ severity: String) -> Tone {
        ["critical", "high", "error"].contains(severity) ? .bad : ["medium", "warning"].contains(severity) ? .warn : .dim
    }

    public struct Rule: Equatable, Sendable {
        public var key: String, title: String, ruleId: String, tool: String
        public var items: [Finding]
    }

    public struct Group: Equatable, Sendable {
        public var severity: String
        public var rules: [Rule]
        public var count: Int { rules.reduce(0) { $0 + $1.items.count } }
    }

    public static func matches(_ f: Finding, _ filter: String) -> Bool {
        let words = filter.lowercased().split(whereSeparator: \.isWhitespace)
        let hay = "\(f.tool) \(f.ruleId) \(f.rule) \(f.message) \(f.path)".lowercased()
        return words.allSatisfy { hay.contains($0) }
    }

    public static func group(_ findings: [Finding], filter: String) -> [Group] {
        var bySeverity: [String: [String: Rule]] = [:]
        for f in findings where matches(f, filter) {
            let key = f.tool + "\n" + f.ruleId
            bySeverity[f.severity, default: [:]][key, default: Rule(key: key, title: f.rule.isEmpty ? f.ruleId : f.rule,
                                                                   ruleId: f.ruleId, tool: f.tool, items: [])].items.append(f)
        }
        func rank(_ s: String) -> Int { order.firstIndex(of: s) ?? order.count }
        return bySeverity.map { sev, rules in
            Group(severity: sev, rules: rules.values.map { r in
                var r = r
                r.items.sort { ($0.path, $0.line) < ($1.path, $1.line) }
                return r
            }.sorted { $0.items.count != $1.items.count ? $0.items.count > $1.items.count : $0.title < $1.title })
        }.sorted { (rank($0.severity), $0.severity) < (rank($1.severity), $1.severity) }
    }

    /// `src/a.go:12`, relative to the root when under it.
    public static func `where`(_ f: Finding, root: String) -> String {
        guard !f.path.isEmpty else { return "" }
        let p = f.path.hasPrefix(root + "/") ? String(f.path.dropFirst(root.count + 1)) : f.path
        return f.line > 0 ? "\(p):\(f.line)" : p
    }

    /// Whether a folder needs another read: it's outside what was loaded.
    public static func stale(_ folder: String, root: String?, loaded: String?) -> Bool {
        guard let root, !root.isEmpty else { return folder != loaded }
        return !(folder == root || folder.hasPrefix(root + "/"))
    }

    /// A finished command that was one of the scanners: results may have changed.
    public static func isScan(_ command: String) -> Bool {
        command.range(of: #"(^|[\s(;&|])(trivy|semgrep|checkov|gitleaks|grype)\s"#, options: .regularExpression) != nil
    }

    /// "just now", "5 min ago", "3 h ago", "2 d ago".
    public static func relative(_ iso: String, now: Date = Date()) -> String {
        guard let d = ISO8601DateFormatter().date(from: iso) else { return "" }
        let s = now.timeIntervalSince(d)
        if s < 45 { return "just now" }
        if s < 3600 { return "\(max(1, Int((s / 60).rounded()))) min ago" }
        if s < 86400 { return "\(Int((s / 3600).rounded())) h ago" }
        return "\(Int((s / 86400).rounded())) d ago"
    }
}

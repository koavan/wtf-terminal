// The side pane's state as each window saves it in its session ("sidebar"),
// validated field by field. Fields this build doesn't use are kept, so
// saving doesn't drop what older versions (the Wails build) saved. Like
// internal/uicore/sidebar.go.
import Foundation

public enum SidebarTab: String, CaseIterable, Sendable {
    case files, ssh, aws, kube, snippets, workspaces, network, processes, history, pins, findings, share, generate, investigate, agents, secrets
}

public struct SidebarState: Equatable, Sendable {
    public static let minWidth = 220.0, maxWidth = 520.0, defaultWidth = 300.0

    public var open = false
    public var width = defaultWidth
    /// The Files tab's locked root; nil follows the focused pane.
    public var pinned: String?
    public var tab: SidebarTab = .files
    public var sshSection = "hosts"
    public var kubeView = "cluster"
    public var generateSection = "key"
    public var historyScope = "pane"
    /// The Network and Processes tabs' views (shared by the app, saved per window).
    public var networkScope = "terminals"
    public var processScope = "terminals"
    public var processSort = "cpu"
    /// The Cloud Secrets tab's store: secretsmanager, ssm or vault.
    public var secretsSource = "secretsmanager"
    /// Everything else in the saved object, as it was.
    var rest: [String: JSONValue] = [:]

    public init() {}

    public static func clampWidth(_ w: Double) -> Double { min(maxWidth, max(minWidth, w)).rounded() }

    public init(json: JSONValue?) {
        guard case .object(let o)? = json else { return }
        rest = o
        if case .bool(let b)? = o["open"] { open = b }
        if case .number(let w)? = o["width"], w.isFinite { width = Self.clampWidth(w) }
        if case .string(let p)? = o["pinned"], p.hasPrefix("/") { pinned = p }
        if case .string(let t)? = o["tab"], let tab = SidebarTab(rawValue: t) { self.tab = tab }
        if case .string(let s)? = o["sshSection"], ["hosts", "keys", "git", "tunnels"].contains(s) { sshSection = s }
        if case .string(let k)? = o["kubeView"], ["cluster", "contexts"].contains(k) { kubeView = k }
        if case .string(let n)? = o["networkScope"], ["terminals", "all"].contains(n) { networkScope = n }
        if case .string(let n)? = o["processScope"], ["terminals", "all"].contains(n) { processScope = n }
        if case .string(let n)? = o["processSort"], ["cpu", "memory", "name"].contains(n) { processSort = n }
        if case .string(let h)? = o["historyScope"], ["pane", "all", "saved"].contains(h) { historyScope = h }
        if case .string(let g)? = o["generateSection"], ["key", "password"].contains(g) { generateSection = g }
        if case .string(let c)? = o["secretsSource"], ["secretsmanager", "ssm", "vault"].contains(c) { secretsSource = c }
    }

    public var json: JSONValue {
        var o = rest
        o["open"] = .bool(open)
        o["width"] = .number(width)
        o["pinned"] = pinned.map { .string($0) } ?? .null
        o["tab"] = .string(tab.rawValue)
        o["sshSection"] = .string(sshSection)
        o["kubeView"] = .string(kubeView)
        o["generateSection"] = .string(generateSection)
        o["historyScope"] = .string(historyScope)
        o["networkScope"] = .string(networkScope)
        o["processScope"] = .string(processScope)
        o["processSort"] = .string(processSort)
        o["secretsSource"] = .string(secretsSource)
        return .object(o)
    }
}

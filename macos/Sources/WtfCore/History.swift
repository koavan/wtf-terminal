// What the History, Pinned and Share & Record tabs keep, in memory per
// window: commands run, pinned output, and how output is exported. Like
// internal/uicore/history.go and export.go.
import Foundation

/// The commands each pane ran, from shell integration.
public struct History: Sendable {
    public struct Entry: Equatable, Sendable {
        /// The command block's id in its terminal.
        public var id: Int
        public var paneId: Int
        public var command: String
        public var cwd: String
        public var startedAt: Date
        public var finishedAt: Date?
        public var durationMs: Double?
        public var exitCode: Int?
        public var running: Bool { finishedAt == nil }
    }

    public static let maxEntries = 1000
    private var panes: [Int: [Entry]] = [:]

    public init() {}

    public mutating func started(pane: Int, id: Int, command: String, cwd: String, at: Date = Date()) {
        guard !command.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
        var list = panes[pane] ?? []
        list.append(Entry(id: id, paneId: pane, command: command, cwd: cwd, startedAt: at))
        if list.count > Self.maxEntries { list.removeFirst(list.count - Self.maxEntries) }
        panes[pane] = list
    }

    /// Finishes the pane's last command, if it's still running; strays are ignored.
    public mutating func finished(pane: Int, exitCode: Int, durationMs: Double, at: Date = Date()) {
        guard var list = panes[pane], let last = list.indices.last, list[last].running else { return }
        list[last].finishedAt = at
        list[last].durationMs = durationMs
        list[last].exitCode = exitCode
        panes[pane] = list
    }

    public func of(_ pane: Int) -> [Entry] { panes[pane] ?? [] }
    public mutating func clear(_ pane: Int) { panes[pane] = nil }
    public func merged(_ ids: [Int]) -> [Entry] { ids.flatMap(of).sorted { $0.startedAt < $1.startedAt } }

    public static func matches(_ e: Entry, _ query: String) -> Bool {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        return q.isEmpty || "\(e.command) \(e.cwd)".lowercased().contains(q)
    }
}

/// Output kept in view: a command's (following it while it runs) or a selection.
public struct Pins: Sendable {
    public struct Pin: Equatable, Sendable {
        public var id: Int
        public var title: String
        public var text: String
        public var truncated = false
        public var paneId: Int
        /// nil for a selection.
        public var blockId: Int?
        public var cwd: String
        public var pinnedAt: Date
        public var running = false
        public var exitCode: Int?
        public var durationMs: Double?
        public var collapsed = false

        public init(title: String, text: String, paneId: Int, blockId: Int?, cwd: String, running: Bool = false,
                    exitCode: Int? = nil, durationMs: Double? = nil, pinnedAt: Date = Date()) {
            id = 0
            self.title = title
            self.text = text
            self.paneId = paneId
            self.blockId = blockId
            self.cwd = cwd
            self.running = running
            self.exitCode = exitCode
            self.durationMs = durationMs
            self.pinnedAt = pinnedAt
        }
    }

    public static let maxPins = 30
    public static let maxChars = 200_000
    public private(set) var list: [Pin] = []
    private var nextId = 1

    public init() {}

    /// The last 200,000 characters, and whether some were cut.
    static func clip(_ s: String) -> (String, Bool) {
        s.count > maxChars ? (String(s.suffix(maxChars)), true) : (s, false)
    }

    /// Pins a draft; pinning the same command again updates its pin in place.
    @discardableResult
    public mutating func add(_ draft: Pin) -> Pin {
        var p = draft
        (p.text, p.truncated) = Self.clip(draft.text)
        p.collapsed = false
        if let b = draft.blockId, let i = list.firstIndex(where: { $0.paneId == draft.paneId && $0.blockId == b }) {
            p.id = list[i].id
            list[i] = p
            return p
        }
        p.id = nextId
        nextId += 1
        list.insert(p, at: 0)
        if list.count > Self.maxPins { list.removeLast(list.count - Self.maxPins) }
        return p
    }

    public mutating func remove(_ id: Int) { list.removeAll { $0.id == id } }
    public mutating func clear() { list = [] }
    public mutating func toggle(_ id: Int) { if let i = list.firstIndex(where: { $0.id == id }) { list[i].collapsed.toggle() } }
    public var live: [Pin] { list.filter(\.running) }

    /// New output for a running pin; `done` finishes it for good.
    public mutating func update(pane: Int, block: Int, text: String, done: (exitCode: Int, durationMs: Double)? = nil) {
        guard let i = list.firstIndex(where: { $0.paneId == pane && $0.blockId == block && $0.running }) else { return }
        (list[i].text, list[i].truncated) = Self.clip(text)
        if let done {
            list[i].running = false
            list[i].exitCode = done.exitCode
            list[i].durationMs = done.durationMs
        }
    }

    public mutating func paneGone(_ pane: Int) {
        for i in list.indices where list[i].paneId == pane { list[i].running = false }
    }
}

public enum Export {
    /// `wtf-kubectl-get-pods-n-shop-2026-10-09-2305`
    public static func name(_ title: String, at: Date, calendar: Calendar = .current) -> String {
        var slug = title.lowercased().replacingOccurrences(of: "[^a-z0-9]+", with: "-", options: .regularExpression)
        slug = slug.trimmingCharacters(in: CharacterSet(charactersIn: "-"))
        slug = String(slug.prefix(40))
        if slug.isEmpty { slug = "terminal" }
        let c = calendar.dateComponents([.year, .month, .day, .hour, .minute], from: at)
        return String(format: "wtf-%@-%04d-%02d-%02d-%02d%02d", slug, c.year!, c.month!, c.day!, c.hour!, c.minute!)
    }

    /// `M:SS`, or `H:MM:SS` from an hour.
    public static func elapsed(_ seconds: Int) -> String {
        let s = max(0, seconds)
        return s >= 3600 ? String(format: "%d:%02d:%02d", s / 3600, s / 60 % 60, s % 60) : String(format: "%d:%02d", s / 60, s % 60)
    }

    public struct Block: Equatable, Sendable {
        public var text: String
        /// nil once it has left the scrollback.
        public var output: String?
        public var startedAt: Date?
        public var exitCode: Int?
        public var durationMs: Double?
        public init(text: String, output: String?, startedAt: Date? = nil, exitCode: Int? = nil, durationMs: Double? = nil) {
            self.text = text
            self.output = output
            self.startedAt = startedAt
            self.exitCode = exitCode
            self.durationMs = durationMs
        }
    }

    static func longestTicks(_ s: String) -> Int {
        var best = 0, run = 0
        for ch in s {
            if ch == "`" { run += 1; best = max(best, run) } else { run = 0 }
        }
        return best
    }

    public static func fence(_ text: String) -> String { String(repeating: "`", count: max(3, longestTicks(text) + 1)) }

    public static func code(_ text: String) -> String {
        let ticks = String(repeating: "`", count: longestTicks(text) + 1)
        let pad = text.hasPrefix("`") || text.hasSuffix("`") ? " " : ""
        return ticks + pad + text + pad + ticks
    }

    /// The Markdown transcript: each command with how it went and its output.
    public static func markdown(_ blocks: [Block], title: String, cwd: String, exportedAt: Date,
                                locale: Locale = .current, timeZone: TimeZone = .current) -> String {
        let clock = DateFormatter()
        clock.locale = locale
        clock.timeZone = timeZone
        clock.dateFormat = "HH:mm:ss"
        let day = DateFormatter()
        day.locale = locale
        day.timeZone = timeZone
        day.setLocalizedDateFormatFromTemplate("MMM d, yyyy")
        let n = blocks.count
        var lines = ["# \(title)", "",
                     "Terminal transcript from wtf, saved \(day.string(from: exportedAt)) \(clock.string(from: exportedAt))"
                         + (cwd.isEmpty ? "" : " · \(cwd)") + " · \(n) \(n == 1 ? "command" : "commands")"]
        for (i, b) in blocks.enumerated() {
            lines += ["", "## \(i + 1). \(code(b.text.isEmpty ? "(no command text)" : b.text))", ""]
            var facts = [b.exitCode == nil ? "still running" : b.exitCode == 0 ? "✓ succeeded" : "✗ exit \(b.exitCode!)"]
            if let s = b.startedAt { facts.append("started \(clock.string(from: s))") }
            if let d = b.durationMs { facts.append("took \(Format.duration(ms: d))") }
            lines.append(facts.joined(separator: " · "))
            if let out = b.output {
                if out.isEmpty {
                    lines += ["", "_No output._"]
                } else {
                    let f = fence(out)
                    lines += ["", f + "text", out, f]
                }
            } else {
                lines += ["", "_Output no longer in the scrollback._"]
            }
        }
        return lines.joined(separator: "\n") + "\n"
    }
}

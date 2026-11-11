// Tracks the prompts and commands a shell reports through OSC 133 (see
// internal/terminal/shellintegration) and turns each into a command block:
// its mark in the margin, its duration, and the command's text and output
// for copying, selecting and running again.
// Like internal/uicore/prompts.go.
import Foundation
import SwiftTerm

/// A line of the scrollback that stays put as output scrolls and old lines
/// are trimmed: its absolute row (counted from the first line the terminal
/// ever had), and the line itself, to find it again after a resize reflows
/// the text.
public final class LineMarker {
    public fileprivate(set) var row: Int
    fileprivate weak var line: BufferLine?

    init(row: Int, line: BufferLine?) {
        self.row = row
        self.line = line
    }

    /// Whether the line is still in the buffer shown now.
    public func isValid(in t: Terminal) -> Bool {
        guard let line, row >= t.buffer.totalLinesTrimmed else { return false }
        return t.getScrollInvariantLine(row: row) === line
    }
}

public enum Rows {
    /// The absolute row of the last line in the buffer.
    public static func last(_ t: Terminal) -> Int {
        // The line count isn't public: find where lines stop.
        var lo = t.buffer.totalLinesTrimmed, hi = lo + 1
        while t.getScrollInvariantLine(row: hi) != nil { hi = lo + (hi - lo) * 2 }
        while hi - lo > 1 {
            let mid = (lo + hi) / 2
            if t.getScrollInvariantLine(row: mid) != nil { lo = mid } else { hi = mid }
        }
        return lo
    }

    /// The absolute row of the cursor: the screen is the buffer's last rows.
    public static func cursor(_ t: Terminal) -> Int {
        last(t) - t.rows + 1 + t.getCursorLocation().y
    }

    /// The absolute row at the top of the view.
    public static func top(_ t: Terminal) -> Int {
        t.buffer.totalLinesTrimmed + t.getTopVisibleRow()
    }

    /// A marker on the cursor's line.
    public static func marker(_ t: Terminal) -> LineMarker {
        let row = cursor(t)
        return LineMarker(row: row, line: t.getScrollInvariantLine(row: row))
    }

    /// A line's text, without trailing blanks.
    public static func text(_ t: Terminal, _ row: Int) -> String {
        t.getScrollInvariantLine(row: row)?.translateToString(trimRight: true) ?? ""
    }
}

public final class PromptTracker {
    /// What's known about one command, for its menu.
    public struct Block: Equatable {
        public let id: Int
        public let text: String
        /// Still running: no exit code yet.
        public var running: Bool { exitCode == nil }
        public let exitCode: Int?
        public let durationMs: Double?
        public let finishedAt: Date?
        public let startedAt: Date?
        /// Its output is still in the scrollback.
        public let hasOutput: Bool
    }

    final class Record {
        let id: Int
        let prompt: LineMarker
        /// Where the command's output starts and ends.
        var output: LineMarker?
        var end: LineMarker?
        var startedAt: Date?
        var text = ""
        var exitCode: Int?
        var durationMs: Double?
        var finishedAt: Date?

        init(id: Int, prompt: LineMarker) {
            self.id = id
            self.prompt = prompt
        }
    }

    /// Commands this quick get no duration label.
    public static let durationLabelMs: Double = 1000
    static let maxRecords = 1000

    public var onPrompt: (() -> Void)?
    public var onCommandStart: ((_ text: String, _ id: Int) -> Void)?
    public var onCommandEnd: ((_ exitCode: Int, _ durationMs: Double) -> Void)?
    /// The variables that pick the shell's cloud and cluster context, at each prompt.
    public var onEnv: ((String) -> Void)?
    /// Marks changed: draw them again.
    public var onChange: (() -> Void)?

    private let term: Terminal
    private let now: () -> Date
    private(set) var records: [Record] = []
    private var current: Record?
    private var pendingText = ""
    private var nextID = 1

    public init(terminal: Terminal, now: @escaping () -> Date = Date.init) {
        term = terminal
        self.now = now
        terminal.registerOscHandler(code: 133) { [weak self] data in self?.on133(String(decoding: data, as: UTF8.self)) }
        terminal.registerOscHandler(code: 633) { [weak self] data in self?.on633(String(decoding: data, as: UTF8.self)) }
    }

    /// Undoes the escaping done by the shell scripts (\\ and \xNN).
    public static func unescape(_ s: String) -> String {
        var out = [UInt8]()
        let b = Array(s.utf8)
        var i = 0
        while i < b.count {
            if b[i] == UInt8(ascii: "\\"), i + 1 < b.count {
                if b[i + 1] == UInt8(ascii: "\\") {
                    out.append(b[i + 1])
                    i += 2
                    continue
                }
                if b[i + 1] == UInt8(ascii: "x"), i + 3 < b.count,
                   let v = UInt8(String(decoding: b[(i + 2)...(i + 3)], as: UTF8.self), radix: 16) {
                    out.append(v)
                    i += 4
                    continue
                }
            }
            out.append(b[i])
            i += 1
        }
        return String(decoding: out, as: UTF8.self)
    }

    private func on633(_ data: String) {
        if data.hasPrefix("E;") {
            pendingText = Self.unescape(String(data.dropFirst(2)))
        } else if data.hasPrefix("P;WtfEnv=") {
            onEnv?(Self.unescape(String(data.dropFirst(9))))
        }
    }

    private func on133(_ data: String) {
        let parts = data.split(separator: ";", maxSplits: 1, omittingEmptySubsequences: false)
        switch parts.first {
        case "A":
            let rec = Record(id: nextID, prompt: Rows.marker(term))
            nextID += 1
            // Prompts only move down: one at or above an earlier prompt means
            // the screen was cleared (clear, Ctrl+L), which erases lines in
            // place, so the old marks would linger on blank rows.
            records.removeAll { $0.prompt.row >= rec.prompt.row }
            current = rec
            records.append(rec)
            if records.count > Self.maxRecords { records.removeFirst(records.count - Self.maxRecords) }
            onPrompt?()
            onChange?()
        case "C":
            if let rec = current, rec.output == nil {
                rec.output = Rows.marker(term)
                rec.startedAt = now()
                rec.text = pendingText
            }
            onCommandStart?(pendingText, current?.id ?? 0)
            pendingText = ""
        case "D":
            guard let rec = current, let started = rec.startedAt, rec.end == nil else { break }
            rec.end = Rows.marker(term)
            let code = parts.count > 1 ? Int(parts[1]) ?? 0 : 0
            let finished = now()
            rec.exitCode = code
            rec.durationMs = finished.timeIntervalSince(started) * 1000
            rec.finishedAt = finished
            onCommandEnd?(code, rec.durationMs!)
            onChange?()
        default:
            break
        }
    }

    /// Finds markers again after a resize reflowed the text; drops those
    /// whose line is gone.
    public func relocate() {
        var rows: [ObjectIdentifier: Int] = [:]
        let first = term.buffer.totalLinesTrimmed
        let last = Rows.last(term)
        if first <= last {
            for row in first...last {
                if let line = term.getScrollInvariantLine(row: row) { rows[ObjectIdentifier(line)] = row }
            }
        }
        func fix(_ m: LineMarker?) {
            guard let m else { return }
            if let line = m.line, let row = rows[ObjectIdentifier(line)] { m.row = row } else { m.line = nil }
        }
        for r in records {
            fix(r.prompt)
            fix(r.output)
            fix(r.end)
        }
        onChange?()
    }

    // MARK: Queries

    private func record(_ id: Int) -> Record? { records.first { $0.id == id } }

    /// Commands whose prompt is on screen rows [from, to] (absolute), for drawing marks.
    public func blocks(from: Int, to: Int) -> [(row: Int, block: Block)] {
        records.compactMap { r in
            guard r.prompt.row >= from, r.prompt.row <= to, r.prompt.isValid(in: term) else { return nil }
            return (r.prompt.row, block(r))
        }
    }

    public func info(_ id: Int) -> Block? { record(id).map(block) }

    private func block(_ r: Record) -> Block {
        Block(id: r.id, text: r.text, exitCode: r.exitCode, durationMs: r.durationMs, finishedAt: r.finishedAt,
              startedAt: r.startedAt, hasOutput: r.output?.isValid(in: term) ?? false)
    }

    /// The row to scroll to for the previous (-1) or next (1) prompt from the
    /// top of the view; nil when there's none (for next: scroll to the bottom).
    public func jump(_ direction: Int, from top: Int) -> Int? {
        let rows = records.filter { $0.prompt.isValid(in: term) }.map(\.prompt.row)
        return direction < 0 ? rows.last { $0 < top } : rows.first { $0 > top }
    }

    /// The row of a command's prompt, or nil once it has left the scrollback.
    public func promptRow(_ id: Int) -> Int? {
        guard let r = record(id), r.prompt.isValid(in: term) else { return nil }
        return r.prompt.row
    }

    /// The command whose block holds a row: from its prompt to the next prompt.
    public func blockAt(_ row: Int) -> Int? {
        var found: Record?
        for r in records where r.prompt.isValid(in: term) {
            if r.prompt.row <= row { found = r } else { break }
        }
        return found?.output != nil ? found?.id : nil
    }

    /// The command running now, if any.
    public var runningId: Int? {
        guard let r = current, r.output != nil, r.end == nil else { return nil }
        return r.id
    }

    /// Every command that got as far as running, with its output (nil once
    /// it has left the scrollback), for a Markdown transcript.
    public func transcript() -> [Export.Block] {
        records.filter { $0.output != nil }.map { r in
            Export.Block(text: r.text, output: output(r.id), startedAt: r.startedAt, exitCode: r.exitCode, durationMs: r.durationMs)
        }
    }

    /// The last n finished commands, with what's left of their output in
    /// the scrollback, oldest first. A command that printed nothing counts.
    public func recent(_ n: Int) -> [Export.Block] {
        var out: [Export.Block] = []
        for r in records.reversed() where out.count < n {
            guard r.exitCode != nil, !r.text.isEmpty else { continue }
            out.append(Export.Block(text: r.text, output: output(r.id) ?? "", startedAt: r.startedAt, exitCode: r.exitCode, durationMs: r.durationMs))
        }
        return out.reversed()
    }

    /// The ids of the last n finished commands, oldest first.
    public func recentIDs(_ n: Int) -> [Int] {
        var out: [Int] = []
        for r in records.reversed() where out.count < n {
            guard r.exitCode != nil, !r.text.isEmpty else { continue }
            out.append(r.id)
        }
        return out.reversed()
    }

    /// The most recent finished command whose output is still in the scrollback.
    public func lastFinished() -> Int? {
        records.last { $0.end != nil && $0.output?.isValid(in: term) == true }?.id
    }

    /// The rows of a command's output: from its output mark to before its end
    /// mark, or the cursor while it runs. Nil once it has left the scrollback.
    public func outputRows(_ id: Int) -> ClosedRange<Int>? {
        guard let r = record(id), let out = r.output, out.isValid(in: term) else { return nil }
        let last = (r.end?.isValid(in: term) == true ? r.end!.row : Rows.cursor(term) + 1) - 1
        return last >= out.row ? out.row...last : nil
    }

    /// A command's output so far, or nil once it has left the scrollback.
    public func output(_ id: Int) -> String? {
        guard record(id)?.output?.isValid(in: term) == true else { return nil }
        guard let rows = outputRows(id) else { return "" }
        var text = ""
        for row in rows {
            guard let line = term.getScrollInvariantLine(row: row) else { continue }
            text += line.translateToString(trimRight: true)
            // A soft-wrapped row continues on the next one.
            if !(row < rows.upperBound && term.getScrollInvariantLine(row: row + 1)?.isWrapped == true) { text += "\n" }
        }
        while text.hasSuffix("\n") { text.removeLast() }
        return text
    }

    /// The rows of a command's whole block, prompt to end, for highlighting.
    public func blockRows(_ id: Int) -> ClosedRange<Int>? {
        guard let r = record(id), r.prompt.isValid(in: term) else { return nil }
        let last = outputRows(id)?.upperBound ?? r.prompt.row
        return r.prompt.row...max(r.prompt.row, last)
    }
}

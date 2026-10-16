// Find in the terminal: every match in the scrollback, for "N of M", and the
// matches on screen, for highlighting. Columns are cells.
import Foundation
import SwiftTerm

public enum Find {
    public struct Match: Equatable, Sendable {
        public var row: Int, col: Int, width: Int
        public init(row: Int, col: Int, width: Int) { self.row = row; self.col = col; self.width = width }
    }

    /// A row's text and the cell each of its characters starts at, with one
    /// more entry for the cell after the last: wide characters take two
    /// cells and combining marks none, so characters aren't cells.
    public struct Line: Sendable {
        public var row: Int, text: String, cells: [Int]
        public init(row: Int, text: String, cells: [Int]? = nil) {
            self.row = row
            self.text = text
            self.cells = cells ?? Array(0...text.count)
        }
    }

    /// Matches in rows of text, left to right, not overlapping.
    public static func matches(_ rows: [(row: Int, text: String)], _ query: String, caseSensitive: Bool) -> [Match] {
        matches(rows.map { Line(row: $0.row, text: $0.text) }, query, caseSensitive: caseSensitive)
    }

    public static func matches(_ lines: [Line], _ query: String, caseSensitive: Bool) -> [Match] {
        guard !query.isEmpty else { return [] }
        let opts: String.CompareOptions = caseSensitive ? [] : [.caseInsensitive]
        var out: [Match] = []
        for line in lines {
            let text = line.text
            var from = text.startIndex
            while from < text.endIndex, let r = text.range(of: query, options: opts, range: from..<text.endIndex) {
                let a = text.distance(from: text.startIndex, to: r.lowerBound)
                let b = a + text.distance(from: r.lowerBound, to: r.upperBound)
                let start = line.cells[min(a, line.cells.count - 1)]
                let end = line.cells[min(b, line.cells.count - 1)]
                out.append(Match(row: line.row, col: start, width: max(1, end - start)))
                from = r.upperBound
            }
        }
        return out
    }

    /// A buffer line's text and cells, trailing blanks dropped.
    public static func line(_ l: BufferLine, row: Int) -> Line {
        let n = l.getTrimmedLength()
        var text = ""
        var cells: [Int] = []
        var count = 0
        for i in 0..<n {
            let ch = l[i].getCharacter()
            // The empty cell after a wide character belongs to it.
            if ch == "\0", i > 0, l[i - 1].width == 2 { continue }
            text.append(ch)
            // A combining mark joins the character before it.
            if text.count > count {
                cells.append(i)
                count = text.count
            }
        }
        cells.append(n)
        return Line(row: row, text: text, cells: cells)
    }

    /// Every match in a terminal's buffer (scrollback and screen). Rows count
    /// from the first line ever written, so they hold while old lines are
    /// trimmed off the top of a full scrollback: `row - t.buffer.totalLinesTrimmed`
    /// is the buffer row.
    public static func matches(in t: Terminal, _ query: String, caseSensitive: Bool, limit: Int = 50_000) -> [Match] {
        var lines: [Line] = []
        var r = t.buffer.totalLinesTrimmed
        while let l = t.getScrollInvariantLine(row: r), lines.count < limit {
            lines.append(line(l, row: r))
            r += 1
        }
        return matches(lines, query, caseSensitive: caseSensitive)
    }

    /// The matches on screen, rows counted from the top of the view: read
    /// from what's showing now, so they're right however the output moved.
    public static func visibleMatches(in t: Terminal, _ query: String, caseSensitive: Bool) -> [Match] {
        guard !query.isEmpty else { return [] }
        let lines = (0..<t.rows).compactMap { r in t.getLine(row: r).map { line($0, row: r) } }
        return matches(lines, query, caseSensitive: caseSensitive)
    }

    /// "3 of 12", "No results", or "" before searching.
    public static func status(_ matches: [Match], current: Int?) -> String {
        if matches.isEmpty { return "No results" }
        guard let current else { return "\(matches.count) found" }
        return "\(current + 1) of \(matches.count)"
    }
}

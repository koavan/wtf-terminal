// Find in the terminal: every match in the scrollback, for "N of M" and for
// highlighting the ones on screen. Rows are buffer lines; columns are cells.
import Foundation
import SwiftTerm

public enum Find {
    public struct Match: Equatable, Sendable {
        public var row: Int, col: Int, width: Int
    }

    /// Matches in rows of text, left to right, not overlapping.
    public static func matches(_ rows: [(row: Int, text: String)], _ query: String, caseSensitive: Bool) -> [Match] {
        guard !query.isEmpty else { return [] }
        let opts: String.CompareOptions = caseSensitive ? [] : [.caseInsensitive]
        let width = query.count
        var out: [Match] = []
        for (row, text) in rows {
            var from = text.startIndex
            while from < text.endIndex, let r = text.range(of: query, options: opts, range: from..<text.endIndex) {
                out.append(Match(row: row, col: text.distance(from: text.startIndex, to: r.lowerBound), width: width))
                from = r.upperBound
            }
        }
        return out
    }

    /// Every match in a terminal's buffer (scrollback and screen).
    public static func matches(in t: Terminal, _ query: String, caseSensitive: Bool, limit: Int = 50_000) -> [Match] {
        let base = t.buffer.totalLinesTrimmed
        var rows: [(Int, String)] = []
        var r = base
        while let line = t.getScrollInvariantLine(row: r), rows.count < limit {
            rows.append((r - base, line.translateToString(trimRight: true)))
            r += 1
        }
        return matches(rows, query, caseSensitive: caseSensitive)
    }

    /// "3 of 12", "No results", or "" before searching.
    public static func status(_ matches: [Match], current: Int?) -> String {
        if matches.isEmpty { return "No results" }
        guard let current else { return "\(matches.count) found" }
        return "\(current + 1) of \(matches.count)"
    }
}

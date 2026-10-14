// Where the secrets on screen are, for covering them while Hide Secrets is
// on: runs of cells per row. Soft-wrapped lines are read whole, and a
// private key that started above the screen is covered to its end. Like
// lib/secretmask.ts, which does the same for xterm.js.
import Foundation
import SwiftTerm

public enum SecretCovers {
    public struct Cover: Equatable, Sendable {
        /// The row on screen (0 at the top of the view), first column, cells.
        public let row: Int
        public let col: Int
        public let width: Int
    }

    /// How far above the screen to look for the start of a private key.
    static let keyLookback = 200
    static let keyBegin = try! NSRegularExpression(pattern: "-----BEGIN [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----")
    static let keyEnd = try! NSRegularExpression(pattern: "-----END [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----")

    /// A line as text, with the row and column of every UTF-16 unit of it.
    struct Logical {
        var text = ""
        var rows: [Int] = []
        var cols: [Int] = []
        var first = 0
        var last = 0
    }

    /// The soft-wrapped line through an absolute row.
    static func logical(_ t: Terminal, _ row: Int) -> Logical? {
        guard t.getScrollInvariantLine(row: row) != nil else { return nil }
        var first = row
        while row - first < 40, let l = t.getScrollInvariantLine(row: first), l.isWrapped { first -= 1 }
        var out = Logical(first: first, last: first)
        var r = first
        while r - first < 80, let line = t.getScrollInvariantLine(row: r), r == first || line.isWrapped {
            // Cells up to the last one with text: trailing blanks aren't part of it.
            var end = line.count
            while end > 0, blank(t.getCharacter(for: line[end - 1])) { end -= 1 }
            let wrapsOn = t.getScrollInvariantLine(row: r + 1)?.isWrapped == true
            if wrapsOn { end = line.count }
            var col = 0
            while col < end {
                let cell = line[col]
                if cell.width == 0 { col += 1; continue } // the right half of a wide character
                let c = t.getCharacter(for: cell)
                let ch: Character = c == "\u{0}" ? " " : c
                for _ in String(ch).utf16 {
                    out.rows.append(r)
                    out.cols.append(col)
                }
                out.text.append(ch)
                col += max(1, Int(cell.width))
            }
            out.last = r
            r += 1
        }
        return out
    }

    static func blank(_ c: Character) -> Bool { c == " " || c == "\u{0}" }

    static func matches(_ re: NSRegularExpression, _ s: String) -> NSRange? {
        let m = re.firstMatch(in: s, range: NSRange(location: 0, length: (s as NSString).length))
        return m?.range
    }

    /// Whether a row is inside a private key that started above it.
    static func insideKey(_ t: Terminal, _ row: Int) -> Bool {
        var y = row - 1
        while y >= max(t.buffer.totalLinesTrimmed, row - keyLookback) {
            let text = t.getScrollInvariantLine(row: y)?.translateToString(trimRight: true) ?? ""
            if matches(keyEnd, text) != nil { return false }
            if matches(keyBegin, text) != nil { return true }
            y -= 1
        }
        return false
    }

    /// The UTF-16 length without trailing whitespace.
    static func trimmedLength(_ s: String) -> Int {
        var t = Substring(s)
        while let last = t.last, last.isWhitespace { t.removeLast() }
        return (String(t) as NSString).length
    }

    /// The covers for the rows on screen now.
    public static func scan(_ t: Terminal) -> [Cover] {
        let top = Rows.top(t)
        let bottom = top + t.rows - 1
        var covers: [Cover] = []
        func cover(_ line: Logical, _ start: Int, _ end: Int) {
            var i = start
            while i < end {
                let row = line.rows[i]
                var j = i
                while j + 1 < end, line.rows[j + 1] == row { j += 1 }
                if row >= top, row <= bottom {
                    covers.append(Cover(row: row - top, col: line.cols[i], width: line.cols[j] - line.cols[i] + 1))
                }
                i = j + 1
            }
        }

        var inKey = insideKey(t, top)
        var y = top
        while y <= bottom {
            // Most lines have nothing: check the plain text before working out where each character is.
            let quick = t.getScrollInvariantLine(row: y)?.translateToString(trimRight: true) ?? ""
            let wraps = t.getScrollInvariantLine(row: y + 1)?.isWrapped == true
            if !inKey, !wraps, t.getScrollInvariantLine(row: y)?.isWrapped != true,
               matches(keyBegin, quick) == nil, Secrets.find(quick).isEmpty {
                y += 1
                continue
            }
            guard let line = logical(t, y) else { break }
            var text = line.text as NSString
            // Inside a private key: the whole line, up to and including its END.
            if inKey {
                let end = matches(keyEnd, line.text)
                let upto = end.map { $0.location + $0.length } ?? trimmedLength(line.text)
                cover(line, 0, min(upto, line.rows.count))
                if end != nil { inKey = false }
                text = (String(repeating: " ", count: upto) + text.substring(from: upto)) as NSString
            }
            for s in Secrets.find(text as String) where s.end <= line.rows.count { cover(line, s.start, s.end) }
            if let begin = matches(keyBegin, text as String),
               matches(keyEnd, text.substring(from: begin.location)) == nil {
                inKey = true
            }
            y = line.last + 1
        }
        return covers
    }
}

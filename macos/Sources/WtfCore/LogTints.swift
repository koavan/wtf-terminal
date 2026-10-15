// Coloring the tokens on screen: which cells take which color. Only what's
// on screen, only text no program colored, never in full-screen programs.
// Port of LogView.paint in lib/logview.ts.
import Foundation
import SwiftTerm

extension Logs {
    /// Colors for the token kinds, from the theme: hex `#rrggbb`.
    public struct Colors: Equatable, Sendable {
        public var error, warn, info, debug, timestamp, url, ip, k8s: String

        /// palette[1] red, [3] yellow, [2] green, [4] blue, [5] magenta, [6] cyan; faded = 45% toward the background.
        public init(palette: [String], foreground: String, background: String) {
            func norm(_ s: String) -> String {
                guard let c = RGB(hex: s) else { return "#888888" }
                return String(format: "#%02x%02x%02x", Int(c.r.rounded()), Int(c.g.rounded()), Int(c.b.rounded()))
            }
            func p(_ i: Int) -> String { i < palette.count ? norm(palette[i]) : "#888888" }
            let fg = RGB(hex: foreground) ?? RGB(200, 200, 200), bg = RGB(hex: background) ?? RGB(0, 0, 0)
            let muted = String(format: "#%02x%02x%02x", Int((fg.r + (bg.r - fg.r) * 0.45).rounded()),
                               Int((fg.g + (bg.g - fg.g) * 0.45).rounded()), Int((fg.b + (bg.b - fg.b) * 0.45).rounded()))
            error = p(1); warn = p(3); info = p(2); debug = muted; timestamp = muted; url = p(4); ip = p(5); k8s = p(6)
        }

        public func color(_ k: Kind) -> String? {
            switch k {
            case .level(.error): error
            case .level(.warn): warn
            case .level(.info): info
            case .level(.debug): debug
            case .timestamp, .time: timestamp
            case .url: url
            case .ip: ip
            case .k8s: k8s
            case .path, .jwt: nil
            }
        }
    }

    /// A run of cells on screen to color: visible row (0 at the top of the view).
    public struct Tint: Equatable, Sendable, Hashable {
        public var row: Int, col: Int, width: Int
        public var color: String
    }

    public static let maxTints = 600

    /// The tints for what's on screen now. Empty in full-screen programs.
    public static func tints(_ t: Terminal, colors: Colors) -> [Tint] {
        guard !t.isCurrentBufferAlternate else { return [] }
        let top = t.buffer.yDisp, bottom = top + t.rows - 1
        var out: [Tint] = []
        var y = top
        while y <= bottom, out.count < maxTints {
            guard let line = SecretCovers.logical(t, y) else { break }
            defer { y = max(y + 1, line.last + 1) }
            let text = line.text.trimmingCharacters(in: .whitespaces)
            guard !text.isEmpty else { continue }
            for tok in scan(line.text) {
                guard let color = colors.color(tok.kind) else { continue }
                // A program's own colors win.
                guard tok.end <= line.plain.count, line.plain[tok.start..<tok.end].allSatisfy({ $0 }) else { continue }
                // One run per row a wrapped token spans.
                var i = tok.start
                while i < tok.end {
                    let row = line.rows[i]
                    var j = i
                    while j + 1 < tok.end, line.rows[j + 1] == row { j += 1 }
                    if row >= top, row <= bottom {
                        out.append(Tint(row: row - top, col: line.cols[i], width: line.cols[j] - line.cols[i] + 1, color: color))
                    }
                    i = j + 1
                }
                if out.count >= maxTints { break }
            }
        }
        return out
    }

    /// The token at a cell (absolute row), with its line, for ⌘-click and right-click.
    public static func token(_ t: Terminal, row: Int, col: Int) -> Token? {
        guard let line = SecretCovers.logical(t, row) else { return nil }
        return scan(line.text).first { tok in
            (tok.start..<min(tok.end, line.rows.count)).contains { line.rows[$0] == row && line.cols[$0] == col }
        }
    }
}

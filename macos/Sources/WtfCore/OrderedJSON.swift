// JSON that keeps its keys in order and prints like JavaScript's
// JSON.stringify(v, null, 2) — JSONSerialization reorders keys and escapes
// "/". For the JSON and JWT viewers. Port of the JSON parts of lib/logs.ts.
import Foundation

public indirect enum OrderedJSON: Equatable, Sendable {
    case null
    case bool(Bool)
    case number(Double)
    case string(String)
    case array([OrderedJSON])
    case object([(String, OrderedJSON)])

    public static func == (a: OrderedJSON, b: OrderedJSON) -> Bool {
        switch (a, b) {
        case (.null, .null): true
        case (.bool(let x), .bool(let y)): x == y
        case (.number(let x), .number(let y)): x == y
        case (.string(let x), .string(let y)): x == y
        case (.array(let x), .array(let y)): x == y
        case (.object(let x), .object(let y)): x.count == y.count && zip(x, y).allSatisfy { $0.0 == $1.0 && $0.1 == $1.1 }
        default: false
        }
    }

    public subscript(key: String) -> OrderedJSON? {
        if case .object(let o) = self { return o.first { $0.0 == key }?.1 }
        return nil
    }

    public var string: String? { if case .string(let s) = self { s } else { nil } }
    public var number: Double? { if case .number(let n) = self { n } else { nil } }
    public var isContainer: Bool {
        switch self {
        case .array, .object: true
        default: false
        }
    }

    // MARK: Parsing

    public struct ParseError: Error {}

    public static func parse(_ text: String) throws -> OrderedJSON {
        var p = Parser(Array(text.unicodeScalars))
        p.space()
        let v = try p.value()
        p.space()
        guard p.i == p.s.count else { throw ParseError() }
        return v
    }

    struct Parser {
        let s: [Unicode.Scalar]
        var i = 0
        init(_ s: [Unicode.Scalar]) { self.s = s }

        mutating func space() {
            while i < s.count, [" ", "\t", "\n", "\r"].contains(s[i]) { i += 1 }
        }

        mutating func expect(_ word: String) throws {
            for c in word.unicodeScalars {
                guard i < s.count, s[i] == c else { throw ParseError() }
                i += 1
            }
        }

        mutating func value() throws -> OrderedJSON {
            guard i < s.count else { throw ParseError() }
            switch s[i] {
            case "{":
                i += 1
                var out: [(String, OrderedJSON)] = []
                space()
                if i < s.count, s[i] == "}" { i += 1; return .object(out) }
                while true {
                    space()
                    guard i < s.count, s[i] == "\"" else { throw ParseError() }
                    let k = try string()
                    space()
                    try expect(":")
                    space()
                    let v = try value()
                    // A repeated key keeps its first place and its last value.
                    if let j = out.firstIndex(where: { $0.0 == k }) { out[j].1 = v } else { out.append((k, v)) }
                    space()
                    guard i < s.count else { throw ParseError() }
                    if s[i] == "," { i += 1; continue }
                    if s[i] == "}" { i += 1; return .object(out) }
                    throw ParseError()
                }
            case "[":
                i += 1
                var out: [OrderedJSON] = []
                space()
                if i < s.count, s[i] == "]" { i += 1; return .array(out) }
                while true {
                    space()
                    out.append(try value())
                    space()
                    guard i < s.count else { throw ParseError() }
                    if s[i] == "," { i += 1; continue }
                    if s[i] == "]" { i += 1; return .array(out) }
                    throw ParseError()
                }
            case "\"": return .string(try string())
            case "t": try expect("true"); return .bool(true)
            case "f": try expect("false"); return .bool(false)
            case "n": try expect("null"); return .null
            default: return .number(try number())
            }
        }

        mutating func number() throws -> Double {
            let start = i
            if i < s.count, s[i] == "-" { i += 1 }
            guard i < s.count, s[i].properties.numericType != nil, ("0"..."9").contains(s[i]) else { throw ParseError() }
            if s[i] == "0" { i += 1 } else { while i < s.count, ("0"..."9").contains(s[i]) { i += 1 } }
            if i < s.count, s[i] == "." {
                i += 1
                guard i < s.count, ("0"..."9").contains(s[i]) else { throw ParseError() }
                while i < s.count, ("0"..."9").contains(s[i]) { i += 1 }
            }
            if i < s.count, s[i] == "e" || s[i] == "E" {
                i += 1
                if i < s.count, s[i] == "+" || s[i] == "-" { i += 1 }
                guard i < s.count, ("0"..."9").contains(s[i]) else { throw ParseError() }
                while i < s.count, ("0"..."9").contains(s[i]) { i += 1 }
            }
            var text = ""
            text.unicodeScalars.append(contentsOf: s[start..<i])
            guard let d = Double(text) else { throw ParseError() }
            return d
        }

        mutating func string() throws -> String {
            i += 1
            var out = String.UnicodeScalarView()
            while i < s.count {
                let c = s[i]
                i += 1
                if c == "\"" { return String(out) }
                if c.value < 0x20 { throw ParseError() }
                guard c == "\\" else { out.append(c); continue }
                guard i < s.count else { throw ParseError() }
                let e = s[i]
                i += 1
                switch e {
                case "\"": out.append("\"")
                case "\\": out.append("\\")
                case "/": out.append("/")
                case "b": out.append("\u{8}")
                case "f": out.append("\u{c}")
                case "n": out.append("\n")
                case "r": out.append("\r")
                case "t": out.append("\t")
                case "u":
                    var u = try hex4()
                    if (0xD800...0xDBFF).contains(u), i + 1 < s.count, s[i] == "\\", s[i + 1] == "u" {
                        let save = i
                        i += 2
                        let lo = try hex4()
                        if (0xDC00...0xDFFF).contains(lo) {
                            u = 0x10000 + ((u - 0xD800) << 10) + (lo - 0xDC00)
                        } else {
                            i = save
                        }
                    }
                    // A lone surrogate can't live in a Swift string.
                    out.append(Unicode.Scalar(u) ?? "\u{FFFD}")
                default: throw ParseError()
                }
            }
            throw ParseError()
        }

        mutating func hex4() throws -> UInt32 {
            guard i + 4 <= s.count else { throw ParseError() }
            var text = ""
            text.unicodeScalars.append(contentsOf: s[i..<i + 4])
            guard let v = UInt32(text, radix: 16) else { throw ParseError() }
            i += 4
            return v
        }
    }

    // MARK: Printing

    /// JSON.stringify(v, null, 2).
    public func pretty(_ indent: String = "") -> String {
        let inner = indent + "  "
        switch self {
        case .null: return "null"
        case .bool(let b): return b ? "true" : "false"
        case .number(let d): return Self.jsNumber(d)
        case .string(let s): return Self.quote(s)
        case .array(let a):
            if a.isEmpty { return "[]" }
            return "[\n" + a.map { inner + $0.pretty(inner) }.joined(separator: ",\n") + "\n" + indent + "]"
        case .object(let o):
            if o.isEmpty { return "{}" }
            return "{\n" + o.map { inner + Self.quote($0.0) + ": " + $0.1.pretty(inner) }.joined(separator: ",\n") + "\n" + indent + "}"
        }
    }

    static func quote(_ s: String) -> String {
        var out = "\""
        for c in s.unicodeScalars {
            switch c {
            case "\"": out += "\\\""
            case "\\": out += "\\\\"
            case "\u{8}": out += "\\b"
            case "\u{c}": out += "\\f"
            case "\n": out += "\\n"
            case "\r": out += "\\r"
            case "\t": out += "\\t"
            default:
                if c.value < 0x20 { out += String(format: "\\u%04x", c.value) } else { out.unicodeScalars.append(c) }
            }
        }
        return out + "\""
    }

    /// A number as JavaScript prints it: shortest round trip, 1 not 1.0, 1e+21, 1e-7.
    public static func jsNumber(_ d: Double) -> String {
        guard d.isFinite else { return "null" }
        if d == 0 { return "0" }
        let sign = d < 0 ? "-" : ""
        // Swift's description is the shortest round-trip form; take its digits and exponent.
        let desc = "\(abs(d))"
        let parts = desc.lowercased().split(separator: "e")
        var mantissa = String(parts[0])
        var exp = parts.count > 1 ? Int(parts[1])! : 0
        if let dot = mantissa.firstIndex(of: ".") {
            exp += mantissa.distance(from: mantissa.startIndex, to: dot)
            mantissa.remove(at: dot)
        } else {
            exp += mantissa.count
        }
        // Digits without leading or trailing zeros; value = 0.digits × 10^exp.
        while mantissa.hasPrefix("0") {
            mantissa.removeFirst()
            exp -= 1
        }
        while mantissa.hasSuffix("0") { mantissa.removeLast() }
        let k = mantissa.count, n = exp
        let digits = mantissa
        if k <= n, n <= 21 { return sign + digits + String(repeating: "0", count: n - k) }
        if 0 < n, n <= 21 { return sign + digits.prefix(n) + "." + digits.dropFirst(n) }
        if -6 < n, n <= 0 { return sign + "0." + String(repeating: "0", count: -n) + digits }
        let e = n - 1
        let m = k == 1 ? digits : digits.prefix(1) + "." + digits.dropFirst()
        return sign + m + "e" + (e >= 0 ? "+" : "-") + String(abs(e))
    }

    /// The first JSON object or array in some text, pretty-printed (log lines
    /// with a payload). Tries at most 20 brackets.
    public static func find(_ text: String) -> String? {
        let chars = Array(text)
        var tries = 0
        for (i, c) in chars.enumerated() where c == "{" || c == "[" {
            tries += 1
            if tries > 20 { break }
            let closer: Character = c == "{" ? "}" : "]"
            guard let end = chars.lastIndex(of: closer), end > i else { continue }
            if let v = try? parse(String(chars[i...end])), v.isContainer { return v.pretty() }
        }
        return nil
    }
}

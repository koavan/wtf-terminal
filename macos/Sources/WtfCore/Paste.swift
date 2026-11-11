// Multi-line paste confirmation: text with more than one line, or with
// control characters, is shown before it reaches the shell. Decided by the
// text alone — never by guessing what a command does.
// Like internal/uicore/paste.go.
import Foundation

public struct PasteFacts: Equatable, Sendable {
    /// Lines it has; a final newline doesn't start another.
    public let lines: Int
    /// Whether it ends with a newline, which runs the last line without bracketed paste.
    public let finalNewline: Bool
    /// Control characters in it.
    public let controls: Int

    public init(_ text: String) {
        finalNewline = Paste.hasFinalNewline(text)
        let body = finalNewline ? Paste.withoutFinalNewline(text) : text
        // "\r\n" is one Character in Swift, so this splits on any line break.
        lines = body.split(omittingEmptySubsequences: false, whereSeparator: { $0 == "\n" || $0 == "\r" || $0 == "\r\n" }).count
        controls = text.unicodeScalars.filter(Paste.isControl).count
    }

    /// Whether a paste is shown first: more than one line, a final newline, or a control character.
    public var needsConfirm: Bool { lines > 1 || finalNewline || controls > 0 }
}

public enum Paste {
    /// C0 controls other than tab and newlines, DEL, and C1 controls (ESC [ as one byte).
    static func isControl(_ s: Unicode.Scalar) -> Bool {
        switch s.value {
        case 0x00...0x08, 0x0B, 0x0C, 0x0E...0x1F, 0x7F, 0x80...0x9F: true
        default: false
        }
    }

    static func hasFinalNewline(_ text: String) -> Bool {
        guard let last = text.last else { return false }
        return last == "\n" || last == "\r" || last == "\r\n"
    }

    /// The text without its final newline, so the shell waits for Enter.
    public static func withoutFinalNewline(_ text: String) -> String {
        hasFinalNewline(text) ? String(text.dropLast()) : text
    }

    /// The text with control characters made visible: ␛ for ESC, ␀–␟ for the rest, ␡ for DEL, ⟨9B⟩ for C1.
    public static func showControls(_ text: String) -> String {
        var out = String.UnicodeScalarView()
        for s in text.unicodeScalars {
            guard isControl(s) else {
                out.append(s)
                continue
            }
            switch s.value {
            case ..<0x20: out.append(Unicode.Scalar(0x2400 + s.value)!)
            case 0x7F: out.append("␡")
            default: out.append(contentsOf: "⟨\(String(s.value, radix: 16, uppercase: true))⟩".unicodeScalars)
            }
        }
        return String(out)
    }
}

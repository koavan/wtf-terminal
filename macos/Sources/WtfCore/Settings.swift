// The config as the backend resolves it (config.Resolved in
// internal/config): the settings from config.toml and the dark and light
// themes. JSON keys match the Go struct tags.
import Foundation

public struct Config: Decodable, Equatable, Sendable {
    public var theme: String
    public var fontFamily: String
    public var fontSize: Double
    public var lineHeight: Double
    public var cursorStyle: String
    public var cursorBlink: Bool
    public var scrollback: Int
    public var optionAsMeta: Bool
    public var copyOnSelect: Bool
    public var shell: String
    public var shellIntegration: Bool
    public var notifyAfter: Double
    public var unfocusedSplitDim: Double
    public var contextBadges: Bool
    public var redactSecrets: Bool
    public var clipboardClear: Double
    public var confirmPaste: Bool
}

public struct Theme: Decodable, Equatable, Sendable {
    public var name: String
    public var background: String
    public var foreground: String
    public var cursor: String
    public var cursorText: String
    public var selectionBackground: String
    public var selectionForeground: String
    public var palette: [String]
    public var dark: Bool
}

public struct ResolvedConfig: Decodable, Equatable, Sendable {
    public var config: Config
    public var dark: Theme
    public var light: Theme
    public var problems: [String]
    public var path: String

    /// The theme for the system's appearance.
    public func theme(dark isDark: Bool) -> Theme { isDark ? dark : light }
}

/// A color as 0–255 components.
public struct RGB: Equatable, Sendable {
    public var r: Double, g: Double, b: Double, a: Double

    public init(_ r: Double, _ g: Double, _ b: Double, _ a: Double = 1) {
        self.r = r
        self.g = g
        self.b = b
        self.a = a
    }

    /// "#rrggbb" or "#rrggbbaa", as the themes write them.
    public init?(hex: String) {
        let h = hex.trimmingCharacters(in: .whitespaces)
        guard h.hasPrefix("#"), h.count == 7 || h.count == 9, let n = UInt32(h.dropFirst(), radix: 16) else { return nil }
        if h.count == 7 {
            self.init(Double(n >> 16 & 255), Double(n >> 8 & 255), Double(n & 255))
        } else {
            self.init(Double(n >> 24 & 255), Double(n >> 16 & 255), Double(n >> 8 & 255), Double(n & 255) / 255)
        }
    }

    /// Part of the way from this color to another: 0 is this, 1 is that.
    public func mixed(with o: RGB, _ t: Double) -> RGB {
        RGB(r + (o.r - r) * t, g + (o.g - g) * t, b + (o.b - b) * t, a + (o.a - a) * t)
    }

    public var hex: String {
        String(format: "#%02x%02x%02x", Int(r.rounded()), Int(g.rounded()), Int(b.rounded()))
    }
}

extension Theme {
    /// The color secrets are covered with while hidden: a solid bar between
    /// the theme's text and background (coverColor in lib/secretmask.ts).
    public var coverColor: RGB {
        let fg = RGB(hex: foreground) ?? RGB(128, 128, 128)
        let bg = RGB(hex: background) ?? RGB(0, 0, 0)
        let c = fg.mixed(with: bg, 0.6)
        return RGB(c.r.rounded(), c.g.rounded(), c.b.rounded())
    }
}

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
    public var sidebarAutoRefresh: Bool
    public var sidebarRefreshInterval: Double
    public var showUsage: Bool
    public var redactSecrets: Bool
    public var clipboardClear: Double
    public var confirmPaste: Bool
    public var logHighlighting: Bool
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

extension RGB {
    /// WCAG relative luminance, 0–1.
    public var luminance: Double {
        func lin(_ v: Double) -> Double {
            let s = v / 255
            return s <= 0.04045 ? s / 12.92 : pow((s + 0.055) / 1.055, 2.4)
        }
        return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)
    }

    /// How far from grey, 0–1 (pale pastels score low).
    var chroma: Double { (max(r, g, b) - min(r, g, b)) / 255 }

    func contrast(_ o: RGB) -> Double {
        let (hi, lo) = (max(luminance, o.luminance), min(luminance, o.luminance))
        return (hi + 0.05) / (lo + 0.05)
    }
}

extension Theme {
    /// The UI accent: the cursor colour when colourful and readable, else the
    /// theme's blue, then magenta (accentFor in lib/themes.ts).
    public var accent: RGB {
        let bg = RGB(hex: background) ?? RGB(0, 0, 0)
        let candidates = [cursor] + [12, 4, 13, 5].compactMap { $0 < palette.count ? palette[$0] : nil }
        for c in candidates {
            if let rgb = RGB(hex: c), rgb.chroma > 0.25, rgb.contrast(bg) >= 2.5 { return RGB(rgb.r, rgb.g, rgb.b) }
        }
        return dark ? RGB(0x8d, 0x9c, 0xff) : RGB(0x4c, 0x5b, 0xd4)
    }

    /// The window chrome around the terminal: a deeper (or lighter) shade of
    /// its background (applyChrome in lib/themes.ts).
    public var chrome: RGB {
        let bg = RGB(hex: background) ?? RGB(0, 0, 0)
        return dark ? bg.mixed(with: RGB(0, 0, 0), 0.28) : bg.mixed(with: RGB(214, 216, 222), 0.45)
    }
}

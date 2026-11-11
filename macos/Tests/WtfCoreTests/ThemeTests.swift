import Testing
@testable import WtfCore

// The cases of the Svelte build's themes tests (accentFor, parseHex,
// luminance; xtermTheme was xterm.js-only).

private let tokyoNight = ["#15161e", "#f7768e", "#9ece6a", "#e0af68", "#7aa2f7", "#bb9af7", "#7dcfff", "#a9b1d6",
                          "#414868", "#f7768e", "#9ece6a", "#e0af68", "#7aa2f7", "#bb9af7", "#7dcfff", "#c0caf5"]

private func theme(background: String = "#1a1b26", cursor: String = "#c0caf5", palette: [String] = tokyoNight, dark: Bool = true) -> Theme {
    Theme(name: "t", background: background, foreground: "#c0caf5", cursor: cursor, cursorText: "#1a1b26",
          selectionBackground: "#33467c", selectionForeground: "", palette: palette, dark: dark)
}

@Test func parsesHexColorsWithOrWithoutAlpha() {
    for s in ["#ff8000", "#ff800080"] {
        let c = RGB(hex: s)
        #expect(c?.r == 255 && c?.g == 128 && c?.b == 0, "\(s)")
    }
    #expect(RGB(hex: "rgba(1,2,3,0.5)") == nil)
}

@Test func computesLuminance() {
    #expect(RGB(0, 0, 0).luminance == 0)
    #expect(abs(RGB(255, 255, 255).luminance - 1) < 0.005)
}

@Test func accentForThemes() {
    #expect(theme(cursor: "#ff79c6").accent.hex == "#ff79c6")
    // Tokyo Night's cursor is its (greyish) foreground: the theme's blue instead.
    #expect(theme().accent.hex == "#7aa2f7")
    // Colors that would be unreadable on the background are skipped.
    var light = [String](repeating: "#fdf6e3", count: 16)
    light[4] = "#268bd2"
    #expect(theme(background: "#fdf6e3", cursor: "#fdf6e3", palette: light, dark: false).accent.hex == "#268bd2")
}

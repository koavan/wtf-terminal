import SwiftTerm
import Testing
@testable import WtfCore

private final class Sink: TerminalDelegate {
    func send(source: Terminal, data: ArraySlice<UInt8>) {}
}

private func term(cols: Int = 40, rows: Int = 6, _ text: String) -> (Terminal, Sink) {
    let sink = Sink()
    let t = Terminal(delegate: sink, options: TerminalOptions(cols: cols, rows: rows, scrollback: 100))
    t.feed(text: text)
    return (t, sink)
}

private let colors = Logs.Colors(palette: ["#000000", "#ff0000", "#00ff00", "#ffff00", "#0000ff", "#ff00ff", "#00ffff", "#ffffff"],
                                 foreground: "#ffffff", background: "#000000")

@Test func tintsWhatsOnScreen() {
    let (t, _) = term("12:00:01 ERROR from 10.0.0.1\r\n\u{1b}[33mWARN\u{1b}[0m pod/a\r\n")
    #expect(Logs.tints(t, colors: colors).map { "\($0.row),\($0.col),\($0.width),\($0.color)" } ==
        ["0,0,8,\(colors.timestamp)", "0,9,5,#ff0000", "0,20,8,#ff00ff", "1,5,5,#00ffff"])
    #expect(colors.timestamp == "#8c8c8c")
}

@Test func tintsFollowAWrap() {
    let (t, _) = term(cols: 10, "abcdefg 2026-10-09T10:00:00Z")
    #expect(Logs.tints(t, colors: colors).map { "\($0.row),\($0.col),\($0.width)" } == ["0,8,2", "1,0,10", "2,0,8"])
}

@Test func noTintsInFullScreenPrograms() {
    let (t, _) = term("\u{1b}[?1049hERROR in vim")
    #expect(Logs.tints(t, colors: colors).isEmpty)
}

@Test func tokenAtACell() {
    let (t, _) = term("x pod/web-1 y")
    if case .k8s(_, let name)? = Logs.token(t, row: 0, col: 4)?.kind { #expect(name == "web-1") } else { Issue.record("no token") }
    #expect(Logs.token(t, row: 0, col: 0) == nil)
    let (w, _) = term("日本 ERROR")
    #expect(Logs.token(w, row: 0, col: 5)?.text == "ERROR")
}

@Test func colorsFromTheTheme() {
    let c = Logs.Colors(palette: ["#000000", "#ff5555", "#50fa7b", "#f1fa8c", "#bd93f9", "#ff79c6", "#8be9fd", "#ffffff"],
                        foreground: "#ffffff", background: "#000000")
    #expect(c.error == "#ff5555" && c.warn == "#f1fa8c")
    #expect(c.timestamp.range(of: "^#[0-9a-f]{6}$", options: .regularExpression) != nil)
}

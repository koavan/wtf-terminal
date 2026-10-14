import SwiftTerm
import Testing
@testable import WtfCore

private final class Sink: TerminalDelegate {
    func send(source: Terminal, data: ArraySlice<UInt8>) {}
}

private func terminal(cols: Int = 40, rows: Int = 6, _ text: String) -> (Terminal, Sink) {
    let sink = Sink()
    let t = Terminal(delegate: sink, options: TerminalOptions(cols: cols, rows: rows, scrollback: 100))
    t.feed(text: text)
    return (t, sink)
}

private let AKID = "AKIAABCDEFGHIJKLMNOP"

@Test func coversASecretWhereItIs() {
    let (t, _) = terminal("ok\r\nkey: \(AKID) end")
    #expect(SecretCovers.scan(t) == [.init(row: 1, col: 5, width: 20)])
}

@Test func nothingToCover() {
    let (t, _) = terminal("NAME READY STATUS\r\nweb-1 1/1 Running")
    #expect(SecretCovers.scan(t).isEmpty)
}

@Test func followsASecretAcrossAWrap() {
    // 30 columns of text, then the key: it wraps after 10 of its 20 characters.
    let (t, _) = terminal(cols: 40, String(repeating: "-", count: 29) + " " + AKID)
    #expect(SecretCovers.scan(t) == [.init(row: 0, col: 30, width: 10), .init(row: 1, col: 0, width: 10)])
}

@Test func countsWideCharactersAsTwoCells() {
    let (t, _) = terminal("日本 \(AKID)")
    #expect(SecretCovers.scan(t) == [.init(row: 0, col: 5, width: 20)])
}

@Test func coversAPrivateKeyThatStartedAboveTheScreen() {
    let body = (1...10).map { "b3BlbnNzaC1rZXktdjEAAAA\($0)" }.joined(separator: "\r\n")
    let (t, _) = terminal(rows: 4, "-----BEGIN OPENSSH PRIVATE KEY-----\r\n\(body)\r\n-----END OPENSSH PRIVATE KEY-----\r\nafter")
    let covers = SecretCovers.scan(t)
    // The screen shows the key's last lines, its END line, and "after".
    #expect(covers.map(\.row) == [0, 1, 2])
    #expect(covers.last == .init(row: 2, col: 0, width: 33))
}

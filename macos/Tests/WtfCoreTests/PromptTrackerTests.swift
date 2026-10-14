import Foundation
import SwiftTerm
import Testing
@testable import WtfCore

// The cases of frontend/src/lib/prompts.test.ts, on SwiftTerm's headless terminal.

private final class Sink: TerminalDelegate {
    func send(source: Terminal, data: ArraySlice<UInt8>) {}
}

private let A = "\u{1b}]133;A\u{07}"
private let C = "\u{1b}]133;C\u{07}"
private func D(_ code: Int) -> String { "\u{1b}]133;D;\(code)\u{07}" }
private func E(_ cmd: String) -> String { "\u{1b}]633;E;\(cmd)\u{07}" }
/// What a shell with the integration prints for one command.
private func run(_ cmd: String, _ output: String, _ code: Int = 0) -> String { "\(A)$ \(cmd)\r\n\(E(cmd))\(C)\(output)\(D(code))" }

private final class Setup {
    let sink = Sink()
    let term: Terminal
    let tracker: PromptTracker
    var clock = Date(timeIntervalSince1970: 0)
    var prompts = 0
    var started: [(String, Int)] = []
    var ended: [(Int, Double)] = []
    var env: [String] = []

    init(rows: Int = 5, cols: Int = 40, scrollback: Int = 1000) {
        term = Terminal(delegate: sink, options: TerminalOptions(cols: cols, rows: rows, scrollback: scrollback))
        var now: () -> Date = { Date() }
        tracker = PromptTracker(terminal: term, now: { now() })
        now = { [unowned self] in clock }
        tracker.onPrompt = { [unowned self] in prompts += 1 }
        tracker.onCommandStart = { [unowned self] in started.append(($0, $1)) }
        tracker.onCommandEnd = { [unowned self] in ended.append(($0, $1)) }
        tracker.onEnv = { [unowned self] in env.append($0) }
    }

    func write(_ s: String) { term.feed(text: s) }
}

@Test func reportsPromptsCommandTextAndExitCodes() {
    let s = Setup()
    s.write(run("false", "", 1) + A + "$ ")
    #expect(s.prompts == 2)
    #expect(s.started.map(\.0) == ["false"] && s.started.map(\.1) == [1])
    #expect(s.ended.map(\.0) == [1])
}

@Test func lastOutputJoinsSoftWrappedLines() {
    let s = Setup()
    let long = String(repeating: "x", count: 50) // wraps at 40 columns
    s.write(run("ls", "one\r\ntwo\r\n") + run("cat", long + "\r\nend\r\n") + A + "$ ")
    let id = s.tracker.lastFinished()
    #expect(id == 2)
    #expect(s.tracker.output(id!) == "\(long)\nend")
    #expect(s.tracker.output(1) == "one\ntwo")
}

@Test func reportsContextVariables() {
    let s = Setup()
    s.write("\u{1b}]633;P;WtfEnv=KUBECONFIG=/a\\x3bb\u{07}" + A)
    #expect(s.env == ["KUBECONFIG=/a;b"])
}

@Test func nothingFinishedYet() {
    let s = Setup()
    s.write(A + "$ ")
    #expect(s.tracker.lastFinished() == nil)
}

@Test func jumpsBetweenPromptsInTheScrollback() {
    let s = Setup()
    let lines = (1...8).map { "line \($0)" }.joined(separator: "\r\n") + "\r\n"
    s.write(run("one", lines) + run("two", lines) + A + "$ ")
    let rows = s.tracker.blocks(from: 0, to: 1000).map(\.row)
    #expect(rows.count == 3)
    #expect(Rows.text(s.term, rows[1]) == "$ two")
    #expect(s.tracker.jump(-1, from: rows[2]) == rows[1])
    #expect(s.tracker.jump(1, from: rows[0]) == rows[1])
    #expect(s.tracker.jump(1, from: rows[2]) == nil)
}

@Test func ignoresAStrayD() {
    let s = Setup()
    s.write(A + "$ " + D(1))
    #expect(s.ended.isEmpty)
}

@Test func unescapesCommandLines() {
    #expect(PromptTracker.unescape(#"echo a\x3bb \\ c"#) == #"echo a;b \ c"#)
    #expect(PromptTracker.unescape(#"caf\xc3\xa9"#) == "café")
}

@Test func findsTheCommandALineBelongsTo() {
    let s = Setup()
    s.write(run("one", "a\r\nb\r\n") + run("two", "c\r\n") + A + "$ ")
    let rows = s.tracker.blocks(from: 0, to: 1000).map(\.row)
    #expect(s.tracker.blockAt(rows[0]) == 1)
    #expect(s.tracker.blockAt(rows[0] + 2) == 1)
    #expect(s.tracker.blockAt(rows[1] + 1) == 2)
    #expect(s.tracker.blockAt(rows[2]) == nil) // a prompt without a command yet
}

@Test func knowsTextStatusDurationAndOutput() {
    let s = Setup()
    s.write(A + "$ sleep 2\r\n" + E("sleep 2") + C)
    s.clock = s.clock.addingTimeInterval(2.5)
    s.write("done\r\n" + D(3) + A + "$ ")
    let b = s.tracker.info(1)!
    #expect(b.text == "sleep 2" && b.exitCode == 3 && !b.running && b.hasOutput)
    #expect(b.durationMs == 2500)
    #expect(s.ended.map(\.1) == [2500])
}

@Test func runningCommandOutputSoFar() {
    let s = Setup()
    s.write(A + "$ tail\r\n" + E("tail") + C + "first\r\nsecond")
    #expect(s.tracker.info(1)?.running == true)
    #expect(s.tracker.output(1) == "first\nsecond")
}

@Test func marksSurviveScrollbackTrimming() {
    let s = Setup(rows: 5, scrollback: 20)
    s.write(run("old", "x\r\n"))
    let filler = (1...40).map { "f\($0)" }.joined(separator: "\r\n") + "\r\n"
    s.write(run("new", filler) + A + "$ ")
    #expect(s.term.buffer.totalLinesTrimmed > 0)
    // The first command has left the scrollback; the last prompt is still where it was.
    #expect(s.tracker.info(1)?.hasOutput == false)
    #expect(s.tracker.output(1) == nil)
    let last = s.tracker.blocks(from: 0, to: .max).last!
    #expect(Rows.text(s.term, last.row) == "$ ")
    #expect(last.row == Rows.cursor(s.term))
}

@Test func marksFollowAReflow() {
    let s = Setup(rows: 5, cols: 40)
    s.write(run("one", String(repeating: "y", count: 70) + "\r\n") + run("two", "z\r\n") + A + "$ ")
    s.term.resize(cols: 20, rows: 5)
    s.tracker.relocate()
    let rows = s.tracker.blocks(from: 0, to: .max).map(\.row)
    #expect(rows.count == 3)
    #expect(rows.map { Rows.text(s.term, $0) } == ["$ one", "$ two", "$ "])
    #expect(s.tracker.output(2) == "z")
}

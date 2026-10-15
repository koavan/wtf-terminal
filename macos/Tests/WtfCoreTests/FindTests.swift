import Testing
@testable import WtfCore

@Test func findsMatches() {
    let rows = [(0, "error: Error ERROR"), (1, "no luck"), (2, "an error")]
    #expect(Find.matches(rows, "error", caseSensitive: false).map { "\($0.row):\($0.col)" } == ["0:0", "0:7", "0:13", "2:3"])
    #expect(Find.matches(rows, "error", caseSensitive: true).count == 2)
    #expect(Find.matches(rows, "", caseSensitive: false).isEmpty)
    #expect(Find.matches([(0, "aaaa")], "aa", caseSensitive: true).count == 2)
    #expect(Find.status([], current: nil) == "No results")
    #expect(Find.status([.init(row: 0, col: 0, width: 1), .init(row: 1, col: 0, width: 1)], current: 1) == "2 of 2")
}

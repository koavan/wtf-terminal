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

@Test func findsMatchesInCells() {
    // "日本 error": the wide characters take two cells each.
    let line = Find.Line(row: 7, text: "日本 error", cells: [0, 2, 4, 5, 6, 7, 8, 9, 10])
    #expect(Find.matches([line], "error", caseSensitive: true) == [.init(row: 7, col: 5, width: 5)])
    #expect(Find.matches([line], "日本", caseSensitive: true) == [.init(row: 7, col: 0, width: 4)])
}

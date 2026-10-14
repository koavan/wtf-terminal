import Testing
@testable import WtfCore

// The outcomes frontend/src/lib/fuzzy.test.ts requires.

@Test func fuzzyMatchesInOrder() {
    #expect(Fuzzy.match("nt", "New Tab")?.indices == [0, 4])
    #expect(Fuzzy.match("xyz", "New Tab") == nil)
    #expect(Fuzzy.match("new tab", "New Tab") != nil)
    #expect(Fuzzy.match("", "anything")?.score == 0)
}

@Test func fuzzyRanksWordStartsAndRuns() {
    func best(_ q: String, _ items: [String]) -> String? { Fuzzy.rank(q, items) { $0 }.first?.item }
    #expect(best("sr", ["Smaller Text", "Split Right"]) == "Split Right")
    #expect(best("ct", ["Clear Buffer", "Close Tab"]) == "Close Tab")
    #expect(best("split", ["Select Pane Left In Tab", "Split Down"]) == "Split Down")
}

@Test func rankKeepsOrderForTies() {
    #expect(Fuzzy.rank("a", ["xa", "ya"]) { $0 }.map(\.item) == ["xa", "ya"])
}

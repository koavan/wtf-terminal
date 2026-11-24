import Testing
@testable import WtfCore

// The cases internal/uicore/keytrail_test.go checks.
@Test func keyTrailCodes() {
    let konami = "up up down down left right left right b a".split(separator: " ").map(String.init)
    let codes = [konami, "amber".map(String.init), "paper".map(String.init)]
    func found(_ keys: String) -> [Int] {
        var t = KeyTrail()
        return keys.split(separator: " ").compactMap { t.key(String($0), codes: codes) }
    }
    #expect(found("up up down down left right left right b a") == [0])
    #expect(found("up up up up down down left right left right b a") == [0])
    #expect(found("up up down down left right left right a b") == [])
    #expect(found("up up down down left right left right b x a") == [])
    #expect(found("a m b e r") == [1])
    #expect(found("p a p e r a m b e r") == [2, 1])
    #expect(found("p a p a p e r") == [2])
    #expect(found("a m b e r e r") == [1])
    #expect(found("up up down down left right left right b a p a p e r") == [0, 2])
}

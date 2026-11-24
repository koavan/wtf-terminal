import Testing
@testable import WtfCore

// The cases internal/uicore/konami_test.go checks.

private func finished(_ keys: String) -> Int {
    var k = Konami()
    let names: [String: Konami.Key] = ["up": .up, "down": .down, "left": .left, "right": .right, "a": .a, "b": .b]
    return keys.split(separator: " ").filter { k.key(names[String($0)] ?? .other) }.count
}

@Test func konamiCode() {
    let code = "up up down down left right left right b a"
    #expect(finished(code) == 1)
    #expect(finished(code + " " + code) == 2)
    #expect(finished("up up down down left right left right a b") == 0)
    #expect(finished("up " + code) == 1)
    #expect(finished("up up up up " + code) == 1)
    #expect(finished("x " + code + " x") == 1)
    #expect(finished("up up down down left right left right b x a") == 0)
    #expect(finished("up up down down left right left right b up " + code) == 1)
}

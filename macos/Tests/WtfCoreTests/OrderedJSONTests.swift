import Testing
@testable import WtfCore

@Test func jsonPretty() throws {
    let v = try OrderedJSON.parse(#"{"b":1,"a":[true,null,"x/y\n"],"e":{},"f":[],"b":2}"#)
    #expect(v.pretty() == "{\n  \"b\": 2,\n  \"a\": [\n    true,\n    null,\n    \"x/y\\n\"\n  ],\n  \"e\": {},\n  \"f\": []\n}")
    #expect(throws: OrderedJSON.ParseError.self) { try OrderedJSON.parse("{\"a\":1,}") }
    #expect(throws: OrderedJSON.ParseError.self) { try OrderedJSON.parse("[1] x") }
    #expect(try OrderedJSON.parse("\"\\ud83d\\ude00 é\\u0001\"").pretty() == "\"😀 é\\u0001\"")
}

@Test func jsNumbers() {
    let cases: [(Double, String)] = [(1.0, "1"), (1e21, "1e+21"), (1e-7, "1e-7"), (0.000001, "0.000001"), (-0.0, "0"),
                                     (123.45, "123.45"), (1791829800, "1791829800"), (1.5e300, "1.5e+300"), (-2.5, "-2.5"),
                                     (100, "100"), (0.1, "0.1"), (12345678901234567890, "12345678901234567000")]
    for (d, s) in cases { #expect(OrderedJSON.jsNumber(d) == s, "\(d)") }
}

@Test func findJson() {
    #expect(OrderedJSON.find(#"2026-10-09 INFO {"a":1,"b":[true]}"#) == "{\n  \"a\": 1,\n  \"b\": [\n    true\n  ]\n}")
    #expect(OrderedJSON.find(#"[INFO] payload=["x"]"#) == "[\n  \"x\"\n]")
    #expect(OrderedJSON.find("[INFO] nothing {here}") == nil)
    #expect(OrderedJSON.find("42") == nil)
}

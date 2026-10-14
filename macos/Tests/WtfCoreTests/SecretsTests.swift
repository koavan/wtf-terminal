import Foundation
import Testing
@testable import WtfCore

/// testdata/secrets/cases.json, written by the TypeScript implementation
/// (node testdata/secrets/generate.ts): the Swift port must agree with it.
struct SharedCase: Decodable, CustomTestStringConvertible {
    struct Span: Decodable { let kind: String; let start: Int; let end: Int }
    let text: String
    let found: [Span]
    let redacted: String
    var testDescription: String { String(text.prefix(50)).debugDescription }

    static let all: [SharedCase] = {
        let url = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent("testdata/secrets/cases.json")
        return try! JSONDecoder().decode([SharedCase].self, from: Data(contentsOf: url))
    }()
}

@Test(arguments: SharedCase.all)
func findsWhatTheTypeScriptFinds(_ c: SharedCase) {
    let want = c.found.map { SecretSpan(start: $0.start, end: $0.end, kind: $0.kind) }
    #expect(Secrets.find(c.text) == want)
    #expect(Secrets.redact(c.text) == c.redacted)
}

@Test func sharedCasesLoaded() {
    #expect(SharedCase.all.count > 30)
}

@Test func jsPatternKeepsWordsASCII() {
    // In JavaScript \b sits between é and A; ICU would see no boundary.
    #expect(Secrets.find("éAKIAABCDEFGHIJKLMNOP").map(\.kind) == ["AWS access key"])
    #expect(Secrets.jsPattern(#"[\w.-]\b\s"#) == #"[A-Za-z0-9_.-](?:(?<![A-Za-z0-9_])(?=[A-Za-z0-9_])|(?<=[A-Za-z0-9_])(?![A-Za-z0-9_]))\s"#)
}

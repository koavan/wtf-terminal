import Foundation
import Testing
@testable import WtfCore

/// testdata/review/reviewable.json, shared with Go's review.Reviewable.
@Test func reviewableMatchesGo() throws {
    let url = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        .appendingPathComponent("testdata/review/reviewable.json")
    let cases = try JSONDecoder().decode([String: Bool].self, from: Data(contentsOf: url))
    #expect(cases.count > 10)
    for (command, want) in cases {
        #expect(Review.reviewable(command) == want, "\(command)")
    }
}

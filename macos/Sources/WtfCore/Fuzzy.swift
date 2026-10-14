// Fuzzy matching for the palette and the tab overview. A port of
// frontend/src/lib/fuzzy.ts: characters in order, rewarding runs, word
// starts and the very start, penalising gaps and long texts.
import Foundation

public enum Fuzzy {
    public struct Match: Equatable {
        public let score: Double
        /// Matched character positions in the text.
        public let indices: [Int]
    }

    static func isBoundary(_ chars: [Character], _ i: Int) -> Bool {
        i == 0 || chars[i - 1].isWhitespace || "-_/.:".contains(chars[i - 1])
    }

    public static func match(_ query: String, _ text: String) -> Match? {
        let q = Array(query.lowercased().filter { !$0.isWhitespace })
        if q.isEmpty { return Match(score: 0, indices: []) }
        let orig = Array(text)
        let t = Array(text.lowercased())
        guard t.count == orig.count else { return nil }
        var indices: [Int] = []
        var score = 0.0, ti = 0, prev = -2
        for ch in q {
            var found = -1
            var j = ti
            while j < t.count {
                defer { j += 1 }
                guard t[j] == ch else { continue }
                if found < 0 { found = j }
                if j == prev + 1 { found = j; break }
                if isBoundary(orig, j) { found = j; break }
            }
            if found < 0 { return nil }
            score += 1
            if found == prev + 1 { score += 7 }
            if isBoundary(orig, found) { score += 5 }
            if found == 0 { score += 4 }
            score -= Double(min(found - ti, 6)) * 0.25
            indices.append(found)
            prev = found
            ti = found + 1
        }
        score -= Double(t.count) * 0.02
        return Match(score: score, indices: indices)
    }

    public static func score(_ query: String, _ text: String) -> Double? { match(query, text)?.score }

    /// Entries that match, best first; ties keep their order.
    public static func rank<T>(_ query: String, _ items: [T], label: (T) -> String) -> [(item: T, match: Match)] {
        items.enumerated().compactMap { i, x in match(query, label(x)).map { (i, x, $0) } }
            .sorted { $0.2.score != $1.2.score ? $0.2.score > $1.2.score : $0.0 < $1.0 }
            .map { ($0.1, $0.2) }
    }
}

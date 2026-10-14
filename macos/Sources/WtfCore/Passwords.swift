// The password generator: chosen character classes, at least one of each,
// from the system's secure random source. Port of lib/passwords.ts.
import Foundation
import Security

public enum Passwords {
    public static let minLength = 4, maxLength = 128
    public static let lower = "abcdefghijklmnopqrstuvwxyz"
    public static let upper = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
    public static let digits = "0123456789"
    public static let symbols = "!#$%&()*+,-./:;<=>?@[]^_{|}~"
    public static let lookAlikes = "Il1O0o|"

    public struct Options: Equatable, Sendable {
        public var length = 24
        public var lower = true, upper = true, digits = true, symbols = true
        public var avoidAmbiguous = false
        public init() {}
    }

    public static func classes(_ o: Options) -> [[Character]] {
        let sets = [(o.lower, lower), (o.upper, upper), (o.digits, digits), (o.symbols, symbols)].filter(\.0).map(\.1)
        return sets.map { s in Array(o.avoidAmbiguous ? s.filter { !lookAlikes.contains($0) } : s) }.filter { !$0.isEmpty }
    }

    public static func clamp(_ n: Int) -> Int { min(maxLength, max(minLength, n)) }

    /// A uniform 32-bit value from SecRandomCopyBytes.
    public static func secureRandom() -> UInt32 {
        var v: UInt32 = 0
        _ = withUnsafeMutableBytes(of: &v) { SecRandomCopyBytes(kSecRandomDefault, 4, $0.baseAddress!) }
        return v
    }

    /// 0..<n without modulo bias.
    public static func randomInt(_ n: Int, _ random: () -> UInt32 = secureRandom) -> Int {
        let limit = (UInt64(1) << 32) / UInt64(n) * UInt64(n)
        while true {
            let v = UInt64(random())
            if v < limit { return Int(v % UInt64(n)) }
        }
    }

    public static func generate(_ o: Options, random: () -> UInt32 = secureRandom) -> String {
        let sets = classes(o)
        guard !sets.isEmpty else { return "" }
        let length = clamp(o.length)
        let pool = sets.flatMap { $0 }
        var out = sets.map { $0[randomInt($0.count, random)] }
        while out.count < length { out.append(pool[randomInt(pool.count, random)]) }
        for i in stride(from: out.count - 1, to: 0, by: -1) { out.swapAt(i, randomInt(i + 1, random)) }
        return String(out)
    }

    public static func entropyBits(_ o: Options) -> Int {
        let pool = classes(o).reduce(0) { $0 + $1.count }
        guard pool > 0 else { return 0 }
        return Int((Double(clamp(o.length)) * log2(Double(pool))).rounded(.down))
    }
}

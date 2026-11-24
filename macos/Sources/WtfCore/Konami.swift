/// Follows the keys typed toward the Konami code, ↑ ↑ ↓ ↓ ← → ← → B A
/// (Konami in internal/uicore).
public struct Konami: Sendable {
    public enum Key: Sendable { case up, down, left, right, a, b, other }

    private static let code: [Key] = [.up, .up, .down, .down, .left, .right, .left, .right, .b, .a]
    private var n = 0

    public init() {}

    /// Takes the next key and reports whether it finished the code.
    public mutating func key(_ k: Key) -> Bool {
        if k == Self.code[n] {
            n += 1
        } else if k != .up {
            n = 0
        } else if n != 2 {
            // A third ↑ still leaves the last two to go on from.
            n = 1
        }
        guard n == Self.code.count else { return false }
        n = 0
        return true
    }
}

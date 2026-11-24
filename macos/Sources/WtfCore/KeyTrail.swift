/// The last keys typed on the home page, to spot the codes that turn the
/// hidden themes on (KeyTrail in internal/uicore; config.Retro).
public struct KeyTrail: Sendable {
    private var last: [String] = []
    /// Longer than any code.
    private static let max = 16

    public init() {}

    /// Adds a key ("up", "down", "left", "right", a letter, or "" for any
    /// other) and returns which code the keys typed now end with. The trail
    /// starts over after a code.
    public mutating func key(_ name: String, codes: [[String]]) -> Int? {
        last.append(name)
        if last.count > Self.max { last.removeFirst(last.count - Self.max) }
        for (i, c) in codes.enumerated() where !c.isEmpty && last.count >= c.count && Array(last.suffix(c.count)) == c {
            last = []
            return i
        }
        return nil
    }
}

/// A hidden theme and the code that turns it on (config.RetroTheme).
public struct RetroTheme: Decodable, Equatable, Sendable {
    public var theme: Theme
    public var code: [String]
    public var toast: String
}

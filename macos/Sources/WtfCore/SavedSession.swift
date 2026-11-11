// The session.json shapes every build writes, older versions (the Wails
// build) included (spec: file version 2 with per-window sessions, each
// version 1), so every build restores the others' windows. Decoding is lenient: a malformed part is skipped, not fatal.
import Foundation

public struct SavedNode: Codable, Equatable, Sendable {
    // Leaf
    public var cwd: String?
    public var gitIdentity: String?
    /// Only when moving between windows: the live shell and its scrollback.
    public var sessionId: String?
    public var buffer: String?
    /// An API pane's request (a terminal pane has none).
    public var api: Api.Request?
    // Split
    public var dir: String?
    public var ratio: Double?
    public var a: Box?
    public var b: Box?

    /// Recursion for Codable structs.
    public final class Box: Codable, Equatable, Sendable {
        public let node: SavedNode
        init(_ n: SavedNode) { node = n }
        public init(from decoder: Decoder) throws { node = try SavedNode(from: decoder) }
        public func encode(to encoder: Encoder) throws { try node.encode(to: encoder) }
        public static func == (l: Box, r: Box) -> Bool { l.node == r.node }
    }

    public init(cwd: String, gitIdentity: String? = nil, sessionId: String? = nil, buffer: String? = nil, api: Api.Request? = nil) {
        self.cwd = cwd
        self.api = api
        self.gitIdentity = gitIdentity
        self.sessionId = sessionId
        self.buffer = buffer
    }

    public init(dir: String, ratio: Double, a: SavedNode, b: SavedNode) {
        self.dir = dir
        self.ratio = ratio
        self.a = Box(a)
        self.b = Box(b)
    }

    enum CodingKeys: String, CodingKey { case cwd, gitIdentity, sessionId, buffer, api, dir, ratio, a, b }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        cwd = try? c.decodeIfPresent(String.self, forKey: .cwd)
        gitIdentity = try? c.decodeIfPresent(String.self, forKey: .gitIdentity)
        sessionId = try? c.decodeIfPresent(String.self, forKey: .sessionId)
        buffer = try? c.decodeIfPresent(String.self, forKey: .buffer)
        api = try? c.decodeIfPresent(Api.Request.self, forKey: .api)
        dir = try? c.decodeIfPresent(String.self, forKey: .dir)
        ratio = try? c.decodeIfPresent(Double.self, forKey: .ratio)
        a = try? c.decodeIfPresent(Box.self, forKey: .a)
        b = try? c.decodeIfPresent(Box.self, forKey: .b)
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        if let cwd {
            try c.encode(cwd, forKey: .cwd)
            try c.encodeIfPresent(gitIdentity, forKey: .gitIdentity)
            try c.encodeIfPresent(sessionId, forKey: .sessionId)
            try c.encodeIfPresent(buffer, forKey: .buffer)
            try c.encodeIfPresent(api, forKey: .api)
        } else {
            try c.encodeIfPresent(dir, forKey: .dir)
            try c.encodeIfPresent(ratio, forKey: .ratio)
            try c.encodeIfPresent(a, forKey: .a)
            try c.encodeIfPresent(b, forKey: .b)
        }
    }
}

extension SavedNode {
    var aNode: SavedNode? { a?.node }
    var bNode: SavedNode? { b?.node }
}

public struct SavedTab: Codable, Equatable, Sendable {
    public var groupId: Int?
    public var root: SavedNode?
    public var focused: Int?
    public var zoomed: Int?
    public var title: String?

    public init(groupId: Int?, root: SavedNode?, focused: Int?, zoomed: Int?, title: String?) {
        self.groupId = groupId
        self.root = root
        self.focused = focused
        self.zoomed = zoomed
        self.title = title
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        groupId = try? c.decodeIfPresent(Int.self, forKey: .groupId)
        root = try? c.decodeIfPresent(SavedNode.self, forKey: .root)
        focused = try? c.decodeIfPresent(Int.self, forKey: .focused)
        zoomed = try? c.decodeIfPresent(Int.self, forKey: .zoomed)
        title = try? c.decodeIfPresent(String.self, forKey: .title)
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(groupId, forKey: .groupId)
        try c.encode(root, forKey: .root)
        try c.encode(focused, forKey: .focused)
        try c.encode(zoomed, forKey: .zoomed)
        try c.encode(title, forKey: .title)
    }

    enum CodingKeys: String, CodingKey { case groupId, root, focused, zoomed, title }

    /// What moves a tab to another window (window:adopt / a new window's bootstrap).
    public struct Payload: Codable, Equatable, Sendable {
        public struct Group: Codable, Equatable, Sendable {
            public var name: String
            public var color: String
        }
        public var kind: String
        public var tab: SavedTab
        public var group: Group?
    }
}

public struct SavedSession: Codable, Sendable {
    public struct Group: Codable, Equatable, Sendable {
        public var id: Int?
        public var name: String?
        public var color: String?
        public var collapsed: Bool?

        public init(id: Int?, name: String?, color: String?, collapsed: Bool?) {
            self.id = id
            self.name = name
            self.color = color
            self.collapsed = collapsed
        }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            id = try? c.decodeIfPresent(Int.self, forKey: .id)
            name = try? c.decodeIfPresent(String.self, forKey: .name)
            color = try? c.decodeIfPresent(String.self, forKey: .color)
            collapsed = try? c.decodeIfPresent(Bool.self, forKey: .collapsed)
        }
    }

    public var version: Int?
    public var fontSize: Double?
    public var tabs: [SavedTab]?
    public var groups: [Group]?
    public var active: Int?
    /// Fields this build doesn't use (the sidebar state, and what older
    /// versions such as the Wails build saved),
    /// kept so saving doesn't drop them.
    public var extra: [String: JSONValue] = [:]

    public init(version: Int, fontSize: Double, tabs: [SavedTab], groups: [Group], active: Int, extra: [String: JSONValue]) {
        self.version = version
        self.fontSize = fontSize
        self.tabs = tabs
        self.groups = groups
        self.active = active
        self.extra = extra
    }

    struct Key: CodingKey {
        var stringValue: String
        var intValue: Int? { nil }
        init(_ s: String) { stringValue = s }
        init?(stringValue: String) { self.stringValue = stringValue }
        init?(intValue: Int) { nil }
    }

    static let known: Set<String> = ["version", "fontSize", "tabs", "groups", "active"]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: Key.self)
        version = try? c.decodeIfPresent(Int.self, forKey: Key("version"))
        fontSize = try? c.decodeIfPresent(Double.self, forKey: Key("fontSize"))
        // Skip malformed tabs and groups one by one.
        tabs = (try? c.decodeIfPresent([Lenient<SavedTab>].self, forKey: Key("tabs")))?.compactMap(\.value)
        groups = (try? c.decodeIfPresent([Lenient<Group>].self, forKey: Key("groups")))?.compactMap(\.value)
        active = try? c.decodeIfPresent(Int.self, forKey: Key("active"))
        for k in c.allKeys where !Self.known.contains(k.stringValue) {
            extra[k.stringValue] = try? c.decode(JSONValue.self, forKey: k)
        }
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: Key.self)
        for (k, v) in extra { try c.encode(v, forKey: Key(k)) }
        try c.encodeIfPresent(version, forKey: Key("version"))
        try c.encodeIfPresent(fontSize, forKey: Key("fontSize"))
        try c.encodeIfPresent(tabs, forKey: Key("tabs"))
        try c.encodeIfPresent(groups, forKey: Key("groups"))
        try c.encodeIfPresent(active, forKey: Key("active"))
    }
}

/// Decodes to nil instead of failing.
struct Lenient<T: Decodable>: Decodable {
    let value: T?
    init(from decoder: Decoder) throws { value = try? T(from: decoder) }
}

/// Any JSON value, for fields passed through untouched.
public enum JSONValue: Codable, Equatable, Sendable {
    case null, bool(Bool), number(Double), string(String), array([JSONValue]), object([String: JSONValue])

    public init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() { self = .null }
        else if let b = try? c.decode(Bool.self) { self = .bool(b) }
        else if let n = try? c.decode(Double.self) { self = .number(n) }
        else if let s = try? c.decode(String.self) { self = .string(s) }
        else if let a = try? c.decode([JSONValue].self) { self = .array(a) }
        else { self = .object(try c.decode([String: JSONValue].self)) }
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        switch self {
        case .null: try c.encodeNil()
        case .bool(let b): try c.encode(b)
        case .number(let n): try c.encode(n)
        case .string(let s): try c.encode(s)
        case .array(let a): try c.encode(a)
        case .object(let o): try c.encode(o)
        }
    }
}

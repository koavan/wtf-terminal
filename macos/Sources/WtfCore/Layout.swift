// A tab's panes as a split tree, like internal/uicore/layout.go.
import Foundation

public enum SplitDir: String, Codable, Sendable { case row, column }

/// A pane leaf, or a split of two subtrees: `row` puts `a` left of `b`,
/// `column` puts `a` above `b`; `ratio` is the share `a` takes.
public indirect enum Layout: Equatable, Sendable {
    case pane(Int)
    case split(SplitDir, Double, Layout, Layout)

    public static let minRatio = 0.1

    public static func clamp(_ r: Double) -> Double { min(1 - minRatio, max(minRatio, r)) }

    /// Pane ids in reading order: depth first, `a` before `b`.
    public var leaves: [Int] {
        switch self {
        case .pane(let id): [id]
        case .split(_, _, let a, let b): a.leaves + b.leaves
        }
    }

    public var isSplit: Bool { if case .split = self { true } else { false } }

    /// Replaces the leaf `id` with a split of it and `new` (new goes right or below).
    public func splitting(_ id: Int, _ dir: SplitDir, new: Int) -> Layout {
        switch self {
        case .pane(let p): p == id ? .split(dir, 0.5, .pane(p), .pane(new)) : self
        case .split(let d, let r, let a, let b): .split(d, r, a.splitting(id, dir, new: new), b.splitting(id, dir, new: new))
        }
    }

    /// The tree without leaf `id`: its sibling takes the parent's place. Nil
    /// when that was the only leaf.
    public func removing(_ id: Int) -> Layout? {
        switch self {
        case .pane(let p): return p == id ? nil : self
        case .split(let d, let r, let a, let b):
            let na = a.removing(id), nb = b.removing(id)
            if let na, let nb { return .split(d, r, na, nb) }
            return na ?? nb
        }
    }

    /// Unit-square rectangles of every leaf.
    public func frames(_ rect: UnitRect = UnitRect(x: 0, y: 0, w: 1, h: 1)) -> [Int: UnitRect] {
        switch self {
        case .pane(let id): return [id: rect]
        case .split(let d, let r, let a, let b):
            let (ra, rb) = rect.divided(d, r)
            return a.frames(ra).merging(b.frames(rb)) { x, _ in x }
        }
    }

    /// Every split with its rectangle, depth first, for drawing dividers. The
    /// path addresses the split for `settingRatio`.
    public func splits(_ rect: UnitRect = UnitRect(x: 0, y: 0, w: 1, h: 1), path: [Bool] = []) -> [(path: [Bool], dir: SplitDir, ratio: Double, rect: UnitRect)] {
        guard case .split(let d, let r, let a, let b) = self else { return [] }
        let (ra, rb) = rect.divided(d, r)
        return [(path, d, r, rect)] + a.splits(ra, path: path + [false]) + b.splits(rb, path: path + [true])
    }

    /// The tree with the ratio of the split at `path` set (clamped).
    public func settingRatio(at path: [Bool], _ ratio: Double) -> Layout {
        guard case .split(let d, let r, let a, let b) = self else { return self }
        guard let first = path.first else { return .split(d, Self.clamp(ratio), a, b) }
        let rest = Array(path.dropFirst())
        return first ? .split(d, r, a, b.settingRatio(at: rest, ratio)) : .split(d, r, a.settingRatio(at: rest, ratio), b)
    }

    /// The nearest pane from `id` in a direction (layout.ts neighbour rule);
    /// nil when there's none (no wrapping).
    public func neighbour(of id: Int, _ dir: FocusDir) -> Int? {
        let f = frames()
        guard let me = f[id] else { return nil }
        var best: (id: Int, dist: Double, overlap: Double)?
        for (other, r) in f where other != id {
            let dist: Double, overlap: Double
            switch dir {
            case .left: dist = me.x - (r.x + r.w); overlap = min(me.y + me.h, r.y + r.h) - max(me.y, r.y)
            case .right: dist = r.x - (me.x + me.w); overlap = min(me.y + me.h, r.y + r.h) - max(me.y, r.y)
            case .up: dist = me.y - (r.y + r.h); overlap = min(me.x + me.w, r.x + r.w) - max(me.x, r.x)
            case .down: dist = r.y - (me.y + me.h); overlap = min(me.x + me.w, r.x + r.w) - max(me.x, r.x)
            }
            guard dist >= -1e-6, overlap > 1e-6 else { continue }
            if let b = best {
                if dist < b.dist - 1e-6 || (abs(dist - b.dist) <= 1e-6 && overlap > b.overlap) { best = (other, dist, overlap) }
            } else {
                best = (other, dist, overlap)
            }
        }
        return best?.id
    }
}

public enum FocusDir: Sendable { case left, right, up, down }

public struct UnitRect: Equatable, Sendable {
    public var x, y, w, h: Double

    public init(x: Double, y: Double, w: Double, h: Double) {
        self.x = x
        self.y = y
        self.w = w
        self.h = h
    }

    func divided(_ d: SplitDir, _ r: Double) -> (UnitRect, UnitRect) {
        switch d {
        case .row: (UnitRect(x: x, y: y, w: w * r, h: h), UnitRect(x: x + w * r, y: y, w: w * (1 - r), h: h))
        case .column: (UnitRect(x: x, y: y, w: w, h: h * r), UnitRect(x: x, y: y + h * r, w: w, h: h * (1 - r)))
        }
    }
}

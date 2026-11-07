// One window's tabs, panes and groups, and the rules that keep them
// consistent. A port of frontend/src/lib/tabs.svelte.ts; the views observe
// it through `onChange`. No AppKit here, so it's tested on its own.
import Foundation

/// What a pane shows: a shell, or the API tab's request editor.
public enum PaneKind: Sendable {
    case terminal, api
}

public final class Pane {
    public let id: Int
    public let kind: PaneKind
    /// An API pane's request, kept up to date by its view (saved with the session).
    public var api: Api.Request?
    /// Backend session ("s<N>"), nil until the shell is started.
    public var sessionId: String?
    public var initialCwd: String
    /// Latest known folder of the foreground process (terminal:info).
    public var cwd: String
    public var process = ""
    public var isShell = true
    /// Title set by the running program (OSC 0/2).
    public var oscTitle = ""
    /// Shell integration seen (a prompt mark arrived).
    public var integrated = false
    public var running = false
    public var command = ""
    public var lastCommand: (text: String, exitCode: Int, durationMs: Double)?
    /// The variables the shell reported at its last prompt.
    public var env: [String: String]?
    /// Its cloud and cluster context, resolved from `env` (address bar badges).
    public var infra: InfraContext?
    /// Typed into the shell once, after it starts.
    public var initialCommand: String?
    /// Git identity to switch to once, at start (before initialCommand).
    public var gitIdentity: String?
    /// Scrollback brought from another window, written once.
    public var restoreBuffer: String?

    init(id: Int, cwd: String, kind: PaneKind = .terminal) {
        self.id = id
        self.kind = kind
        initialCwd = cwd
        self.cwd = cwd
    }

    /// A program other than the shell runs, or a command line does.
    public var busy: Bool { running || (!isShell && !process.isEmpty) }

    /// The git identity this pane uses: what the shell reported, else the one pending.
    public var identity: String? {
        let id = env.map { $0["WTF_GIT_IDENTITY"] ?? "" } ?? gitIdentity ?? ""
        return id.isEmpty ? nil : id
    }
}

public struct Tab: Equatable {
    public let id: Int
    public var root: Layout
    public var focusedPaneId: Int
    public var zoomedPaneId: Int?
    /// Output arrived while in the background.
    public var activity = false
    /// A command finished while in the background: true for success.
    public var finished: Bool?
    public var groupId: Int?
    public var customTitle: String?
}

public enum GroupColor: String, CaseIterable, Codable, Sendable {
    case blue, red, yellow, green, pink, purple, cyan, orange, grey

    /// The colour in the dark and light themes.
    public func hex(dark: Bool) -> String {
        switch self {
        case .grey: dark ? "#a9adb8" : "#5f6368"
        case .blue: dark ? "#8ab4f8" : "#1a73e8"
        case .red: dark ? "#f28b82" : "#d93025"
        case .yellow: dark ? "#fdd663" : "#c98a00"
        case .green: dark ? "#81c995" : "#188038"
        case .pink: dark ? "#ff8bcb" : "#d01884"
        case .purple: dark ? "#c58af9" : "#a142f4"
        case .cyan: dark ? "#78d9ec" : "#007b83"
        case .orange: dark ? "#fcad70" : "#e8710a"
        }
    }
}

public struct TabGroup: Equatable {
    public let id: Int
    public var name: String
    public var color: GroupColor
    public var collapsed: Bool
}

public final class WindowModel {
    public static let maxTitle = 60
    static let maxReopen = 20

    public private(set) var tabs: [Tab] = []
    public private(set) var groups: [TabGroup] = []
    public private(set) var activeId: Int?
    public private(set) var panes: [Int: Pane] = [:]
    /// Closed tabs' focused panes, to reopen: a folder, or an API request.
    private var recentlyClosed: [(cwd: String, api: Api.Request?)] = []
    private var nextTab = 1, nextPane = 1, nextGroup = 1

    /// The last tab went: the window closes.
    public var onEmpty: (() -> Void)?
    /// Something changed: redraw, and save the session.
    public var onChange: (() -> Void)?

    public init() {}

    private func changed() { onChange?() }

    // MARK: Lookups

    public func tab(_ id: Int) -> Tab? { tabs.first { $0.id == id } }
    public var active: Tab? { activeId.flatMap(tab) }
    public func index(_ id: Int) -> Int? { tabs.firstIndex { $0.id == id } }
    public func group(_ id: Int) -> TabGroup? { groups.first { $0.id == id } }
    public func tabsIn(_ group: Int) -> [Tab] { tabs.filter { $0.groupId == group } }
    public func isHidden(_ t: Tab) -> Bool { t.groupId.flatMap(group)?.collapsed ?? false }
    public var visible: [Tab] { tabs.filter { !isHidden($0) } }
    public func focusedPane(_ t: Tab) -> Pane? { panes[t.focusedPaneId] }
    public var activePane: Pane? { active.flatMap(focusedPane) }
    public func tabOf(pane: Int) -> Tab? { tabs.first { $0.root.leaves.contains(pane) } }
    public func pane(session: String) -> Pane? { panes.values.first { $0.sessionId == session } }

    private func update(_ id: Int, _ f: (inout Tab) -> Void) {
        if let i = index(id) { f(&tabs[i]) }
    }

    // MARK: Titles

    /// "~" for home and below it: the way paths are shown.
    public static func prettyPath(_ path: String, home: String = NSHomeDirectory()) -> String {
        if path == home { return "~" }
        if path.hasPrefix(home + "/") { return "~" + path.dropFirst(home.count) }
        return path
    }

    /// What a pane is called: the running program, or the folder of an idle shell.
    public static func paneTitle(_ p: Pane, home: String = NSHomeDirectory()) -> String {
        if p.kind == .api { return Api.title(p.api) }
        if !p.isShell, !p.process.isEmpty {
            if !p.oscTitle.isEmpty { return p.oscTitle }
            if p.process == "ssh", p.running, p.command.hasPrefix("ssh ") { return p.command }
            return p.process
        }
        let path = prettyPath(p.cwd, home: home)
        if path.isEmpty { return "Terminal" }
        if path == "~" || path == "/" { return path }
        return String(path.split(separator: "/").last ?? Substring(path))
    }

    public func title(_ t: Tab) -> String {
        t.customTitle ?? focusedPane(t).map { Self.paneTitle($0) } ?? "Terminal"
    }

    /// Whitespace collapsed, trimmed, at most 60 characters; empty → nil (automatic).
    public static func cleanTitle(_ s: String?) -> String? {
        guard let s else { return nil }
        let t = s.split(whereSeparator: \.isWhitespace).joined(separator: " ")
        return t.isEmpty ? nil : String(t.prefix(maxTitle))
    }

    public func rename(_ id: Int, _ draft: String) {
        guard let t = tab(id) else { return }
        let trimmed = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        // Typing the automatic name back keeps the tab automatic.
        if t.customTitle == nil, trimmed == title(t) { return }
        update(id) { $0.customTitle = Self.cleanTitle(draft) }
        changed()
    }

    // MARK: Tabs

    private func newPane(cwd: String, kind: PaneKind = .terminal) -> Pane {
        let p = Pane(id: nextPane, cwd: cwd, kind: kind)
        nextPane += 1
        panes[p.id] = p
        return p
    }

    /// Opens a one-pane tab and activates it. `cwd` nil: the active pane's folder.
    @discardableResult
    public func open(cwd: String? = nil, at index: Int? = nil, group: Int? = nil,
                     command: String? = nil, gitIdentity: String? = nil) -> Int {
        let p = newPane(cwd: cwd ?? activePane?.cwd ?? "")
        p.initialCommand = command
        p.gitIdentity = gitIdentity
        return insert(p, at: index, group: group)
    }

    /// Opens an API tab and activates it.
    @discardableResult
    public func openApi(_ request: Api.Request = Api.blank, at index: Int? = nil, group: Int? = nil) -> Int {
        let p = newPane(cwd: activePane?.cwd ?? "", kind: .api)
        p.api = request
        return insert(p, at: index, group: group)
    }

    private func insert(_ p: Pane, at index: Int?, group: Int?) -> Int {
        let t = Tab(id: nextTab, root: .pane(p.id), focusedPaneId: p.id, groupId: group)
        nextTab += 1
        tabs.insert(t, at: min(max(0, index ?? tabs.count), tabs.count))
        activate(t.id, notify: false)
        changed()
        return t.id
    }

    /// A new tab right after `id`, in its group: a copy of its folder and
    /// identity when duplicating, else in the active pane's folder.
    @discardableResult
    public func openNextTo(_ id: Int, duplicate: Bool = false) -> Int? {
        guard let i = index(id) else { return nil }
        let t = tabs[i]
        let src = focusedPane(t)
        if duplicate, let src, src.kind == .api {
            return openApi(src.api ?? Api.blank, at: i + 1, group: t.groupId)
        }
        return open(cwd: duplicate ? src?.cwd : nil, at: i + 1, group: t.groupId,
                    gitIdentity: duplicate ? src?.identity : nil)
    }

    @discardableResult
    public func duplicate() -> Int? { activeId.flatMap { openNextTo($0, duplicate: true) } }

    /// The nearest visible tab to an index, looking right first.
    private func nearestVisible(_ i: Int) -> Int? {
        guard !tabs.isEmpty else { return nil }
        let start = min(i, tabs.count - 1)
        for d in 0...tabs.count {
            for j in d == 0 ? [start] : [start + d, start - d] where j >= 0 && j < tabs.count {
                if !isHidden(tabs[j]) { return tabs[j].id }
            }
        }
        return nil
    }

    public func close(_ id: Int) { closeMany([id]) }

    public func closeMany(_ ids: [Int]) {
        let closing = Set(ids)
        let activeIndex = activeId.flatMap(index) ?? 0
        let wasActive = activeId.map(closing.contains) ?? false
        for t in tabs where closing.contains(t.id) {
            if let p = focusedPane(t) {
                recentlyClosed.append((p.cwd, p.kind == .api ? p.api ?? Api.blank : nil))
                if recentlyClosed.count > Self.maxReopen { recentlyClosed.removeFirst() }
            }
            t.root.leaves.forEach { panes[$0] = nil }
        }
        tabs.removeAll { closing.contains($0.id) }
        prune()
        if tabs.isEmpty {
            activeId = nil
            changed()
            onEmpty?()
            return
        }
        if wasActive {
            if let next = nearestVisible(min(activeIndex, tabs.count - 1)) {
                activate(next, notify: false)
            } else {
                open(cwd: "")
            }
        }
        changed()
    }

    public func reopen() {
        guard let last = recentlyClosed.popLast() else { return }
        if let api = last.api { openApi(api) } else { open(cwd: last.cwd) }
    }

    public func activate(_ id: Int) { activate(id, notify: true) }

    private func activate(_ id: Int, notify: Bool) {
        guard let t = tab(id) else { return }
        if let g = t.groupId, let gi = groups.firstIndex(where: { $0.id == g }) { groups[gi].collapsed = false }
        activeId = id
        update(id) {
            $0.activity = false
            $0.finished = nil
        }
        if notify { changed() }
    }

    /// The next or previous visible tab, wrapping.
    public func cycle(_ step: Int) {
        let v = visible
        guard !v.isEmpty else { return }
        let i = v.firstIndex { $0.id == activeId } ?? 0
        activate(v[((i + step) % v.count + v.count) % v.count].id)
    }

    /// ⌘1…⌘8: the n-th visible tab (1-based); 9: the last.
    public func selectNumber(_ n: Int) {
        let v = visible
        if n == 9 { if let last = v.last { activate(last.id) }; return }
        if n >= 1, n <= v.count { activate(v[n - 1].id) }
    }

    public func selectLast() { selectNumber(9) }

    // MARK: Order and groups

    /// Moves a tab before another (nil: to the end), then settles its group.
    public func moveBefore(_ id: Int, _ before: Int?) {
        guard let i = index(id), id != before else { return }
        let t = tabs.remove(at: i)
        let j = before.flatMap(index) ?? tabs.count
        tabs.insert(t, at: j)
        settle(id)
        prune()
        changed()
    }

    /// A moved tab joins the open group around it, stays in its own group
    /// next to a member or when it's the group's only tab, else leaves it.
    private func settle(_ id: Int) {
        guard let i = index(id) else { return }
        func openGroup(_ j: Int) -> Int? {
            guard j >= 0, j < tabs.count, let g = tabs[j].groupId, group(g)?.collapsed == false else { return nil }
            return g
        }
        let left = openGroup(i - 1), right = openGroup(i + 1)
        let mine = tabs[i].groupId
        if let left, left == right {
            tabs[i].groupId = left
        } else if let mine, left == mine || right == mine {
            return
        } else if let mine, tabsIn(mine).count == 1 {
            return
        } else {
            tabs[i].groupId = nil
        }
    }

    /// ⌘⇧← / ⌘⇧→: one slot among the visible tabs.
    public func moveActive(_ step: Int) {
        guard let id = activeId else { return }
        let v = visible
        guard let i = v.firstIndex(where: { $0.id == id }) else { return }
        let j = i + step
        guard j >= 0, j < v.count else { return }
        if step < 0 {
            moveBefore(id, v[j].id)
        } else {
            moveBefore(id, j + 1 < v.count ? v[j + 1].id : nil)
        }
    }

    /// Drops groups without tabs, and keeps the active tab visible.
    private func prune() {
        let used = Set(tabs.compactMap(\.groupId))
        groups.removeAll { !used.contains($0.id) }
    }

    /// Moves a tab to just after a group's last member (not counting itself).
    private func moveAfterGroup(_ id: Int, _ g: Int) {
        guard let i = index(id) else { return }
        let t = tabs.remove(at: i)
        let last = tabs.lastIndex { $0.groupId == g }
        tabs.insert(t, at: last.map { $0 + 1 } ?? min(i, tabs.count))
    }

    /// "Add Tab to New Group": the first unused colour.
    @discardableResult
    public func createGroup(_ id: Int) -> Int? {
        guard let t = tab(id) else { return nil }
        let used = Set(groups.map(\.color))
        let color = GroupColor.allCases.first { !used.contains($0) } ?? GroupColor.allCases[groups.count % 9]
        if let old = t.groupId { moveAfterGroup(id, old) }
        let g = TabGroup(id: nextGroup, name: "", color: color, collapsed: false)
        nextGroup += 1
        groups.append(g)
        update(id) { $0.groupId = g.id }
        prune()
        changed()
        return g.id
    }

    public func addToGroup(_ id: Int, _ g: Int) {
        guard tab(id) != nil, group(g) != nil, tab(id)?.groupId != g else { return }
        moveAfterGroup(id, g)
        update(id) { $0.groupId = g }
        if activeId == id, let gi = groups.firstIndex(where: { $0.id == g }) { groups[gi].collapsed = false }
        prune()
        changed()
    }

    public func removeFromGroup(_ id: Int) {
        guard let g = tab(id)?.groupId else { return }
        moveAfterGroup(id, g)
        update(id) { $0.groupId = nil }
        prune()
        changed()
    }

    public func ungroup(_ g: Int) {
        for i in tabs.indices where tabs[i].groupId == g { tabs[i].groupId = nil }
        prune()
        changed()
    }

    public func closeGroup(_ g: Int) { closeMany(tabsIn(g).map(\.id)) }

    public func setGroup(_ g: Int, name: String? = nil, color: GroupColor? = nil) {
        guard let gi = groups.firstIndex(where: { $0.id == g }) else { return }
        if let name { groups[gi].name = name }
        if let color { groups[gi].color = color }
        changed()
    }

    public func toggleCollapsed(_ g: Int) {
        guard let gi = groups.firstIndex(where: { $0.id == g }) else { return }
        groups[gi].collapsed.toggle()
        if groups[gi].collapsed, let a = active, a.groupId == g {
            let at = index(a.id) ?? 0
            if let next = nearestVisible(at) { activate(next, notify: false) } else { open() }
        }
        changed()
    }

    @discardableResult
    public func newTabInGroup(_ g: Int) -> Int? {
        guard let last = tabs.lastIndex(where: { $0.groupId == g }) else { return nil }
        return open(at: last + 1, group: g)
    }

    // MARK: Panes

    /// Splits the active tab's focused pane; the new pane gets focus.
    @discardableResult
    public func split(_ dir: SplitDir, cwd: String? = nil, command: String? = nil) -> Int? {
        guard let t = active, let src = focusedPane(t) else { return nil }
        let p = newPane(cwd: cwd ?? src.cwd)
        p.gitIdentity = src.identity
        p.initialCommand = command
        update(t.id) {
            $0.root = $0.root.splitting(src.id, dir, new: p.id)
            $0.zoomedPaneId = nil
            $0.focusedPaneId = p.id
        }
        changed()
        return p.id
    }

    public func focusPane(_ id: Int) {
        guard let t = tabOf(pane: id), t.focusedPaneId != id else { return }
        update(t.id) { $0.focusedPaneId = id }
        changed()
    }

    public func focusDirection(_ dir: FocusDir) {
        guard let t = active, t.zoomedPaneId == nil, let n = t.root.neighbour(of: t.focusedPaneId, dir) else { return }
        focusPane(n)
    }

    public func cyclePane(_ step: Int) {
        guard let t = active, t.zoomedPaneId == nil else { return }
        let l = t.root.leaves
        guard l.count > 1, let i = l.firstIndex(of: t.focusedPaneId) else { return }
        focusPane(l[((i + step) % l.count + l.count) % l.count])
    }

    public func toggleZoom() {
        guard let t = active else { return }
        if t.zoomedPaneId != nil {
            update(t.id) { $0.zoomedPaneId = nil }
        } else if t.root.isSplit {
            update(t.id) { $0.zoomedPaneId = $0.focusedPaneId }
        } else {
            return
        }
        changed()
    }

    /// Each split takes its share of the panes it holds, so N panes get 1/N each.
    public func evenOut(_ id: Int) {
        func even(_ l: Layout) -> Layout {
            guard case .split(let d, _, let a, let b) = l else { return l }
            return .split(d, Double(a.leaves.count) / Double(l.leaves.count), even(a), even(b))
        }
        update(id) { $0.root = even($0.root) }
        changed()
    }

    public func setRatio(tab id: Int, path: [Bool], _ ratio: Double) {
        update(id) { $0.root = $0.root.settingRatio(at: path, ratio) }
        changed()
    }

    /// Removes a pane; the tab goes with its last pane.
    public func closePane(_ id: Int) {
        guard let t = tabOf(pane: id) else { return }
        guard let root = t.root.removing(id) else {
            closeMany([t.id])
            return
        }
        let before = t.root.leaves
        panes[id] = nil
        update(t.id) { tab in
            tab.root = root
            if tab.zoomedPaneId == id { tab.zoomedPaneId = nil }
            if tab.focusedPaneId == id {
                let left = root.leaves
                let i = before.firstIndex(of: id) ?? 0
                tab.focusedPaneId = before[..<i].last(where: left.contains) ?? left[0]
            }
        }
        changed()
    }

    public func closeActivePane() {
        if let id = active?.focusedPaneId { closePane(id) }
    }

    /// A shell exited: its pane closes, without asking.
    public func sessionExited(_ session: String) {
        if let p = pane(session: session) { closePane(p.id) }
    }

    /// terminal:info for one of this window's panes.
    public func applyInfo(session: String, process: String, cwd: String, isShell: Bool) {
        guard let p = pane(session: session) else { return }
        let name = process.hasPrefix("-") ? String(process.dropFirst()) : process
        if name != p.process { p.oscTitle = "" }
        p.process = name
        p.isShell = isShell
        if !cwd.isEmpty { p.cwd = cwd }
        changed()
    }

    /// Output arrived for a pane: marks its tab when it's in the background.
    public func markActivity(pane id: Int) {
        guard let t = tabOf(pane: id), t.id != activeId, !t.activity else { return }
        update(t.id) { $0.activity = true }
        changed()
    }

    public func markFinished(pane id: Int, ok: Bool) {
        guard let t = tabOf(pane: id), t.id != activeId else { return }
        update(t.id) { $0.finished = ok }
        changed()
    }

    // MARK: Moving between windows

    /// Takes a tab out of this window, its shells still running, for another
    /// window to adopt. `buffers` gives each pane's scrollback.
    public func detach(_ id: Int, buffers: (Pane) -> String) -> SavedTab.Payload? {
        guard let i = index(id) else { return nil }
        let t = tabs[i]
        let g = t.groupId.flatMap(group)
        let saved = save(t, live: true, buffers: buffers)
        let activeIndex = activeId.flatMap(index) ?? 0
        t.root.leaves.forEach { panes[$0] = nil }
        tabs.remove(at: i)
        prune()
        if tabs.isEmpty {
            activeId = nil
            changed()
            onEmpty?()
        } else {
            if activeId == id, let next = nearestVisible(min(activeIndex, tabs.count - 1)) { activate(next, notify: false) }
            changed()
        }
        return SavedTab.Payload(kind: "tab", tab: saved, group: g.map { .init(name: $0.name, color: $0.color.rawValue) })
    }

    /// Takes one pane out of its tab, as a one-pane tab with its live shell,
    /// for another window to adopt (parking an agent in the background
    /// window, #34). A tab's only pane takes the whole tab, without its
    /// group. As uicore's DetachPane.
    public func detachPane(_ id: Int, buffers: (Pane) -> String) -> SavedTab.Payload? {
        guard let t = tabOf(pane: id), panes[id] != nil else { return nil }
        if t.root.leaves.count == 1 {
            guard var payload = detach(t.id, buffers: buffers) else { return nil }
            payload.group = nil
            payload.tab.groupId = nil
            return payload
        }
        let saved = save(Tab(id: 0, root: .pane(id), focusedPaneId: id), live: true, buffers: buffers)
        closePane(id)
        return SavedTab.Payload(kind: "tab", tab: saved, group: nil)
    }

    /// Takes in a tab another window detached. False for a malformed payload.
    @discardableResult
    public func adopt(_ payload: SavedTab.Payload) -> Bool {
        guard payload.kind == "tab", let root = load(payload.tab.root, depth: 0, live: true) else { return false }
        var gid: Int?
        if let pg = payload.group {
            let color = GroupColor(rawValue: pg.color) ?? .blue
            let name = String(pg.name.prefix(100))
            if !name.isEmpty, let g = groups.first(where: { $0.name == name && $0.color == color }) {
                gid = g.id
            } else {
                let g = TabGroup(id: nextGroup, name: name, color: color, collapsed: false)
                nextGroup += 1
                groups.append(g)
                gid = g.id
            }
        }
        let leaves = root.leaves
        let focused = payload.tab.focused.flatMap { $0 >= 0 && $0 < leaves.count ? leaves[$0] : nil } ?? leaves[0]
        let t = Tab(id: nextTab, root: root, focusedPaneId: focused, groupId: gid,
                    customTitle: Self.cleanTitle(payload.tab.title))
        nextTab += 1
        let at = gid.flatMap { g in tabs.lastIndex { $0.groupId == g }.map { $0 + 1 } } ?? tabs.count
        tabs.insert(t, at: at)
        activate(t.id, notify: false)
        changed()
        return true
    }

    // MARK: Session

    public func snapshot(fontSize: Double, extra: [String: JSONValue] = [:]) -> SavedSession {
        SavedSession(
            version: 1, fontSize: fontSize,
            tabs: tabs.map { save($0, live: false, buffers: { _ in "" }) },
            groups: groups.map { .init(id: $0.id, name: $0.name, color: $0.color.rawValue, collapsed: $0.collapsed) },
            active: activeId.flatMap(index) ?? 0, extra: extra)
    }

    private func save(_ t: Tab, live: Bool, buffers: (Pane) -> String) -> SavedTab {
        func node(_ l: Layout) -> SavedNode {
            switch l {
            case .pane(let id):
                let p = panes[id]
                if let p, p.kind == .api {
                    // Credentials stay in memory: moving windows keeps them, saving doesn't.
                    let r = p.api ?? Api.blank
                    return SavedNode(cwd: p.cwd, api: live ? r : Api.forSaving(r))
                }
                return SavedNode(cwd: p?.cwd ?? "", gitIdentity: live ? nil : p?.identity,
                                 sessionId: live ? p?.sessionId : nil, buffer: live ? p.map(buffers) : nil)
            case .split(let d, let r, let a, let b):
                return SavedNode(dir: d.rawValue, ratio: r, a: node(a), b: node(b))
            }
        }
        let leaves = t.root.leaves
        return SavedTab(groupId: t.groupId, root: node(t.root), focused: leaves.firstIndex(of: t.focusedPaneId) ?? 0,
                        zoomed: t.zoomedPaneId.flatMap(leaves.firstIndex), title: t.customTitle)
    }

    /// A saved tree, made of new panes. Nil when unusable. Only a tab moving
    /// between windows (`live`) brings its running shells and scrollback.
    private func load(_ n: SavedNode?, depth: Int, live: Bool = false) -> Layout? {
        guard let n, depth <= 16 else { return nil }
        if let cwd = n.cwd, let api = n.api {
            let p = newPane(cwd: cwd, kind: .api)
            p.api = api
            return .pane(p.id)
        }
        if let cwd = n.cwd {
            let p = newPane(cwd: cwd)
            if let g = n.gitIdentity, !g.isEmpty { p.gitIdentity = String(g.prefix(64)) }
            if live {
                p.sessionId = n.sessionId
                p.restoreBuffer = n.buffer
            }
            return .pane(p.id)
        }
        guard let dir = n.dir.flatMap(SplitDir.init(rawValue:)) else { return nil }
        let a = load(n.aNode, depth: depth + 1, live: live), b = load(n.bNode, depth: depth + 1, live: live)
        guard let a, let b else { return a ?? b }
        let r = n.ratio.flatMap { $0.isFinite ? Layout.clamp($0) : nil } ?? 0.5
        return .split(dir, r, a, b)
    }

    /// Restores a saved window. False when nothing usable is in it.
    @discardableResult
    public func restore(_ s: SavedSession) -> Bool {
        var groupIds: [Int: Int] = [:]
        var newGroups: [TabGroup] = []
        for g in s.groups ?? [] {
            guard let old = g.id, groupIds[old] == nil else { continue }
            let ng = TabGroup(id: nextGroup, name: String((g.name ?? "").prefix(100)),
                              color: g.color.flatMap(GroupColor.init(rawValue:)) ?? .blue, collapsed: g.collapsed == true)
            nextGroup += 1
            groupIds[old] = ng.id
            newGroups.append(ng)
        }
        var parsed: [Tab] = []
        for st in s.tabs ?? [] {
            guard let root = load(st.root, depth: 0) else { continue }
            let leaves = root.leaves
            let focused = st.focused.flatMap { $0 >= 0 && $0 < leaves.count ? leaves[$0] : nil } ?? leaves[0]
            let zoomed = root.isSplit ? st.zoomed.flatMap { $0 >= 0 && $0 < leaves.count ? leaves[$0] : nil } : nil
            parsed.append(Tab(id: nextTab, root: root, focusedPaneId: focused, zoomedPaneId: zoomed,
                              groupId: st.groupId.flatMap { groupIds[$0] }, customTitle: Self.cleanTitle(st.title)))
            nextTab += 1
        }
        guard !parsed.isEmpty else { return false }
        // Members of a group come together where its first member is.
        var ordered: [Tab] = []
        var placed = Set<Int>()
        for t in parsed where !placed.contains(t.id) {
            if let g = t.groupId {
                for m in parsed where m.groupId == g { ordered.append(m); placed.insert(m.id) }
            } else {
                ordered.append(t)
                placed.insert(t.id)
            }
        }
        tabs = ordered
        groups = newGroups
        prune()
        let a = s.active.flatMap { $0 >= 0 && $0 < parsed.count ? parsed[$0].id : nil } ?? tabs[0].id
        activate(a, notify: false)
        changed()
        return true
    }
}

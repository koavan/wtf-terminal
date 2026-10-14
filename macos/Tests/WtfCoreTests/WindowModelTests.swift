import Foundation
import Testing
@testable import WtfCore

// The cases of frontend/src/lib/tabs.test.ts, and its random-operations check.

private final class Setup {
    let w = WindowModel()
    var emptied = 0
    init() { w.onEmpty = { [unowned self] in emptied += 1 } }

    /// Opens n tabs (in /t0, /t1, …) and returns their ids in order.
    @discardableResult
    func open(_ n: Int) -> [Int] {
        for i in 0..<n { w.open(cwd: "/t\(i)") }
        return w.tabs.map(\.id)
    }

    var ids: [Int] { w.tabs.map(\.id) }
    func groupOf(_ id: Int) -> Int? { w.tab(id)?.groupId }
    var paneIds: [Int] { w.active!.root.leaves }
}

/// The rules every operation must keep.
private func checkInvariants(_ w: WindowModel, _ note: @autoclosure () -> String = "") {
    var seen = Set<Int>()
    var prev: Int?
    for t in w.tabs {
        if let g = t.groupId, g != prev {
            #expect(!seen.contains(g), "group \(g) is split \(note())")
            seen.insert(g)
        }
        prev = t.groupId
    }
    #expect(Set(w.groups.map(\.id)) == Set(w.tabs.compactMap(\.groupId)), "groups exist exactly when used \(note())")
    for t in w.tabs {
        #expect(t.root.leaves.contains(t.focusedPaneId))
        if let z = t.zoomedPaneId { #expect(t.root.leaves.contains(z)) }
        for p in t.root.leaves { #expect(w.panes[p] != nil) }
    }
    if !w.tabs.isEmpty {
        #expect(w.active != nil, "active tab exists \(note())")
        if let a = w.active { #expect(!w.isHidden(a), "active tab is visible \(note())") }
    }
    #expect(w.panes.count == w.tabs.reduce(0) { $0 + $1.root.leaves.count }, "no stray panes \(note())")
}

@Test func createsGroupsWithTheFirstUnusedColour() {
    let s = Setup()
    let t = s.open(2)
    let g1 = s.w.createGroup(t[0])!, g2 = s.w.createGroup(t[1])!
    #expect(s.w.group(g1)?.color == .blue && s.w.group(g2)?.color == .red)
    #expect(s.groupOf(t[0]) == g1)
    checkInvariants(s.w)
}

@Test func addingToAGroupMovesTheTabNextToItsMembers() {
    let s = Setup()
    let t = s.open(3)
    let g = s.w.createGroup(t[0])!
    s.w.addToGroup(t[2], g)
    #expect(s.ids == [t[0], t[2], t[1]])
    #expect(s.groupOf(t[2]) == g)
}

@Test func removingFromTheMiddleStepsOutAfterTheGroup() {
    let s = Setup()
    let t = s.open(4)
    let g = s.w.createGroup(t[0])!
    s.w.addToGroup(t[1], g)
    s.w.addToGroup(t[2], g)
    s.w.removeFromGroup(t[1])
    #expect(s.ids == [t[0], t[2], t[1], t[3]])
    #expect(s.groupOf(t[1]) == nil)
    checkInvariants(s.w)
}

@Test func draggingBetweenMembersJoinsAndAwayLeaves() {
    let s = Setup()
    let t = s.open(4)
    let g = s.w.createGroup(t[0])!
    s.w.addToGroup(t[1], g)
    s.w.moveBefore(t[3], t[1])
    #expect(s.groupOf(t[3]) == g)
    s.w.moveBefore(t[3], nil)
    #expect(s.groupOf(t[3]) == nil)
    checkInvariants(s.w)
}

@Test func aOneTabGroupTravelsWithItsTab() {
    let s = Setup()
    let t = s.open(3)
    let g = s.w.createGroup(t[0])!
    s.w.moveBefore(t[0], nil)
    #expect(s.ids.last == t[0] && s.groupOf(t[0]) == g)
    s.w.moveBefore(t[0], t[2])
    #expect(s.groupOf(t[0]) == g)
}

@Test func collapsingTheActiveGroupFocusesTheNearestTabOutsideIt() {
    let s = Setup()
    let t = s.open(3)
    let g = s.w.createGroup(t[1])!
    s.w.activate(t[1])
    s.w.toggleCollapsed(g)
    #expect(s.w.activeId == t[2])
    #expect(s.w.visible.map(\.id) == [t[0], t[2]])
    checkInvariants(s.w)
}

@Test func collapsingTheOnlyVisibleTabsOpensAFreshTab() {
    let s = Setup()
    let t = s.open(1)
    let g = s.w.createGroup(t[0])!
    s.w.toggleCollapsed(g)
    #expect(s.w.tabs.count == 2 && s.w.activeId != t[0])
    checkInvariants(s.w)
}

@Test func activatingAHiddenTabExpandsItsGroup() {
    let s = Setup()
    let t = s.open(2)
    let g = s.w.createGroup(t[0])!
    s.w.activate(t[1])
    s.w.toggleCollapsed(g)
    s.w.activate(t[0])
    #expect(s.w.group(g)?.collapsed == false)
}

@Test func closingAGroupClosesItsTabsUngroupingKeepsThem() {
    let s = Setup()
    let t = s.open(3)
    let g = s.w.createGroup(t[0])!
    s.w.addToGroup(t[1], g)
    s.w.ungroup(g)
    #expect(s.ids == t && s.w.groups.isEmpty)
    let g2 = s.w.createGroup(t[0])!
    s.w.addToGroup(t[1], g2)
    s.w.closeGroup(g2)
    #expect(s.ids == [t[2]])
    checkInvariants(s.w)
}

@Test func closingTheLastTabClosesTheWindow() {
    let s = Setup()
    s.w.close(s.open(1)[0])
    #expect(s.emptied == 1)
}

@Test func newTabInAGroupOpensAfterItsLastMember() {
    let s = Setup()
    let t = s.open(3)
    let g = s.w.createGroup(t[0])!
    s.w.addToGroup(t[1], g)
    s.w.newTabInGroup(g)
    let added = s.w.activeId!
    #expect(s.ids == [t[0], t[1], added, t[2]] && s.groupOf(added) == g)
}

@Test func cyclesAndNumbersOnlyVisibleTabs() {
    let s = Setup()
    let t = s.open(3)
    let g = s.w.createGroup(t[1])!
    s.w.activate(t[0])
    s.w.toggleCollapsed(g)
    s.w.cycle(1)
    #expect(s.w.activeId == t[2])
    s.w.selectNumber(1)
    #expect(s.w.activeId == t[0])
    s.w.selectLast()
    #expect(s.w.activeId == t[2])
}

@Test func movesTheActiveTabOneSlotAtATime() {
    let s = Setup()
    let t = s.open(3)
    s.w.activate(t[0])
    s.w.moveActive(1)
    #expect(s.ids == [t[1], t[0], t[2]])
    s.w.moveActive(1)
    #expect(s.ids == [t[1], t[2], t[0]])
    s.w.moveActive(1)
    #expect(s.ids == [t[1], t[2], t[0]])
    s.w.moveActive(-1)
    #expect(s.ids == [t[1], t[0], t[2]])
}

@Test(arguments: 1...5)
func randomOperationsKeepTheInvariants(seed: Int) {
    let s = Setup()
    var state = UInt64(seed) &* 2_654_435_761
    func rand(_ n: Int) -> Int {
        state = (state &* 1_103_515_245 &+ 12345) & 0x7fff_ffff
        return Int(state % UInt64(n))
    }
    func pick<T>(_ xs: [T]) -> T? { xs.isEmpty ? nil : xs[rand(xs.count)] }
    let w = s.w
    s.open(4)
    for step in 0..<2000 {
        let tab = pick(w.tabs), visibleTab = pick(w.visible), group = pick(w.groups)
        let op = rand(18)
        if w.tabs.isEmpty { s.open(2) }
        switch op {
        case 0: w.open()
        case 1: if let tab, w.tabs.count > 1 { w.close(tab.id) }
        case 2: if let tab { w.createGroup(tab.id) }
        case 3: if let tab, let group { w.addToGroup(tab.id, group.id) }
        case 4: if let tab { w.removeFromGroup(tab.id) }
        case 5: if let group { w.toggleCollapsed(group.id) }
        case 6: if let group, w.tabs.count > w.tabsIn(group.id).count { w.ungroup(group.id) }
        case 7: if let visibleTab { w.moveBefore(visibleTab.id, pick(w.visible)?.id) }
        case 8: w.moveActive(rand(2) == 1 ? 1 : -1)
        case 9: if let tab { w.activate(tab.id) }
        case 10: w.cycle(rand(2) == 1 ? 1 : -1)
        case 11: if let group { w.newTabInGroup(group.id) }
        case 12: w.duplicate()
        case 13: w.split(rand(2) == 1 ? .row : .column)
        case 14: w.closeActivePane(); if w.tabs.isEmpty { s.open(2) }
        case 15: w.focusDirection([.left, .right, .up, .down][rand(4)])
        case 16: w.toggleZoom()
        default: w.cyclePane(1)
        }
        if w.tabs.isEmpty { s.open(2) }
        checkInvariants(w, "seed \(seed) step \(step) op \(op)")
        if w.tabs.count > 30 { w.closeMany(w.tabs.prefix(15).map(\.id)) }
    }
}

@Test func splitsTheFocusedPaneAndFocusesTheNewOne() {
    let s = Setup()
    s.w.open(cwd: "/work")
    let first = s.w.activePane!.id
    s.w.split(.row)
    #expect(s.paneIds.count == 2 && s.w.activePane!.id != first && s.w.activePane!.initialCwd == "/work")
}

@Test func movesFocusByDirectionInA2x2Grid() {
    let s = Setup()
    s.w.open()
    let tl = s.w.activePane!.id
    s.w.split(.row)
    let tr = s.w.activePane!.id
    s.w.split(.column)
    let br = s.w.activePane!.id
    s.w.focusPane(tl)
    s.w.split(.column)
    let bl = s.w.activePane!.id
    s.w.focusDirection(.right); #expect(s.w.activePane!.id == br)
    s.w.focusDirection(.up); #expect(s.w.activePane!.id == tr)
    s.w.focusDirection(.left); #expect(s.w.activePane!.id == tl)
    s.w.focusDirection(.down); #expect(s.w.activePane!.id == bl)
    s.w.focusDirection(.down); #expect(s.w.activePane!.id == bl)
}

@Test func closingAPaneGivesItsSpaceToTheSiblingAndFocusToThePrevious() {
    let s = Setup()
    s.w.open()
    let a = s.w.activePane!.id
    s.w.split(.row)
    let b = s.w.activePane!.id
    s.w.split(.column)
    s.w.closeActivePane()
    #expect(s.paneIds == [a, b] && s.w.activePane!.id == b && s.w.active!.root.isSplit)
}

@Test func closingTheLastPaneClosesTheTab() {
    let s = Setup()
    let t = s.open(2)
    s.w.activate(t[1])
    s.w.closeActivePane()
    #expect(s.ids == [t[0]])
}

@Test func aShellExitingClosesJustItsPane() {
    let s = Setup()
    s.w.open()
    s.w.activePane!.sessionId = "s1"
    s.w.split(.row)
    s.w.activePane!.sessionId = "s2"
    s.w.sessionExited("s1")
    #expect(s.paneIds.count == 1 && s.w.activePane!.sessionId == "s2")
}

@Test func zoomOnlyAppliesToSplitTabsAndClearsWhenItsPaneCloses() {
    let s = Setup()
    s.w.open()
    s.w.toggleZoom()
    #expect(s.w.active!.zoomedPaneId == nil)
    s.w.split(.row)
    s.w.toggleZoom()
    #expect(s.w.active!.zoomedPaneId == s.w.activePane!.id)
    s.w.closeActivePane()
    #expect(s.w.active!.zoomedPaneId == nil)
}

@Test func routesProcessInfoToTheRightPane() {
    let s = Setup()
    s.w.open()
    let a = s.w.activePane!
    a.sessionId = "s9"
    s.w.split(.row)
    s.w.applyInfo(session: "s9", process: "-vim", cwd: "/src", isShell: false)
    #expect(a.process == "vim" && a.cwd == "/src" && s.w.activePane!.process == "")
}

private func roundTrip(_ s: SavedSession) -> SavedSession {
    try! JSONDecoder().decode(SavedSession.self, from: JSONEncoder().encode(s))
}

private func json(_ text: String) -> SavedSession { try! JSONDecoder().decode(SavedSession.self, from: Data(text.utf8)) }

@Test func roundTripsTabsGroupsLayoutsFoldersAndFocus() {
    let s = Setup()
    let t = s.open(3)
    let g = s.w.createGroup(t[0])!
    s.w.setGroup(g, name: "api", color: .green)
    s.w.addToGroup(t[2], g)
    s.w.activate(t[2])
    s.w.split(.row)
    s.w.split(.column)
    s.w.setRatio(tab: t[2], path: [], 0.3)
    s.w.activate(t[1])
    let before = roundTrip(s.w.snapshot(fontSize: 14))

    let r = Setup()
    #expect(r.w.restore(before))
    let after = r.w.snapshot(fontSize: 14)
    #expect(after.tabs!.map(\.root) == before.tabs!.map(\.root))
    #expect(after.tabs!.map(\.focused) == before.tabs!.map(\.focused))
    #expect(after.active == before.active)
    #expect(r.w.groups.map { "\($0.name) \($0.color)" } == ["api green"])
    #expect(r.w.tabs[1].root.leaves.map { r.w.panes[$0]!.initialCwd } == ["/t2", "/t2", "/t2"])
    checkInvariants(r.w)
}

@Test func keepsAPaneOnItsGitIdentityThroughSplitDuplicateAndRestore() {
    let s = Setup()
    let t = s.open(1)
    s.w.panes[s.w.tab(t[0])!.focusedPaneId]!.env = ["PWD": "/t0", "WTF_GIT_IDENTITY": "work"]
    s.w.split(.row)
    #expect(s.w.activePane!.gitIdentity == "work")
    s.w.duplicate()
    #expect(s.w.activePane!.gitIdentity == "work")
    s.w.open(cwd: "/plain")
    #expect(s.w.activePane!.gitIdentity == nil)
    let snap = roundTrip(s.w.snapshot(fontSize: 13))
    #expect(snap.tabs![0].root?.aNode?.gitIdentity == "work" && snap.tabs![0].root?.bNode?.gitIdentity == "work")
    #expect(snap.tabs![2].root == SavedNode(cwd: "/plain"))
    let r = Setup()
    r.w.restore(snap)
    #expect(r.w.tabs[0].root.leaves.map { r.w.panes[$0]!.gitIdentity } == ["work", "work"])
    #expect(r.w.tabs[2].root.leaves.map { r.w.panes[$0]!.gitIdentity } == [nil])
}

@Test func expandsACollapsedGroupIfItsTabWasActive() {
    let s = Setup()
    let t = s.open(2)
    s.w.createGroup(t[0])
    var snap = s.w.snapshot(fontSize: 13)
    snap.groups![0].collapsed = true
    snap.active = 0
    let r = Setup()
    r.w.restore(snap)
    #expect(r.w.groups[0].collapsed == false)
    checkInvariants(r.w)
}

@Test func rejectsGarbageAndSkipsMalformedParts() {
    #expect(!Setup().w.restore(json("{}")))
    #expect(!Setup().w.restore(json(#"{"tabs":"nope"}"#)))
    #expect(!Setup().w.restore(json(#"{"tabs":[{"root":{"dir":"row"}}]}"#)))
    let s = Setup()
    #expect(s.w.restore(json("""
    {"tabs":[{"root":{"cwd":"/x"},"groupId":7,"focused":99,"zoomed":0},{"root":"bad"},
             {"root":{"dir":"row","ratio":42,"a":{"cwd":"/l"},"b":{"nonsense":true}}},{"root":{"cwd":"/y"},"groupId":7}],
     "groups":[{"id":7,"name":"g","color":"chartreuse","collapsed":"yes"}],"active":0}
    """)))
    #expect(s.w.tabs.count == 3)
    #expect(s.w.groups[0].color == .blue && !s.w.groups[0].collapsed)
    #expect(s.w.tabs.allSatisfy { !$0.root.isSplit })
    checkInvariants(s.w)
}

@Test func regroupsScatteredMembers() {
    let s = Setup()
    s.w.restore(json("""
    {"tabs":[{"root":{"cwd":"/1"},"groupId":1},{"root":{"cwd":"/2"}},{"root":{"cwd":"/3"},"groupId":1}],
     "groups":[{"id":1,"name":"","color":"red","collapsed":false}],"active":0}
    """))
    #expect(s.w.tabs.map { s.w.focusedPane($0)!.cwd } == ["/1", "/3", "/2"])
    checkInvariants(s.w)
}

@Test func keepsFieldsItDoesNotKnow() {
    let snap = json(#"{"version":1,"tabs":[{"root":{"cwd":"/x"}}],"sidebar":{"open":true,"width":300}}"#)
    let s = Setup()
    s.w.restore(snap)
    let out = String(decoding: try! JSONEncoder().encode(s.w.snapshot(fontSize: 13, extra: snap.extra)), as: UTF8.self)
    #expect(out.contains(#""sidebar":{"#))
}

@Test func detachesATabWithItsSessionsAndAnotherWindowAdoptsIt() {
    let s = Setup()
    let t = s.open(2)
    let g = s.w.createGroup(t[1])!
    s.w.setGroup(g, name: "api")
    s.w.activate(t[1])
    s.w.activePane!.sessionId = "s1"
    s.w.split(.row)
    s.w.activePane!.sessionId = "s2"
    let payload = s.w.detach(t[1]) { "buffer-\($0.id)" }!
    #expect(s.ids == [t[0]] && s.w.activeId == t[0] && s.w.groups.isEmpty && s.emptied == 0)

    let o = Setup()
    o.open(1)
    let moved = try! JSONDecoder().decode(SavedTab.Payload.self, from: JSONEncoder().encode(payload))
    #expect(o.w.adopt(moved))
    let adopted = o.w.active!
    let panes = adopted.root.leaves.map { o.w.panes[$0]! }
    #expect(panes.map(\.sessionId) == ["s1", "s2"])
    #expect(panes.allSatisfy { $0.restoreBuffer?.hasPrefix("buffer-") == true })
    #expect(o.w.group(adopted.groupId!)?.name == "api" && o.w.group(adopted.groupId!)?.color == .blue)
    #expect(o.w.focusedPane(adopted)?.sessionId == "s2")
    checkInvariants(o.w)
}

@Test func joinsAnExistingGroupWithTheSameNameAndColour() {
    let s = Setup()
    let t = s.open(1)
    let g = s.w.createGroup(t[0])!
    s.w.setGroup(g, name: "api")
    s.open(1)
    s.w.adopt(.init(kind: "tab", tab: .init(groupId: nil, root: SavedNode(cwd: "/x", sessionId: "s9"), focused: 0, zoomed: nil, title: nil),
                    group: .init(name: "api", color: "blue")))
    #expect(s.w.groups.count == 1 && s.w.tabsIn(g).count == 2)
    checkInvariants(s.w)
}

@Test func detachingTheLastTabEmptiesTheWindow() {
    let s = Setup()
    _ = s.w.detach(s.open(1)[0]) { _ in "" }
    #expect(s.emptied == 1)
}

@Test func savedSessionsNeverCarrySessionIdsIntoARestore() {
    let s = Setup()
    s.w.restore(json(#"{"tabs":[{"root":{"cwd":"/x","sessionId":"stale","buffer":"x"}}],"groups":[],"active":0}"#))
    #expect(s.w.activePane!.sessionId == nil && s.w.activePane!.restoreBuffer == nil)
}

@Test func rejectsMalformedAdoptions() {
    let s = Setup()
    s.open(1)
    #expect(!s.w.adopt(.init(kind: "tab", tab: .init(groupId: nil, root: nil, focused: nil, zoomed: nil, title: nil), group: nil)))
    #expect(!s.w.adopt(.init(kind: "other", tab: .init(groupId: nil, root: SavedNode(cwd: "/x"), focused: 0, zoomed: nil, title: nil), group: nil)))
    #expect(s.w.tabs.count == 1)
}

@Test func aCustomNameReplacesTheAutomaticOneUntilCleared() {
    let s = Setup()
    let a = s.open(1)[0]
    #expect(s.w.title(s.w.tab(a)!) == "t0")
    s.w.rename(a, "  api   server  ")
    #expect(s.w.title(s.w.tab(a)!) == "api server")
    s.w.activePane!.cwd = "/elsewhere"
    #expect(s.w.title(s.w.tab(a)!) == "api server")
    s.w.rename(a, "   ")
    #expect(s.w.tab(a)!.customTitle == nil && s.w.title(s.w.tab(a)!) == "elsewhere")
    s.w.rename(a, String(repeating: "x", count: 200))
    #expect(s.w.tab(a)!.customTitle?.count == 60)
}

@Test func ignoresJunkTitlesInASavedSession() {
    let s = Setup()
    s.w.restore(json(#"{"tabs":[{"root":{"cwd":"/x"},"title":42},{"root":{"cwd":"/y"},"title":""}],"groups":[],"active":0}"#))
    #expect(s.w.tabs.map(\.customTitle) == [nil, nil])
}

@Test func paneTitles() {
    let p = Pane(id: 1, cwd: "/home/me/src/api")
    #expect(WindowModel.paneTitle(p, home: "/home/me") == "api")
    p.cwd = "/home/me"
    #expect(WindowModel.paneTitle(p, home: "/home/me") == "~")
    p.process = "ssh"; p.isShell = false; p.running = true; p.command = "ssh web1"
    #expect(WindowModel.paneTitle(p, home: "/home/me") == "ssh web1")
    p.oscTitle = "remote: vim"
    #expect(WindowModel.paneTitle(p, home: "/home/me") == "remote: vim")
}

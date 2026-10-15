import Testing
@testable import WtfCore

@Test func sidebarStateRestoresWhatIsValid() {
    let s = SidebarState(json: .object(["open": .bool(true), "width": .number(900), "pinned": .string("relative"),
                                        "tab": .string("kube"), "sshSection": .string("bogus"), "processSort": .string("memory")]))
    #expect(s.open && s.width == 520 && s.pinned == nil && s.tab == .kube && s.sshSection == "hosts")
    // Fields this build doesn't use are kept.
    guard case .object(let o) = s.json else { Issue.record("not an object"); return }
    #expect(o["processSort"] == .string("memory"))
    #expect(SidebarState(json: .string("junk")) == SidebarState())
    #expect(SidebarState.clampWidth(100) == 220)
}

@Test func generateSectionRoundTrips() {
    var s = SidebarState(json: .object(["generateSection": .string("password")]))
    #expect(s.generateSection == "password")
    #expect(SidebarState(json: .object(["generateSection": .string("bogus")])).generateSection == "key")
    s.generateSection = "key"
    #expect(SidebarState(json: s.json).generateSection == "key")
}

@Test func historyScopeRoundTrips() {
    #expect(SidebarState(json: .object(["historyScope": .string("all")])).historyScope == "all")
    #expect(SidebarState(json: .object(["historyScope": .string("x")])).historyScope == "pane")
}

@Test func paneViewsRoundTrip() {
    var s = SidebarState(json: .object(["networkScope": .string("all"), "processSort": .string("memory"), "processScope": .string("bogus")]))
    #expect(s.networkScope == "all" && s.processSort == "memory" && s.processScope == "terminals")
    s.processScope = "all"
    let back = SidebarState(json: s.json)
    #expect(back.processScope == "all" && back.networkScope == "all")
}

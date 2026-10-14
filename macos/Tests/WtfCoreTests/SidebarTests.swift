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

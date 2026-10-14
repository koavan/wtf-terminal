import AppKit
import SwiftTerm

/// One terminal view attached to one backend shell session.
@MainActor
final class TerminalController: NSObject, @MainActor TerminalViewDelegate, OutputSink {
    let view: TerminalView
    private let window: String
    private(set) var id: String?
    /// Called with the title the window should show.
    var onTitle: ((String) -> Void)?
    var onExit: ((Int) -> Void)?

    init(window: String, frame: NSRect) {
        self.window = window
        view = TerminalView(frame: frame, font: NSFont.monospacedSystemFont(ofSize: 13, weight: .regular))
        super.init()
        view.terminalDelegate = self
        view.autoresizingMask = [.width, .height]
        // CoreText beat Metal in the M0 benchmark; Metal stays opt-in.
        if ProcessInfo.processInfo.environment["WTF_NATIVE_RENDERER"] == "metal" {
            do { try view.setUseMetal(true) } catch { NSLog("metal unavailable: \(error)") }
        }
    }

    /// Starts a shell in the user's home directory.
    func start() async throws {
        let term = view.getTerminal()
        let id = try await Backend.call("terminal", "Create", [window, term.cols, term.rows, ""], as: String.self)
        self.id = id
        Backend.on("terminal:info", TerminalInfo.self) { [weak self] info in
            guard let self, info.id == self.id else { return }
            self.onTitle?(Self.title(info))
        }
        Backend.on("terminal:exit", TerminalExit.self) { [weak self] exit in
            guard let self, exit.id == self.id else { return }
            self.onExit?(exit.code)
        }
        try Backend.attach(id, to: self)
    }

    func type(_ text: String) {
        if let id { Backend.write(id, Array(text.utf8)[...]) }
    }

    func output(_ bytes: ArraySlice<UInt8>) { view.feed(byteArray: bytes) }

    private static func title(_ info: TerminalInfo) -> String {
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let dir = info.cwd.hasPrefix(home) ? "~" + info.cwd.dropFirst(home.count) : info.cwd
        return info.isShell ? dir : "\(info.process) — \(dir)"
    }

    // MARK: TerminalViewDelegate

    func send(source: TerminalView, data: ArraySlice<UInt8>) {
        if let id, !data.isEmpty { Backend.write(id, data) }
    }

    func sizeChanged(source: TerminalView, newCols: Int, newRows: Int) {
        guard let id else { return }
        Task { try? await Backend.run("terminal", "Resize", [id, newCols, newRows]) }
    }

    // The shell's own title (OSC 0/2) is ignored, as in the Wails build:
    // the title comes from terminal:info.
    func setTerminalTitle(source: TerminalView, title: String) {}
    func hostCurrentDirectoryUpdate(source: TerminalView, directory: String?) {}
    func scrolled(source: TerminalView, position: Double) {}
    func requestOpenLink(source: TerminalView, link: String, params: [String: String]) {
        if let url = URL(string: link) { NSWorkspace.shared.open(url) }
    }
    func bell(source: TerminalView) { NSSound.beep() }
    func clipboardCopy(source: TerminalView, content: Data) {
        if let s = String(data: content, encoding: .utf8) {
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(s, forType: .string)
        }
    }
    func clipboardRead(source: TerminalView) -> Data? { nil }
    func iTermContent(source: TerminalView, content: ArraySlice<UInt8>) {}
    func rangeChanged(source: TerminalView, startY: Int, endY: Int) {}
}

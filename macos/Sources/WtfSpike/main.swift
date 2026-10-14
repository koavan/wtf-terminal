// M0 spike: a single native window running one shell from the Go backend in
// a SwiftTerm view, for benchmarking against the Wails build.
//
// Environment:
//   WTF_SPIKE_RENDERER=coretext|metal   renderer (default coretext, faster in M0)
//   WTF_SPIKE_RUN="cmd"                 typed into the shell once it starts
import AppKit
import CWtf
import SwiftTerm

// Main-actor isolated; output callbacks arrive on Go threads and hop to main
// before touching the view.
@MainActor
final class TerminalController: NSObject, @MainActor TerminalViewDelegate {
    let view: TerminalView
    private var id: Int64 = -1
    // Keyboard input goes to the PTY off the main thread: a blocking write
    // there could deadlock against output delivery, which waits on main.
    private let input = DispatchQueue(label: "wtf.input")

    init(frame: NSRect) {
        view = TerminalView(frame: frame, font: NSFont.monospacedSystemFont(ofSize: 13, weight: .regular))
        super.init()
        view.terminalDelegate = self
        view.autoresizingMask = [.width, .height]
        if ProcessInfo.processInfo.environment["WTF_SPIKE_RENDERER"] == "metal" {
            do { try view.setUseMetal(true) } catch { NSLog("metal unavailable: \(error)") }
        }
    }

    func start() {
        let term = view.getTerminal()
        let ctx = Unmanaged.passUnretained(self).toOpaque()
        id = wtf_start(Int32(term.cols), Int32(term.rows), Callbacks.onOutput, Callbacks.onExit, ctx)
        if id < 0 { NSLog("could not start shell"); NSApp.terminate(nil); return }
        if let cmd = ProcessInfo.processInfo.environment["WTF_SPIKE_RUN"], !cmd.isEmpty {
            DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) { MainActor.assumeIsolated { self.write(Array((cmd + "\r").utf8)[...]) } }
        }
    }

    private func write(_ data: ArraySlice<UInt8>) {
        let id = self.id
        let bytes = Array(data)
        input.async { bytes.withUnsafeBufferPointer { p in
            p.baseAddress!.withMemoryRebound(to: CChar.self, capacity: bytes.count) {
                wtf_write(id, UnsafeMutablePointer(mutating: $0), Int64(bytes.count))
            }
        } }
    }

    // MARK: TerminalViewDelegate

    func send(source: TerminalView, data: ArraySlice<UInt8>) {
        if ProcessInfo.processInfo.environment["WTF_SPIKE_DEBUG"] != nil {
            NSLog("send: \(String(decoding: data, as: UTF8.self).debugDescription)")
        }
        if !data.isEmpty { write(data) }
    }
    func sizeChanged(source: TerminalView, newCols: Int, newRows: Int) {
        if id >= 0 { wtf_resize(id, Int32(newCols), Int32(newRows)) }
    }
    func setTerminalTitle(source: TerminalView, title: String) { source.window?.title = title.isEmpty ? "wtf" : title }
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

    func close() { if id >= 0 { wtf_close(id) } }
}

@MainActor
// C callbacks from Go threads; nonisolated, so they hop to main explicitly.
// (main.swift globals are main-actor isolated, hence the enum.)
enum Callbacks {
static let onOutput: wtf_output_fn = { ctx, _, buf, len in
    guard let ctx, let buf else { return }
    let bytes = [UInt8](UnsafeRawBufferPointer(start: buf, count: Int(len)))
    let addr = UInt(bitPattern: ctx) // raw pointers aren't Sendable
    // Synchronous: the Go side sends nothing more for this session until the
    // frame is parsed, which is the backpressure.
    DispatchQueue.main.sync {
        MainActor.assumeIsolated {
            Unmanaged<TerminalController>.fromOpaque(UnsafeRawPointer(bitPattern: addr)!).takeUnretainedValue().view.feed(byteArray: bytes[...])
        }
    }
}

static let onExit: wtf_exit_fn = { _, _, code in
    DispatchQueue.main.async { MainActor.assumeIsolated { NSLog("shell exited: \(code)"); NSApp.terminate(nil) } }
}
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    var window: NSWindow!
    var term: TerminalController!

    func applicationDidFinishLaunching(_ note: Notification) {
        let frame = NSRect(x: 0, y: 0, width: 960, height: 600)
        window = NSWindow(contentRect: frame, styleMask: [.titled, .closable, .miniaturizable, .resizable],
                          backing: .buffered, defer: false)
        window.title = "wtf (native spike)"
        term = TerminalController(frame: frame)
        window.contentView = term.view
        window.center()
        window.makeKeyAndOrderFront(nil)
        window.makeFirstResponder(term.view)
        NSApp.activate(ignoringOtherApps: true)
        term.start()
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ app: NSApplication) -> Bool { true }
    func applicationWillTerminate(_ note: Notification) { term?.close() }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.setActivationPolicy(.regular)
let menu = NSMenu()
let appItem = NSMenuItem()
menu.addItem(appItem)
appItem.submenu = NSMenu()
appItem.submenu!.addItem(withTitle: "Quit", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
let editItem = NSMenuItem()
menu.addItem(editItem)
editItem.submenu = NSMenu(title: "Edit")
editItem.submenu!.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
editItem.submenu!.addItem(withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
app.mainMenu = menu
app.run()

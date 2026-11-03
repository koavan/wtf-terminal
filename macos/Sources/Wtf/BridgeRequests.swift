// Answers the bridge's requests about a shell (internal/bridge, #32): what
// it is, its recent command blocks, typing at its prompt, and running a
// line as a command block. The tools that use them, and every check on
// what may run, are in Go; the approval is Claude Code's own prompt.
import AppKit
import WtfCore

private struct BridgeRequest: Decodable {
    let id: String
    let kind: String
    let session: String
    let count: Int?
    let text: String?
}

@MainActor
enum BridgeRequests {
    private static var listening = false
    /// A run that hasn't finished by then gets Ctrl-C (as an investigation step).
    private static let runTimeout: Duration = .seconds(60)

    static func listen() {
        guard !listening else { return }
        listening = true
        Backend.on("bridge:request", BridgeRequest.self) { handle($0) }
    }

    private static func reply(_ id: String, _ r: Go.bridge.Reply) {
        Task { try? await Services.bridge.Reply(id, r) }
    }

    private static func fail(_ id: String, _ message: String) { reply(id, Go.bridge.Reply(error: message)) }

    private static func find(_ session: String) -> (WindowController, Pane, TerminalController)? {
        for w in WindowManager.shared.windows {
            if let p = w.model.pane(session: session), let t = w.terminals[p.id] { return (w, p, t) }
        }
        return nil
    }

    private static func handle(_ req: BridgeRequest) {
        guard let (w, p, term) = find(req.session) else {
            fail(req.id, "That shell has closed")
            return
        }
        switch req.kind {
        case "pane":
            let title = w.model.tabOf(pane: p.id).map { w.model.title($0) } ?? ""
            reply(req.id, Go.bridge.Reply(pane: Go.bridge.Pane(
                title: title, cwd: p.cwd, env: p.env ?? [:], running: p.busy,
                process: p.isShell ? "" : p.process, integrated: p.integrated)))
        case "blocks":
            let blocks = term.recentBlockIDs(max(1, req.count ?? 5)).compactMap { block(term, $0) }
            reply(req.id, Go.bridge.Reply(blocks: blocks))
        case "type":
            switch term.typeAtPrompt(req.text ?? "") {
            case .typed: reply(req.id, Go.bridge.Reply())
            case .busy: fail(req.id, "The shell is busy: a program is running in it")
            case .copied: fail(req.id, "The command has several lines and this shell would run each as it's typed, so it was copied to the clipboard instead")
            }
        case "run":
            run(req, p, term)
        default:
            fail(req.id, "wtf doesn't know the request \(req.kind)")
        }
    }

    private static func block(_ term: TerminalController, _ id: Int, timedOut: Bool = false) -> Go.bridge.Block? {
        guard let b = term.block(id) else { return nil }
        return Go.bridge.Block(id: id, command: b.text, exitCode: b.exitCode ?? -1,
                               output: term.blockOutput(id) ?? "", durationMs: b.durationMs ?? 0, timedOut: timedOut)
    }

    /// Types the line with Enter and waits for its block to end.
    private static func run(_ req: BridgeRequest, _ pane: Pane, _ term: TerminalController) {
        guard pane.integrated else {
            fail(req.id, "This shell has no shell integration, so wtf can't tell when a command ends")
            return
        }
        let line = req.text ?? ""
        let session = req.session
        Task {
            // The shell reports it's back at its prompt a moment after the
            // last command's block ends: wait for that, briefly.
            let settle = ContinuousClock.now + .seconds(5)
            @MainActor func idle() -> Bool {
                guard let (_, p, t) = find(session), t === term else { return false }
                return p.isShell && !p.running
            }
            while !idle(), ContinuousClock.now < settle {
                try? await Task.sleep(for: .milliseconds(100))
            }
            guard idle() else {
                fail(req.id, "The shell isn't at its prompt (a program is running in it, or it closed)")
                return
            }
            let before = term.lastFinishedBlock
            // Typed without moving the keyboard focus.
            term.type(Shell.clearLine + line + "\r")
            let clock = ContinuousClock()
            var deadline = clock.now + runTimeout
            var interrupted = false
            while true {
                try? await Task.sleep(for: .milliseconds(150))
                guard find(session)?.2 === term else {
                    fail(req.id, "The shell closed")
                    return
                }
                if let id = term.lastFinishedBlock, id != before, let b = block(term, id, timedOut: interrupted), b.exitCode >= 0 {
                    reply(req.id, Go.bridge.Reply(block: b))
                    return
                }
                if clock.now > deadline {
                    if interrupted {
                        fail(req.id, "The command didn't stop after Ctrl-C")
                        return
                    }
                    term.type("\u{03}")
                    interrupted = true
                    deadline = clock.now + .seconds(5)
                }
            }
        }
    }
}

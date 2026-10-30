import AppKit
import WtfCore

/// What a finished request cost, as far as the provider says (ai.Usage).
struct AIUsage: Decodable, Sendable {
    /// Whether the provider reported token counts (CLI tools don't).
    var counted = false
    var inputTokens = 0
    var outputTokens = 0
    /// US dollars, when the provider reports it; else 0.
    var cost = 0.0
}

/// A request's end: its whole reply, an error, or cancelled (ai.Done).
struct AIDone: Decodable, Sendable {
    var id = ""
    var text = ""
    var usage = AIUsage()
    var error = ""
    var cancelled = false
    var millis = 0

    /// "1.2 s · 12 in, 3 out tokens · $0.0004": what the provider reported.
    var summary: String {
        var parts = [millis < 1000 ? "\(millis) ms" : String(format: "%.1f s", Double(millis) / 1000)]
        if usage.counted { parts.append("\(usage.inputTokens) in, \(usage.outputTokens) out tokens") }
        if usage.cost > 0 { parts.append(String(format: "$%.4f", usage.cost)) }
        return parts.joined(separator: " · ")
    }
}

private struct AIChunk: Decodable {
    let id: String
    let text: String
}

/// One request to the AI the user configured, for any feature: it previews
/// first, and the first request to a provider waits until the user has seen
/// exactly what will be sent (the preview sheet) and pressed Send. The reply
/// streams to onChunk and ends with onDone — also for an error before
/// sending, or Cancel in the preview.
@MainActor
final class AIRequest {
    /// Requests waiting for their events, by id. One pair of listeners for
    /// all of them: Backend.on listeners are never removed.
    private static var running: [String: AIRequest] = [:]
    private static var listening = false

    let id = "ai-" + UUID().uuidString
    let request: Go.ai.Request
    var onChunk: ((String) -> Void)?
    var onDone: ((AIDone) -> Void)?
    /// Previewing or waiting for the reply.
    private(set) var busy = false
    private var cancelled = false
    private var sheet: FormSheet?

    init(_ request: Go.ai.Request) { self.request = request }

    private static func listen() {
        guard !listening else { return }
        listening = true
        Backend.on("ai:chunk", AIChunk.self) { c in running[c.id]?.onChunk?(c.text) }
        Backend.on("ai:done", AIDone.self) { d in running.removeValue(forKey: d.id)?.finish(d) }
    }

    /// Sends it. window is where the preview sheet goes (on top of any
    /// sheet already there).
    func start(in window: NSWindow?) {
        guard !busy else { return }
        busy = true
        cancelled = false
        Task {
            do {
                let out = try await Services.ai.Preview(request)
                if cancelled { return finish(AIDone(id: id, cancelled: true)) }
                if out.firstSend {
                    sheet = AIPreview.show(out, firstSend: true, in: window) { send in
                        // Held only while the sheet is open (FormSheet drops onClose).
                        self.sheet = nil
                        send ? self.ask(shown: true) : self.finish(AIDone(id: self.id, cancelled: true))
                    }
                    if sheet == nil { finish(AIDone(id: id, error: "There's no window to show what will be sent")) }
                } else {
                    ask(shown: false)
                }
            } catch {
                finish(AIDone(id: id, error: error.localizedDescription))
            }
        }
    }

    private func ask(shown: Bool) {
        Self.listen()
        // Listen before asking: the first chunk can arrive before Ask returns.
        Self.running[id] = self
        Task {
            do {
                try await Services.ai.Ask(id, request, shown)
            } catch {
                Self.running[id] = nil
                finish(AIDone(id: id, error: error.localizedDescription))
            }
        }
    }

    /// Stops it: in the preview, or while the reply streams in.
    func cancel() {
        guard busy else { return }
        cancelled = true
        if let s = sheet {
            sheet = nil
            s.close() // finishes it as cancelled
        } else if Self.running[id] != nil {
            Task { try? await Services.ai.Cancel(id) }
        }
    }

    private func finish(_ d: AIDone) {
        guard busy else { return }
        busy = false
        onDone?(d)
    }

    /// What a request would send, shown without sending ("What's Sent").
    static func showPreview(_ r: Go.ai.Request, in window: NSWindow?) {
        Task {
            do {
                let out = try await Services.ai.Preview(r)
                AIPreview.show(out, firstSend: false, in: window) { _ in }
            } catch {
                Toast.show("Couldn't preview: \(error.localizedDescription)", in: window?.contentView)
            }
        }
    }
}

/// Exactly what a request sends: where it goes, the system text and the
/// messages after Hide Secrets' redaction. Before the first request to a
/// provider it asks (Cancel / Send); on request it only shows (Close).
@MainActor
enum AIPreview {
    /// done(true) on Send, done(false) on Cancel or Close. Nil without a window.
    @discardableResult
    static func show(_ out: Go.ai.Outgoing, firstSend: Bool, in window: NSWindow?, done: @escaping (Bool) -> Void) -> FormSheet? {
        // A sheet can't open over another one on the same window: go on top.
        var host = window
        while let s = host?.attachedSheet { host = s }
        guard let host else { return nil }
        let p = out.provider
        let route = [p.kind, p.model].filter { !$0.isEmpty }.joined(separator: " · ") + " → " + p.destination
        let f = FormSheet(title: firstSend ? "Send to \(p.name)?" : "What’s sent to \(p.name)", subtitle: route, width: 600)
        f.add(f.note(where_(p)))
        f.add(box(out, width: 552))
        f.add(f.note(out.redacted == 0 ? "No secrets were found to hide."
            : "\(out.redacted) \(out.redacted == 1 ? "secret was" : "secrets were") replaced with [REDACTED]."))
        if firstSend { f.add(f.note("This shows before the first request to each provider; later ones are sent straight away.")) }
        var sent = false
        f.onClose = { done(sent) }
        if firstSend {
            f.buttons([("Send", true, { [weak f] in
                sent = true
                f?.close()
            })])
        } else {
            f.buttons([], cancel: "Close")
        }
        f.present(in: host)
        return f
    }

    private static func where_(_ p: Go.ai.ProviderInfo) -> String {
        if p.kind == "command" {
            return "wtf runs \(p.destination) in an empty folder and gives it this text on stdin. The tool’s own login and settings apply."
        }
        if p.local { return "\(p.destination) is on this machine: the text stays here." }
        return "This text goes to \(p.destination)."
    }

    /// The system text and messages, role-labelled, in a scrolling monospace box.
    private static func box(_ out: Go.ai.Outgoing, width: CGFloat) -> NSView {
        let mono = NSFont.monospacedSystemFont(ofSize: 11.5, weight: .regular)
        let head = NSFont.monospacedSystemFont(ofSize: 11.5, weight: .bold)
        let s = NSMutableAttributedString()
        func part(_ role: String, _ text: String) {
            if s.length > 0 { s.append(NSAttributedString(string: "\n\n")) }
            s.append(NSAttributedString(string: "── \(role) ──\n", attributes: [.font: head, .foregroundColor: NSColor.secondaryLabelColor]))
            s.append(NSAttributedString(string: text, attributes: [.font: mono, .foregroundColor: NSColor.labelColor]))
        }
        if !out.system.isEmpty { part("system", out.system) }
        for m in out.messages { part(m.role, m.content) }
        let scroll = NSTextView.scrollableTextView()
        scroll.borderType = .bezelBorder
        let tv = scroll.documentView as! NSTextView
        tv.isEditable = false
        tv.textContainerInset = NSSize(width: 6, height: 6)
        tv.textStorage?.setAttributedString(s)
        tv.layoutManager?.ensureLayout(for: tv.textContainer!)
        let used = tv.layoutManager?.usedRect(for: tv.textContainer!).height ?? 16
        scroll.translatesAutoresizingMaskIntoConstraints = false
        scroll.widthAnchor.constraint(equalToConstant: width).isActive = true
        scroll.heightAnchor.constraint(equalToConstant: min(340, max(80, used + 16))).isActive = true
        return scroll
    }
}

extension Commands {
    /// "AI: Use <name> as Default" and "… for Quick Features" for each
    /// provider in config.toml, but the one already chosen.
    static var aiProviderCommands: [Command] {
        guard let c = Settings.shared.config else { return [] }
        return c.aiProviderNames.flatMap { name -> [Command] in
            var list: [Command] = []
            if name != c.aiDefault {
                list.append(Command("ai.default.\(name)", "AI: Use \(name) as Default") { w in useAI(name, fast: false, in: w) })
            }
            if name != c.aiFast {
                list.append(Command("ai.fast.\(name)", "AI: Use \(name) for Quick Features") { w in useAI(name, fast: true, in: w) })
            }
            return list
        }
    }

    private static func useAI(_ name: String, fast: Bool, in w: WindowController) {
        Task {
            do {
                try await Services.config.SetAIProvider(fast, name)
                Toast.show(fast ? "Quick AI features ask \(name) now" : "AI features ask \(name) now", in: w.rootView)
            } catch {
                Toast.show("Couldn't change the setting: \(error.localizedDescription)", in: w.rootView)
            }
        }
    }
}

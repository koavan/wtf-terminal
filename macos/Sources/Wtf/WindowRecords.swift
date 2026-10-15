import AppKit
import UniformTypeIdentifiers
import WtfCore

/// What a window keeps in memory about its terminals: the commands run,
/// pinned output, recordings and the files saved from it. Never on disk.
@MainActor
final class WindowRecords {
    enum SavedKind: String { case recording = "Recording", text = "Output", markdown = "Transcript" }
    struct Saved { var path: String; var kind: SavedKind; var at: Date }

    static let maxSaved = 20

    var history = History()
    var pins = Pins()
    /// Panes being recorded: where to, and since when.
    var recording: [Int: (path: String, since: Date)] = [:]
    var saved: [Saved] = []
    /// The command block each pane is running (for pins that follow it).
    var runningBlock: [Int: Int] = [:]
    private var observers = Observers()
    private var scanObservers = Observers()

    func observe<T: AnyObject>(_ owner: T, _ fn: @escaping (T) -> Void) { observers.add(owner, fn) }
    func changed() { observers.notify() }

    /// A scanner finished in some terminal (the Findings tab reloads).
    func observeScans<T: AnyObject>(_ owner: T, _ fn: @escaping (T) -> Void) { scanObservers.add(owner, fn) }
    func scanFinished() { scanObservers.notify() }

    func remember(_ path: String, _ kind: SavedKind) {
        saved.removeAll { $0.path == path }
        saved.insert(Saved(path: path, kind: kind, at: Date()), at: 0)
        if saved.count > Self.maxSaved { saved.removeLast(saved.count - Self.maxSaved) }
        changed()
    }
}

extension WindowController {
    /// A pane's tab title, for labels.
    func label(pane id: Int) -> String {
        model.tabOf(pane: id).map { model.title($0) } ?? "terminal"
    }

    /// Shows a pane: its tab in front, the pane focused.
    func show(pane id: Int) {
        guard let t = model.tabOf(pane: id) else { return }
        model.activate(t.id)
        model.focusPane(id)
        focusActive()
    }

    // MARK: Pins

    /// Pins a command's output (following it while it runs) and shows the Pinned tab.
    func pin(block id: Int, in term: TerminalController) {
        guard let b = term.block(id), let p = model.panes[term.paneId] else { return }
        records.pins.add(.init(title: b.text.isEmpty ? "Command output" : b.text, text: term.blockOutput(id) ?? "", paneId: p.id,
                               blockId: id, cwd: p.cwd, running: b.running, exitCode: b.exitCode, durationMs: b.durationMs))
        pinned()
    }

    func pinSelection(_ text: String, in term: TerminalController) {
        guard !text.isEmpty, let p = model.panes[term.paneId] else { return }
        records.pins.add(.init(title: "Selection", text: text, paneId: p.id, blockId: nil, cwd: p.cwd))
        pinned()
    }

    func pinLastOutput() {
        guard let term = activeTerminal, let id = term.lastFinishedBlock else {
            Toast.show("No finished command to pin yet", in: rootView)
            return
        }
        pin(block: id, in: term)
    }

    private func pinned() {
        records.changed()
        sidebar.tab = .pins
        sidebar.open = true
        layoutSidebar()
        focusActive()
    }

    // MARK: Recording

    func isRecording(pane id: Int) -> Bool { records.recording[id] != nil }

    func toggleRecording(pane: Pane? = nil) {
        guard let p = pane ?? activePane else { return }
        isRecording(pane: p.id) ? stopRecording(pane: p.id) : startRecording(p)
    }

    private func startRecording(_ p: Pane) {
        guard let session = p.sessionId, let w = window else { return }
        let title = model.tabOf(pane: p.id).map { model.title($0) } ?? (p.cwd.isEmpty ? "terminal" : WindowModel.prettyPath(p.cwd))
        let panel = NSSavePanel()
        panel.title = "Record Terminal"
        panel.nameFieldStringValue = Export.name(title, at: Date()) + ".cast"
        panel.canCreateDirectories = true
        panel.allowedContentTypes = [UTType(filenameExtension: "cast") ?? .data]
        panel.beginSheetModal(for: w) { [weak self] r in
            guard let self, r == .OK, let url = panel.url else { return }
            Task {
                do {
                    try await Services.terminal.StartRecording(session, url.path, title)
                    self.records.recording[p.id] = (url.path, Date())
                    self.records.changed()
                    self.refreshRecordingIndicators()
                    let where_ = WindowModel.prettyPath(url.path)
                    Toast.show(Redaction.shared.on ? "Recording to \(where_) — output is recorded as is; Hide Secrets doesn't apply to it"
                        : "Recording to \(where_)", in: self.rootView, seconds: 5)
                } catch {
                    Toast.show("Couldn't start recording: \(error.localizedDescription)", in: self.rootView)
                }
            }
        }
    }

    func stopRecording(pane id: Int) {
        guard let rec = records.recording.removeValue(forKey: id) else { return }
        records.changed()
        refreshRecordingIndicators()
        guard let session = model.panes[id]?.sessionId else { return }
        Task {
            do {
                let s = try await Services.terminal.StopRecording(session)
                records.remember(rec.path, .recording)
                Toast.show("Saved recording to \(WindowModel.prettyPath(rec.path)) (\(Format.duration(ms: s.seconds * 1000)))", in: rootView, seconds: 5)
            } catch {
                Toast.show("The recording ended with an error: \(error.localizedDescription)", in: rootView, seconds: 5)
            }
        }
    }

    /// A pane went away: its shell ended (the backend closed the file) or its tab moved.
    func recordsPaneGone(_ id: Int, moved: Bool) {
        records.history.clear(id)
        records.pins.paneGone(id)
        records.runningBlock[id] = nil
        if let rec = records.recording.removeValue(forKey: id), !moved {
            records.remember(rec.path, .recording)
            Toast.show("Saved recording to \(WindowModel.prettyPath(rec.path))", in: rootView, seconds: 5)
        }
        records.changed()
    }

    /// A pane that came from another window: is its shell being recorded?
    func syncRecording(pane id: Int) {
        guard let session = model.panes[id]?.sessionId else { return }
        Task {
            let s = try? await Services.terminal.Recording(session)
            if let s, s.active {
                records.recording[id] = (s.path, Date().addingTimeInterval(-s.seconds))
            } else {
                records.recording[id] = nil
            }
            records.changed()
            refreshRecordingIndicators()
        }
    }

    // MARK: Saving output

    private func savePanel(_ title: String, _ ext: String, _ type: UTType, base: String, _ done: @escaping (String) -> Void) {
        guard let w = window else { return }
        let panel = NSSavePanel()
        panel.title = title
        panel.nameFieldStringValue = Export.name(base, at: Date()) + "." + ext
        panel.canCreateDirectories = true
        panel.allowedContentTypes = [type]
        panel.beginSheetModal(for: w) { r in
            if r == .OK, let url = panel.url { done(url.path) }
        }
    }

    func saveOutputAsText(pane: Pane? = nil) {
        guard let p = pane ?? activePane, let term = terminals[p.id] else { return }
        let text = Redaction.shared.text(term.plainText()) + "\n"
        savePanel("Save Output as Text", "txt", .plainText, base: label(pane: p.id)) { [weak self] path in
            Task { [weak self] in
                guard let self else { return }
                do {
                    try await Services.files.SaveText(path, text)
                    records.remember(path, .text)
                    Toast.show("Saved output to \(WindowModel.prettyPath(path))", in: rootView)
                } catch {
                    Toast.show("Couldn't save: \(error.localizedDescription)", in: rootView)
                }
            }
        }
    }

    func saveTranscript(pane: Pane? = nil) {
        guard let p = pane ?? activePane, let term = terminals[p.id] else { return }
        guard p.integrated else {
            Toast.show("A transcript needs shell integration (bash or zsh) — Save Output as Text works anywhere", in: rootView)
            return
        }
        let blocks = term.transcript()
        guard !blocks.isEmpty else {
            Toast.show("No commands to save yet", in: rootView)
            return
        }
        let title = label(pane: p.id)
        let md = Redaction.shared.text(Export.markdown(blocks, title: title, cwd: WindowModel.prettyPath(p.cwd), exportedAt: Date()))
        savePanel("Save Commands as Markdown", "md", UTType(filenameExtension: "md") ?? .plainText, base: title) { [weak self] path in
            Task { [weak self] in
                guard let self else { return }
                do {
                    try await Services.files.SaveText(path, md)
                    records.remember(path, .markdown)
                    let n = blocks.count
                    Toast.show("Saved \(n) \(n == 1 ? "command" : "commands") to \(WindowModel.prettyPath(path))", in: rootView)
                } catch {
                    Toast.show("Couldn't save: \(error.localizedDescription)", in: rootView)
                }
            }
        }
    }
}

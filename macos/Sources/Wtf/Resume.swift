import AppKit
import WtfCore

/// Resuming agents after a restart (#38). A tab that ran Claude Code
/// saves it with its pane; background agents are saved when wtf quits.
/// After a relaunch, each such tab shows a bar (Resume / Dismiss) and the
/// Agents tab lists the background ones under Resumable. Nothing resumes
/// on its own: `claude --resume <id>` runs only on your click. Like
/// internal/gtkui/resume.go.
@MainActor
enum Resume {
    /// Background agents saved when wtf last quit, not yet resumed or dismissed.
    private(set) static var background: [Go.state.Agent] = []
    /// Claude Code session → the name its agent had: given back when it
    /// reports in again after resuming.
    static var pendingNames: [String: String] = [:]

    static func prefix(_ a: Go.state.Agent) -> String {
        a.provider == "codex" ? "codex resume " : "claude --resume "
    }

    /// At launch, once the saved windows are open: drops what can't be
    /// resumed any more (its transcript or folder is gone, quietly) and
    /// shows the rest.
    static func loadAtLaunch() {
        Task {
            background = (try? await Services.state.BackgroundAgents()) ?? []
            Agents.changed()
            var pending: [(WindowController, Pane, Go.state.Agent)] = []
            for w in WindowManager.shared.windows where !w.background {
                for p in w.model.panes.values { if let a = p.resumable { pending.append((w, p, a)) } }
            }
            guard !pending.isEmpty else { return }
            let ok = (try? await Services.state.StillResumable(pending.map(\.2))) ?? []
            for (i, (w, p, _)) in pending.enumerated() {
                if i < ok.count, ok[i] {
                    showBar(p, in: w)
                } else {
                    p.resumable = nil
                    w.scheduleSave()
                }
            }
        }
    }

    /// What resumes `a` in a shell in `cwd`: claude finds a session by the
    /// folder it started in.
    static func command(_ a: Go.state.Agent, from cwd: String) -> String {
        let resume = prefix(a) + Shell.quote(a.session)
        return cwd == a.dir ? resume : "cd \(Shell.quote(a.dir)) && \(resume)"
    }

    /// What an agent is called in the bar and the list.
    static func title(_ a: Go.state.Agent) -> String {
        a.name.isEmpty ? Paths.base(a.dir) : a.name
    }

    // MARK: Tabs

    static func showBar(_ p: Pane, in w: WindowController) {
        guard let a = p.resumable, let term = w.terminals[p.id] else { return }
        let bar = ResumeBar(a, command: command(a, from: p.cwd),
                            resume: { [weak w] in if let w { resume(p, in: w) } },
                            dismiss: { [weak w] in if let w { dismiss(p, in: w) } })
        term.container.setResumeBar(bar)
    }

    /// Types `claude --resume` into the pane's shell (a new tab if it's busy).
    static func resume(_ p: Pane, in w: WindowController) {
        guard let a = p.resumable else { return }
        if !a.name.isEmpty { pendingNames[a.session] = a.name }
        dismiss(p, in: w)
        w.runInPane(p.id, command(a, from: p.cwd))
    }

    static func dismiss(_ p: Pane, in w: WindowController) {
        p.resumable = nil
        w.terminals[p.id]?.container.setResumeBar(nil)
        w.scheduleSave()
    }

    // MARK: Background

    /// Resumes a background agent in a new tab of `w`, or in the background.
    static func resumeBackground(_ a: Go.state.Agent, inTab: Bool, from w: WindowController) {
        if !a.name.isEmpty { pendingNames[a.session] = a.name }
        let cmd = prefix(a) + Shell.quote(a.session)
        if inTab {
            w.model.open(cwd: a.dir, command: cmd)
        } else {
            WindowManager.shared.startInBackground(cwd: a.dir, command: cmd)
        }
        forget(a)
    }

    static func forget(_ a: Go.state.Agent) {
        background.removeAll { $0 == a }
        let left = background
        Task { try? await Services.state.SaveBackgroundAgents(left) }
        Agents.changed()
    }

    /// Resume All…: lists them, then resumes each in the background.
    static func resumeAll(from w: WindowController) {
        let list = background
        guard !list.isEmpty else { return }
        Confirm.show(in: w.window, title: "Resume \(list.count) agents?",
                     message: "Each runs claude --resume in a new shell in the background, in its folder. The Agents tab shows them.",
                     items: list.map { "\(title($0)) — \(WindowModel.prettyPath($0.dir))" }, button: "Resume All",
                     limit: 10, destructive: false) {
            for a in list { resumeBackground(a, inTab: false, from: w) }
        }
    }

    /// When wtf quits: the agents running in the background, and the ones
    /// still offered from last time, for the next launch.
    static func saveAtQuit() {
        var list = background
        if let bg = WindowManager.shared.background {
            for p in bg.model.panes.values { if let a = p.agent, !list.contains(a) { list.append(a) } }
        }
        try? Backend.runNow("state", "SaveBackgroundAgents", [list])
    }
}

/// "Claude Code was running here when wtf quit · Resume · Dismiss".
@MainActor
final class ResumeBar: NSView {
    init(_ a: Go.state.Agent, command: String, resume: @escaping () -> Void, dismiss: @escaping () -> Void) {
        super.init(frame: .zero)
        wantsLayer = true
        layer?.cornerRadius = 8
        layer?.backgroundColor = NSColor.controlAccentColor.withAlphaComponent(0.16).cgColor
        layer?.borderColor = NSColor.controlAccentColor.withAlphaComponent(0.45).cgColor
        layer?.borderWidth = 1
        let icon = NSImageView(image: NSImage(systemSymbolName: "sparkles", accessibilityDescription: nil)!)
        icon.contentTintColor = .controlAccentColor
        let text = a.name.isEmpty ? "Claude Code was running here when wtf quit." : "Claude Code “\(a.name)” was running here when wtf quit."
        let label = NSTextField(labelWithString: text)
        label.font = .systemFont(ofSize: 12)
        label.lineBreakMode = .byTruncatingTail
        label.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        let go = ClosureButton(title: "Resume", resume)
        go.bezelStyle = .rounded
        go.controlSize = .small
        go.keyEquivalent = ""
        go.toolTip = "Runs: " + command
        let no = ClosureButton(title: "Dismiss", dismiss)
        no.bezelStyle = .rounded
        no.controlSize = .small
        no.toolTip = "Stop offering it"
        let spacer = NSView()
        spacer.setContentHuggingPriority(.init(1), for: .horizontal)
        let row = NSStackView(views: [icon, label, spacer, no, go])
        row.spacing = 8
        row.edgeInsets = NSEdgeInsets(top: 0, left: 10, bottom: 0, right: 8)
        row.translatesAutoresizingMaskIntoConstraints = false
        addSubview(row)
        NSLayoutConstraint.activate([
            row.leadingAnchor.constraint(equalTo: leadingAnchor), row.trailingAnchor.constraint(equalTo: trailingAnchor),
            row.centerYAnchor.constraint(equalTo: centerYAnchor),
        ])
        setAccessibilityElement(true)
        setAccessibilityRole(.group)
        setAccessibilityLabel(text)
    }

    required init?(coder: NSCoder) { fatalError() }
}

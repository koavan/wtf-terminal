import AppKit
import WtfCore

/// The commands the palette lists and the menus run, by the ids the Wails
/// build uses (lib/commands.ts). Only what the native app does so far:
/// side-pane commands come with M4/M5.
@MainActor
struct Command {
    let id: String
    let label: String
    /// Shortcut text shown in the palette (the menu holds the real binding).
    let key: String
    let available: @MainActor (WindowController) -> Bool
    let run: @MainActor (WindowController) -> Void

    init(_ id: String, _ label: String, _ key: String = "", available: (@MainActor (WindowController) -> Bool)? = nil,
         _ run: @escaping @MainActor (WindowController) -> Void) {
        self.id = id
        self.label = label
        self.key = key
        self.available = available ?? { _ in true }
        self.run = run
    }
}

@MainActor
enum Commands {
    private static func split(_ w: WindowController) -> Bool { w.model.active?.root.isSplit == true }
    private static func integrated(_ w: WindowController) -> Bool { w.activePane?.integrated == true }
    private static func send(_ w: WindowController, _ sel: Selector) {
        // Terminal commands go to the focused terminal's view.
        _ = w.activeTerminal?.view.tryToPerform(sel, with: nil)
    }

    static var all: [Command] { [
        Command("palette", "Command Palette", "⌘⇧P") { Palette.toggle(.commands, in: $0) },
        Command("app.about", "About wtf") { AboutDialog.show(in: $0) },
        Command("inspect.tls", "Inspect TLS Certificate…") { CertViewer(.host(""), owner: $0).show() },
        Command("tools.show", "Command-Line Tools (kubectl, AWS CLI, git…)") { ToolsDialog.show(in: $0) },
        Command("claude.ask", "Ask Claude (runs Claude Code beside this shell)", "⌘I") { ClaudePane.ask(in: $0) },
        Command("ai.ask", "Command from Plain English… (Claude)") { ClaudePane.command(in: $0) },
        Command("review.last", "Review Last Command’s Changes…", available: integrated) { $0.reviewLastCommand() },
        Command("ai.investigate", "Investigate… (Claude)") { ClaudePane.investigate(in: $0) },
        Command("ai.explain", "Explain Last Failed Command… (Claude)", available: integrated) { $0.explainLastFailure() },
        Command("ai.providers", "AI Providers (add, keys, default, test)") { SettingsWindow.show(.ai, from: $0) },
        Command("help.manual", "Open Manual", "⌘?") { ManualWindow.show(from: $0) },
        Command("help.shortcuts", "Show Keyboard Shortcuts") { _ in ManualWindow.show("shortcuts") },
        Command("workspace.open", "Open Workspace…") { Palette.toggle(.workspaces, in: $0) },
        Command("theme.pick", "Change Theme…") { Palette.toggle(.themes, in: $0) },
        Command("config.open", "Settings…", "⌘,") { SettingsWindow.show(from: $0) },
        Command("config.file", "Open config.toml") { w in
            Task { @MainActor in do { try await Services.config.OpenConfigFile() } catch { Toast.show("Couldn't open config.toml: \(error.localizedDescription)", in: w.rootView) } }
        },
        Command("themes.folder", "Open Themes Folder") { w in
            Task { @MainActor in do { try await Backend.run("config", "OpenThemesFolder") } catch { Toast.show("Couldn't open folder: \(error.localizedDescription)", in: w.rootView) } }
        },
        Command("paste.confirm", "Toggle Paste Confirmation") { w in
            let on = !(Settings.shared.config?.confirmPaste ?? true)
            Task { @MainActor in
                try? await Backend.run("config", "SetConfirmPaste", [on])
                Toast.show(on ? "Multi-line pastes are shown first" : "Pastes go straight in", in: w.rootView)
            }
        },
        Command("sidebar.toggle", "Toggle Sidebar", "⌘B") { $0.toggleSidebar(nil) },
        Command("sidebar.files", "Show Files", "⌘⇧E") { $0.showSidebar(.files) },
        Command("sidebar.ssh", "Show SSH Hosts", "⌘⇧S") { $0.showSidebar(.ssh) },
        Command("sidebar.aws", "Show AWS Profiles", "⌘⇧U") { $0.showSidebar(.aws) },
        Command("sidebar.generate", "Show Generate Keys & Passwords", "⌘⇧N") { $0.showSidebar(.generate) },
        Command("generate.key", "Generate SSH Key…") { $0.showGenerator("key") },
        Command("generate.password", "Generate Password…") { $0.showGenerator("password") },
        Command("sidebar.kube", "Show Kubernetes", "⌘⇧K") { $0.showSidebar(.kube) },
        Command("sidebar.snippets", "Show Snippets", "⌘⇧Y") { $0.showSidebar(.snippets) },
        Command("snippets.run", "Run Snippet…", "⌘⇧R") { Palette.toggle(.snippets, in: $0) },
        Command("snippets.open", "Open Snippets File") { $0.openSnippetsFile() },
        Command("sidebar.history", "Show Command History", "⌘⇧H") { $0.showSidebar(.history) },
        Command("sidebar.pins", "Show Pinned Output", "⌘⇧I") { $0.showSidebar(.pins) },
        Command("sidebar.share", "Show Share & Record", "⌘⇧J") { $0.showSidebar(.share) },
        Command("sidebar.findings", "Show Findings (Security Scans)", "⌘⇧F") { $0.showSidebar(.findings) },
        Command("findings.scan", "Run Security Scan…") { ScanDialog.show(in: $0) },
        Command("record.toggle", "Start or Stop Recording…", "⌥⌘S") { $0.toggleRecording() },
        Command("export.text", "Save Output as Text…") { $0.saveOutputAsText() },
        Command("export.markdown", "Save Commands as Markdown…", available: integrated) { $0.saveTranscript() },
        Command("term.pinOutput", "Pin Last Command Output", available: integrated) { $0.pinLastOutput() },
        Command("sidebar.workspaces", "Show Workspaces", "⌘⇧L") { $0.showSidebar(.workspaces) },
        Command("sidebar.network", "Show Network Connections", "⌘⇧O") { $0.showSidebar(.network) },
        Command("sidebar.processes", "Show Processes", "⌘⇧A") { $0.showSidebar(.processes) },
        Command("usage.show", "Resource Usage… (wtf’s CPU and Memory)") { UsageDialog.show(in: $0) },
        Command("usage.toggle", "Toggle Resource Usage in Address Bar") { UsageStore.toggle(in: $0) },
        Command("workspace.save", "Save Window as Workspace…") { WorkspaceEditor.show(.capture($0.model.captureWorkspace()), in: $0) },
        Command("sidebar.autoRefresh", "Toggle Side Pane Auto-Refresh") { w in
            let on = !(Settings.shared.config?.sidebarAutoRefresh ?? true)
            Task { @MainActor in
                do {
                    try await Services.config.SetSidebarAutoRefresh(on)
                    Toast.show(on ? "Side pane auto-refresh on" : "Side pane auto-refresh off", in: w.rootView)
                } catch {
                    Toast.show("Couldn't change the setting: \(error.localizedDescription)", in: w.rootView)
                }
            }
        },
        Command("tab.new", "New Tab", "⌘T") { $0.model.open() },
        Command("tab.newApi", "New API Tab (HTTP Requests)", "⌥⌘T") { $0.model.openApi() },
        Command("tab.duplicate", "Duplicate Tab") { $0.model.duplicate() },
        Command("tab.close", "Close Tab", "⌥⌘W") { w in if let id = w.model.activeId { w.closeTabs([id]) } },
        Command("tab.overview", "Show All Tabs", "⌘⇧\\") { TabOverview.toggle(in: $0) },
        Command("tab.toNewWindow", "Move Tab to New Window") { w in if let id = w.model.activeId { w.moveToNewWindow(id) } },
        Command("window.new", "New Window", "⌘N") { _ in WindowManager.shared.newWindow() },
        Command("window.close", "Close Window", "⌘⇧W") { w in _ = w.windowShouldClose(w.window!) },
        Command("tab.rename", "Rename Tab…") { w in w.renameTab(nil) },
        Command("tab.reopen", "Reopen Closed Tab", "⌘⇧T") { $0.model.reopen() },
        Command("tab.next", "Next Tab", "⌘⇧]") { $0.model.cycle(1) },
        Command("tab.prev", "Previous Tab", "⌘⇧[") { $0.model.cycle(-1) },
        Command("tab.moveRight", "Move Tab Right", "⌘⇧→") { $0.model.moveActive(1) },
        Command("tab.moveLeft", "Move Tab Left", "⌘⇧←") { $0.model.moveActive(-1) },
        Command("broadcast.toggle", "Toggle Broadcast Input", "⌥⌘B") { Broadcast.shared.toggleActive(in: $0) },
        Command("tab.group", "Add Tab to New Group", "⌥⌘G") { $0.addTabToNewGroup(nil) },
        Command("pane.splitRight", "Split Right", "⌘D") { $0.model.split(.row) },
        Command("pane.splitDown", "Split Down", "⌘⇧D") { $0.model.split(.column) },
        Command("pane.close", "Close Pane", "⌘W") { $0.closePaneOrTab(nil) },
        Command("pane.zoom", "Toggle Pane Zoom", "⌘⇧↩", available: split) { $0.model.toggleZoom() },
        Command("pane.next", "Next Pane", "⌘]", available: split) { $0.model.cyclePane(1) },
        Command("pane.prev", "Previous Pane", "⌘[", available: split) { $0.model.cyclePane(-1) },
        Command("pane.left", "Focus Pane Left", "⌥⌘←", available: split) { $0.model.focusDirection(.left) },
        Command("pane.right", "Focus Pane Right", "⌥⌘→", available: split) { $0.model.focusDirection(.right) },
        Command("pane.up", "Focus Pane Above", "⌥⌘↑", available: split) { $0.model.focusDirection(.up) },
        Command("pane.down", "Focus Pane Below", "⌥⌘↓", available: split) { $0.model.focusDirection(.down) },
        Command("find", "Find", "⌘F") { w in
            if let a = w.activeApi { a.find(.showFindInterface) } else { w.activeTerminal?.showFind() }
        },
        Command("find.next", "Find Next", "⌘G") { w in
            if let a = w.activeApi { a.find(.nextMatch) } else { w.activeTerminal?.findNext() }
        },
        Command("find.prev", "Find Previous", "⌘⇧G") { w in
            if let a = w.activeApi { a.find(.previousMatch) } else { w.activeTerminal?.findPrevious() }
        },
        Command("prompt.prev", "Jump to Previous Prompt", "⌘↑", available: integrated) { $0.activeTerminal?.jumpPrompt(-1) },
        Command("prompt.next", "Jump to Next Prompt", "⌘↓", available: integrated) { $0.activeTerminal?.jumpPrompt(1) },
        Command("secrets.toggle", "Hide Secrets (toggle)", "⌥⌘R") { w in
            Redaction.shared.on.toggle()
            Toast.show(Redaction.shared.on ? "Secrets hidden — on screen and when copied" : "Secrets shown", in: w.rootView)
        },
        Command("term.copyOutput", "Copy Last Command Output", "⌥⌘C", available: integrated) { $0.activeTerminal?.copyLastOutput() },
        Command("term.clear", "Clear Buffer", "⌘K") { $0.activeTerminal?.clearBuffer() },
        Command("term.selectAll", "Select All", "⌘A") { $0.activeTerminal?.view.selectAll() },
        Command("term.copy", "Copy", "⌘C") { w in w.activeTerminal.map { $0.view.copy($0) } },
        Command("term.paste", "Paste", "⌘V") { w in w.activeTerminal.map { $0.view.paste($0) } },
        Command("font.bigger", "Bigger Text", "⌘=") { _ in Settings.shared.biggerText() },
        Command("font.smaller", "Smaller Text", "⌘-") { _ in Settings.shared.smallerText() },
        Command("font.reset", "Actual Size", "⌘0") { _ in Settings.shared.actualSize() },
    ] + aiProviderCommands }

    static func run(_ id: String, in w: WindowController) {
        if id.hasPrefix("tab.select.") {
            let n = id.dropFirst("tab.select.".count)
            n == "last" ? w.model.selectLast() : w.model.selectNumber(Int(n) ?? 0)
            return
        }
        all.first { $0.id == id }?.run(w)
    }
}

/// Menu items carry a command id; the key window runs it.
@MainActor
final class CommandTarget: NSObject {
    static let shared = CommandTarget()

    @objc func run(_ sender: NSMenuItem) {
        guard let id = sender.representedObject as? String, let w = WindowManager.shared.keyWindow else { return }
        Commands.run(id, in: w)
    }
}

import AppKit
import WtfCore

extension Notification.Name {
    /// A worktree was added or removed: the Files tab reads its repository again.
    static let worktreesChanged = Notification.Name("wtf.worktreesChanged")
}

/// Git worktrees (#37): New Worktree…, Open Worktree… and Remove
/// Worktree… for the repository a folder is in. Every change is shown as
/// the git command first and runs only on your confirm; removing never
/// deletes a branch, and git's own refusal (changed or untracked files)
/// asks again before --force. Like internal/gtkui/worktrees.go.
@MainActor
enum WorktreeUI {
    /// Reads the repository `dir` is in, then runs `f`; says why not otherwise.
    static func withRepo(_ dir: String, owner: WindowController, _ f: @escaping (Go.worktree.Repo) -> Void) {
        guard !dir.isEmpty else {
            Toast.show("Waiting for the shell’s folder", in: owner.rootView)
            return
        }
        Task {
            do { f(try await Services.worktree.Repo(dir)) } catch {
                Toast.show("\(WindowModel.prettyPath(dir)): \(error.localizedDescription)", in: owner.rootView)
            }
        }
    }

    /// The focused pane's folder: where the palette commands ask from.
    static func here(_ w: WindowController) -> String { w.activePane?.cwd ?? "" }

    // MARK: New

    static func new(from dir: String, owner: WindowController) {
        withRepo(dir, owner: owner) { NewWorktreeSheet(repo: $0, owner: owner).show() }
    }

    /// What happens once a worktree is there.
    enum Then: Int { case tab, claude, background }

    static func opened(_ path: String, then: Then, prompt: String, owner: WindowController) {
        let p = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
        switch then {
        case .tab: openTab(path, command: nil, owner: owner)
        case .claude: openTab(path, command: Background.command(prompt: p), owner: owner)
        case .background: owner.startAgent(in: path, prompt: p, background: true)
        }
    }

    /// A new tab in `path`, right after the active one and in its group.
    static func openTab(_ path: String, command: String?, owner: WindowController) {
        let m = owner.model
        let at = m.active.flatMap { m.index($0.id) }.map { $0 + 1 }
        m.open(cwd: path, at: at, group: m.active?.groupId, command: command)
        owner.window?.makeKeyAndOrderFront(nil)
    }

    // MARK: Open

    static func open(from dir: String, owner: WindowController) {
        withRepo(dir, owner: owner) { repo in
            let trees = repo.worktrees.filter { !$0.prunable }
            let f = FormSheet(title: "Open Worktree", subtitle: "\(repo.name)’s checkouts. Open goes to a tab already in it, or opens one.")
            let list = f.popup(trees.map { Worktrees.title($0) }, selected: trees.firstIndex { $0.path != repo.current } ?? 0)
            f.field("Worktree", list)
            f.buttons([
                ("Start Claude Code", false, { [weak f] in
                    f?.close()
                    openTab(trees[list.indexOfSelectedItem].path, command: Background.command(prompt: ""), owner: owner)
                }),
                ("Open", true, { [weak f] in
                    f?.close()
                    go(to: trees[list.indexOfSelectedItem].path, owner: owner)
                }),
            ])
            f.present(in: owner.window, focus: list)
        }
    }

    /// Goes to a tab whose shell is in `path` (any window), else opens one.
    static func go(to path: String, owner: WindowController) {
        for w in WindowManager.shared.windows where !w.background {
            let ids = w.model.panes.values.filter { Worktrees.inside($0.cwd, path) }.map(\.id).sorted()
            guard let id = ids.first, let t = w.model.tabOf(pane: id) else { continue }
            w.model.activate(t.id)
            w.model.focusPane(id)
            w.window?.makeKeyAndOrderFront(nil)
            return
        }
        openTab(path, command: nil, owner: owner)
    }

    // MARK: Remove

    static func remove(from dir: String, owner: WindowController) {
        withRepo(dir, owner: owner) { repo in
            let trees = repo.worktrees.filter { !$0.main }
            guard !trees.isEmpty else {
                Toast.show("\(repo.name) has no other worktrees", in: owner.rootView)
                return
            }
            let f = FormSheet(title: "Remove Worktree",
                              subtitle: "Removes a worktree’s folder and git’s record of it. Its branch stays. You’ll see the command, and what isn’t committed there, before anything runs.")
            let list = f.popup(trees.map { Worktrees.title($0) }, selected: trees.firstIndex { $0.path == repo.current } ?? 0)
            f.field("Worktree", list)
            f.buttons([("Remove…", true, { [weak f] in
                f?.close()
                confirmRemove(trees[list.indexOfSelectedItem].path, repo: repo.main, owner: owner)
            })])
            f.present(in: owner.window, focus: list)
        }
    }

    /// Shows what removing the worktree at `path` runs and loses, then
    /// removes it on confirm. When git refuses, asks again before --force.
    static func confirmRemove(_ path: String, repo: String, owner: WindowController) {
        Task {
            let plan: Go.worktree.RemovePlan
            do { plan = try await Services.worktree.PlanRemove(repo, path) } catch {
                Toast.show(error.localizedDescription, in: owner.rootView)
                return
            }
            let tabs = WindowManager.shared.windows.filter { !$0.background }
                .reduce(0) { n, w in n + w.model.panes.values.filter { Worktrees.inside($0.cwd, path) }.count }
            var message = plan.prunable
                ? "Its folder is already gone: this only drops git’s record of it."
                : "Removes the folder \(WindowModel.prettyPath(plan.path))."
            if !plan.branch.isEmpty { message += " The branch \(plan.branch) stays." }
            if !plan.changes.isEmpty {
                message += "\n\nNot committed there (git won’t remove it unless you then force it):\n"
                    + plan.changes.map { "  " + $0 }.joined(separator: "\n")
                    + (plan.more > 0 ? "\n  and \(plan.more) more" : "")
            }
            if tabs > 0 { message += "\n\n\(tabs) shell\(tabs == 1 ? " is" : "s are") in that folder; they stay open." }
            message += "\n\nwtf runs:"
            Confirm.show(in: owner.window, title: "Remove worktree \(plan.branch.isEmpty ? Paths.base(plan.path) : plan.branch)?",
                         message: message, items: [plan.command], button: "Remove") {
                run(path, repo: repo, force: false, owner: owner)
            }
        }
    }

    private static func run(_ path: String, repo: String, force: Bool, owner: WindowController) {
        Task {
            do {
                let r = try await Services.worktree.Remove(repo, path, force)
                if !r.refused.isEmpty {
                    Confirm.show(in: owner.window, title: "Git won’t remove it",
                                 message: "git says: \(r.refused)\n\nRemove it anyway? Uncommitted changes and untracked files in it are lost. The branch stays. wtf runs:",
                                 items: ["git worktree remove --force \(Shell.quote(path))"], button: "Remove Anyway") {
                        run(path, repo: repo, force: true, owner: owner)
                    }
                    return
                }
                NotificationCenter.default.post(name: .worktreesChanged, object: nil)
                Toast.show("Removed the worktree \(WindowModel.prettyPath(path))", in: owner.rootView)
            } catch {
                Toast.show(error.localizedDescription, in: owner.rootView)
            }
        }
    }

    // MARK: Menus

    /// The worktree items for a menu about `repo` (Files tab).
    static func menuItems(_ repo: Go.worktree.Repo, owner: WindowController, heading: Bool = true) -> [NSMenuItem] {
        var items: [NSMenuItem] = []
        if heading {
            let h = NSMenuItem(title: "Worktrees of \(repo.name)", action: nil, keyEquivalent: "")
            h.isEnabled = false
            items.append(h)
        }
        items.append(ClosureMenuItem(title: "New Worktree…", keyEquivalent: "") {
            NewWorktreeSheet(repo: repo, owner: owner).show()
        })
        let others = repo.worktrees.filter { $0.path != repo.current && !$0.prunable }
        if !others.isEmpty {
            let open = NSMenuItem(title: "Open Worktree", action: nil, keyEquivalent: "")
            let sub = NSMenu()
            for w in others {
                sub.addItem(ClosureMenuItem(title: Worktrees.title(w), keyEquivalent: "") { go(to: w.path, owner: owner) })
            }
            open.submenu = sub
            items.append(open)
        }
        let removable = repo.worktrees.filter { !$0.main }
        if !removable.isEmpty {
            let rm = NSMenuItem(title: "Remove Worktree", action: nil, keyEquivalent: "")
            let sub = NSMenu()
            for w in removable {
                sub.addItem(ClosureMenuItem(title: Worktrees.title(w) + "…", keyEquivalent: "") {
                    confirmRemove(w.path, repo: repo.main, owner: owner)
                })
            }
            rm.submenu = sub
            items.append(rm)
        }
        return items
    }
}

/// New Worktree…: a branch (an existing one, or a new one from a branch
/// or commit), its folder, and what to do once it's there. Create shows the
/// git command and runs it only on confirm.
@MainActor
final class NewWorktreeSheet: NSObject, NSComboBoxDelegate, NSTextFieldDelegate {
    private static var open: [NewWorktreeSheet] = []

    private let repo: Go.worktree.Repo
    private unowned let owner: WindowController
    private let f: FormSheet
    private let branch = NSComboBox()
    private let base = NSComboBox()
    private let kind = NSTextField(labelWithString: "")
    private let detach = NSButton(checkboxWithTitle: "Detached: check it out on no branch", target: nil, action: nil)
    private var baseRow: NSView!
    private var folder: NSTextField!
    private var folderEdited = false
    private var then: NSPopUpButton!
    private var prompt: NSTextField!

    init(repo: Go.worktree.Repo, owner: WindowController) {
        self.repo = repo
        self.owner = owner
        f = FormSheet(title: "New Worktree",
                      subtitle: "Another checkout of \(repo.name), on its own branch in its own folder: what runs there can’t touch your files here. Nothing runs until you confirm.")
        super.init()
        branch.placeholderString = "New branch name, or pick one"
        branch.addItems(withObjectValues: Worktrees.free(repo))
        branch.completes = true
        branch.numberOfVisibleItems = 12
        branch.delegate = self
        let branchRow = NSStackView(views: [branch, kind])
        branchRow.orientation = .vertical
        branchRow.alignment = .leading
        branchRow.spacing = 3
        kind.font = .systemFont(ofSize: 11)
        kind.textColor = .secondaryLabelColor
        f.field("Branch", branchRow)
        branch.widthAnchor.constraint(equalTo: branchRow.widthAnchor).isActive = true
        detach.target = self
        detach.action = #selector(detachChanged)
        detach.toolTip = "Git allows a branch in one worktree at a time. Detached checks out a branch (even one checked out elsewhere) or any commit, for a look: commits made there aren’t on any branch."
        f.add(detach)

        base.addItems(withObjectValues: repo.branches)
        base.stringValue = Worktrees.current(repo)
        base.placeholderString = "HEAD"
        base.completes = true
        base.numberOfVisibleItems = 12
        baseRow = f.field("From", base, help: "The branch or commit the new branch starts at.")

        folder = f.text(repo.parent + "/")
        folder.delegate = self
        f.field("Folder", folder, help: "worktree-dir in config.toml changes where these go.")

        then = f.popup(["Open a Tab", "Start Claude Code in a Tab", "Start Claude Code in the Background"])
        then.target = self
        then.action = #selector(thenChanged)
        f.field("Then", then)
        prompt = f.text(placeholder: "First prompt for Claude Code (optional)")
        f.field("Prompt", prompt)
        f.buttons([("Create…", true, { [weak self] in self?.create() })])
        f.onClose = { [weak self] in Self.open.removeAll { $0 === self } }
        update()
        thenChanged()
    }

    func show() {
        Self.open.append(self)
        f.present(in: owner.window, focus: branch)
    }

    private var name: String { branch.stringValue.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var detached: Bool { detach.state == .on }
    private var isNew: Bool { !detached && !repo.branches.contains(name) }

    @objc private func detachChanged() {
        branch.removeAllItems()
        branch.addItems(withObjectValues: detached ? repo.branches : Worktrees.free(repo))
        branch.placeholderString = detached ? "A branch or commit" : "New branch name, or pick one"
        update()
    }

    private func update() {
        let n = name
        baseRow.isHidden = !isNew
        if detached {
            kind.stringValue = n.isEmpty ? "Pick a branch, or type a commit." : "Checked out on no branch: commits made there aren’t on any branch."
        } else if n.isEmpty {
            kind.stringValue = "Type a new branch’s name, or pick a branch to check out."
        } else if isNew {
            kind.stringValue = "A new branch."
        } else if let w = repo.worktrees.first(where: { $0.branch == n }) {
            kind.stringValue = "Already checked out in \(WindowModel.prettyPath(w.path))."
        } else {
            kind.stringValue = "An existing branch: it’s checked out there."
        }
        if !folderEdited { folder.stringValue = n.isEmpty ? repo.parent + "/" : Worktrees.suggest(repo, branch: n) }
    }

    @objc private func thenChanged() { prompt.isEnabled = then.indexOfSelectedItem != 0 }

    func controlTextDidChange(_ note: Notification) {
        if note.object as? NSTextField === folder {
            folderEdited = true
        } else {
            update()
        }
    }

    func comboBoxSelectionDidChange(_ note: Notification) {
        // The new selection isn't in stringValue yet.
        DispatchQueue.main.async { [weak self] in self?.update() }
    }

    private func create() {
        f.error = nil
        let req = Go.worktree.AddRequest(dir: repo.current, branch: name, new: isNew,
                                         base: isNew ? base.stringValue.trimmingCharacters(in: .whitespaces) : "",
                                         detach: detached,
                                         path: folder.stringValue.trimmingCharacters(in: .whitespaces))
        let then = WorktreeUI.Then(rawValue: self.then.indexOfSelectedItem) ?? .tab
        let prompt = self.prompt.stringValue
        Task {
            let plan: Go.worktree.AddPlan
            do { plan = try await Services.worktree.PlanAdd(req) } catch {
                f.error = error.localizedDescription
                return
            }
            let what = plan.detach ? "\(plan.base) on no branch (detached)"
                : plan.new ? "a new branch \(plan.branch) from \(plan.base)" : "the branch \(plan.branch)"
            Confirm.show(in: f.panel, title: "Create worktree \(plan.detach ? plan.base : plan.branch)?",
                         message: "Checks out \(what) in \(WindowModel.prettyPath(plan.path)). wtf runs:",
                         items: [plan.command], button: "Create", destructive: false) { [weak self] in
                self?.add(req, then: then, prompt: prompt)
            }
        }
    }

    private func add(_ req: Go.worktree.AddRequest, then: WorktreeUI.Then, prompt: String) {
        f.button("Create…")?.isEnabled = false
        Task {
            do {
                let w = try await Services.worktree.Add(req)
                f.close()
                NotificationCenter.default.post(name: .worktreesChanged, object: nil)
                WorktreeUI.opened(w.path, then: then, prompt: prompt, owner: owner)
            } catch {
                f.button("Create…")?.isEnabled = true
                f.error = error.localizedDescription
            }
        }
    }
}

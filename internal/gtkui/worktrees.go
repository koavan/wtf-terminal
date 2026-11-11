//go:build linux

package gtkui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/uicore"
	"github.com/dayanraj/wtf/internal/worktree"
)

// Git worktrees (#37): New Worktree…, Open Worktree… and Remove Worktree…
// for the repository a folder is in. Every change is shown as the git
// command first and runs only on your confirm; removing never deletes a
// branch, and git's own refusal (changed or untracked files) asks again
// before --force. Like the macOS app's WorktreeSheets.swift.

// worktreeHere is the focused pane's folder: where the commands ask from.
func (w *Window) worktreeHere() string {
	if ps := w.model.ActivePane(); ps != nil {
		return ps.Cwd
	}
	return ""
}

// withRepo reads the repository dir is in off the main loop, then runs f;
// says why not otherwise.
func (w *Window) withRepo(dir string, f func(worktree.Repo)) {
	if dir == "" {
		w.toast("Waiting for the shell’s folder")
		return
	}
	svc := w.app.be.Worktree
	go func() {
		r, err := svc.Repo(dir)
		onMainAsync(func() {
			if err != nil {
				w.toast(uicore.PrettyPath(dir, w.app.home) + ": " + err.Error())
				return
			}
			f(r)
		})
	}()
}

// worktreesChanged: a worktree was added or removed; the Files tabs read
// their repository again.
func (a *App) worktreesChanged() {
	for _, w := range a.windows {
		if f, ok := w.sidePane().(*filesTab); ok {
			f.reload()
		}
	}
}

// worktreeThen is what happens once a new worktree is there.
type worktreeThen int

const (
	worktreeTab worktreeThen = iota
	worktreeClaude
	worktreeBackground
)

func (w *Window) worktreeOpened(path string, then worktreeThen, prompt string) {
	prompt = strings.TrimSpace(prompt)
	switch then {
	case worktreeClaude:
		w.worktreeTab(path, agentCommand(prompt))
	case worktreeBackground:
		w.startAgentIn(path, prompt, true)
	default:
		w.worktreeTab(path, "")
	}
}

// worktreeTab opens a tab in path, right after the active one and in its group.
func (w *Window) worktreeTab(path, command string) {
	opts := uicore.OpenOptions{Cwd: &path, Command: command}
	if t, ok := w.model.Active(); ok {
		at := w.model.Index(t.ID) + 1
		opts.At, opts.Group = &at, t.GroupID
	}
	w.model.Open(opts)
	w.win.Present()
}

// worktreeGo goes to a tab whose shell is in path (any window you can
// see), else opens one.
func (w *Window) worktreeGo(path string) {
	for _, ow := range w.app.visibleWindows() {
		var ids []int
		for id, ps := range ow.model.Panes {
			if worktree.Inside(ps.Cwd, path) {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		for _, id := range ids {
			if t, ok := ow.model.TabOf(id); ok {
				ow.model.Activate(t.ID)
				ow.model.FocusPane(id)
				ow.win.Present()
				return
			}
		}
	}
	w.worktreeTab(path, "")
}

// shellsIn counts the shells (in windows you can see) whose folder is in path.
func (a *App) shellsIn(path string) int {
	n := 0
	for _, w := range a.visibleWindows() {
		for _, ps := range w.model.Panes {
			if worktree.Inside(ps.Cwd, path) {
				n++
			}
		}
	}
	return n
}

// confirmPlan asks before a change, showing it. Cancel runs cancel (may be nil).
func (w *Window) confirmPlan(parent *gtk.Window, title, body, button string, destructive bool, ok, cancel func()) {
	d := adw.NewMessageDialog(parent, title, body)
	d.AddResponse("cancel", "Cancel")
	d.AddResponse("ok", button)
	if destructive {
		d.SetResponseAppearance("ok", adw.ResponseDestructive)
		d.SetDefaultResponse("cancel")
	} else {
		d.SetResponseAppearance("ok", adw.ResponseSuggested)
		d.SetDefaultResponse("ok")
	}
	d.SetCloseResponse("cancel")
	d.ConnectResponse(func(r string) {
		if r == "ok" {
			ok()
		} else if cancel != nil {
			cancel()
		}
	})
	w.app.debugLog("confirm: %s", title)
	d.Present()
}

// New

// newWorktree: New Worktree… for the repository dir is in.
func (w *Window) newWorktree(dir string) { w.withRepo(dir, w.newWorktreeDialog) }

// newWorktreeDialog: a branch (an existing one, or a new one from a branch
// or commit), its folder, and what to do once it's there. Create shows the
// git command and runs it only on confirm.
func (w *Window) newWorktreeDialog(repo worktree.Repo) {
	f := newSSHForm(w, "New Worktree",
		"Another checkout of "+repo.Name+", on its own branch in its own folder: what runs there can’t touch your files here. Nothing runs until you confirm.", 460)
	branchBox, branch := sshCombo(uicore.WorktreeFree(repo), "", "New branch name, or pick one")
	kind := f.label("", false)
	branchCol := gtk.NewBox(gtk.OrientationVertical, 3)
	branchCol.Append(branchBox)
	branchCol.Append(kind)
	f.field("Branch", branchCol, "")
	detach := f.check("Detached: check it out on no branch", false)
	detach.SetTooltipText("Git allows a branch in one worktree at a time. Detached checks out a branch (even one checked out elsewhere) or any commit, for a look: commits made there aren’t on any branch.")
	f.add(detach)
	baseBox, base := sshCombo(repo.Branches, uicore.WorktreeCurrent(repo), "HEAD")
	baseRow := f.field("From", baseBox, "The branch or commit the new branch starts at.")
	folder := f.entry(repo.Parent+"/", "")
	f.field("Folder", folder, "worktree-dir in config.toml changes where these go.")
	then := gtk.NewDropDownFromStrings([]string{"Open a Tab", "Start Claude Code in a Tab", "Start Claude Code in the Background"})
	f.field("Then", then, "")
	prompt := f.entry("", "First prompt for Claude Code (optional)")
	f.field("Prompt", prompt, "")

	name := func() string { return strings.TrimSpace(branch.Text()) }
	isNew := func() bool { return !detach.Active() && !slices.Contains(repo.Branches, name()) }
	folderEdited, settingFolder := false, false
	update := func() {
		n := name()
		baseRow.root.SetVisible(isNew())
		switch {
		case detach.Active() && n == "":
			kind.SetText("Pick a branch, or type a commit.")
		case detach.Active():
			kind.SetText("Checked out on no branch: commits made there aren’t on any branch.")
		case n == "":
			kind.SetText("Type a new branch’s name, or pick a branch to check out.")
		case isNew():
			kind.SetText("A new branch.")
		default:
			kind.SetText("An existing branch: it’s checked out there.")
			for _, wt := range repo.Worktrees {
				if wt.Branch == n {
					kind.SetText("Already checked out in " + uicore.PrettyPath(wt.Path, w.app.home) + ".")
				}
			}
		}
		if !folderEdited {
			settingFolder = true
			if n == "" {
				folder.SetText(repo.Parent + "/")
			} else {
				folder.SetText(uicore.WorktreeSuggest(repo, n))
			}
			settingFolder = false
		}
	}
	branch.ConnectChanged(update)
	// Detached offers every branch, even those checked out elsewhere.
	detach.ConnectToggled(func() {
		text := branch.Text()
		choices, hint := uicore.WorktreeFree(repo), "New branch name, or pick one"
		if detach.Active() {
			choices, hint = repo.Branches, "A branch or commit"
		}
		branchCol.Remove(branchBox)
		branchBox, branch = sshCombo(choices, text, hint)
		branchCol.Prepend(branchBox)
		branch.ConnectChanged(update)
		update()
	})
	folder.ConnectChanged(func() {
		if !settingFolder {
			folderEdited = true
		}
	})
	thenChanged := func() { prompt.SetSensitive(then.Selected() != 0) }
	then.NotifyProperty("selected", thenChanged)

	svc := w.app.be.Worktree
	f.button("cancel", "Cancel", adw.ResponseDefault, nil)
	f.button("create", "Create…", adw.ResponseSuggested, func() {
		req := worktree.AddRequest{Dir: repo.Current, Branch: name(), New: isNew(), Detach: detach.Active(), Path: strings.TrimSpace(folder.Text())}
		if req.New {
			req.Base = strings.TrimSpace(base.Text())
		}
		how := worktreeThen(then.Selected())
		first := prompt.Text()
		go func() {
			plan, err := svc.PlanAdd(req)
			onMainAsync(func() {
				if err != nil {
					f.fail(err.Error())
					return
				}
				what, title := "the branch "+plan.Branch, plan.Branch
				switch {
				case plan.Detach:
					what, title = plan.Base+" on no branch (detached)", plan.Base
				case plan.New:
					what = "a new branch " + plan.Branch + " from " + plan.Base
				}
				w.confirmPlan(&w.win.Window, "Create worktree "+title+"?",
					"Checks out "+what+" in "+uicore.PrettyPath(plan.Path, w.app.home)+". wtf runs:\n\n"+plan.Command,
					"Create", false, func() {
						go func() {
							wt, err := svc.Add(req)
							onMainAsync(func() {
								if err != nil {
									f.fail(err.Error())
									return
								}
								f.close()
								w.app.worktreesChanged()
								w.worktreeOpened(wt.Path, how, first)
							})
						}()
					}, func() { f.d.Present() })
			})
		}()
	})
	update()
	thenChanged()
	f.present(branch)
}

// Open

// openWorktree: Open Worktree… for the repository dir is in.
func (w *Window) openWorktree(dir string) {
	w.withRepo(dir, func(repo worktree.Repo) {
		var trees []worktree.Worktree
		var titles []string
		sel := -1
		for _, wt := range repo.Worktrees {
			if wt.Prunable {
				continue
			}
			if sel < 0 && wt.Path != repo.Current {
				sel = len(trees)
			}
			trees = append(trees, wt)
			titles = append(titles, uicore.WorktreeTitle(wt, w.app.home))
		}
		f := newSSHForm(w, "Open Worktree", repo.Name+"’s checkouts. Open goes to a tab already in it, or opens one.", 460)
		list := gtk.NewDropDownFromStrings(titles)
		if sel >= 0 {
			list.SetSelected(uint(sel))
		}
		f.field("Worktree", list, "")
		pick := func() (worktree.Worktree, bool) {
			i := int(list.Selected())
			if i < 0 || i >= len(trees) {
				return worktree.Worktree{}, false
			}
			return trees[i], true
		}
		f.button("cancel", "Cancel", adw.ResponseDefault, nil)
		f.button("claude", "Start Claude Code", adw.ResponseDefault, func() {
			if wt, ok := pick(); ok {
				f.close()
				w.worktreeTab(wt.Path, agentCommand(""))
			}
		})
		f.button("open", "Open", adw.ResponseSuggested, func() {
			if wt, ok := pick(); ok {
				f.close()
				w.worktreeGo(wt.Path)
			}
		})
		f.present(list)
	})
}

// Remove

// removeWorktree: Remove Worktree… for the repository dir is in.
func (w *Window) removeWorktree(dir string) {
	w.withRepo(dir, func(repo worktree.Repo) {
		var trees []worktree.Worktree
		var titles []string
		sel := 0
		for _, wt := range repo.Worktrees {
			if wt.Main {
				continue
			}
			if wt.Path == repo.Current {
				sel = len(trees)
			}
			trees = append(trees, wt)
			titles = append(titles, uicore.WorktreeTitle(wt, w.app.home))
		}
		if len(trees) == 0 {
			w.toast(repo.Name + " has no other worktrees")
			return
		}
		f := newSSHForm(w, "Remove Worktree",
			"Removes a worktree’s folder and git’s record of it. Its branch stays. You’ll see the command, and what isn’t committed there, before anything runs.", 460)
		list := gtk.NewDropDownFromStrings(titles)
		list.SetSelected(uint(sel))
		f.field("Worktree", list, "")
		f.button("cancel", "Cancel", adw.ResponseDefault, nil)
		f.button("remove", "Remove…", adw.ResponseSuggested, func() {
			i := int(list.Selected())
			if i < 0 || i >= len(trees) {
				return
			}
			f.close()
			w.confirmRemoveWorktree(trees[i].Path, repo.Main)
		})
		f.present(list)
	})
}

// confirmRemoveWorktree shows what removing the worktree at path runs and
// loses, then removes it on confirm. When git refuses, asks again before
// --force.
func (w *Window) confirmRemoveWorktree(path, repo string) {
	svc := w.app.be.Worktree
	go func() {
		plan, err := svc.PlanRemove(repo, path)
		onMainAsync(func() {
			if err != nil {
				w.toast(err.Error())
				return
			}
			var b strings.Builder
			if plan.Prunable {
				b.WriteString("Its folder is already gone: this only drops git’s record of it.")
			} else {
				b.WriteString("Removes the folder " + uicore.PrettyPath(plan.Path, w.app.home) + ".")
			}
			if plan.Branch != "" {
				b.WriteString(" The branch " + plan.Branch + " stays.")
			}
			if len(plan.Changes) > 0 {
				b.WriteString("\n\nNot committed there (git won’t remove it unless you then force it):\n  " + strings.Join(plan.Changes, "\n  "))
				if plan.More > 0 {
					fmt.Fprintf(&b, "\n  and %d more", plan.More)
				}
			}
			if n := w.app.shellsIn(path); n > 0 {
				fmt.Fprintf(&b, "\n\n%d %s in that folder; they stay open.", n, map[bool]string{true: "shell is", false: "shells are"}[n == 1])
			}
			b.WriteString("\n\nwtf runs:\n\n" + plan.Command)
			name := plan.Branch
			if name == "" {
				name = uicore.BaseName(plan.Path)
			}
			w.confirmPlan(&w.win.Window, "Remove worktree "+name+"?", b.String(), "Remove", true,
				func() { w.runRemoveWorktree(path, repo, false) }, nil)
		})
	}()
}

func (w *Window) runRemoveWorktree(path, repo string, force bool) {
	svc := w.app.be.Worktree
	go func() {
		r, err := svc.Remove(repo, path, force)
		onMainAsync(func() {
			switch {
			case err != nil:
				w.toast(err.Error())
			case r.Refused != "":
				w.confirmPlan(&w.win.Window, "Git won’t remove it",
					"git says: "+r.Refused+"\n\nRemove it anyway? Uncommitted changes and untracked files in it are lost. The branch stays. wtf runs:\n\n"+
						"git worktree remove --force "+uicore.ShellQuote(path),
					"Remove Anyway", true, func() { w.runRemoveWorktree(path, repo, true) }, nil)
			default:
				w.app.worktreesChanged()
				w.toast("Removed the worktree " + uicore.PrettyPath(path, w.app.home))
			}
		})
	}()
}

// Menus

// worktreeMenu is the worktree items for a menu about repo (Files tab).
func (w *Window) worktreeMenu(repo worktree.Repo) []*menuItem {
	items := []*menuItem{item("New Worktree…", func() { w.newWorktreeDialog(repo) })}
	var open, remove []*menuItem
	for _, wt := range repo.Worktrees {
		path, title := wt.Path, uicore.WorktreeTitle(wt, w.app.home)
		if wt.Path != repo.Current && !wt.Prunable {
			open = append(open, item(title, func() { w.worktreeGo(path) }))
		}
		if !wt.Main {
			main := repo.Main
			remove = append(remove, item(title+"…", func() { w.confirmRemoveWorktree(path, main) }))
		}
	}
	if len(open) > 0 {
		items = append(items, submenu("Open Worktree", open))
	}
	if len(remove) > 0 {
		items = append(items, submenu("Remove Worktree", remove))
	}
	return items
}

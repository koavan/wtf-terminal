//go:build linux

package gtkui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/dayanraj/wtf/internal/about"
	"github.com/dayanraj/wtf/internal/tools"
	"github.com/dayanraj/wtf/internal/uicore"
)

func init() {
	extraCSS = append(extraCSS, `
.wtf-sheet-body { padding: 4px 24px 18px; }
.wtf-sheet-sub { font-size: 0.92em; }
.wtf-tools-welcome { background: alpha(@accent_color, 0.12); border-radius: 8px; padding: 8px 10px; }
.wtf-tools-name { font-weight: bold; }
.wtf-tools-muted { font-size: 0.9em; opacity: 0.7; }
.wtf-tools-mono { font-family: monospace; }
.wtf-tools-ok { background: @success_color; }
.wtf-tools-missing { background: alpha(currentColor, 0.3); }
.wtf-tools-dot { margin-top: 6px; }
.wtf-tools-code text { background: alpha(currentColor, 0.05); }
.wtf-tools-code { font-family: monospace; font-size: 0.92em; }
.wtf-tools-key { font-weight: 600; opacity: 0.7; }
.wtf-about-name { font-size: 1.8em; font-weight: bold; }
.wtf-about-chip { font-size: 0.85em; min-height: 22px; padding: 0 9px; border-radius: 11px; }
.wtf-about-path { padding: 0 4px; min-height: 0; }
`)
}

// toolsSheet is a modal window over a terminal window whose body is
// rebuilt as it changes, with a row of buttons: the Command-Line Tools and
// About dialogs. One at a time. Like the macOS app's PagedSheet.
type toolsSheet struct {
	w     *Window
	win   *adw.Window
	title *adw.WindowTitle
	body  *gtk.Box
	// The body's width, for wrapping.
	inner int
}

var toolsShownSheet *toolsSheet

func newToolsSheet(w *Window, title string, width int) *toolsSheet {
	if toolsShownSheet != nil {
		toolsShownSheet.win.Close()
	}
	s := &toolsSheet{w: w, inner: width - 48}
	toolsShownSheet = s
	s.win = adw.NewWindow()
	s.win.SetTransientFor(&w.win.Window)
	s.win.SetModal(true)
	s.win.SetResizable(false)
	s.win.SetDefaultSize(width, -1)
	s.win.SetTitle(title)
	s.title = adw.NewWindowTitle(title, "")
	head := adw.NewHeaderBar()
	head.SetTitleWidget(s.title)
	s.body = gtk.NewBox(gtk.OrientationVertical, 10)
	s.body.AddCSSClass("wtf-sheet-body")
	s.body.SetSizeRequest(width, -1)
	view := adw.NewToolbarView()
	view.AddTopBar(head)
	view.SetContent(s.body)
	s.win.SetContent(view)
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(func(keyval, _ uint, _ gdk.ModifierType) bool {
		if keyval == gdk.KEY_Escape {
			s.win.Close()
			return true
		}
		return false
	})
	s.win.AddController(keys)
	s.win.ConnectCloseRequest(func() bool {
		if toolsShownSheet == s {
			toolsShownSheet = nil
		}
		w.focusActive()
		return false
	})
	return s
}

func (s *toolsSheet) close() { s.win.Close() }

func (s *toolsSheet) clear() {
	for c := s.body.FirstChild(); c != nil; c = s.body.FirstChild() {
		s.body.Remove(c)
	}
}

// label is wrapping text; classes style it (dim-label, error…).
func (s *toolsSheet) label(text string, classes ...string) *gtk.Label {
	l := gtk.NewLabel(text)
	l.SetXAlign(0)
	l.SetWrap(true)
	l.SetWrapMode(pango.WrapWordChar)
	l.SetMaxWidthChars(1)
	l.SetHExpand(true)
	for _, c := range classes {
		l.AddCSSClass(c)
	}
	return l
}

// subtitle is the line under the title, in the body.
func (s *toolsSheet) subtitle(text string) {
	if text != "" {
		s.body.Append(s.label(text, "dim-label", "wtf-sheet-sub"))
	}
}

// codeBox is monospace text to read and select, scrolling past a height.
func (s *toolsSheet) codeBox(text string, maxHeight int) *gtk.Frame {
	tv := gtk.NewTextView()
	tv.SetEditable(false)
	tv.SetCursorVisible(false)
	tv.SetMonospace(true)
	tv.SetWrapMode(gtk.WrapWordChar)
	tv.SetTopMargin(6)
	tv.SetBottomMargin(6)
	tv.SetLeftMargin(8)
	tv.SetRightMargin(8)
	tv.Buffer().SetText(text)
	tv.AddCSSClass("wtf-tools-code")
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetPropagateNaturalHeight(true)
	scroll.SetMaxContentHeight(maxHeight)
	scroll.SetChild(tv)
	f := gtk.NewFrame("")
	f.SetChild(scroll)
	return f
}

// button is a button in the bottom row.
func (s *toolsSheet) button(label string, run func()) *gtk.Button {
	b := gtk.NewButtonWithLabel(label)
	b.ConnectClicked(run)
	return b
}

// buttons ends the body with a row: start on the left, end on the right;
// def is the default (Enter), shown as suggested.
func (s *toolsSheet) buttons(start, end []*gtk.Button, def *gtk.Button) {
	row := gtk.NewBox(gtk.OrientationHorizontal, 8)
	row.SetMarginTop(6)
	for _, b := range start {
		row.Append(b)
	}
	spacer := gtk.NewBox(gtk.OrientationHorizontal, 0)
	spacer.SetHExpand(true)
	row.Append(spacer)
	for _, b := range end {
		row.Append(b)
	}
	s.body.Append(row)
	if def != nil {
		def.AddCSSClass("suggested-action")
		s.win.SetDefaultWidget(def)
		def.GrabFocus()
	}
}

// toolsLink is a flat button that looks like a link.
func toolsLink(label, tip string, run func()) *gtk.Button {
	b := gtk.NewButtonWithLabel(label)
	b.AddCSSClass("flat")
	b.AddCSSClass("link")
	b.SetTooltipText(tip)
	b.SetHAlign(gtk.AlignStart)
	b.ConnectClicked(run)
	return b
}

// toolsOpenURL opens a web page in the browser.
func toolsOpenURL(w *Window, url string) {
	gtk.NewURILauncher(url).Launch(context.Background(), &w.win.Window, nil)
}

// Command-line tools

// toolsDialog is Command-Line Tools: which programs the side pane runs
// are there, and installing the missing ones — the exact command or script
// is shown first, then run in a new tab (never by wtf itself). Like
// ToolsDialog.svelte and the macOS app's ToolsDialog.
type toolsDialog struct {
	*toolsSheet
	list     []tools.Status
	err      string
	checking bool
	welcome  []string
	focus    string
	// The tool being installed (the install page), and the way picked.
	installing *tools.Status
	method     int
	busy       bool
}

// showTools opens Command-Line Tools; focus is a tool id to open at its
// install page ("" for the list).
func (w *Window) showTools(focus string) { w.openTools(focus, nil) }

func (w *Window) openTools(focus string, welcome []string) {
	d := &toolsDialog{toolsSheet: newToolsSheet(w, "Command-Line Tools", 600), focus: focus, welcome: welcome}
	d.render()
	d.win.Present()
	d.check()
}

// toolsFirstRun says, once ever (per state folder) after the window opens,
// which tools aren't installed. Not during self-checks.
func (w *Window) toolsFirstRun() {
	if w.app.debug != "" {
		return
	}
	svc := w.app.be.Tools
	go func() {
		names := svc.FirstRun("")
		onMainAsync(func() {
			if len(names) == 0 || toolsShownSheet != nil || w.closing {
				return
			}
			w.openTools("", names)
		})
	}()
}

// shellPATH is the focused shell's PATH, where tools are looked for first.
func (d *toolsDialog) shellPATH() string {
	if ps := d.w.model.ActivePane(); ps != nil && ps.Env != nil {
		return ps.Env["PATH"]
	}
	return ""
}

func (d *toolsDialog) check() {
	d.checking = true
	d.render()
	svc, path := d.w.app.be.Tools, d.shellPATH()
	go func() {
		list := svc.Check(path)
		onMainAsync(func() {
			if toolsShownSheet != d.toolsSheet {
				return
			}
			d.list, d.err, d.checking = list, "", false
			for i := range d.list {
				if t := &d.list[i]; t.ID == d.focus && len(t.Methods) > 0 {
					d.installing, d.method = t, 0
				}
			}
			d.focus = ""
			d.render()
		})
	}()
}

func (d *toolsDialog) render() {
	d.clear()
	if d.installing != nil {
		d.renderInstall(*d.installing)
		return
	}
	d.title.SetTitle("Command-Line Tools")
	d.subtitle("Programs side-pane features run. wtf looks on the focused shell's PATH first, then where installers put them.")
	if n := len(d.welcome); n > 0 {
		verb := "aren't"
		if n == 1 {
			verb = "isn't"
		}
		l := d.label(fmt.Sprintf("Welcome to wtf. %s %s installed yet. The terminal works without them; the features below that use them say so when you open them. Install any now, or later from the palette (Command-Line Tools) or About wtf.",
			strings.Join(d.welcome, ", "), verb), "wtf-tools-welcome")
		d.body.Append(l)
	}
	if d.list != nil {
		rows := gtk.NewBox(gtk.OrientationVertical, 10)
		for i, t := range d.list {
			if i > 0 {
				rows.Append(gtk.NewSeparator(gtk.OrientationHorizontal))
			}
			rows.Append(d.row(t))
		}
		scroll := gtk.NewScrolledWindow()
		scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
		scroll.SetPropagateNaturalHeight(true)
		h := 420
		if wh := d.w.win.Height(); wh > 0 && wh*6/10 < h {
			h = wh * 6 / 10
		}
		scroll.SetMaxContentHeight(h)
		scroll.SetChild(rows)
		d.body.Append(scroll)
	} else if d.err != "" {
		d.body.Append(d.label(d.err, "error"))
	} else {
		d.body.Append(d.label("Checking…", "dim-label"))
	}
	if d.list != nil && d.err != "" {
		d.body.Append(d.label(d.err, "error"))
	}
	manual := d.button("Manual", func() {
		d.close()
		d.w.app.showManual("tools")
	})
	again := d.button("Check Again", d.check)
	if d.checking {
		again.SetLabel("Checking…")
		again.SetSensitive(false)
	}
	done := d.button("Done", d.close)
	d.buttons([]*gtk.Button{manual}, []*gtk.Button{again, done}, done)
}

// row is one tool: a dot (green when all there), name and version, where
// it is, what uses it, and Install… or its website.
func (d *toolsDialog) row(t tools.Status) *gtk.Box {
	dot := gtk.NewBox(gtk.OrientationHorizontal, 0)
	dot.AddCSSClass("wtf-dot")
	dot.AddCSSClass("wtf-tools-dot")
	if t.Found && len(t.Missing) == 0 {
		dot.AddCSSClass("wtf-tools-ok")
	} else {
		dot.AddCSSClass("wtf-tools-missing")
	}
	dot.SetVAlign(gtk.AlignStart)

	text := gtk.NewBox(gtk.OrientationVertical, 2)
	text.SetHExpand(true)
	head := gtk.NewBox(gtk.OrientationHorizontal, 8)
	name := gtk.NewLabel(t.Name)
	name.AddCSSClass("wtf-tools-name")
	head.Append(name)
	state := gtk.NewLabel("")
	state.SetXAlign(0)
	state.SetEllipsize(pango.EllipsizeEnd)
	state.AddCSSClass("wtf-tools-muted")
	if t.Found {
		state.SetText(t.Version)
		state.AddCSSClass("wtf-tools-mono")
		state.SetSelectable(true)
	} else {
		state.SetMarkup("<i>not installed</i>")
	}
	head.Append(state)
	text.Append(head)
	muted := func(s, tip string, classes ...string) {
		l := d.label(s, append([]string{"wtf-tools-muted"}, classes...)...)
		l.SetTooltipText(tip)
		text.Append(l)
	}
	if t.Found {
		muted(uicore.PrettyPath(t.Path, d.w.app.home), t.Path, "wtf-tools-mono")
		if len(t.Missing) > 0 {
			l := d.label("", "wtf-tools-muted")
			l.SetMarkup("<i>" + escapeMarkup("Not found: "+strings.Join(t.Missing, ", ")) + "</i>")
			text.Append(l)
		}
	}
	muted("Used by: "+strings.Join(t.UsedBy, " · "), "")
	if !t.Found && len(t.Methods) == 0 {
		if t.BuiltIn == "" {
			muted("Install it with your system's package manager.", "")
		} else {
			muted("Part of "+t.BuiltIn+"; it may have been removed.", "")
		}
	}

	side := gtk.NewBox(gtk.OrientationVertical, 4)
	side.SetVAlign(gtk.AlignStart)
	if len(t.Methods) > 0 {
		b := gtk.NewButtonWithLabel("Install…")
		b.AddCSSClass("suggested-action")
		b.ConnectClicked(func() {
			d.installing, d.method, d.err = &t, 0, ""
			d.render()
		})
		side.Append(b)
	}
	if t.Homepage != "" {
		l := toolsLink("Website", t.Homepage, func() { toolsOpenURL(d.w, t.Homepage) })
		l.SetHAlign(gtk.AlignEnd)
		side.Append(l)
	}

	row := gtk.NewBox(gtk.OrientationHorizontal, 10)
	row.SetMarginEnd(8)
	row.Append(dot)
	row.Append(text)
	row.Append(side)
	return row
}

// renderInstall shows a tool's ways to install, and exactly what the
// chosen one runs.
func (d *toolsDialog) renderInstall(t tools.Status) {
	d.title.SetTitle("Install " + t.Name)
	d.subtitle(t.About)
	methods := t.Methods
	if len(methods) > 1 {
		var first *gtk.CheckButton
		for i, m := range methods {
			label := m.Label
			if i == 0 {
				label += "  (recommended)"
			}
			r := gtk.NewCheckButtonWithLabel(label)
			if first == nil {
				first = r
			} else {
				r.SetGroup(first)
			}
			r.SetActive(i == d.method)
			r.ConnectToggled(func() {
				if r.Active() && d.method != i {
					d.method = i
					d.render()
				}
			})
			d.body.Append(r)
		}
	}
	m := methods[min(d.method, len(methods)-1)]
	if m.Script != "" {
		d.body.Append(d.label("wtf writes this script to its cache folder and runs it with sh in a new tab:", "dim-label"))
		d.body.Append(d.codeBox(m.Script, 240))
	} else {
		d.body.Append(d.label("Runs in a new tab:", "dim-label"))
		d.body.Append(d.codeBox(m.Command, 120))
	}
	facts := [][2]string{{"Installs to", m.Dest}, {"Checked", m.Verify}}
	if m.Sudo {
		facts = append(facts, [2]string{"Password", "Uses sudo; it asks for your password in the tab."})
	}
	grid := gtk.NewGrid()
	grid.SetColumnSpacing(12)
	grid.SetRowSpacing(4)
	for i, f := range facts {
		k := gtk.NewLabel(f[0])
		k.SetXAlign(0)
		k.SetYAlign(0)
		k.SetSizeRequest(84, -1)
		k.AddCSSClass("wtf-tools-key")
		grid.Attach(k, 0, i, 1, 1)
		grid.Attach(d.label(f[1]), 1, i, 1, 1)
	}
	d.body.Append(grid)
	if m.Note != "" {
		n := d.label(m.Note, "dim-label")
		n.SetSelectable(true)
		d.body.Append(n)
	}
	if d.err != "" {
		d.body.Append(d.label(d.err, "error"))
	}
	back := d.button("Back", func() {
		d.installing, d.err = nil, ""
		d.render()
	})
	cancel := d.button("Cancel", d.close)
	run := d.button("Run in New Tab", func() { d.install(t, m) })
	run.SetSensitive(!d.busy)
	d.buttons([]*gtk.Button{back}, []*gtk.Button{cancel, run}, run)
}

// install gets the command (writing the script, if any) and runs it in a
// new tab in the focused shell's folder.
func (d *toolsDialog) install(t tools.Status, m tools.Method) {
	if d.busy {
		return
	}
	d.busy = true
	svc, path := d.w.app.be.Tools, d.shellPATH()
	go func() {
		cmd, err := svc.Install(t.ID, m.ID, path)
		onMainAsync(func() {
			if toolsShownSheet != d.toolsSheet {
				return
			}
			if err != nil {
				d.err, d.busy = err.Error(), false
				d.render()
				return
			}
			w := d.w
			d.close()
			var cwd *string
			if ps := w.model.ActivePane(); ps != nil {
				cwd = &ps.Cwd
			}
			w.model.Open(uicore.OpenOptions{Cwd: cwd, Command: cmd})
			w.toast("Installing " + t.Name + " — check again from Command-Line Tools when it's done")
		})
	}()
}

// About

// aboutChips are the features the About dialog links to in the manual.
var aboutChips = [][2]string{
	{"Tabs, groups & splits", "tabs"}, {"Command blocks", "shell-integration"}, {"Log-friendly output", "logs"},
	{"Pinned output", "pins"}, {"Hide Secrets", "secrets"}, {"Recording", "recording"}, {"SSH", "ssh-hosts"},
	{"AWS profiles", "aws"}, {"Kubernetes", "kubernetes"}, {"Snippets & runbooks", "snippets"},
	{"Workspaces", "workspaces"}, {"Port forwards", "network"}, {"Broadcast input", "broadcast"},
}

// aboutDiagnostics is the version and system for a bug report: no file
// paths but the shell's name.
func aboutDiagnostics(i about.Info) string {
	built := ""
	if i.BuildDate != "" {
		built = ", built " + i.BuildDate
	}
	integration := "off"
	if i.ShellIntegration {
		integration = "on"
	}
	return strings.Join([]string{
		fmt.Sprintf("wtf %s (%s%s)", i.Version, i.Commit, built),
		i.OS + " · " + i.Arch,
		"Go " + i.GoVersion + " · native Linux (GTK 4, VTE)",
		"Shell: " + filepath.Base(i.Shell) + ", shell integration " + integration,
	}, "\n")
}

// showAbout opens About wtf: version, what it is, where its files are, and
// the way to the manual and the tools. Like AboutDialog.svelte and the
// macOS app's AboutDialog.
func (w *Window) showAbout() {
	s := newToolsSheet(w, "About wtf", 520)
	var info *about.Info
	var render func()
	render = func() {
		s.clear()
		// wtf's own icon once installed; a generic one when run from a build.
		icon := gtk.NewImageFromIconName(appID)
		if !gtk.IconThemeGetForDisplay(gdk.DisplayGetDefault()).HasIcon(appID) {
			icon = gtk.NewImageFromIconName("utilities-terminal-symbolic")
			icon.AddCSSClass("accent")
		}
		icon.SetPixelSize(64)
		hero := gtk.NewBox(gtk.OrientationVertical, 2)
		hero.SetVAlign(gtk.AlignCenter)
		name := s.label("wtf", "wtf-about-name")
		hero.Append(name)
		hero.Append(s.label("A terminal for all the What-The-Fuck moments", "dim-label"))
		if info != nil {
			v := "Version " + info.Version + " · " + info.Commit
			if info.BuildDate != "" {
				v += " · built " + info.BuildDate
			}
			l := s.label(v, "dim-label", "wtf-sheet-sub")
			l.SetTooltipText("The git commit this build was made from")
			hero.Append(l)
		}
		top := gtk.NewBox(gtk.OrientationHorizontal, 14)
		top.Append(icon)
		top.Append(hero)
		s.body.Append(top)
		s.body.Append(s.label("A browser-like terminal for DevOps work: tabs you can group, split and move between windows, an address bar that says which cluster and AWS account each shell points at, and a side pane for your files, SSH hosts, AWS profiles, Kubernetes clusters, snippets, workspaces, port forwards and processes."))

		chips := gtk.NewFlowBox()
		chips.SetSelectionMode(gtk.SelectionNone)
		chips.SetColumnSpacing(4)
		chips.SetRowSpacing(4)
		chips.SetMaxChildrenPerLine(8)
		for _, c := range aboutChips {
			b := gtk.NewButtonWithLabel(c[0])
			b.AddCSSClass("wtf-about-chip")
			b.SetTooltipText("Open the manual at " + c[0])
			topic := c[1]
			b.ConnectClicked(func() {
				s.close()
				w.app.showManual(topic)
			})
			chips.Append(b)
		}
		s.body.Append(chips)

		if i := info; i != nil {
			grid := gtk.NewGrid()
			grid.SetColumnSpacing(12)
			grid.SetRowSpacing(4)
			row := 0
			add := func(key string, value gtk.Widgetter) {
				k := gtk.NewLabel(key)
				k.SetXAlign(0)
				k.SetSizeRequest(80, -1)
				k.AddCSSClass("wtf-tools-key")
				grid.Attach(k, 0, row, 1, 1)
				grid.Attach(value, 1, row, 1, 1)
				row++
			}
			reveal := func(path, tip string) *gtk.Button {
				pretty := uicore.PrettyPath(path, w.app.home)
				b := toolsLink(pretty, tip, func() {
					svc := w.app.be.Files
					go func() {
						if err := svc.Reveal(path); err != nil {
							onMainAsync(func() { w.toast(pretty + " doesn’t exist yet") })
						}
					}()
				})
				b.AddCSSClass("wtf-about-path")
				return b
			}
			integration := "off"
			if i.ShellIntegration {
				integration = "on"
			}
			add("Shell", s.label(filepath.Base(i.Shell)+" ("+uicore.PrettyPath(i.Shell, w.app.home)+") · shell integration "+integration))
			add("System", s.label(i.OS+" · "+i.Arch))
			add("Built with", s.label("Go "+strings.TrimPrefix(i.GoVersion, "go")+" · GTK 4, libadwaita and VTE"))
			add("Settings", reveal(i.ConfigFile, "Show in Files"))
			add("Snippets", reveal(i.SnippetsFile, "Show in Files"))
			add("Session", reveal(i.StateDir, "Saved windows and tabs, port forwards"))
			add("Backups", reveal(i.CacheDir, "Copies of ssh, AWS and kubeconfig files kept before wtf changed them, and deleted SSH keys"))
			s.body.Append(grid)
		}
		s.body.Append(s.label("Native Linux build: GTK 4, libadwaita and VTE over wtf’s Go backend. Icons: Adwaita.", "dim-label", "wtf-sheet-sub"))

		diag := s.button("Copy Diagnostics", func() {
			if info == nil {
				return
			}
			w.win.Clipboard().SetText(aboutDiagnostics(*info))
			w.toast("Copied version and system details")
		})
		diag.SetSensitive(info != nil)
		diag.SetTooltipText("Version and system details for a bug report (no file paths)")
		toolsB := s.button("Tools", func() {
			s.close()
			w.showTools("")
		})
		toolsB.SetTooltipText("kubectl, the AWS CLI, git and OpenSSH: found or not, and installing them")
		var end []*gtk.Button
		if info != nil && info.Repository != "" {
			repo := info.Repository
			end = append(end, s.button("GitHub", func() { toolsOpenURL(w, repo) }))
		}
		end = append(end, s.button("Shortcuts", func() {
			s.close()
			w.app.showManual("shortcuts")
		}))
		manual := s.button("Manual", func() {
			s.close()
			w.app.showManual("getting-started")
		})
		end = append(end, manual)
		s.buttons([]*gtk.Button{diag, toolsB}, end, manual)
	}
	render()
	s.win.Present()
	svc := w.app.be.About
	go func() {
		i := svc.Info()
		onMainAsync(func() {
			if toolsShownSheet != s {
				return
			}
			info = &i
			render()
		})
	}()
}

// installTitle is the menu item that opens Command-Line Tools at a missing
// tool.
func installTitle(tool string) string {
	if tool == "aws" {
		return "Install the AWS CLI…"
	}
	return "Install " + tool + "…"
}

// withInstallHint points a missing tool's error at its row menu's Install….
func withInstallHint(err string) string {
	if uicore.MissingTool(err) == "" {
		return err
	}
	return err + " — right-click to install it"
}

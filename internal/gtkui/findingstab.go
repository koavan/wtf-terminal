//go:build linux

package gtkui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/findings"
	"github.com/dayanraj/wtf/internal/uicore"
)

func init() {
	registerSidebarTab(sidebarTabInfo{tab: uicore.SidebarFindings, label: "Findings", icon: "security-high-symbolic", cmd: "sidebar.findings",
		make: func(w *Window) sidePaneTab { return newFindingsTab(w) }})
	extraCSS = append(extraCSS, `
.wtf-finding-bad { background: alpha(@error_color, 0.2); color: @error_color; }
.wtf-finding-warn { background: alpha(@warning_color, 0.22); color: @warning_color; }
.wtf-findings-run { margin: 6px 12px 4px; }
`)
}

// findingsHelpURI is a rule documentation link the tab opens.
var findingsHelpURI = regexp.MustCompile(`^https?://`)

// findingsTab is the Findings tab: security scanners' SARIF results for
// the focused shell's repository, by severity then rule. Read when the tab
// opens, when the shell leaves the folder, on refresh, and when a scan
// finishes; never polled. Like the macOS app's FindingsView.
type findingsTab struct {
	w       *Window
	root    *gtk.Box
	header  *paneHeader
	refresh *gtk.Button
	filter  *gtk.SearchEntry
	note    *gtk.Box
	list    *sideList

	report  *findings.Report
	err     string
	loading bool
	// The folder the report was read for; "" before the first read.
	folder    string
	seq       int
	closed    bool
	folded    map[string]bool
	openRules map[string]bool
	items     map[string]findings.Finding
}

func newFindingsTab(w *Window) *findingsTab {
	f := &findingsTab{w: w, folded: map[string]bool{}, openRules: map[string]bool{}, items: map[string]findings.Finding{}}
	f.header = newPaneHeader("Findings")
	f.header.tool("media-playback-start-symbolic", "Run a security scan", w.runSecurityScan)
	f.refresh = f.header.tool("view-refresh-symbolic", "Read the results again", func() {
		if c := f.cwd(); c != "" {
			f.load(c)
		}
	})
	f.filter = gtk.NewSearchEntry()
	f.filter.SetPlaceholderText("Filter by rule, file, message, tool")
	f.filter.AddCSSClass("wtf-side-filter")
	f.filter.ConnectSearchChanged(f.rebuild)
	f.filter.ConnectStopSearch(func() {
		f.filter.SetText("")
		w.focusActive()
	})
	f.note = gtk.NewBox(gtk.OrientationVertical, 0)
	f.list = newSideList(false)
	f.list.onClick = f.clicked
	f.list.onMenu = func(ids []string, x, y float64) {
		if items := f.menu(ids[0]); len(items) > 0 {
			f.list.menu.show(x, y, items, nil)
		}
	}

	f.root = gtk.NewBox(gtk.OrientationVertical, 4)
	f.root.Append(f.header.root)
	f.root.Append(f.filter)
	f.root.Append(f.note)
	f.root.Append(f.list.root)
	if c := f.cwd(); c != "" {
		f.load(c)
	}
	f.rebuild()
	return f
}

func (f *findingsTab) widget() gtk.Widgetter { return f.root }

func (f *findingsTab) close() { f.closed = true }

// commandFinished: nothing; a finished scan comes through scanFinished,
// from whichever pane ran it.
func (f *findingsTab) commandFinished() {}

// scanFinished: a scanner finished in some terminal of the window; read
// the results again.
func (f *findingsTab) scanFinished() {
	if f.folder != "" {
		f.load(f.folder)
	}
}

func (f *findingsTab) cwd() string {
	if ps := f.w.model.ActivePane(); ps != nil {
		return ps.Cwd
	}
	return ""
}

func (f *findingsTab) total() int {
	if f.report == nil {
		return 0
	}
	return len(f.report.Findings)
}

func (f *findingsTab) reportRoot() string {
	if f.report == nil {
		return ""
	}
	return f.report.Root
}

// paneChanged: read again when the shell left the report's repository.
func (f *findingsTab) paneChanged() {
	c := f.cwd()
	if c != "" && uicore.FindingsStale(c, f.reportRoot(), f.folder) {
		f.load(c)
	}
	f.refresh.SetSensitive(!f.loading && c != "")
	if f.report == nil && f.err == "" {
		// The "focus a terminal" note may now be "reading".
		f.rebuild()
	}
}

func (f *findingsTab) load(folder string) {
	f.folder = folder
	f.seq++
	mine := f.seq
	f.loading = true
	f.refresh.SetSensitive(false)
	svc := f.w.app.be.Findings
	go func() {
		r, err := svc.Load(folder)
		onMainAsync(func() {
			if mine != f.seq || f.closed {
				return
			}
			f.loading = false
			if err != nil {
				f.report, f.err = nil, err.Error()
			} else {
				f.report, f.err = &r, ""
			}
			f.refresh.SetSensitive(f.cwd() != "")
			f.rebuild()
		})
	}()
}

// Building

func findingsToneClass(severity string) string {
	switch uicore.FindingToneOf(severity) {
	case uicore.FindingBad:
		return "wtf-finding-bad"
	case uicore.FindingWarn:
		return "wtf-finding-warn"
	}
	return ""
}

func (f *findingsTab) pretty(path string) string { return uicore.PrettyPath(path, f.w.app.home) }

func (f *findingsTab) rebuild() {
	if f.closed {
		return
	}
	root := f.reportRoot()
	if root == "" {
		f.header.setTitle("Findings", "")
	} else {
		f.header.setTitle(uicore.BaseName(f.pretty(root)), "Findings for "+f.pretty(root))
	}
	f.filter.SetVisible(f.total() > 0)
	f.items = map[string]findings.Finding{}
	var note *gtk.Box
	var sections []sideSection
	q := f.filter.Text()
	switch {
	case f.err != "":
		note = sshNote("", f.err)
	case f.report != nil:
		r := f.report
		if len(r.Sources) > 0 {
			sec := sideSection{}
			for i, s := range r.Sources {
				sec.rows = append(sec.rows, f.sourceRow(i, s))
			}
			sections = append(sections, sec)
		}
		if r.Truncated > 0 {
			sections = append(sections, sideSection{empty: fmt.Sprintf("Showing the first %d findings; %d more are in the files.", f.total(), r.Truncated)})
		}
		groups := uicore.GroupFindings(r.Findings, q)
		for _, g := range groups {
			sections = append(sections, f.groupSection(g, root))
		}
		if len(groups) == 0 {
			switch {
			case strings.TrimSpace(q) != "":
				note = sshNote("", "No findings match “"+q+"”.")
			case len(r.Sources) > 0:
				note = sshNote("No findings", "The scanners found nothing in "+f.pretty(root)+".")
			default:
				note = sshNote("No scan results for "+f.pretty(root),
					"Run a scanner — Trivy, Semgrep, Checkov, Gitleaks or Grype — and its findings show here, grouped by severity. A .sarif file at the top of the folder (from CI, say) is read too.")
				b := gtk.NewButtonWithLabel("Run Scan…")
				b.AddCSSClass("suggested-action")
				b.AddCSSClass("wtf-findings-run")
				b.SetHAlign(gtk.AlignStart)
				b.ConnectClicked(f.w.runSecurityScan)
				note.Append(b)
			}
		}
	default:
		if f.cwd() == "" {
			note = sshNote("", "Focus a terminal to see findings for its folder.")
		} else {
			note = sshNote("", "Reading results…")
		}
	}
	sshSetNote(f.note, note)
	f.list.set(sections)
}

func (f *findingsTab) sourceRow(i int, s findings.Source) sideRow {
	r := sideRow{id: "s:" + strconv.Itoa(i), icon: "x-office-document-symbolic", title: s.Tool, alert: s.Error, tip: f.pretty(s.Path)}
	if s.FromScan {
		r.icon = "security-high-symbolic"
		r.tip += "\nFrom a scan run in wtf (kept in its cache)"
	}
	if s.Version != "" {
		r.title += " " + s.Version
	}
	if s.Error == "" {
		r.detail = fmt.Sprintf("%d %s · %s", s.Count, plural(s.Count, "finding"), uicore.FindingRelative(s.Modified, time.Now()))
	}
	return r
}

func (f *findingsTab) groupSection(g uicore.FindingGroup, root string) sideSection {
	sev := g.Severity
	label := uicore.FindingSeverityLabels[sev]
	if label == "" && sev != "" {
		label = strings.ToUpper(sev[:1]) + sev[1:]
	}
	sec := sideSection{title: label, badge: strconv.Itoa(g.Count()), open: !f.folded[sev], toggle: func() {
		f.folded[sev] = !f.folded[sev]
		f.rebuild()
	}}
	tone := findingsToneClass(sev)
	for _, r := range g.Rules {
		ruleKey := sev + "\n" + r.Key
		open := f.openRules[ruleKey]
		row := sideRow{id: "r:" + ruleKey, icon: "pan-end-symbolic", title: r.Title, detail: r.Tool,
			pills: []sidePill{{text: strconv.Itoa(len(r.Items)), class: tone}}}
		if open {
			row.icon = "pan-down-symbolic"
		}
		if r.RuleID != r.Title && r.RuleID != "" {
			row.detail = r.RuleID + " · " + r.Tool
		}
		desc := ""
		if len(r.Items) > 0 {
			if d := r.Items[0].Description; d != "" && d != r.Title {
				desc = "\n" + d
			}
		}
		row.tip = r.RuleID + desc + "\n" + r.Tool
		sec.rows = append(sec.rows, row)
		if !open {
			continue
		}
		for i, it := range r.Items {
			id := fmt.Sprintf("f:%s\n%d", ruleKey, i)
			f.items[id] = it
			where := uicore.FindingWhere(it, root)
			if where == "" {
				where = "no location"
			}
			first, _, _ := strings.Cut(it.Message, "\n")
			if first == r.Title {
				first = ""
			}
			tip := it.Message
			if it.Score != 0 {
				tip += fmt.Sprintf("\nScore %g", it.Score)
			}
			icon := "insert-link-symbolic"
			if strings.HasPrefix(it.Path, "/") {
				icon = "text-x-generic-symbolic"
			}
			sec.rows = append(sec.rows, sideRow{id: id, icon: icon, title: where, mono: true, detail: first, tip: tip, depth: 1})
		}
	}
	return sec
}

// Actions

func (f *findingsTab) clicked(id string) {
	if key, ok := strings.CutPrefix(id, "r:"); ok {
		f.openRules[key] = !f.openRules[key]
		f.rebuild()
	} else if it, ok := f.items[id]; ok && strings.HasPrefix(it.Path, "/") {
		f.w.openInEditor(it.Path, it.Line)
	}
}

func (f *findingsTab) copy(text, toast string) {
	f.w.win.Clipboard().SetText(text)
	f.w.toast(toast)
}

// reveal opens the file's folder in the file manager.
func (f *findingsTab) reveal(path string) {
	svc := f.w.app.be.Files
	go func() {
		if err := svc.Reveal(path); err != nil {
			onMainAsync(func() { f.w.toast("Couldn't reveal it: " + err.Error()) })
		}
	}()
}

func (f *findingsTab) ruleItems(it findings.Finding) []*menuItem {
	var items []*menuItem
	if it.RuleID != "" {
		items = append(items, item("Copy Rule ID", func() { f.copy(it.RuleID, "Copied rule ID") }))
	}
	if findingsHelpURI.MatchString(it.HelpURI) {
		items = append(items, item("Open Rule Documentation", func() { sshOpenURL(f.w, it.HelpURI) }))
	}
	return items
}

func (f *findingsTab) menu(id string) []*menuItem {
	var items []*menuItem
	if it, ok := f.items[id]; ok {
		where := uicore.FindingWhere(it, f.reportRoot())
		items = append(items, item("Triage with Claude…", func() { f.w.claudeTriage(it.RuleID, where) }), menuSection)
		switch {
		case strings.HasPrefix(it.Path, "/"):
			open := "Open in Editor"
			if it.Line > 0 {
				open = fmt.Sprintf("Open in Editor at Line %d", it.Line)
			}
			items = append(items,
				item(open, func() { f.w.openInEditor(it.Path, it.Line) }),
				item("Open Containing Folder", func() { f.reveal(it.Path) }),
				menuSection,
				item("Copy Path", func() { f.copy(it.Path, "Copied path") }))
			if it.Line > 0 {
				where := uicore.FindingWhere(it, f.reportRoot())
				items = append(items, item("Copy "+where, func() { f.copy(where, "Copied path and line") }))
			}
		case it.Path != "":
			items = append(items, item("Copy Location", func() { f.copy(it.Path, "Copied location") }))
		}
		if len(items) > 0 {
			items = append(items, menuSection)
		}
		items = append(items, item("Copy Message", func() { f.copy(it.Message, "Copied message") }))
		return append(items, f.ruleItems(it)...)
	}
	if key, ok := strings.CutPrefix(id, "r:"); ok {
		label := "Show Locations"
		if f.openRules[key] {
			label = "Hide Locations"
		}
		items = append(items, item(label, func() { f.clicked(id) }), menuSection)
		if f.report != nil {
			for _, it := range f.report.Findings {
				if key == it.Severity+"\n"+it.Tool+"\n"+it.RuleID {
					items = append(items, f.ruleItems(it)...)
					break
				}
			}
		}
		return items
	}
	if n, ok := strings.CutPrefix(id, "s:"); ok && f.report != nil {
		i, err := strconv.Atoi(n)
		if err != nil || i < 0 || i >= len(f.report.Sources) {
			return nil
		}
		s := f.report.Sources[i]
		items = append(items,
			item("Open Containing Folder", func() { f.reveal(s.Path) }),
			item("Copy Path", func() { f.copy(s.Path, "Copied path") }))
		if s.FromScan {
			items = append(items, menuSection, item("Remove These Results…", func() { f.forget(s) }))
		}
		return items
	}
	return nil
}

// forget deletes one of wtf's own scan results, after asking.
func (f *findingsTab) forget(s findings.Source) {
	w := f.w
	w.app.confirm(&w.win.Window, "Remove the "+s.Tool+" results?",
		"Deletes "+f.pretty(s.Path)+" from wtf's cache. Run the scan again to get them back.", "Remove", func() {
			svc := w.app.be.Findings
			go func() {
				err := svc.Forget(s.Path)
				onMainAsync(func() {
					if err != nil {
						w.toast("Couldn't remove them: " + err.Error())
						return
					}
					if !f.closed && f.folder != "" {
						f.load(f.folder)
					}
				})
			}()
		})
}

// Run Security Scan

// findingsLastScanner is the last scanner run, for this app session.
var findingsLastScanner = "trivy"

// runSecurityScan (Run Security Scan…): pick an installed scanner, see the
// exact command, and run it in the focused shell; its SARIF goes to wtf's
// cache, not the repository. Like the macOS app's ScanDialog.
func (w *Window) runSecurityScan() {
	paneID, cwd, path := -1, "", ""
	if ps := w.model.ActivePane(); ps != nil {
		paneID, cwd, path = ps.ID, ps.Cwd, ps.Env["PATH"]
	}
	f := newSSHForm(w, "Run Security Scan", "Scans the focused shell’s repository.", 520)
	radios := gtk.NewBox(gtk.OrientationVertical, 8)
	details := gtk.NewBox(gtk.OrientationVertical, 8)
	f.add(radios)
	f.add(details)
	var (
		scanners []findings.Scanner
		choice   string
		scan     *findings.Scan
		done     bool
	)
	chosen := func() *findings.Scanner {
		for i := range scanners {
			if scanners[i].ID == choice {
				return &scanners[i]
			}
		}
		return nil
	}
	empty := func(b *gtk.Box) {
		for c := b.FirstChild(); c != nil; c = b.FirstChild() {
			b.Remove(c)
		}
	}
	// render redraws what depends on the choice and its command.
	render := func() {
		empty(details)
		if scan != nil {
			f.d.SetBody("Scans " + uicore.PrettyPath(scan.Root, w.app.home) + ". Results show in the Findings tab when it finishes.")
		}
		c := chosen()
		if c != nil && c.Note != "" {
			details.Append(f.label(c.Note, false))
		}
		if scan != nil {
			details.Append(f.label(scan.Command, true))
			details.Append(f.label("Runs in your shell (a new tab if it’s busy). The SARIF goes to "+
				uicore.PrettyPath(scan.Output, w.app.home)+", not into the repository.", false))
		}
		if c != nil && !c.Found {
			b := gtk.NewButtonWithLabel("Install " + c.Name + "…")
			b.SetHAlign(gtk.AlignStart)
			tool := c.Tool
			b.ConnectClicked(func() {
				f.close()
				w.showTools(tool)
			})
			details.Append(b)
		}
		f.enable("run", scan != nil && c != nil && c.Found)
	}
	fetchCommand := func() {
		scan = nil
		id := choice
		svc := w.app.be.Findings
		go func() {
			s, err := svc.ScanCommand(cwd, id)
			onMainAsync(func() {
				if done || choice != id {
					return
				}
				if err != nil {
					f.setError(err.Error())
				} else {
					scan = &s
					f.setError("")
				}
				render()
			})
		}()
	}
	f.button("cancel", "Cancel", adw.ResponseDefault, nil)
	f.button("run", "Run Scan", adw.ResponseSuggested, func() {
		c := chosen()
		if scan == nil || c == nil || !c.Found {
			return
		}
		command := scan.Command
		findingsLastScanner = choice
		done = true
		f.close()
		ps, p := w.model.Panes[paneID], w.panes[paneID]
		if ps != nil && p != nil && ps.IsShell && !ps.Running {
			p.write(uicore.ClearLine + command + "\r")
			p.term.GrabFocus()
		} else {
			dir := cwd
			if ps != nil {
				dir = ps.Cwd
			}
			w.model.Open(uicore.OpenOptions{Cwd: &dir, Command: command})
			w.toast("Scanning in a new tab: that shell was busy")
		}
		w.sidebarState.Tab, w.sidebarState.Open = uicore.SidebarFindings, true
		w.layoutSidebar()
	})
	f.d.ConnectDestroy(func() { done = true })
	f.enable("run", false)

	if cwd == "" {
		f.present(nil)
		f.setError("Focus a terminal first: the scan runs in its folder.")
		return
	}
	details.Append(f.label("Looking for scanners…", false))
	f.present(nil)
	svc := w.app.be.Findings
	go func() {
		list := svc.Scanners(path)
		onMainAsync(func() {
			if done {
				return
			}
			scanners = list
			choice = ""
			for _, s := range list {
				if s.ID == findingsLastScanner {
					choice = s.ID
				}
			}
			if choice == "" {
				for _, s := range list {
					if s.Found {
						choice = s.ID
						break
					}
				}
			}
			if choice == "" && len(list) > 0 {
				choice = list[0].ID
			}
			var group *gtk.CheckButton
			for _, s := range list {
				label := s.Name
				if !s.Found {
					label += " · not installed"
				}
				radio := gtk.NewCheckButtonWithLabel(label)
				if group == nil {
					group = radio
				} else {
					radio.SetGroup(group)
				}
				radio.SetActive(s.ID == choice)
				id := s.ID
				radio.ConnectToggled(func() {
					if !radio.Active() || choice == id {
						return
					}
					choice = id
					fetchCommand()
					render()
				})
				col := gtk.NewBox(gtk.OrientationVertical, 1)
				col.Append(radio)
				about := f.label(s.About, false)
				about.SetMarginStart(28)
				col.Append(about)
				radios.Append(col)
			}
			render()
			if choice != "" {
				fetchCommand()
			}
		})
	}()
}

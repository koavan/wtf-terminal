//go:build linux

package gtkui

import (
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/uicore"
)

// The terminal's inspector menus: the token under the mouse (URLs, JWTs,
// times, addresses, paths, Kubernetes resources), what the selection can
// be inspected as, a YAML base64 value, JSON in the line. Like the macOS
// app's TerminalController (inspectItems, token, commandClick).

// inspectResolved is a path looked up lately: per folder and name, for 5s.
type inspectResolved struct {
	path string
	at   time.Time
}

// Paths found lately, for every pane (cleared past 200).
var inspectPaths = map[string]inspectResolved{}

// cellAt is the absolute row and column at a point in the terminal.
func (p *Pane) cellAt(x, y float64) (row, col int, ok bool) {
	top, ok := p.top()
	if !ok {
		return 0, 0, false
	}
	cw, ch := p.term.CharSize()
	row = top + max(0, int(y)-padTop)/max(1, ch)
	col = max(0, int(x)-padLeft) / max(1, cw)
	return row, col, true
}

// paneState is the pane's state in its window's model.
func (p *Pane) paneState() *uicore.PaneState { return p.win.model.Panes[p.id] }

// cwd is the shell's folder, as last reported.
func (p *Pane) cwd() string {
	if ps := p.paneState(); ps != nil {
		return ps.Cwd
	}
	return ""
}

// kubeTarget is the cluster the shell reported at its last prompt (not the
// Kubernetes tab's).
func (p *Pane) kubeTarget() uicore.KubeTarget {
	t := uicore.KubeTarget{Env: map[string]string{}}
	if ps := p.paneState(); ps != nil && ps.Env != nil {
		t.Env = ps.Env
	}
	if c := p.win.infra[p.id]; c != nil {
		t.Context, t.Namespace = c.KubeContext, c.KubeNamespace
	}
	return t
}

// resolve is the file a path names, made absolute; "" if there's none.
func (p *Pane) resolve(dir, name string) string {
	key := dir + "\x00" + name
	if r, ok := inspectPaths[key]; ok && time.Since(r.at) < 5*time.Second {
		return r.path
	}
	if len(inspectPaths) > 200 {
		inspectPaths = map[string]inspectResolved{}
	}
	path := p.win.app.be.Files.ResolveFile(dir, name)
	inspectPaths[key] = inspectResolved{path, time.Now()}
	return path
}

// token is the token at a cell (absolute row), as the menus use it: none
// for a JWT while Hide Secrets is on; a path only when the file is on this
// computer (made absolute).
func (p *Pane) token(row, col int) (uicore.LogToken, bool) {
	tok, ok := uicore.LogTokenAt(p.term, row, col)
	if !ok || tok.Kind == uicore.LogKindJWT && p.win.app.settings.hide {
		return uicore.LogToken{}, false
	}
	return uicore.LogResolve(tok, p.cwd(), p.resolve)
}

// logicalLine is the soft-wrapped line holding an absolute row, and the
// byte index of a column in it.
func (p *Pane) logicalLine(row, col int) (string, int, bool) {
	_, upper, _, _ := p.term.View()
	first, last := p.firstRow(upper), p.feed.screenTop+p.term.Rows()-1
	if row < first || row > last {
		return "", 0, false
	}
	start := row
	for start > first && row-start < 40 && !p.term.EndsLine(start-1) {
		start--
	}
	var b strings.Builder
	index := 0
	for r := start; r <= last && r-start < 80; r++ {
		text := p.term.Row(r)
		continues := !p.term.EndsLine(r) && r < last
		if !continues {
			text = strings.TrimRight(text, " ")
		}
		if r == row {
			index = b.Len() + inspectByteAt(text, col)
		}
		b.WriteString(text)
		if !continues {
			break
		}
	}
	return b.String(), index, true
}

// inspectByteAt is the byte index of a column in a row's text (past its
// end, as if padded with spaces).
func inspectByteAt(text string, col int) int {
	cells := 0
	for i, r := range text {
		w := uicore.CellWidth(r)
		if col < cells+w {
			return i
		}
		cells += w
	}
	return len(text) + col - cells
}

// menuItems are a token menu's entries as menu items, each running its
// action.
func (p *Pane) menuItems(entries []uicore.LogEntry) []*menuItem {
	var items []*menuItem
	for _, e := range entries {
		switch e.Kind {
		case uicore.LogHeading:
			items = append(items, item(e.Label, nil))
		case uicore.LogSeparator:
			items = append(items, menuSection)
		default:
			a := e.Action
			items = append(items, item(e.Label, func() { p.run(a) }))
		}
	}
	return items
}

// run does what a token menu's item says.
func (p *Pane) run(a uicore.LogAction) {
	w := p.win
	switch a.Kind {
	case uicore.LogOpenURL:
		p.openLink(a.Text)
	case uicore.LogCopy:
		p.copyText(a.Text, a.What)
	case uicore.LogCopySecret:
		w.copySecret(a.Text, a.What)
	case uicore.LogRunInTab:
		cwd := p.cwd()
		w.model.Open(uicore.OpenOptions{Cwd: &cwd, Command: a.Text})
	case uicore.LogRunHere:
		w.runInPane(a.Text)
	case uicore.LogDescribe:
		// At the prompt, the shell has its own variables; a new tab needs
		// them in front.
		t := p.kubeTarget()
		if ps := p.paneState(); ps != nil && ps.IsShell && !ps.Running {
			bare := t
			bare.Env = nil
			p.write(uicore.ClearLine + bare.Describe(a.ResourceKind, a.Name) + "\r")
			p.term.GrabFocus()
		} else {
			cwd := p.cwd()
			w.model.Open(uicore.OpenOptions{Cwd: &cwd, Command: t.Describe(a.ResourceKind, a.Name)})
		}
	case uicore.LogOpenInEditor:
		w.openInEditor(a.Text, a.Line)
	case uicore.LogOpenFile:
		files, path := w.app.be.Files, a.Text
		go func() {
			if err := files.Open(path); err != nil {
				onMainAsync(func() { w.toast("Couldn't open it: " + err.Error()) })
			}
		}()
	case uicore.LogReveal:
		files, path := w.app.be.Files, a.Text
		go func() {
			if err := files.Reveal(path); err != nil {
				onMainAsync(func() { w.toast("Couldn't reveal it: " + err.Error()) })
			}
		}()
	case uicore.LogShowSecret:
		w.showSecret(a.Text, p.kubeTarget())
	case uicore.LogInspectJWT:
		w.showJWT(a.Text)
	case uicore.LogInspectTLS:
		w.showCertHost(a.Text)
	}
}

// inspectItems are the inspector entries at a point in the terminal: the
// token under the mouse and its menu, what the selection can be inspected
// as (or a YAML base64 value), JSON in the selection or the line. Groups
// are split by sections; nil for none. Like outputEntries in
// TerminalPane.svelte.
func (p *Pane) inspectItems(x, y float64) []*menuItem {
	w := p.win
	hide := w.app.settings.hide
	var groups [][]*menuItem
	row, col, ok := p.cellAt(x, y)
	ok = ok && !p.feed.alternate
	var line string
	index, haveLine := 0, false
	if ok {
		line, index, haveLine = p.logicalLine(row, col)
	}
	selection := ""
	if p.term.HasSelection() {
		if s := p.term.Selection(); strings.TrimSpace(s) != "" {
			selection = s
		}
	}

	// The token under the mouse, and its menu.
	if ok {
		if tok, found := p.token(row, col); found {
			if g := p.menuItems(uicore.LogMenu(tok, p.kubeTarget(), time.Now(), time.Local)); len(g) > 0 {
				groups = append(groups, g)
			}
		}
	}
	// What the selection can be inspected as.
	if selection != "" {
		var g []*menuItem
		for _, e := range uicore.InspectSelectionEntries(selection, !hide) {
			a := e.Action
			g = append(g, item(e.Label, func() { w.openInspect(a) }))
		}
		if len(g) > 0 {
			groups = append(groups, g)
		}
	} else if !hide && haveLine {
		if value := uicore.InspectYAMLBase64At(line, index); value != "" {
			groups = append(groups, []*menuItem{item("Decode Base64…", func() { w.showDecoded(value) })})
		}
	}
	// JSON in the selection, or in the line.
	for _, text := range []string{selection, line} {
		if text == "" {
			continue
		}
		if json := uicore.FindJSON(w.app.settings.text(text)); json != "" {
			groups = append(groups, []*menuItem{item("Pretty-print JSON", func() { w.showJSON(json) })})
			break
		}
	}

	var items []*menuItem
	for i, g := range groups {
		if i > 0 {
			items = append(items, menuSection)
		}
		items = append(items, g...)
	}
	return items
}

// tokenClick is Ctrl-click on a token that isn't a URL (VTE opens those):
// it shows the token's menu. A JWT while Hide Secrets is on says why it
// won't. True when it handled the click.
func (p *Pane) tokenClick(x, y float64) bool {
	if p.feed.alternate {
		return false
	}
	row, col, ok := p.cellAt(x, y)
	if !ok {
		return false
	}
	raw, ok := uicore.LogTokenAt(p.term, row, col)
	if !ok || raw.Kind == uicore.LogKindURL || !uicore.LogIsLink(raw.Kind) {
		return false
	}
	if raw.Kind == uicore.LogKindJWT && p.win.app.settings.hide {
		p.win.toast("Hide Secrets is on: turn it off to inspect tokens")
		return true
	}
	tok, ok := p.token(row, col)
	if !ok {
		return false
	}
	items := p.menuItems(uicore.LogMenu(tok, p.kubeTarget(), time.Now(), time.Local))
	if len(items) == 0 {
		return false
	}
	p.menu.show(x, y, items, nil)
	return true
}

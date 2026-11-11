//go:build linux

package gtkui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/dayanraj/wtf/internal/uicore"
	"github.com/dayanraj/wtf/manual"
)

func init() {
	extraCSS = append(extraCSS, `
.wtf-manual-nav { background: @sidebar_bg_color; }
.wtf-manual-nav list { background: transparent; }
.wtf-manual-group { font-size: 0.75em; font-weight: bold; opacity: 0.6; padding: 10px 8px 2px; }
.wtf-manual-topic { padding: 3px 8px; }
.wtf-manual-page { padding: 8px 32px 40px; }
.wtf-manual-h2 { margin-top: 14px; }
.wtf-manual-h3 { margin-top: 8px; }
.wtf-manual-tip { border-left: 3px solid @accent_color; background: alpha(@accent_color, 0.1);
  border-radius: 0 8px 8px 0; padding: 8px 12px; }
.wtf-manual-code { background: alpha(currentColor, 0.06); border-radius: 8px; }
.wtf-manual-code label { font-family: monospace; font-size: 0.92em; padding: 10px 12px; }
.wtf-manual-th { font-weight: 600; opacity: 0.7; }
`)
}

// manualWindow is the manual: the Markdown pages in manual/,
// rendered with GTK widgets (no web view). Pages are listed and searched
// on the left. One per app, made on first open and let go on close. Like
// the macOS app's ManualWindow.
type manualWindow struct {
	a      *App
	win    *adw.Window
	title  *adw.WindowTitle
	search *gtk.SearchEntry
	list   *gtk.ListBox
	// The topic id of each list row; "" for a group heading.
	rows    []string
	nothing *gtk.Label
	scroll  *gtk.ScrolledWindow
	page    *gtk.Box
	back    *gtk.Button
	forward *gtk.Button
	// The page shown, and the ones Back and Forward go to.
	current      string
	trail, ahead []string
	// Every page's plain text, read at the first search.
	plain []uicore.ManualSearchPage
}

var manualWindows = map[*App]*manualWindow{}

// showManual opens the manual at a page ("" for the welcome page).
func (a *App) showManual(topic string) {
	if topic == "" {
		topic = "getting-started"
	}
	m := manualWindows[a]
	if m == nil {
		m = newManualWindow(a)
		manualWindows[a] = m
	}
	m.open(topic, true)
	m.win.Present()
}

// showHelp opens the manual at help for what's in front (help.manual).
func (w *Window) showHelp() { w.app.showManual(uicore.ManualHelpTopic(w.sidebarState)) }

// manualTopic is the page the manual shows; "" when it's closed.
func (a *App) manualTopic() string {
	if m := manualWindows[a]; m != nil {
		return m.current
	}
	return ""
}

// manualSource is a page's Markdown.
func manualSource(id string) string {
	b, err := manual.Files.ReadFile(id + ".md")
	if err != nil {
		return "# " + id + "\n\nThis page is missing from the app."
	}
	return string(b)
}

// manualContext names commands and their shortcuts from the keymap.
var manualContext = uicore.ManualContext{
	Label: func(id string) (string, bool) {
		c, ok := uicore.CommandByID(id)
		return c.Title, ok
	},
	Shortcut: func(id string) string {
		c, _ := uicore.CommandByID(id)
		return uicore.KeyLabel(c.Keys)
	},
}

func newManualWindow(a *App) *manualWindow {
	m := &manualWindow{a: a}
	m.win = adw.NewWindow()
	m.win.SetApplication(&a.gtk.Application)
	m.win.SetTitle("wtf Manual")
	m.win.SetDefaultSize(980, 720)
	m.win.SetSizeRequest(640, 420)

	m.back = gtk.NewButtonFromIconName("go-previous-symbolic")
	m.back.SetTooltipText("Back (Alt+←)")
	m.back.ConnectClicked(m.goBack)
	m.forward = gtk.NewButtonFromIconName("go-next-symbolic")
	m.forward.SetTooltipText("Forward (Alt+→)")
	m.forward.ConnectClicked(m.goForward)
	m.title = adw.NewWindowTitle("wtf Manual", "")
	head := adw.NewHeaderBar()
	head.PackStart(m.back)
	head.PackStart(m.forward)
	head.SetTitleWidget(m.title)

	m.search = gtk.NewSearchEntry()
	m.search.SetPlaceholderText("Search the manual")
	m.search.SetMarginTop(8)
	m.search.SetMarginStart(8)
	m.search.SetMarginEnd(8)
	m.search.ConnectSearchChanged(m.refilter)
	m.search.ConnectActivate(func() {
		for _, id := range m.rows {
			if id != "" {
				m.open(id, true)
				return
			}
		}
	})
	m.search.ConnectStopSearch(m.escape)
	m.list = gtk.NewListBox()
	m.list.SetSelectionMode(gtk.SelectionSingle)
	m.list.ConnectRowActivated(func(r *gtk.ListBoxRow) {
		if i := r.Index(); i >= 0 && i < len(m.rows) && m.rows[i] != "" {
			m.open(m.rows[i], true)
		}
	})
	m.nothing = gtk.NewLabel("")
	m.nothing.SetWrap(true)
	m.nothing.AddCSSClass("wtf-side-note")
	m.list.SetPlaceholder(m.nothing)
	navScroll := gtk.NewScrolledWindow()
	navScroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	navScroll.SetVExpand(true)
	navScroll.SetChild(m.list)
	nav := gtk.NewBox(gtk.OrientationVertical, 6)
	nav.AddCSSClass("wtf-manual-nav")
	nav.Append(m.search)
	nav.Append(navScroll)

	m.page = gtk.NewBox(gtk.OrientationVertical, 10)
	m.page.AddCSSClass("wtf-manual-page")
	clamp := adw.NewClamp()
	clamp.SetMaximumSize(744)
	clamp.SetHExpand(true)
	clamp.SetChild(m.page)
	m.scroll = gtk.NewScrolledWindow()
	m.scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	m.scroll.SetChild(clamp)

	split := gtk.NewPaned(gtk.OrientationHorizontal)
	split.SetStartChild(nav)
	split.SetEndChild(m.scroll)
	split.SetPosition(230)
	split.SetShrinkStartChild(false)
	split.SetResizeStartChild(false)
	view := adw.NewToolbarView()
	view.AddTopBar(head)
	view.SetContent(split)
	m.win.SetContent(view)

	// Anywhere in the window: Esc clears the search, then closes; Ctrl+F
	// searches; Alt+arrows go back and forward.
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(func(keyval, _ uint, state gdk.ModifierType) bool {
		mods := state & (gdk.ControlMask | gdk.AltMask | gdk.ShiftMask | gdk.SuperMask)
		switch {
		case keyval == gdk.KEY_Escape && mods == 0:
			m.escape()
		case (keyval == gdk.KEY_f || keyval == gdk.KEY_F) && mods == gdk.ControlMask:
			m.search.GrabFocus()
		case keyval == gdk.KEY_Left && mods == gdk.AltMask:
			m.goBack()
		case keyval == gdk.KEY_Right && mods == gdk.AltMask:
			m.goForward()
		default:
			return false
		}
		return true
	})
	m.win.AddController(keys)
	m.win.ConnectCloseRequest(func() bool {
		if manualWindows[a] == m {
			delete(manualWindows, a)
		}
		if w := a.front(); w != nil {
			w.focusActive()
		}
		return false
	})
	m.refilter()
	m.search.GrabFocus()
	return m
}

// escape clears the search, or closes the window when it's empty.
func (m *manualWindow) escape() {
	if m.search.Text() == "" {
		m.win.Close()
		return
	}
	m.search.SetText("")
}

// open shows a page; push keeps the one shown for Back.
func (m *manualWindow) open(id string, push bool) {
	if _, ok := uicore.ManualTopicByID(id); !ok {
		id = uicore.ManualTopics[0].ID
	}
	if push && m.current != "" && m.current != id {
		m.trail = append(m.trail, m.current)
		m.ahead = nil
	}
	m.current = id
	m.render()
	m.selectCurrent()
}

func (m *manualWindow) goBack() {
	if len(m.trail) == 0 {
		return
	}
	prev := m.trail[len(m.trail)-1]
	m.trail = m.trail[:len(m.trail)-1]
	m.ahead = append(m.ahead, m.current)
	m.open(prev, false)
}

func (m *manualWindow) goForward() {
	if len(m.ahead) == 0 {
		return
	}
	next := m.ahead[len(m.ahead)-1]
	m.ahead = m.ahead[:len(m.ahead)-1]
	m.trail = append(m.trail, m.current)
	m.open(next, false)
}

// Pages

// render draws the page shown, from the top.
func (m *manualWindow) render() {
	topic, _ := uicore.ManualTopicByID(m.current)
	m.title.SetSubtitle(topic.Group)
	m.back.SetSensitive(len(m.trail) > 0)
	m.forward.SetSensitive(len(m.ahead) > 0)
	for c := m.page.FirstChild(); c != nil; c = m.page.FirstChild() {
		m.page.Remove(c)
	}
	p := uicore.ManualParse(manualSource(m.current), manualContext)
	if m.current == "shortcuts" {
		p.Blocks = append(p.Blocks, manualShortcutsTable())
	}
	for _, b := range p.Blocks {
		if w := m.block(b); w != nil {
			m.page.Append(w)
		}
	}
	m.scroll.VAdjustment().SetValue(0)
}

// manualShortcutsTable lists every command and its shortcut, from the
// keymap itself.
func manualShortcutsTable() uicore.ManualBlock {
	cmds := append([]uicore.Command(nil), uicore.Commands...)
	sort.SliceStable(cmds, func(i, j int) bool { return strings.ToLower(cmds[i].Title) < strings.ToLower(cmds[j].Title) })
	b := uicore.ManualBlock{Kind: uicore.ManualTable, Rows: [][][]uicore.ManualRun{{{{Text: "Command"}}, {{Text: "Shortcut"}}}}}
	for _, c := range cmds {
		key := uicore.ManualRun{Text: "palette only", Italic: true}
		if c.Keys != "" {
			key = uicore.ManualRun{Text: uicore.KeyLabel(c.Keys), Key: true}
		}
		b.Rows = append(b.Rows, [][]uicore.ManualRun{{{Text: c.Title}}, {key}})
	}
	return b
}

// block is one block of a page as a widget; nil for an empty one.
func (m *manualWindow) block(b uicore.ManualBlock) gtk.Widgetter {
	switch b.Kind {
	case uicore.ManualHeading:
		l := m.text(b.Text)
		switch b.Level {
		case 1:
			l.AddCSSClass("title-1")
		case 2:
			l.AddCSSClass("title-3")
			l.AddCSSClass("wtf-manual-h2")
		default:
			l.AddCSSClass("heading")
			l.AddCSSClass("wtf-manual-h3")
		}
		return l
	case uicore.ManualParagraph:
		if len(b.Text) == 0 {
			return nil
		}
		return m.text(b.Text)
	case uicore.ManualTip:
		box := gtk.NewBox(gtk.OrientationVertical, 0)
		box.AddCSSClass("wtf-manual-tip")
		box.Append(m.text(b.Text))
		return box
	case uicore.ManualCode:
		l := gtk.NewLabel(b.Code)
		l.SetXAlign(0)
		l.SetSelectable(true)
		sc := gtk.NewScrolledWindow()
		sc.SetPolicy(gtk.PolicyAutomatic, gtk.PolicyNever)
		sc.SetPropagateNaturalHeight(true)
		sc.SetChild(l)
		sc.AddCSSClass("wtf-manual-code")
		return sc
	case uicore.ManualList:
		box := gtk.NewBox(gtk.OrientationVertical, 4)
		for i, it := range b.Items {
			mark := gtk.NewLabel("•")
			if b.Ordered {
				mark.SetText(fmt.Sprintf("%d.", i+1))
			}
			mark.SetYAlign(0)
			mark.SetXAlign(1)
			mark.SetSizeRequest(18, -1)
			row := gtk.NewBox(gtk.OrientationHorizontal, 6)
			row.Append(mark)
			row.Append(m.text(it))
			box.Append(row)
		}
		return box
	case uicore.ManualTable:
		return m.table(b.Rows)
	}
	return nil
}

// table is a grid with a line under each row; the first row is the header.
func (m *manualWindow) table(rows [][][]uicore.ManualRun) *gtk.Grid {
	g := gtk.NewGrid()
	g.SetColumnSpacing(16)
	g.SetRowSpacing(5)
	g.SetMarginTop(4)
	g.SetMarginBottom(4)
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	for i, r := range rows {
		for j, cell := range r {
			l := m.text(cell)
			l.SetMaxWidthChars(40)
			l.SetYAlign(0)
			if i == 0 {
				l.AddCSSClass("wtf-manual-th")
			}
			if j == len(r)-1 {
				l.SetHExpand(true)
			}
			g.Attach(l, j, 2*i, 1, 1)
		}
		g.Attach(gtk.NewSeparator(gtk.OrientationHorizontal), 0, 2*i+1, cols, 1)
	}
	return g
}

// text is runs as a wrapping, selectable label; links in it navigate or
// open the browser.
func (m *manualWindow) text(runs []uicore.ManualRun) *gtk.Label {
	l := gtk.NewLabel("")
	l.SetMarkup(manualMarkup(runs))
	l.SetXAlign(0)
	l.SetWrap(true)
	l.SetWrapMode(pango.WrapWordChar)
	l.SetSelectable(true)
	l.SetHExpand(true)
	l.ConnectActivateLink(func(uri string) bool {
		// Later: the link's label goes when the page is redrawn.
		onMainAsync(func() { m.follow(uri) })
		return true
	})
	return l
}

// follow goes to another page, or opens a web link in the browser.
func (m *manualWindow) follow(uri string) {
	if id, ok := strings.CutPrefix(uri, "wtf-manual:"); ok {
		m.open(id, true)
		return
	}
	if strings.HasPrefix(uri, "https://") {
		gtk.NewURILauncher(uri).Launch(context.Background(), &m.win.Window, nil)
	}
}

var manualMarkupEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")

// manualMarkup is runs as Pango markup: code and keys in monospace on a
// tint, links to pages as wtf-manual:<id>.
func manualMarkup(runs []uicore.ManualRun) string {
	var b strings.Builder
	for _, r := range runs {
		t := manualMarkupEscape.Replace(r.Text)
		switch {
		case r.Key:
			t = `<span font_family="monospace" weight="600" background="#808080" bgalpha="22%">` + " " + t + " </span>"
		case r.Code:
			t = `<span font_family="monospace" size="92%" background="#808080" bgalpha="16%">` + t + "</span>"
		}
		if r.Italic {
			t = "<i>" + t + "</i>"
		}
		if r.Bold {
			t = "<b>" + t + "</b>"
		}
		switch {
		case r.Topic != "":
			t = `<a href="wtf-manual:` + manualMarkupEscape.Replace(r.Topic) + `">` + t + "</a>"
		case r.URL != "":
			t = `<a href="` + manualMarkupEscape.Replace(r.URL) + `">` + t + "</a>"
		}
		b.WriteString(t)
	}
	return b.String()
}

// Navigation list and search

// refilter lists the pages matching the search, under their groups.
func (m *manualWindow) refilter() {
	q := m.search.Text()
	shown := uicore.ManualTopics
	if strings.TrimSpace(q) != "" {
		if m.plain == nil {
			for _, t := range uicore.ManualTopics {
				m.plain = append(m.plain, uicore.ManualSearchPage{Topic: t, Text: uicore.ManualPlainText(manualSource(t.ID))})
			}
		}
		shown = uicore.ManualSearch(q, m.plain)
	}
	m.list.RemoveAll()
	m.rows = nil
	group := ""
	for _, t := range shown {
		if t.Group != group {
			group = t.Group
			l := gtk.NewLabel(strings.ToUpper(group))
			l.SetXAlign(0)
			l.AddCSSClass("wtf-manual-group")
			row := gtk.NewListBoxRow()
			row.SetChild(l)
			row.SetSelectable(false)
			row.SetActivatable(false)
			m.list.Append(row)
			m.rows = append(m.rows, "")
		}
		l := gtk.NewLabel(t.Title)
		l.SetXAlign(0)
		l.SetEllipsize(pango.EllipsizeEnd)
		l.SetTooltipText(t.Title)
		l.AddCSSClass("wtf-manual-topic")
		m.list.Append(l)
		m.rows = append(m.rows, t.ID)
	}
	m.nothing.SetText("Nothing matches “" + q + "”.")
	m.selectCurrent()
}

// selectCurrent selects the page shown in the list, when it's listed.
func (m *manualWindow) selectCurrent() {
	for i, id := range m.rows {
		if id != "" && id == m.current {
			m.list.SelectRow(m.list.RowAtIndex(i))
			return
		}
	}
	m.list.UnselectAll()
}

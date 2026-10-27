//go:build linux

package gtkui

import (
	"context"
	"fmt"
	"strings"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/dayanraj/wtf/internal/apiclient"
	"github.com/dayanraj/wtf/internal/uicore"
)

// An API pane: a request editor (method, URL, parameters, headers, body)
// over its response. It sits in the tab page like a terminal, so tabs,
// splits, groups and moving between windows work the same. Requests go out
// only on Send; credential header values are masked until revealed and
// never saved with the session (uicore.APIForSaving). Like the macOS app's
// ApiView.swift.

func init() {
	extraCSS = append(extraCSS, `
.wtf-api { padding: 10px 12px 8px; }
.wtf-api-url { font-family: monospace; }
.wtf-api-mono { font-family: monospace; font-size: 0.95em; }
.wtf-api-body { border: 1px solid alpha(currentColor, 0.15); border-radius: 6px; }
.wtf-api-body textview, .wtf-api-response textview { background: transparent; }
.wtf-api-status { font-weight: bold; }
.wtf-api-status.ok { color: @success_color; }
.wtf-api-status.redirect { color: @warning_color; }
.wtf-api-status.fail { color: @error_color; }
.wtf-api-meta { opacity: 0.65; font-size: 0.92em; }
.wtf-api-hint { opacity: 0.6; margin-top: 28px; }
.wtf-api-hint.fail { color: @error_color; opacity: 1; }
`)
}

type apiPane struct {
	w    *Window
	id   int
	root *gtk.Box
	req  apiclient.Request
	res  *apiclient.Response
	// The last request's error.
	failure string
	sending bool
	// Names this pane's request in flight, for Cancel.
	flight string
	// Filling the views from req: their change handlers stay quiet.
	loading    bool
	lastURLLen int
	placed     bool
	unobserve  func()

	methods      *gtk.StringList
	method       *gtk.DropDown
	url          *gtk.Entry
	send         *gtk.Button
	reqStack     *gtk.Stack
	params       *fieldList
	headers      *fieldList
	body         *gtk.TextView
	bodyPage     *gtk.Box
	paned        *gtk.Paned
	status       *gtk.Label
	meta         *gtk.Label
	respKind     *gtk.Box
	respBody     *gtk.ToggleButton
	respHeaders  *gtk.ToggleButton
	raw          *gtk.ToggleButton
	copyResponse *gtk.Button
	respScroll   *gtk.ScrolledWindow
	respView     *gtk.TextView
	hint         *gtk.Label
	findBar      *gtk.Revealer
	findEntry    *gtk.SearchEntry
	insecure     *gio.SimpleAction
}

var apiFlights int

func newAPIPane(w *Window, id int, r apiclient.Request) *apiPane {
	apiFlights++
	a := &apiPane{w: w, id: id, req: r, flight: fmt.Sprintf("api-%s-%d", w.name, apiFlights)}
	a.build()
	a.load(r)
	a.showResponse()
	a.unobserve = w.app.settings.observe(a.showResponse)
	return a
}

// Building

func (a *apiPane) build() {
	a.root = gtk.NewBox(gtk.OrientationVertical, 8)
	a.root.AddCSSClass("wtf-api")
	a.root.SetHExpand(true)
	a.root.SetVExpand(true)

	// Method, URL, Send and the ⋯ menu.
	a.methods = gtk.NewStringList(uicore.APIMethods)
	a.method = gtk.NewDropDown(a.methods, nil)
	a.method.NotifyProperty("selected", func() {
		if a.loading {
			return
		}
		a.req.Method = a.methods.String(a.method.Selected())
		a.edited()
	})
	a.url = gtk.NewEntry()
	a.url.AddCSSClass("wtf-api-url")
	a.url.SetHExpand(true)
	a.url.SetPlaceholderText("https://api.example.com/path — or paste a curl command")
	a.url.ConnectChanged(a.urlChanged)
	a.url.ConnectActivate(a.sendOrCancel)
	a.send = gtk.NewButtonWithLabel("Send")
	a.send.AddCSSClass("suggested-action")
	a.send.SetTooltipText("Send the request (Ctrl+Return)")
	a.send.ConnectClicked(a.sendOrCancel)
	more := gtk.NewMenuButton()
	more.SetIconName("view-more-symbolic")
	more.AddCSSClass("flat")
	more.SetTooltipText("Copy as curl, Run in Shell, TLS checks…")
	more.SetMenuModel(a.moreMenu())
	bar := gtk.NewBox(gtk.OrientationHorizontal, 8)
	bar.Append(a.method)
	bar.Append(a.url)
	bar.Append(a.send)
	bar.Append(more)
	a.root.Append(bar)

	// Request: parameters, headers or the body.
	a.params = newFieldList("Parameter", false, a.paramsEdited)
	a.headers = newFieldList("Header", true, a.headersEdited)
	a.body = gtk.NewTextView()
	a.body.AddCSSClass("wtf-api-mono")
	a.body.SetMonospace(true)
	a.body.SetWrapMode(gtk.WrapWordChar)
	a.body.SetTopMargin(6)
	a.body.SetLeftMargin(6)
	a.body.SetRightMargin(6)
	a.body.Buffer().ConnectChanged(func() {
		if a.loading {
			return
		}
		a.req.Body = bufferText(a.body.Buffer())
		a.edited()
	})
	bodyScroll := gtk.NewScrolledWindow()
	bodyScroll.SetChild(a.body)
	bodyScroll.SetVExpand(true)
	bodyScroll.AddCSSClass("wtf-api-body")
	contentType := gtk.NewMenuButton()
	contentType.SetLabel("Content-Type")
	contentType.AddCSSClass("flat")
	contentType.SetTooltipText("Sets the Content-Type header")
	ct := gio.NewMenu()
	group := gio.NewSimpleActionGroup()
	for i, t := range []string{"application/json", "text/plain", "application/x-www-form-urlencoded", "application/xml"} {
		name := fmt.Sprintf("ct%d", i)
		act := gio.NewSimpleAction(name, nil)
		act.ConnectActivate(func(*glib.Variant) { a.setContentType(t) })
		group.AddAction(act)
		ct.Append(t, "apict."+name)
	}
	a.root.InsertActionGroup("apict", group)
	contentType.SetMenuModel(ct)
	format := gtk.NewButtonWithLabel("Format JSON")
	format.AddCSSClass("flat")
	format.ConnectClicked(a.formatBody)
	bodyBar := gtk.NewBox(gtk.OrientationHorizontal, 6)
	bodyBar.Append(contentType)
	bodyBar.Append(format)
	a.bodyPage = gtk.NewBox(gtk.OrientationVertical, 6)
	a.bodyPage.Append(bodyBar)
	a.bodyPage.Append(bodyScroll)

	a.reqStack = gtk.NewStack()
	a.reqStack.SetVExpand(true)
	a.reqStack.AddTitled(a.params.root, "params", "Params")
	a.reqStack.AddTitled(a.headers.root, "headers", "Headers")
	a.reqStack.AddTitled(a.bodyPage, "body", "Body")
	switcher := gtk.NewStackSwitcher()
	switcher.SetStack(a.reqStack)
	switcher.SetHAlign(gtk.AlignStart)
	requestPage := gtk.NewBox(gtk.OrientationVertical, 8)
	requestPage.Append(switcher)
	requestPage.Append(a.reqStack)
	requestPage.SetMarginBottom(6)

	// Response: its status line, then the body or the headers.
	a.status = gtk.NewLabel("")
	a.status.AddCSSClass("wtf-api-status")
	a.meta = gtk.NewLabel("")
	a.meta.AddCSSClass("wtf-api-meta")
	a.meta.SetEllipsize(pango.EllipsizeMiddle)
	a.meta.SetHExpand(true)
	a.meta.SetXAlign(0)
	a.respBody = gtk.NewToggleButtonWithLabel("Body")
	a.respHeaders = gtk.NewToggleButtonWithLabel("Headers")
	a.respHeaders.SetGroup(a.respBody)
	a.respBody.SetActive(true)
	a.respBody.ConnectToggled(a.showResponse)
	a.respKind = gtk.NewBox(gtk.OrientationHorizontal, 0)
	a.respKind.AddCSSClass("linked")
	a.respKind.Append(a.respBody)
	a.respKind.Append(a.respHeaders)
	a.raw = gtk.NewToggleButtonWithLabel("Raw")
	a.raw.SetTooltipText("Show JSON as it came, not indented")
	a.raw.ConnectToggled(a.showResponse)
	a.copyResponse = gtk.NewButtonWithLabel("Copy")
	a.copyResponse.ConnectClicked(a.copyShown)
	statusRow := gtk.NewBox(gtk.OrientationHorizontal, 8)
	statusRow.Append(a.status)
	statusRow.Append(a.meta)
	statusRow.Append(a.respKind)
	statusRow.Append(a.raw)
	statusRow.Append(a.copyResponse)

	a.findEntry = gtk.NewSearchEntry()
	a.findEntry.SetHExpand(true)
	a.findEntry.SetPlaceholderText("Find in the response")
	a.findEntry.ConnectSearchChanged(func() { a.findFrom(true, false) })
	a.findEntry.ConnectActivate(func() { a.find(1) })
	a.findEntry.ConnectNextMatch(func() { a.find(1) })
	a.findEntry.ConnectPreviousMatch(func() { a.find(-1) })
	a.findEntry.ConnectStopSearch(func() { a.findBar.SetRevealChild(false) })
	a.findBar = gtk.NewRevealer()
	a.findBar.SetChild(a.findEntry)

	a.respView = gtk.NewTextView()
	a.respView.AddCSSClass("wtf-api-mono")
	a.respView.SetMonospace(true)
	a.respView.SetEditable(false)
	a.respView.SetWrapMode(gtk.WrapWordChar)
	a.respView.SetTopMargin(4)
	a.respView.SetLeftMargin(4)
	a.respScroll = gtk.NewScrolledWindow()
	a.respScroll.SetChild(a.respView)
	a.respScroll.SetVExpand(true)
	a.respScroll.AddCSSClass("wtf-api-response")
	a.hint = gtk.NewLabel("")
	a.hint.AddCSSClass("wtf-api-hint")
	a.hint.SetWrap(true)
	a.hint.SetVAlign(gtk.AlignStart)
	a.hint.SetVExpand(true)
	responsePage := gtk.NewBox(gtk.OrientationVertical, 6)
	responsePage.SetMarginTop(8)
	responsePage.Append(statusRow)
	responsePage.Append(a.findBar)
	responsePage.Append(a.respScroll)
	responsePage.Append(a.hint)

	a.paned = gtk.NewPaned(gtk.OrientationVertical)
	a.paned.SetStartChild(requestPage)
	a.paned.SetEndChild(responsePage)
	a.paned.SetResizeStartChild(false)
	a.paned.SetShrinkStartChild(false)
	a.paned.SetShrinkEndChild(false)
	a.paned.SetVExpand(true)
	// The request part starts at about 40% of the pane.
	a.paned.NotifyProperty("max-position", func() {
		if max := a.paned.ObjectProperty("max-position").(int); !a.placed && max > 200 {
			a.placed = true
			a.paned.SetPosition(max * 2 / 5)
		}
	})
	a.root.Append(a.paned)

	// Ctrl+Return sends from anywhere in the pane.
	keys := gtk.NewShortcutController()
	keys.AddShortcut(gtk.NewShortcut(gtk.NewShortcutTriggerParseString("<Control>Return"),
		gtk.NewCallbackAction(func(gtk.Widgetter, *glib.Variant) bool { a.sendOrCancel(); return true })))
	a.root.AddController(keys)
	// The keyboard went into this pane: it becomes the focused pane.
	focus := gtk.NewEventControllerFocus()
	focus.ConnectEnter(func() { a.w.model.FocusPane(a.id) })
	a.root.AddController(focus)
}

func (a *apiPane) moreMenu() *gio.MenuModel {
	group := gio.NewSimpleActionGroup()
	add := func(name string, run func()) {
		act := gio.NewSimpleAction(name, nil)
		act.ConnectActivate(func(*glib.Variant) { run() })
		group.AddAction(act)
	}
	add("copy-curl", a.copyCurl)
	add("run-shell", a.runInShell)
	add("paste-curl", a.pasteCurl)
	add("clear", a.clearResponse)
	a.insecure = gio.NewSimpleActionStateful("insecure", nil, glib.NewVariantBoolean(false))
	a.insecure.ConnectActivate(func(*glib.Variant) { a.toggleInsecure() })
	group.AddAction(a.insecure)
	a.root.InsertActionGroup("api", group)

	m := gio.NewMenu()
	curl := gio.NewMenu()
	curl.Append("Copy as curl", "api.copy-curl")
	curl.Append("Run in Shell", "api.run-shell")
	curl.Append("Paste curl Command", "api.paste-curl")
	m.AppendSection("", curl)
	other := gio.NewMenu()
	other.Append("Skip TLS Certificate Checks (-k)", "api.insecure")
	other.Append("Clear Response", "api.clear")
	m.AppendSection("", other)
	return &m.MenuModel
}

// Request ⇄ views

// load shows a request in the editor (a new tab, or an imported curl command).
func (a *apiPane) load(r apiclient.Request) {
	a.loading = true
	defer func() { a.loading = false }()
	a.req = r
	m := strings.ToUpper(r.Method)
	if m == "" {
		m = "GET"
	}
	found := -1
	for i := uint(0); i < a.methods.NItems(); i++ {
		if a.methods.String(i) == m {
			found = int(i)
		}
	}
	if found < 0 {
		a.methods.Append(m)
		found = int(a.methods.NItems()) - 1
	}
	a.method.SetSelected(uint(found))
	a.url.SetText(r.URL)
	a.lastURLLen = len([]rune(r.URL))
	a.params.set(uicore.APIParams(r.URL))
	a.headers.set(r.Headers)
	a.body.Buffer().SetText(r.Body)
	a.insecure.SetState(glib.NewVariantBoolean(r.Insecure))
	a.updateCounts()
}

func (a *apiPane) edited() {
	a.updateCounts()
	if ps := a.w.model.Panes[a.id]; ps != nil {
		r := a.req
		ps.API = &r
	}
	a.w.strip.refresh()
	a.w.updateTitle()
	a.w.scheduleSave()
}

// updateCounts: "Headers (2)", how many are sent.
func (a *apiPane) updateCounts() {
	count := func(fs []apiclient.Header) int {
		n := 0
		for _, f := range fs {
			if !f.Off && f.Name != "" {
				n++
			}
		}
		return n
	}
	title := func(name string, n int) string {
		if n > 0 {
			return fmt.Sprintf("%s (%d)", name, n)
		}
		return name
	}
	a.reqStack.Page(a.params.root).SetTitle(title("Params", count(a.params.fields)))
	a.reqStack.Page(a.headers.root).SetTitle(title("Headers", count(a.req.Headers)))
	body := "Body"
	if a.req.Body != "" {
		body = "Body •"
	}
	a.reqStack.Page(a.bodyPage).SetTitle(body)
}

func (a *apiPane) urlChanged() {
	if a.loading {
		return
	}
	text := a.url.Text()
	n := len([]rune(text))
	jumped := n-a.lastURLLen > 1
	a.lastURLLen = n
	// A pasted curl command fills in the whole request.
	if jumped && strings.HasPrefix(strings.TrimSpace(text), "curl ") {
		a.importCurl(text, a.req.URL, true)
		return
	}
	a.req.URL = text
	// Parameters follow the URL; ticked-off ones stay listed.
	params := uicore.APIParams(text)
	for _, f := range a.params.fields {
		if f.Off {
			params = append(params, f)
		}
	}
	a.params.set(params)
	a.edited()
}

func (a *apiPane) paramsEdited(fields []apiclient.Header) {
	a.req.URL = uicore.APIURL(a.req.URL, fields)
	a.loading = true
	a.url.SetText(a.req.URL)
	a.loading = false
	a.lastURLLen = len([]rune(a.req.URL))
	a.edited()
}

func (a *apiPane) headersEdited(fields []apiclient.Header) {
	a.req.Headers = fields
	a.edited()
}

func (a *apiPane) setContentType(t string) {
	hs := append([]apiclient.Header(nil), a.req.Headers...)
	set := false
	for i := range hs {
		if strings.EqualFold(hs[i].Name, "Content-Type") {
			hs[i].Value, hs[i].Off, set = t, false, true
			break
		}
	}
	if !set {
		hs = append(hs, apiclient.Header{Name: "Content-Type", Value: t})
	}
	a.headers.set(hs)
	a.headersEdited(hs)
	a.w.toast("Content-Type: " + t)
}

func (a *apiPane) formatBody() {
	text := bufferText(a.body.Buffer())
	if strings.TrimSpace(text) == "" {
		return
	}
	out, err := a.w.app.be.APIClient.FormatJSON(text)
	if err != nil {
		a.w.toast(err.Error())
		return
	}
	// One user action, so Ctrl+Z undoes it.
	buf := a.body.Buffer()
	buf.BeginUserAction()
	buf.SetText(out)
	buf.EndUserAction()
}

// importCurl reads a curl command into the editor; on failure the URL
// goes back to fallback (when restore).
func (a *apiPane) importCurl(text, fallback string, restore bool) {
	r, err := a.w.app.be.APIClient.ParseCurl(text)
	if err != nil {
		if restore {
			a.loading = true
			a.url.SetText(fallback)
			a.loading = false
			a.lastURLLen = len([]rune(fallback))
		}
		a.w.toast("Couldn't read the curl command: " + err.Error())
		return
	}
	// The entry is still handling its change: fill it in afterwards.
	coreglib.IdleAdd(func() {
		a.load(r)
		switch {
		case r.Body != "":
			a.reqStack.SetVisibleChildName("body")
		case len(r.Headers) > 0:
			a.reqStack.SetVisibleChildName("headers")
		default:
			a.reqStack.SetVisibleChildName("params")
		}
		a.edited()
		a.w.toast("Filled in from the curl command")
	})
}

// Sending

func (a *apiPane) sendOrCancel() {
	if a.sending {
		a.cancel()
	} else {
		a.sendRequest()
	}
}

func (a *apiPane) sendRequest() {
	if a.sending {
		return
	}
	a.sending = true
	a.failure = ""
	a.send.SetLabel("Cancel")
	a.send.RemoveCSSClass("suggested-action")
	a.send.SetTooltipText("Stop waiting for the response")
	a.status.SetText("Sending…")
	setClass(&a.status.Widget, "")
	a.meta.SetText("")
	r, flight, svc := a.req, a.flight, a.w.app.be.APIClient
	go func() {
		res, err := svc.Send(flight, r)
		onMainAsync(func() {
			a.sending = false
			a.send.SetLabel("Send")
			a.send.AddCSSClass("suggested-action")
			a.send.SetTooltipText("Send the request (Ctrl+Return)")
			if err != nil {
				a.res, a.failure = nil, err.Error()
			} else {
				a.res = &res
			}
			a.showResponse()
		})
	}()
}

func (a *apiPane) cancel() { a.w.app.be.APIClient.Cancel(a.flight) }

// close stops a request still in flight: the pane is closing.
func (a *apiPane) close() {
	if a.sending {
		a.cancel()
	}
	if a.unobserve != nil {
		a.unobserve()
		a.unobserve = nil
	}
}

// Response

func setClass(w *gtk.Widget, class string) {
	for _, c := range []string{"ok", "redirect", "fail"} {
		w.RemoveCSSClass(c)
	}
	if class != "" {
		w.AddCSSClass(class)
	}
}

func (a *apiPane) showResponse() {
	s := a.w.app.settings
	res := a.res
	showText := func(text string) {
		a.respScroll.SetVisible(text != "")
		a.hint.SetVisible(text == "")
		if bufferText(a.respView.Buffer()) != text {
			a.respView.Buffer().SetText(text)
		}
	}
	if res == nil {
		a.respKind.SetVisible(false)
		a.raw.SetVisible(false)
		a.copyResponse.SetVisible(false)
		a.meta.SetText("")
		a.respScroll.SetVisible(false)
		a.hint.SetVisible(true)
		if a.failure != "" {
			a.status.SetText("Failed")
			setClass(&a.status.Widget, "fail")
			a.hint.SetText(s.text(a.failure))
			setClass(&a.hint.Widget, "fail")
		} else {
			if !a.sending {
				a.status.SetText("")
			}
			a.hint.SetText("Press Send or Ctrl+Return to see the response here.")
			setClass(&a.hint.Widget, "")
		}
		return
	}
	a.status.SetText(fmt.Sprintf("%d %s", res.Status, res.StatusText))
	switch {
	case res.Status >= 200 && res.Status < 300:
		setClass(&a.status.Widget, "ok")
	case res.Status >= 300 && res.Status < 400:
		setClass(&a.status.Widget, "redirect")
	default:
		setClass(&a.status.Widget, "fail")
	}
	facts := []string{uicore.APIDuration(res.Millis), uicore.APISize(res.Size)}
	if res.Proto != "" {
		facts = append(facts, res.Proto)
	}
	if res.Truncated {
		facts = append(facts, "cut off at "+uicore.APISize(res.Size))
	}
	if a.req.Insecure {
		facts = append(facts, "TLS checks off")
	}
	// Redirected: where the response came from.
	if asked := strings.TrimSpace(a.req.URL); res.URL != asked && res.URL != "http://"+asked {
		facts = append(facts, "from "+s.text(res.URL))
	}
	a.meta.SetText(strings.Join(facts, " · "))
	a.meta.SetTooltipText(a.meta.Text())
	a.respKind.SetVisible(true)
	a.copyResponse.SetVisible(true)
	setClass(&a.hint.Widget, "")
	body := a.respBody.Active()
	a.raw.SetVisible(body && res.Pretty != "")
	if !body {
		lines := make([]string, len(res.Headers))
		for i, h := range res.Headers {
			lines[i] = h.Name + ": " + h.Value
		}
		a.hint.SetText("The response has no headers.")
		showText(s.text(strings.Join(lines, "\n")))
		return
	}
	if res.Binary {
		ct := res.ContentType
		if ct == "" {
			ct = "no Content-Type"
		}
		a.copyResponse.SetVisible(false)
		a.hint.SetText(fmt.Sprintf("The response isn't text (%s, %s), so it isn't shown.", ct, uicore.APISize(res.Size)))
		showText("")
		return
	}
	text := res.Body
	if res.Pretty != "" && !a.raw.Active() {
		text = res.Pretty
	}
	a.hint.SetText("The response has no body.")
	showText(s.text(text))
}

// copyShown copies the response as shown (secrets hidden when Hide
// Secrets is on).
func (a *apiPane) copyShown() {
	text := bufferText(a.respView.Buffer())
	if text == "" {
		return
	}
	a.w.win.Clipboard().SetText(text)
	if a.respBody.Active() {
		a.w.toast("Copied the response body")
	} else {
		a.w.toast("Copied the response headers")
	}
}

// The ⋯ menu

func (a *apiPane) sendsCredentials() bool {
	for _, h := range a.req.Headers {
		if !h.Off && h.Value != "" && uicore.APIIsSecret(h.Name) {
			return true
		}
	}
	return strings.Contains(a.req.URL, "@")
}

func (a *apiPane) copyCurl() {
	cmd := apiclient.Curl(a.req, true)
	shown := a.w.app.settings.text(cmd)
	if a.sendsCredentials() && shown == cmd {
		a.w.copySecret(cmd, "the curl command")
		return
	}
	a.w.win.Clipboard().SetText(shown)
	a.w.toast("Copied the curl command")
}

func (a *apiPane) runInShell() {
	if strings.TrimSpace(a.req.URL) == "" {
		a.w.toast("Enter a URL first")
		return
	}
	a.w.runFromAPI(a.id, apiclient.Curl(a.req, false))
}

func (a *apiPane) pasteCurl() {
	cb := a.w.win.Clipboard()
	cb.ReadTextAsync(context.Background(), func(res gio.AsyncResulter) {
		text, err := cb.ReadTextFinish(res)
		if text = strings.TrimSpace(text); err != nil || text == "" {
			a.w.toast("The clipboard is empty")
			return
		}
		a.importCurl(text, "", false)
	})
}

func (a *apiPane) toggleInsecure() {
	a.req.Insecure = !a.req.Insecure
	a.insecure.SetState(glib.NewVariantBoolean(a.req.Insecure))
	a.edited()
	if a.req.Insecure {
		a.w.toast("TLS certificates won't be checked (curl -k)")
	} else {
		a.w.toast("TLS certificates are checked")
	}
}

func (a *apiPane) clearResponse() {
	a.res, a.failure = nil, ""
	a.showResponse()
}

// Keyboard

// focus gives the keyboard to the URL field, unless something in the pane
// has it already.
func (a *apiPane) focus() {
	if f := a.w.win.Focus(); f != nil && gtk.BaseWidget(f).IsAncestor(a.root) {
		return
	}
	a.url.GrabFocus()
}

// showFind: Ctrl+Shift+F finds in the response.
func (a *apiPane) showFind() {
	if !a.respScroll.Visible() {
		return
	}
	a.findBar.SetRevealChild(true)
	a.findEntry.GrabFocus()
}

// find moves to the next (1) or previous (-1) match.
func (a *apiPane) find(dir int) {
	if !a.findBar.RevealChild() {
		a.showFind()
		return
	}
	a.findFrom(false, dir < 0)
}

// findFrom selects a match of the find text: the first one (from the top),
// or the next or previous one from the selection, wrapping.
func (a *apiPane) findFrom(top, back bool) {
	needle := a.findEntry.Text()
	buf := a.respView.Buffer()
	if needle == "" {
		it := buf.StartIter()
		buf.SelectRange(it, it)
		return
	}
	flags := gtk.TextSearchCaseInsensitive | gtk.TextSearchTextOnly
	start, end, _ := buf.SelectionBounds()
	var from *gtk.TextIter
	switch {
	case top:
		from = buf.StartIter()
	case back:
		from = start
	default:
		from = end
	}
	var ms, me *gtk.TextIter
	var ok bool
	if back {
		ms, me, ok = from.BackwardSearch(needle, flags, nil)
		if !ok {
			ms, me, ok = buf.EndIter().BackwardSearch(needle, flags, nil)
		}
	} else {
		ms, me, ok = from.ForwardSearch(needle, flags, nil)
		if !ok {
			ms, me, ok = buf.StartIter().ForwardSearch(needle, flags, nil)
		}
	}
	if !ok {
		a.findEntry.AddCSSClass("error")
		return
	}
	a.findEntry.RemoveCSSClass("error")
	buf.SelectRange(ms, me)
	a.respView.ScrollToIter(ms, 0.1, false, 0, 0)
}

func bufferText(b *gtk.TextBuffer) string {
	start, end := b.Bounds()
	return b.Text(start, end, true)
}

// fieldList is name/value rows with a tick to leave one out: query
// parameters, headers. Header values that are credentials
// (uicore.APIIsSecret) are masked until the eye is clicked.
type fieldList struct {
	root     *gtk.ScrolledWindow
	box      *gtk.Box
	add      *gtk.Button
	noun     string
	masks    bool
	fields   []apiclient.Header
	rows     []*fieldRow
	revealed map[int]bool
	onChange func([]apiclient.Header)
	filling  bool
}

type fieldRow struct {
	box   *gtk.Box
	tick  *gtk.CheckButton
	name  *gtk.Entry
	value *gtk.Entry
}

func newFieldList(noun string, masks bool, onChange func([]apiclient.Header)) *fieldList {
	f := &fieldList{noun: noun, masks: masks, onChange: onChange, revealed: map[int]bool{}}
	f.box = gtk.NewBox(gtk.OrientationVertical, 6)
	// Always last, so an empty list still has a way to add one.
	f.add = gtk.NewButton()
	content := gtk.NewBox(gtk.OrientationHorizontal, 6)
	content.Append(gtk.NewImageFromIconName("list-add-symbolic"))
	content.Append(gtk.NewLabel("Add " + noun))
	f.add.SetChild(content)
	f.add.AddCSSClass("flat")
	f.add.SetHAlign(gtk.AlignStart)
	f.add.ConnectClicked(f.addRow)
	f.box.Append(f.add)
	f.root = gtk.NewScrolledWindow()
	f.root.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	f.root.SetChild(f.box)
	f.root.SetVExpand(true)
	return f
}

// set shows these fields. With a row being typed in, the rows stay and
// only the others are updated.
func (f *fieldList) set(fs []apiclient.Header) {
	fs = append([]apiclient.Header(nil), fs...)
	if len(fs) == len(f.rows) {
		f.fields = fs
		f.filling = true
		for i, r := range f.rows {
			if !r.typing() {
				f.fill(i)
			}
		}
		f.filling = false
		return
	}
	f.fields = fs
	f.rebuild()
}

// typing: one of the row's entries has the keyboard (their GtkText child does).
func (r *fieldRow) typing() bool { return focusWithin(&r.name.Widget) || focusWithin(&r.value.Widget) }

func focusWithin(w *gtk.Widget) bool { return w.StateFlags()&gtk.StateFlagFocusWithin != 0 }

func (f *fieldList) rebuild() {
	for _, r := range f.rows {
		f.box.Remove(r.box)
	}
	f.rows = nil
	for k := range f.revealed {
		if k >= len(f.fields) {
			delete(f.revealed, k)
		}
	}
	f.filling = true
	var after gtk.Widgetter // nil: first in the box, before Add
	for i := range f.fields {
		r := f.makeRow(i)
		f.rows = append(f.rows, r)
		f.box.InsertChildAfter(r.box, after)
		after = r.box
		f.fill(i)
	}
	f.filling = false
}

func (f *fieldList) makeRow(i int) *fieldRow {
	r := &fieldRow{box: gtk.NewBox(gtk.OrientationHorizontal, 6)}
	r.tick = gtk.NewCheckButton()
	r.tick.SetTooltipText("Send this " + strings.ToLower(f.noun))
	r.tick.ConnectToggled(func() {
		if f.filling || i >= len(f.fields) {
			return
		}
		f.fields[i].Off = !r.tick.Active()
		f.onChange(f.copy())
	})
	r.name = gtk.NewEntry()
	r.name.SetPlaceholderText("Name")
	r.name.AddCSSClass("wtf-api-mono")
	r.name.SetHExpand(true)
	r.name.ConnectChanged(func() {
		if f.filling || i >= len(f.fields) {
			return
		}
		f.fields[i].Name = r.name.Text()
		// Naming a header Authorization masks its value from then on.
		f.mask(i)
		f.onChange(f.copy())
	})
	r.value = gtk.NewEntry()
	r.value.SetPlaceholderText("Value")
	r.value.AddCSSClass("wtf-api-mono")
	r.value.SetHExpand(true)
	r.value.ConnectChanged(func() {
		if f.filling || i >= len(f.fields) {
			return
		}
		f.fields[i].Value = r.value.Text()
		f.onChange(f.copy())
	})
	r.value.ConnectIconPress(func(gtk.EntryIconPosition) {
		f.revealed[i] = !f.revealed[i]
		f.mask(i)
	})
	// The name a third of the row, the value the rest.
	names := gtk.NewBox(gtk.OrientationHorizontal, 0)
	names.SetHExpand(true)
	names.Append(r.name)
	values := gtk.NewBox(gtk.OrientationHorizontal, 0)
	values.SetHExpand(true)
	values.Append(r.value)
	sizes := gtk.NewBox(gtk.OrientationHorizontal, 6)
	sizes.SetHomogeneous(false)
	sizes.SetHExpand(true)
	r.name.SetWidthChars(14)
	r.value.SetWidthChars(28)
	sizes.Append(names)
	sizes.Append(values)
	remove := gtk.NewButtonFromIconName("window-close-symbolic")
	remove.AddCSSClass("flat")
	remove.SetTooltipText("Remove")
	remove.ConnectClicked(func() { f.removeRow(i) })
	r.box.Append(r.tick)
	r.box.Append(sizes)
	r.box.Append(remove)
	return r
}

// fill puts field i's text in its row, masking a credential's value.
func (f *fieldList) fill(i int) {
	r, fd := f.rows[i], f.fields[i]
	r.tick.SetActive(!fd.Off)
	if r.name.Text() != fd.Name {
		r.name.SetText(fd.Name)
	}
	if r.value.Text() != fd.Value {
		r.value.SetText(fd.Value)
	}
	f.mask(i)
}

func (f *fieldList) mask(i int) {
	if i >= len(f.rows) {
		return
	}
	r := f.rows[i]
	secret := f.masks && uicore.APIIsSecret(f.fields[i].Name)
	r.value.SetVisibility(!secret || f.revealed[i])
	switch {
	case !secret:
		r.value.SetIconFromIconName(gtk.EntryIconSecondary, "")
	case f.revealed[i]:
		r.value.SetIconFromIconName(gtk.EntryIconSecondary, "view-conceal-symbolic")
		r.value.SetIconTooltipText(gtk.EntryIconSecondary, "Hide the value")
	default:
		r.value.SetIconFromIconName(gtk.EntryIconSecondary, "view-reveal-symbolic")
		r.value.SetIconTooltipText(gtk.EntryIconSecondary, "Show the value")
	}
}

func (f *fieldList) copy() []apiclient.Header { return append([]apiclient.Header(nil), f.fields...) }

func (f *fieldList) removeRow(i int) {
	if i >= len(f.fields) {
		return
	}
	f.fields = append(f.fields[:i], f.fields[i+1:]...)
	revealed := map[int]bool{}
	for k, v := range f.revealed {
		switch {
		case k < i:
			revealed[k] = v
		case k > i:
			revealed[k-1] = v
		}
	}
	f.revealed = revealed
	f.rebuild()
	f.onChange(f.copy())
}

func (f *fieldList) addRow() {
	f.fields = append(f.fields, apiclient.Header{})
	f.rebuild()
	f.onChange(f.copy())
	if n := len(f.rows); n > 0 {
		f.rows[n-1].name.GrabFocus()
	}
}

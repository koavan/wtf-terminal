//go:build linux

package gtkui

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/dayanraj/wtf/internal/certs"
	"github.com/dayanraj/wtf/internal/uicore"
)

// The token and certificate inspector's dialogs: JSON, JWT, decoded
// base64 and certificates (a Kubernetes secret's is showSecret). One at a
// time: opening one closes the last. Decoded values live only while the
// dialog is open. Like the macOS app's Viewers.swift.

func init() {
	extraCSS = append(extraCSS, `
.wtf-viewer { padding: 4px 16px 16px; }
.wtf-viewer-heading { font-size: 0.78em; font-weight: bold; opacity: 0.7; }
.wtf-viewer-key { opacity: 0.65; }
.wtf-viewer-mono { font-family: monospace; }
.wtf-viewer-small { font-size: 0.92em; }
.wtf-viewer-kind { font-weight: bold; }
.wtf-viewer-box { font-family: monospace; font-size: 0.92em; background: alpha(currentColor, 0.05); border-radius: 8px; }
.wtf-viewer-tag { border-radius: 5px; padding: 0 6px; font-size: 0.85em; font-weight: bold; background: alpha(currentColor, 0.1); }
.wtf-viewer-tag.good { color: @success_color; background: alpha(@success_color, 0.1); }
.wtf-viewer-tag.bad { color: @error_color; background: alpha(@error_color, 0.16); }
`)
}

// viewer is a modal dialog of the inspector: a title, what it says, and
// Done with other buttons at its foot.
type viewer struct {
	w        *Window
	win      *adw.Window
	title    *adw.WindowTitle
	sub      *gtk.Label
	content  *gtk.Box
	buttons  *gtk.Box
	closed   bool
	onClosed func()
	// Runs when wtf stops being in front.
	focusLost func()
	ownerSig  coreglib.SignalHandle
}

var shownViewer *viewer

// newViewer makes a viewer, closing whatever viewer was open; show presents it.
func newViewer(w *Window, width int) *viewer {
	if shownViewer != nil {
		shownViewer.win.Close()
	}
	if shownSecret != nil {
		shownSecret.win.Close()
	}
	v := &viewer{w: w}
	shownViewer = v
	v.win = adw.NewWindow()
	v.win.SetTransientFor(&w.win.Window)
	v.win.SetModal(true)
	v.win.SetDefaultSize(width, -1)
	v.title = adw.NewWindowTitle("", "")
	head := adw.NewHeaderBar()
	head.SetTitleWidget(v.title)
	v.sub = gtk.NewLabel("")
	v.sub.SetXAlign(0)
	v.sub.SetWrap(true)
	v.sub.AddCSSClass("dim-label")
	v.content = gtk.NewBox(gtk.OrientationVertical, 10)
	v.buttons = gtk.NewBox(gtk.OrientationHorizontal, 8)
	body := gtk.NewBox(gtk.OrientationVertical, 12)
	body.AddCSSClass("wtf-viewer")
	body.Append(v.sub)
	body.Append(v.content)
	body.Append(v.buttons)
	view := adw.NewToolbarView()
	view.AddTopBar(head)
	view.SetContent(body)
	v.win.SetContent(view)

	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(func(keyval, _ uint, _ gdk.ModifierType) bool {
		if keyval == gdk.KEY_Escape {
			v.win.Close()
			return true
		}
		return false
	})
	v.win.AddController(keys)
	v.win.ConnectCloseRequest(func() bool {
		if v.ownerSig != 0 {
			w.win.HandlerDisconnect(v.ownerSig)
		}
		v.closed = true
		if v.onClosed != nil {
			v.onClosed()
		}
		if shownViewer == v {
			shownViewer = nil
		}
		return false
	})
	return v
}

func (v *viewer) show() { v.win.Present() }

// onLosingFocus runs f once neither the viewer nor its window is in front
// (checked after the hand-over between them).
func (v *viewer) onLosingFocus(f func()) {
	v.focusLost = f
	changed := func() {
		onMainAsync(func() {
			if !v.closed && !v.win.IsActive() && !v.w.win.IsActive() {
				v.focusLost()
			}
		})
	}
	v.win.NotifyProperty("is-active", changed)
	v.ownerSig = v.w.win.NotifyProperty("is-active", changed)
}

func (v *viewer) setTitle(title, sub string) {
	v.title.SetTitle(title)
	v.win.SetTitle(title)
	v.sub.SetText(sub)
}

// clear empties the viewer for render.
func (v *viewer) clear() {
	for _, b := range []*gtk.Box{v.content, v.buttons} {
		for c := b.FirstChild(); c != nil; c = b.FirstChild() {
			b.Remove(c)
		}
	}
}

func (v *viewer) add(w gtk.Widgetter) { v.content.Append(w) }

// maxBoxHeight is the window's height: boxes take a share of it.
func (v *viewer) maxBoxHeight() int {
	if h := v.w.win.Height(); h > 0 {
		return h
	}
	return 700
}

// textBox is a selectable monospace box: wrapping, or not (scrolling
// sideways).
func (v *viewer) textBox(text string, maxHeight int, wrap bool) (*gtk.ScrolledWindow, *gtk.TextBuffer) {
	tv := gtk.NewTextView()
	tv.SetEditable(false)
	tv.SetCursorVisible(false)
	tv.SetMonospace(true)
	tv.SetTopMargin(8)
	tv.SetBottomMargin(8)
	tv.SetLeftMargin(8)
	tv.SetRightMargin(8)
	tv.AddCSSClass("wtf-viewer-box")
	hpolicy := gtk.PolicyAutomatic
	if wrap {
		tv.SetWrapMode(gtk.WrapWordChar)
		hpolicy = gtk.PolicyNever
	}
	tv.Buffer().SetText(text)
	box := gtk.NewScrolledWindow()
	box.SetPolicy(hpolicy, gtk.PolicyAutomatic)
	box.SetPropagateNaturalHeight(true)
	box.SetMinContentHeight(28)
	box.SetMaxContentHeight(maxHeight)
	box.SetChild(tv)
	return box, tv.Buffer()
}

// viewerTag is a small rounded tag (expired, server, CA…): class "good", "bad"
// or "".
func viewerTag(text, class string) *gtk.Label {
	l := gtk.NewLabel(text)
	l.AddCSSClass("wtf-viewer-tag")
	if class != "" {
		l.AddCSSClass(class)
	}
	l.SetVAlign(gtk.AlignCenter)
	return l
}

// viewerValue is a selectable wrapping value; classes like "wtf-viewer-mono",
// "error" or "dim-label".
func viewerValue(s string, classes ...string) *gtk.Label {
	l := gtk.NewLabel(s)
	l.SetXAlign(0)
	l.SetWrap(true)
	l.SetWrapMode(pango.WrapWordChar)
	l.SetSelectable(true)
	l.SetHExpand(true)
	for _, c := range classes {
		l.AddCSSClass(c)
	}
	return l
}

func viewerHeading(s string) *gtk.Label {
	l := gtk.NewLabel(strings.ToUpper(s))
	l.SetXAlign(0)
	l.AddCSSClass("wtf-viewer-heading")
	return l
}

func viewerRow(ws ...gtk.Widgetter) *gtk.Box {
	b := gtk.NewBox(gtk.OrientationHorizontal, 6)
	for _, w := range ws {
		b.Append(w)
	}
	return b
}

type viewerFact struct {
	key   string
	value gtk.Widgetter
}

// viewerFacts are label-value rows.
func viewerFacts(rows []viewerFact) *gtk.Grid {
	g := gtk.NewGrid()
	g.SetColumnSpacing(12)
	g.SetRowSpacing(5)
	for i, r := range rows {
		k := gtk.NewLabel(r.key)
		k.SetXAlign(0)
		k.SetYAlign(0)
		k.SetVAlign(gtk.AlignStart)
		k.SetSizeRequest(104, -1)
		k.AddCSSClass("wtf-viewer-key")
		g.Attach(k, 0, i, 1, 1)
		g.Attach(r.value, 1, i, 1, 1)
	}
	return g
}

// done puts buttons at the foot: the left ones, then Done.
func (v *viewer) done(left ...*gtk.Button) {
	for _, b := range left {
		v.buttons.Append(b)
	}
	d := gtk.NewButtonWithLabel("Done")
	d.AddCSSClass("suggested-action")
	d.SetHExpand(true)
	d.SetHAlign(gtk.AlignEnd)
	d.ConnectClicked(func() { v.win.Close() })
	v.buttons.Append(d)
	v.win.SetDefaultWidget(d)
	// Not a selectable label: it would open with its text selected.
	v.win.SetFocus(d)
}

func viewerButton(label string, run func()) *gtk.Button {
	b := gtk.NewButtonWithLabel(label)
	b.ConnectClicked(run)
	return b
}

// viewerTime is "Oct 9, 2026, 12:34:56".
func viewerTime(t time.Time) string { return t.Local().Format("Jan 2, 2006, 15:04:05") }

// openInspect opens the viewer an inspect action asks for.
func (w *Window) openInspect(a uicore.InspectAction) {
	switch a.Kind {
	case uicore.InspectActionJWT:
		w.showJWT(a.Text)
	case uicore.InspectActionBase64:
		w.showDecoded(a.Text)
	case uicore.InspectActionCertText:
		w.showCertText(a.Text)
	case uicore.InspectActionTLS:
		w.showCertHost(a.Text)
	}
}

// JSON

var viewerJSONToken = regexp.MustCompile(`("(?:[^"\\]|\\.)*")(\s*:)?|\b(true|false|null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)`)

// showJSON shows pretty-printed JSON from the terminal's output, colored:
// keys blue, strings green, literals purple, numbers orange.
func (w *Window) showJSON(json string) {
	v := newViewer(w, 640)
	v.setTitle("JSON", strconv.Itoa(strings.Count(json, "\n")+1)+" lines, pretty-printed from the terminal's output.")
	box, buf := v.textBox(json, v.maxBoxHeight()*6/10, false)
	colors := [4]string{"#1c71d8", "#26a269", "#9141ac", "#c64600"}
	if w.app.settings.style.Dark() {
		colors = [4]string{"#78aeed", "#8ff0a4", "#dc8add", "#ffa348"}
	}
	tags := [4]*gtk.TextTag{}
	for i, c := range colors {
		tags[i] = gtk.NewTextTag(strconv.Itoa(i))
		tags[i].SetObjectProperty("foreground", c)
		buf.TagTable().Add(tags[i])
	}
	// Byte offsets to character offsets, in one pass.
	at, chars := 0, 0
	char := func(i int) int {
		chars += utf8.RuneCountInString(json[at:i])
		at = i
		return chars
	}
	apply := func(t *gtk.TextTag, from, to int) {
		start := buf.IterAtOffset(char(from))
		end := buf.IterAtOffset(char(to))
		buf.ApplyTag(t, start, end)
	}
	for _, m := range viewerJSONToken.FindAllStringSubmatchIndex(json, -1) {
		switch {
		case m[2] >= 0 && m[4] >= 0:
			apply(tags[0], m[2], m[3])
		case m[2] >= 0:
			apply(tags[1], m[2], m[3])
		case m[6] >= 0:
			apply(tags[2], m[6], m[7])
		case m[8] >= 0:
			apply(tags[3], m[8], m[9])
		}
	}
	v.add(box)
	v.done(viewerButton("Copy", func() {
		w.win.Clipboard().SetText(json)
		w.toast("Copied JSON")
	}))
	v.show()
}

// JWT

var jwtTimeLabels = map[string]string{"exp": "Expires", "nbf": "Not valid before", "iat": "Issued", "auth_time": "Signed in"}

// showJWT shows what a JWT says (its signature isn't checked).
func (w *Window) showJWT(token string) {
	v := newViewer(w, 600)
	jwt, ok := uicore.InspectDecodeJWT(token)
	v.onClosed = func() { jwt = uicore.InspectJWT{} }
	v.setTitle("JWT", "Decoded from the terminal's output. The signature isn't verified: wtf has no key to check it with.")
	if !ok {
		v.add(viewerValue("This isn't a JWT wtf can read.", "error"))
		v.done()
		v.show()
		return
	}
	now := time.Now()
	var exp, nbf *time.Time
	for _, t := range jwt.Times {
		at := t.At
		switch t.Claim {
		case "exp":
			exp = &at
		case "nbf":
			nbf = &at
		}
	}
	var status *gtk.Label
	switch {
	case exp != nil && !exp.After(now):
		status = viewerTag("expired", "bad")
	case nbf != nil && nbf.After(now):
		status = viewerTag("not yet valid", "bad")
	case exp != nil:
		status = viewerTag("not expired", "good")
	default:
		status = viewerTag("no expiry", "")
	}
	status.SetHAlign(gtk.AlignStart)
	rows := []viewerFact{{"Status", status}}
	for _, t := range jwt.Times {
		label := jwtTimeLabels[t.Claim]
		if label == "" {
			label = t.Claim
		}
		rows = append(rows, viewerFact{label, viewerValue(viewerTime(t.At) + " · " + uicore.RelativeTime(t.At, now))})
	}
	if iss, ok := jwt.Claims.Get("iss"); ok && iss.Kind == uicore.JSONString {
		rows = append(rows, viewerFact{"Issuer", viewerValue(iss.Str, "wtf-viewer-mono")})
	}
	if sub, ok := jwt.Claims.Get("sub"); ok && sub.Kind == uicore.JSONString {
		rows = append(rows, viewerFact{"Subject", viewerValue(sub.Str, "wtf-viewer-mono")})
	}
	if aud, ok := jwt.Claims.Get("aud"); ok {
		rows = append(rows, viewerFact{"Audience", viewerValue(jwtAudience(aud), "wtf-viewer-mono")})
	}
	alg := jwt.Alg
	if alg == "" {
		alg = "none"
	}
	if !jwt.Signed {
		alg += " · unsigned"
	}
	rows = append(rows, viewerFact{"Algorithm", viewerValue(alg, "wtf-viewer-mono")})
	v.add(viewerFacts(rows))
	claims := jwt.Claims.Pretty()
	v.add(viewerHeading("Claims"))
	box, _ := v.textBox(claims, v.maxBoxHeight()*32/100, true)
	v.add(box)
	v.add(viewerHeading("Header"))
	box, _ = v.textBox(jwt.Header.Pretty(), v.maxBoxHeight()*20/100, true)
	v.add(box)
	v.done(viewerButton("Copy Claims", func() {
		w.win.Clipboard().SetText(claims)
		w.toast("Copied claims")
	}))
	v.show()
}

// jwtAudience is `aud` as JavaScript's [aud].flat().join(", ") prints it.
func jwtAudience(v uicore.JSONValue) string {
	var str func(v uicore.JSONValue) string
	str = func(v uicore.JSONValue) string {
		switch v.Kind {
		case uicore.JSONString:
			return v.Str
		case uicore.JSONNumber:
			return uicore.JSNumber(v.Num)
		case uicore.JSONBool:
			if v.Bool {
				return "true"
			}
			return "false"
		case uicore.JSONArray:
			parts := make([]string, len(v.Items))
			for i, x := range v.Items {
				parts[i] = str(x)
			}
			return strings.Join(parts, ",")
		case uicore.JSONObject:
			return "[object Object]"
		}
		return ""
	}
	if v.Kind == uicore.JSONArray {
		parts := make([]string, len(v.Items))
		for i, x := range v.Items {
			parts[i] = str(x)
		}
		return strings.Join(parts, ", ")
	}
	return str(v)
}

// Decoded base64

var decodedKindLabels = map[uicore.InspectKind]string{
	uicore.InspectCertificate: "PEM certificate", uicore.InspectJWTKind: "JWT", uicore.InspectJSON: "JSON",
	uicore.InspectText: "Text", uicore.InspectBinary: "Binary data",
}

// decodedViewer shows a decoded base64 value, hidden until revealed and
// hidden again when wtf loses focus.
type decodedViewer struct {
	*viewer
	decoded *uicore.InspectDecoded
	shown   bool
}

// showDecoded decodes base64 text and shows what it turned out to be.
func (w *Window) showDecoded(text string) {
	v := &decodedViewer{viewer: newViewer(w, 600)}
	if d, ok := uicore.InspectDecodeBase64(text); ok {
		v.decoded = &d
	}
	v.onClosed = func() { v.decoded = nil }
	v.onLosingFocus(func() {
		if v.shown {
			v.shown = false
			v.render()
		}
	})
	v.render()
	v.show()
}

func (v *decodedViewer) render() {
	v.clear()
	v.setTitle("Decoded Base64", "Hidden until you reveal it, and hidden again when wtf loses focus.")
	d := v.decoded
	if d == nil {
		v.add(viewerValue("This isn't base64 wtf can decode.", "error"))
		v.done()
		return
	}
	kind := gtk.NewLabel(decodedKindLabels[d.Kind])
	kind.AddCSSClass("wtf-viewer-kind")
	meta := uicore.InspectSize(d.Size, "")
	if d.Kind == uicore.InspectBinary && d.Size > 256 {
		meta += " · first 256 shown"
	}
	metaL := gtk.NewLabel(meta)
	metaL.AddCSSClass("dim-label")
	metaL.SetHExpand(true)
	metaL.SetXAlign(0)
	icon, tip := "view-reveal-symbolic", "Reveal"
	if v.shown {
		icon, tip = "view-conceal-symbolic", "Hide"
	}
	eye := gtk.NewButtonFromIconName(icon)
	eye.AddCSSClass("flat")
	eye.SetTooltipText(tip)
	eye.ConnectClicked(v.toggle)
	v.add(viewerRow(kind, metaL, eye))
	if v.shown {
		box, _ := v.textBox(d.Text, v.maxBoxHeight()/2, d.Kind != uicore.InspectBinary)
		v.add(box)
	} else {
		mask := gtk.NewButtonWithLabel("••••••••  Reveal")
		mask.SetSizeRequest(-1, 36)
		mask.AddCSSClass("dim-label")
		mask.ConnectClicked(v.toggle)
		v.add(mask)
	}
	w := v.w
	var left []*gtk.Button
	text := d.Text
	if d.Kind == uicore.InspectCertificate {
		left = append(left, viewerButton("Inspect Certificate…", func() { w.showCertText(text) }))
	}
	if d.Kind == uicore.InspectJWTKind {
		left = append(left, viewerButton("Inspect JWT…", func() { w.showJWT(uicore.InspectFirstJWT(text)) }))
	}
	if d.Kind != uicore.InspectBinary {
		left = append(left, viewerButton("Copy", func() { w.copySecret(text, "decoded text") }))
	}
	v.done(left...)
}

func (v *decodedViewer) toggle() {
	v.shown = !v.shown
	v.render()
}

// Certificates

type certSource int

const (
	certHost certSource = iota
	certFile
	certText
)

// certViewer shows a certificate's details: from text, a file, or the
// chain a TLS server presents.
type certViewer struct {
	*viewer
	source  certSource
	arg     string // the file or text
	host    string
	report  *certs.Report
	err     string
	busy    bool
	now     time.Time
	seq     int
	inspect *gtk.Button
}

var (
	certScheme   = regexp.MustCompile(`^\w+://`)
	certHostTail = regexp.MustCompile(`[:/].*$`)
)

// showCertHost shows the certificates a server presents ("" asks for one).
func (w *Window) showCertHost(host string) { w.showCert(certHost, host) }

// showCertFile shows the certificates in a file.
func (w *Window) showCertFile(path string) { w.showCert(certFile, path) }

// showCertText shows the certificates in some text.
func (w *Window) showCertText(text string) { w.showCert(certText, text) }

func (w *Window) showCert(source certSource, arg string) {
	v := &certViewer{viewer: newViewer(w, 620), source: source}
	if source == certHost {
		v.host = arg
	} else {
		v.arg = arg
	}
	v.onClosed = func() { v.report = nil }
	v.render()
	v.show()
	v.run()
}

func (v *certViewer) hostMode() bool { return v.source == certHost }

// run reads the certificates (connects, for a server).
func (v *certViewer) run() {
	h := strings.TrimSpace(v.host)
	if v.hostMode() && h == "" {
		return
	}
	v.err, v.report, v.busy = "", nil, true
	v.seq++
	seq := v.seq
	v.render()
	svc, source, arg := v.w.app.be.Certs, v.source, v.arg
	go func() {
		var r certs.Report
		var err error
		switch source {
		case certFile:
			r, err = svc.InspectFile(arg)
		case certText:
			r, err = svc.InspectText(arg)
		default:
			r, err = svc.InspectHost(h)
		}
		onMainAsync(func() {
			if v.closed || seq != v.seq {
				return
			}
			if err != nil {
				v.err = err.Error()
			} else {
				v.report = &r
				v.now = time.Now()
			}
			v.busy = false
			v.render()
		})
	}()
}

func (v *certViewer) canInspect() bool { return !v.busy && strings.TrimSpace(v.host) != "" }

func (v *certViewer) render() {
	v.clear()
	switch v.source {
	case certHost:
		v.setTitle("TLS Certificate", "Connects to the server and shows the certificates it presents. Nothing is sent but the TLS handshake.")
	case certFile:
		v.setTitle("Certificate", uicore.PrettyPath(v.arg, v.w.app.home))
	default:
		v.setTitle("Certificate", "From the selected text.")
	}
	if v.hostMode() {
		e := gtk.NewEntry()
		e.SetText(v.host)
		e.SetPlaceholderText("example.com, host:8443 or https://…")
		e.AddCSSClass("wtf-viewer-mono")
		e.SetHExpand(true)
		v.inspect = viewerButton("Inspect", v.run)
		v.inspect.SetSensitive(v.canInspect())
		e.ConnectChanged(func() {
			v.host = e.Text()
			v.inspect.SetSensitive(v.canInspect())
		})
		e.ConnectActivate(func() {
			if v.canInspect() {
				v.run()
			}
		})
		v.add(viewerRow(e, v.inspect))
		if v.host == "" {
			v.win.SetFocus(e)
		}
	}
	switch r := v.report; {
	case v.err != "":
		v.add(viewerValue(v.err, "error"))
	case v.busy:
		msg := "Reading…"
		if v.hostMode() {
			msg = "Connecting to " + strings.TrimSpace(v.host) + "…"
		}
		v.add(viewerValue(msg, "dim-label"))
	case r != nil:
		if v.hostMode() {
			name := certHostTail.ReplaceAllString(certScheme.ReplaceAllString(strings.TrimSpace(v.host), ""), "")
			var trust *gtk.Box
			if r.Verified {
				trust = viewerRow(viewerTag("verifies", "good"), viewerValue("against this computer's trusted roots, for "+name))
			} else {
				trust = viewerRow(viewerTag("doesn't verify", "bad"), viewerValue(r.VerifyError, "wtf-viewer-mono"))
			}
			proto := r.Protocol + " · " + r.Cipher
			if r.ALPN != "" {
				proto += " · " + r.ALPN
			}
			v.add(viewerFacts([]viewerFact{
				{"Connected to", viewerValue(r.Address, "wtf-viewer-mono")},
				{"Protocol", viewerValue(proto, "wtf-viewer-mono")},
				{"Trust", trust},
			}))
		}
		for _, n := range r.Notes {
			v.add(viewerValue(n, "dim-label"))
		}
		list := gtk.NewBox(gtk.OrientationVertical, 10)
		for i, c := range r.Certificates {
			list.Append(v.card(i, c))
		}
		scroll := gtk.NewScrolledWindow()
		scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
		scroll.SetPropagateNaturalHeight(true)
		scroll.SetMaxContentHeight(v.maxBoxHeight() * 56 / 100)
		scroll.SetChild(list)
		v.add(scroll)
	}
	v.done()
}

func (v *certViewer) card(i int, c certs.Cert) *gtk.Box {
	subject := c.Subject
	if subject == "" {
		subject = "(no subject)"
	}
	head := viewerRow(viewerValue(subject, "wtf-viewer-kind"))
	if v.hostMode() {
		role := "intermediate"
		switch {
		case i == 0:
			role = "server"
		case c.SelfSigned:
			role = "root"
		}
		head.Append(viewerTag(role, ""))
	}
	if c.IsCA {
		head.Append(viewerTag("CA", ""))
	}
	if c.SelfSigned {
		head.Append(viewerTag("self-signed", ""))
	}
	nb, na := time.UnixMilli(c.NotBefore), time.UnixMilli(c.NotAfter)
	short := func(t time.Time) string { return t.Local().Format("Jan 2, 2006, 15:04") }
	var validity *gtk.Label
	switch {
	case !na.After(v.now):
		validity = viewerTag("expired "+uicore.RelativeTime(na, v.now), "bad")
	case nb.After(v.now):
		validity = viewerTag("not valid until "+uicore.RelativeTime(nb, v.now), "bad")
	default:
		validity = viewerTag("expires "+uicore.RelativeTime(na, v.now), "")
	}
	rows := []viewerFact{{"Valid", viewerRow(viewerValue(short(nb)+" – "+short(na)), validity)}}
	if len(c.Names) > 0 {
		rows = append(rows, viewerFact{"Names", viewerValue(strings.Join(c.Names, ", "), "wtf-viewer-mono")})
	}
	rows = append(rows,
		viewerFact{"Issuer", viewerValue(c.Issuer)},
		viewerFact{"Key", viewerValue(c.Key + " · signed with " + c.SignatureAlgorithm)},
		viewerFact{"Serial", viewerValue(c.Serial, "wtf-viewer-mono", "wtf-viewer-small")})
	w, fp := v.w, c.SHA256
	copyB := viewerButton("Copy", func() {
		w.win.Clipboard().SetText(fp)
		w.toast("Copied fingerprint")
	})
	copyB.AddCSSClass("flat")
	copyB.SetVAlign(gtk.AlignStart)
	rows = append(rows, viewerFact{"SHA-256", viewerRow(viewerValue(fp, "wtf-viewer-mono", "wtf-viewer-small"), copyB)})
	card := gtk.NewBox(gtk.OrientationVertical, 6)
	card.AddCSSClass("wtf-card")
	card.Append(head)
	card.Append(viewerFacts(rows))
	return card
}

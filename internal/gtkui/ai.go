//go:build linux

package gtkui

import (
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/dayanraj/wtf/internal/ai"
)

func init() {
	extraCSS = append(extraCSS, `
.wtf-ai-route { font-size: 0.92em; }
.wtf-ai-note { background: alpha(@accent_color, 0.10); border-radius: 8px; padding: 8px 10px; }
`)
}

// AI requests, for every AI feature: what's sent is shown before the first
// request to a provider (and on request), then the reply streams in. Like
// the macOS app's AIRequest and AIPreview.

// aiCall is one request: previewed, maybe confirmed, then streaming. Main
// loop only.
type aiCall struct {
	w       *Window
	id      string
	onChunk func(text string)
	onDone  func(ai.Done)
	offs    []func()
	// Ask was called; until then a cancel ends the call here.
	sent      bool
	cancelled bool
	finished  bool
	preview   *aiPreview
}

// aiSeq numbers requests (main loop only).
var aiSeq int

// askAI sends req to the AI, from a dialog over parent (nil for the
// window). Before the first request to a provider it shows exactly what will
// be sent and waits for Send. onChunk gets the reply as it streams in;
// onDone ends every call, whether it replied, failed, was stopped or the
// preview was cancelled (Done.Cancelled). Without Env, the focused shell's
// variables go along, so a command provider is found on its PATH.
func (w *Window) askAI(parent *gtk.Window, req ai.Request, onChunk func(string), onDone func(ai.Done)) *aiCall {
	aiSeq++
	c := &aiCall{w: w, id: fmt.Sprintf("gtk-%d", aiSeq), onChunk: onChunk, onDone: onDone}
	if parent == nil {
		parent = &w.win.Window
	}
	req = w.aiRequest(req)
	svc := w.app.be.AI
	go func() {
		out, err := svc.Preview(req)
		onMainAsync(func() {
			switch {
			case c.finished:
			case err != nil:
				c.finish(ai.Done{ID: c.id, Error: err.Error()})
			case out.FirstSend:
				c.preview = showAIPreview(w, parent, out, true, func(send bool) {
					c.preview = nil
					if send {
						c.send(req, true)
					} else {
						c.finish(ai.Done{ID: c.id, Cancelled: true})
					}
				})
			default:
				c.send(req, false)
			}
		})
	}()
	return c
}

// aiRequest fills in the focused shell's variables.
func (w *Window) aiRequest(req ai.Request) ai.Request {
	if req.Env == nil {
		if ps := w.model.ActivePane(); ps != nil {
			req.Env = ps.Env
		}
	}
	return req
}

// send asks, with the reply's events routed here.
func (c *aiCall) send(req ai.Request, shown bool) {
	if c.finished {
		return
	}
	ev := c.w.app.events
	c.offs = append(c.offs,
		ev.on(ai.EventChunk, func(data any) {
			if ch, ok := data.(ai.Chunk); ok && ch.ID == c.id && !c.finished && c.onChunk != nil {
				c.onChunk(ch.Text)
			}
		}),
		ev.on(ai.EventDone, func(data any) {
			if d, ok := data.(ai.Done); ok && d.ID == c.id {
				c.finish(d)
			}
		}))
	c.sent = true
	svc, id := c.w.app.be.AI, c.id
	go func() {
		err := svc.Ask(id, req, shown)
		onMainAsync(func() {
			switch {
			case err != nil:
				c.finish(ai.Done{ID: id, Error: err.Error()})
			case c.cancelled:
				// Stopped while Ask was on its way: it's running now.
				svc.Cancel(id)
			}
		})
	}()
}

// cancel stops the call: a running request ends with Done.Cancelled when
// the provider has stopped; one not sent yet ends now.
func (c *aiCall) cancel() {
	if c.finished || c.cancelled {
		return
	}
	c.cancelled = true
	if c.sent {
		svc, id := c.w.app.be.AI, c.id
		go svc.Cancel(id)
		return
	}
	if c.preview != nil {
		p := c.preview
		c.preview = nil
		p.answered = true
		p.win.Close()
	}
	c.finish(ai.Done{ID: c.id, Cancelled: true})
}

func (c *aiCall) finish(d ai.Done) {
	if c.finished {
		return
	}
	c.finished = true
	for _, off := range c.offs {
		off()
	}
	c.offs = nil
	if c.onDone != nil {
		c.onDone(d)
	}
}

// previewAI shows what req would send, without sending it (What's Sent).
func (w *Window) previewAI(parent *gtk.Window, req ai.Request) {
	if parent == nil {
		parent = &w.win.Window
	}
	req = w.aiRequest(req)
	svc := w.app.be.AI
	go func() {
		out, err := svc.Preview(req)
		onMainAsync(func() {
			if err != nil {
				w.toast("Couldn't prepare the request: " + err.Error())
				return
			}
			showAIPreview(w, parent, out, false, nil)
		})
	}()
}

// aiRoute is "kind · model → destination".
func aiRoute(p ai.ProviderInfo) string {
	parts := []string{p.Kind}
	if p.Model != "" {
		parts = append(parts, p.Model)
	}
	s := strings.Join(parts, " · ")
	if p.Destination != "" {
		s += " → " + p.Destination
	}
	return s
}

// aiWhere says where the text goes.
func aiWhere(p ai.ProviderInfo) string {
	switch {
	case p.Kind == "command":
		return "wtf runs " + p.Destination + " in an empty folder and gives it this text on stdin. The tool's own login and settings apply."
	case p.Local:
		return "The text stays on this machine: " + p.Destination + " is a local server."
	}
	return "This text goes to " + p.Destination + "."
}

// aiPreview is "what's being sent": the provider, where the text goes, and
// the exact text after Hide Secrets' redaction. One per request.
type aiPreview struct {
	win *adw.Window
	// Send or Cancel was chosen (closing is Cancel).
	answered bool
	// Send, when it asks (for the self-check).
	send *gtk.Button
}

// showAIPreview shows out over parent. With confirm, its buttons are
// Cancel and Send and answer gets the choice; without, it's Close only.
func showAIPreview(w *Window, parent *gtk.Window, out ai.Outgoing, confirm bool, answer func(send bool)) *aiPreview {
	p := &aiPreview{win: adw.NewWindow()}
	title := "Send to " + out.Provider.Name + "?"
	if !confirm {
		title = "What's Sent to " + out.Provider.Name
	}
	p.win.SetTransientFor(parent)
	p.win.SetModal(true)
	// Not resizable: it follows its content's height, up to the scroll's.
	p.win.SetResizable(false)
	p.win.SetDefaultSize(640, -1)
	p.win.SetTitle(title)
	head := adw.NewHeaderBar()
	head.SetTitleWidget(adw.NewWindowTitle(title, ""))
	body := gtk.NewBox(gtk.OrientationVertical, 10)
	body.AddCSSClass("wtf-sheet-body")
	body.SetSizeRequest(640, -1)

	label := func(text string, classes ...string) *gtk.Label {
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
	route := label(aiRoute(out.Provider), "dim-label", "wtf-ai-route", "wtf-tools-mono")
	route.SetSelectable(true)
	body.Append(route)
	where := aiWhere(out.Provider)
	if confirm {
		where += " This is shown before the first request to each provider; later ones go straight through."
	}
	body.Append(label(where, "wtf-ai-note"))
	switch out.Redacted {
	case 0:
		body.Append(label("No secrets were found to hide.", "dim-label", "wtf-sheet-sub"))
	case 1:
		body.Append(label("1 secret was replaced with [REDACTED].", "wtf-sheet-sub"))
	default:
		body.Append(label(fmt.Sprintf("%d secrets were replaced with [REDACTED].", out.Redacted), "wtf-sheet-sub"))
	}

	// The exact text, each part labelled.
	tv := gtk.NewTextView()
	tv.SetEditable(false)
	tv.SetCursorVisible(false)
	tv.SetMonospace(true)
	tv.SetWrapMode(gtk.WrapWordChar)
	tv.SetTopMargin(6)
	tv.SetBottomMargin(6)
	tv.SetLeftMargin(8)
	tv.SetRightMargin(8)
	tv.AddCSSClass("wtf-tools-code")
	buf := tv.Buffer()
	role := gtk.NewTextTag("role")
	role.SetObjectProperty("weight", int(pango.WeightBold))
	role.SetObjectProperty("foreground", "#1c71d8")
	if w.app.settings.style.Dark() {
		role.SetObjectProperty("foreground", "#78aeed")
	}
	buf.TagTable().Add(role)
	part := func(name, text string) {
		end := buf.EndIter()
		if buf.CharCount() > 0 {
			buf.Insert(end, "\n\n")
			end = buf.EndIter()
		}
		from := buf.CharCount()
		buf.Insert(end, name+"\n")
		buf.ApplyTag(role, buf.IterAtOffset(from), buf.EndIter())
		buf.Insert(buf.EndIter(), text)
	}
	if out.System != "" {
		part("System", out.System)
	}
	for _, m := range out.Messages {
		name := "User"
		if m.Role == "assistant" {
			name = "Assistant"
		}
		part(name, m.Content)
	}
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetPropagateNaturalHeight(true)
	h := 360
	if wh := w.win.Height(); wh > 0 && wh/2 < h {
		h = max(wh/2, 160)
	}
	scroll.SetMaxContentHeight(h)
	scroll.SetChild(tv)
	frame := gtk.NewFrame("")
	frame.SetChild(scroll)
	body.Append(frame)

	row := gtk.NewBox(gtk.OrientationHorizontal, 8)
	row.SetMarginTop(6)
	spacer := gtk.NewBox(gtk.OrientationHorizontal, 0)
	spacer.SetHExpand(true)
	row.Append(spacer)
	var def *gtk.Button
	if confirm {
		cancel := gtk.NewButtonWithLabel("Cancel")
		cancel.ConnectClicked(func() { p.win.Close() })
		send := gtk.NewButtonWithLabel("Send")
		send.ConnectClicked(func() {
			p.answered = true
			p.win.Close()
			answer(true)
		})
		row.Append(cancel)
		row.Append(send)
		def, p.send = send, send
	} else {
		done := gtk.NewButtonWithLabel("Close")
		done.ConnectClicked(func() { p.win.Close() })
		row.Append(done)
		def = done
	}
	body.Append(row)

	view := adw.NewToolbarView()
	view.AddTopBar(head)
	view.SetContent(body)
	p.win.SetContent(view)
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(func(keyval, _ uint, _ gdk.ModifierType) bool {
		if keyval == gdk.KEY_Escape {
			p.win.Close()
			return true
		}
		return false
	})
	p.win.AddController(keys)
	p.win.ConnectCloseRequest(func() bool {
		if confirm && !p.answered {
			p.answered = true
			answer(false)
		}
		return false
	})
	def.AddCSSClass("suggested-action")
	p.win.SetDefaultWidget(def)
	p.win.Present()
	def.GrabFocus()
	w.app.debugLog("ai preview: %s (%d redacted)", out.Provider.Name, out.Redacted)
	return p
}

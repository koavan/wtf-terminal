//go:build linux

package gtkui

import (
	"fmt"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/dayanraj/wtf/internal/uicore"
	"github.com/dayanraj/wtf/internal/usage"
)

func init() {
	extraCSS = append(extraCSS, `
.wtf-usage { padding: 0 4px; min-height: 20px; }
.wtf-usage label { font-size: 0.88em; font-feature-settings: "tnum"; }
.wtf-usage image, .wtf-usage label { opacity: 0.7; }
.wtf-usage-num { font-feature-settings: "tnum"; }
.wtf-usage-total { background: alpha(currentColor, 0.06); border-radius: 6px; }
.wtf-usage-line { padding: 3px 6px; }
.wtf-usage-head { font-weight: 600; }
.wtf-usage-note { font-size: 0.85em; }
`)
}

// usageStore is wtf's CPU and memory, sampled every 2s only while a
// readout or the dialog is on screen and a wtf window is in front. One per
// app. Like usage.svelte.ts and the macOS app's UsageStore.
type usageStore struct {
	a        *App
	current  *usage.Usage
	watchers map[any]bool
	poller   *poller
	busy     bool
	// Who redraws after a sample.
	observers map[any]func()
	// The Resource Usage dialog, while open.
	dialog *adw.MessageDialog
}

var usageStores = map[*App]*usageStore{}

func usageStoreOf(a *App) *usageStore {
	s := usageStores[a]
	if s == nil {
		s = &usageStore{a: a, watchers: map[any]bool{}, observers: map[any]func(){}}
		usageStores[a] = s
	}
	return s
}

// watch samples while anyone watches.
func (s *usageStore) watch(who any) {
	s.watchers[who] = true
	if s.poller == nil {
		s.poller = newPoller(s.a, nil, 2*time.Second, s.sample)
	}
}

func (s *usageStore) unwatch(who any) {
	delete(s.watchers, who)
	if len(s.watchers) == 0 && s.poller != nil {
		s.poller.stop()
		s.poller = nil
	}
}

func (s *usageStore) sample() {
	if s.busy {
		return
	}
	s.busy = true
	svc := s.a.be.Usage
	go func() {
		u := svc.Sample()
		onMainAsync(func() {
			s.current, s.busy = &u, false
			for _, f := range s.observers {
				f()
			}
		})
	}()
}

// usageReadout is the address bar's "CPU · memory" readout, shown while
// show-usage is on; click for the breakdown. It samples only while shown.
type usageReadout struct {
	w         *Window
	root      *gtk.Button
	label     *gtk.Label
	st        *usageStore
	mapped    bool
	watching  bool
	unobserve func()
}

func newUsageReadout(w *Window) *usageReadout {
	r := &usageReadout{w: w, st: usageStoreOf(w.app)}
	r.label = gtk.NewLabel("…")
	box := gtk.NewBox(gtk.OrientationHorizontal, 4)
	box.Append(gtk.NewImageFromIconName("power-profile-performance-symbolic"))
	box.Append(r.label)
	r.root = gtk.NewButton()
	r.root.SetChild(box)
	r.root.AddCSSClass("flat")
	r.root.AddCSSClass("wtf-usage")
	r.root.SetFocusOnClick(false)
	r.root.SetCanFocus(false)
	r.root.SetVAlign(gtk.AlignCenter)
	r.root.SetTooltipText("wtf's CPU (% of one core) and memory: the app and its windows, not the programs you run in it. Click for the breakdown.")
	r.root.ConnectClicked(w.showUsage)
	r.root.ConnectMap(func() {
		r.mapped = true
		r.update()
	})
	r.root.ConnectUnmap(func() {
		r.mapped = false
		r.update()
	})
	r.st.observers[r] = r.show
	r.unobserve = w.app.settings.observe(r.update)
	r.update()
	r.show()
	return r
}

func (r *usageReadout) widget() gtk.Widgetter { return r.root }

// update follows the setting: shown, and sampling while on screen.
func (r *usageReadout) update() {
	on := r.w.app.settings.config().ShowUsage
	r.root.SetVisible(on)
	if want := on && r.mapped; want != r.watching {
		r.watching = want
		if want {
			r.st.watch(r)
		} else {
			r.st.unwatch(r)
		}
	}
}

func (r *usageReadout) show() {
	if u := r.st.current; u != nil {
		cpu, mem := uicore.UsageTotal(*u)
		r.label.SetText(uicore.UsageCPU(cpu) + " · " + uicore.UsageBytes(mem))
	}
}

// stop stops sampling for it and following the setting.
func (r *usageReadout) stop() {
	r.unobserve()
	delete(r.st.observers, r)
	if r.watching {
		r.watching = false
		r.st.unwatch(r)
	}
}

// showUsage is the Resource Usage dialog: wtf in all, then the app and
// your programs, process by process, live while open. One at a time. Like
// UsageDialog.svelte and the macOS app's UsageDialog.
func (w *Window) showUsage() {
	st := usageStoreOf(w.app)
	if st.dialog != nil {
		st.dialog.Present()
		return
	}
	d := adw.NewMessageDialog(&w.win.Window, "Resource Usage", "")
	st.dialog = d
	body := gtk.NewBox(gtk.OrientationVertical, 2)
	body.SetSizeRequest(512, -1)
	show := gtk.NewCheckButtonWithLabel("Show in the address bar")
	show.SetActive(w.app.settings.config().ShowUsage)
	show.ConnectToggled(func() {
		on, svc := show.Active(), w.app.be.Config
		go func() {
			if err := svc.SetShowUsage(on); err != nil {
				onMainAsync(func() { w.toast("Couldn't save the setting: " + err.Error()) })
			}
		}()
	})
	box := gtk.NewBox(gtk.OrientationVertical, 10)
	box.Append(body)
	box.Append(show)
	d.SetExtraChild(box)
	d.AddResponse("close", "Done")
	d.SetCloseResponse("close")
	d.SetDefaultResponse("close")
	key := new(int)
	d.ConnectResponse(func(string) {
		st.unwatch(key)
		delete(st.observers, key)
		st.dialog = nil
	})
	update := func() { usageFill(d, body, st.current) }
	st.observers[key] = update
	st.watch(key)
	update()
	d.Present()
}

// usageFill redraws the dialog from a sample (nil before the first).
func usageFill(d *adw.MessageDialog, body *gtk.Box, u *usage.Usage) {
	cores, kind := 0, ""
	if u != nil {
		cores, kind = u.Cores, u.MemoryKind
	}
	n := "?"
	if cores > 0 {
		n = fmt.Sprint(cores)
	}
	sub := fmt.Sprintf("CPU is a percentage of one core (%s cores = %d%%), over the last 2 seconds. ", n, cores*100)
	if kind == "rss" {
		sub += "Memory is resident memory (RSS); pages shared between processes count in each."
	} else {
		sub += "Memory is the footprint the system reports."
	}
	d.SetBody(sub)
	for c := body.FirstChild(); c != nil; c = body.FirstChild() {
		body.Remove(c)
	}
	if u == nil {
		body.Append(formNote("Measuring…", "dim-label"))
		return
	}
	cpu, mem := uicore.UsageTotal(*u)
	total := usageLine("wtf in all", uicore.UsageCPU(cpu), uicore.UsageBytes(mem), "wtf-usage-head", 6)
	total.AddCSSClass("wtf-usage-total")
	body.Append(total)
	if u.Note != "" {
		body.Append(formNote(u.Note, "dim-label"))
	}
	measured := u.Interval > 0
	type group struct {
		title, note string
		g           usage.Group
	}
	groups := []group{{"wtf", "The app — its windows, terminals and backend — and what it runs itself (tunnels, git and kubectl checks)", u.App}}
	// A group only shows if something landed there.
	if len(u.Window.Procs) > 0 {
		groups = append(groups, group{"Windows", "Helper processes that draw wtf’s windows", u.Window})
	}
	groups = append(groups, group{"Your programs", "Shells and what runs in them — not counted in wtf’s total", u.Programs})
	for _, g := range groups {
		gcpu := g.g.CPU
		if !measured {
			gcpu = -1
		}
		head := usageLine(g.title, uicore.UsageCPU(gcpu), uicore.UsageBytes(g.g.Memory), "wtf-usage-head", 6)
		head.SetMarginTop(8)
		head.SetTooltipText(g.note)
		body.Append(head)
		note := sideLabel(g.note, "dim-label")
		note.AddCSSClass("wtf-usage-note")
		note.SetMarginStart(6)
		body.Append(note)
		if len(g.g.Procs) == 0 {
			none := sideLabel("None", "dim-label")
			none.SetMarginStart(18)
			body.Append(none)
		}
		for _, p := range g.g.Procs {
			l := usageLine(fmt.Sprintf("%s  %d", p.Name, p.PID), uicore.UsageCPU(p.CPU), uicore.UsageBytes(p.Memory), "monospace", 18)
			l.SetTooltipText(fmt.Sprintf("%s (pid %d)", p.Name, p.PID))
			body.Append(l)
		}
	}
}

// usageLine is a row of the grid: name | CPU | memory, numbers right-aligned.
func usageLine(name, cpu, mem, class string, inset int) *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontal, 0)
	row.AddCSSClass("wtf-usage-line")
	row.SetMarginStart(inset - 6)
	n := gtk.NewLabel(name)
	n.SetXAlign(0)
	n.SetHExpand(true)
	n.SetEllipsize(pango.EllipsizeEnd)
	row.Append(n)
	for _, c := range []struct {
		text  string
		width int
	}{{cpu, 64}, {mem, 76}} {
		l := gtk.NewLabel(c.text)
		l.SetXAlign(1)
		l.SetSizeRequest(c.width, -1)
		l.AddCSSClass("wtf-usage-num")
		row.Append(l)
	}
	if class != "" {
		row.AddCSSClass(class)
	}
	return row
}

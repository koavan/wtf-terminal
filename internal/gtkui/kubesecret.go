//go:build linux

package gtkui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/dayanraj/wtf/internal/kube"
	"github.com/dayanraj/wtf/internal/sshhosts"
	"github.com/dayanraj/wtf/internal/uicore"
)

// The Kubernetes tab's dialogs: a secret's values, Scale, a context's
// name and namespace, Add EKS Cluster, and a kubectl port-forward. Like
// the macOS app's SecretViewer, FormSheets and TunnelEditor.

// Secrets

// secretViewer shows a Kubernetes secret's keys with their values hidden
// until revealed. They hide again when wtf loses focus, and are dropped
// when it closes. One at a time.
type secretViewer struct {
	w        *Window
	name     string
	target   uicore.KubeTarget
	win      *adw.Window
	list     *gtk.Box
	all      *gtk.Button
	entries  []kube.SecretEntry
	revealed map[string]bool
	loading  bool
	err      string
	ownerSig coreglib.SignalHandle
}

var shownSecret *secretViewer

// showSecret opens a secret's viewer, closing the one open.
func (w *Window) showSecret(name string, t uicore.KubeTarget) {
	if shownSecret != nil {
		shownSecret.win.Close()
	}
	v := &secretViewer{w: w, name: name, target: t, revealed: map[string]bool{}, loading: true}
	shownSecret = v
	v.win = adw.NewWindow()
	v.win.SetTransientFor(&w.win.Window)
	v.win.SetModal(true)
	v.win.SetDefaultSize(560, -1)
	v.win.SetTitle("Secret " + name)

	head := adw.NewHeaderBar()
	head.SetTitleWidget(adw.NewWindowTitle("Secret "+name, ""))
	sub := gtk.NewLabel("Context " + t.Context + ", namespace " + t.Namespace + ". Values hide again when wtf loses focus.")
	sub.SetXAlign(0)
	sub.SetWrap(true)
	sub.AddCSSClass("dim-label")
	v.list = gtk.NewBox(gtk.OrientationVertical, 8)
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetPropagateNaturalHeight(true)
	scroll.SetMaxContentHeight(480)
	scroll.SetVExpand(true)
	scroll.SetChild(v.list)
	v.all = gtk.NewButtonWithLabel("Reveal All")
	v.all.ConnectClicked(func() {
		if len(v.revealed) == len(v.entries) {
			v.revealed = map[string]bool{}
		} else {
			for _, e := range v.entries {
				v.revealed[e.Key] = true
			}
		}
		v.render()
	})
	done := gtk.NewButtonWithLabel("Done")
	done.AddCSSClass("suggested-action")
	done.SetHExpand(true)
	done.SetHAlign(gtk.AlignEnd)
	done.ConnectClicked(func() { v.win.Close() })
	buttons := gtk.NewBox(gtk.OrientationHorizontal, 8)
	buttons.Append(v.all)
	buttons.Append(done)
	body := gtk.NewBox(gtk.OrientationVertical, 12)
	body.SetMarginStart(16)
	body.SetMarginEnd(16)
	body.SetMarginBottom(16)
	body.Append(sub)
	body.Append(scroll)
	body.Append(buttons)
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
	// Leaving wtf hides the values; moving between this and its window doesn't.
	v.win.NotifyProperty("is-active", v.focusChanged)
	v.ownerSig = w.win.NotifyProperty("is-active", v.focusChanged)
	v.win.ConnectCloseRequest(func() bool {
		w.win.HandlerDisconnect(v.ownerSig)
		v.entries, v.revealed = nil, nil
		if shownSecret == v {
			shownSecret = nil
		}
		return false
	})
	v.render()
	v.win.Present()

	svc := w.app.be.Kube
	go func() {
		entries, err := svc.SecretValues(kube.Request{Env: t.Env, Context: t.Context, Namespace: t.Namespace}, name)
		onMainAsync(func() {
			if v.revealed == nil {
				return
			}
			if err != nil {
				v.err = err.Error()
			}
			v.entries, v.loading = entries, false
			v.render()
		})
	}()
}

// focusChanged hides the values once neither the viewer nor its window is
// in front (checked after the hand-over between them).
func (v *secretViewer) focusChanged() {
	onMainAsync(func() {
		if len(v.revealed) == 0 || v.win.IsActive() || v.w.win.IsActive() {
			return
		}
		v.revealed = map[string]bool{}
		v.render()
	})
}

// secretSize is "512 B" or "1.5 KB".
func secretSize(n int) string {
	if n < 1024 {
		return strconv.Itoa(n) + " B"
	}
	return fmt.Sprintf("%.1f KB", float64(n)/1024)
}

func (v *secretViewer) render() {
	for c := v.list.FirstChild(); c != nil; c = v.list.FirstChild() {
		v.list.Remove(c)
	}
	note := func(text, class string) {
		l := gtk.NewLabel(text)
		l.SetXAlign(0)
		l.SetWrap(true)
		l.SetSelectable(class == "error")
		l.AddCSSClass(class)
		v.list.Append(l)
	}
	switch {
	case v.err != "":
		note(v.err, "error")
	case v.loading:
		note("Reading the secret…", "dim-label")
	case len(v.entries) == 0:
		note("This secret has no data.", "dim-label")
	}
	for _, e := range v.entries {
		key := gtk.NewLabel(e.Key)
		key.SetXAlign(0)
		key.SetEllipsize(pango.EllipsizeEnd)
		key.AddCSSClass("wtf-secret-key")
		meta := secretSize(e.Size)
		if e.Binary {
			meta = "binary · " + meta
		}
		size := gtk.NewLabel(meta)
		size.AddCSSClass("dim-label")
		size.SetHExpand(true)
		size.SetXAlign(0)
		shown := v.revealed[e.Key]
		eye, eyeTip := "view-reveal-symbolic", "Reveal"
		if shown {
			eye, eyeTip = "view-conceal-symbolic", "Hide"
		}
		toggle := gtk.NewButtonFromIconName(eye)
		toggle.AddCSSClass("flat")
		toggle.SetTooltipText(eyeTip)
		toggle.ConnectClicked(func() {
			if shown {
				delete(v.revealed, e.Key)
			} else {
				v.revealed[e.Key] = true
			}
			v.render()
		})
		what, copyTip := e.Key, "Copy"
		if e.Binary {
			what, copyTip = e.Key+" (base64)", "Copy (base64)"
		}
		copyB := gtk.NewButtonFromIconName("edit-copy-symbolic")
		copyB.AddCSSClass("flat")
		copyB.SetTooltipText(copyTip)
		copyB.ConnectClicked(func() { v.w.copySecret(e.Value, what) })
		head := gtk.NewBox(gtk.OrientationHorizontal, 8)
		head.Append(key)
		head.Append(size)
		head.Append(toggle)
		head.Append(copyB)
		v.list.Append(head)
		if !shown {
			dots := gtk.NewLabel("••••••••")
			dots.SetXAlign(0)
			dots.AddCSSClass("dim-label")
			v.list.Append(dots)
			continue
		}
		text := gtk.NewTextView()
		text.SetEditable(false)
		text.SetCursorVisible(false)
		text.SetWrapMode(gtk.WrapWordChar)
		text.SetMonospace(true)
		text.SetTopMargin(8)
		text.SetBottomMargin(8)
		text.SetLeftMargin(8)
		text.SetRightMargin(8)
		text.AddCSSClass("wtf-secret-value")
		text.Buffer().SetText(e.Value)
		box := gtk.NewScrolledWindow()
		box.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
		box.SetPropagateNaturalHeight(true)
		box.SetMaxContentHeight(220)
		box.SetChild(text)
		v.list.Append(box)
	}
	v.all.SetVisible(len(v.entries) > 0)
	if len(v.entries) > 0 && len(v.revealed) == len(v.entries) {
		v.all.SetLabel("Hide All")
	} else {
		v.all.SetLabel("Reveal All")
	}
}

// Forms

// kubeForm is a dialog with a form: labelled fields, notes, a line for
// what's wrong and the command it will run.
type kubeForm struct {
	d       *adw.MessageDialog
	grid    *gtk.Grid
	rows    int
	errText *gtk.Label
	preview *gtk.Label
}

func newKubeForm(w *Window, title, body string) *kubeForm {
	f := &kubeForm{d: adw.NewMessageDialog(&w.win.Window, title, body), grid: gtk.NewGrid()}
	f.grid.SetRowSpacing(8)
	f.grid.SetColumnSpacing(12)
	f.grid.SetSizeRequest(440, -1)
	f.errText = formNote("", "wtf-form-error")
	f.preview = formNote("", "monospace")
	f.preview.SetSelectable(true)
	f.d.SetExtraChild(f.grid)
	f.d.AddResponse("cancel", "Cancel")
	f.d.SetCloseResponse("cancel")
	return f
}

func formNote(text, class string) *gtk.Label {
	l := gtk.NewLabel(text)
	l.SetXAlign(0)
	l.SetWrap(true)
	l.SetWrapMode(pango.WrapWordChar)
	if class != "" {
		l.AddCSSClass(class)
	}
	return l
}

// field adds a labelled row; the label is returned to rename it.
func (f *kubeForm) field(label string, w gtk.Widgetter) *gtk.Label {
	l := gtk.NewLabel(label)
	l.SetXAlign(0)
	f.grid.Attach(l, 0, f.rows, 1, 1)
	f.grid.Attach(w, 1, f.rows, 1, 1)
	f.rows++
	return l
}

// add adds a full-width row.
func (f *kubeForm) add(w gtk.Widgetter) {
	f.grid.Attach(w, 0, f.rows, 2, 1)
	f.rows++
}

func (f *kubeForm) entry(text, placeholder string) *gtk.Entry {
	e := gtk.NewEntry()
	e.SetText(text)
	e.SetPlaceholderText(placeholder)
	e.SetHExpand(true)
	e.SetActivatesDefault(true)
	return e
}

func (f *kubeForm) setError(msg string) {
	f.errText.SetText(msg)
	f.errText.SetVisible(msg != "")
}

// button adds the main button, the default.
func (f *kubeForm) button(id, label string) {
	f.d.AddResponse(id, label)
	f.d.SetResponseAppearance(id, adw.ResponseSuggested)
	f.d.SetDefaultResponse(id)
}

// present shows the form with its error and preview lines last.
func (f *kubeForm) present(focus gtk.Widgetter) {
	f.add(f.errText)
	f.add(f.preview)
	f.errText.SetVisible(false)
	f.d.Present()
	if focus != nil {
		gtk.BaseWidget(focus).GrabFocus()
	}
}

var replicasRe = regexp.MustCompile(`^\d+$`)

// scale asks how many replicas, shows the command, and runs it in a new tab.
func (r *kubeResources) scale(kind, name string, current int) {
	ct, ok := r.commandTarget()
	if !ok {
		return
	}
	f := newKubeForm(r.w, "Scale "+name, fmt.Sprintf("%s It has %d %s now.", r.whereText(), current, plural(current, "replica")))
	field := f.entry(strconv.Itoa(current), "")
	f.field("Replicas", field)
	command := func() string {
		v := strings.TrimSpace(field.Text())
		n, err := strconv.Atoi(v)
		switch {
		case !replicasRe.MatchString(v) || err != nil || n > 1000:
			f.setError("Enter a number of replicas, 0 or more")
		case n == current:
			f.setError(fmt.Sprintf("It already has %d", current))
		default:
			f.setError("")
			return ct.Scale(kind, name, n)
		}
		return ""
	}
	f.button("scale", "Scale")
	field.ConnectChanged(func() {
		c := command()
		f.preview.SetText("")
		if c != "" {
			f.preview.SetText(c + "\nRuns in a new tab.")
		}
		f.d.SetResponseEnabled("scale", c != "")
	})
	f.d.ConnectResponse(func(id string) {
		if c := command(); id == "scale" && c != "" {
			r.newTab(c)
		}
	})
	f.present(field)
	f.d.SetResponseEnabled("scale", false)
}

// prompt is Rename… or Set Default Namespace…: it shows the kubectl
// command, then wtf runs it.
func (c *kubeContexts) prompt(k kube.KubeContext, rename bool) {
	title := "Default Namespace of " + k.Name
	ns := k.Namespace
	if ns == "" {
		ns = "default"
	}
	body := "Commands in this context use it unless they name another (-n). Now: " + ns + "."
	now, label, button := k.Namespace, "Namespace", "Set Namespace"
	if rename {
		title = "Rename " + k.Name
		body = "In " + uicore.PrettyPath(k.File, c.w.app.home) + ". Shells using it by name keep the old name until they switch again."
		now, label, button = k.Name, "New name", "Rename"
	}
	f := newKubeForm(c.w, title, body)
	placeholder := "default"
	if rename {
		placeholder = ""
	}
	field := f.entry(now, placeholder)
	f.field(label, field)
	value := func() string {
		v := strings.TrimSpace(field.Text())
		switch {
		case v == "" && rename:
			f.setError("Enter a name")
		case v == "":
			f.setError("Enter a namespace")
		case v == now && rename:
			f.setError("That’s its name now")
		case v == now:
			f.setError("That’s its namespace now")
		default:
			f.setError("")
			return v
		}
		return ""
	}
	f.button("ok", button)
	field.ConnectChanged(func() {
		v := value()
		f.preview.SetText("")
		if v != "" {
			cmd := "kubectl config set-context " + uicore.ShellQuote(k.Name) + " --namespace=" + uicore.ShellQuote(v)
			if rename {
				cmd = "kubectl config rename-context " + uicore.ShellQuote(k.Name) + " " + uicore.ShellQuote(v)
			}
			f.preview.SetText(cmd + "\nwtf runs this, after keeping a copy of the kubeconfig.")
		}
		f.d.SetResponseEnabled("ok", v != "")
	})
	f.d.ConnectResponse(func(id string) {
		if v := value(); id == "ok" && v != "" {
			kind := "namespace"
			if rename {
				kind = "rename"
			}
			c.apply(kube.Op{Kind: kind, Name: k.Name, Value: v, File: k.File}, true)
		}
	})
	f.present(field)
	f.d.SetResponseEnabled("ok", false)
}

// picker is an entry with a button that lists choices; choices calls back
// with them, now or once read.
func picker(e *gtk.Entry, tip string, choices func(show func([]string)), picked func()) *gtk.Box {
	b := gtk.NewButtonFromIconName("pan-down-symbolic")
	b.SetTooltipText(tip)
	b.SetFocusOnClick(false)
	box := gtk.NewBox(gtk.OrientationHorizontal, 0)
	box.AddCSSClass("linked")
	box.SetHExpand(true)
	box.Append(e)
	box.Append(b)
	menu := newMenuPopover(box)
	b.ConnectClicked(func() {
		choices(func(list []string) {
			var items []*menuItem
			for _, c := range list {
				items = append(items, item(c, func() {
					e.SetText(c)
					picked()
				}))
			}
			if len(items) > 0 {
				menu.show(float64(box.Width()-12), float64(box.Height()), items, nil)
			}
		})
	})
	return box
}

// addEKS is Add EKS Cluster: aws eks update-kubeconfig in a new tab, after
// a backup.
func (c *kubeContexts) addEKS() {
	w, env, svc := c.w, c.env(), c.w.app.be.Kube
	f := newKubeForm(w, "Add EKS Cluster", "Adds a context for an EKS cluster to your kubeconfig, using one of your AWS profiles")
	profiles := gtk.NewStringList([]string{"default credentials"})
	profile := gtk.NewDropDown(profiles, nil)
	region := f.entry("us-east-1", "")
	cluster := f.entry("", "cluster name")
	found := formNote("", "dim-label")
	alias := f.entry("", "the cluster's ARN, if empty")
	file := ""
	if _, ok := env["KUBECONFIG"]; ok && len(c.configs.Files) > 0 {
		file = c.configs.Files[0]
	}
	regionsByProfile := map[string]string{}
	var clusters []string
	chosenProfile := func() string {
		if i := profile.Selected(); i > 0 && i != gtk.INVALID_LIST_POSITION {
			return profiles.String(i)
		}
		return ""
	}
	command := func() string {
		cl, rg := strings.TrimSpace(cluster.Text()), strings.TrimSpace(region.Text())
		if cl == "" || rg == "" {
			return ""
		}
		return uicore.EKSAdd(cl, rg, chosenProfile(), strings.TrimSpace(alias.Text()), file)
	}
	refresh := func() {
		cmd := command()
		f.preview.SetText("")
		if cmd != "" {
			f.preview.SetText(cmd + "\nRuns in a new tab. wtf keeps a copy of your kubeconfig first (kube-backups). It logs in with aws eks get-token.")
		}
		f.d.SetResponseEnabled("add", cmd != "")
	}
	for _, e := range []*gtk.Entry{region, cluster, alias} {
		e.ConnectChanged(refresh)
	}
	profile.NotifyProperty("selected", func() {
		if r := regionsByProfile[chosenProfile()]; r != "" {
			region.SetText(r)
		}
		refresh()
	})
	list := gtk.NewButtonWithLabel("List Clusters")
	list.SetTooltipText("Asks AWS (eks list-clusters)")
	list.ConnectClicked(func() {
		r, p := strings.TrimSpace(region.Text()), chosenProfile()
		found.RemoveCSSClass("error")
		if r == "" {
			found.SetText("Choose a region first")
			return
		}
		found.SetText("Asking…")
		list.SetLabel("Asking…")
		list.SetSensitive(false)
		go func() {
			names, err := svc.EKSClusters(env, p, r)
			onMainAsync(func() {
				list.SetLabel("List Clusters")
				list.SetSensitive(true)
				if err != nil {
					found.SetText(err.Error())
					found.AddCSSClass("error")
					return
				}
				clusters = names
				if len(names) == 1 && cluster.Text() == "" {
					cluster.SetText(names[0])
				}
				if len(names) == 0 {
					found.SetText("No clusters in " + r + " for this profile")
				} else {
					found.SetText(fmt.Sprintf("%d in %s: %s", len(names), r, strings.Join(names, ", ")))
				}
				refresh()
			})
		}()
	})
	clusterRow := gtk.NewBox(gtk.OrientationHorizontal, 6)
	clusterRow.Append(picker(cluster, "Clusters listed", func(show func([]string)) { show(clusters) }, refresh))
	clusterRow.Append(list)

	f.field("AWS profile", profile)
	f.field("Region", picker(region, "Regions", func(show func([]string)) { show(uicore.EKSRegions) }, refresh))
	f.field("Cluster", clusterRow)
	f.add(found)
	f.field("Context name", alias)
	f.button("add", "Add in Terminal")
	f.d.ConnectResponse(func(id string) {
		cmd := command()
		if id != "add" || cmd == "" {
			return
		}
		go func() {
			svc.BackupConfigs(env)
			onMainAsync(func() { w.model.Open(uicore.OpenOptions{Command: cmd}) })
		}()
	})
	f.present(cluster)
	refresh()
	// Profiles from the local AWS files (no network).
	aws := w.app.be.AWSProfiles
	go func() {
		l := aws.List()
		onMainAsync(func() {
			for _, p := range l.Profiles {
				regionsByProfile[p.Name] = p.Region
				profiles.Append(p.Name)
			}
		})
	}()
}

// kubePortForward is a new kubectl port-forward, run in the background
// while it's on and saved with the tunnels, or run once in a new tab.
func (w *Window) kubePortForward(d uicore.TunnelDraft) {
	f := newKubeForm(w, "New Port Forward", "A kubectl port-forward, run in the background while it’s on")
	explain := formNote("Reach a pod, service or deployment in a cluster on a port of this computer. If its pod restarts, the forward reconnects.", "dim-label")
	context := f.entry(d.Context, "kubectl context")
	namespace := f.entry(d.Namespace, "default")
	resource := f.entry(d.Resource, "svc/web, deploy/api or pod/name")
	target := f.entry(d.DestPort, "80")
	target.SetWidthChars(6)
	target.SetHExpand(false)
	listen := f.entry(d.ListenPort, "8080")
	name := f.entry(d.Name, "optional, e.g. staging db")
	auto := gtk.NewCheckButtonWithLabel("Start when wtf opens")
	auto.SetActive(d.AutoStart)
	hint := formNote("Runs with the focused shell’s KUBECONFIG, AWS profile and PATH, so it reaches the same cluster. Listens on 127.0.0.1 only.", "dim-label")
	read := func() {
		d.Context, d.Namespace, d.Resource = context.Text(), namespace.Text(), resource.Text()
		d.DestPort, d.ListenPort, d.Name, d.AutoStart = target.Text(), listen.Text(), name.Text(), auto.Active()
	}
	dots := func(s string) string {
		if s == "" {
			return "…"
		}
		return s
	}
	refresh := func() {
		read()
		prefix := ""
		if v := d.Env["KUBECONFIG"]; v != "" {
			prefix += "KUBECONFIG=" + uicore.ShellQuote(v) + " "
		}
		if v := d.Env["AWS_PROFILE"]; v != "" {
			prefix += "AWS_PROFILE=" + uicore.ShellQuote(v) + " "
		}
		f.preview.SetText(prefix + "kubectl --context " + uicore.ShellQuote(dots(d.Context)) + " --namespace " + uicore.ShellQuote(dots(d.Namespace)) +
			" port-forward " + dots(d.Resource) + " " + dots(d.Spec()))
	}
	for _, e := range []*gtk.Entry{context, namespace, resource, target, listen, name} {
		e.ConnectChanged(refresh)
	}
	row := func(a, b gtk.Widgetter) *gtk.Box {
		box := gtk.NewBox(gtk.OrientationHorizontal, 6)
		box.Append(a)
		box.Append(b)
		return box
	}
	f.add(explain)
	f.field("Context and namespace", row(context, namespace))
	f.field("Target and its port", row(resource, target))
	f.field("Port here", listen)
	f.field("Name", name)
	f.add(auto)
	f.add(hint)
	f.d.AddResponse("run", "Run in Terminal")
	f.d.AddResponse("save", "Save")
	f.button("start", "Save & Start")
	f.d.ConnectResponse(func(id string) {
		read()
		switch id {
		case "run":
			w.model.Open(uicore.OpenOptions{Command: f.preview.Text()})
		case "save", "start":
			t := sshhosts.Tunnel{Name: strings.TrimSpace(d.Name), Kind: "kubectl", Spec: d.Spec(), AutoStart: d.AutoStart,
				Context: strings.TrimSpace(d.Context), Namespace: strings.TrimSpace(d.Namespace), Resource: strings.TrimSpace(d.Resource), Env: d.Env}
			svc, start := w.app.be.SSHHosts, id == "start"
			go func() {
				list, err := svc.SaveTunnel(t)
				if err == nil && start && len(list) > 0 {
					_, err = svc.StartTunnel(list[len(list)-1].ID)
				}
				onMainAsync(func() {
					switch {
					case err != nil:
						w.toast(err.Error())
					case !start:
						w.toast("Added tunnel")
					}
				})
			}()
		}
	})
	f.present(context)
	refresh()
}

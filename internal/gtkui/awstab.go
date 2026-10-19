//go:build linux

package gtkui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/awsprofiles"
	"github.com/dayanraj/wtf/internal/uicore"
)

func init() {
	registerSidebarTab(sidebarTabInfo{tab: uicore.SidebarAWS, label: "AWS Profiles", icon: "weather-overcast-symbolic", cmd: "sidebar.aws",
		make: func(w *Window) sidePaneTab { return newAWSTab(w) }})
}

// awsCheck is what Check Identity and Check Access Keys found for a
// profile.
type awsCheck struct {
	busy        bool
	identity    *awsprofiles.Identity
	identityErr string
	keys        []awsprofiles.AccessKey
	keysChecked bool
	keysErr     string
}

// awsShared is the AWS profiles, shared by every window: read when the
// tab first shows and after each change, never polled. Checks run only
// when asked and are kept per profile while the app runs. Main loop only.
// Like the macOS app's AwsStore.
type awsShared struct {
	list            awsprofiles.List
	loaded, loading bool
	checks          map[string]*awsCheck
	observers       map[int]func()
	next            int
}

var awsStore = &awsShared{checks: map[string]*awsCheck{}, observers: map[int]func(){}}

func (s *awsShared) observe(f func()) (off func()) {
	s.next++
	id := s.next
	s.observers[id] = f
	return func() { delete(s.observers, id) }
}

func (s *awsShared) changed() {
	for _, f := range s.observers {
		f()
	}
}

func (s *awsShared) check(name string) *awsCheck {
	c := s.checks[name]
	if c == nil {
		c = &awsCheck{}
		s.checks[name] = c
	}
	return c
}

func (s *awsShared) load(svc *awsprofiles.Service) {
	s.loading = true
	s.changed()
	go func() {
		l := svc.List()
		onMainAsync(func() {
			s.list, s.loading, s.loaded = l, false, true
			s.changed()
		})
	}()
}

func (s *awsShared) apply(l awsprofiles.List) {
	s.list, s.loaded = l, true
	s.changed()
}

func (s *awsShared) verify(svc *awsprofiles.Service, name string) {
	c := s.check(name)
	c.busy, c.identityErr = true, ""
	s.changed()
	go func() {
		id, err := svc.Verify(name)
		onMainAsync(func() {
			c.identity, c.identityErr = nil, ""
			if err != nil {
				c.identityErr = err.Error()
			} else {
				c.identity = &id
			}
			c.busy = false
			s.changed()
		})
	}()
}

func (s *awsShared) checkKeys(svc *awsprofiles.Service, name string) {
	c := s.check(name)
	c.busy = true
	s.changed()
	go func() {
		keys, err := svc.AccessKeys(name)
		onMainAsync(func() {
			c.keys, c.keysChecked, c.keysErr = nil, false, ""
			if err != nil {
				c.keysErr = err.Error()
			} else {
				c.keys, c.keysChecked = keys, true
			}
			c.busy = false
			s.changed()
		})
	}()
}

// awsTab is the AWS Profiles tab: the profiles in ~/.aws/credentials and
// ~/.aws/config. Use one in the focused shell, check it with AWS, add,
// edit, rotate, copy to default, delete — every write planned and
// confirmed first. Like AwsView.svelte and the macOS app's AwsView.
type awsTab struct {
	w      *Window
	root   *gtk.Box
	reload *gtk.Button
	filter *gtk.SearchEntry
	note   *gtk.Box
	list   *sideList
	off    func()
	closed bool
}

func newAWSTab(w *Window) *awsTab {
	t := &awsTab{w: w}
	header := newPaneHeader("AWS Profiles")
	header.tool("list-add-symbolic", "Add profile", func() { t.showEditor(nil) })
	t.reload = header.tool("view-refresh-symbolic", "Reload", func() { awsStore.load(t.svc()) })
	t.filter = gtk.NewSearchEntry()
	t.filter.SetPlaceholderText("Filter profiles")
	t.filter.AddCSSClass("wtf-side-filter")
	t.filter.ConnectSearchChanged(t.rebuild)
	t.filter.ConnectStopSearch(func() {
		t.filter.SetText("")
		w.focusActive()
	})
	t.note = gtk.NewBox(gtk.OrientationVertical, 0)
	t.list = newSideList(false)
	t.list.onActivate = func(id string) {
		if p, ok := t.profile(id); ok {
			t.use(p)
		}
	}
	t.list.onMenu = func(ids []string, x, y float64) {
		if p, ok := t.profile(ids[0]); ok {
			t.list.menu.show(x, y, t.menu(p), nil)
		}
	}
	t.root = gtk.NewBox(gtk.OrientationVertical, 4)
	t.root.Append(header.root)
	t.root.Append(t.filter)
	t.root.Append(t.note)
	t.root.Append(t.list.root)
	t.off = awsStore.observe(t.rebuild)
	if !awsStore.loaded && !awsStore.loading {
		awsStore.load(t.svc())
	}
	t.rebuild()
	return t
}

func (t *awsTab) widget() gtk.Widgetter { return t.root }

func (t *awsTab) close() {
	t.closed = true
	t.off()
}

// paneChanged: the in-use mark follows the focused shell.
func (t *awsTab) paneChanged() { t.rebuild() }

func (t *awsTab) commandFinished() {}

func (t *awsTab) svc() *awsprofiles.Service { return t.w.app.be.AWSProfiles }

func (t *awsTab) pretty(path string) string { return uicore.PrettyPath(path, t.w.app.home) }

func (t *awsTab) profile(name string) (awsprofiles.Profile, bool) {
	for _, p := range awsStore.list.Profiles {
		if p.Name == name {
			return p, true
		}
	}
	return awsprofiles.Profile{}, false
}

func (t *awsTab) inUse() string {
	if ps := t.w.model.ActivePane(); ps != nil {
		return uicore.AWSInUse(t.w.infra[ps.ID])
	}
	return ""
}

func (t *awsTab) rebuild() {
	if t.closed {
		return
	}
	t.reload.SetSensitive(!awsStore.loading)
	inUse := t.inUse()
	q := t.filter.Text()
	var rows []sideRow
	for _, p := range awsStore.list.Profiles {
		if uicore.AWSMatches(p, q) {
			rows = append(rows, t.row(p, inUse))
		}
	}
	sections := []sideSection{{rows: rows}}
	if probs := awsStore.list.Problems; len(probs) > 0 {
		sections = append(sections, sideSection{title: "Problems", rows: sshProblemRows(probs)})
	}
	t.list.set(sections)
	var note *gtk.Box
	if awsStore.loaded && len(rows) == 0 {
		if strings.TrimSpace(q) == "" {
			note = sshNote("No AWS profiles", "Profiles from ~/.aws/credentials and ~/.aws/config show here. Press + to add one.")
		} else {
			note = sshNote("", "No profiles match “"+q+"”.")
		}
	}
	sshSetNote(t.note, note)
}

func (t *awsTab) row(p awsprofiles.Profile, inUse string) sideRow {
	l := awsStore.list
	r := sideRow{id: p.Name, icon: "weather-overcast-symbolic", title: p.Name, mono: true, detail: uicore.AWSDetail(p),
		tip:    uicore.AWSTooltip(p, l.CredentialsFile, l.ConfigFile, t.pretty),
		button: "media-playback-start-symbolic", buttonTip: "Use in this shell", buttonRun: func() { t.use(p) }}
	if p.Name == inUse {
		r.pills = []sidePill{{text: "in use", class: "in-use", tip: "The focused shell uses this profile"}}
	}
	c := awsStore.checks[p.Name]
	if c == nil {
		return r
	}
	// A check's results show under the profile, a line each; all of it is
	// in the tooltip. The row takes the first line's color.
	type line struct{ text, class string }
	var lines []line
	if c.busy {
		lines = append(lines, line{"Asking AWS…", "dim-label"})
	}
	if c.identity != nil {
		lines = append(lines, line{uicore.AWSShortArn(c.identity.Arn), "success"})
		r.tip += "\n\n" + c.identity.Arn
	} else if c.identityErr != "" {
		lines = append(lines, line{c.identityErr, "error"})
	}
	if c.keysChecked {
		now := time.Now()
		var tips []string
		for _, k := range c.keys {
			lines = append(lines, line{uicore.AWSKeyLine(k, now), "dim-label"})
			tip := uicore.AWSKeyLine(k, now) + "\nCreated " + k.Created
			if k.LastUsed != "" {
				tip += "\nLast used " + k.LastUsed
			}
			tips = append(tips, tip)
		}
		if len(tips) > 0 {
			r.tip += "\n\n" + strings.Join(tips, "\n")
		}
	} else if c.keysErr != "" {
		lines = append(lines, line{c.keysErr, "error"})
	}
	if len(lines) > 0 {
		texts := make([]string, len(lines))
		for i, x := range lines {
			texts[i] = x.text
		}
		r.alert, r.alertClass = strings.Join(texts, "\n"), lines[0].class
	}
	return r
}

// use exports AWS_PROFILE in the focused shell (no Ctrl-U first), or
// opens a tab with it.
func (t *awsTab) use(p awsprofiles.Profile) {
	cmd := uicore.AWSUseCommand(p.Name)
	ps, pane := t.w.model.ActivePane(), t.w.active()
	if ps != nil && pane != nil && ps.IsShell && !ps.Running {
		pane.write(cmd + "\r")
		pane.term.GrabFocus()
		t.w.toast("This shell now uses " + p.Name)
		return
	}
	t.w.model.Open(uicore.OpenOptions{Command: cmd})
	t.w.toast("Opened a tab using " + p.Name)
}

func (t *awsTab) copy(text, toast string) {
	t.w.win.Clipboard().SetText(text)
	t.w.toast(toast)
}

func (t *awsTab) menu(p awsprofiles.Profile) []*menuItem {
	w, svc := t.w, t.svc()
	items := []*menuItem{
		item("Use in This Shell", func() { t.use(p) }),
		item("Open New Tab with Profile", func() { w.model.Open(uicore.OpenOptions{Command: uicore.AWSUseCommand(p.Name)}) }),
	}
	if p.Kind == "sso" {
		items = append(items, item("Log In with SSO", func() {
			w.model.Open(uicore.OpenOptions{Command: "aws sso login --profile " + uicore.ShellQuote(p.Name)})
		}))
	}
	items = append(items, menuSection, item("Check Identity (sts)", func() { awsStore.verify(svc, p.Name) }))
	if p.Kind == "keys" {
		items = append(items, item("Check Access Keys (iam)", func() { awsStore.checkKeys(svc, p.Name) }))
	}
	items = append(items, menuSection, item("Edit…", func() { t.showEditor(&p) }))
	if p.Kind == "keys" && strings.HasPrefix(p.AccessKeyID, "AKIA") {
		items = append(items, item("Rotate Access Key…", func() { t.showRotate(p) }))
	}
	if p.Name != "default" {
		items = append(items, item("Copy to Default…", func() {
			t.confirmChange("Copy "+p.Name+" to default?", "Copy to Default",
				" — the CLI uses [default] when no profile is chosen",
				func(v map[string]string) ([]string, error) { return svc.PlanMakeDefault(p.Name, v) },
				func(v map[string]string) (awsprofiles.List, error) { return svc.MakeDefault(p.Name, v) },
				"default now has "+p.Name+"’s settings")
		}))
	}
	items = append(items, menuSection)
	if p.AccessKeyID != "" {
		items = append(items, item("Copy Access Key ID", func() { t.copy(p.AccessKeyID, "Copied access key ID") }))
	}
	if p.Account != "" {
		items = append(items, item("Copy Account ID ("+p.Account+")", func() { t.copy(p.Account, "Copied account ID") }))
	}
	items = append(items, item("Copy Name", func() { t.copy(p.Name, "Copied profile name") }), menuSection,
		item("Delete…", func() {
			note := ""
			if p.AccessKeyID != "" {
				note = " (the key stays valid in AWS; this only removes it from this computer)"
			}
			t.confirmChange("Delete "+p.Name+"?", "Delete Profile", note,
				func(v map[string]string) ([]string, error) { return svc.PlanDelete(p.Name, v) },
				func(v map[string]string) (awsprofiles.List, error) { return svc.Delete(p.Name, v) },
				"Deleted "+p.Name)
		}))
	return items
}

// confirmChange plans a change, shows every line of it, and makes it once
// confirmed.
func (t *awsTab) confirmChange(title, button, note string, plan func(map[string]string) ([]string, error),
	run func(map[string]string) (awsprofiles.List, error), done string) {
	w := t.w
	versions := awsStore.list.Versions
	go func() {
		steps, err := plan(versions)
		onMainAsync(func() {
			switch {
			case err != nil:
				w.toast(err.Error())
			case len(steps) == 0:
				w.toast("Nothing to change")
			default:
				body := "wtf will make these changes" + note + ". A copy of each file is kept in wtf’s cache folder (aws-backups).\n\n• " +
					strings.Join(steps, "\n• ")
				w.app.confirm(&w.win.Window, title, body, button, func() {
					go func() {
						l, err := run(versions)
						onMainAsync(func() {
							if err != nil {
								w.toast(err.Error())
								return
							}
							awsStore.apply(l)
							w.toast(done)
						})
					}()
				})
			}
		})
	}()
}

// The editor's kinds of credentials ("none", settings only, isn't one to
// choose).
var awsEditorKinds = []string{"keys", "role", "sso", "process"}

// showEditor adds (p nil) or edits a profile: a form, then the exact
// changes to the two files, written only once confirmed. Secrets are
// write-only. Like AwsProfileEditor.svelte and the macOS app's
// AwsProfileEditor.
func (t *awsTab) showEditor(p *awsprofiles.Profile) {
	w, svc := t.w, t.svc()
	l := awsStore.list
	d := uicore.NewAWSDraft()
	title, original := "New AWS Profile", ""
	if p != nil {
		d = uicore.AWSDraftOf(*p)
		title, original = "Edit "+p.Name, p.Name
	}
	subtitle := "In " + t.pretty(l.CredentialsFile) + " and " + t.pretty(l.ConfigFile)
	f := newSSHForm(w, title, subtitle, 470)
	var plan []string
	busy := false
	// The dialog closes after each answer; it comes back here.
	again := func() { onMainAsync(func() { f.d.Present() }) }

	form := gtk.NewBox(gtk.OrientationVertical, 10)
	name := f.entry(d.Name, "prod")
	name.ConnectChanged(func() {
		d.Name = name.Text()
		f.enable("review", strings.TrimSpace(d.Name) != "")
	})
	nameHelp := ""
	if p != nil && len(p.SourceFor) > 0 {
		nameHelp = "Source profile for " + strings.Join(p.SourceFor, ", ") + ": they follow a rename."
	}
	form.Append(awsField(f, "Name", name, nameHelp))

	kindBar := gtk.NewBox(gtk.OrientationHorizontal, 0)
	kindBar.AddCSSClass("linked")
	kindBar.SetHomogeneous(true)
	kindBox := gtk.NewBox(gtk.OrientationVertical, 10)
	field := func(label string, control gtk.Widgetter, help string) {
		kindBox.Append(awsField(f, label, control, help))
	}
	// The kind's own fields, drawn again when the kind changes.
	showKind := func() {
		for c := kindBox.FirstChild(); c != nil; c = kindBox.FirstChild() {
			kindBox.Remove(c)
		}
		if d.Kind == "keys" {
			id := f.entry(d.AccessKeyID, "AKIA…")
			id.AddCSSClass("monospace")
			id.ConnectChanged(func() { d.AccessKeyID = id.Text() })
			field("Access key ID", id, "")
			keeps := p != nil && p.HasSecret && strings.TrimSpace(d.AccessKeyID) == p.AccessKeyID
			placeholder, help := "", "wtf never shows a saved secret again."
			if keeps {
				placeholder, help = "unchanged", "Leave empty to keep the current secret. "+help
			}
			secret := f.entry(d.SecretAccessKey, placeholder)
			secret.SetVisibility(false)
			secret.SetInputPurpose(gtk.InputPurposePassword)
			secret.ConnectChanged(func() { d.SecretAccessKey = secret.Text() })
			field("Secret access key", secret, help)
			placeholder = "only for temporary (ASIA…) keys"
			if p != nil && p.HasSessionToken {
				placeholder = "unchanged"
			}
			token := f.entry(d.SessionToken, placeholder)
			token.SetVisibility(false)
			token.SetInputPurpose(gtk.InputPurposePassword)
			token.SetSensitive(!d.RemoveSessionToken)
			token.ConnectChanged(func() { d.SessionToken = token.Text() })
			field("Session token", token, "")
			if p != nil && p.HasSessionToken {
				rm := f.check("Remove the session token", d.RemoveSessionToken)
				rm.ConnectToggled(func() {
					d.RemoveSessionToken = rm.Active()
					token.SetSensitive(!rm.Active())
				})
				kindBox.Append(rm)
			}
			return
		}
		for _, fd := range uicore.AWSKindFields[d.Kind] {
			key := fd.Key
			var e *gtk.Entry
			var control gtk.Widgetter
			if key == "source_profile" {
				var names []string
				for _, x := range l.Profiles {
					if p == nil || x.Name != p.Name {
						names = append(names, x.Name)
					}
				}
				control, e = sshCombo(names, d.Fields[key], fd.Placeholder)
			} else {
				e = f.entry(d.Fields[key], fd.Placeholder)
				control = e
			}
			e.ConnectChanged(func() { d.Fields[key] = e.Text() })
			field(fd.Label, control, "")
		}
	}
	var first *gtk.ToggleButton
	for _, k := range awsEditorKinds {
		b := gtk.NewToggleButtonWithLabel(uicore.AWSKindLabel[k])
		b.SetFocusOnClick(false)
		if first == nil {
			first = b
		} else {
			b.SetGroup(first)
		}
		b.SetActive(k == d.Kind)
		b.ConnectToggled(func() {
			if b.Active() && d.Kind != k {
				d.Kind = k
				showKind()
			}
		})
		kindBar.Append(b)
	}
	form.Append(kindBar)
	form.Append(kindBox)
	showKind()

	regionBox, region := sshCombo(uicore.AWSRegions, d.Region, "us-east-1")
	region.ConnectChanged(func() { d.Region = region.Text() })
	outputs := make([]string, len(uicore.AWSOutputs))
	for i, o := range uicore.AWSOutputs {
		outputs[i] = o
		if o == "" {
			outputs[i] = "default (json)"
		}
	}
	output := gtk.NewDropDownFromStrings(outputs)
	output.SetSelected(uint(max(0, slices.Index(uicore.AWSOutputs, d.Output))))
	output.NotifyProperty("selected", func() {
		if i := int(output.Selected()); i >= 0 && i < len(uicore.AWSOutputs) {
			d.Output = uicore.AWSOutputs[i]
		}
	})
	regionCol := sshColumn("Region", regionBox)
	regionCol.SetHExpand(true)
	form.Append(sshRowOf(regionCol, sshColumn("Output", output)))

	more := gtk.NewTextView()
	more.AddCSSClass("monospace")
	more.SetWrapMode(gtk.WrapWordChar)
	more.Buffer().SetText(d.More)
	more.Buffer().ConnectChanged(func() { d.More = sshTextOf(more) })
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetSizeRequest(-1, 52)
	scroll.AddCSSClass("card")
	scroll.SetChild(more)
	help := "Other ~/.aws/config settings, one “key = value” per line, e.g. duration_seconds = 3600."
	if p != nil && len(p.Locked) > 0 {
		help += " Nested settings (" + strings.Join(p.Locked, ", ") + ") are kept as they are."
	}
	form.Append(awsField(f, "More settings", scroll, help))
	if p != nil && len(p.CredentialsOther) > 0 {
		var other []string
		for _, s := range p.CredentialsOther {
			other = append(other, s.Key+" = "+s.Value)
		}
		form.Append(f.label("Also in the credentials file, kept as written: "+strings.Join(other, ", ")+".", false))
	}

	// The review: every change to the files, before any is made.
	review := gtk.NewBox(gtk.OrientationVertical, 10)
	review.SetVisible(false)
	planLines := f.label("", true)
	planNote := f.label("", false)
	review.Append(planLines)
	review.Append(planNote)
	f.add(form)
	f.add(review)

	reviewing := func(on bool) {
		form.SetVisible(!on)
		review.SetVisible(on)
		if on {
			f.d.SetHeading("Review Changes")
			f.d.SetBody("Nothing is written until you confirm.")
			f.d.SetResponseLabel("cancel", "Back")
			f.d.SetResponseLabel("review", "Confirm & Save")
			bullets := make([]string, len(plan))
			for i, s := range plan {
				bullets[i] = "• " + s
			}
			planLines.SetText(strings.Join(bullets, "\n"))
			if len(plan) == 0 {
				planNote.SetText("Nothing to change.")
			} else {
				planNote.SetText("A copy of each file is kept in wtf’s cache folder (aws-backups) before it changes.")
			}
			f.enable("review", len(plan) > 0 && !busy)
		} else {
			f.d.SetHeading(title)
			f.d.SetBody(subtitle)
			f.d.SetResponseLabel("cancel", "Cancel")
			f.d.SetResponseLabel("review", "Review…")
			f.enable("review", strings.TrimSpace(d.Name) != "" && !busy)
		}
	}
	goDraft := func() (awsprofiles.Draft, error) { return uicore.AWSGoDraft(d, original, awsStore.list.Versions) }
	failed := func(msg string) {
		f.setError(msg)
		again()
	}
	f.button("cancel", "Cancel", adw.ResponseDefault, func() {
		if plan == nil {
			f.close()
			w.focusActive()
			return
		}
		plan = nil
		reviewing(false)
		again()
	})
	f.button("review", "Review…", adw.ResponseSuggested, func() {
		if busy {
			again()
			return
		}
		if strings.TrimSpace(d.Name) == "" {
			failed("Give the profile a name")
			return
		}
		draft, err := goDraft()
		if err != nil {
			failed(err.Error())
			return
		}
		busy = true
		f.enable("review", false)
		if plan == nil {
			go func() {
				steps, err := svc.PlanSave(draft)
				onMainAsync(func() {
					busy = false
					if err != nil {
						reviewing(false)
						failed(err.Error())
						return
					}
					plan = steps
					if plan == nil {
						plan = []string{}
					}
					reviewing(true)
					f.d.Present()
				})
			}()
			return
		}
		go func() {
			l, err := svc.Save(draft)
			onMainAsync(func() {
				busy = false
				if err != nil {
					plan = nil
					reviewing(false)
					failed(err.Error())
					return
				}
				awsStore.apply(l)
				f.close()
				w.focusActive()
				if p != nil {
					w.toast("Saved " + draft.Name)
				} else {
					w.toast("Added " + draft.Name)
				}
			})
		}()
	})
	f.present(name)
	reviewing(false)
}

// awsField is a labelled control with optional help under it, like
// sshForm.field but in a box of its own.
func awsField(f *sshForm, label string, control gtk.Widgetter, help string) *gtk.Box {
	b := gtk.NewBox(gtk.OrientationVertical, 4)
	l := gtk.NewLabel(label)
	l.SetXAlign(0)
	l.AddCSSClass("caption-heading")
	b.Append(l)
	b.Append(control)
	if help != "" {
		b.Append(f.label(help, false))
	}
	return b
}

// showRotate rotates an IAM user's access key: check, show the plan, then
// create, write, verify, deactivate and delete — undoing what it can on
// failure. The new secret never reaches the UI. Like AwsRotate.svelte and
// the macOS app's AwsRotate.
func (t *awsTab) showRotate(p awsprofiles.Profile) {
	w, svc := t.w, t.svc()
	f := newSSHForm(w, "Rotate Access Key — "+p.Name, "Replaces "+p.AccessKeyID+" with a new key for the same IAM user", 480)
	const (
		checking = iota
		ready
		rotating
		done
		failed
	)
	phase, deleteOld := checking, true
	var lines []string
	status := f.label("", false)
	steps := f.label("", false)
	steps.RemoveCSSClass("dim-label")
	steps.RemoveCSSClass("caption")
	del := f.check("Delete the old key afterwards (otherwise it’s left deactivated; AWS allows two keys per user)", true)
	undo := f.label("If AWS doesn’t accept the new key, wtf puts the old one back and deletes the new one.", false)
	f.add(status)
	f.add(steps)
	f.add(del)
	f.add(undo)
	render := func() {
		status.SetVisible(phase == checking || phase == rotating)
		if phase == checking {
			status.SetText("Checking the key with AWS…")
		} else {
			status.SetText("Rotating — this takes up to half a minute while AWS takes the new key on…")
		}
		numbered := make([]string, len(lines))
		for i, s := range lines {
			numbered[i] = fmt.Sprintf("%d. %s", i+1, s)
		}
		steps.SetText(strings.Join(numbered, "\n"))
		steps.SetVisible(phase == ready || phase == done)
		del.SetVisible(phase == ready)
		undo.SetVisible(phase == ready)
		f.enable("rotate", phase == ready)
		f.enable("cancel", phase != rotating)
		switch phase {
		case ready:
			f.d.SetResponseLabel("cancel", "Cancel")
		case done:
			f.d.SetResponseLabel("cancel", "Done")
		default:
			f.d.SetResponseLabel("cancel", "Close")
		}
	}
	check := func() {
		phase = checking
		f.setError("")
		render()
		go func() {
			l, err := svc.PlanRotate(p.Name, deleteOld)
			onMainAsync(func() {
				if err != nil {
					f.setError(err.Error())
					phase = failed
				} else {
					lines, phase = l, ready
				}
				render()
			})
		}()
	}
	del.ConnectToggled(func() {
		deleteOld = del.Active()
		check()
	})
	f.button("cancel", "Close", adw.ResponseDefault, func() {
		// Rotating can't be left half done from here.
		if phase == rotating {
			onMainAsync(func() { f.d.Present() })
			return
		}
		f.close()
		w.focusActive()
	})
	f.button("rotate", "Rotate Key", adw.ResponseSuggested, func() {
		phase = rotating
		render()
		onMainAsync(func() { f.d.Present() })
		versions := awsStore.list.Versions
		go func() {
			res, err := svc.Rotate(p.Name, deleteOld, versions)
			onMainAsync(func() {
				if err != nil {
					f.setError(err.Error())
					phase = failed
					awsStore.load(svc)
				} else {
					lines = res.Steps
					delete(awsStore.checks, p.Name)
					awsStore.apply(res.List)
					phase = done
					w.toast(p.Name + " now uses " + res.NewKeyID)
				}
				render()
			})
		}()
	})
	f.present(nil)
	check()
}

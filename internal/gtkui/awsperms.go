//go:build linux

package gtkui

import (
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/awsprofiles"
)

// awsPermsViewer is Check Permissions (iam): who a profile is and the IAM
// policies attached to it, each policy's document read when asked. Only
// reads. Like the macOS app's AwsPermissionsViewer.
type awsPermsViewer struct {
	*viewer
	profile string
	perms   *awsprofiles.Permissions
	err     string
	// Per policy (by key): its document, an error, or loading.
	docs    map[string]string
	docErrs map[string]string
	loading map[string]bool
}

func policyKey(p awsprofiles.Policy) string {
	return p.Via + "\n" + p.Owner + "\n" + p.Name + "\n" + p.Arn
}

// showAWSPermissions opens the viewer and asks AWS.
func (w *Window) showAWSPermissions(profile string) {
	v := &awsPermsViewer{viewer: newViewer(w, 620), profile: profile,
		docs: map[string]string{}, docErrs: map[string]string{}, loading: map[string]bool{}}
	v.render()
	v.show()
	svc := w.app.be.AWSProfiles
	go func() {
		p, err := svc.Permissions(profile)
		onMainAsync(func() {
			if v.closed {
				return
			}
			if err != nil {
				v.err = err.Error()
			} else {
				v.perms = &p
			}
			v.render()
		})
	}()
}

func awsKindLabel(kind string) string {
	switch kind {
	case "user":
		return "IAM user"
	case "role":
		return "Assumed role"
	case "root":
		return "Root user"
	}
	return "Other"
}

// policyHeading groups policies: "Permissions boundary", "Attached to user x"…
func policyHeading(p awsprofiles.Policy) string {
	switch p.Via {
	case "boundary":
		return "Permissions boundary"
	case "user":
		return "Attached to user " + p.Owner
	case "group":
		return "Group " + p.Owner
	case "role":
		return "Role " + p.Owner
	}
	return p.Via
}

func (v *awsPermsViewer) toggleDoc(p awsprofiles.Policy) {
	k := policyKey(p)
	if _, ok := v.docs[k]; ok {
		delete(v.docs, k)
		v.render()
		return
	}
	if v.loading[k] {
		return
	}
	v.loading[k] = true
	delete(v.docErrs, k)
	v.render()
	svc, profile := v.w.app.be.AWSProfiles, v.profile
	go func() {
		doc, err := svc.PolicyDocument(profile, p)
		onMainAsync(func() {
			if v.closed {
				return
			}
			delete(v.loading, k)
			if err != nil {
				v.docErrs[k] = err.Error()
			} else {
				v.docs[k] = doc
			}
			v.render()
		})
	}()
}

func (v *awsPermsViewer) render() {
	v.clear()
	v.setTitle("Permissions of "+v.profile, "What's attached to these credentials, as IAM reports it. Service control, resource and session policies can still allow less.")
	switch {
	case v.err != "":
		v.add(viewerValue(withInstallHint(v.err), "error"))
		v.done()
		return
	case v.perms == nil:
		v.add(viewerValue("Asking AWS…", "dim-label"))
		v.done()
		return
	}
	p := v.perms
	facts := []viewerFact{
		{"ARN", viewerValue(p.Identity.Arn, "wtf-viewer-mono")},
		{"Account", viewerValue(p.Identity.Account, "wtf-viewer-mono")},
		{"Kind", viewerValue(awsKindLabel(p.Kind))},
	}
	if p.Name != "" {
		facts = append(facts, viewerFact{"Name", viewerValue(p.Name, "wtf-viewer-mono")})
	}
	if p.Session != "" {
		facts = append(facts, viewerFact{"Session", viewerValue(p.Session, "wtf-viewer-mono")})
	}
	if len(p.Groups) > 0 {
		facts = append(facts, viewerFact{"Groups", viewerValue(strings.Join(p.Groups, ", "))})
	}
	v.add(viewerFacts(facts))
	switch p.Kind {
	case "root":
		v.add(viewerValue("The root user can do anything in the account; IAM policies don't limit it.", "dim-label"))
	case "other":
		v.add(viewerValue("wtf can't list policies for this kind of identity.", "dim-label"))
	}

	list := gtk.NewBox(gtk.OrientationVertical, 6)
	heading := ""
	for _, pol := range p.Policies {
		if h := policyHeading(pol); h != heading {
			heading = h
			hl := viewerHeading(h)
			hl.SetMarginTop(6)
			list.Append(hl)
		}
		name := gtk.NewLabel(pol.Name)
		name.SetXAlign(0)
		name.SetSelectable(true)
		name.AddCSSClass("wtf-viewer-mono")
		row := viewerRow(name)
		if pol.AWSManaged {
			row.Append(viewerTag("AWS managed", ""))
		}
		if pol.Inline {
			row.Append(viewerTag("inline", ""))
		}
		spacer := gtk.NewBox(gtk.OrientationHorizontal, 0)
		spacer.SetHExpand(true)
		row.Append(spacer)
		k := policyKey(pol)
		_, shown := v.docs[k]
		label := "Show"
		switch {
		case v.loading[k]:
			label = "Reading…"
		case shown:
			label = "Hide"
		}
		show := gtk.NewButtonWithLabel(label)
		show.AddCSSClass("flat")
		show.SetSensitive(!v.loading[k])
		show.SetTooltipText("The policy's JSON document")
		show.ConnectClicked(func() { v.toggleDoc(pol) })
		row.Append(show)
		if pol.Arn != "" {
			arn := pol.Arn
			cp := gtk.NewButtonFromIconName("edit-copy-symbolic")
			cp.AddCSSClass("flat")
			cp.SetTooltipText("Copy ARN")
			cp.ConnectClicked(func() {
				v.w.win.Clipboard().SetText(arn)
				v.w.toast("Copied policy ARN")
			})
			row.Append(cp)
		}
		if pol.Arn != "" {
			row.SetTooltipText(pol.Arn)
		}
		list.Append(row)
		if e := v.docErrs[k]; e != "" {
			list.Append(viewerValue(e, "error"))
		}
		if shown {
			box, _ := v.textBox(v.docs[k], v.maxBoxHeight()/3, false)
			list.Append(box)
		}
	}
	if len(p.Policies) == 0 && (p.Kind == "user" || p.Kind == "role") && len(p.Problems) == 0 {
		list.Append(viewerValue("No policies are attached.", "dim-label"))
	}
	for _, prob := range p.Problems {
		list.Append(viewerValue(prob, "error"))
	}
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetPropagateNaturalHeight(true)
	scroll.SetMaxContentHeight(v.maxBoxHeight() * 6 / 10)
	scroll.SetChild(list)
	v.add(scroll)
	v.done()
}

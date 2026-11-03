//go:build linux

package gtkui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/uicore"
)

// checkHistory (WTF_NATIVE_DEBUG=history): saved history (#17), in a
// throwaway store (backend.go). A shell points at a Kubernetes context and
// an AWS profile and region and runs commands, one with a secret; History's
// Saved view lists them newest first with that context, without the secret;
// the Kubernetes filter and search narrow it; clearing the pane's history
// forgets its saved commands.
func (w *Window) checkHistory() {
	a := w.app
	dir, _ := os.MkdirTemp("", "wtf-history-kube-")
	kube := filepath.Join(dir, "config")
	_ = os.WriteFile(kube, []byte("apiVersion: v1\nkind: Config\ncurrent-context: prod\ncontexts:\n- name: prod\n  context: {cluster: c1, namespace: shop}\n"+
		"clusters:\n- name: c1\n  cluster: {server: https://x}\n"), 0o600)
	p := w.active()
	check(waitUntil(5*time.Second, func() bool { return p != nil && len(p.tracker.Blocks(0, 1<<30)) > 0 }), "shell integration")
	if p == nil {
		w.finish()
		return
	}
	p.write("export KUBECONFIG=" + kube + " AWS_PROFILE=ops AWS_REGION=eu-west-1\r")
	check(waitUntil(5*time.Second, func() bool { ps := w.model.Panes[p.id]; return ps != nil && ps.Env["AWS_REGION"] == "eu-west-1" }),
		"the shell reports its context")
	for _, c := range []string{"echo first-saved", "echo token=ghp_abcdefghijklmnopqrstuvwxyz0123456789", "false"} {
		p.write(c + "\r")
		check(waitUntil(5*time.Second, func() bool {
			id, ok := p.tracker.LastFinished()
			b, _ := p.tracker.Info(id)
			return ok && b.Text == c
		}), "ran %q", c)
	}

	onMain(func() {
		w.sidebarState.HistoryScope = "saved"
		w.showSidebar(uicore.SidebarHistory)
	})
	var h *historyTab
	titles := func() []string {
		var out []string
		if h != nil && h.result != nil {
			for _, e := range h.result.Entries {
				out = append(out, e.Command)
			}
		}
		return out
	}
	check(waitUntil(5*time.Second, func() bool {
		h, _ = w.sidePane().(*historyTab)
		return h != nil && h.result != nil && len(h.result.Entries) >= 3
	}), "History's Saved view lists the commands: %q", titles())
	if h == nil || h.result == nil || len(h.result.Entries) < 3 {
		w.finish()
		return
	}
	var newest, secret string
	var ctx, ns, region, profile string
	var code int
	onMain(func() {
		e := h.result.Entries[0]
		newest, code = e.Command, e.ExitCode
		ctx, ns, region, profile = e.KubeContext, e.KubeNamespace, e.AWSRegion, e.AWSProfile
		secret = h.result.Entries[1].Command
	})
	check(newest == "false" && code == 1, "newest first, with its exit code: %q %d", newest, code)
	check(ctx == "prod" && ns == "shop" && profile == "ops" && region == "eu-west-1", "with its context: %s/%s, %s %s", ctx, ns, profile, region)
	check(strings.HasPrefix(secret, "echo token=") && !strings.Contains(secret, "ghp_"), "secrets are hidden before saving: %q", secret)
	var kubes []string
	onMain(func() { kubes = slices.Clone(h.kubeValues) })
	check(slices.Contains(kubes, "prod"), "the Kubernetes filter lists prod: %q", kubes)
	w.shot("history-saved")

	onMain(func() { h.filter.SetText("first") })
	check(waitUntil(5*time.Second, func() bool { t := titles(); return len(t) == 1 && t[0] == "echo first-saved" }), "search narrows it: %q", titles())
	onMain(func() {
		h.filter.SetText("")
		h.kube.SetSelected(uint(slices.Index(h.kubeValues, "prod")))
	})
	check(waitUntil(5*time.Second, func() bool { return len(titles()) >= 3 && h.kubeSel == "prod" }), "the prod filter keeps them: %q", titles())

	// Clearing the pane's history forgets its saved commands.
	onMain(func() {
		if ps := w.model.Panes[p.id]; ps != nil {
			_ = a.be.History.ForgetSession(ps.SessionID)
		}
		a.savedHistoryChanged()
	})
	check(waitUntil(5*time.Second, func() bool { return h.result != nil && len(h.result.Entries) == 0 }), "forgetting the shell's commands empties it: %q", titles())
	w.finish()
}

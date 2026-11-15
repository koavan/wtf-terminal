//go:build linux

package gtkui

import (
	"github.com/diamondburned/gotk4-adwaita/pkg/adw"

	"github.com/dayanraj/wtf/internal/update"
)

// A newer wtf on GitHub (internal/update): the daily check's toast, and
// Check for Updates…. Nothing is downloaded: Download opens the release
// page. Like the macOS app's Updates.swift.

func (a *App) listenUpdates() {
	a.events.on(update.EventAvailable, func(data any) {
		if r, ok := data.(update.Release); ok {
			if w := a.front(); w != nil {
				w.updateToast(r)
			}
		}
	})
}

// updateToast stays until dismissed; Details… offers Download and Skip.
func (w *Window) updateToast(r update.Release) {
	t := adw.NewToast("wtf " + r.Version + " is available")
	t.SetTimeout(0)
	t.SetButtonLabel("Details…")
	t.ConnectButtonClicked(func() { w.updateDialog(r) })
	w.toasts.AddToast(t)
	w.app.debugLog("toast: update %s", r.Version)
}

func (w *Window) updateDialog(r update.Release) {
	d := adw.NewMessageDialog(&w.win.Window, "wtf "+r.Version+" is available",
		r.Title+"\n\nYou have "+w.app.build.Version+". Download opens the release page on GitHub, with what's new and the .deb and .rpm.")
	d.AddResponse("later", "Later")
	d.AddResponse("skip", "Skip This Version")
	d.AddResponse("download", "Download…")
	d.SetResponseAppearance("download", adw.ResponseSuggested)
	d.SetDefaultResponse("download")
	d.SetCloseResponse("later")
	d.ConnectResponse(func(resp string) {
		switch resp {
		case "download":
			toolsOpenURL(w, r.URL)
		case "skip":
			go w.app.be.Update.Skip(r.Version)
		}
	})
	d.Present()
}

// checkForUpdates is Check for Updates…: asks now, and says what it found.
func (w *Window) checkForUpdates() {
	w.toast("Checking for updates…")
	svc := w.app.be.Update
	go func() {
		st, err := svc.Check()
		onMainAsync(func() {
			switch {
			case err != nil:
				w.toast("Couldn't check for updates: " + err.Error())
			case st.Available != nil:
				w.updateDialog(*st.Available)
			default:
				w.toast("wtf " + st.Current + " is the latest version")
			}
		})
	}()
}

//go:build linux

// Command wtf-gtk is the native Linux UI (GTK 4 + VTE): see internal/gtkui
// and NATIVE_LINUX_ROADMAP.md.
package main

import (
	"os"

	"github.com/dayanraj/wtf/internal/gtkui"
)

// Set at build time with -ldflags -X.
var version, commit, buildDate = "dev", "", ""

func main() {
	os.Exit(gtkui.Main(gtkui.Build{Version: version, Commit: commit, Date: buildDate}))
}

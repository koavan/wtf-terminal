package terminal

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
)

var standaloneID atomic.Uint64

// StartStandalone starts a shell the way Service.Create does, for a UI that
// doesn't run inside Wails (the native macOS spike). The caller drains
// Output, writes input with Write and hangs up with Close.
func StartStandalone(version string, cols, rows int) (*Session, error) {
	shell := resolveShell()
	dir := ""
	if cache, err := os.UserCacheDir(); err == nil {
		d := filepath.Join(cache, "wtf", "shell-integration")
		if err := installIntegration(d); err == nil {
			dir = d
		} else {
			log.Printf("shell integration disabled: %v", err)
		}
	}
	home, _ := os.UserHomeDir()
	args, extraEnv := integrate(dir, shell)
	svc := &Service{version: version}
	id := fmt.Sprintf("n%d", standaloneID.Add(1))
	return startSession(id, spawnOptions{
		Shell: shell,
		Args:  args,
		Cwd:   home,
		Cols:  clampDim(cols),
		Rows:  clampDim(rows),
		Env:   append(svc.env(), extraEnv...),
	})
}

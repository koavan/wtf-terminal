package tools

import (
	"os"
	"os/exec"
	"path/filepath"
)

// Find locates a program the way the features that run it do: on the
// shell's PATH first (it can differ from wtf's), then wtf's PATH, then where
// installers put programs, since an app started from the Finder gets a
// minimal PATH. "" when it isn't anywhere.
func Find(name, shellPATH string) string {
	for _, dir := range filepath.SplitList(shellPATH) {
		if !filepath.IsAbs(dir) {
			continue
		}
		if p := filepath.Join(dir, name); isExec(p) {
			return p
		}
	}
	if p, err := exec.LookPath(name); err == nil && filepath.IsAbs(p) {
		return p
	}
	for _, dir := range installDirs() {
		if p := filepath.Join(dir, name); isExec(p) {
			return p
		}
	}
	return ""
}

// installDirs are where Homebrew, the official installers and wtf's own
// installs (~/.local/bin) put programs. Tests replace it.
var installDirs = func() []string {
	dirs := []string{"/opt/homebrew/bin", "/usr/local/bin"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".local", "bin"))
	}
	return append(dirs, "/usr/bin", "/bin", "/usr/sbin", "/sbin", "/snap/bin")
}

func isExec(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

// onPATH reports whether dir is one of the entries of a PATH.
func onPATH(dir, path string) bool {
	for _, d := range filepath.SplitList(path) {
		if filepath.Clean(d) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}

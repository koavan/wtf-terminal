// Package about gathers what the About window shows: the version and
// build, the platform, the shell, and where wtf keeps its files — facts
// for a bug report, or for fixing something by hand.
package about

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

type Info struct {
	Version string `json:"version"`
	// The git commit it was built from (`git describe --dirty`), and when.
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`

	GoVersion    string `json:"goVersion"`
	WailsVersion string `json:"wailsVersion"`
	// "macOS 26.0 (25A123)", "Ubuntu 24.04.1 LTS"…
	OS   string `json:"os"`
	Arch string `json:"arch"`

	// The shell new tabs start, and whether shell integration loads.
	Shell            string `json:"shell"`
	ShellIntegration bool   `json:"shellIntegration"`

	ConfigFile   string `json:"configFile"`
	SnippetsFile string `json:"snippetsFile"`
	StateDir     string `json:"stateDir"`
	CacheDir     string `json:"cacheDir"`

	Repository string `json:"repository"`
}

const repository = "https://github.com/dayanraj-rently/wtf-terminal"

type Service struct {
	version, commit, buildDate string
	configDir, stateDir        string
	shell                      func() (string, bool)
}

func NewService(version, commit, buildDate, configDir, stateDir string, shell func() (string, bool)) *Service {
	return &Service{version: version, commit: commit, buildDate: buildDate, configDir: configDir, stateDir: stateDir, shell: shell}
}

// Info reads the facts afresh; it runs when the About window opens.
func (s *Service) Info() Info {
	in := Info{
		Version: s.version, Commit: s.commit, BuildDate: s.buildDate,
		GoVersion: runtime.Version(), Arch: runtime.GOARCH, OS: osName(),
		ConfigFile:   filepath.Join(s.configDir, "config.toml"),
		SnippetsFile: filepath.Join(s.configDir, "snippets.toml"),
		StateDir:     s.stateDir,
		Repository:   repository,
	}
	if in.Commit == "" {
		in.Commit = "unknown (a development build)"
	}
	if cache, err := os.UserCacheDir(); err == nil {
		in.CacheDir = filepath.Join(cache, "wtf")
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, d := range bi.Deps {
			if d.Path == "github.com/wailsapp/wails/v3" {
				in.WailsVersion = d.Version
			}
		}
	}
	if s.shell != nil {
		in.Shell, in.ShellIntegration = s.shell()
	}
	return in
}

// osName is the operating system's name and version.
func osName() string {
	switch runtime.GOOS {
	case "darwin":
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		v, err := exec.CommandContext(ctx, "/usr/bin/sw_vers", "-productVersion").Output()
		if err != nil {
			return "macOS"
		}
		name := "macOS " + strings.TrimSpace(string(v))
		if b, err := exec.CommandContext(ctx, "/usr/bin/sw_vers", "-buildVersion").Output(); err == nil {
			name += " (" + strings.TrimSpace(string(b)) + ")"
		}
		return name
	case "linux":
		if f, err := os.Open("/etc/os-release"); err == nil {
			defer f.Close()
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				if v, ok := strings.CutPrefix(sc.Text(), "PRETTY_NAME="); ok {
					return strings.Trim(v, `"`)
				}
			}
		}
		return "Linux"
	}
	return runtime.GOOS
}

package about

import (
	"runtime"
	"strings"
	"testing"
)

func TestInfo(t *testing.T) {
	s := NewService("1.2.3", "abc1234-dirty", "2026-10-09", "/cfg/wtf", "/state/wtf", func() (string, bool) { return "/bin/zsh", true })
	in := s.Info()
	if in.Version != "1.2.3" || in.Commit != "abc1234-dirty" || in.BuildDate != "2026-10-09" {
		t.Errorf("build %+v", in)
	}
	if in.ConfigFile != "/cfg/wtf/config.toml" || in.SnippetsFile != "/cfg/wtf/snippets.toml" || in.StateDir != "/state/wtf" {
		t.Errorf("paths %+v", in)
	}
	if in.Shell != "/bin/zsh" || !in.ShellIntegration || in.Arch != runtime.GOARCH || !strings.HasPrefix(in.GoVersion, "go") || in.OS == "" {
		t.Errorf("platform %+v", in)
	}
	if !strings.HasPrefix(in.Repository, "https://github.com/") {
		t.Errorf("repo %q", in.Repository)
	}
	if NewService("1", "", "", "/c", "/s", nil).Info().Commit == "" {
		t.Error("a build without a commit says so")
	}
}

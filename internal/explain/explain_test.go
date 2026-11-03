package explain

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestShellFacts(t *testing.T) {
	home, _ := os.UserHomeDir()
	got := ShellFacts(nil, map[string]string{"SHELL": "/bin/zsh"}, filepath.Join(home, "src"))
	if !strings.HasPrefix(got, "Folder: ~/src\nSystem: ") || !strings.Contains(got, ", zsh\n") {
		t.Fatalf("facts = %q", got)
	}
}

func TestToolsOnPath(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"kubectl", "not-a-known-tool"} {
		os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755)
	}
	// tools.Find also looks in the usual install folders.
	if got := ToolsOnPath(map[string]string{"PATH": dir}); !slices.Contains(got, "kubectl") || slices.Contains(got, "not-a-known-tool") || !slices.IsSorted(got) {
		t.Fatalf("tools = %v", got)
	}
}

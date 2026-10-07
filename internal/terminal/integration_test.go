package terminal

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runIntegrated starts `shell` with the integration installed and a scratch
// HOME, feeds it input, and returns everything it printed.
func runIntegrated(t *testing.T, shell string, home map[string]string, input string, until string) string {
	t.Helper()
	if _, err := exec.LookPath(shell); err != nil {
		t.Skipf("%s not installed", shell)
	}
	dir := t.TempDir()
	if err := installIntegration(dir); err != nil {
		t.Fatal(err)
	}
	homeDir := t.TempDir()
	for name, body := range home {
		if err := os.WriteFile(filepath.Join(homeDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	env := []string{"HOME=" + homeDir, "PATH=/usr/bin:/bin", "TERM=xterm-256color", "LANG=en_US.UTF-8"}
	args, extra := integrate(dir, shell)
	sess, err := startSession("it", spawnOptions{
		Shell: shell, Args: args, Cwd: homeDir, Cols: 120, Rows: 40,
		Env: append(env, extra...),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sess.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var out bytes.Buffer
	wait := func(want string) {
		for !strings.Contains(out.String(), want) {
			select {
			case chunk, ok := <-sess.Output:
				if !ok {
					t.Fatalf("shell exited before %q appeared:\n%q", want, out.String())
				}
				out.Write(chunk)
			case <-ctx.Done():
				t.Fatalf("timed out waiting for %q:\n%q", want, out.String())
			}
		}
	}
	wait("\x1b]133;A\x07") // first prompt
	if _, err := sess.Write([]byte(input)); err != nil {
		t.Fatal(err)
	}
	wait(until)
	return out.String()
}

// marks extracts the OSC 133/633 sequences, in order.
func marks(out string) []string {
	var got []string
	for {
		i := strings.Index(out, "\x1b]")
		if i < 0 {
			return got
		}
		out = out[i+2:]
		j := strings.IndexByte(out, '\x07')
		if j < 0 {
			return got
		}
		if seq := out[:j]; strings.HasPrefix(seq, "133;") || strings.HasPrefix(seq, "633;") {
			got = append(got, seq)
		}
		out = out[j+1:]
	}
}

func TestBashIntegration(t *testing.T) {
	out := runIntegrated(t, "/bin/bash",
		map[string]string{
			".bash_profile": `export FROM_PROFILE=yes
PROMPT_COMMAND='LAST_STATUS=$?'
`,
		},
		"false\recho \"profile=$FROM_PROFILE last=$LAST_STATUS\"\rexit\r",
		"profile=yes last=1",
	)

	got := strings.Join(marks(out), " | ")
	want := "133;A | 633;E;false | 133;C | 133;D;1 | 133;A | " +
		`633;E;echo "profile=$FROM_PROFILE last=$LAST_STATUS" | 133;C`
	if !strings.HasPrefix(got, want) {
		t.Fatalf("marks:\n got  %s\n want %s...", got, want)
	}
}

func TestBashEscapesCommandLine(t *testing.T) {
	out := runIntegrated(t, "/bin/bash", nil, "echo 'a;b\\c'\r", "\x1b]133;C\x07")
	for _, m := range marks(out) {
		if strings.HasPrefix(m, "633;E;") {
			if m != `633;E;echo 'a\x3bb\\c'` {
				t.Fatalf("command line mark = %q", m)
			}
			return
		}
	}
	t.Fatalf("no command line mark in %q", out)
}

func TestZshIntegration(t *testing.T) {
	out := runIntegrated(t, "/bin/zsh",
		map[string]string{".zshrc": "export FROM_ZSHRC=yes\n"},
		"false\recho \"zshrc=$FROM_ZSHRC\"\r",
		"zshrc=yes",
	)
	got := strings.Join(marks(out), " | ")
	want := "133;A | 633;E;false | 133;C | 133;D;1 | 133;A | " + `633;E;echo "zshrc=$FROM_ZSHRC" | 133;C`
	if !strings.HasPrefix(got, want) {
		t.Fatalf("marks:\n got  %s\n want %s...", got, want)
	}
}

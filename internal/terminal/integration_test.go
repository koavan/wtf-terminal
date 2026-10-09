package terminal

import (
	"bytes"
	"regexp"
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
	out, _, _ := runIntegratedHome(t, shell, home, input, until)
	return out
}

// runIntegratedHome is runIntegrated that also returns the scratch HOME and
// the running session.
func runIntegratedHome(t *testing.T, shell string, home map[string]string, input string, until string) (string, string, *Session) {
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
	return out.String(), homeDir, sess
}

// marks extracts the OSC 133/633 prompt and command marks, in order.
func marks(out string) []string {
	var got []string
	for _, m := range allMarks(out) {
		if !strings.HasPrefix(m, "633;P;") {
			got = append(got, m)
		}
	}
	return got
}

// envReports returns the context variables reported at each prompt.
func envReports(out string) []string {
	var got []string
	for _, m := range allMarks(out) {
		if v, ok := strings.CutPrefix(m, "633;P;WtfEnv="); ok {
			got = append(got, v)
		}
	}
	return got
}

// allMarks extracts the OSC 133/633 sequences, in order.
func allMarks(out string) []string {
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

func TestBashReportsEnv(t *testing.T) {
	out, home, _ := runIntegratedHome(t, "/bin/bash",
		map[string]string{".bash_profile": "export AWS_PROFILE='a;b' AWS_ACCESS_KEY_ID=AKIASECRET\n"},
		"export KUBECONFIG=/k\r",
		`KUBECONFIG=/k\x0a`, // only in a report; the echoed input has no \x0a
	)
	reports := envReports(out)
	home, _ = filepath.EvalSymlinks(home) // /var → /private/var on macOS
	want := `PWD=` + home + `\x0aKUBECONFIG=/k\x0aAWS_PROFILE=a\x3bb\x0aWTF_AWS_ENV_KEYS=1\x0a`
	if reports[len(reports)-1] != want {
		t.Fatalf("reports %q\nwant last %q", reports, want)
	}
}

func TestZshReportsEnvAfterUserHooks(t *testing.T) {
	// A precmd hook from .zshrc that changes the environment, like direnv's.
	zshrc := "autoload -Uz add-zsh-hook\nlate() { export TF_WORKSPACE=late }\nadd-zsh-hook precmd late\n"
	out, home, _ := runIntegratedHome(t, "/bin/zsh", map[string]string{".zshrc": zshrc}, "true\r", `TF_WORKSPACE=late\x0a`)
	reports := envReports(out)
	home, _ = filepath.EvalSymlinks(home) // /var → /private/var on macOS
	want := `PWD=` + home + `\x0aTF_WORKSPACE=late\x0a`
	if reports[len(reports)-1] != want {
		t.Fatalf("reports %q\nwant last %q", reports, want)
	}
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

var oscSeq = regexp.MustCompile("\x1b\\][^\x07]*\x07")

// historyLines returns the lines `listHistory` printed (tagged "H:"). The
// invisible prompt marks can share a line with output, so drop them first.
func historyLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(oscSeq.ReplaceAllString(out, ""), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(l), "H:"); ok {
			lines = append(lines, strings.TrimSpace(rest))
		}
	}
	return lines
}

// In zsh the left side of a pipe can't see the history, so go via a file.
const listHistory = "history > ~/h.txt; sed 's/^/H:/' ~/h.txt\recho DONE-$((1+1))\r"

// The "clear history" key sequence removes this session's commands from
// the shell's history, without running a visible command, and leaves the
// history file's earlier entries alone — including after the shell exits
// and saves its history.
func TestClearHistoryKey(t *testing.T) {
	for _, shell := range []string{"/bin/bash", "/bin/zsh"} {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			histFile := ".bash_history"
			if strings.HasSuffix(shell, "zsh") {
				histFile = ".zsh_history"
			}
			home := map[string]string{
				".zshrc":        "HISTSIZE=100\nSAVEHIST=100\nHISTFILE=~/.zsh_history\n",
				".bash_profile": "HISTSIZE=100\nHISTFILE=~/.bash_history\n",
				histFile:        "echo old-cmd-from-file\n",
			}
			input := "echo first-cmd\recho second-cmd\r" + ClearHistoryKeys + "echo after-cmd\r" + listHistory
			out, homeDir, sess := runIntegratedHome(t, shell, home, input, "DONE-2")

			lines := strings.Join(historyLines(out), "\n")
			if strings.Contains(lines, "first-cmd") || strings.Contains(lines, "second-cmd") {
				t.Fatalf("history still lists this session's commands:\n%s", lines)
			}
			if !strings.Contains(lines, "old-cmd-from-file") || !strings.Contains(lines, "after-cmd") {
				t.Fatalf("history lost saved or later commands:\n%s", lines)
			}
			for _, m := range marks(out) {
				if strings.HasPrefix(m, "633;E;") && !strings.Contains(m, "echo") && !strings.Contains(m, "history") {
					t.Fatalf("something other than the typed commands was reported: %q", m)
				}
			}

			// Exit; the shell saves its history. The file must keep the old entry.
			if _, err := sess.Write([]byte("exit\r")); err != nil {
				t.Fatal(err)
			}
			go func() {
				for range sess.Output {
				}
			}()
			select {
			case <-sess.Done:
			case <-time.After(10 * time.Second):
				t.Fatal("shell didn't exit")
			}
			saved, _ := os.ReadFile(filepath.Join(homeDir, histFile))
			if !strings.Contains(string(saved), "old-cmd-from-file") {
				t.Fatalf("the history file lost its earlier entries:\n%s", saved)
			}
			if strings.Contains(string(saved), "first-cmd") {
				t.Fatalf("the cleared command was saved anyway:\n%s", saved)
			}
		})
	}
}

// Without the clear, history lists earlier commands (so the test above
// really shows the clearing working).
func TestHistoryListsCommandsWithoutClear(t *testing.T) {
	for _, shell := range []string{"/bin/bash", "/bin/zsh"} {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			home := map[string]string{".zshrc": "HISTSIZE=100\n", ".bash_profile": "HISTSIZE=100\n"}
			out := runIntegrated(t, shell, home, "echo first-cmd\r"+listHistory, "DONE-2")
			found := false
			for _, l := range historyLines(out) {
				found = found || strings.Contains(l, "first-cmd")
			}
			if !found {
				t.Fatalf("history doesn't list the earlier command: %q", historyLines(out))
			}
		})
	}
}

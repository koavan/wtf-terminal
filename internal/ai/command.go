package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/dayanraj/wtf/internal/terminal"
	"github.com/dayanraj/wtf/internal/tools"
)

// commandProvider runs a CLI tool: the prompt goes to its stdin and its
// stdout is the reply. It runs in an empty folder, so a coding agent finds
// no project to act on; its own login, model and settings apply.
type commandProvider struct {
	argv []string
	env  map[string]string
}

func (p commandProvider) stream(ctx context.Context, system string, msgs []Message, _ int, out func(string)) (Usage, error) {
	shellPATH := p.env["PATH"]
	program := p.argv[0]
	if !strings.Contains(program, "/") {
		if found := tools.Find(program, shellPATH); found != "" {
			program = found
		} else {
			return Usage{}, fmt.Errorf("%s isn't installed, or isn't on your shell's PATH", p.argv[0])
		}
	}
	dir, err := os.MkdirTemp("", "wtf-ai-")
	if err != nil {
		return Usage{}, err
	}
	defer os.RemoveAll(dir)

	cmd := exec.CommandContext(ctx, program, p.argv[1:]...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	if shellPATH != "" {
		cmd.Env = append(cmd.Env, "PATH="+shellPATH)
	}
	cmd.Stdin = strings.NewReader(promptText(system, msgs))
	// In the macOS app, with a clean signal mask, so Stop's SIGTERM works.
	if terminal.Launcher != nil {
		cmd.Path, cmd.Args = terminal.Launcher(cmd.Path, cmd.Args)
	}
	// Stop the tool and anything it started.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 3 * time.Second
	var stderr tail
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Usage{}, err
	}
	if err := cmd.Start(); err != nil {
		return Usage{}, err
	}

	// Pass on whole UTF-8 characters only.
	buf := make([]byte, 4096)
	var held []byte
	for {
		n, rerr := stdout.Read(buf)
		if n > 0 {
			held = append(held, buf[:n]...)
			cut := len(held)
			if i := lastStart(held); !utf8.FullRune(held[i:]) {
				cut = i
			}
			if cut > 0 {
				out(string(held[:cut]))
				held = append(held[:0], held[cut:]...)
			}
		}
		if rerr != nil {
			if !errors.Is(rerr, io.EOF) && ctx.Err() == nil {
				err = rerr
			}
			break
		}
	}
	if len(held) > 0 {
		out(string(held))
	}
	if werr := cmd.Wait(); werr != nil && ctx.Err() == nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = werr.Error()
		}
		return Usage{}, fmt.Errorf("%s: %s", filepath.Base(p.argv[0]), msg)
	}
	return Usage{}, err
}

// lastStart is where the last UTF-8 character in b starts.
func lastStart(b []byte) int {
	i := len(b) - 1
	for i > 0 && !utf8.RuneStart(b[i]) {
		i--
	}
	return max(i, 0)
}

// promptText is a conversation as one text, for a tool that reads a prompt.
func promptText(system string, msgs []Message) string {
	var b strings.Builder
	if system != "" {
		b.WriteString(system)
		b.WriteString("\n\n")
	}
	if len(msgs) == 1 {
		b.WriteString(msgs[0].Content)
		return b.String()
	}
	for i, m := range msgs {
		if i > 0 {
			b.WriteString("\n\n")
		}
		if m.Role == "assistant" {
			b.WriteString("Assistant: ")
		} else {
			b.WriteString("User: ")
		}
		b.WriteString(m.Content)
	}
	return b.String()
}

// tail keeps the last few KB written to it, for a failed tool's message.
type tail struct{ b bytes.Buffer }

const tailMax = 4 << 10

func (t *tail) Write(p []byte) (int, error) {
	t.b.Write(p)
	if t.b.Len() > 2*tailMax {
		keep := append([]byte(nil), t.b.Bytes()[t.b.Len()-tailMax:]...)
		t.b.Reset()
		t.b.Write(keep)
	}
	return len(p), nil
}

func (t *tail) String() string {
	b := t.b.Bytes()
	if len(b) > tailMax {
		b = b[len(b)-tailMax:]
	}
	return string(b)
}

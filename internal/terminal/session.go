package terminal

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

const readBufSize = 64 * 1024

// Session is one shell process attached to a pseudo-terminal.
type Session struct {
	ID    string
	Shell string // base name of the shell binary, e.g. "zsh"

	cmd *exec.Cmd
	pty *os.File

	// Output carries PTY output chunks to whichever stream client is attached.
	// It is closed when the PTY reaches EOF (the shell exited).
	Output chan []byte
	// Done is closed once the shell process has been reaped.
	Done chan struct{}

	exitCode  int
	closeOnce sync.Once
	attachMu  sync.Mutex // allows only one stream consumer at a time
}

type spawnOptions struct {
	Shell string
	// Args is the full argv, including argv[0]; defaults to a login shell.
	Args []string
	Cwd  string
	Cols uint16
	Rows uint16
	Env  []string
}

func startSession(id string, opts spawnOptions) (*Session, error) {
	shellBase := filepath.Base(opts.Shell)

	cmd := exec.Command(opts.Shell)
	cmd.Args = opts.Args
	if len(cmd.Args) == 0 {
		cmd.Args = []string{"-" + shellBase} // leading dash = login shell
	}
	cmd.Dir = opts.Cwd
	cmd.Env = opts.Env

	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: opts.Cols, Rows: opts.Rows})
	if err != nil {
		return nil, err
	}

	s := &Session{
		ID:     id,
		Shell:  shellBase,
		cmd:    cmd,
		pty:    f,
		Output: make(chan []byte, 256),
		Done:   make(chan struct{}),
	}
	go s.readLoop()
	go s.wait()
	return s, nil
}

func (s *Session) readLoop() {
	defer close(s.Output)
	buf := make([]byte, readBufSize)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			// Blocks when nobody is draining output: that stalls the shell,
			// which is exactly the backpressure we want.
			s.Output <- chunk
		}
		if err != nil {
			return
		}
	}
}

func (s *Session) wait() {
	err := s.cmd.Wait()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		s.exitCode = 0
	case errors.As(err, &exitErr):
		s.exitCode = exitErr.ExitCode()
	default:
		s.exitCode = -1
	}
	close(s.Done)
}

// ExitCode is only meaningful after Done is closed.
func (s *Session) ExitCode() int { return s.exitCode }

func (s *Session) Write(p []byte) (int, error) { return s.pty.Write(p) }

func (s *Session) Resize(cols, rows uint16) error {
	return pty.Setsize(s.pty, &pty.Winsize{Cols: cols, Rows: rows})
}

// Close hangs up the terminal, which makes the shell and its jobs exit.
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		if s.cmd.Process != nil {
			// Negative pid signals the whole process group the shell leads.
			_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGHUP)
		}
		_ = s.pty.Close()
	})
}

// foreground returns the pid of the process group currently in the
// foreground of this terminal (the shell itself when it is idle).
func (s *Session) foreground() (int, error) {
	rc, err := s.pty.SyscallConn()
	if err != nil {
		return 0, err
	}
	var pgid int
	var ioErr error
	// Control (rather than Fd) keeps the file in non-blocking mode.
	err = rc.Control(func(fd uintptr) {
		pgid, ioErr = unix.IoctlGetInt(int(fd), unix.TIOCGPGRP)
	})
	if err != nil {
		return 0, err
	}
	return pgid, ioErr
}

// Info describes what is running in a session right now.
type Info struct {
	ID      string `json:"id"`
	Process string `json:"process"`
	Cwd     string `json:"cwd"`
	IsShell bool   `json:"isShell"`
}

func (s *Session) info() (Info, error) {
	pid, err := s.foreground()
	if err != nil {
		return Info{}, err
	}
	name := strings.TrimPrefix(processName(pid), "-")
	return Info{
		ID:      s.ID,
		Process: name,
		Cwd:     processCwd(pid),
		IsShell: name == s.Shell,
	}, nil
}

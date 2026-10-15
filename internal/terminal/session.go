package terminal

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

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
	closed    atomic.Bool
	attachMu  sync.Mutex // allows only one stream consumer at a time

	// Window that shows this session; guarded by Service.mu.
	window string

	// The terminal's size, cols<<16 | rows, for a recording's header.
	size atomic.Uint32
	// Set while the session is being recorded (see record.go).
	rec atomic.Pointer[recorder]
}

// Launcher, when set, starts shells through a helper: `Launcher(argv)` gives
// the command that runs argv. The native macOS app sets it so shells start
// with a clean signal mask (see cmd/libwtf): Go threads in a library block
// the signals Go doesn't handle, and a shell forked from one would pass
// that on to every program (no Ctrl-C, no hang-up, no resize signal).
var Launcher func(path string, argv []string) (string, []string)

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
	if Launcher != nil {
		cmd.Path, cmd.Args = Launcher(cmd.Path, cmd.Args)
	}

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
	s.size.Store(uint32(opts.Cols)<<16 | uint32(opts.Rows))
	go s.readLoop()
	go s.wait()
	return s, nil
}

func (s *Session) readLoop() {
	defer close(s.Output)
	// The shell is gone: a recording ends with it.
	defer func() { _, _ = s.StopRecording() }()
	buf := make([]byte, readBufSize)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if rec := s.rec.Load(); rec != nil {
				rec.output(chunk, time.Now())
			}
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
	if s.size.Swap(uint32(cols)<<16|uint32(rows)) != uint32(cols)<<16|uint32(rows) {
		if rec := s.rec.Load(); rec != nil {
			rec.resize(cols, rows, time.Now())
		}
	}
	return pty.Setsize(s.pty, &pty.Winsize{Cols: cols, Rows: rows})
}

// StartRecording records the session's output to an asciicast file at path.
func (s *Session) StartRecording(path, title string) error {
	if s.closed.Load() {
		return errors.New("the shell has exited")
	}
	size := s.size.Load()
	rec, err := newRecorder(path, uint16(size>>16), uint16(size), title, s.Shell, time.Now())
	if err != nil {
		return err
	}
	if !s.rec.CompareAndSwap(nil, rec) {
		rec.close()
		return errors.New("this terminal is already being recorded")
	}
	return nil
}

// StopRecording ends a recording, if there is one, and says how it went.
func (s *Session) StopRecording() (RecordingStatus, error) {
	rec := s.rec.Swap(nil)
	if rec == nil {
		return RecordingStatus{}, nil
	}
	st := rec.status(time.Now())
	err := rec.close()
	st.Active = false
	return st, err
}

// Recording is the session's recording now; Active is false when there's none.
func (s *Session) Recording() RecordingStatus {
	if rec := s.rec.Load(); rec != nil {
		return rec.status(time.Now())
	}
	return RecordingStatus{}
}

// Close hangs up the terminal, which makes the shell and its jobs exit.
func (s *Session) Close() {
	s.closed.Store(true)
	s.closeOnce.Do(func() {
		if s.cmd.Process != nil {
			// The program in the foreground (a dev server, tail -f) is in a
			// group of its own, and on macOS the hang-up reaches only the
			// shell, which may not pass it on while it waits: signal that
			// group directly, as Linux does. SIGCONT wakes a stopped job so
			// it can act on it.
			if fg, err := s.foreground(); err == nil && fg > 1 && fg != s.cmd.Process.Pid {
				_ = syscall.Kill(-fg, syscall.SIGHUP)
				_ = syscall.Kill(-fg, syscall.SIGCONT)
			}
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

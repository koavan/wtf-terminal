// Command wtf-runner runs on a remote Linux machine (a "runner", #41) and
// holds the Claude Code agents wtf starts there. wtf installs it, with
// wtf-mcp (the hook helper) and the status hooks, into
// ~/.local/share/wtf-runner over SSH.
//
//	wtf-runner serve     the daemon (internal/runnerd); attach starts it
//	wtf-runner attach    wtf's stream: stdin/stdout ↔ the daemon
//	wtf-runner stop      asks the daemon to exit (agents keep running)
//	wtf-runner --version
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/dayanraj/wtf/internal/runnerd"
)

// version is set at build time (-X main.version=…).
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	base := baseDir()
	switch os.Args[1] {
	case "--version", "version":
		fmt.Printf("%s protocol=%s\n", version, runnerd.Protocol)
	case "serve":
		f, err := os.OpenFile(filepath.Join(base, runnerd.RunDir, runnerd.LogFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err == nil {
			log.SetOutput(f)
		}
		d := &runnerd.Daemon{Base: base, Version: version}
		if err := d.Serve(); err != nil {
			log.Fatal(err)
		}
	case "attach":
		if err := attach(base); err != nil {
			fmt.Fprintln(os.Stderr, "wtf-runner:", err)
			os.Exit(1)
		}
	case "stop":
		if err := stop(base); err != nil {
			fmt.Fprintln(os.Stderr, "wtf-runner:", err)
			os.Exit(1)
		}
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: wtf-runner serve | attach | stop | --version")
	os.Exit(2)
}

// baseDir is the folder this program is in (~/.local/share/wtf-runner).
func baseDir() string {
	exe, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Dir(exe)
}

func sock(base string) string { return filepath.Join(base, runnerd.RunDir, runnerd.ControlSock) }

// dial reaches the daemon, starting it first if it isn't running.
func dial(base string, start bool) (net.Conn, error) {
	if c, err := net.Dial("unix", sock(base)); err == nil || !start {
		return c, err
	}
	if err := os.MkdirAll(filepath.Join(base, runnerd.RunDir), 0o700); err != nil {
		return nil, err
	}
	exe, _ := os.Executable()
	logf, err := os.OpenFile(filepath.Join(base, runnerd.RunDir, runnerd.LogFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, "serve")
	cmd.Stdout, cmd.Stderr = logf, logf
	// Its own session: it outlives this ssh connection.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Dir = base
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go cmd.Wait()
	for range 50 {
		time.Sleep(100 * time.Millisecond)
		if c, err := net.Dial("unix", sock(base)); err == nil {
			return c, nil
		}
	}
	return nil, fmt.Errorf("the daemon didn't start (see %s)", filepath.Join(base, runnerd.RunDir, runnerd.LogFile))
}

// attach copies stdin to the daemon and the daemon to stdout until either
// side ends.
func attach(base string) error {
	c, err := dial(base, true)
	if err != nil {
		return err
	}
	defer c.Close()
	done := make(chan error, 2)
	go func() {
		_, err := io.Copy(os.Stdout, c)
		done <- err
	}()
	go func() {
		_, err := io.Copy(c, os.Stdin)
		done <- err
	}()
	return <-done
}

func stop(base string) error {
	c, err := dial(base, false)
	if err != nil {
		return nil // not running
	}
	defer c.Close()
	b, _ := json.Marshal(runnerd.Command{ID: 1, Op: runnerd.OpQuit})
	if _, err := c.Write(append(b, '\n')); err != nil {
		return err
	}
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(c)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return nil
		}
		var m runnerd.Message
		if json.Unmarshal(line, &m) == nil && m.Type == "reply" && m.ID == 1 {
			if m.Error != "" {
				return fmt.Errorf("%s", m.Error)
			}
			return nil
		}
	}
}

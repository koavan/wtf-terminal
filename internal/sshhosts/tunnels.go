package sshhosts

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// SSH tunnels: saved port forwards (ssh -L, -R, -D), each run as its own
// background `ssh -N` while switched on. They stop when wtf quits, can
// start when it opens, and reconnect if a working connection drops.
//
// ssh runs with BatchMode, so it never waits for a password or passphrase
// that no one could type: keys must be in the agent, or passphrase-free.
// It also gets its own connection (no ControlMaster sharing), so stopping
// a tunnel never affects the user's sessions.

type Tunnel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// The host as given to ssh (a config alias or an address), and a port
	// for hosts that aren't in the config.
	Host string `json:"host"`
	Port string `json:"port"`
	// local (-L), remote (-R) or dynamic (-D, a SOCKS proxy) for ssh;
	// kubectl for a kubectl port-forward.
	Kind string `json:"kind"`
	// ssh's forward spec: "8080:localhost:80", or "1080" for dynamic;
	// for kubectl, "LOCAL:REMOTE" ports.
	Spec      string `json:"spec"`
	AutoStart bool   `json:"autoStart"`

	// kubectl only: where and what to forward to (pod/x, svc/x, deploy/x),
	// and the variables of the shell it was made in (KUBECONFIG, the AWS
	// profile, PATH for login helpers), so it reaches the same cluster later.
	Context   string            `json:"context,omitempty"`
	Namespace string            `json:"namespace,omitempty"`
	Resource  string            `json:"resource,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

// Tunnel states.
const (
	TunnelStopped    = "stopped"
	TunnelConnecting = "connecting"
	TunnelUp         = "up"
	// Lost its connection and is trying again.
	TunnelRetrying = "retrying"
	// Never came up; stays off until started again.
	TunnelFailed = "failed"
)

type TunnelStatus struct {
	Tunnel
	State string `json:"state"`
	Error string `json:"error"`
	// When the state last changed (Unix ms).
	Since int64 `json:"since"`
	// The ssh command line, to show or run by hand.
	Command string `json:"command"`
}

// EventTunnels carries []TunnelStatus whenever a tunnel changes.
const EventTunnels = "ssh:tunnels"

const (
	upMarker = "wtf-tunnel-up"
	// Without the marker (LocalCommand disabled somehow), a tunnel that is
	// still running this long after starting has connected.
	upFallback   = 12 * time.Second
	stopTimeout  = 3 * time.Second
	maxErrorLine = 300
)

var (
	retryDelays = []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute}

	portRe    = `([1-9][0-9]{0,4})`
	hostRe    = `(\[[0-9A-Fa-f:.]+\]|[A-Za-z0-9_.*-]+)`
	forwardRe = regexp.MustCompile(`^(` + hostRe + `:)?` + portRe + `:` + hostRe + `:` + portRe + `$`)
	dynamicRe = regexp.MustCompile(`^(` + hostRe + `:)?` + portRe + `$`)
)

// tunnelCommand builds the process for a tunnel; tests replace it.
var tunnelCommand = func(t Tunnel, args []string) *exec.Cmd {
	if t.Kind == "kubectl" {
		cmd := exec.Command(findKubectl(t.Env["PATH"]), args...)
		cmd.Env = kubectlEnv(t.Env)
		return cmd
	}
	return exec.Command(sshTool("ssh"), args...)
}

// Variables a kubectl forward takes from the shell it was made in.
var kubectlVars = map[string]bool{"PATH": true, "KUBECONFIG": true, "AWS_PROFILE": true, "AWS_REGION": true, "AWS_DEFAULT_REGION": true}

// kubectlEnv is wtf's environment with the forward's shell variables in
// place of its own (and no stray AWS keys or kubeconfig of wtf's).
func kubectlEnv(saved map[string]string) []string {
	var out []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if _, ok := saved[name]; ok || name == "KUBECONFIG" || strings.HasPrefix(name, "AWS_") {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range saved {
		if kubectlVars[k] {
			out = append(out, k+"="+v)
		}
	}
	return out
}

// findKubectl looks on the saved PATH first, then wtf's.
func findKubectl(path string) string {
	for _, dir := range filepath.SplitList(path) {
		p := filepath.Join(dir, "kubectl")
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p
		}
	}
	if p, err := exec.LookPath("kubectl"); err == nil {
		return p
	}
	return "kubectl"
}

// kubectl says this when the forward is listening.
const kubectlUp = "Forwarding from "

type tunnelManager struct {
	mu      sync.Mutex
	file    string
	order   []string
	tunnels map[string]*runningTunnel
	changed func([]TunnelStatus)
	closed  bool
}

type runningTunnel struct {
	Tunnel
	state string
	err   string
	since time.Time
	// Wanted on: the user started it and hasn't stopped it.
	on  bool
	cmd *exec.Cmd
	// Closed when the current ssh has ended.
	done  chan struct{}
	retry *time.Timer
	tries int
	// Bumped on every start/stop, so a finished process from an earlier
	// run can tell it's stale.
	gen int
}

func newTunnelManager(file string, changed func([]TunnelStatus)) *tunnelManager {
	m := &tunnelManager{file: file, tunnels: map[string]*runningTunnel{}, changed: changed}
	m.load()
	return m
}

func (m *tunnelManager) load() {
	if m.file == "" {
		return
	}
	data, err := os.ReadFile(m.file)
	if err != nil {
		return
	}
	var saved struct {
		Tunnels []Tunnel `json:"tunnels"`
	}
	if json.Unmarshal(data, &saved) != nil {
		return
	}
	for _, t := range saved.Tunnels {
		if t.ID == "" || m.tunnels[t.ID] != nil || validateTunnel(&t) != nil {
			continue
		}
		m.order = append(m.order, t.ID)
		m.tunnels[t.ID] = &runningTunnel{Tunnel: t, state: TunnelStopped, since: time.Now()}
	}
}

// save writes the tunnel definitions. Called with mu held.
func (m *tunnelManager) save() error {
	if m.file == "" {
		return nil
	}
	saved := struct {
		Tunnels []Tunnel `json:"tunnels"`
	}{Tunnels: []Tunnel{}}
	for _, id := range m.order {
		saved.Tunnels = append(saved.Tunnels, m.tunnels[id].Tunnel)
	}
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.file), 0o700); err != nil {
		return err
	}
	tmp := m.file + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.file)
}

// list is the current state of every tunnel. Called with mu held.
func (m *tunnelManager) list() []TunnelStatus {
	out := make([]TunnelStatus, 0, len(m.order))
	for _, id := range m.order {
		t := m.tunnels[id]
		out = append(out, TunnelStatus{
			Tunnel:  t.Tunnel,
			State:   t.state,
			Error:   t.err,
			Since:   t.since.UnixMilli(),
			Command: displayCommand(t.Tunnel),
		})
	}
	return out
}

// notify reports a change. Called with mu held; the callback runs outside.
func (m *tunnelManager) notify() {
	if m.changed == nil {
		return
	}
	list := m.list()
	go m.changed(list)
}

func (m *tunnelManager) setState(t *runningTunnel, state, err string) {
	t.state, t.err, t.since = state, err, time.Now()
	m.notify()
}

func (m *tunnelManager) Save(in Tunnel) ([]TunnelStatus, error) {
	if err := validateTunnel(&in); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tunnels[in.ID]
	restart := false
	if t == nil {
		in.ID = newID()
		t = &runningTunnel{Tunnel: in, state: TunnelStopped, since: time.Now()}
		m.tunnels[in.ID] = t
		m.order = append(m.order, in.ID)
	} else {
		restart = t.on && (t.Host != in.Host || t.Port != in.Port || t.Kind != in.Kind || t.Spec != in.Spec ||
			t.Context != in.Context || t.Namespace != in.Namespace || t.Resource != in.Resource)
		t.Tunnel = in
	}
	if err := m.save(); err != nil {
		return nil, err
	}
	if restart {
		m.stop(t)
		m.start(t)
	}
	m.notify()
	return m.list(), nil
}

func (m *tunnelManager) Delete(id string) ([]TunnelStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tunnels[id]
	if t == nil {
		return m.list(), nil
	}
	m.stop(t)
	delete(m.tunnels, id)
	for i, x := range m.order {
		if x == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	if err := m.save(); err != nil {
		return nil, err
	}
	m.notify()
	return m.list(), nil
}

func (m *tunnelManager) Start(id string) ([]TunnelStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tunnels[id]
	if t == nil {
		return nil, errors.New("no such tunnel")
	}
	if !t.on {
		t.tries = 0
		m.start(t)
	}
	return m.list(), nil
}

func (m *tunnelManager) Stop(id string) ([]TunnelStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t := m.tunnels[id]; t != nil {
		m.stop(t)
		m.setState(t, TunnelStopped, "")
	}
	return m.list(), nil
}

func (m *tunnelManager) List() []TunnelStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.list()
}

// StartAuto starts the tunnels marked to start with wtf.
func (m *tunnelManager) StartAuto() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range m.order {
		if t := m.tunnels[id]; t.AutoStart && !t.on {
			m.start(t)
		}
	}
}

// Close stops every tunnel, for when wtf quits.
func (m *tunnelManager) Close() {
	m.mu.Lock()
	m.closed = true
	var running []chan struct{}
	for _, t := range m.tunnels {
		if t.cmd != nil {
			running = append(running, t.done)
		}
		m.stop(t)
	}
	m.mu.Unlock()
	deadline := time.After(stopTimeout)
	for _, done := range running {
		select {
		case <-done:
		case <-deadline:
			return
		}
	}
}

// start launches a tunnel's ssh. Called with mu held.
func (m *tunnelManager) start(t *runningTunnel) {
	if m.closed {
		return
	}
	t.on = true
	t.gen++
	gen := t.gen
	if t.retry != nil {
		t.retry.Stop()
		t.retry = nil
	}
	cmd := tunnelCommand(t.Tunnel, tunnelArgs(t.Tunnel, true))
	cmd.Stdin = nil
	// Its own process group, so a terminal's Ctrl-C never reaches it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	state := TunnelConnecting
	if t.state == TunnelRetrying {
		state = TunnelRetrying
	}
	if err := cmd.Start(); err != nil {
		t.on = false
		m.setState(t, TunnelFailed, err.Error())
		return
	}
	t.cmd = cmd
	done := make(chan struct{})
	t.done = done
	m.setState(t, state, t.err)

	lastErr := make(chan string, 1)
	go func() {
		lastErr <- lastErrorLine(stderr)
	}()
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); line == upMarker || strings.HasPrefix(line, kubectlUp) {
				m.markUp(t, gen)
			}
		}
	}()
	fallback := time.AfterFunc(upFallback, func() { m.markUp(t, gen) })
	go func() {
		err := cmd.Wait()
		close(done)
		fallback.Stop()
		m.exited(t, gen, <-lastErr, err)
	}()
}

func (m *tunnelManager) markUp(t *runningTunnel, gen int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.gen != gen || !t.on || t.state == TunnelUp {
		return
	}
	t.tries = 0
	m.setState(t, TunnelUp, "")
}

// exited handles a tunnel's ssh ending: on purpose, before it ever came
// up (failed), or after (retry).
func (m *tunnelManager) exited(t *runningTunnel, gen int, lastErr string, waitErr error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.gen != gen {
		return
	}
	t.cmd = nil
	if !t.on || m.closed {
		return
	}
	msg := lastErr
	if msg == "" && waitErr != nil {
		msg = "ssh " + waitErr.Error()
	}
	if msg == "" {
		msg = "ssh exited"
	}
	if t.state == TunnelConnecting {
		t.on = false
		m.setState(t, TunnelFailed, msg)
		return
	}
	delay := retryDelays[min(t.tries, len(retryDelays)-1)]
	t.tries++
	m.setState(t, TunnelRetrying, msg)
	t.retry = time.AfterFunc(delay, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if t.gen == gen && t.on && t.cmd == nil {
			m.start(t)
		}
	})
}

// stop ends a tunnel's ssh, if running. Called with mu held.
func (m *tunnelManager) stop(t *runningTunnel) {
	t.on = false
	t.gen++
	if t.retry != nil {
		t.retry.Stop()
		t.retry = nil
	}
	if t.cmd != nil && t.cmd.Process != nil {
		p := t.cmd.Process
		_ = p.Signal(syscall.SIGTERM)
		go func() {
			time.Sleep(stopTimeout)
			_ = p.Kill() // no-op if it already ended
		}()
	}
	t.cmd = nil
}

// lastErrorLine reads ssh's stderr to the end and returns the last line
// worth showing.
func lastErrorLine(r io.Reader) string {
	last := ""
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "Warning: Permanently added") {
			continue
		}
		if len(line) > maxErrorLine {
			line = line[:maxErrorLine] + "…"
		}
		last = line
	}
	return last
}

// tunnelArgs are ssh's arguments for a tunnel. The managed form adds what
// wtf needs to run it unattended; the plain one is for showing the user.
func tunnelArgs(t Tunnel, managed bool) []string {
	if t.Kind == "kubectl" {
		return []string{"--context", t.Context, "--namespace", t.Namespace, "port-forward", "--address", "127.0.0.1", t.Resource, t.Spec}
	}
	flag := map[string]string{"local": "-L", "remote": "-R", "dynamic": "-D"}[t.Kind]
	args := []string{"-N"}
	if managed {
		args = append(args,
			"-o", "BatchMode=yes",
			"-o", "ExitOnForwardFailure=yes",
			"-o", "ServerAliveInterval=15",
			"-o", "ServerAliveCountMax=3",
			"-o", "ConnectTimeout=10",
			"-o", "ControlMaster=no",
			"-o", "ControlPath=none",
			"-o", "PermitLocalCommand=yes",
			"-o", "LocalCommand=echo "+upMarker,
		)
	}
	if t.Port != "" {
		args = append(args, "-p", t.Port)
	}
	args = append(args, flag, t.Spec)
	if managed {
		args = append(args, "--")
	}
	return append(args, t.Host)
}

// displayCommand is the tunnel as a command to show or run by hand.
func displayCommand(t Tunnel) string {
	if t.Kind == "kubectl" {
		prefix := ""
		if k := t.Env["KUBECONFIG"]; k != "" {
			prefix = "KUBECONFIG=" + quoteArgs([]string{k})[0] + " "
		}
		if p := t.Env["AWS_PROFILE"]; p != "" {
			prefix += "AWS_PROFILE=" + quoteArgs([]string{p})[0] + " "
		}
		return prefix + "kubectl " + strings.Join(quoteArgs(tunnelArgs(t, false)), " ")
	}
	return "ssh " + strings.Join(quoteArgs(tunnelArgs(t, false)), " ")
}

func quoteArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if a != "" && strings.Trim(a, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.:/@=[]") == "" {
			out[i] = a
		} else {
			out[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return out
}

func validateTunnel(t *Tunnel) error {
	t.Name = strings.TrimSpace(t.Name)
	t.Host = strings.TrimSpace(t.Host)
	t.Port = strings.TrimSpace(t.Port)
	t.Spec = strings.TrimSpace(t.Spec)
	if t.Kind == "kubectl" {
		return validateKubectl(t)
	}
	switch {
	case t.Host == "":
		return errors.New("choose a host to connect through")
	case strings.HasPrefix(t.Host, "-") || strings.ContainsAny(t.Host, " \t\r\n'\"`$;|&<>"):
		return fmt.Errorf("%q isn't a host name", t.Host)
	}
	if t.Port != "" {
		if n, err := strconv.Atoi(t.Port); err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("port %q must be a number from 1 to 65535", t.Port)
		}
	}
	var m []string
	switch t.Kind {
	case "local", "remote":
		m = forwardRe.FindStringSubmatch(t.Spec)
		if m == nil {
			return fmt.Errorf("%q should look like 8080:localhost:80", t.Spec)
		}
		m = []string{m[3], m[5]}
	case "dynamic":
		m = dynamicRe.FindStringSubmatch(t.Spec)
		if m == nil {
			return fmt.Errorf("%q should be a port, like 1080", t.Spec)
		}
		m = []string{m[3]}
	default:
		return fmt.Errorf("unknown tunnel kind %q", t.Kind)
	}
	for _, p := range m {
		if n, _ := strconv.Atoi(p); n > 65535 {
			return fmt.Errorf("port %s is above 65535", p)
		}
	}
	return nil
}

var (
	kubeResource = regexp.MustCompile(`^(pod|pods|po|svc|service|services|deploy|deployment|deployments|sts|statefulset|statefulsets|rs|replicaset)/[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
	kubePorts    = regexp.MustCompile(`^([1-9][0-9]{0,4})(:([1-9][0-9]{0,4}))?$`)
	kubeName     = regexp.MustCompile(`^[^\s-][^\s]*$`)
)

func validateKubectl(t *Tunnel) error {
	t.Host, t.Port = "", ""
	t.Context, t.Namespace, t.Resource = strings.TrimSpace(t.Context), strings.TrimSpace(t.Namespace), strings.TrimSpace(t.Resource)
	switch {
	case !kubeName.MatchString(t.Context):
		return fmt.Errorf("%q isn't a context name", t.Context)
	case !kubeName.MatchString(t.Namespace):
		return fmt.Errorf("%q isn't a namespace", t.Namespace)
	case !kubeResource.MatchString(t.Resource):
		return fmt.Errorf("%q should look like svc/web, deploy/api or pod/name", t.Resource)
	}
	m := kubePorts.FindStringSubmatch(t.Spec)
	if m == nil {
		return fmt.Errorf("%q should be LOCAL:REMOTE ports, like 8080:80", t.Spec)
	}
	for _, p := range []string{m[1], m[3]} {
		if n, _ := strconv.Atoi(p); n > 65535 {
			return fmt.Errorf("port %s is above 65535", p)
		}
	}
	for k, v := range t.Env {
		if !kubectlVars[k] || strings.ContainsAny(v, "\x00\n") {
			delete(t.Env, k)
		}
	}
	return nil
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ── Service methods ──────────────────────────────────────────────────────

func (s *Service) Tunnels() []TunnelStatus { return s.tunnels.List() }

// SaveTunnel adds a tunnel (empty ID) or changes one; a running tunnel
// whose connection changed restarts.
func (s *Service) SaveTunnel(t Tunnel) ([]TunnelStatus, error) { return s.tunnels.Save(t) }

func (s *Service) DeleteTunnel(id string) ([]TunnelStatus, error) { return s.tunnels.Delete(id) }

func (s *Service) StartTunnel(id string) ([]TunnelStatus, error) { return s.tunnels.Start(id) }

func (s *Service) StopTunnel(id string) ([]TunnelStatus, error) { return s.tunnels.Stop(id) }

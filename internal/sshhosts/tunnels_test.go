package sshhosts

import (
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSSH replaces ssh with a shell script; each start runs the next
// script (the last one repeats). It records the arguments it was given.
func fakeSSH(t *testing.T, scripts ...string) *[][]string {
	t.Helper()
	var mu sync.Mutex
	var calls [][]string
	old, oldDelays := tunnelCommand, retryDelays
	tunnelCommand = func(_ Tunnel, args []string) *exec.Cmd {
		mu.Lock()
		defer mu.Unlock()
		script := scripts[min(len(calls), len(scripts)-1)]
		calls = append(calls, args)
		return exec.Command("sh", "-c", script)
	}
	retryDelays = []time.Duration{50 * time.Millisecond}
	t.Cleanup(func() { tunnelCommand, retryDelays = old, oldDelays })
	return &calls
}

func waitState(t *testing.T, m *tunnelManager, id, want string) TunnelStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		for _, s := range m.List() {
			if s.ID == id && s.State == want {
				return s
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("tunnel never reached %s: %+v", want, m.List())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTunnelLifecycle(t *testing.T) {
	calls := fakeSSH(t, "echo "+upMarker+"; exec sleep 30")
	file := filepath.Join(t.TempDir(), "tunnels.json")
	m := newTunnelManager(file, nil)
	defer m.Close()

	list, err := m.Save(Tunnel{Name: "db", Host: "bastion", Kind: "local", Spec: "5432:db.internal:5432"})
	if err != nil {
		t.Fatal(err)
	}
	id := list[0].ID
	if list[0].Command != "ssh -N -L 5432:db.internal:5432 bastion" {
		t.Errorf("command %q", list[0].Command)
	}
	if _, err := m.Start(id); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, id, TunnelUp)
	args := strings.Join((*calls)[0], " ")
	for _, want := range []string{"-N", "BatchMode=yes", "ExitOnForwardFailure=yes", "ControlMaster=no", "-L 5432:db.internal:5432 -- bastion"} {
		if !strings.Contains(args, want) {
			t.Errorf("args %q lack %q", args, want)
		}
	}

	m.Stop(id)
	waitState(t, m, id, TunnelStopped)

	// Saved definitions come back in a new manager, stopped.
	m2 := newTunnelManager(file, nil)
	if l := m2.List(); len(l) != 1 || l[0].Spec != "5432:db.internal:5432" || l[0].State != TunnelStopped {
		t.Errorf("reloaded %+v", l)
	}
	if l, _ := m2.Delete(id); len(l) != 0 {
		t.Errorf("after delete %+v", l)
	}
	if l := newTunnelManager(file, nil).List(); len(l) != 0 {
		t.Errorf("delete wasn't saved: %+v", l)
	}
}

func TestTunnelFailsBeforeUp(t *testing.T) {
	fakeSSH(t, "echo 'bind [127.0.0.1]:8080: Address already in use' >&2; echo 'Could not request local forwarding.' >&2; exit 255")
	m := newTunnelManager("", nil)
	defer m.Close()
	list, _ := m.Save(Tunnel{Host: "web", Kind: "local", Spec: "8080:localhost:80"})
	m.Start(list[0].ID)
	s := waitState(t, m, list[0].ID, TunnelFailed)
	if s.Error != "Could not request local forwarding." {
		t.Errorf("error %q", s.Error)
	}
}

func TestTunnelReconnects(t *testing.T) {
	// Comes up, drops, then comes up again on the retry.
	calls := fakeSSH(t, "echo "+upMarker+"; sleep 0.2; echo 'Connection to web closed by remote host.' >&2; exit 255", "echo "+upMarker+"; exec sleep 30")
	var mu sync.Mutex
	var states []string
	m := newTunnelManager("", func(l []TunnelStatus) {
		mu.Lock()
		states = append(states, l[0].State)
		mu.Unlock()
	})
	defer m.Close()
	list, _ := m.Save(Tunnel{Host: "web", Kind: "dynamic", Spec: "1080", AutoStart: true})
	m.StartAuto()
	waitState(t, m, list[0].ID, TunnelRetrying)
	waitState(t, m, list[0].ID, TunnelUp)
	if len(*calls) != 2 {
		t.Errorf("ssh started %d times", len(*calls))
	}
	time.Sleep(50 * time.Millisecond) // let the callbacks land
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(strings.Join(states, ","), "up,retrying") {
		t.Errorf("states %v", states)
	}
}

func TestValidateTunnel(t *testing.T) {
	ok := []Tunnel{
		{Host: "web", Kind: "local", Spec: "8080:localhost:80"},
		{Host: "web", Kind: "local", Spec: "0.0.0.0:8080:10.0.0.5:80"},
		{Host: "web", Kind: "remote", Spec: "9000:localhost:3000"},
		{Host: "web", Kind: "local", Spec: "[::1]:8080:[fe80::1]:80"},
		{Host: "10.0.0.5", Port: "2222", Kind: "dynamic", Spec: "1080"},
	}
	for _, tt := range ok {
		if err := validateTunnel(&tt); err != nil {
			t.Errorf("%+v: %v", tt, err)
		}
	}
	bad := []Tunnel{
		{Host: "", Kind: "local", Spec: "8080:localhost:80"},
		{Host: "-oProxyCommand=x", Kind: "local", Spec: "8080:localhost:80"},
		{Host: "web; rm", Kind: "local", Spec: "8080:localhost:80"},
		{Host: "web", Kind: "local", Spec: "8080"},
		{Host: "web", Kind: "local", Spec: "99999:localhost:80"},
		{Host: "web", Kind: "dynamic", Spec: "localhost"},
		{Host: "web", Kind: "tcp", Spec: "1080"},
		{Host: "web", Port: "x", Kind: "dynamic", Spec: "1080"},
	}
	for _, tt := range bad {
		if err := validateTunnel(&tt); err == nil {
			t.Errorf("%+v should be refused", tt)
		}
	}
}

func TestKubectlForward(t *testing.T) {
	// Up when kubectl says so; the pod goes away; reconnects.
	calls := fakeSSH(t,
		"echo 'Forwarding from 127.0.0.1:15432 -> 5432'; sleep 0.2; echo 'error: lost connection to pod' >&2; exit 1",
		"echo 'Forwarding from 127.0.0.1:15432 -> 5432'; exec sleep 30")
	m := newTunnelManager("", nil)
	defer m.Close()
	in := Tunnel{
		Name: "staging db", Kind: "kubectl", Context: "staging", Namespace: "data", Resource: "svc/postgres", Spec: "15432:5432",
		Env: map[string]string{"KUBECONFIG": "/home/me/.kube/staging.yaml", "AWS_PROFILE": "devops", "PATH": "/opt/bin", "AWS_SECRET_ACCESS_KEY": "nope"},
	}
	list, err := m.Save(in)
	if err != nil {
		t.Fatal(err)
	}
	st := list[0]
	if st.Command != "KUBECONFIG=/home/me/.kube/staging.yaml AWS_PROFILE=devops kubectl --context staging --namespace data port-forward --address 127.0.0.1 svc/postgres 15432:5432" {
		t.Errorf("command %q", st.Command)
	}
	if _, kept := st.Env["AWS_SECRET_ACCESS_KEY"]; kept || st.Env["PATH"] != "/opt/bin" {
		t.Errorf("env %v: only the shell variables a forward needs", st.Env)
	}
	m.Start(st.ID)
	waitState(t, m, st.ID, TunnelUp)
	waitState(t, m, st.ID, TunnelRetrying)
	waitState(t, m, st.ID, TunnelUp)
	if got := strings.Join((*calls)[0], " "); got != "--context staging --namespace data port-forward --address 127.0.0.1 svc/postgres 15432:5432" {
		t.Errorf("args %q", got)
	}
}

func TestKubectlEnv(t *testing.T) {
	t.Setenv("AWS_SECRET_ACCESS_KEY", "app-secret")
	t.Setenv("KUBECONFIG", "/wtf/own")
	env := strings.Join(kubectlEnv(map[string]string{"KUBECONFIG": "/k", "AWS_PROFILE": "p", "PATH": "/opt/bin"}), "\n")
	for _, want := range []string{"KUBECONFIG=/k", "AWS_PROFILE=p", "PATH=/opt/bin"} {
		if !strings.Contains(env, want) {
			t.Errorf("env lacks %s", want)
		}
	}
	if strings.Contains(env, "app-secret") || strings.Contains(env, "/wtf/own") {
		t.Error("wtf's own AWS keys and kubeconfig must not reach the forward")
	}
}

func TestValidateKubectl(t *testing.T) {
	ok := Tunnel{Kind: "kubectl", Context: "arn:aws:eks:us-east-1:1:cluster/prod", Namespace: "shop", Resource: "deploy/web", Spec: "8080:80", Host: "ignored"}
	if err := validateTunnel(&ok); err != nil || ok.Host != "" {
		t.Errorf("%+v: %v", ok, err)
	}
	same := Tunnel{Kind: "kubectl", Context: "c", Namespace: "n", Resource: "pod/web-1", Spec: "8080"}
	if err := validateTunnel(&same); err != nil {
		t.Errorf("single port: %v", err)
	}
	for _, bad := range []Tunnel{
		{Kind: "kubectl", Context: "-x", Namespace: "n", Resource: "svc/a", Spec: "1:2"},
		{Kind: "kubectl", Context: "c", Namespace: "a b", Resource: "svc/a", Spec: "1:2"},
		{Kind: "kubectl", Context: "c", Namespace: "n", Resource: "secret/a", Spec: "1:2"},
		{Kind: "kubectl", Context: "c", Namespace: "n", Resource: "svc/a", Spec: "99999:2"},
		{Kind: "kubectl", Context: "c", Namespace: "n", Resource: "svc/a", Spec: "a:b"},
	} {
		if err := validateTunnel(&bad); err == nil {
			t.Errorf("%+v should be refused", bad)
		}
	}
}

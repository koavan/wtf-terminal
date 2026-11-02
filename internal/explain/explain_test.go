package explain

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dayanraj/wtf/internal/infra"
)

func TestPrograms(t *testing.T) {
	cases := map[string][]string{
		"kubectl get pods -n api":                          {"kubectl"},
		"AWS_PROFILE=dev aws s3 ls | grep logs":            {"aws"},
		"sudo terraform apply && kubectl rollout status x": {"terraform", "kubectl"},
		"./deploy.sh --version":                            nil,
		"time env FOO=1 helm upgrade x ./chart":            {"helm"},
		"echo hi; git push":                                {"git"},
		"kubectl get po; kubectl logs x":                   {"kubectl"},
		"mytool --flag | jq .":                             {"jq"},
	}
	for cmd, want := range cases {
		if got := programs(cmd); !slices.Equal(got, want) {
			t.Errorf("programs(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestExitMeaning(t *testing.T) {
	for code, want := range map[int]string{
		1:   "1",
		127: "127 (the shell's code for command not found)",
		137: "137 (128 + 9: ended by signal 9, SIGKILL)",
		130: "130 (128 + 2: ended by signal 2, SIGINT)",
	} {
		if got := exitMeaning(code); got != want {
			t.Errorf("exitMeaning(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestTail(t *testing.T) {
	var lines []string
	for i := range 400 {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	out, dropped := tail(strings.Join(lines, "\n") + "\n\n")
	if dropped != 250 || !strings.HasPrefix(out, "line 250\n") || !strings.HasSuffix(out, "line 399") {
		t.Fatalf("tail: dropped %d, starts %q", dropped, out[:20])
	}
	long := strings.Repeat("x", 30000)
	out, dropped = tail("first\n" + long)
	if dropped != 1 || len(out) > tailBytes+len("…") || !strings.HasPrefix(out, "…") {
		t.Fatalf("one long line: dropped %d, %d bytes", dropped, len(out))
	}
}

func TestParse(t *testing.T) {
	r := parse("The namespace `api` doesn't exist in context dev.\n\n```sh\n$ kubectl get namespaces\n```\n")
	if r.Fix != "kubectl get namespaces" || r.Cause != "The namespace `api` doesn't exist in context dev." {
		t.Fatalf("parse: %+v", r)
	}
	r = parse("Out of memory: the pod was killed.")
	if r.Fix != "" || r.Cause != "Out of memory: the pod was killed." {
		t.Fatalf("no block: %+v", r)
	}
	// Control characters can't reach the terminal.
	r = parse("x\n```sh\nls\x1b[201~\r; rm\tx\n```")
	if r.Fix != "ls[201~; rm x" {
		t.Fatalf("controls: %q", r.Fix)
	}
	// The last block is the fix.
	r = parse("It printed\n```\nError: x\n```\nso run\n```bash\nterraform init -upgrade\n```")
	if r.Fix != "terraform init -upgrade" || !strings.Contains(r.Cause, "Error: x") {
		t.Fatalf("two blocks: %+v", r)
	}
}

func TestRequest(t *testing.T) {
	home := t.TempDir()
	kubeconfig := filepath.Join(home, "kubeconfig")
	os.WriteFile(kubeconfig, []byte(`
current-context: dev
contexts:
- name: dev
  context: {cluster: dev-cluster, namespace: api, user: me}
`), 0o600)
	tool := filepath.Join(home, "bin", "kubectl")
	os.MkdirAll(filepath.Dir(tool), 0o755)
	os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755)

	s := NewService(infra.NewService())
	var asked []string
	s.version = func(dir string, env []string, path string, args []string) string {
		asked = append(asked, path+" "+strings.Join(args, " "))
		return "Client Version: v1.31.2"[len("Client Version: "):]
	}
	r := s.Request(Failure{
		Command: "kubectl get pods", ExitCode: 1, DurationMs: 1234,
		Output: "Error from server (NotFound): namespaces \"api\" not found\n",
		Env:    map[string]string{"PWD": home, "PATH": filepath.Join(home, "bin"), "KUBECONFIG": kubeconfig, "SHELL": "/bin/zsh"},
	})
	if r.System == "" || len(r.Messages) != 1 || r.Env["KUBECONFIG"] != kubeconfig {
		t.Fatalf("request: %+v", r)
	}
	msg := r.Messages[0].Content
	for _, want := range []string{
		"Command: kubectl get pods\n", "Exit code: 1\n", "Ran for: 1.2 s\n",
		"Kubernetes: context dev, cluster dev-cluster, namespace api\n",
		"  kubectl: v1.31.2\n", "Output:\nError from server (NotFound)",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("facts lack %q:\n%s", want, msg)
		}
	}
	if len(asked) != 1 || asked[0] != tool+" version --client" {
		t.Errorf("asked %v", asked)
	}

	// A program that's not on PATH is said to be missing, and not run.
	asked = nil
	r = s.Request(Failure{Command: "helm list", ExitCode: 127, Env: map[string]string{"PATH": filepath.Join(home, "bin")}})
	if msg := r.Messages[0].Content; !strings.Contains(msg, "helm: not found on the shell's PATH") || !strings.Contains(msg, "Output: none") {
		t.Errorf("missing tool:\n%s", msg)
	}
	if len(asked) != 0 {
		t.Errorf("asked %v", asked)
	}
}

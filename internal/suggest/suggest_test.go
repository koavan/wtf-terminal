package suggest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dayanraj/wtf/internal/infra"
)

func TestRequest(t *testing.T) {
	home := t.TempDir()
	kubeconfig := filepath.Join(home, "kubeconfig")
	os.WriteFile(kubeconfig, []byte(`
current-context: dev
contexts:
- name: dev
  context: {cluster: dev-cluster, namespace: api, user: me}
`), 0o600)
	bin := filepath.Join(home, "bin")
	os.MkdirAll(bin, 0o755)
	for _, name := range []string{"kubectl", "jq", "my-script"} {
		os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755)
	}
	s := NewService(infra.NewService())

	if _, err := s.Request(Ask{Text: "  "}); err == nil {
		t.Fatal("an empty request was accepted")
	}
	r, err := s.Request(Ask{
		Text: " delete evicted pods in all namespaces ",
		Env:  map[string]string{"PWD": home, "PATH": bin, "KUBECONFIG": kubeconfig, "SHELL": "/bin/zsh"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Fast || r.System == "" || len(r.Messages) != 1 || r.Env["PATH"] != bin {
		t.Fatalf("request: %+v", r)
	}
	msg := r.Messages[0].Content
	for _, want := range []string{
		"Kubernetes: context dev, cluster dev-cluster, namespace api\n",
		"\nRequest: delete evicted pods in all namespaces\n",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("facts lack %q:\n%s", want, msg)
		}
	}

	// Well-known tools only (tools.Find also looks in the usual install folders).
	_, installed, _ := strings.Cut(msg, "Installed: ")
	installed, _, _ = strings.Cut(installed, "\n")
	if !strings.Contains(", "+installed+",", ", jq,") || !strings.Contains(installed, "kubectl") || strings.Contains(installed, "my-script") {
		t.Errorf("installed: %q", installed)
	}

	f := s.FlagsRequest(r, "```sh\nkubectl get pods -A\n```\nLists pods.")
	if len(f.Messages) != 3 || f.Messages[1].Role != "assistant" || f.Messages[2].Role != "user" || len(r.Messages) != 1 {
		t.Fatalf("flags request: %+v", f.Messages)
	}
}

func TestParse(t *testing.T) {
	s := NewService(nil)
	got := s.Parse("```sh\nkubectl delete pods -A --field-selector=status.phase=Failed\n```\nDeletes failed (evicted) pods in every namespace.")
	if got.Command != "kubectl delete pods -A --field-selector=status.phase=Failed" || got.Note != "Deletes failed (evicted) pods in every namespace." {
		t.Fatalf("parse: %+v", got)
	}
	got = s.Parse("That needs a person, not a command.")
	if got.Command != "" || got.Note != "That needs a person, not a command." {
		t.Fatalf("no block: %+v", got)
	}
}

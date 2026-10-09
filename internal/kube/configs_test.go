package kube

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A home folder in a temp dir: the Service never sees the user's own
// ~/.kube, and backups and per-shell files go to temp folders too.
func configService(t *testing.T) (*Service, string) {
	t.Helper()
	home := t.TempDir()
	return &Service{home: home, stateDir: filepath.Join(home, "state"), cacheDir: filepath.Join(home, "cache")}, home
}

const mainConfig = `apiVersion: v1
kind: Config
current-context: prod
clusters:
- name: prod-cluster
  cluster:
    server: https://ABC123.gr7.us-east-1.eks.amazonaws.com
    certificate-authority-data: Q0VSVA==
- name: kind-local
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: prod
  context:
    cluster: prod-cluster
    user: eks-user
    namespace: shop
- name: local
  context:
    cluster: kind-local
    user: kind-user
- name: broken
  context:
    cluster: gone
    user: kind-user
users:
- name: eks-user
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      command: aws-iam-authenticator
      args: [token, -i, sales]
      env:
      - name: AWS_PROFILE
        value: devops
      - name: SECRET_THING
        value: hunter2
- name: kind-user
  user:
    client-certificate-data: Q0xJRU5U
    client-key-data: S0VZ
`

const extraConfig = `apiVersion: v1
kind: Config
current-context: local
contexts:
- name: prod
  context:
    cluster: other
    user: other
- name: staging
  context:
    cluster: prod-cluster
    user: token-user
users:
- name: token-user
  user:
    token: sekrit-token
`

func writeConfigs(t *testing.T, home string) (main, extra string) {
	t.Helper()
	main = filepath.Join(home, ".kube", "config")
	extra = filepath.Join(home, "work", "extra.yaml")
	write := func(p, data string) {
		os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(main, mainConfig)
	write(extra, extraConfig)
	return main, extra
}

func ctxByName(l ConfigList, name string) *KubeContext {
	for i := range l.Contexts {
		if l.Contexts[i].Name == name {
			return &l.Contexts[i]
		}
	}
	return nil
}

func TestContexts(t *testing.T) {
	s, home := configService(t)
	main, extra := writeConfigs(t, home)
	bin := t.TempDir()
	env := map[string]string{"PWD": filepath.Join(home, "work"), "PATH": bin, "KUBECONFIG": main + ":extra.yaml"}

	l := s.Contexts(env)
	if strings.Join(l.Files, ",") != main+","+extra {
		t.Errorf("files %v", l.Files)
	}
	var names []string
	for _, c := range l.Contexts {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "broken,local,prod,staging" {
		t.Errorf("contexts %v", names)
	}

	prod := ctxByName(l, "prod")
	// The first definition wins, and so does the first current-context.
	if prod.Cluster != "prod-cluster" || prod.File != main || !prod.Current || prod.Namespace != "shop" {
		t.Errorf("prod %+v", prod)
	}
	if prod.Auth != "exec" || prod.ExecCommand != "aws-iam-authenticator" || prod.PluginFound ||
		prod.EKSCluster != "sales" || prod.EKSRegion != "us-east-1" || prod.AWSProfile != "devops" {
		t.Errorf("prod auth %+v", prod)
	}
	if strings.Join(prod.ExecEnv, ",") != "AWS_PROFILE=devops,SECRET_THING" {
		t.Errorf("exec env %v: only names, except the AWS profile and region", prod.ExecEnv)
	}
	if c := ctxByName(l, "local"); c.Auth != "client-cert" || c.Current {
		t.Errorf("local %+v", c)
	}
	if c := ctxByName(l, "staging"); c.Auth != "token" || c.File != extra {
		t.Errorf("staging %+v", c)
	}
	if c := ctxByName(l, "broken"); len(c.Problems) != 1 || !strings.Contains(c.Problems[0], `"gone"`) {
		t.Errorf("broken %+v", c)
	}

	// With the plugin on the shell's PATH.
	os.WriteFile(filepath.Join(bin, "aws-iam-authenticator"), []byte("#!/bin/sh\n"), 0o755)
	if c := ctxByName(s.Contexts(env), "prod"); !c.PluginFound {
		t.Error("plugin on PATH should be found")
	}

	data, _ := json.Marshal(l)
	for _, secret := range []string{"hunter2", "sekrit-token", "Q0xJRU5U", "S0VZ", "Q0VSVA=="} {
		if strings.Contains(string(data), secret) {
			t.Errorf("the list contains a secret: %s", secret)
		}
	}
}

func TestShellConfig(t *testing.T) {
	s, home := configService(t)
	main, _ := writeConfigs(t, home)
	env := map[string]string{}

	cmd, err := s.ShellConfig(env, "local", "", "dev")
	if err != nil {
		t.Fatal(err)
	}
	overlay := strings.TrimPrefix(strings.SplitN(strings.Trim(strings.TrimPrefix(cmd, "export KUBECONFIG="), "'"), ":", 2)[0], "'")
	if !strings.HasPrefix(cmd, "export KUBECONFIG=") || !strings.HasSuffix(strings.Trim(cmd, "'"), ":"+main) || !strings.HasPrefix(overlay, s.overlayDir()) {
		t.Fatalf("command %q", cmd)
	}
	data, _ := os.ReadFile(overlay)
	text := string(data)
	for _, want := range []string{"current-context: local", "cluster: kind-local", "user: kind-user", "namespace: dev"} {
		if !strings.Contains(text, want) {
			t.Errorf("overlay lacks %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"server:", "client-certificate", "token", "users:", "clusters:"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("overlay must hold no cluster or credentials (%q):\n%s", unwanted, text)
		}
	}
	if fi, _ := os.Stat(overlay); fi.Mode().Perm() != 0o600 {
		t.Errorf("overlay mode %v", fi.Mode().Perm())
	}

	// A shell using it: wtf's own file isn't listed, the contexts still are.
	shell := map[string]string{"KUBECONFIG": overlay + ":" + main}
	l := s.Contexts(shell)
	if strings.Join(l.Files, ",") != main || ctxByName(l, "prod") == nil {
		t.Errorf("files %v", l.Files)
	}
	// Switching again replaces wtf's file instead of stacking them.
	cmd2, _ := s.ShellConfig(shell, "prod", "", "")
	if strings.Count(cmd2, s.overlayDir()) != 1 {
		t.Errorf("second switch %q", cmd2)
	}

	if _, err := s.ShellConfig(env, "nope", "", ""); err == nil {
		t.Error("unknown context")
	}
	s.ClearShellConfigs()
	if _, err := os.Stat(overlay); !os.IsNotExist(err) {
		t.Error("per-shell files should go on startup")
	}
}

func TestPlanAndApplyKubectlEdits(t *testing.T) {
	s, home := configService(t)
	main, _ := writeConfigs(t, home)
	f := &fakeKubectl{answers: map[string]string{"config": ""}}
	bin := f.install(t)
	env := map[string]string{"PATH": bin}

	plan, err := s.PlanEdit(env, Op{Kind: "delete", Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	// kind-user is shared with "broken", so it stays; kind-local goes.
	if strings.Join(plan, "\n") != "kubectl config delete-context local\nkubectl config delete-cluster kind-local" {
		t.Errorf("plan %q", plan)
	}
	plan, _ = s.PlanEdit(env, Op{Kind: "delete", Name: "prod"})
	if !slices.Contains(plan, "kubectl config delete-user eks-user") || !strings.Contains(strings.Join(plan, "\n"), "current context") {
		t.Errorf("plan %q", plan)
	}
	if plan, _ := s.PlanEdit(env, Op{Kind: "namespace", Name: "prod", Value: "payments"}); plan[0] != "kubectl config set-context prod --namespace=payments" {
		t.Errorf("plan %q", plan)
	}
	for _, bad := range []Op{{Kind: "rename", Name: "prod", Value: "local"}, {Kind: "rename", Name: "prod", Value: "a b"}, {Kind: "namespace", Name: "prod", Value: "-x"}, {Kind: "x", Name: "prod"}} {
		if _, err := s.PlanEdit(env, bad); err == nil {
			t.Errorf("%+v should be refused", bad)
		}
	}

	versions := s.Contexts(env).Versions
	if _, err := s.ApplyEdit(env, Op{Kind: "rename", Name: "prod", Value: "production"}, versions); err != nil {
		t.Fatal(err)
	}
	last := f.calls[len(f.calls)-1]
	if strings.Join(last, " ") != "config rename-context prod production" {
		t.Errorf("ran %v", last)
	}
	if env := strings.Join(f.envs[len(f.envs)-1], "\n"); !strings.Contains(env, "KUBECONFIG="+main) {
		t.Errorf("kubectl must edit the shell's files: %s", env)
	}
	if backups, _ := os.ReadDir(filepath.Join(home, "cache", "kube-backups")); len(backups) != 1 {
		t.Errorf("backups %d", len(backups))
	}

	os.WriteFile(main, []byte(mainConfig+"# changed\n"), 0o600)
	calls := len(f.calls)
	if _, err := s.ApplyEdit(env, Op{Kind: "delete", Name: "local"}, versions); err == nil || len(f.calls) != calls {
		t.Error("a file changed since listing must stop the edit")
	}
}

func TestFixAuth(t *testing.T) {
	s, home := configService(t)
	main, _ := writeConfigs(t, home)
	env := map[string]string{}

	plan, err := s.PlanEdit(env, Op{Kind: "fixAuth", Name: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan[0], "aws --region us-east-1 eks get-token --cluster-name sales --output json") || !strings.Contains(plan[1], "devops") {
		t.Errorf("plan %q", plan)
	}
	if _, err := s.ApplyEdit(env, Op{Kind: "fixAuth", Name: "prod"}, s.Contexts(env).Versions); err != nil {
		t.Fatal(err)
	}
	c := ctxByName(s.Contexts(env), "prod")
	if c.ExecCommand != "aws" || strings.Join(c.ExecArgs, " ") != "--region us-east-1 eks get-token --cluster-name sales --output json" ||
		c.AWSProfile != "devops" || c.EKSCluster != "sales" {
		t.Errorf("after fix %+v", c)
	}
	data, _ := os.ReadFile(main)
	if !strings.Contains(string(data), "client-key-data: S0VZ") || !strings.Contains(string(data), "value: hunter2") {
		t.Error("the rest of the file must stay")
	}
	if _, err := s.PlanEdit(env, Op{Kind: "fixAuth", Name: "local"}); err == nil {
		t.Error("only aws-iam-authenticator users can be switched")
	}
}

func TestMerge(t *testing.T) {
	s, home := configService(t)
	main, _ := writeConfigs(t, home)
	incoming := filepath.Join(home, "Downloads", "team.yaml")
	os.MkdirAll(filepath.Dir(incoming), 0o700)
	os.WriteFile(incoming, []byte(`apiVersion: v1
kind: Config
clusters:
- name: team-cluster
  cluster: {server: "https://team.example.com"}
- name: kind-local
  cluster: {server: "https://evil.example.com"}
contexts:
- name: team
  context: {cluster: team-cluster, user: team-user}
- name: prod
  context: {cluster: team-cluster, user: team-user}
users:
- name: team-user
  user: {token: abc}
`), 0o600)
	env := map[string]string{}

	plan, err := s.PlanEdit(env, Op{Kind: "merge", Value: incoming})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan, "\n")
	for _, want := range []string{"add cluster team-cluster", "add user team-user", "add context team", "Skip cluster kind-local", "Skip context prod"} {
		if !strings.Contains(joined, want) {
			t.Errorf("plan lacks %q:\n%s", want, joined)
		}
	}
	if _, err := s.ApplyEdit(env, Op{Kind: "merge", Value: incoming}, s.Contexts(env).Versions); err != nil {
		t.Fatal(err)
	}
	l := s.Contexts(env)
	if c := ctxByName(l, "team"); c == nil || c.Server != "https://team.example.com" || c.File != main {
		t.Errorf("team %+v", c)
	}
	if c := ctxByName(l, "local"); c.Server != "https://127.0.0.1:6443" {
		t.Errorf("existing entries must win: %+v", c)
	}
	if _, err := s.PlanEdit(env, Op{Kind: "merge", Value: incoming}); err == nil {
		t.Error("merging the same file twice adds nothing")
	}
}

func TestEKSClusters(t *testing.T) {
	s, _ := configService(t)
	f := &fakeKubectl{answers: map[string]string{"eks": `{"clusters":["sales","production"]}`}}
	f.install(t)
	got, err := s.EKSClusters(map[string]string{}, "devops", "us-east-1")
	if err != nil || strings.Join(got, ",") != "production,sales" {
		t.Fatalf("clusters %v %v", got, err)
	}
	if c := f.calls[0]; strings.Join(c, " ") != "eks list-clusters --region us-east-1 --output json --profile devops" {
		t.Errorf("call %v", c)
	}
	for _, bad := range [][2]string{{"devops", "Mars"}, {"-x", "us-east-1"}} {
		if _, err := s.EKSClusters(map[string]string{}, bad[0], bad[1]); err == nil {
			t.Errorf("%v should be refused", bad)
		}
	}
}

func TestOtherKubeconfigsInDotKube(t *testing.T) {
	s, home := configService(t)
	main, _ := writeConfigs(t, home)
	dir := filepath.Join(home, ".kube")
	os.WriteFile(filepath.Join(dir, "qe.yaml"), []byte(`apiVersion: v1
kind: Config
current-context: qe
clusters:
- name: default
  cluster: {server: "https://qe.example.com:6443"}
contexts:
- name: qe
  context: {cluster: default, user: default}
users:
- name: default
  user: {token: qe-token}
`), 0o600)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a kubeconfig"), 0o600)
	os.MkdirAll(filepath.Join(dir, "cache", "discovery"), 0o700)
	os.WriteFile(filepath.Join(dir, "cache", "x.yaml"), []byte("contexts:\n- name: cached\n"), 0o600)
	f := &fakeKubectl{answers: map[string]string{"config": "", "version": `{"serverVersion":{"gitVersion":"v1.30.1"}}`, "auth": `{}`}}
	f.install(t)
	env := map[string]string{}

	l := s.Contexts(env)
	qeFile := filepath.Join(dir, "qe.yaml")
	if strings.Join(l.Files, ",") != main || strings.Join(l.OtherFiles, ",") != qeFile {
		t.Fatalf("files %v, other %v", l.Files, l.OtherFiles)
	}
	qe := ctxByName(l, "qe")
	if qe == nil || qe.Active || qe.Current || qe.File != qeFile || qe.Server != "https://qe.example.com:6443" || qe.Auth != "token" {
		t.Errorf("qe %+v", qe)
	}
	if p := ctxByName(l, "prod"); !p.Active || !p.Current {
		t.Errorf("prod %+v", p)
	}
	if ctxByName(l, "cached") != nil {
		t.Error("kubectl's cache isn't a kubeconfig")
	}
	if l.Versions[qeFile] == "" {
		t.Error("other files need versions too")
	}

	// Switching to it: the shell sees only that file.
	cmd, err := s.ShellConfig(env, "qe", qeFile, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.Trim(cmd, "'"), ":"+qeFile) || strings.Contains(cmd, main) {
		t.Errorf("command %q", cmd)
	}

	// Testing it reads only that file.
	if c, err := s.TestConnection(env, "qe", qeFile); err != nil || c.Version != "v1.30.1" {
		t.Errorf("connection %+v %v", c, err)
	}
	if e := strings.Join(f.envs[0], "\n"); !strings.Contains(e, "KUBECONFIG="+qeFile+"\n") {
		t.Errorf("env %s", e)
	}

	// Editing it touches only that file.
	plan, err := s.PlanEdit(env, Op{Kind: "namespace", Name: "qe", Value: "tests", File: qeFile})
	if err != nil || plan[0] != "kubectl config set-context qe --namespace=tests" {
		t.Fatalf("plan %q %v", plan, err)
	}
	if _, err := s.ApplyEdit(env, Op{Kind: "namespace", Name: "qe", Value: "tests", File: qeFile}, l.Versions); err != nil {
		t.Fatal(err)
	}
	if e := strings.Join(f.envs[len(f.envs)-1], "\n"); !strings.Contains(e, "KUBECONFIG="+qeFile+"\n") {
		t.Errorf("kubectl must edit only qe.yaml: %s", e)
	}

	// Nothing outside the listed files can be named.
	for _, bad := range []string{"/etc/passwd", filepath.Join(dir, "notes.txt"), filepath.Join(dir, "cache", "x.yaml")} {
		if _, err := s.PlanEdit(env, Op{Kind: "delete", Name: "qe", File: bad}); err == nil {
			t.Errorf("%s should be refused", bad)
		}
		if _, err := s.ShellConfig(env, "qe", bad, ""); err == nil {
			t.Errorf("%s should be refused", bad)
		}
	}
}

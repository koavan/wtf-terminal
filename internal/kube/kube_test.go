package kube

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// fakeKubectl answers by the kubectl subcommand; it never runs anything.
// It records each call's arguments and environment.
type fakeKubectl struct {
	answers map[string]string // "config", "get deployments.apps,pods", "get events", "get namespaces"
	errs    map[string]error
	mu      sync.Mutex
	calls   [][]string
	envs    [][]string
}

func (f *fakeKubectl) install(t *testing.T) string {
	t.Helper()
	old := runKubectl
	t.Cleanup(func() { runKubectl = old })
	runKubectl = func(_ context.Context, bin string, env []string, args ...string) ([]byte, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, args)
		f.envs = append(f.envs, env)
		key := args[0]
		if key == "get" {
			key += " " + args[1]
		}
		if err := f.errs[key]; err != nil {
			return nil, err
		}
		a, ok := f.answers[key]
		if !ok {
			return nil, errors.New("unexpected kubectl " + strings.Join(args, " "))
		}
		return []byte(a), nil
	}
	// A kubectl the lookup can find on the "shell's" PATH.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte("#!/bin/sh\nexit 99\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

const configView = `{"current-context":"prod","contexts":[{"name":"prod","context":{"cluster":"arn:aws:eks:us-east-1:123456789012:cluster/sales","namespace":"shop"}}]}`

const workloads = `{"items":[
 {"kind":"Deployment","metadata":{"name":"web","creationTimestamp":"2026-10-01T00:00:00Z"},
  "spec":{"replicas":3,"selector":{"matchLabels":{"app":"web"}},"template":{"spec":{"containers":[{"image":"web:1.2"}]}}},
  "status":{"readyReplicas":2,"updatedReplicas":3,"availableReplicas":2,"conditions":[{"type":"Available","status":"False","message":"Deployment does not have minimum availability."},{"type":"Progressing","status":"True","message":"ok"}]}},
 {"kind":"Pod","metadata":{"name":"web-1","labels":{"app":"web"},"creationTimestamp":"2026-10-08T00:00:00Z","ownerReferences":[{"kind":"ReplicaSet","name":"web-7d9f","controller":true}]},
  "spec":{"nodeName":"n1","containers":[{"name":"app","image":"web:1.2"},{"name":"proxy","image":"envoy"}]},
  "status":{"phase":"Running","podIP":"10.0.0.7","containerStatuses":[
    {"name":"app","ready":false,"restartCount":7,"state":{"waiting":{"reason":"CrashLoopBackOff"}},"lastState":{"terminated":{"reason":"OOMKilled","exitCode":137,"finishedAt":"2026-10-09T10:00:00Z"}}},
    {"name":"proxy","ready":true,"restartCount":0,"state":{"running":{"startedAt":"2026-10-08T00:00:00Z"}}}]}},
 {"kind":"Pod","metadata":{"name":"web-2","labels":{"app":"web"},"creationTimestamp":"2026-10-08T00:00:00Z"},
  "spec":{"containers":[{"name":"app","image":"web:1.2"}]},
  "status":{"phase":"Running","containerStatuses":[{"name":"app","ready":true,"restartCount":0,"state":{"running":{}}}]}},
 {"kind":"Pod","metadata":{"name":"migrate","creationTimestamp":"2026-10-08T00:00:00Z"},
  "spec":{"initContainers":[{"name":"wait"}],"containers":[{"name":"app","image":"m"}]},
  "status":{"phase":"Pending","initContainerStatuses":[{"name":"wait","state":{"running":{}}}],"containerStatuses":[{"name":"app","state":{"waiting":{"reason":"PodInitializing"}}}]}},
 {"kind":"Pod","metadata":{"name":"old","creationTimestamp":"2026-10-08T00:00:00Z","deletionTimestamp":"2026-10-09T00:00:00Z"},
  "spec":{"containers":[{"name":"app","image":"m"}]},"status":{"phase":"Running","containerStatuses":[{"name":"app","ready":true,"state":{"running":{}}}]}},
 {"kind":"Pod","metadata":{"name":"job-x","creationTimestamp":"2026-10-08T00:00:00Z"},
  "spec":{"containers":[{"name":"app","image":"m"}]},"status":{"phase":"Succeeded","containerStatuses":[{"name":"app","state":{"terminated":{"reason":"Completed","exitCode":0}}}]}}
]}`

const events = `{"items":[
 {"type":"Normal","reason":"Pulled","message":"pulled","count":1,"lastTimestamp":"2026-10-09T09:00:00Z","involvedObject":{"kind":"Pod","name":"web-2"}},
 {"type":"Warning","reason":"BackOff","message":"Back-off restarting failed container","count":42,"lastTimestamp":"2026-10-09T10:05:00Z","involvedObject":{"kind":"Pod","name":"web-1"}},
 {"type":"Normal","reason":"Scheduled","message":"assigned","eventTime":"2026-10-09T09:30:00.123456Z","involvedObject":{"kind":"Pod","name":"web-1"}}
]}`

func shellEnv(pathDir string) map[string]string {
	return map[string]string{"PWD": "/work", "PATH": pathDir + ":/usr/bin", "KUBECONFIG": "kube/conf:~/.kube/config", "AWS_PROFILE": "devops", "TF_WORKSPACE": "x"}
}

// fakeWith answers config view and the listing calls with the test data.
func fakeWith() *fakeKubectl {
	return &fakeKubectl{answers: map[string]string{
		"config": configView, "get deployments.apps": workloads, "get pods": workloads, "get events": events,
	}}
}

var resolved = func(dir string) Request {
	return Request{Env: shellEnv(dir), Context: "prod", Namespace: "shop"}
}

func TestTargetReadsOnlyTheKubeconfig(t *testing.T) {
	f := fakeWith()
	dir := f.install(t)
	t.Setenv("AWS_PROFILE", "wtf-own")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "app-secret")
	s := &Service{home: "/home/me"}

	tg, err := s.Target(Request{Env: shellEnv(dir)})
	if err != nil {
		t.Fatal(err)
	}
	if tg.Context != "prod" || tg.Namespace != "shop" || !strings.HasSuffix(tg.Cluster, "cluster/sales") {
		t.Errorf("target %+v", tg)
	}
	if len(f.calls) != 1 || f.calls[0][0] != "config" {
		t.Errorf("calls %v: resolving must not reach the cluster", f.calls)
	}
	if tg, _ := s.Target(Request{Env: shellEnv(dir), Namespace: "other"}); tg.Namespace != "other" {
		t.Errorf("namespace override: %+v", tg)
	}

	// The shell's variables, not wtf's own.
	env := strings.Join(f.envs[0], "\n")
	for _, want := range []string{"PATH=" + dir + ":/usr/bin", "KUBECONFIG=/work/kube/conf:/home/me/.kube/config", "AWS_PROFILE=devops"} {
		if !strings.Contains(env, want) {
			t.Errorf("env lacks %s", want)
		}
	}
	for _, unwanted := range []string{"wtf-own", "app-secret", "TF_WORKSPACE"} {
		if strings.Contains(env, unwanted) {
			t.Errorf("env has %s", unwanted)
		}
	}
}

func TestReadsOneKindAtATime(t *testing.T) {
	f := fakeWith()
	dir := f.install(t)
	s := &Service{home: "/home/me"}

	deps, err := s.Deployments(resolved(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 {
		t.Fatalf("deployments %+v (pods in the answer must be ignored)", deps)
	}
	d := deps[0]
	if d.Name != "web" || d.Desired != 3 || d.Ready != 2 || d.Selector["app"] != "web" || len(d.Problems) != 1 || d.Images[0] != "web:1.2" {
		t.Errorf("deployment %+v", d)
	}
	if strings.Join(f.calls[0][:2], " ") != "get deployments.apps" || f.calls[0][slices.Index(f.calls[0], "--namespace")+1] != "shop" ||
		f.calls[0][slices.Index(f.calls[0], "--context")+1] != "prod" {
		t.Errorf("call %v", f.calls[0])
	}

	pods, err := s.Pods(resolved(dir), map[string]string{"app": "web", "tier": "front"})
	if err != nil {
		t.Fatal(err)
	}
	if c := f.calls[1]; c[slices.Index(c, "--selector")+1] != "app=web,tier=front" {
		t.Errorf("pods call %v", c)
	}
	status := map[string]string{}
	for _, p := range pods {
		status[p.Name] = p.Status
	}
	want := map[string]string{"web-1": "CrashLoopBackOff", "web-2": "Running", "migrate": "Init:0/1", "old": "Terminating", "job-x": "Completed"}
	for name, st := range want {
		if status[name] != st {
			t.Errorf("%s status %q, want %q", name, status[name], st)
		}
	}
	var w1 Pod
	for _, p := range pods {
		if p.Name == "web-1" {
			w1 = p
		}
	}
	if w1.Ready != 1 || w1.Total != 2 || w1.Restarts != 7 || w1.Owner != "ReplicaSet/web-7d9f" || w1.LastRestart != "2026-10-09T10:00:00Z" ||
		w1.Containers[0].LastReason != "OOMKilled" || w1.Containers[0].Reason != "CrashLoopBackOff" {
		t.Errorf("web-1 %+v", w1)
	}

	evs, err := s.Events(resolved(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 || evs[0].Reason != "BackOff" || evs[0].Count != 42 || evs[1].Reason != "Scheduled" {
		t.Errorf("events %+v", evs)
	}
	if len(f.calls) != 3 {
		t.Errorf("each read is one call: %v", f.calls)
	}
}

func TestReadErrors(t *testing.T) {
	f := fakeWith()
	f.errs = map[string]error{"get pods": errors.New("error: You must be logged in to the server (Unauthorized)")}
	dir := f.install(t)
	s := &Service{home: "/home/me"}

	if _, err := s.Pods(resolved(dir), nil); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("err %v", err)
	}
	refused := []func() error{
		func() error { _, err := s.Deployments(Request{Env: shellEnv(dir)}); return err }, // not resolved
		func() error {
			_, err := s.Events(Request{Env: shellEnv(dir), Context: "--kubeconfig=/etc/x", Namespace: "a"})
			return err
		},
		func() error {
			_, err := s.Events(Request{Env: shellEnv(dir), Context: "prod", Namespace: "a b"})
			return err
		},
		func() error { _, err := s.Pods(resolved(dir), map[string]string{"app": "web,evil=1"}); return err },
		func() error { _, err := s.Target(Request{Env: shellEnv(dir), Context: "-x"}); return err },
	}
	before := len(f.calls)
	for i, call := range refused {
		if call() == nil {
			t.Errorf("request %d should be refused", i)
		}
	}
	if len(f.calls) != before {
		t.Errorf("refused requests must not run kubectl: %v", f.calls[before:])
	}
}

func TestNamespaces(t *testing.T) {
	f := &fakeKubectl{answers: map[string]string{"get namespaces": "namespace/shop\nnamespace/default\nnamespace/kube-system\n"}}
	dir := f.install(t)
	got, err := (&Service{}).Namespaces(Request{Env: shellEnv(dir), Context: "prod"})
	if err != nil || strings.Join(got, ",") != "default,kube-system,shop" {
		t.Errorf("namespaces %v %v", got, err)
	}
	if c := f.calls[0]; len(f.calls) != 1 || c[slices.Index(c, "--context")+1] != "prod" {
		t.Errorf("calls %v", f.calls)
	}
}

func TestParseTable(t *testing.T) {
	text := "NAME      SCHEDULE      TIMEZONE   SUSPEND   ACTIVE   LAST SCHEDULE   AGE\n" +
		"backup    */5 * * * *   <none>     False     0        2m              10d\n" +
		"report    0 3 * * 1     UTC        True      0        <none>          3d\n"
	tb := parseTable(text)
	if strings.Join(tb.Columns, "|") != "NAME|SCHEDULE|TIMEZONE|SUSPEND|ACTIVE|LAST SCHEDULE|AGE" {
		t.Errorf("columns %q", tb.Columns)
	}
	if len(tb.Rows) != 2 || strings.Join(tb.Rows[0], "|") != "backup|*/5 * * * *|<none>|False|0|2m|10d" ||
		strings.Join(tb.Rows[1], "|") != "report|0 3 * * 1|UTC|True|0|<none>|3d" {
		t.Errorf("rows %q", tb.Rows)
	}
	if tb := parseTable(""); len(tb.Columns) != 0 || len(tb.Rows) != 0 {
		t.Errorf("empty: %+v", tb)
	}
}

func TestTableKinds(t *testing.T) {
	f := &fakeKubectl{answers: map[string]string{
		"get secrets": "NAME                          TYPE                 DATA   AGE\n" +
			"db-password                   Opaque               2      40d\n" +
			"sh.helm.release.v1.web.v12    helm.sh/release.v1   1      5d\n",
		"get nodes":                       "NAME          STATUS   ROLES    AGE   VERSION\nip-10-0-1-5   Ready    <none>   90d   v1.29.3\n",
		"get services":                    "",
		"get ingresses.networking.k8s.io": "NAME   CLASS   HOSTS              ADDRESS   PORTS   AGE\nweb    nginx   a.com,b.com        1.2.3.4   80      1d\n",
	}}
	dir := f.install(t)
	s := &Service{home: "/home/me"}

	sec, err := s.Table(resolved(dir), "secrets")
	if err != nil {
		t.Fatal(err)
	}
	if len(sec.Rows) != 2 || strings.Join(sec.Rows[1], "|") != "sh.helm.release.v1.web.v12|helm.sh/release.v1|1|5d" {
		t.Errorf("secrets %q", sec.Rows)
	}
	// Secrets are listed as a table only: never as JSON or YAML, which carry values.
	for _, a := range f.calls[0] {
		if a == "-o" || a == "--output" || strings.HasPrefix(a, "-o") || strings.HasPrefix(a, "--output") {
			t.Errorf("secrets read with an output format: %v", f.calls[0])
		}
	}

	nodes, err := s.Table(Request{Env: shellEnv(dir), Context: "prod"}, "nodes")
	if err != nil || nodes.Rows[0][0] != "ip-10-0-1-5" || slices.Contains(f.calls[1], "--namespace") {
		t.Errorf("nodes %+v %v (call %v)", nodes, err, f.calls[1])
	}

	if svc, err := s.Table(resolved(dir), "services"); err != nil || len(svc.Rows) != 0 {
		t.Errorf("no services: %+v %v", svc, err)
	}
	ing, _ := s.Table(resolved(dir), "ingresses")
	if ing.Rows[0][2] != "a.com,b.com" || ing.Rows[0][3] != "1.2.3.4" {
		t.Errorf("ingress %q", ing.Rows)
	}

	before := len(f.calls)
	for _, bad := range []string{"pods/exec", "secrets -o json", "", "customresourcedefinitions"} {
		if _, err := s.Table(resolved(dir), bad); err == nil {
			t.Errorf("kind %q should be refused", bad)
		}
	}
	if _, err := s.Table(Request{Env: shellEnv(dir), Context: "prod"}, "secrets"); err == nil {
		t.Error("a namespaced kind needs a namespace")
	}
	if len(f.calls) != before {
		t.Error("refused requests must not run kubectl")
	}
}

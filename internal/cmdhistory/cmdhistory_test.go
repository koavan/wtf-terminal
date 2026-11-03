package cmdhistory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dayanraj/wtf/internal/infra"
)

func TestRecordSearchForget(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	kube := filepath.Join(home, "kubeconfig")
	os.WriteFile(kube, []byte("apiVersion: v1\nkind: Config\ncurrent-context: prod\ncontexts:\n- name: prod\n  context: {cluster: c1, namespace: shop}\n- name: dev\n  context: {cluster: c2}\nclusters:\n- name: c1\n  cluster: {server: https://x}\n- name: c2\n  cluster: {server: https://y}\n"), 0o600)
	s := NewService(dir, infra.NewService())
	now := time.Now().UTC()
	rec := func(session, cmd string, env map[string]string, ago time.Duration, code int) {
		t.Helper()
		if err := s.Record(Record{Session: session, Command: cmd, Cwd: "/w", Env: env, StartedAt: now.Add(-ago).Format(time.RFC3339), DurationMs: 1200, ExitCode: code}); err != nil {
			t.Fatal(err)
		}
	}
	prod := map[string]string{"KUBECONFIG": kube, "AWS_PROFILE": "ops", "AWS_REGION": "eu-west-1"}
	rec("s1", "kubectl get pods", prod, 3*time.Hour, 0)
	rec("s1", "export TOKEN=ghp_abcdefghijklmnopqrstuvwxyz0123456789", prod, 2*time.Hour, 0)
	rec("s2", "kubectl rollout restart deploy/api", prod, time.Hour, 1)
	rec("s2", "   ", prod, time.Minute, 0)

	r := s.Search(Query{})
	if r.Total != 3 || !r.Saving || r.Entries[0].Command != "kubectl rollout restart deploy/api" {
		t.Fatalf("all = %+v", r)
	}
	e := r.Entries[0]
	if e.KubeContext != "prod" || e.KubeNamespace != "shop" || e.AWSProfile != "ops" || e.AWSRegion != "eu-west-1" || e.ExitCode != 1 {
		t.Errorf("context = %+v", e)
	}
	if strings.Contains(r.Entries[1].Command, "ghp_") {
		t.Errorf("a secret was saved: %q", r.Entries[1].Command)
	}
	if len(r.KubeContexts) != 1 || r.KubeContexts[0] != (Facet{"prod", 3}) || r.AWSRegions[0].Value != "eu-west-1" {
		t.Errorf("facets = %+v %+v", r.KubeContexts, r.AWSRegions)
	}
	for q, want := range map[*Query]int{
		{Text: "KUBECTL pods"}: 1, {KubeContext: "prod"}: 3, {KubeContext: "dev"}: 0, {Failed: true}: 1,
		{Since: now.Add(-90 * time.Minute).Format(time.RFC3339)}: 1, {AWSRegion: "us-east-1"}: 0, {Limit: 1}: 3,
	} {
		if got := s.Search(*q); got.Total != want || (q.Limit == 1 && len(got.Entries) != 1) {
			t.Errorf("%+v: %d, want %d", *q, got.Total, want)
		}
	}

	// A new app run reads the file; the old run's session isn't its own.
	s2 := NewService(dir, nil)
	if got := s2.Search(Query{}).Total; got != 3 {
		t.Fatalf("reloaded %d", got)
	}
	if err := s2.ForgetSession("s2"); err != nil || s2.Search(Query{}).Total != 3 {
		t.Errorf("another run's s2 was forgotten")
	}
	if err := s.ForgetSession("s2"); err != nil || s.Search(Query{}).Total != 2 {
		t.Errorf("ForgetSession: %v %d", err, s.Search(Query{}).Total)
	}
	if got := NewService(dir, nil).Search(Query{}).Total; got != 2 {
		t.Errorf("after forgetting, the file has %d", got)
	}
	first := s.Search(Query{}).Entries[0].ID
	if err := s.Forget(first); err != nil || s.Search(Query{}).Total != 1 {
		t.Errorf("Forget: %v", err)
	}
	fi, _ := os.Stat(filepath.Join(dir, fileName))
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode())
	}
	if err := s.ForgetAll(); err != nil || s.Search(Query{}).Total != 0 {
		t.Errorf("ForgetAll: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, fileName)); !os.IsNotExist(err) {
		t.Errorf("the file is still there")
	}
}

func TestRetention(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().AddDate(0, 0, -100).UTC().Format(time.RFC3339)
	recent := time.Now().AddDate(0, 0, -5).UTC().Format(time.RFC3339)
	os.WriteFile(filepath.Join(dir, fileName), []byte(`{"id":"a","command":"old","startedAt":"`+old+`"}`+"\nnot json\n"+`{"id":"b","command":"recent","startedAt":"`+recent+`"}`+"\n"), 0o600)
	s := NewService(dir, nil)
	if r := s.Search(Query{}); r.Total != 1 || r.Entries[0].Command != "recent" {
		t.Fatalf("kept %+v", r.Entries)
	}
	data, _ := os.ReadFile(filepath.Join(dir, fileName))
	if strings.Contains(string(data), "old") || strings.Contains(string(data), "not json") {
		t.Errorf("not pruned: %s", data)
	}
	// history-days = 0: nothing is saved, and the file goes.
	off := NewService(dir, nil)
	off.Days = func() int { return 0 }
	if err := off.Record(Record{Command: "ls", StartedAt: recent}); err != nil {
		t.Fatal(err)
	}
	if r := off.Search(Query{}); r.Total != 0 || r.Saving {
		t.Errorf("off: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, fileName)); !os.IsNotExist(err) {
		t.Errorf("off: the file stays")
	}
}

func TestReadOnly(t *testing.T) {
	dir := t.TempDir()
	recent := time.Now().AddDate(0, 0, -5).UTC().Format(time.RFC3339)
	old := time.Now().AddDate(0, 0, -200).UTC().Format(time.RFC3339)
	body := `{"id":"a","run":"r","session":"s1","command":"kept","startedAt":"` + recent + `"}` + "\n" + `{"id":"b","command":"old","startedAt":"` + old + `"}` + "\n"
	os.WriteFile(filepath.Join(dir, fileName), []byte(body), 0o600)
	s := NewService(dir, nil)
	s.ReadOnly = true
	s.run = "r"
	_ = s.Record(Record{Command: "ls", StartedAt: recent})
	_ = s.ForgetSession("s1")
	_ = s.ForgetAll()
	if data, _ := os.ReadFile(filepath.Join(dir, fileName)); string(data) != body {
		t.Fatalf("read-only changed the file: %s", data)
	}
}

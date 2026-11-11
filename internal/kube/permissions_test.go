package kube

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const canI = `Resources                                       Non-Resource URLs   Resource Names   Verbs
pods                                            []                  []               [get list watch]
configmaps                                      []                  [app-config]     [get]
selfsubjectrulesreviews.authorization.k8s.io    []                  []               [create]
                                                [/healthz]          []               [get]
                                                [/api/*]            []               [get]
`

func TestPermissions(t *testing.T) {
	f := fakeWith()
	dir := f.install(t)
	var calls []string
	old := runKubectl
	runKubectl = func(ctx context.Context, bin string, env []string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		switch args[1] {
		case "can-i":
			return []byte(canI), nil
		case "whoami":
			return []byte(`{"status":{"userInfo":{"username":"arn:aws:iam::1:role/dev","groups":["system:authenticated","devs"]}}}`), nil
		}
		return old(ctx, bin, env, args...)
	}
	s := NewService(t.TempDir())
	p, err := s.Permissions(Request{Env: shellEnv(dir), Context: "prod", Namespace: "shop"})
	if err != nil {
		t.Fatal(err)
	}
	if p.User != "arn:aws:iam::1:role/dev" || !reflect.DeepEqual(p.Groups, []string{"system:authenticated", "devs"}) || p.Admin {
		t.Fatalf("who: %+v", p)
	}
	want := []Rule{
		{Resource: "pods", Names: []string{}, Verbs: []string{"get", "list", "watch"}},
		{Resource: "configmaps", Names: []string{"app-config"}, Verbs: []string{"get"}},
		{Resource: "selfsubjectrulesreviews.authorization.k8s.io", Names: []string{}, Verbs: []string{"create"}},
		{Resource: "/healthz", NonResource: true, Names: []string{}, Verbs: []string{"get"}},
		{Resource: "/api/*", NonResource: true, Names: []string{}, Verbs: []string{"get"}},
	}
	if !reflect.DeepEqual(p.Rules, want) {
		t.Fatalf("rules:\n%+v\nwant\n%+v", p.Rules, want)
	}
	if !strings.Contains(calls[0], "--context prod --namespace shop") {
		t.Fatalf("can-i args: %s", calls[0])
	}
}

func TestPermissionsAdminAndNoWhoami(t *testing.T) {
	f := fakeWith()
	dir := f.install(t)
	old := runKubectl
	runKubectl = func(ctx context.Context, bin string, env []string, args ...string) ([]byte, error) {
		if args[1] == "whoami" {
			return nil, errors.New(`error: the server doesn't have a resource type "selfsubjectreviews"`)
		}
		return []byte("Resources   Non-Resource URLs   Resource Names   Verbs\n*.*         []                  []               [*]\n            [*]                 []               [*]\n"), nil
	}
	t.Cleanup(func() { runKubectl = old })
	p, err := NewService(t.TempDir()).Permissions(Request{Env: shellEnv(dir), Context: "prod", Namespace: "shop"})
	if err != nil || !p.Admin || p.User != "" || len(p.Rules) != 2 {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := NewService(t.TempDir()).Permissions(Request{Env: shellEnv(dir), Context: "-x", Namespace: "shop"}); err == nil {
		t.Fatal("a bad context name should fail")
	}
}

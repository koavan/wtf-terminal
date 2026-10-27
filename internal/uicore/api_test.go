package uicore

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/dayanraj/wtf/internal/apiclient"
)

// The cases of macos/Tests/WtfCoreTests/ApiTests.swift.

func TestAPITitles(t *testing.T) {
	blank := APIBlank()
	for _, c := range []struct {
		r    *apiclient.Request
		want string
	}{
		{nil, "API"},
		{&blank, "API"},
		{&apiclient.Request{Method: "post", URL: "https://api.example.com/v1/users/?page=2#x"}, "POST api.example.com/v1/users"},
		{&apiclient.Request{URL: "localhost:8080"}, "GET localhost:8080"},
	} {
		if got := APITitle(c.r); got != c.want {
			t.Errorf("%+v: %q", c.r, got)
		}
	}
}

func TestAPISecretHeaders(t *testing.T) {
	for _, n := range []string{"Authorization", " cookie ", "X-Auth-Token", "X-API-Key", "apikey", "X-Session-Id", "Proxy-Authorization"} {
		if !APIIsSecret(n) {
			t.Errorf("%s isn't secret", n)
		}
	}
	for _, n := range []string{"Accept", "Content-Type", "User-Agent", "X-Request-Id"} {
		if APIIsSecret(n) {
			t.Errorf("%s is secret", n)
		}
	}
	r := apiclient.Request{Headers: []apiclient.Header{
		{Name: "Authorization", Value: "Bearer abc"}, {Name: "Accept", Value: "*/*"}, {Name: "Cookie", Value: "a=1", Off: true},
	}}
	want := []apiclient.Header{{Name: "Authorization"}, {Name: "Accept", Value: "*/*"}, {Name: "Cookie", Off: true}}
	if got := APIForSaving(r).Headers; !reflect.DeepEqual(got, want) {
		t.Errorf("%+v", got)
	}
	if r.Headers[0].Value != "Bearer abc" {
		t.Error("APIForSaving changed its argument")
	}
}

func TestAPIParams(t *testing.T) {
	u := "https://x.io/a?q=hello%20world&flag&empty=&a%26b=c%3Dd#top"
	p := APIParams(u)
	want := []apiclient.Header{{Name: "q", Value: "hello world"}, {Name: "flag"}, {Name: "empty"}, {Name: "a&b", Value: "c=d"}}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("%+v", p)
	}
	if got := APIURL(u, p); got != "https://x.io/a?q=hello%20world&flag&empty&a%26b=c%3Dd#top" {
		t.Errorf("%s", got)
	}
	edited := append([]apiclient.Header(nil), p...)
	edited[0].Off = true
	edited = append(edited, apiclient.Header{Name: "new", Value: "1+1"})
	if got := APIURL(u, edited); got != "https://x.io/a?flag&empty&a%26b=c%3Dd&new=1%2B1#top" {
		t.Errorf("%s", got)
	}
	if got := APIURL("https://x.io/a?b=1", nil); got != "https://x.io/a" {
		t.Errorf("%s", got)
	}
	if got := APIParams("https://x.io"); len(got) != 0 {
		t.Errorf("%+v", got)
	}
}

func TestAPIFormatting(t *testing.T) {
	for in, want := range map[int64]string{512: "512 B", 1536: "1.5 KB", 3 << 20: "3.0 MB"} {
		if got := APISize(in); got != want {
			t.Errorf("%d: %s", in, got)
		}
	}
	if APIDuration(240) != "240 ms" || APIDuration(1340) != "1.3 s" {
		t.Error(APIDuration(240), APIDuration(1340))
	}
}

func TestAPITabsSaveWithoutCredentialsAndMoveWithThem(t *testing.T) {
	w := NewWindowModel("/home/u")
	cwd := "/t"
	w.Open(OpenOptions{Cwd: &cwd})
	r := apiclient.Request{Method: "POST", URL: "https://x.io", Headers: []apiclient.Header{{Name: "Authorization", Value: "Bearer s3cret"}}, Body: "{}"}
	id := w.OpenAPI(r, nil, 0)
	tab, _ := w.Tab(id)
	if w.Title(tab) != "POST x.io" || w.ActivePane().Kind != PaneAPI {
		t.Fatalf("title %q", w.Title(tab))
	}

	// Saved: the request without its credential.
	data, err := json.Marshal(w.Snapshot(13, nil))
	if err != nil {
		t.Fatal(err)
	}
	var saved SavedSession
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	back := NewWindowModel("/home/u")
	if !back.Restore(saved) {
		t.Fatal("not restored")
	}
	var api *PaneState
	terminals := 0
	for _, p := range back.Panes {
		if p.Kind == PaneAPI {
			api = p
		} else {
			terminals++
		}
	}
	if api == nil || api.API.URL != "https://x.io" || api.API.Body != "{}" ||
		!reflect.DeepEqual(api.API.Headers, []apiclient.Header{{Name: "Authorization"}}) || terminals != 1 {
		t.Errorf("restored %+v, %d terminals", api, terminals)
	}

	// Moved to another window: all of it.
	payload, ok := w.Detach(id, nil)
	if !ok {
		t.Fatal("not detached")
	}
	other := NewWindowModel("/home/u")
	if !other.Adopt(payload) || other.ActivePane().Kind != PaneAPI || !reflect.DeepEqual(*other.ActivePane().API, r) {
		t.Errorf("adopted %+v", other.ActivePane())
	}
}

func TestAPITabsDuplicateAndReopen(t *testing.T) {
	w := NewWindowModel("/home/u")
	cwd := "/t"
	w.Open(OpenOptions{Cwd: &cwd})
	r := apiclient.Request{Method: "GET", URL: "https://x.io/a"}
	id := w.OpenAPI(r, nil, 0)
	dup := w.OpenNextTo(id, true)
	dt, _ := w.Tab(dup)
	if !reflect.DeepEqual(*w.FocusedPane(dt).API, r) {
		t.Errorf("duplicate %+v", w.FocusedPane(dt).API)
	}
	w.CloseMany([]int{dup})
	w.CloseMany([]int{id})
	w.Reopen()
	if p := w.ActivePane(); p.Kind != PaneAPI || !reflect.DeepEqual(*p.API, r) {
		t.Errorf("reopened %+v", p)
	}
	// Workspaces leave API tabs out.
	if n := len(w.CaptureWorkspace("/home/u").Tabs); n != 1 {
		t.Errorf("%d workspace tabs", n)
	}
}

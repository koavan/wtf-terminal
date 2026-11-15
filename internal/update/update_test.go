package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/dayanraj/wtf/internal/events"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		latest, current string
		want            bool
	}{
		{"1.9.4", "1.9.3", true},
		{"v1.10.0", "1.9.3", true},
		{"2.0", "1.99.99", true},
		{"1.9.3", "1.9.3", false},
		{"1.9.2", "1.9.3", false},
		{"1.9.4", "dev", false},
		{"1.9.4", "", false},
		{"1.9.4-rc1", "1.9.3", false},
		{"1.9.4", "1.9.3-dirty", false},
	} {
		if got := newer(c.latest, c.current); got != c.want {
			t.Errorf("newer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

type fakeGitHub struct {
	mu    sync.Mutex
	tag   string
	calls int
	code  int
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.code != 0 {
		w.WriteHeader(f.code)
		return
	}
	w.Write([]byte(`{"tag_name":"` + f.tag + `","name":"wtf ` + f.tag + ` — things","html_url":"https://github.com/o/r/releases/tag/` + f.tag + `","body":"notes"}`))
}

func newTest(t *testing.T, version string, gh *fakeGitHub) (*Service, *[]Release, *time.Time) {
	t.Helper()
	srv := httptest.NewServer(gh)
	t.Cleanup(srv.Close)
	s := NewService(version, t.TempDir())
	s.api = srv.URL
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	var got []Release
	events.SetEmitter(func(name string, data any) {
		if name == EventAvailable {
			got = append(got, data.(Release))
		}
	})
	t.Cleanup(func() { events.SetEmitter(nil) })
	return s, &got, &now
}

func TestAutoNotifiesOnceAndChecksDaily(t *testing.T) {
	gh := &fakeGitHub{tag: "v1.9.4"}
	s, got, now := newTest(t, "1.9.3", gh)
	ctx := context.Background()

	s.auto(ctx)
	if len(*got) != 1 || (*got)[0].Version != "1.9.4" || (*got)[0].Title != "wtf v1.9.4 — things" {
		t.Fatalf("events = %+v", *got)
	}
	// An hour later: no request (checked today), no second event.
	*now = now.Add(time.Hour)
	s.auto(ctx)
	if gh.calls != 1 || len(*got) != 1 {
		t.Fatalf("calls %d, events %d", gh.calls, len(*got))
	}
	// A restart within the day uses the saved answer and tells again.
	s2 := NewService("1.9.3", s.stateDir)
	s2.api, s2.now = s.api, s.now
	s2.auto(ctx)
	if gh.calls != 1 || len(*got) != 2 {
		t.Fatalf("after restart: calls %d, events %d", gh.calls, len(*got))
	}
	// A day later it asks again, and tells about a newer version.
	gh.tag = "v1.9.5"
	*now = now.Add(interval)
	s.auto(ctx)
	if gh.calls != 2 || len(*got) != 3 || (*got)[2].Version != "1.9.5" {
		t.Fatalf("next day: calls %d, events %+v", gh.calls, *got)
	}
}

func TestSkip(t *testing.T) {
	gh := &fakeGitHub{tag: "v1.9.4"}
	s, got, now := newTest(t, "1.9.3", gh)
	ctx := context.Background()
	if err := s.Skip("v1.9.4"); err != nil {
		t.Fatal(err)
	}
	s.auto(ctx)
	if len(*got) != 0 {
		t.Fatalf("skipped version told: %+v", *got)
	}
	// Check for Updates… still shows it.
	st, err := s.Check()
	if err != nil || st.Available == nil || st.Available.Version != "1.9.4" {
		t.Fatalf("Check = %+v, %v", st, err)
	}
	// A newer one is told about.
	gh.tag = "v1.9.5"
	*now = now.Add(interval)
	s.auto(ctx)
	if len(*got) != 1 || (*got)[0].Version != "1.9.5" {
		t.Fatalf("events = %+v", *got)
	}
}

func TestUpToDateDisabledAndErrors(t *testing.T) {
	gh := &fakeGitHub{tag: "v1.9.3"}
	s, got, now := newTest(t, "1.9.3", gh)
	ctx := context.Background()
	st, err := s.Check()
	if err != nil || st.Available != nil || st.Current != "1.9.3" {
		t.Fatalf("Check = %+v, %v", st, err)
	}

	s.Enabled = func() bool { return false }
	gh.tag = "v2.0.0"
	*now = now.Add(interval)
	s.auto(ctx)
	if gh.calls != 1 || len(*got) != 0 {
		t.Fatalf("update-check off: calls %d, events %d", gh.calls, len(*got))
	}

	gh.code = http.StatusForbidden
	if _, err := s.Check(); err == nil {
		t.Fatal("rate limited: no error")
	}
	s.Enabled = nil
	s.auto(ctx) // fails quietly
	if len(*got) != 0 {
		t.Fatalf("events after an error: %+v", *got)
	}
}

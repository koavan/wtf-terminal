// Package update asks GitHub once a day whether a newer wtf has been
// released, and tells the UI when there is one. It never downloads or
// installs anything: the UI offers the release page.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dayanraj/wtf/internal/about"
	"github.com/dayanraj/wtf/internal/events"
)

// EventAvailable carries a Release newer than this build: once per version
// per run, from the automatic check only.
const EventAvailable = "update:available"

// Release is a version published on GitHub.
type Release struct {
	Version string `json:"version"` // "1.9.4", without the v
	Title   string `json:"title"`   // the release's name, "wtf 1.9.4 — …"
	URL     string `json:"url"`     // its page on GitHub
	Notes   string `json:"notes"`   // its description (Markdown)
}

// Status is what Check returns.
type Status struct {
	Current string `json:"current"`
	// The latest release, when it's newer than Current; else nil.
	Available *Release `json:"available"`
}

const (
	interval  = 24 * time.Hour
	firstWait = 15 * time.Second
	stateFile = "update.json"
)

// saved is update.json in the state folder, so restarts don't check again.
type saved struct {
	LastChecked time.Time `json:"lastChecked"`
	Latest      *Release  `json:"latest,omitempty"`
	Skipped     string    `json:"skipped,omitempty"`
}

type Service struct {
	version  string
	stateDir string
	// Whether update-check is on; read before every automatic check.
	Enabled func() bool

	// For tests.
	api    string
	client *http.Client
	now    func() time.Time

	mu       sync.Mutex
	notified map[string]bool
	cancel   context.CancelFunc
	done     chan struct{}
}

func NewService(version, stateDir string) *Service {
	return &Service{
		version:  version,
		stateDir: stateDir,
		api:      "https://api.github.com/repos/" + about.RepoPath + "/releases/latest",
		client:   &http.Client{Timeout: 20 * time.Second},
		now:      time.Now,
		notified: map[string]bool{},
	}
}

// ServiceStartup starts the automatic check: 15 seconds after launch (always
// asking GitHub, so a restart sees a release from earlier today), then every
// day. A build without a release version (a development build) never checks
// on its own.
func (s *Service) ServiceStartup(ctx context.Context) error {
	if _, ok := parse(s.version); !ok || os.Getenv("WTF_NATIVE_DEBUG") != "" {
		return nil
	}
	ctx, s.cancel = context.WithCancel(ctx)
	s.done = make(chan struct{})
	go s.loop(ctx)
	return nil
}

func (s *Service) ServiceShutdown() error {
	if s.cancel != nil {
		s.cancel()
		<-s.done
	}
	return nil
}

func (s *Service) loop(ctx context.Context) {
	defer close(s.done)
	wait, launch := firstWait, true
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		s.auto(ctx, launch)
		launch = false
		// Checked hourly whether a day has passed: a sleeping laptop's timers
		// don't count the time it slept.
		wait = time.Hour
	}
}

// auto asks GitHub on launch and when a day has passed since the last check
// (otherwise it uses what that check found), and tells the UI about a newer,
// not skipped version.
func (s *Service) auto(ctx context.Context, launch bool) {
	if s.Enabled != nil && !s.Enabled() {
		return
	}
	st := s.load()
	latest := st.Latest
	if launch || s.now().Sub(st.LastChecked) >= interval || s.now().Before(st.LastChecked) {
		r, err := s.fetch(ctx)
		switch {
		case err == nil:
			latest = r
			st = s.load() // Skip may have run meanwhile
			st.LastChecked, st.Latest = s.now(), r
			s.save(st)
		case !launch:
			// Offline, or GitHub's rate limit: try again in an hour.
			return
		}
		// A failed launch check uses what the last check found; the hourly
		// check asks again once a day has passed.
	}
	if latest == nil || !newer(latest.Version, s.version) || latest.Version == st.Skipped {
		return
	}
	s.mu.Lock()
	seen := s.notified[latest.Version]
	s.notified[latest.Version] = true
	s.mu.Unlock()
	if !seen {
		events.Emit(EventAvailable, *latest)
	}
}

// Check asks GitHub now (Check for Updates…), whatever update-check says
// and even for a version that was skipped.
func (s *Service) Check() (Status, error) {
	r, err := s.fetch(context.Background())
	if err != nil {
		return Status{}, err
	}
	st := s.load()
	st.LastChecked, st.Latest = s.now(), r
	s.save(st)
	out := Status{Current: s.version}
	if newer(r.Version, s.version) {
		out.Available = r
		s.mu.Lock()
		s.notified[r.Version] = true // shown now; the daily check needn't again
		s.mu.Unlock()
	}
	return out, nil
}

// Skip stops the daily check telling about this version. A newer one is
// told about again.
func (s *Service) Skip(version string) error {
	st := s.load()
	st.Skipped = strings.TrimPrefix(version, "v")
	return s.save(st)
}

// fetch reads the latest release (GitHub leaves out drafts and pre-releases).
func (s *Service) fetch(ctx context.Context) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.api, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "wtf/"+s.version)
	res, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach GitHub: %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	switch {
	case res.StatusCode == http.StatusNotFound:
		return nil, errors.New("no release has been published yet")
	case res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusTooManyRequests:
		return nil, errors.New("GitHub is limiting requests from this network; try again in an hour")
	case res.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("GitHub answered %s", res.Status)
	}
	var gh struct {
		Tag   string `json:"tag_name"`
		Name  string `json:"name"`
		URL   string `json:"html_url"`
		Body  string `json:"body"`
		Draft bool   `json:"draft"`
		Pre   bool   `json:"prerelease"`
	}
	if err := json.Unmarshal(body, &gh); err != nil {
		return nil, fmt.Errorf("reading GitHub's answer: %w", err)
	}
	v := strings.TrimPrefix(gh.Tag, "v")
	if _, ok := parse(v); !ok || gh.Draft || gh.Pre {
		return nil, fmt.Errorf("the latest release's tag %q isn't a version", gh.Tag)
	}
	if !strings.HasPrefix(gh.URL, "https://github.com/") {
		gh.URL = "https://github.com/" + about.RepoPath + "/releases/tag/" + gh.Tag
	}
	title := strings.TrimSpace(gh.Name)
	if title == "" {
		title = "wtf " + v
	}
	return &Release{Version: v, Title: title, URL: gh.URL, Notes: strings.TrimSpace(gh.Body)}, nil
}

func (s *Service) load() saved {
	var st saved
	if b, err := os.ReadFile(filepath.Join(s.stateDir, stateFile)); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	return st
}

func (s *Service) save(st saved) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.stateDir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(s.stateDir, stateFile+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.stateDir, stateFile))
}

// parse reads "1.9.3" (or "v1.9.3", "1.10"); a suffix such as "-dev" or
// "+abc" makes it no release version.
func parse(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	if len(parts) < 2 || len(parts) > 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// newer reports whether latest is a later release than current.
func newer(latest, current string) bool {
	l, ok1 := parse(latest)
	c, ok2 := parse(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

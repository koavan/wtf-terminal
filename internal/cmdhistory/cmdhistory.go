// Package cmdhistory keeps the commands run in wtf's shells across windows
// and restarts, each with the context it ran in: the Kubernetes context and
// namespace, AWS profile, account and region, Google Cloud project,
// Terraform workspace and git branch the shell pointed at (#17), as the
// address bar shows them (read from files, never asked of a cloud). The
// History tab's Everywhere view and the plugin's search_history tool (#31)
// read it.
//
// It's a JSON-lines file in wtf's state folder, readable only by the user.
// Commands go through Hide Secrets' redaction before they're written.
// history-days (default 90) is how long entries are kept; 0 saves nothing.
package cmdhistory

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dayanraj/wtf/internal/infra"
	"github.com/dayanraj/wtf/internal/uicore"
)

const (
	fileName = "history.jsonl"
	// Entries kept, newest; older ones go when the file is next rewritten.
	maxEntries = 100_000
	// Search results at most.
	maxResults = 2000
	maxCommand = 8000
)

// Entry is one command as saved.
type Entry struct {
	// Unique: the app run it came from, and the session's own count.
	ID string `json:"id"`
	// The app run (a random id per start) and the shell's session in it:
	// clearing a shell forgets only that run's session.
	Run     string `json:"run"`
	Session string `json:"session"`
	Command string `json:"command"`
	Cwd     string `json:"cwd"`
	// RFC 3339.
	StartedAt  string  `json:"startedAt"`
	DurationMs float64 `json:"durationMs"`
	ExitCode   int     `json:"exitCode"`

	KubeContext   string `json:"kubeContext,omitempty"`
	KubeNamespace string `json:"kubeNamespace,omitempty"`
	AWSProfile    string `json:"awsProfile,omitempty"`
	AWSAccount    string `json:"awsAccount,omitempty"`
	AWSRegion     string `json:"awsRegion,omitempty"`
	GCPProject    string `json:"gcpProject,omitempty"`
	TFWorkspace   string `json:"tfWorkspace,omitempty"`
	GitBranch     string `json:"gitBranch,omitempty"`

	at time.Time
}

// Record is a finished command, as a UI knows it.
type Record struct {
	Session string `json:"session"`
	Command string `json:"command"`
	Cwd     string `json:"cwd"`
	// The shell's variables when the command started (its last prompt's).
	Env        map[string]string `json:"env"`
	StartedAt  string            `json:"startedAt"`
	DurationMs float64           `json:"durationMs"`
	ExitCode   int               `json:"exitCode"`
}

// Query filters the saved history; empty fields match everything.
type Query struct {
	// Words that must all appear in the command or folder.
	Text          string `json:"text"`
	KubeContext   string `json:"kubeContext"`
	KubeNamespace string `json:"kubeNamespace"`
	AWSAccount    string `json:"awsAccount"`
	AWSProfile    string `json:"awsProfile"`
	AWSRegion     string `json:"awsRegion"`
	// RFC 3339; only entries started at or after it.
	Since string `json:"since"`
	// Failed: only commands that exited non-zero.
	Failed bool `json:"failed"`
	Limit  int  `json:"limit"`
}

// Facet is a value of a context field, with how many entries have it.
type Facet struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// Result is a search's entries, newest first, and the context values in
// the whole saved history (for the filter menus).
type Result struct {
	Entries []Entry `json:"entries"`
	// Matching entries, before Limit.
	Total int `json:"total"`
	// False when history-days is 0.
	Saving       bool    `json:"saving"`
	Days         int     `json:"days"`
	KubeContexts []Facet `json:"kubeContexts"`
	AWSAccounts  []Facet `json:"awsAccounts"`
	AWSRegions   []Facet `json:"awsRegions"`
	AWSProfiles  []Facet `json:"awsProfiles"`
	Namespaces   []Facet `json:"namespaces"`
}

type Service struct {
	infra *infra.Service
	path  string
	// history-days.
	Days func() int
	// ReadOnly: nothing is recorded, pruned or forgotten (the self-checks,
	// which run in the real profile on macOS).
	ReadOnly bool

	run string
	mu  sync.Mutex
	// Oldest first.
	entries []Entry
	seq     int
	loaded  bool
}

func NewService(stateDir string, in *infra.Service) *Service {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return &Service{infra: in, path: filepath.Join(stateDir, fileName), run: hex.EncodeToString(b)}
}

func (s *Service) days() int {
	if s.Days == nil {
		return 90
	}
	return s.Days()
}

// load reads the file once, dropping what's past history-days. Locked.
func (s *Service) load() {
	if s.loaded {
		return
	}
	s.loaded = true
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	cut := s.cutoff()
	dropped := false
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			dropped = true
			continue
		}
		t, err := time.Parse(time.RFC3339, e.StartedAt)
		if err != nil || t.Before(cut) {
			dropped = true
			continue
		}
		e.at = t
		s.entries = append(s.entries, e)
	}
	sort.SliceStable(s.entries, func(i, j int) bool { return s.entries[i].at.Before(s.entries[j].at) })
	if len(s.entries) > maxEntries {
		s.entries = s.entries[len(s.entries)-maxEntries:]
		dropped = true
	}
	if dropped && !s.ReadOnly {
		_ = s.rewrite()
	}
}

func (s *Service) cutoff() time.Time { return time.Now().AddDate(0, 0, -s.days()) }

// rewrite writes every entry kept. Locked.
func (s *Service) rewrite() error {
	if s.days() <= 0 {
		return os.Remove(s.path)
	}
	var b bytes.Buffer
	for _, e := range s.entries {
		line, _ := json.Marshal(e)
		b.Write(append(line, '\n'))
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Record saves a finished command with the context its shell pointed at.
// Nothing is saved with history-days = 0 or for an empty command.
func (s *Service) Record(r Record) error {
	cmd := strings.TrimSpace(r.Command)
	if cmd == "" || s.days() <= 0 || s.ReadOnly {
		return nil
	}
	if len(cmd) > maxCommand {
		cmd = cmd[:maxCommand] + "…"
	}
	at, err := time.Parse(time.RFC3339, r.StartedAt)
	if err != nil {
		at = time.Now().Add(-time.Duration(r.DurationMs) * time.Millisecond)
	}
	e := Entry{Run: s.run, Session: r.Session, Command: uicore.Redact(cmd), Cwd: r.Cwd,
		StartedAt: at.UTC().Format(time.RFC3339), DurationMs: r.DurationMs, ExitCode: r.ExitCode, at: at}
	if s.infra != nil && r.Env != nil {
		env := r.Env
		if env["PWD"] == "" && r.Cwd != "" {
			env = maps(env)
			env["PWD"] = r.Cwd
		}
		c := s.infra.Resolve(env)
		e.KubeContext, e.KubeNamespace = c.KubeContext, c.KubeNamespace
		e.AWSProfile, e.AWSAccount, e.AWSRegion = c.AWSProfile, c.AWSAccount, c.AWSRegion
		e.GCPProject, e.TFWorkspace = c.GCPProject, c.TFWorkspace
		if c.Git != nil {
			e.GitBranch = c.Git.Branch
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	s.seq++
	e.ID = fmt.Sprintf("%s-%d", s.run, s.seq)
	s.entries = append(s.entries, e)
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	line, _ := json.Marshal(e)
	_, err = f.Write(append(line, '\n'))
	return err
}

func maps(m map[string]string) map[string]string {
	c := make(map[string]string, len(m)+1)
	for k, v := range m {
		c[k] = v
	}
	return c
}

// Search lists saved commands matching q, newest first.
func (s *Service) Search(q Query) Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	res := Result{Entries: []Entry{}, Saving: s.days() > 0, Days: s.days()}
	var since time.Time
	if q.Since != "" {
		since, _ = time.Parse(time.RFC3339, q.Since)
	}
	words := strings.Fields(strings.ToLower(q.Text))
	limit := q.Limit
	if limit <= 0 || limit > maxResults {
		limit = maxResults
	}
	facets := [5]map[string]int{{}, {}, {}, {}, {}}
	for i := len(s.entries) - 1; i >= 0; i-- {
		e := s.entries[i]
		for k, v := range []string{e.KubeContext, e.AWSAccount, e.AWSRegion, e.AWSProfile, e.KubeNamespace} {
			if v != "" {
				facets[k][v]++
			}
		}
		if !match(e, q, words, since) {
			continue
		}
		res.Total++
		if len(res.Entries) < limit {
			res.Entries = append(res.Entries, e)
		}
	}
	list := func(m map[string]int) []Facet {
		out := []Facet{}
		for v, n := range m {
			out = append(out, Facet{v, n})
		}
		slices.SortFunc(out, func(a, b Facet) int {
			if a.Count != b.Count {
				return b.Count - a.Count
			}
			return strings.Compare(a.Value, b.Value)
		})
		return out
	}
	res.KubeContexts, res.AWSAccounts, res.AWSRegions, res.AWSProfiles, res.Namespaces =
		list(facets[0]), list(facets[1]), list(facets[2]), list(facets[3]), list(facets[4])
	return res
}

func match(e Entry, q Query, words []string, since time.Time) bool {
	switch {
	case q.KubeContext != "" && e.KubeContext != q.KubeContext,
		q.KubeNamespace != "" && e.KubeNamespace != q.KubeNamespace,
		q.AWSAccount != "" && e.AWSAccount != q.AWSAccount,
		q.AWSProfile != "" && e.AWSProfile != q.AWSProfile,
		q.AWSRegion != "" && e.AWSRegion != q.AWSRegion,
		!since.IsZero() && e.at.Before(since),
		q.Failed && e.ExitCode == 0:
		return false
	}
	text := strings.ToLower(e.Command + " " + e.Cwd)
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}

// ForgetSession removes the commands a shell of this app run saved (its
// History list was cleared).
func (s *Service) ForgetSession(session string) error {
	return s.forget(func(e Entry) bool { return e.Run == s.run && e.Session == session })
}

// Forget removes one entry.
func (s *Service) Forget(id string) error {
	return s.forget(func(e Entry) bool { return e.ID == id })
}

// ForgetAll removes every saved command.
func (s *Service) ForgetAll() error {
	if s.ReadOnly {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loaded = true
	s.entries = nil
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Service) forget(drop func(Entry) bool) error {
	if s.ReadOnly {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	n := len(s.entries)
	s.entries = slices.DeleteFunc(s.entries, drop)
	if len(s.entries) == n {
		return nil
	}
	return s.rewrite()
}

// Path is where the history is saved, for the manual and Settings.
func (s *Service) Path() string { return s.path }

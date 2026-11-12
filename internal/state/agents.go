package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Resuming agents after a restart (#38). A tab that ran Claude Code saves
// the agent with its pane (in the window's session); agents running in the
// background, which no window shows, are saved here when wtf quits. After
// a relaunch the UIs offer to resume them (claude --resume): never on
// their own.

const (
	backgroundFile = "background-agents.json"
	maxAgents      = 50
)

// Agent is a Claude Code session that can be resumed.
type Agent struct {
	// Claude Code's session id (its hooks' session_id).
	Session string `json:"session"`
	// The folder it was started in: claude --resume finds a session by it.
	Dir string `json:"dir"`
	// The name you gave it in the Agents tab ("" for none).
	Name string `json:"name"`
	// Its transcript (its hooks' transcript_path): gone means it can't be resumed.
	Transcript string `json:"transcript"`
}

var sessionID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,99}$`)

// Resumable: a well-formed id, its folder still there and its transcript too.
func Resumable(a Agent) bool {
	if !sessionID.MatchString(a.Session) || !filepath.IsAbs(a.Dir) || !filepath.IsAbs(a.Transcript) ||
		!strings.HasSuffix(a.Transcript, ".jsonl") {
		return false
	}
	if fi, err := os.Stat(a.Dir); err != nil || !fi.IsDir() {
		return false
	}
	fi, err := os.Stat(a.Transcript)
	return err == nil && fi.Mode().IsRegular()
}

// BackgroundAgents reads the background agents saved when wtf last quit,
// keeping the ones that can still be resumed.
func (s *Store) BackgroundAgents() []Agent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Agent{}
	data, err := os.ReadFile(filepath.Join(s.dir, backgroundFile))
	if err != nil {
		return out
	}
	var list []Agent
	if json.Unmarshal(data, &list) != nil {
		return out
	}
	for _, a := range list {
		if Resumable(a) && len(out) < maxAgents {
			out = append(out, a)
		}
	}
	return out
}

// SaveBackgroundAgents replaces the saved background agents (none removes
// the file).
func (s *Store) SaveBackgroundAgents(list []Agent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(list) == 0 {
		if err := os.Remove(filepath.Join(s.dir, backgroundFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if len(list) > maxAgents {
		list = list[:maxAgents]
	}
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return s.writeFile(backgroundFile, data)
}

// BackgroundAgents: the background agents saved when wtf last quit that
// can still be resumed.
func (s *Service) BackgroundAgents() []Agent { return s.store.BackgroundAgents() }

// SaveBackgroundAgents replaces them: when wtf quits, and after you resume
// or dismiss one.
func (s *Service) SaveBackgroundAgents(list []Agent) error { return s.store.SaveBackgroundAgents(list) }

// StillResumable says, for each agent restored with a tab, whether it can
// still be resumed (its transcript and folder are there).
func (s *Service) StillResumable(list []Agent) []bool {
	out := make([]bool, len(list))
	for i, a := range list {
		out[i] = Resumable(a)
	}
	return out
}

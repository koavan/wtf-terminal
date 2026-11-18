package bridge

import (
	"slices"
	"strings"

	"github.com/dayanraj/wtf/internal/events"
)

// Remote agents (#41): agents on a runner, whose own bridge (in
// wtf-runner's daemon) hears their hooks. internal/runner mirrors each one
// here under a session of its own (RemotePrefix…), so the UIs list them,
// float their approvals and answer them like any other; the answers go
// back to the runner through the agent's Forward.

// RemotePrefix starts a mirrored agent's session.
const RemotePrefix = "rr:"

// IsRemote says whether a session is a runner's agent.
func IsRemote(session string) bool { return strings.HasPrefix(session, RemotePrefix) }

// Forward sends an answer (Answer, AnswerQuestion, AllowAlways, DenyWith,
// AnswerText, AnswerAll, with their arguments after the approval's id as
// the runner knows it), Seen or Rename to the runner.
type Forward func(method, approval string, args ...any) error

// Mirror lists (or updates) a runner's agent: st as its runner's bridge
// last said, under session. Its approvals get ids of their own here. (A
// function, not a method, so the UIs don't call it.)
func Mirror(s *Service, session string, st Status, fwd Forward) {
	st.Session = session
	st.Approvals = slices.Clone(st.Approvals)
	for i := range st.Approvals {
		st.Approvals[i].ID = session + "/" + st.Approvals[i].ID
	}
	s.mu.Lock()
	if s.remotes == nil {
		s.remotes = map[string]Forward{}
	}
	s.remotes[session] = fwd
	if old := s.agents[session]; old != nil && old.Started != 0 {
		st.Started = old.Started
	}
	s.agents[session] = &st
	out := st.clone()
	s.mu.Unlock()
	events.Emit(EventStatus, out)
}

// Unmirror drops a runner's agent (it ended, or wtf lost its runner).
func Unmirror(s *Service, session string) {
	s.mu.Lock()
	a := s.agents[session]
	delete(s.agents, session)
	delete(s.remotes, session)
	s.mu.Unlock()
	if a != nil {
		events.Emit(EventStatus, Status{Session: session, Provider: a.Provider, State: StateEnded})
	}
}

// remoteApproval: a mirrored agent's approval, its Forward and its id on
// the runner.
func (s *Service) remoteApproval(id string) (Forward, string, bool) {
	session, orig, ok := cutLast(id, "/")
	if !ok || !IsRemote(session) {
		return nil, "", false
	}
	s.mu.Lock()
	f := s.remotes[session]
	s.mu.Unlock()
	return f, orig, f != nil
}

func (s *Service) remote(session string) Forward {
	if !IsRemote(session) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.remotes[session]
}

func cutLast(s, sep string) (before, after string, ok bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}

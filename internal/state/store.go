// Package state persists each window's session (tabs, groups, split
// layouts) so it can be restored on the next launch.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	sessionFile = "session.json"
	// A window's session is a few KB; anything this big is a bug.
	maxSessionBytes = 4 << 20
	fileVersion     = 2
)

// Window is one window's saved session.
type Window struct {
	Name    string          `json:"name"`
	Session json.RawMessage `json:"session"`
}

type file struct {
	Version int      `json:"version"`
	Windows []Window `json:"windows"`
}

// Store reads and writes the session file in a directory.
type Store struct {
	dir string
	mu  sync.Mutex
}

func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// DefaultDir is the per-user config directory for wtf,
// e.g. ~/Library/Application Support/wtf or ~/.config/wtf.
func DefaultDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "wtf"), nil
}

// Windows returns the saved windows in the order they were first saved.
// A missing or corrupt file means no windows: the app starts fresh rather
// than failing to launch.
func (s *Store) Windows() []Window {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

func (s *Store) read() []Window {
	b, err := os.ReadFile(filepath.Join(s.dir, sessionFile))
	if err != nil || !json.Valid(b) {
		return nil
	}
	var f file
	if json.Unmarshal(b, &f) == nil && f.Version == fileVersion {
		return f.Windows
	}
	// Version 1 held a single window's session at the top level.
	var legacy struct {
		Version int `json:"version"`
	}
	if json.Unmarshal(b, &legacy) == nil && legacy.Version == 1 {
		return []Window{{Name: "main", Session: b}}
	}
	return nil
}

// SaveWindow replaces one window's session, keeping the others.
func (s *Store) SaveWindow(name, data string) error {
	if len(data) > maxSessionBytes {
		return fmt.Errorf("session too large (%d bytes)", len(data))
	}
	if !json.Valid([]byte(data)) {
		return errors.New("session is not valid JSON")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	windows := s.read()
	found := false
	for i := range windows {
		if windows[i].Name == name {
			windows[i].Session = json.RawMessage(data)
			found = true
		}
	}
	if !found {
		windows = append(windows, Window{Name: name, Session: json.RawMessage(data)})
	}
	return s.write(windows)
}

// Forget drops a window that was closed for good.
func (s *Store) Forget(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	windows := s.read()
	kept := windows[:0]
	for _, w := range windows {
		if w.Name != name {
			kept = append(kept, w)
		}
	}
	return s.write(kept)
}

// write atomically replaces the session file, so a crash mid-write can
// never leave a half-written session behind.
func (s *Store) write(windows []Window) error {
	if windows == nil {
		windows = []Window{}
	}
	data, err := json.Marshal(file{Version: fileVersion, Windows: windows})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, sessionFile+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(s.dir, sessionFile))
}

// Service exposes the store to the frontend.
type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
}

// SaveWindow saves a window's session JSON.
func (s *Service) SaveWindow(name, data string) error { return s.store.SaveWindow(name, data) }

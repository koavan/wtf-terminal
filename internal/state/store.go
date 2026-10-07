// Package state persists the window's session (tabs, groups, split layouts)
// so it can be restored on the next launch.
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
	// A session is a few KB; anything this big is a bug, not a session.
	maxSessionBytes = 4 << 20
)

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

// Load returns the saved session, or "" when there is none.
func (s *Store) Load() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(filepath.Join(s.dir, sessionFile))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !json.Valid(b) {
		return "", nil // corrupt: start fresh rather than fail to launch
	}
	return string(b), nil
}

// Save atomically replaces the session file, so a crash mid-write can never
// leave a half-written session behind.
func (s *Store) Save(data string) error {
	if len(data) > maxSessionBytes {
		return fmt.Errorf("session too large (%d bytes)", len(data))
	}
	if !json.Valid([]byte(data)) {
		return errors.New("session is not valid JSON")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, sessionFile+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.WriteString(data); err != nil {
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

// LoadSession returns the saved session JSON, or "" when there is none.
func (s *Service) LoadSession() (string, error) { return s.store.Load() }

// SaveSession replaces the saved session with the given JSON.
func (s *Service) SaveSession(data string) error { return s.store.Save(data) }

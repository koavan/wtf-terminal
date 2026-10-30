package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dayanraj/wtf/internal/config"
	"github.com/zalando/go-keyring"
)

// Keys live in the macOS Keychain or the Linux Secret Service under this
// service name, one per provider name.
const keyringService = "wtf AI"

// seenFile records which providers the user has seen a preview for.
const seenFile = "ai-previewed.json"

type keyLookup struct {
	key  string
	from string
	err  error
}

// key finds a provider's API key: the variable named by api-key-env, else
// the keychain. Keychain answers are cached until SetKey or DeleteKey.
func (s *Service) key(name string, p config.AIProvider) keyLookup {
	if p.APIKeyEnv != "" {
		return keyLookup{key: strings.TrimSpace(os.Getenv(p.APIKeyEnv)), from: "$" + p.APIKeyEnv}
	}
	s.mu.Lock()
	k, ok := s.keys[name]
	s.mu.Unlock()
	if ok {
		return k
	}
	v, err := keyring.Get(keyringService, name)
	switch {
	case err == nil:
		k = keyLookup{key: v, from: "keychain"}
	case errors.Is(err, keyring.ErrNotFound):
		k = keyLookup{}
	default:
		// Don't cache: the keychain may be locked, or come up later.
		return keyLookup{err: fmt.Errorf("reading the API key for %s: %w", name, err)}
	}
	s.mu.Lock()
	s.keys[name] = k
	s.mu.Unlock()
	return k
}

// SetKey stores a provider's API key in the keychain.
func (s *Service) SetKey(name, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("enter a key")
	}
	if _, ok := s.config().AI.Providers[name]; !ok {
		return fmt.Errorf("there's no AI provider named %q", name)
	}
	if err := keyring.Set(keyringService, name, key); err != nil {
		return keyringError(err)
	}
	s.mu.Lock()
	s.keys[name] = keyLookup{key: key, from: "keychain"}
	s.mu.Unlock()
	return nil
}

// DeleteKey removes a provider's API key from the keychain.
func (s *Service) DeleteKey(name string) error {
	err := keyring.Delete(keyringService, name)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return keyringError(err)
	}
	s.mu.Lock()
	s.keys[name] = keyLookup{}
	s.mu.Unlock()
	return nil
}

func keyringError(err error) error {
	return fmt.Errorf("couldn't use the keychain (%v); set api-key-env in config.toml to read the key from an environment variable instead", err)
}

func (s *Service) wasSeen(name, destination string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadSeen()
	return s.seen[name] == destination
}

func (s *Service) markSeen(name, destination string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadSeen()
	s.seen[name] = destination
	if s.stateDir == "" {
		return
	}
	data, err := json.MarshalIndent(s.seen, "", "  ")
	if err == nil {
		_ = os.MkdirAll(s.stateDir, 0o700)
		_ = os.WriteFile(filepath.Join(s.stateDir, seenFile), data, 0o600)
	}
}

// loadSeen reads seenFile once. Callers hold s.mu.
func (s *Service) loadSeen() {
	if s.seen != nil {
		return
	}
	s.seen = map[string]string{}
	if s.stateDir == "" {
		return
	}
	if data, err := os.ReadFile(filepath.Join(s.stateDir, seenFile)); err == nil {
		_ = json.Unmarshal(data, &s.seen)
	}
}

func sortedNames(m map[string]config.AIProvider) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

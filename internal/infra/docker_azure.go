package infra

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Docker and Azure aren't badges; the Approvals inbox (#40) reads where a
// docker or az command would act from the same kind of files: nothing runs,
// nothing is asked of a cloud.

// DockerContext is the docker context a command uses and where its daemon
// is. name is the context the command names ("" for none): else
// DOCKER_HOST (endpoint only, no context), DOCKER_CONTEXT, then the CLI
// config's currentContext, else "default", the local socket.
func (s *Service) DockerContext(env map[string]string, name string) (context, endpoint string) {
	if name == "" {
		if h := env["DOCKER_HOST"]; h != "" && env["DOCKER_CONTEXT"] == "" {
			return "", h
		}
		name = env["DOCKER_CONTEXT"]
	}
	dir := s.dockerDir(env)
	if name == "" && dir != "" {
		var cfg struct {
			CurrentContext string `json:"currentContext"`
		}
		if b := readSmallFile(filepath.Join(dir, "config.json")); b != nil {
			json.Unmarshal(b, &cfg)
		}
		name = cfg.CurrentContext
	}
	if name == "" || name == "default" {
		return "default", "the local daemon"
	}
	if dir == "" {
		return name, ""
	}
	sum := sha256.Sum256([]byte(name))
	var meta struct {
		Endpoints map[string]struct {
			Host string `json:"Host"`
		} `json:"Endpoints"`
	}
	if b := readSmallFile(filepath.Join(dir, "contexts", "meta", hex.EncodeToString(sum[:]), "meta.json")); b != nil {
		json.Unmarshal(b, &meta)
	}
	return name, meta.Endpoints["docker"].Host
}

func (s *Service) dockerDir(env map[string]string) string {
	if d := abs(env["DOCKER_CONFIG"], env); d != "" {
		return d
	}
	if s.home == "" {
		return ""
	}
	return filepath.Join(s.home, ".docker")
}

// AzureSubscription is the Azure subscription a command uses, from the
// CLI's azureProfile.json: want (a command's --subscription, a name or an
// id), else the default one. ok is false when there's no such subscription
// or no profile.
func (s *Service) AzureSubscription(env map[string]string, want string) (name, id string, ok bool) {
	dir := abs(env["AZURE_CONFIG_DIR"], env)
	if dir == "" && s.home != "" {
		dir = filepath.Join(s.home, ".azure")
	}
	b := readSmallFile(filepath.Join(dir, "azureProfile.json"))
	if b == nil {
		return "", "", false
	}
	var profile struct {
		Subscriptions []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			IsDefault bool   `json:"isDefault"`
		} `json:"subscriptions"`
	}
	// The CLI writes it with a byte order mark.
	if json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &profile) != nil {
		return "", "", false
	}
	for _, sub := range profile.Subscriptions {
		if (want == "" && sub.IsDefault) || (want != "" && (strings.EqualFold(sub.ID, want) || sub.Name == want)) {
			return sub.Name, sub.ID, true
		}
	}
	return "", "", false
}

// readSmallFile reads a file up to maxFileSize; nil when it can't.
func readSmallFile(path string) []byte {
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxFileSize {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return b
}

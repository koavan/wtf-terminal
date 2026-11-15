package infra

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestDockerContext(t *testing.T) {
	home := t.TempDir()
	s := newService(home, nil, func() {})
	env := map[string]string{}
	if c, e := s.DockerContext(env, ""); c != "default" || e != "the local daemon" {
		t.Errorf("no config: %q %q", c, e)
	}
	dir := filepath.Join(home, ".docker")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"auths":{},"currentContext":"prod"}`), 0o600)
	sum := sha256.Sum256([]byte("prod"))
	meta := filepath.Join(dir, "contexts", "meta", hex.EncodeToString(sum[:]))
	os.MkdirAll(meta, 0o755)
	os.WriteFile(filepath.Join(meta, "meta.json"), []byte(`{"Name":"prod","Endpoints":{"docker":{"Host":"ssh://deploy@prod-1"}}}`), 0o644)
	if c, e := s.DockerContext(env, ""); c != "prod" || e != "ssh://deploy@prod-1" {
		t.Errorf("current context: %q %q", c, e)
	}
	if c, e := s.DockerContext(env, "default"); c != "default" || e != "the local daemon" {
		t.Errorf("--context default: %q %q", c, e)
	}
	if c, e := s.DockerContext(map[string]string{"DOCKER_HOST": "tcp://10.0.0.5:2376"}, ""); c != "" || e != "tcp://10.0.0.5:2376" {
		t.Errorf("DOCKER_HOST: %q %q", c, e)
	}
	if c, e := s.DockerContext(map[string]string{"DOCKER_CONTEXT": "other"}, ""); c != "other" || e != "" {
		t.Errorf("unknown DOCKER_CONTEXT: %q %q", c, e)
	}
}

func TestAzureSubscription(t *testing.T) {
	home := t.TempDir()
	s := newService(home, nil, func() {})
	if _, _, ok := s.AzureSubscription(map[string]string{}, ""); ok {
		t.Error("a subscription without a profile")
	}
	os.MkdirAll(filepath.Join(home, ".azure"), 0o755)
	os.WriteFile(filepath.Join(home, ".azure", "azureProfile.json"), []byte("\xef\xbb\xbf"+`{"installationId":"x","subscriptions":[
		{"id":"1111-aaaa","name":"Dev","isDefault":false},{"id":"2222-bbbb","name":"Prod","isDefault":true}]}`), 0o600)
	for _, c := range []struct{ want, name, id string }{{"", "Prod", "2222-bbbb"}, {"Dev", "Dev", "1111-aaaa"}, {"1111-AAAA", "Dev", "1111-aaaa"}} {
		if n, id, ok := s.AzureSubscription(map[string]string{}, c.want); !ok || n != c.name || id != c.id {
			t.Errorf("%q: %q %q %v", c.want, n, id, ok)
		}
	}
	if _, _, ok := s.AzureSubscription(map[string]string{}, "nope"); ok {
		t.Error("an unknown subscription")
	}
}

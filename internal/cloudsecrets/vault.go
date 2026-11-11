package cloudsecrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/dayanraj/wtf/internal/tools"
)

// ── Vault ────────────────────────────────────────────────────────────────

// Vault variables taken from the shell. VAULT_TOKEN is never among them:
// the shell doesn't report it, and the CLI's token helper supplies one.
var vaultVars = []string{"VAULT_ADDR", "VAULT_NAMESPACE", "VAULT_CACERT"}

// vault runs the vault CLI with the shell's server and namespace.
func (s *Service) vault(ctx context.Context, r Request, stdin []byte, args ...string) ([]byte, error) {
	bin := tools.Find("vault", r.Env["PATH"])
	if bin == "" {
		return nil, errors.New("the vault CLI isn't installed")
	}
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "VAULT_") {
			env = append(env, kv)
		}
	}
	for _, name := range vaultVars {
		if v := r.Env[name]; v != "" && !strings.ContainsAny(v, "\x00\n") {
			env = append(env, name+"="+v)
		}
	}
	env = withVar(env, "PATH", shellPath(r.Env))
	out, err := runCLI(ctx, bin, env, stdin, args...)
	if err != nil {
		return nil, vaultErr(err)
	}
	return out, nil
}

// vaultErr turns the CLI's "Error making API request … Code: 403. Errors:
// * permission denied" into "permission denied (403)".
func vaultErr(err error) error {
	var ce *cliError
	if !errors.As(err, &ce) {
		return err
	}
	var code string
	var msgs []string
	for _, l := range strings.Split(ce.stderr, "\n") {
		l = strings.TrimSpace(l)
		if c, ok := strings.CutPrefix(l, "Code: "); ok {
			code, _, _ = strings.Cut(c, ".")
		} else if m, ok := strings.CutPrefix(l, "* "); ok && !strings.HasSuffix(m, "occurred:") {
			msgs = append(msgs, m)
		}
	}
	if len(msgs) == 0 {
		if strings.Contains(ce.stderr, "missing client token") {
			return errors.New("not logged in to Vault: run vault login in the shell")
		}
		return err
	}
	msg := strings.Join(msgs, "; ")
	if code != "" {
		msg += " (" + code + ")"
	}
	return errors.New(msg)
}

func notFound(err error) bool {
	var ce *cliError
	return errors.As(err, &ce) && strings.Contains(ce.stderr, "No value found at")
}

func validPath(p string) error {
	if strings.Trim(p, "/") == "" {
		return errors.New("no Vault path")
	}
	return validName("Vault path", p)
}

// VaultMounts lists the server's key/value secrets engines. Users who may
// not list mounts get an error; the tab then takes a typed path.
func (s *Service) VaultMounts(r Request) ([]Mount, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := s.vault(ctx, r, nil, "secrets", "list", "-format=json")
	if err != nil {
		return nil, err
	}
	var res map[string]struct {
		Type        string            `json:"type"`
		Description string            `json:"description"`
		Options     map[string]string `json:"options"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("unexpected answer from Vault: %.60q", out)
	}
	mounts := []Mount{}
	for path, m := range res {
		if m.Type != "kv" && m.Type != "generic" {
			continue
		}
		kv := 1
		if m.Options["version"] == "2" {
			kv = 2
		}
		mounts = append(mounts, Mount{Path: path, KV: kv, Description: m.Description})
	}
	sort.Slice(mounts, func(i, j int) bool { return mounts[i].Path < mounts[j].Path })
	return mounts, nil
}

// VaultList lists a folder: secrets, and folders (Folder, names ending in /).
func (s *Service) VaultList(r Request, path string) ([]Item, error) {
	if err := validPath(path); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := s.vault(ctx, r, nil, "kv", "list", "-format=json", path)
	if notFound(err) {
		return []Item{}, nil
	}
	if err != nil {
		return nil, err
	}
	var keys []string
	if err := json.Unmarshal(out, &keys); err != nil {
		return nil, fmt.Errorf("unexpected answer from Vault: %.60q", out)
	}
	items := []Item{}
	for _, k := range keys {
		items = append(items, Item{Name: k, Folder: strings.HasSuffix(k, "/")})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Folder != items[j].Folder {
			return items[i].Folder
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

// VaultRead reads a secret's latest version. kv is the mount's KV version.
func (s *Service) VaultRead(r Request, path string, kv int) (Value, error) {
	if err := validPath(path); err != nil {
		return Value{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := s.vault(ctx, r, nil, "kv", "get", "-format=json", path)
	if err != nil {
		return Value{}, err
	}
	return parseVaultRead(path, out, kv)
}

func parseVaultRead(path string, out []byte, kv int) (Value, error) {
	v := Value{Name: path, Fields: []Field{}}
	var res struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return v, fmt.Errorf("unexpected answer from Vault: %.60q", out)
	}
	data := res.Data
	if kv == 2 {
		var d struct {
			Data     json.RawMessage `json:"data"`
			Metadata struct {
				Version      int64  `json:"version"`
				CreatedTime  string `json:"created_time"`
				DeletionTime string `json:"deletion_time"`
				Destroyed    bool   `json:"destroyed"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(res.Data, &d); err != nil {
			return v, fmt.Errorf("unexpected answer from Vault: %.60q", out)
		}
		if d.Metadata.Destroyed || d.Metadata.DeletionTime != "" || string(d.Data) == "null" {
			return v, fmt.Errorf("version %d of this secret is deleted", d.Metadata.Version)
		}
		data, v.Version, v.Changed = d.Data, fmt.Sprint(d.Metadata.Version), d.Metadata.CreatedTime
	}
	var obj map[string]any
	if json.Unmarshal(data, &obj) != nil {
		return v, fmt.Errorf("unexpected answer from Vault: %.60q", out)
	}
	pretty, _ := json.MarshalIndent(obj, "", "  ")
	v.Text = string(pretty)
	v.Fields = fieldsOf(v.Text)
	return v, nil
}

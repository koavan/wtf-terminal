// Package cloudsecrets reads and changes secrets in AWS Secrets Manager,
// AWS Systems Manager Parameter Store and HashiCorp Vault for the Cloud
// Secrets side-pane tab (#18).
//
// Like the Kubernetes tab, it takes the focused shell's view: its AWS
// profile and region, its VAULT_ADDR and VAULT_NAMESPACE, and its PATH
// (so credential helpers are found). Vault's token comes from the vault
// CLI's own token helper (~/.vault-token after `vault login`); a
// VAULT_TOKEN in the shell is never reported to wtf, so it isn't used.
//
// Lists hold names and metadata only. A value is read only when the user
// opens that secret, goes back to the dialog that asked, and is kept
// nowhere. Every write is planned first (no values in the plan), confirmed
// by the user, and checked against the version they saw, so a change made
// elsewhere in the meantime isn't overwritten. Values reach the CLIs on
// stdin, never on the command line.
package cloudsecrets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/infra"
	"github.com/dayanraj/wtf/internal/tools"
)

// Stores, as Change.Store and the tab's source picker name them.
const (
	SecretsManager = "secretsmanager"
	ParameterStore = "ssm"
	Vault          = "vault"
)

const (
	timeout  = 30 * time.Second
	maxItems = "2000"
)

// Request says whose view to take.
type Request struct {
	// The variables the shell reported (PATH, AWS_*, VAULT_ADDR…).
	Env map[string]string `json:"env"`
	// A region picked in the tab; "" follows the shell's.
	Region string `json:"region"`
}

// Target is where a request's calls go.
type Target struct {
	Profile string `json:"profile"`
	Account string `json:"account"`
	Region  string `json:"region"`
	// Why AWS can't be browsed from this shell; "" when it can.
	AWSError string `json:"awsError"`
	// "" when the shell doesn't set VAULT_ADDR (the CLI's default then applies).
	VaultAddr      string `json:"vaultAddr"`
	VaultNamespace string `json:"vaultNamespace"`
}

// Item is one row of a list: a secret, a parameter, or a Vault entry.
type Item struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// When it last changed / was last read, as AWS reports them.
	Changed  string `json:"changed"`
	Accessed string `json:"accessed"`
	// Parameter Store: String, StringList or SecureString.
	Type    string `json:"type"`
	Version string `json:"version"`
	// Secrets Manager: automatic rotation is on.
	Rotation bool `json:"rotation"`
	// Vault: a folder to open, not a secret.
	Folder bool `json:"folder"`
}

// Field is one key of a value that's a JSON object.
type Field struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Value is one secret's value, read when the user asks to see it.
type Value struct {
	Name string `json:"name"`
	// The value as stored: text, or base64 for binary data. Vault's is
	// its key/value data as indented JSON.
	Text   string `json:"text"`
	Binary bool   `json:"binary"`
	// The fields, when Text is a JSON object.
	Fields []Field `json:"fields"`
	// The version read: Secrets Manager's VersionId, Parameter Store's or
	// Vault KV 2's version number ("" for KV 1).
	Version string `json:"version"`
	Type    string `json:"type"`
	Changed string `json:"changed"`
}

// Mount is a Vault key/value secrets engine.
type Mount struct {
	Path        string `json:"path"`
	KV          int    `json:"kv"`
	Description string `json:"description"`
}

type Service struct {
	// The shell's AWS profile, account and region (infra.Service.Resolve).
	resolve func(env map[string]string) infra.Context
	home    string
}

func NewService(i *infra.Service) *Service {
	home, _ := os.UserHomeDir()
	return &Service{resolve: i.Resolve, home: home}
}

// runCLI runs aws or vault with stdin and returns stdout; tests replace it.
var runCLI = func(ctx context.Context, bin string, env []string, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, errors.New("no answer in time")
	}
	if err != nil {
		return nil, &cliError{stderr: stderr.String(), err: err}
	}
	return out, nil
}

type cliError struct {
	stderr string
	err    error
}

func (e *cliError) Error() string { return lastLine(e.stderr, e.err.Error()) }

func lastLine(s, fallback string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	line := strings.TrimSpace(lines[len(lines)-1])
	if line == "" {
		return fallback
	}
	if len(line) > 300 {
		line = line[:300] + "…"
	}
	return line
}

// Target works out the profile, region and Vault server a request means.
// It reads local files only.
func (s *Service) Target(r Request) Target {
	c := s.resolve(r.Env)
	t := Target{Profile: c.AWSProfile, Account: c.AWSAccount, Region: c.AWSRegion,
		VaultAddr: r.Env["VAULT_ADDR"], VaultNamespace: r.Env["VAULT_NAMESPACE"]}
	if r.Region != "" {
		t.Region = r.Region
	}
	switch {
	case t.Profile == infra.EnvCredentials:
		t.AWSError = "This shell's AWS credentials are environment variables, which wtf never reads. Use a profile (AWS_PROFILE) to browse its secrets here."
	case t.Profile == "":
		t.AWSError = "This shell has no AWS profile. Pick one in the AWS Profiles tab (Use in This Shell)."
	case t.Region == "":
		t.AWSError = "No region: the profile doesn't set one. Pick a region above."
	}
	return t
}

// ── AWS ──────────────────────────────────────────────────────────────────

// shellPath is the shell's PATH, else wtf's.
func shellPath(env map[string]string) string {
	if p := env["PATH"]; p != "" {
		return p
	}
	return os.Getenv("PATH")
}

func (s *Service) awsFile(env map[string]string, variable, name string) string {
	p := env[variable]
	switch {
	case p == "":
		return filepath.Join(s.home, ".aws", name)
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(s.home, p[2:])
	case !filepath.IsAbs(p) && env["PWD"] != "":
		return filepath.Join(env["PWD"], p)
	}
	return p
}

func validRegion(r string) bool {
	if r == "" || len(r) > 30 {
		return false
	}
	for _, c := range r {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// aws runs the aws CLI as the shell's profile, in the target region.
func (s *Service) aws(ctx context.Context, r Request, stdin []byte, args ...string) ([]byte, Target, error) {
	t := s.Target(r)
	if t.AWSError != "" {
		return nil, t, errors.New(t.AWSError)
	}
	if !validRegion(t.Region) || strings.HasPrefix(t.Profile, "-") {
		return nil, t, fmt.Errorf("%q isn't a region", t.Region)
	}
	bin := tools.Find("aws", r.Env["PATH"])
	if bin == "" {
		return nil, t, errors.New("the aws CLI isn't installed")
	}
	env := infra.AWSEnv(s.awsFile(r.Env, "AWS_CONFIG_FILE", "config"), s.awsFile(r.Env, "AWS_SHARED_CREDENTIALS_FILE", "credentials"))
	env = withVar(env, "PATH", shellPath(r.Env))
	args = append(args, "--profile", t.Profile, "--region", t.Region, "--output", "json")
	out, err := runCLI(ctx, bin, env, stdin, args...)
	return out, t, err
}

// withVar sets one variable in an environment list.
func withVar(env []string, name, value string) []string {
	out := env[:0:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, name+"=") {
			out = append(out, kv)
		}
	}
	return append(out, name+"="+value)
}

// validName refuses names that would read as options or hold control characters.
func validName(kind, n string) error {
	if n == "" || strings.HasPrefix(n, "-") || len(n) > 2048 || strings.ContainsFunc(n, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return fmt.Errorf("%q isn't a %s name", n, kind)
	}
	return nil
}

// ListSecrets lists Secrets Manager's secrets: names and metadata, never values.
func (s *Service) ListSecrets(r Request) ([]Item, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, _, err := s.aws(ctx, r, nil, "secretsmanager", "list-secrets", "--max-items", maxItems)
	if err != nil {
		return nil, err
	}
	var res struct {
		SecretList []struct {
			Name, Description, LastChangedDate, LastAccessedDate string
			RotationEnabled                                      bool
		}
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("unexpected answer from AWS: %.60q", out)
	}
	items := []Item{}
	for _, x := range res.SecretList {
		items = append(items, Item{Name: x.Name, Description: x.Description, Changed: x.LastChangedDate, Accessed: x.LastAccessedDate, Rotation: x.RotationEnabled})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

// ListParameters lists Parameter Store's parameters, without their values.
func (s *Service) ListParameters(r Request) ([]Item, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, _, err := s.aws(ctx, r, nil, "ssm", "describe-parameters", "--max-items", maxItems)
	if err != nil {
		return nil, err
	}
	var res struct{ Parameters []parameter }
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("unexpected answer from AWS: %.60q", out)
	}
	items := []Item{}
	for _, p := range res.Parameters {
		items = append(items, Item{Name: p.Name, Description: p.Description, Changed: p.LastModifiedDate, Type: p.Type, Version: fmt.Sprint(p.Version)})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

type parameter struct {
	Name, Type, KeyId, LastModifiedDate, Description, Tier, DataType string
	Version                                                          int64
}

// ReadSecret reads one secret's current value.
func (s *Service) ReadSecret(r Request, name string) (Value, error) {
	if err := validName("secret", name); err != nil {
		return Value{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, _, err := s.aws(ctx, r, nil, "secretsmanager", "get-secret-value", "--secret-id", name)
	if err != nil {
		return Value{}, err
	}
	var res struct {
		Name, VersionId, SecretString, SecretBinary, CreatedDate string
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return Value{}, errors.New("unexpected answer from AWS (get-secret-value)")
	}
	v := Value{Name: res.Name, Version: res.VersionId, Changed: res.CreatedDate, Fields: []Field{}}
	if res.SecretBinary != "" && res.SecretString == "" {
		v.Text, v.Binary = res.SecretBinary, true
	} else {
		v.Text = res.SecretString
		v.Fields = fieldsOf(v.Text)
	}
	return v, nil
}

// ReadParameter reads one parameter, decrypting a SecureString.
func (s *Service) ReadParameter(r Request, name string) (Value, error) {
	if err := validName("parameter", name); err != nil {
		return Value{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, _, err := s.aws(ctx, r, nil, "ssm", "get-parameter", "--name", name, "--with-decryption")
	if err != nil {
		return Value{}, err
	}
	var res struct {
		Parameter struct {
			Name, Type, Value, LastModifiedDate string
			Version                             int64
		}
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return Value{}, errors.New("unexpected answer from AWS (get-parameter)")
	}
	p := res.Parameter
	return Value{Name: p.Name, Text: p.Value, Fields: fieldsOf(p.Value), Version: fmt.Sprint(p.Version), Type: p.Type, Changed: p.LastModifiedDate}, nil
}

// fieldsOf lists a JSON object's keys and values (nested values as JSON),
// sorted by key; anything else has no fields.
func fieldsOf(text string) []Field {
	fields := []Field{}
	var obj map[string]json.RawMessage
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "{") || json.Unmarshal([]byte(t), &obj) != nil {
		return fields
	}
	for k, raw := range obj {
		var str string
		if json.Unmarshal(raw, &str) == nil {
			fields = append(fields, Field{Key: k, Value: str})
		} else {
			fields = append(fields, Field{Key: k, Value: string(raw)})
		}
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Key < fields[j].Key })
	return fields
}

// Package awsprofiles manages the AWS CLI's named profiles for the AWS
// side-pane tab: the access keys in ~/.aws/credentials and the settings in
// ~/.aws/config (region, assumed roles, SSO…).
//
// Secrets never leave this package: the list says whether a profile has a
// secret key or session token, never what it is, and a draft's empty
// secret means "keep the current one".
//
// Every change can be planned first — a list of what would change, in
// words — and the frontend asks the user to confirm that list before
// saving. Saving checks that the files are still the versions that were
// listed, keeps a copy of the old files in wtf's cache, and writes
// atomically, keeping each file's permissions.
package awsprofiles

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/paths"
)

// Setting is one "key = value" line, as written.
type Setting struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Profile struct {
	Name string `json:"name"`
	// keys (access keys), role (assumes a role), sso, process
	// (credential_process), or none.
	Kind          string `json:"kind"`
	InCredentials bool   `json:"inCredentials"`
	InConfig      bool   `json:"inConfig"`

	AccessKeyID     string `json:"accessKeyId"`
	HasSecret       bool   `json:"hasSecret"`
	HasSessionToken bool   `json:"hasSessionToken"`
	// Other lines of its credentials section; values of secret-looking
	// keys are replaced by "(hidden)".
	CredentialsOther []Setting `json:"credentialsOther"`

	Region string `json:"region"`
	Output string `json:"output"`
	// Its ~/.aws/config section's settings as written, region and output
	// included. Keys with a nested block are listed in Locked instead and
	// are never changed.
	Settings []Setting `json:"settings"`
	Locked   []string  `json:"locked"`

	// The account its config names (role_arn, sso_account_id), if any.
	Account string `json:"account"`
	// Profiles that use it as their source_profile.
	SourceFor []string `json:"sourceFor"`
}

type List struct {
	Profiles        []Profile         `json:"profiles"`
	CredentialsFile string            `json:"credentialsFile"`
	ConfigFile      string            `json:"configFile"`
	Versions        map[string]string `json:"versions"`
	Problems        []string          `json:"problems"`
}

// Draft is a profile to save.
type Draft struct {
	// The profile being changed; "" adds one.
	Original string `json:"original"`
	Name     string `json:"name"`
	// File versions from List.
	Versions map[string]string `json:"versions"`

	// Whether the profile has access keys in the credentials file.
	UseKeys     bool   `json:"useKeys"`
	AccessKeyID string `json:"accessKeyId"`
	// Empty keeps the current secret (only when the key ID is unchanged).
	SecretAccessKey string `json:"secretAccessKey"`
	// Empty keeps the current token; RemoveSessionToken drops it.
	SessionToken       string `json:"sessionToken"`
	RemoveSessionToken bool   `json:"removeSessionToken"`

	// The config settings it should have, all of them (locked keys aside).
	Settings []Setting `json:"settings"`
}

const (
	maxBackups = 20
	maxFile    = 1 << 20

	keyID     = "aws_access_key_id"
	keySecret = "aws_secret_access_key"
	keyToken  = "aws_session_token"
	// The old name for the session token, still read by the CLI.
	keyTokenOld = "aws_security_token"
)

var (
	errChanged  = errors.New("the AWS files changed since the list was loaded — reload and try again")
	accessKeyRe = regexp.MustCompile(`^[A-Z0-9]{16,128}$`)
	settingRe   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]*$`)
	regionRe    = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-\d+$`)
	accountRe   = regexp.MustCompile(`^arn:aws[\w-]*:iam::(\d{12}):`)
)

type Service struct {
	home        string
	credentials string
	config      string
	backupDir   string
}

func NewService() *Service {
	home, _ := os.UserHomeDir()
	s := &Service{home: home}
	s.credentials = envPath("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(home, ".aws", "credentials"))
	s.config = envPath("AWS_CONFIG_FILE", filepath.Join(home, ".aws", "config"))
	if cache, err := paths.UserCacheDir(); err == nil {
		s.backupDir = filepath.Join(cache, "wtf", "aws-backups")
	}
	return s
}

func envPath(name, fallback string) string {
	if p := os.Getenv(name); p != "" && filepath.IsAbs(p) {
		return p
	}
	return fallback
}

func version(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:12])
}

func configSection(name string) string {
	if name == "default" {
		return "default"
	}
	return "profile " + name
}

// files are the two documents as read, and their versions.
type files struct {
	creds, config         *doc
	credsData, configData []byte
	credsOK, configOK     bool
}

func (s *Service) read() (*files, []string) {
	f := &files{}
	var problems []string
	load := func(path string) ([]byte, bool) {
		fi, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil, false
		}
		if err == nil && fi.Size() > maxFile {
			err = fmt.Errorf("%s is too big to be an AWS file", path)
		}
		var data []byte
		if err == nil {
			data, err = os.ReadFile(path)
		}
		if err != nil {
			problems = append(problems, err.Error())
			return nil, false
		}
		return data, true
	}
	f.credsData, f.credsOK = load(s.credentials)
	f.configData, f.configOK = load(s.config)
	f.creds, f.config = newDoc(f.credsData), newDoc(f.configData)
	return f, problems
}

func (f *files) versions(s *Service) map[string]string {
	v := map[string]string{s.credentials: "", s.config: ""}
	if f.credsOK {
		v[s.credentials] = version(f.credsData)
	}
	if f.configOK {
		v[s.config] = version(f.configData)
	}
	return v
}

// List reads the profiles afresh; the files are small.
func (s *Service) List() List {
	f, problems := s.read()
	out := List{Profiles: []Profile{}, CredentialsFile: s.credentials, ConfigFile: s.config, Versions: f.versions(s), Problems: problems}
	if out.Problems == nil {
		out.Problems = []string{}
	}

	byName := map[string]*Profile{}
	get := func(name string) *Profile {
		if p := byName[name]; p != nil {
			return p
		}
		p := &Profile{Name: name, CredentialsOther: []Setting{}, Settings: []Setting{}, Locked: []string{}, SourceFor: []string{}}
		byName[name] = p
		return p
	}
	for _, sec := range f.creds.sections() {
		p := get(sec.name)
		p.InCredentials = true
		for _, k := range sec.keys {
			switch k.key {
			case keyID:
				p.AccessKeyID = k.value
			case keySecret:
				p.HasSecret = k.value != ""
			case keyToken, keyTokenOld:
				p.HasSessionToken = p.HasSessionToken || k.value != ""
			default:
				p.CredentialsOther = append(p.CredentialsOther, Setting{Key: keyName(k), Value: maskValue(k.key, k.value)})
			}
		}
	}
	for _, sec := range f.config.sections() {
		name, ok := strings.CutPrefix(sec.name, "profile ")
		if !ok && sec.name != "default" {
			continue // sso-session, services…
		}
		p := get(strings.TrimSpace(name))
		p.InConfig = true
		for _, k := range sec.keys {
			if k.nested {
				p.Locked = append(p.Locked, keyName(k))
				continue
			}
			p.Settings = append(p.Settings, Setting{Key: keyName(k), Value: k.value})
			switch k.key {
			case "region":
				p.Region = k.value
			case "output":
				p.Output = k.value
			}
		}
	}

	for _, p := range byName {
		p.Kind = kindOf(p)
		if m := accountRe.FindStringSubmatch(setting(p.Settings, "role_arn")); m != nil {
			p.Account = m[1]
		} else {
			p.Account = setting(p.Settings, "sso_account_id")
		}
		if src := setting(p.Settings, "source_profile"); src != "" && byName[src] != nil {
			byName[src].SourceFor = append(byName[src].SourceFor, p.Name)
		}
	}
	for _, p := range byName {
		sort.Strings(p.SourceFor)
		out.Profiles = append(out.Profiles, *p)
	}
	sort.Slice(out.Profiles, func(i, j int) bool {
		a, b := out.Profiles[i].Name, out.Profiles[j].Name
		if (a == "default") != (b == "default") {
			return a == "default"
		}
		return strings.ToLower(a) < strings.ToLower(b)
	})
	return out
}

func kindOf(p *Profile) string {
	switch {
	case setting(p.Settings, "role_arn") != "":
		return "role"
	case setting(p.Settings, "sso_start_url") != "" || setting(p.Settings, "sso_session") != "":
		return "sso"
	case setting(p.Settings, "credential_process") != "":
		return "process"
	case p.AccessKeyID != "" || p.HasSecret:
		return "keys"
	}
	return "none"
}

// setting finds a setting by key, ignoring case.
func setting(list []Setting, key string) string {
	for _, s := range list {
		if strings.EqualFold(s.Key, key) {
			return s.Value
		}
	}
	return ""
}

// keyName is a key as written in its line.
func keyName(k *keyLine) string {
	return strings.TrimSpace(strings.TrimRight(strings.TrimSpace(k.prefix), "="))
}

func maskValue(key, value string) string {
	if strings.Contains(key, "secret") || strings.Contains(key, "token") || strings.Contains(key, "password") {
		return "(hidden)"
	}
	return value
}

// ── Changes ──────────────────────────────────────────────────────────────

// change is one edit to the files being prepared, with its description.
type change struct {
	s       *Service
	f       *files
	lines   []string
	touched map[string]bool
}

func (c *change) note(file, format string, args ...any) {
	c.lines = append(c.lines, prettyHome(c.s.home, file)+": "+fmt.Sprintf(format, args...))
	c.touched[file] = true
}

func prettyHome(home, path string) string {
	if home != "" && strings.HasPrefix(path, home+"/") {
		return "~" + path[len(home):]
	}
	return path
}

// shortKey shows an access key ID as AWS's console does in lists.
func shortKey(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:4] + "…" + id[len(id)-4:]
}

// begin reads the files and checks they're the versions the caller saw.
func (s *Service) begin(versions map[string]string) (*change, error) {
	f, problems := s.read()
	if len(problems) > 0 {
		return nil, errors.New(problems[0])
	}
	current := f.versions(s)
	for _, path := range []string{s.credentials, s.config} {
		if want, ok := versions[path]; !ok || want != current[path] {
			return nil, errChanged
		}
	}
	return &change{s: s, f: f, touched: map[string]bool{}}, nil
}

func validName(name string) error {
	switch {
	case name == "":
		return errors.New("give the profile a name")
	case strings.TrimSpace(name) != name || strings.ContainsAny(name, "[]#;=\r\n\t"):
		return fmt.Errorf("%q can't contain brackets, #, ;, = or surrounding spaces", name)
	case strings.HasPrefix(name, "profile ") || strings.HasPrefix(name, "sso-session "):
		return fmt.Errorf("%q would be read as a different kind of section", name)
	}
	return nil
}

// exists says whether a profile is defined in either file.
func (c *change) exists(name string) bool {
	return c.f.creds.find(name) != nil || c.f.config.find(configSection(name)) != nil
}

// applySave makes the draft's edits in memory.
func (c *change) applySave(d Draft) error {
	d.Name = strings.TrimSpace(d.Name)
	if err := validName(d.Name); err != nil {
		return err
	}
	orig := d.Original
	if orig == "" {
		if c.exists(d.Name) {
			return fmt.Errorf("a profile named %s already exists", d.Name)
		}
		orig = d.Name
	} else {
		if !c.exists(orig) {
			return errChanged
		}
		if d.Name != orig && c.exists(d.Name) {
			return fmt.Errorf("a profile named %s already exists", d.Name)
		}
	}
	creds, config := c.f.creds, c.f.config
	cf, kf := c.s.credentials, c.s.config

	// ── credentials ──
	sec := creds.find(orig)
	if d.UseKeys {
		id := strings.TrimSpace(d.AccessKeyID)
		secret := strings.TrimSpace(d.SecretAccessKey)
		token := strings.TrimSpace(d.SessionToken)
		if !accessKeyRe.MatchString(id) {
			return fmt.Errorf("%q doesn't look like an access key ID (AKIA… or ASIA…)", id)
		}
		if secret != "" && (len(secret) < 16 || strings.ContainsAny(secret, " \t\r\n")) {
			return errors.New("the secret access key looks incomplete")
		}
		if strings.ContainsAny(token, " \t\r\n") {
			return errors.New("the session token can't contain spaces")
		}
		oldID := ""
		if sec != nil {
			if k := sec.get(keyID); k != nil {
				oldID = k.value
			}
		}
		hasSecret := sec != nil && sec.get(keySecret) != nil && sec.get(keySecret).value != ""
		if secret == "" && (id != oldID || !hasSecret) {
			return errors.New("enter the secret access key that goes with this key ID")
		}
		if sec == nil {
			creds.add(d.Name)
			c.note(cf, "add [%s]", d.Name)
		} else if d.Name != orig {
			creds.rename(orig, d.Name)
			c.note(cf, "rename [%s] to [%s]", orig, d.Name)
		}
		if id != oldID {
			if oldID == "" {
				c.note(cf, "[%s] access key ID %s", d.Name, id)
			} else {
				c.note(cf, "[%s] access key ID %s → %s", d.Name, oldID, id)
			}
			creds.set(d.Name, keyID, id)
		}
		if secret != "" {
			if s := creds.find(d.Name).get(keySecret); s == nil || s.value != secret {
				c.note(cf, "[%s] new secret access key (not shown)", d.Name)
				creds.set(d.Name, keySecret, secret)
			}
		}
		if d.RemoveSessionToken {
			for _, k := range []string{keyToken, keyTokenOld} {
				if creds.find(d.Name).get(k) != nil {
					creds.unset(d.Name, k)
					c.note(cf, "[%s] remove the session token", d.Name)
				}
			}
		} else if token != "" {
			if t := creds.find(d.Name).get(keyToken); t == nil || t.value != token {
				c.note(cf, "[%s] new session token (not shown)", d.Name)
				creds.set(d.Name, keyToken, token)
			}
		}
	} else if sec != nil {
		id := ""
		if k := sec.get(keyID); k != nil {
			id = " (access key " + k.value + ")"
		}
		creds.remove(orig)
		c.note(cf, "remove [%s]%s", orig, id)
	}

	// ── config ──
	want, err := cleanSettings(d.Settings)
	if err != nil {
		return err
	}
	origSec, newSec := configSection(orig), configSection(d.Name)
	csec := config.find(origSec)
	locked := 0
	if csec != nil {
		for _, k := range csec.keys {
			if k.nested {
				locked++
			}
		}
	}
	switch {
	case csec == nil && len(want) == 0:
	case csec != nil && len(want) == 0 && locked == 0:
		config.remove(origSec)
		c.note(kf, "remove [%s]", origSec)
	default:
		if csec == nil {
			config.add(newSec)
			c.note(kf, "add [%s]", newSec)
		} else if newSec != origSec {
			config.rename(origSec, newSec)
			c.note(kf, "rename [%s] to [%s]", origSec, newSec)
		}
		wanted := map[string]bool{}
		for _, w := range want {
			wanted[strings.ToLower(w.Key)] = true
		}
		for _, k := range config.find(newSec).keys {
			if !k.nested && !wanted[k.key] {
				config.unset(newSec, k.key)
				c.note(kf, "[%s] remove %s", newSec, keyName(k))
			}
		}
		for _, w := range want {
			k := config.find(newSec).get(strings.ToLower(w.Key))
			switch {
			case k != nil && k.nested:
				return fmt.Errorf("%s has nested settings; edit it in the file", w.Key)
			case k == nil:
				config.set(newSec, strings.ToLower(w.Key), w.Value)
				c.note(kf, "[%s] %s = %s", newSec, w.Key, w.Value)
			case k.value != w.Value:
				config.set(newSec, k.key, w.Value)
				c.note(kf, "[%s] %s: %s → %s", newSec, keyName(k), k.value, w.Value)
			}
		}
	}

	// Profiles that take their credentials from this one follow the rename.
	if d.Name != orig {
		for _, s := range config.sections() {
			if k := s.get("source_profile"); k != nil && k.value == orig {
				config.set(s.name, "source_profile", d.Name)
				c.note(kf, "[%s] source_profile: %s → %s", s.name, orig, d.Name)
			}
		}
	}
	return nil
}

func cleanSettings(in []Setting) ([]Setting, error) {
	var out []Setting
	seen := map[string]bool{}
	for _, s := range in {
		key, value := strings.TrimSpace(s.Key), strings.TrimSpace(s.Value)
		if value == "" {
			continue // an emptied field removes the setting
		}
		lower := strings.ToLower(key)
		switch {
		case !settingRe.MatchString(key):
			return nil, fmt.Errorf("%q isn't a setting name", key)
		case seen[lower]:
			return nil, fmt.Errorf("%s is given twice", key)
		case strings.ContainsAny(value, "\r\n"):
			return nil, fmt.Errorf("%s: a value can't span lines", key)
		case lower == keyID || lower == keySecret || lower == keyToken:
			return nil, fmt.Errorf("%s belongs in the access key fields", key)
		case lower == "region" && !regionRe.MatchString(value):
			return nil, fmt.Errorf("%q doesn't look like a region, like us-east-1", value)
		case lower == "output" && value != "json" && value != "yaml" && value != "yaml-stream" && value != "text" && value != "table":
			return nil, fmt.Errorf("output must be json, yaml, yaml-stream, text or table")
		}
		seen[lower] = true
		out = append(out, Setting{Key: key, Value: value})
	}
	return out, nil
}

// PlanSave lists what saving a draft would change, without changing anything.
func (s *Service) PlanSave(d Draft) ([]string, error) {
	c, err := s.begin(d.Versions)
	if err != nil {
		return nil, err
	}
	if err := c.applySave(d); err != nil {
		return nil, err
	}
	return c.plan(), nil
}

// Save makes the changes PlanSave listed, and returns the new list.
func (s *Service) Save(d Draft) (List, error) {
	c, err := s.begin(d.Versions)
	if err != nil {
		return List{}, err
	}
	if err := c.applySave(d); err != nil {
		return List{}, err
	}
	return s.commit(c)
}

func (c *change) applyDelete(name string) error {
	if !c.exists(name) {
		return errChanged
	}
	if sec := c.f.creds.find(name); sec != nil {
		id := ""
		if k := sec.get(keyID); k != nil {
			id = " (access key " + k.value + ")"
		}
		c.f.creds.remove(name)
		c.note(c.s.credentials, "remove [%s]%s", name, id)
	}
	if c.f.config.find(configSection(name)) != nil {
		c.f.config.remove(configSection(name))
		c.note(c.s.config, "remove [%s]", configSection(name))
	}
	for _, sec := range c.f.config.sections() {
		if k := sec.get("source_profile"); k != nil && k.value == name {
			c.lines = append(c.lines, fmt.Sprintf("[%s] uses it as source_profile and will stop working", sec.name))
		}
	}
	return nil
}

// PlanDelete lists what deleting a profile would change. The keys stay
// valid in AWS: this only removes them from this computer.
func (s *Service) PlanDelete(name string, versions map[string]string) ([]string, error) {
	c, err := s.begin(versions)
	if err != nil {
		return nil, err
	}
	if err := c.applyDelete(name); err != nil {
		return nil, err
	}
	return c.plan(), nil
}

func (s *Service) Delete(name string, versions map[string]string) (List, error) {
	c, err := s.begin(versions)
	if err != nil {
		return List{}, err
	}
	if err := c.applyDelete(name); err != nil {
		return List{}, err
	}
	return s.commit(c)
}

// applyDefault copies a profile's keys and settings into [default], which
// the AWS CLI uses when no profile is chosen.
func (c *change) applyDefault(name string) error {
	if name == "default" {
		return errors.New("that's already the default profile")
	}
	src := c.f.creds.find(name)
	csrc := c.f.config.find(configSection(name))
	if src == nil && csrc == nil {
		return errChanged
	}
	d := Draft{Name: "default"}
	if c.exists("default") {
		d.Original = "default"
	}
	if src != nil {
		if k := src.get(keyID); k != nil {
			d.UseKeys, d.AccessKeyID = true, k.value
		}
		if k := src.get(keySecret); k != nil {
			d.SecretAccessKey = k.value
		}
		if k := src.get(keyToken); k != nil {
			d.SessionToken = k.value
		} else if k := src.get(keyTokenOld); k != nil {
			d.SessionToken = k.value
		} else {
			d.RemoveSessionToken = true
		}
	}
	if csrc != nil {
		for _, k := range csrc.keys {
			if !k.nested {
				d.Settings = append(d.Settings, Setting{Key: keyName(k), Value: k.value})
			}
		}
	}
	if d.UseKeys && d.SecretAccessKey == "" {
		return fmt.Errorf("%s has no secret access key to copy", name)
	}
	return c.applySave(d)
}

// PlanMakeDefault lists what copying a profile to [default] would change.
func (s *Service) PlanMakeDefault(name string, versions map[string]string) ([]string, error) {
	c, err := s.begin(versions)
	if err != nil {
		return nil, err
	}
	if err := c.applyDefault(name); err != nil {
		return nil, err
	}
	return c.plan(), nil
}

func (s *Service) MakeDefault(name string, versions map[string]string) (List, error) {
	c, err := s.begin(versions)
	if err != nil {
		return List{}, err
	}
	if err := c.applyDefault(name); err != nil {
		return List{}, err
	}
	return s.commit(c)
}

func (c *change) plan() []string {
	if c.lines == nil {
		return []string{}
	}
	return c.lines
}

// commit writes the files the change touched.
func (s *Service) commit(c *change) (List, error) {
	if len(c.lines) == 0 {
		return s.List(), nil
	}
	write := func(path string, d *doc, old []byte, existed bool) error {
		data := d.bytes()
		if string(data) == string(old) {
			return nil
		}
		return s.write(path, data, old, existed)
	}
	if c.touched[s.credentials] {
		if err := write(s.credentials, c.f.creds, c.f.credsData, c.f.credsOK); err != nil {
			return List{}, err
		}
	}
	if c.touched[s.config] {
		if err := write(s.config, c.f.config, c.f.configData, c.f.configOK); err != nil {
			return List{}, err
		}
	}
	return s.List(), nil
}

// write replaces a file atomically, keeping its permissions (0600 for a
// new one: it may hold secrets), after keeping a copy of the old one.
func (s *Service) write(path string, data, old []byte, existed bool) error {
	target := path
	if t, err := filepath.EvalSymlinks(path); err == nil {
		target = t
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(target); err == nil {
		mode = fi.Mode().Perm()
	} else if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	if existed {
		s.backup(path, old)
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".wtf-aws-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}

// backup keeps the last few versions in wtf's cache (owner-only: they hold
// keys).
func (s *Service) backup(path string, data []byte) {
	if s.backupDir == "" || os.MkdirAll(s.backupDir, 0o700) != nil {
		return
	}
	stamp := time.Now().Format("20060102-150405.000")
	_ = os.WriteFile(filepath.Join(s.backupDir, stamp+"-"+filepath.Base(path)), data, 0o600)
	des, err := os.ReadDir(s.backupDir)
	if err != nil || len(des) <= maxBackups {
		return
	}
	for _, de := range des[:len(des)-maxBackups] {
		_ = os.Remove(filepath.Join(s.backupDir, de.Name()))
	}
}

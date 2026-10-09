// Package infra works out which infrastructure a shell is pointed at — its
// Kubernetes context and cluster, AWS profile and account, gcloud project
// and Terraform workspace — so the address bar can show it.
//
// The shell reports the environment variables that matter at every prompt
// (see internal/terminal/shellintegration); everything else is read from
// the tools' own config files, the way the tools themselves would. Nothing
// here polls: one call costs a few stat(2)s, and parsed files are cached
// until they change. The one exception is an AWS account that only AWS
// can tell, which is looked up once in the background (see awsaccount.go).
package infra

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"gopkg.in/yaml.v3"
)

// EventChanged tells the frontend to resolve its shells' contexts again: an
// AWS account lookup finished.
const EventChanged = "infra:changed"

func init() {
	application.RegisterEvent[string](EventChanged)
}

// Context is what a shell is pointed at. Empty fields mean "nothing".
type Context struct {
	KubeContext   string `json:"kubeContext"`
	KubeCluster   string `json:"kubeCluster"`
	KubeNamespace string `json:"kubeNamespace"`
	// "env credentials" when access keys in the environment override any
	// profile; the account is then unknown.
	AWSProfile string `json:"awsProfile"`
	// From the profile's config, else asked of AWS (see awsaccount.go).
	AWSAccount string `json:"awsAccount"`
	// Why there is no account: AccountPending, or what went wrong.
	AWSAccountNote string `json:"awsAccountNote"`
	AWSRegion      string `json:"awsRegion"`
	GCPProject     string `json:"gcpProject"`
	// Terraform workspace of the current folder; "default" is left out.
	TFWorkspace string `json:"tfWorkspace"`
}

// Bigger files are skipped; no real kubeconfig or ini file comes close.
const maxFileSize = 8 << 20

type Service struct {
	home string

	// Guards files and accounts.
	mu       sync.Mutex
	files    map[string]cached
	accounts map[string]*accountEntry

	lookup  accountLookup
	changed func()
}

type cached struct {
	mod  time.Time
	size int64
	kube kubeconfig
	ini  iniFile
}

func NewService() *Service {
	home, _ := os.UserHomeDir()
	return newService(home, stsLookup, func() {
		application.Get().Event.Emit(EventChanged, "")
	})
}

func newService(home string, lookup accountLookup, changed func()) *Service {
	return &Service{
		home:     home,
		files:    map[string]cached{},
		accounts: map[string]*accountEntry{},
		lookup:   lookup,
		changed:  changed,
	}
}

// Resolve works out a shell's context from the variables it reported.
// PWD is the shell's working directory.
func (s *Service) Resolve(env map[string]string) Context {
	var c Context
	c.KubeContext, c.KubeCluster, c.KubeNamespace = s.kube(env)
	s.aws(env, &c)
	c.GCPProject = s.gcp(env)
	c.TFWorkspace = s.terraform(env)
	return c
}

// abs resolves a path the shell gave relative to its working directory.
func abs(p string, env map[string]string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	if cwd := env["PWD"]; filepath.IsAbs(cwd) {
		return filepath.Join(cwd, p)
	}
	return ""
}

// ── Kubernetes ───────────────────────────────────────────────────────────

type kubeconfig struct {
	CurrentContext string `yaml:"current-context"`
	Contexts       []struct {
		Name    string `yaml:"name"`
		Context struct {
			Cluster   string `yaml:"cluster"`
			Namespace string `yaml:"namespace"`
		} `yaml:"context"`
	} `yaml:"contexts"`
}

// kube follows kubectl's merge rules for $KUBECONFIG: the first file that
// sets current-context wins, and so does the first definition of a context.
func (s *Service) kube(env map[string]string) (ctx, cluster, ns string) {
	var paths []string
	if list, ok := env["KUBECONFIG"]; ok && list != "" {
		for _, p := range filepath.SplitList(list) {
			if p = abs(p, env); p != "" {
				paths = append(paths, p)
			}
		}
	} else if s.home != "" {
		paths = []string{filepath.Join(s.home, ".kube", "config")}
	}

	var configs []kubeconfig
	for _, p := range paths {
		if c, ok := s.load(p, kubeFormat); ok {
			configs = append(configs, c.kube)
			if ctx == "" {
				ctx = c.kube.CurrentContext
			}
		}
	}
	if ctx == "" {
		return "", "", ""
	}
	for _, c := range configs {
		for _, def := range c.Contexts {
			if def.Name == ctx {
				return ctx, def.Context.Cluster, def.Context.Namespace
			}
		}
	}
	return ctx, "", ""
}

// ── AWS ──────────────────────────────────────────────────────────────────

// EnvCredentials is the profile shown when access keys in the environment
// take precedence over any profile.
const EnvCredentials = "env credentials"

var roleARN = regexp.MustCompile(`^arn:aws[\w-]*:iam::(\d{12}):`)

// aws reports the profile in effect: the one the shell chose, else
// "default" when that profile exists.
//
// The account comes from the profile's own config when it names one: the
// role it assumes (role_arn), else its SSO account, else aws_account_id.
// Otherwise (static keys, credential_process) AWS is asked.
func (s *Service) aws(env map[string]string, c *Context) {
	region := firstOf(env["AWS_REGION"], env["AWS_DEFAULT_REGION"])
	profile := firstOf(env["AWS_PROFILE"], env["AWS_DEFAULT_PROFILE"], env["AWS_VAULT"])
	// The shell reports whether access keys are set, never their values.
	// They win over AWS_PROFILE; aws-vault sets them for its own profile.
	if env["WTF_AWS_ENV_KEYS"] != "" && (env["AWS_VAULT"] == "" || profile != env["AWS_VAULT"]) {
		c.AWSProfile, c.AWSRegion = EnvCredentials, region
		return
	}

	configFile := s.awsFile(env, "AWS_CONFIG_FILE", "config")
	credsFile := s.awsFile(env, "AWS_SHARED_CREDENTIALS_FILE", "credentials")
	config, _ := s.load(configFile, iniFormat)
	// Only the section names: the keys never need to be in memory.
	creds, _ := s.load(credsFile, sectionsFormat)

	section := "profile " + profile
	if profile == "" || profile == "default" {
		section = "default"
	}
	known := config.ini.has(section) || creds.ini.has(firstOf(profile, "default"))
	if profile == "" {
		if !known {
			return // no AWS setup to speak of
		}
		profile = "default"
	}
	c.AWSProfile = profile
	c.AWSRegion = firstOf(region, config.ini.get(section, "region"))

	if m := roleARN.FindStringSubmatch(config.ini.get(section, "role_arn")); m != nil {
		c.AWSAccount = m[1]
	} else {
		c.AWSAccount = firstOf(config.ini.get(section, "sso_account_id"), config.ini.get(section, "aws_account_id"))
	}
	if c.AWSAccount == "" && known {
		c.AWSAccount, c.AWSAccountNote = s.account(profile, c.AWSRegion, configFile, credsFile)
	}
}

// awsFile is where an AWS file lives: the variable's path, or ~/.aws/<name>.
func (s *Service) awsFile(env map[string]string, variable, name string) string {
	if p := abs(env[variable], env); p != "" {
		return p
	}
	if s.home == "" {
		return ""
	}
	return filepath.Join(s.home, ".aws", name)
}

// ── Google Cloud ─────────────────────────────────────────────────────────

func (s *Service) gcp(env map[string]string) string {
	if p := env["CLOUDSDK_CORE_PROJECT"]; p != "" {
		return p
	}
	dir := abs(env["CLOUDSDK_CONFIG"], env)
	if dir == "" && s.home != "" {
		dir = filepath.Join(s.home, ".config", "gcloud")
	}
	if dir == "" {
		return ""
	}
	name := env["CLOUDSDK_ACTIVE_CONFIG_NAME"]
	if name == "" {
		name = readSmall(filepath.Join(dir, "active_config"))
	}
	if name == "" {
		name = "default"
	}
	if c, ok := s.load(filepath.Join(dir, "configurations", "config_"+name), iniFormat); ok {
		return c.ini.get("core", "project")
	}
	return ""
}

// ── Terraform ────────────────────────────────────────────────────────────

// terraform reports the workspace only inside an initialized Terraform
// folder, where it decides what `terraform apply` touches.
func (s *Service) terraform(env map[string]string) string {
	cwd := env["PWD"]
	if !filepath.IsAbs(cwd) {
		return ""
	}
	data := env["TF_DATA_DIR"]
	if data == "" {
		data = ".terraform"
	}
	if !filepath.IsAbs(data) {
		data = filepath.Join(cwd, data)
	}
	if fi, err := os.Stat(data); err != nil || !fi.IsDir() {
		return ""
	}
	ws := firstOf(env["TF_WORKSPACE"], readSmall(filepath.Join(data, "environment")))
	if ws == "default" {
		return ""
	}
	return ws
}

// ── Files ────────────────────────────────────────────────────────────────

type format int

const (
	kubeFormat format = iota
	iniFormat
	// An ini file of which only the section names are kept.
	sectionsFormat
)

// load parses a kubeconfig or ini file, reusing the last parse while the
// file is unchanged.
func (s *Service) load(path string, f format) (cached, bool) {
	if path == "" {
		return cached{}, false
	}
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxFileSize {
		return cached{}, false
	}
	s.mu.Lock()
	c, hit := s.files[path]
	s.mu.Unlock()
	if hit && c.mod.Equal(fi.ModTime()) && c.size == fi.Size() {
		return c, true
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return cached{}, false
	}
	c = cached{mod: fi.ModTime(), size: fi.Size()}
	switch f {
	case iniFormat:
		c.ini = parseIni(data, true)
	case sectionsFormat:
		c.ini = parseIni(data, false)
	default:
		// A broken kubeconfig counts as an empty one, like a missing file.
		_ = yaml.Unmarshal(data, &c.kube)
	}
	s.mu.Lock()
	s.files[path] = c
	s.mu.Unlock()
	return c, true
}

// iniFile maps "section" → key → value. Good enough for ~/.aws/config and
// gcloud's configurations, which are plain key = value sections.
type iniFile map[string]map[string]string

// parseIni reads an ini file; without values, only the section names.
func parseIni(data []byte, values bool) iniFile {
	out := iniFile{}
	section := ""
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "" || line[0] == '#' || line[0] == ';':
		case line[0] == '[' && strings.HasSuffix(line, "]"):
			section = strings.Join(strings.Fields(line[1:len(line)-1]), " ")
			if out[section] == nil {
				out[section] = map[string]string{}
			}
		case !values:
		default:
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			if out[section] == nil {
				out[section] = map[string]string{}
			}
			out[section][strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

func (f iniFile) get(section, key string) string { return f[section][key] }

func (f iniFile) has(section string) bool { _, ok := f[section]; return ok }

// readSmall returns a short file's trimmed contents, or "".
func readSmall(path string) string {
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > 4096 {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func firstOf(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

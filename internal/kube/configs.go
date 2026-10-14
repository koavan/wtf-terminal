package kube

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/dayanraj/wtf/internal/infra"
	"github.com/dayanraj/wtf/internal/paths"
)

// Managing kubeconfigs: the contexts in the shell's KUBECONFIG files,
// switching one shell to a context, and editing, merging and repairing
// the files.
//
// Switching is per shell, kubie-style, and copies no credentials: wtf
// writes a small file holding only `current-context` and that context's
// entry (cluster, user and namespace names), and the shell puts it first
// in its KUBECONFIG. kubectl takes the first current-context and the first
// definition of a context it finds, so that shell — and only that shell —
// uses the context; `kubectl config` commands run there change the small
// file, not the shared one. The files live in wtf's state folder and go
// when wtf starts again (their shells are gone by then).
//
// Credentials are never returned: a context says how it authenticates
// (exec plugin, token, client certificate…), never with what.

// KubeContext is one context, as kubectl would merge it.
type KubeContext struct {
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	User      string `json:"user"`
	Namespace string `json:"namespace"`
	// The file that defines it.
	File   string `json:"file"`
	Server string `json:"server"`
	// exec, token, client-cert, auth-provider, basic or none.
	Auth        string   `json:"auth"`
	ExecCommand string   `json:"execCommand"`
	ExecArgs    []string `json:"execArgs"`
	// Names of the variables the exec plugin gets; values only for
	// AWS_PROFILE and region variables, which aren't secret.
	ExecEnv []string `json:"execEnv"`
	// Whether the exec plugin is on the shell's PATH (or at its path).
	PluginFound bool `json:"pluginFound"`
	// For EKS clusters: the cluster's name, region and AWS profile.
	EKSCluster string `json:"eksCluster"`
	EKSRegion  string `json:"eksRegion"`
	AWSProfile string `json:"awsProfile"`
	// The merged kubeconfig's current context.
	Current bool `json:"current"`
	// In a file the shell reads (its KUBECONFIG, or ~/.kube/config). Others
	// are kubeconfigs found in ~/.kube that kubectl there doesn't see.
	Active bool `json:"active"`
	// Facts that keep it from working: a missing cluster or user entry.
	Problems []string `json:"problems"`
}

type ConfigList struct {
	Contexts []KubeContext `json:"contexts"`
	// The kubeconfig files, in KUBECONFIG order (wtf's own files left out).
	Files []string `json:"files"`
	// Other kubeconfig files found in ~/.kube, each read on its own.
	OtherFiles []string          `json:"otherFiles"`
	Problems   []string          `json:"problems"`
	Versions   map[string]string `json:"versions"`
}

type kcExec struct {
	Command    string   `yaml:"command"`
	Args       []string `yaml:"args"`
	APIVersion string   `yaml:"apiVersion"`
	Env        []struct {
		Name  string `yaml:"name"`
		Value string `yaml:"value"`
	} `yaml:"env"`
}

type kcFile struct {
	CurrentContext string `yaml:"current-context"`
	Clusters       []struct {
		Name    string `yaml:"name"`
		Cluster struct {
			Server string `yaml:"server"`
		} `yaml:"cluster"`
	} `yaml:"clusters"`
	Contexts []struct {
		Name    string `yaml:"name"`
		Context struct {
			Cluster   string `yaml:"cluster"`
			User      string `yaml:"user"`
			Namespace string `yaml:"namespace"`
		} `yaml:"context"`
	} `yaml:"contexts"`
	Users []struct {
		Name string `yaml:"name"`
		User struct {
			Exec                  *kcExec `yaml:"exec"`
			Token                 string  `yaml:"token"`
			TokenFile             string  `yaml:"tokenFile"`
			ClientCertificate     string  `yaml:"client-certificate"`
			ClientCertificateData string  `yaml:"client-certificate-data"`
			AuthProvider          *struct {
				Name string `yaml:"name"`
			} `yaml:"auth-provider"`
			Username string `yaml:"username"`
		} `yaml:"user"`
	} `yaml:"users"`
}

const (
	maxKubeconfig  = 4 << 20
	maxKubeBackups = 20
)

var (
	eksHost     = regexp.MustCompile(`\.([a-z]{2}(?:-[a-z]+)+-\d+)\.eks\.amazonaws\.com`)
	contextName = regexp.MustCompile(`^[^\s/\\]+$`)
)

// overlayDir is where the per-shell files go.
func (s *Service) overlayDir() string {
	if s.stateDir == "" {
		return ""
	}
	return filepath.Join(s.stateDir, "kube-shells")
}

// backupDir keeps copies of kubeconfigs before wtf changes them.
func (s *Service) backupDir() string {
	if s.cacheDir != "" {
		return filepath.Join(s.cacheDir, "kube-backups")
	}
	if cache, err := paths.UserCacheDir(); err == nil {
		return filepath.Join(cache, "wtf", "kube-backups")
	}
	return ""
}

// ClearShellConfigs removes the per-shell files of an earlier run: their
// shells ended with it.
func (s *Service) ClearShellConfigs() {
	if dir := s.overlayDir(); dir != "" {
		_ = os.RemoveAll(dir)
	}
}

// files are the kubeconfig files a shell uses: its KUBECONFIG (wtf's own
// per-shell files left out), else ~/.kube/config.
func (s *Service) files(env map[string]string) []string {
	var out []string
	if list := env["KUBECONFIG"]; list != "" {
		for _, p := range filepath.SplitList(s.absList(list, env["PWD"])) {
			if p == "" || s.isOverlay(p) {
				continue
			}
			out = append(out, p)
		}
	}
	if len(out) == 0 && s.home != "" {
		out = []string{filepath.Join(s.home, ".kube", "config")}
	}
	return out
}

func (s *Service) isOverlay(p string) bool {
	dir := s.overlayDir()
	return dir != "" && strings.HasPrefix(filepath.Clean(p), dir+string(filepath.Separator))
}

func fileVersion(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:12])
}

// Contexts lists the contexts of the shell's kubeconfig files, merged the
// way kubectl merges them, then those of the other kubeconfigs in ~/.kube,
// each file on its own. It reads files only.
func (s *Service) Contexts(env map[string]string) ConfigList {
	shell := s.files(env)
	out := s.merged(shell, env)
	for i := range out.Contexts {
		out.Contexts[i].Active = true
	}
	out.OtherFiles = []string{}
	for _, f := range s.discover() {
		if slices.Contains(shell, f) {
			continue
		}
		l := s.merged([]string{f}, env)
		if len(l.Contexts) == 0 {
			continue
		}
		for _, c := range l.Contexts {
			c.Current = false // a file kubectl doesn't read has no say
			out.Contexts = append(out.Contexts, c)
		}
		out.OtherFiles = append(out.OtherFiles, f)
		out.Versions[f] = l.Versions[f]
	}
	return out
}

// discover finds the kubeconfig files in ~/.kube: files that parse as a
// kubeconfig with at least one context. Folders (kubectl's cache) are skipped.
func (s *Service) discover() []string {
	dir := filepath.Join(s.home, ".kube")
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, de := range des {
		if strings.HasPrefix(de.Name(), ".") {
			continue
		}
		p := filepath.Join(dir, de.Name())
		fi, err := os.Stat(p) // follows symlinks
		if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxKubeconfig || s.isOverlay(p) {
			continue
		}
		if f, err := readKubeconfig(p); err == nil && len(f.Contexts) > 0 {
			out = append(out, p)
		}
	}
	return out
}

// scope is the set of files a context is read from: the shell's files, or
// one other kubeconfig from ~/.kube (only those, nothing else on disk).
func (s *Service) scope(env map[string]string, file string) ([]string, error) {
	shell := s.files(env)
	if file == "" || slices.Contains(shell, file) {
		return shell, nil
	}
	if slices.Contains(s.discover(), filepath.Clean(file)) {
		return []string{filepath.Clean(file)}, nil
	}
	return nil, fmt.Errorf("%s isn't a kubeconfig wtf lists", file)
}

// merged reads a set of files the way kubectl reads KUBECONFIG.
func (s *Service) merged(files []string, env map[string]string) ConfigList {
	out := ConfigList{Contexts: []KubeContext{}, Files: files, Problems: []string{}, Versions: map[string]string{}}
	type parsed struct {
		path string
		f    kcFile
	}
	var all []parsed
	current := ""
	for _, p := range out.Files {
		out.Versions[p] = fileVersion(p)
		f, err := readKubeconfig(p)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				out.Problems = append(out.Problems, err.Error())
			}
			continue
		}
		all = append(all, parsed{p, f})
		if current == "" {
			current = f.CurrentContext
		}
	}

	clusters := map[string]string{} // name → server, first wins
	users := map[string]int{}       // name → index into userDefs
	type userDef struct {
		exec *kcExec
		auth string
	}
	var userDefs []userDef
	for _, pf := range all {
		for _, c := range pf.f.Clusters {
			if _, ok := clusters[c.Name]; !ok {
				clusters[c.Name] = c.Cluster.Server
			}
		}
		for _, u := range pf.f.Users {
			if _, ok := users[u.Name]; ok {
				continue
			}
			d := userDef{exec: u.User.Exec, auth: "none"}
			switch {
			case u.User.Exec != nil:
				d.auth = "exec"
			case u.User.Token != "" || u.User.TokenFile != "":
				d.auth = "token"
			case u.User.ClientCertificate != "" || u.User.ClientCertificateData != "":
				d.auth = "client-cert"
			case u.User.AuthProvider != nil:
				d.auth = "auth-provider"
			case u.User.Username != "":
				d.auth = "basic"
			}
			users[u.Name] = len(userDefs)
			userDefs = append(userDefs, d)
		}
	}

	seen := map[string]bool{}
	for _, pf := range all {
		for _, c := range pf.f.Contexts {
			if seen[c.Name] {
				continue
			}
			seen[c.Name] = true
			kc := KubeContext{
				Name: c.Name, Cluster: c.Context.Cluster, User: c.Context.User, Namespace: c.Context.Namespace,
				File: pf.path, Current: c.Name == current, ExecArgs: []string{}, ExecEnv: []string{}, Problems: []string{},
			}
			if server, ok := clusters[c.Context.Cluster]; ok {
				kc.Server = server
			} else {
				kc.Problems = append(kc.Problems, fmt.Sprintf("cluster %q isn't defined in any file", c.Context.Cluster))
			}
			if i, ok := users[c.Context.User]; ok {
				d := userDefs[i]
				kc.Auth = d.auth
				if d.exec != nil {
					s.describeExec(&kc, d.exec, env)
				}
			} else if c.Context.User != "" {
				kc.Problems = append(kc.Problems, fmt.Sprintf("user %q isn't defined in any file", c.Context.User))
			}
			if m := eksHost.FindStringSubmatch(kc.Server); m != nil && kc.EKSRegion == "" {
				kc.EKSRegion = m[1]
			}
			out.Contexts = append(out.Contexts, kc)
		}
	}
	sort.SliceStable(out.Contexts, func(i, j int) bool { return out.Contexts[i].Name < out.Contexts[j].Name })
	return out
}

func readKubeconfig(path string) (kcFile, error) {
	var f kcFile
	fi, err := os.Stat(path)
	if err != nil {
		return f, err
	}
	if fi.Size() > maxKubeconfig {
		return f, fmt.Errorf("%s is too big to be a kubeconfig", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return f, err
	}
	if err := yaml.Unmarshal(data, &f); err != nil {
		return f, fmt.Errorf("%s isn't a valid kubeconfig: %v", path, err)
	}
	return f, nil
}

// describeExec fills in what an exec plugin is, and whether the shell can
// run it — without the values of its environment, apart from the AWS
// profile and region.
func (s *Service) describeExec(kc *KubeContext, e *kcExec, env map[string]string) {
	kc.ExecCommand = e.Command
	kc.ExecArgs = append([]string{}, e.Args...)
	for _, v := range e.Env {
		switch v.Name {
		case "AWS_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION", "AWS_STS_REGIONAL_ENDPOINTS":
			kc.ExecEnv = append(kc.ExecEnv, v.Name+"="+v.Value)
			if v.Name == "AWS_PROFILE" {
				kc.AWSProfile = v.Value
			} else if v.Name != "AWS_STS_REGIONAL_ENDPOINTS" {
				kc.EKSRegion = v.Value
			}
		default:
			kc.ExecEnv = append(kc.ExecEnv, v.Name)
		}
	}
	kc.PluginFound = s.findPlugin(e.Command, env) != ""

	base := filepath.Base(e.Command)
	arg := func(names ...string) string {
		for i, a := range e.Args {
			for _, n := range names {
				if a == n && i+1 < len(e.Args) {
					return e.Args[i+1]
				}
				if v, ok := strings.CutPrefix(a, n+"="); ok {
					return v
				}
			}
		}
		return ""
	}
	switch base {
	case "aws-iam-authenticator":
		kc.EKSCluster = arg("-i", "--cluster-id")
		if r := arg("--region"); r != "" {
			kc.EKSRegion = r
		}
		if p := arg("--profile"); p != "" {
			kc.AWSProfile = p
		}
	case "aws":
		kc.EKSCluster = arg("--cluster-name")
		if r := arg("--region"); r != "" {
			kc.EKSRegion = r
		}
		if p := arg("--profile"); p != "" {
			kc.AWSProfile = p
		}
	}
}

// findPlugin looks for an exec plugin the way kubectl does: a path as
// given, else on the shell's PATH.
func (s *Service) findPlugin(command string, env map[string]string) string {
	if command == "" {
		return ""
	}
	if strings.Contains(command, "/") {
		p := command
		if strings.HasPrefix(p, "~/") {
			p = filepath.Join(s.home, p[2:])
		}
		if isExec(p) {
			return p
		}
		return ""
	}
	path := env["PATH"]
	if path == "" {
		path = os.Getenv("PATH")
	}
	for _, dir := range filepath.SplitList(path) {
		if dir != "" && isExec(filepath.Join(dir, command)) {
			return filepath.Join(dir, command)
		}
	}
	return ""
}

// ── Using a context in one shell ─────────────────────────────────────────

// ShellConfig writes a per-shell file for a context and returns the
// command that points a shell at it:
// export KUBECONFIG='<that file>:<the shell's files>' — or, for a context
// from another kubeconfig in ~/.kube, '<that file>:<its file>', so that
// shell sees only that file (names in different files can't mix).
func (s *Service) ShellConfig(env map[string]string, name, file, namespace string) (string, error) {
	files, err := s.scope(env, file)
	if err != nil {
		return "", err
	}
	list := s.merged(files, env)
	var kc *KubeContext
	for i := range list.Contexts {
		if list.Contexts[i].Name == name {
			kc = &list.Contexts[i]
		}
	}
	if kc == nil {
		return "", fmt.Errorf("no context named %s", name)
	}
	if err := validName("namespace", namespace); err != nil {
		return "", err
	}
	if namespace == "" {
		namespace = kc.Namespace
	}
	dir := s.overlayDir()
	if dir == "" {
		return "", errors.New("wtf has no state folder to keep the shell's context in")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	ctx := map[string]string{"cluster": kc.Cluster, "user": kc.User}
	if namespace != "" {
		ctx["namespace"] = namespace
	}
	doc := map[string]any{
		"apiVersion":      "v1",
		"kind":            "Config",
		"current-context": name,
		"contexts":        []any{map[string]any{"name": name, "context": ctx}},
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return "", err
	}
	id := make([]byte, 6)
	_, _ = rand.Read(id)
	path := filepath.Join(dir, hex.EncodeToString(id)+".yaml")
	header := "# wtf: this shell's Kubernetes context. The clusters and credentials\n# stay in the files after it in KUBECONFIG. Removed when wtf restarts.\n"
	if err := os.WriteFile(path, append([]byte(header), data...), 0o600); err != nil {
		return "", err
	}
	parts := append([]string{path}, list.Files...)
	return "export KUBECONFIG=" + shellQuote(strings.Join(parts, string(filepath.ListSeparator))), nil
}

func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./:@=,+") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ── Testing a connection ─────────────────────────────────────────────────

// Connection is what the cluster said when asked.
type Connection struct {
	// The server's Kubernetes version.
	Version string `json:"version"`
	// Who the cluster says you are, when it allows asking.
	Username string   `json:"username"`
	Groups   []string `json:"groups"`
}

// TestConnection asks a context's cluster who you are and its version.
// Only on request: it reaches the cluster.
func (s *Service) TestConnection(env map[string]string, name, file string) (Connection, error) {
	if err := validName("context", name); err != nil || name == "" {
		return Connection{}, fmt.Errorf("%q isn't a context name", name)
	}
	files, err := s.scope(env, file)
	if err != nil {
		return Connection{}, err
	}
	r := Request{Env: withKubeconfig(env, files)}
	bin, err := s.kubectl(r)
	if err != nil {
		return Connection{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := runKubectl(ctx, bin, s.env(r), "version", "--context", name, "--request-timeout", "10s", "-o", "json")
	if err != nil {
		return Connection{}, err
	}
	var v struct {
		ServerVersion struct{ GitVersion string } `json:"serverVersion"`
	}
	_ = json.Unmarshal(out, &v)
	c := Connection{Version: v.ServerVersion.GitVersion, Groups: []string{}}
	if out, err := runKubectl(ctx, bin, s.env(r), "auth", "whoami", "--context", name, "--request-timeout", "10s", "-o", "json"); err == nil {
		var w struct {
			Status struct {
				UserInfo struct {
					Username string
					Groups   []string
				} `json:"userInfo"`
			}
		}
		if json.Unmarshal(out, &w) == nil {
			c.Username = w.Status.UserInfo.Username
			if w.Status.UserInfo.Groups != nil {
				c.Groups = w.Status.UserInfo.Groups
			}
		}
	}
	return c, nil
}

// ── Editing (planned, confirmed, backed up) ──────────────────────────────

// edit is a change to the kubeconfig files: kubectl config commands, or
// new contents for a file.
type edit struct {
	steps    []string
	commands [][]string
	writes   map[string][]byte
}

// checkVersions refuses to change files that changed since they were listed.
func (s *Service) checkVersions(files []string, versions map[string]string) error {
	for _, f := range files {
		if versions[f] != fileVersion(f) {
			return errors.New("the kubeconfig changed since the list was loaded — reload and try again")
		}
	}
	return nil
}

// withKubeconfig is the shell's variables with KUBECONFIG set to files.
func withKubeconfig(env map[string]string, files []string) map[string]string {
	out := map[string]string{}
	for k, v := range env {
		out[k] = v
	}
	out["KUBECONFIG"] = strings.Join(files, string(filepath.ListSeparator))
	return out
}

func (s *Service) find(files []string, env map[string]string, name string) (KubeContext, ConfigList, error) {
	list := s.merged(files, env)
	for _, c := range list.Contexts {
		if c.Name == name {
			return c, list, nil
		}
	}
	return KubeContext{}, list, fmt.Errorf("no context named %s", name)
}

func (s *Service) planRename(files []string, env map[string]string, name, newName string) (edit, error) {
	_, list, err := s.find(files, env, name)
	if err != nil {
		return edit{}, err
	}
	if !contextName.MatchString(newName) {
		return edit{}, fmt.Errorf("%q can't be a context name", newName)
	}
	for _, c := range list.Contexts {
		if c.Name == newName {
			return edit{}, fmt.Errorf("a context named %s already exists", newName)
		}
	}
	return edit{commands: [][]string{{"config", "rename-context", name, newName}}}, nil
}

func (s *Service) planDelete(files []string, env map[string]string, name string) (edit, error) {
	kc, list, err := s.find(files, env, name)
	if err != nil {
		return edit{}, err
	}
	e := edit{commands: [][]string{{"config", "delete-context", name}}}
	clusterUsed, userUsed := false, false
	for _, c := range list.Contexts {
		if c.Name == name {
			continue
		}
		clusterUsed = clusterUsed || c.Cluster == kc.Cluster
		userUsed = userUsed || c.User == kc.User
	}
	if kc.Cluster != "" && !clusterUsed && kc.Server != "" {
		e.commands = append(e.commands, []string{"config", "delete-cluster", kc.Cluster})
	}
	if kc.User != "" && !userUsed && kc.Auth != "" {
		e.commands = append(e.commands, []string{"config", "delete-user", kc.User})
	}
	if kc.Current {
		e.steps = append(e.steps, "It's the current context: kubectl will have none until you pick one")
	}
	return e, nil
}

func (s *Service) planNamespace(files []string, env map[string]string, name, namespace string) (edit, error) {
	if _, _, err := s.find(files, env, name); err != nil {
		return edit{}, err
	}
	if err := validName("namespace", namespace); err != nil {
		return edit{}, err
	}
	return edit{commands: [][]string{{"config", "set-context", name, "--namespace=" + namespace}}}, nil
}

// planFixAuth rewrites an aws-iam-authenticator user to run
// `aws eks get-token`, which needs only the aws CLI. The profile and
// region it used are kept.
func (s *Service) planFixAuth(files []string, env map[string]string, name string) (edit, error) {
	kc, list, err := s.find(files, env, name)
	if err != nil {
		return edit{}, err
	}
	if filepath.Base(kc.ExecCommand) != "aws-iam-authenticator" {
		return edit{}, fmt.Errorf("%s doesn't use aws-iam-authenticator", name)
	}
	if kc.EKSCluster == "" || kc.EKSRegion == "" {
		return edit{}, errors.New("can't tell this EKS cluster's name and region from its kubeconfig")
	}
	// The user entry lives in the first file that defines it.
	for _, f := range list.Files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var root yaml.Node
		if yaml.Unmarshal(data, &root) != nil || len(root.Content) == 0 {
			continue
		}
		user := findNamed(root.Content[0], "users", kc.User)
		if user == nil {
			continue
		}
		exec := mapGet(mapGet(user, "user"), "exec")
		if exec == nil {
			return edit{}, errors.New("its user has no exec section")
		}
		args := []string{"--region", kc.EKSRegion, "eks", "get-token", "--cluster-name", kc.EKSCluster, "--output", "json"}
		mapSet(exec, "command", &yaml.Node{Kind: yaml.ScalarNode, Value: "aws"})
		seq := &yaml.Node{Kind: yaml.SequenceNode}
		for _, a := range args {
			seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: a})
		}
		mapSet(exec, "args", seq)
		out, err := yaml.Marshal(&root)
		if err != nil {
			return edit{}, err
		}
		steps := []string{
			fmt.Sprintf("%s: user %s logs in with `aws %s` instead of aws-iam-authenticator", prettyHome(s.home, f), kc.User, strings.Join(args, " ")),
		}
		if kc.AWSProfile != "" {
			steps = append(steps, fmt.Sprintf("It keeps using AWS profile %s", kc.AWSProfile))
		}
		steps = append(steps, "Other contexts with the same user change too")
		return edit{steps: steps, writes: map[string][]byte{f: out}}, nil
	}
	return edit{}, fmt.Errorf("user %s isn't in the kubeconfig files", kc.User)
}

// MergeInfo describes a kubeconfig file before it's merged in.
type MergeInfo struct {
	Contexts []string `json:"contexts"`
	// Entries already defined: they're skipped, the existing ones stay.
	Conflicts []string `json:"conflicts"`
	Target    string   `json:"target"`
}

// planMerge adds another file's clusters, users and contexts to the first
// kubeconfig file, skipping names that are already defined.
func (s *Service) planMerge(env map[string]string, path string) (edit, MergeInfo, error) {
	var info MergeInfo
	files := s.files(env)
	target := files[0]
	info.Target = target
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return edit{}, info, errors.New("choose a kubeconfig file")
	}
	incoming, err := os.ReadFile(path)
	if err != nil {
		return edit{}, info, err
	}
	var src yaml.Node
	if err := yaml.Unmarshal(incoming, &src); err != nil || len(src.Content) == 0 || src.Content[0].Kind != yaml.MappingNode {
		return edit{}, info, fmt.Errorf("%s isn't a kubeconfig", filepath.Base(path))
	}
	var dst yaml.Node
	existing, err := os.ReadFile(target)
	switch {
	case errors.Is(err, os.ErrNotExist):
		_ = yaml.Unmarshal([]byte("apiVersion: v1\nkind: Config\nclusters: []\ncontexts: []\nusers: []\n"), &dst)
	case err != nil:
		return edit{}, info, err
	default:
		if err := yaml.Unmarshal(existing, &dst); err != nil || len(dst.Content) == 0 {
			return edit{}, info, fmt.Errorf("%s isn't a valid kubeconfig", target)
		}
	}
	list := s.Contexts(env)
	defined := map[string]bool{}
	for _, c := range list.Contexts {
		defined["contexts/"+c.Name] = true
		defined["clusters/"+c.Cluster] = true
		defined["users/"+c.User] = true
	}
	var e edit
	added := 0
	for _, section := range []string{"clusters", "users", "contexts"} {
		items := mapGet(src.Content[0], section)
		if items == nil || items.Kind != yaml.SequenceNode {
			continue
		}
		dseq := mapGet(dst.Content[0], section)
		if dseq == nil || dseq.Kind != yaml.SequenceNode {
			dseq = &yaml.Node{Kind: yaml.SequenceNode}
			mapSet(dst.Content[0], section, dseq)
		}
		for _, it := range items.Content {
			n := mapGet(it, "name")
			if n == nil {
				continue
			}
			key := section + "/" + n.Value
			if defined[key] || findNamed(dst.Content[0], section, n.Value) != nil {
				info.Conflicts = append(info.Conflicts, strings.TrimSuffix(section, "s")+" "+n.Value)
				continue
			}
			dseq.Content = append(dseq.Content, it)
			added++
			if section == "contexts" {
				info.Contexts = append(info.Contexts, n.Value)
			}
			e.steps = append(e.steps, fmt.Sprintf("%s: add %s %s", prettyHome(s.home, target), strings.TrimSuffix(section, "s"), n.Value))
		}
	}
	for _, c := range info.Conflicts {
		e.steps = append(e.steps, fmt.Sprintf("Skip %s: already defined (the existing one stays)", c))
	}
	if added == 0 {
		return edit{}, info, errors.New("nothing to add: everything in it is already defined")
	}
	out, err := yaml.Marshal(&dst)
	if err != nil {
		return edit{}, info, err
	}
	e.writes = map[string][]byte{target: out}
	return e, info, nil
}

// Op names an edit, for PlanEdit and ApplyEdit.
type Op struct {
	Kind string `json:"kind"` // rename, delete, namespace, fixAuth, merge
	// The context, and the new name / namespace / file path.
	Name  string `json:"name"`
	Value string `json:"value"`
	// The kubeconfig the context is in; "" or one of the shell's files
	// means the shell's files.
	File string `json:"file"`
}

// opFiles are the files an edit reads and changes.
func (s *Service) opFiles(env map[string]string, op Op) ([]string, error) {
	if op.Kind == "merge" {
		return s.files(env), nil // into the shell's first file
	}
	return s.scope(env, op.File)
}

func (s *Service) plan(env map[string]string, op Op) (edit, error) {
	files, err := s.opFiles(env, op)
	if err != nil {
		return edit{}, err
	}
	switch op.Kind {
	case "rename":
		return s.planRename(files, env, op.Name, op.Value)
	case "delete":
		return s.planDelete(files, env, op.Name)
	case "namespace":
		return s.planNamespace(files, env, op.Name, op.Value)
	case "fixAuth":
		return s.planFixAuth(files, env, op.Name)
	case "merge":
		e, _, err := s.planMerge(env, op.Value)
		return e, err
	}
	return edit{}, fmt.Errorf("unknown change %q", op.Kind)
}

// PlanEdit lists what a change would do, without doing it.
func (s *Service) PlanEdit(env map[string]string, op Op) ([]string, error) {
	e, err := s.plan(env, op)
	if err != nil {
		return nil, err
	}
	steps := []string{}
	for _, c := range e.commands {
		steps = append(steps, "kubectl "+strings.Join(quoteAll(c), " "))
	}
	return append(steps, e.steps...), nil
}

func quoteAll(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = shellQuote(a)
	}
	return out
}

// ApplyEdit makes a planned change: files unchanged since listing, backed
// up first, then the kubectl commands run or the new contents written.
func (s *Service) ApplyEdit(env map[string]string, op Op, versions map[string]string) (ConfigList, error) {
	files, err := s.opFiles(env, op)
	if err != nil {
		return ConfigList{}, err
	}
	if err := s.checkVersions(files, versions); err != nil {
		return ConfigList{}, err
	}
	e, err := s.plan(env, op)
	if err != nil {
		return ConfigList{}, err
	}
	s.backup(files)
	for path, data := range e.writes {
		if err := writeAtomic(path, data); err != nil {
			return ConfigList{}, err
		}
	}
	if len(e.commands) > 0 {
		r := Request{Env: env}
		bin, err := s.kubectl(r)
		if err != nil {
			return ConfigList{}, err
		}
		// kubectl edits the context's files, never wtf's own.
		runEnv := s.env(Request{Env: map[string]string{"KUBECONFIG": strings.Join(files, string(filepath.ListSeparator)), "PATH": env["PATH"]}})
		for _, c := range e.commands {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			_, err := runKubectl(ctx, bin, runEnv, c...)
			cancel()
			if err != nil {
				return s.Contexts(env), fmt.Errorf("kubectl %s: %v", strings.Join(c, " "), err)
			}
		}
	}
	return s.Contexts(env), nil
}

// BackupConfigs keeps a copy of the shell's kubeconfig files, before a
// command run in a terminal (aws eks update-kubeconfig) changes them.
func (s *Service) BackupConfigs(env map[string]string) {
	s.backup(s.files(env))
}

func (s *Service) backup(files []string) {
	dir := s.backupDir()
	if dir == "" || os.MkdirAll(dir, 0o700) != nil {
		return
	}
	stamp := time.Now().Format("20060102-150405.000")
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		name := strings.ReplaceAll(strings.TrimPrefix(prettyHome(s.home, f), "~/"), "/", "_")
		_ = os.WriteFile(filepath.Join(dir, stamp+"-"+name), data, 0o600)
	}
	des, err := os.ReadDir(dir)
	if err != nil || len(des) <= maxKubeBackups {
		return
	}
	for _, de := range des[:len(des)-maxKubeBackups] {
		_ = os.Remove(filepath.Join(dir, de.Name()))
	}
}

func writeAtomic(path string, data []byte) error {
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
	tmp, err := os.CreateTemp(filepath.Dir(target), ".wtf-kube-*")
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

func prettyHome(home, path string) string {
	if home != "" && strings.HasPrefix(path, home+"/") {
		return "~" + path[len(home):]
	}
	return path
}

// ── YAML node helpers ────────────────────────────────────────────────────

func mapGet(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func mapSet(m *yaml.Node, key string, v *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = v
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, v)
}

// findNamed finds the item named `name` in a top-level list (users, …).
func findNamed(root *yaml.Node, section, name string) *yaml.Node {
	seq := mapGet(root, section)
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return nil
	}
	for _, it := range seq.Content {
		if n := mapGet(it, "name"); n != nil && n.Value == name {
			return it
		}
	}
	return nil
}

// ── EKS ──────────────────────────────────────────────────────────────────

var awsRegion = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-\d+$`)

// EKSClusters lists the EKS clusters an AWS profile sees in a region, for
// the Add EKS Cluster dialog (when asked).
func (s *Service) EKSClusters(env map[string]string, profile, region string) ([]string, error) {
	if !awsRegion.MatchString(region) {
		return nil, fmt.Errorf("%q isn't a region, like us-east-1", region)
	}
	if strings.HasPrefix(profile, "-") || strings.ContainsAny(profile, " \t\r\n") {
		return nil, fmt.Errorf("%q isn't a profile name", profile)
	}
	bin := infra.FindAWS()
	if p := s.findPlugin("aws", env); p != "" {
		bin = p
	}
	if bin == "" {
		return nil, errors.New("the aws CLI isn't installed")
	}
	args := []string{"eks", "list-clusters", "--region", region, "--output", "json"}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	runEnv := append(infra.AWSEnv(filepath.Join(s.home, ".aws", "config"), filepath.Join(s.home, ".aws", "credentials")), "PATH="+env["PATH"])
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := runKubectl(ctx, bin, runEnv, args...)
	if err != nil {
		return nil, err
	}
	var r struct{ Clusters []string }
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, fmt.Errorf("unexpected answer from AWS: %.60q", out)
	}
	if r.Clusters == nil {
		r.Clusters = []string{}
	}
	sort.Strings(r.Clusters)
	return r.Clusters, nil
}

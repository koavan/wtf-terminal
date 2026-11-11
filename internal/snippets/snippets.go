// Package snippets reads the user's command snippets and runbooks: commands
// with {{placeholders}} filled in from the command palette, kept in plain
// TOML files so a team can share them through git.
//
// Files, in the order they're listed:
//
//   - .wtf/snippets.toml in the shell's folder or any parent (a project's
//     own runbooks, committed with it), nearest first;
//   - ~/.config/wtf/snippets.toml, the user's own;
//   - ~/.config/wtf/snippets/*.toml, e.g. a cloned team repo.
//
// Placeholders are filled in by the UI (internal/uicore, WtfCore on macOS);
// this package only reads the files, and appends to one for Save as Snippet.
package snippets

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Param describes a placeholder: what to ask, and how to fill it in.
type Param struct {
	Name        string `json:"name" toml:"-"`
	Description string `json:"description" toml:"description,omitempty"`
	// A literal, or $kube_namespace, $kube_context, $aws_profile,
	// $aws_region or $cwd for the focused shell's.
	Default string   `json:"default" toml:"default,omitempty"`
	Choices []string `json:"choices" toml:"choices,omitempty"`
	// Inserted as typed, not shell-quoted (several flags, a pipeline…).
	Raw bool `json:"raw" toml:"raw,omitempty"`
}

type Step struct {
	Title   string `json:"title" toml:"title,omitempty"`
	Command string `json:"command" toml:"command"`
}

type Snippet struct {
	// Unique in a list: file and position.
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	// One command, or Steps for a runbook.
	Command string `json:"command"`
	Steps   []Step `json:"steps"`
	// Declared parameters (placeholders without one just ask for a value).
	Params []Param `json:"params"`
	Source string  `json:"source"`
	// Its position among the file's [[snippet]] blocks, for editing.
	Index int `json:"index"`
	// The source is a project's .wtf/snippets.toml.
	Project bool `json:"project"`
}

// Target is a file snippets can be saved to.
type Target struct {
	Path string `json:"path"`
	// project, user or team.
	Kind   string `json:"kind"`
	Exists bool   `json:"exists"`
}

type List struct {
	Snippets   []Snippet   `json:"snippets"`
	Workspaces []Workspace `json:"workspaces"`
	// The files that were read (existing ones only).
	Files    []string `json:"files"`
	Problems []string `json:"problems"`
	// The user's own file, and the nearest project file if there is one.
	UserFile    string `json:"userFile"`
	ProjectFile string `json:"projectFile"`
	// Where snippets can be saved: the files above, a new project file at
	// the repository's root when there's none yet, and team files.
	Targets  []Target          `json:"targets"`
	Versions map[string]string `json:"versions"`
}

type fileSnippet struct {
	Name        string           `toml:"name"`
	Description string           `toml:"description,omitempty"`
	Tags        []string         `toml:"tags,omitempty"`
	Command     string           `toml:"command,omitempty"`
	Steps       []Step           `toml:"steps,omitempty"`
	Params      map[string]Param `toml:"params,omitempty"`
}

type file struct {
	Snippet   []fileSnippet   `toml:"snippet,omitempty"`
	Workspace []fileWorkspace `toml:"workspace,omitempty"`
}

const maxFile = 1 << 20

type Service struct {
	home      string
	configDir string
}

func NewService(configDir string) *Service {
	home, _ := os.UserHomeDir()
	return &Service{home: home, configDir: configDir}
}

func (s *Service) userFile() string { return filepath.Join(s.configDir, "snippets.toml") }

// projectFiles are the .wtf/snippets.toml files from cwd up, nearest first,
// stopping at the home folder (or the root).
func (s *Service) projectFiles(cwd string) []string {
	var out []string
	if cwd == "" || !filepath.IsAbs(cwd) {
		return nil
	}
	for dir := filepath.Clean(cwd); ; dir = filepath.Dir(dir) {
		p := filepath.Join(dir, ".wtf", "snippets.toml")
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			out = append(out, p)
		}
		if dir == s.home || dir == filepath.Dir(dir) {
			break
		}
	}
	return out
}

func version(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:12])
}

// List reads the snippets available in a shell's folder.
func (s *Service) List(cwd string) List {
	out := List{Snippets: []Snippet{}, Workspaces: []Workspace{}, Files: []string{}, Problems: []string{}, UserFile: s.userFile(), Versions: map[string]string{}}
	project := s.projectFiles(cwd)
	if len(project) > 0 {
		out.ProjectFile = project[0]
	}
	paths := append([]string{}, project...)
	paths = append(paths, s.userFile())
	if extra, err := filepath.Glob(filepath.Join(s.configDir, "snippets", "*.toml")); err == nil {
		sort.Strings(extra)
		paths = append(paths, extra...)
	}
	isProject := map[string]bool{}
	for _, p := range project {
		isProject[p] = true
	}

	out.Versions[s.userFile()] = ""
	for _, p := range paths {
		data, err := readSmall(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			out.Problems = append(out.Problems, err.Error())
			continue
		}
		out.Files = append(out.Files, p)
		out.Versions[p] = version(data)
		var f file
		if _, err := toml.Decode(string(data), &f); err != nil {
			out.Problems = append(out.Problems, fmt.Sprintf("%s: %v", prettyHome(s.home, p), err))
			continue
		}
		for i, fs := range f.Snippet {
			sn, err := convert(fs)
			if err != nil {
				out.Problems = append(out.Problems, fmt.Sprintf("%s, snippet %d: %v", prettyHome(s.home, p), i+1, err))
				continue
			}
			sn.ID = fmt.Sprintf("%s#%d", p, i)
			sn.Source = p
			sn.Index = i
			sn.Project = isProject[p]
			out.Snippets = append(out.Snippets, sn)
		}
		for i, fw := range f.Workspace {
			w, err := convertWorkspace(fw)
			if err != nil {
				out.Problems = append(out.Problems, fmt.Sprintf("%s, workspace %d: %v", prettyHome(s.home, p), i+1, err))
				continue
			}
			w.ID = fmt.Sprintf("%s#w%d", p, i)
			w.Source = p
			w.Index = i
			w.Project = isProject[p]
			out.Workspaces = append(out.Workspaces, w)
		}
	}
	out.Targets = s.targets(cwd)
	return out
}

// targets are the files snippets may be saved to, from a shell's folder.
func (s *Service) targets(cwd string) []Target {
	var out []Target
	project := s.projectFiles(cwd)
	if len(project) > 0 {
		out = append(out, Target{Path: project[0], Kind: "project", Exists: true})
	} else if root := projectRoot(cwd, s.home); root != "" {
		out = append(out, Target{Path: filepath.Join(root, ".wtf", "snippets.toml"), Kind: "project"})
	}
	_, err := os.Stat(s.userFile())
	out = append(out, Target{Path: s.userFile(), Kind: "user", Exists: err == nil})
	if extra, err := filepath.Glob(filepath.Join(s.configDir, "snippets", "*.toml")); err == nil {
		sort.Strings(extra)
		for _, p := range extra {
			out = append(out, Target{Path: p, Kind: "team", Exists: true})
		}
	}
	for _, p := range project[min(1, len(project)):] {
		out = append(out, Target{Path: p, Kind: "project", Exists: true})
	}
	return out
}

// projectRoot is the repository a folder is in (the nearest parent with a
// .git), for a new project snippets file; "" outside one, or for home itself.
func projectRoot(cwd, home string) string {
	if cwd == "" || !filepath.IsAbs(cwd) {
		return ""
	}
	for dir := filepath.Clean(cwd); dir != home && dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
	}
	return ""
}

func readSmall(p string) ([]byte, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if fi.Size() > maxFile {
		return nil, fmt.Errorf("%s is too big for a snippets file", p)
	}
	return os.ReadFile(p)
}

func convert(fs fileSnippet) (Snippet, error) {
	sn := Snippet{Name: strings.TrimSpace(fs.Name), Description: strings.TrimSpace(fs.Description), Tags: fs.Tags,
		Command: strings.TrimSpace(fs.Command), Steps: []Step{}, Params: []Param{}}
	if sn.Tags == nil {
		sn.Tags = []string{}
	}
	switch {
	case sn.Name == "":
		return sn, errors.New("it needs a name")
	case sn.Command == "" && len(fs.Steps) == 0:
		return sn, errors.New("it needs a command, or steps")
	case sn.Command != "" && len(fs.Steps) > 0:
		return sn, errors.New("give it a command or steps, not both")
	}
	for i, st := range fs.Steps {
		st.Command = strings.TrimSpace(st.Command)
		if st.Command == "" {
			return sn, fmt.Errorf("step %d has no command", i+1)
		}
		sn.Steps = append(sn.Steps, st)
	}
	if strings.ContainsAny(sn.Command, "\r\n") {
		return sn, errors.New("a command must be one line; use steps for several")
	}
	for _, st := range sn.Steps {
		if strings.ContainsAny(st.Command, "\r\n") {
			return sn, errors.New("each step's command must be one line")
		}
	}
	sn.Params = paramList(fs.Params)
	return sn, nil
}

// paramList is a file's [….params.NAME] tables as a list, by name.
func paramList(m map[string]Param) []Param {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	out := []Param{}
	for _, n := range names {
		p := m[n]
		p.Name = n
		if p.Choices == nil {
			p.Choices = []string{}
		}
		out = append(out, p)
	}
	return out
}

// cleanParams checks edited parameters and keeps those with something to say.
func cleanParams(in []Param) (map[string]Param, error) {
	var out map[string]Param
	for _, p := range in {
		if !paramNameRe.MatchString(p.Name) {
			return nil, fmt.Errorf("%q isn't a placeholder name", p.Name)
		}
		p.Description, p.Default = strings.TrimSpace(p.Description), strings.TrimSpace(p.Default)
		var choices []string
		for _, c := range p.Choices {
			if c = strings.TrimSpace(c); c != "" {
				choices = append(choices, c)
			}
		}
		p.Choices = choices
		if p.Description == "" && p.Default == "" && len(p.Choices) == 0 && !p.Raw {
			continue
		}
		if out == nil {
			out = map[string]Param{}
		}
		out[p.Name] = p
	}
	return out, nil
}

// ── Editing ──────────────────────────────────────────────────────────────

// Def is a snippet as the editor fills it in.
type Def struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Command     string   `json:"command"`
	Steps       []Step   `json:"steps"`
	Params      []Param  `json:"params"`
}

// Edit adds a snippet or workspace to a file (Index -1) or replaces one.
type Edit struct {
	// "snippet" (or "") or "workspace".
	Kind  string `json:"kind"`
	File  string `json:"file"`
	Index int    `json:"index"`
	// The file's version from List ("" for a file that doesn't exist yet).
	Version   string       `json:"version"`
	Snippet   Def          `json:"snippet"`
	Workspace WorkspaceDef `json:"workspace"`
}

// Preview is what an edit writes.
type Preview struct {
	File string `json:"file"`
	// The snippet's block, as it will be in the file.
	Text string `json:"text"`
	// The file doesn't exist yet and will be created.
	NewFile bool `json:"newFile"`
}

const header = `# wtf snippets: commands with {{placeholders}}, run from the command palette
# (Run Snippet…) or the Snippets side-pane tab. {{name}} asks for a value
# (shell-quoted for you); {{ns=default}} has a default, which may be
# $kube_namespace, $kube_context, $aws_profile, $aws_region or $cwd.
# See the manual's Snippets page.
`

var (
	errChanged  = errors.New("the snippets file changed since it was read — reload and try again")
	topLevel    = regexp.MustCompile(`^\s*\[\[\s*(snippet|workspace)\s*\]\]\s*(#.*)?$`)
	paramNameRe = regexp.MustCompile(`^[A-Za-z_][\w-]*$`)
)

// kindOf is an edit's kind, checked.
func kindOf(k string) (string, error) {
	switch k {
	case "", "snippet":
		return "snippet", nil
	case "workspace":
		return "workspace", nil
	}
	return "", fmt.Errorf("unknown kind %q", k)
}

// PlanEdit renders what an edit will write, without writing.
func (s *Service) PlanEdit(e Edit, cwd string) (Preview, error) {
	target, text, err := s.prepare(e, cwd)
	if err != nil {
		return Preview{}, err
	}
	_, statErr := os.Stat(target)
	return Preview{File: target, Text: text, NewFile: errors.Is(statErr, os.ErrNotExist)}, nil
}

// ApplyEdit makes an edit, if the file is still the version listed. Only
// the snippet's own block changes; the rest of the file stays as written.
func (s *Service) ApplyEdit(e Edit, cwd string) (List, error) {
	target, text, err := s.prepare(e, cwd)
	if err != nil {
		return List{}, err
	}
	lines, err := s.readFor(target, e.Version)
	if err != nil {
		return List{}, err
	}
	block := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if e.Index < 0 {
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, block...)
	} else {
		kind, _ := kindOf(e.Kind)
		from, to, ok := blockRange(lines, kind, e.Index)
		if !ok {
			return List{}, errChanged
		}
		lines = append(lines[:from], append(block, lines[to:]...)...)
	}
	if err := writeAtomic(target, []byte(strings.Join(lines, "\n")+"\n")); err != nil {
		return List{}, err
	}
	return s.List(cwd), nil
}

// Delete removes a snippet's or workspace's block from its file.
func (s *Service) Delete(kind, file string, index int, version, cwd string) (List, error) {
	kind, err := kindOf(kind)
	if err != nil {
		return List{}, err
	}
	target, err := s.writable(file, cwd)
	if err != nil {
		return List{}, err
	}
	lines, err := s.readFor(target, version)
	if err != nil {
		return List{}, err
	}
	from, to, ok := blockRange(lines, kind, index)
	if !ok {
		return List{}, errChanged
	}
	// Don't leave a double blank line behind.
	if to < len(lines) && strings.TrimSpace(lines[to]) == "" && (from == 0 || strings.TrimSpace(lines[from-1]) == "") {
		to++
	}
	lines = append(lines[:from], lines[to:]...)
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if err := writeAtomic(target, []byte(strings.Join(lines, "\n")+"\n")); err != nil {
		return List{}, err
	}
	return s.List(cwd), nil
}

// readFor reads a file to change, checking it's the version listed. A
// missing file starts as the explanation at the top of every snippets file.
func (s *Service) readFor(path, want string) ([]string, error) {
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if want != "" {
			return nil, errChanged
		}
		return strings.Split(strings.TrimRight(header, "\n"), "\n"), nil
	case err != nil:
		return nil, err
	case version(data) != want:
		return nil, errChanged
	}
	return strings.Split(strings.TrimRight(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n"), "\n"), nil
}

// blockRange finds the lines of the index-th [[snippet]] (or [[workspace]])
// block: from its header up to the next top-level block of either kind,
// less the comments and blank lines just before it (they introduce it).
func blockRange(lines []string, kind string, index int) (int, int, bool) {
	var all, mine []int
	for i, l := range lines {
		if m := topLevel.FindStringSubmatch(l); m != nil {
			all = append(all, i)
			if m[1] == kind {
				mine = append(mine, i)
			}
		}
	}
	if index < 0 || index >= len(mine) {
		return 0, 0, false
	}
	from, to := mine[index], len(lines)
	for _, h := range all {
		if h > from {
			to = h
			break
		}
	}
	for to > from+1 {
		t := strings.TrimSpace(lines[to-1])
		if t != "" && !strings.HasPrefix(t, "#") {
			break
		}
		to--
	}
	return from, to, true
}

// writable checks a file is one snippets may be saved to from here.
func (s *Service) writable(file, cwd string) (string, error) {
	target := filepath.Clean(file)
	for _, t := range s.targets(cwd) {
		if t.Path == target {
			return target, nil
		}
	}
	return "", fmt.Errorf("%s isn't a snippets file wtf saves to from here", prettyHome(s.home, file))
}

// prepare checks an edit and renders its block.
func (s *Service) prepare(e Edit, cwd string) (string, string, error) {
	kind, err := kindOf(e.Kind)
	if err != nil {
		return "", "", err
	}
	target, err := s.writable(e.File, cwd)
	if err != nil {
		return "", "", err
	}
	if kind == "workspace" {
		fw, err := fromDef(e.Workspace)
		if err != nil {
			return "", "", err
		}
		var buf bytes.Buffer
		enc := toml.NewEncoder(&buf)
		enc.Indent = ""
		if err := enc.Encode(file{Workspace: []fileWorkspace{fw}}); err != nil {
			return "", "", err
		}
		return target, buf.String(), nil
	}
	d := e.Snippet
	fs := fileSnippet{
		Name: strings.TrimSpace(d.Name), Description: strings.TrimSpace(d.Description),
		Command: strings.TrimSpace(d.Command),
	}
	for _, t := range d.Tags {
		if t = strings.TrimSpace(t); t != "" {
			fs.Tags = append(fs.Tags, t)
		}
	}
	for _, st := range d.Steps {
		st.Title, st.Command = strings.TrimSpace(st.Title), strings.TrimSpace(st.Command)
		if st.Title == "" && st.Command == "" {
			continue
		}
		fs.Steps = append(fs.Steps, st)
	}
	params, err := cleanParams(d.Params)
	if err != nil {
		return "", "", err
	}
	fs.Params = params
	if strings.ContainsAny(fs.Name, "\r\n") || strings.ContainsAny(fs.Description, "\r\n") {
		return "", "", errors.New("the name and description must be one line each")
	}
	if _, err := convert(fs); err != nil {
		return "", "", err
	}
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(file{Snippet: []fileSnippet{fs}}); err != nil {
		return "", "", err
	}
	return target, buf.String(), nil
}

// OpenUserFile creates the user's snippets file with examples if needed,
// and opens it in the default text editor.
func (s *Service) OpenUserFile() error {
	p := s.userFile()
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		if err := writeAtomic(p, []byte(header+examples)); err != nil {
			return err
		}
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("/usr/bin/open", "-t", p)
	} else {
		cmd = exec.Command("xdg-open", p)
	}
	return cmd.Start()
}

const examples = `
# Examples — edit or delete them.

[[snippet]]
name = "Restart a deployment"
description = "Rolls every pod of a deployment, one at a time"
command = "kubectl -n {{ns=$kube_namespace}} rollout restart deploy/{{name}}"

[[snippet]]
name = "Tail a pod's logs"
command = "kubectl -n {{ns=$kube_namespace}} logs -f --tail={{lines=200}} {{pod}}"

[[snippet]]
name = "Who am I in AWS"
command = "aws sts get-caller-identity --profile {{profile=$aws_profile}}"

[[snippet]]
name = "Find big files"
command = "du -ah {{folder=.}} | sort -rh | head -n {{count=20}}"

# A runbook: steps you run one at a time, with values filled in once.
[[snippet]]
name = "Drain a node for maintenance"
description = "Cordon, drain, then uncordon when the work is done"

[[snippet.steps]]
title = "Stop new pods landing on it"
command = "kubectl cordon {{node}}"

[[snippet.steps]]
title = "Move its pods elsewhere"
command = "kubectl drain {{node}} --ignore-daemonsets --delete-emptydir-data"

[[snippet.steps]]
title = "After maintenance: let pods back"
command = "kubectl uncordon {{node}}"

[snippet.params.node]
description = "The node's name (kubectl get nodes)"

# A workspace: tabs opened together (Open Workspace… in the command palette).
[[workspace]]
name = "Kubernetes on call"
description = "Pods and events of a namespace, side by side, and a shell"

[[workspace.tab]]
title = "{{ns}} pods"
group = "on call"
color = "red"
layout = "row"

[[workspace.tab.pane]]
command = "kubectl -n {{ns}} get pods -w"

[[workspace.tab.pane]]
command = "kubectl -n {{ns}} get events -w"

[[workspace.tab]]
title = "shell"
group = "on call"
cwd = "~"

[workspace.params.ns]
default = "$kube_namespace"
`

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".wtf-snippets-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func prettyHome(home, path string) string {
	if home != "" && strings.HasPrefix(path, home+"/") {
		return "~" + path[len(home):]
	}
	return path
}

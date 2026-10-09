package sshhosts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Adding, editing and removing Host entries in the user's config files.
//
// Edits touch as little as possible: a changed setting keeps its line,
// indentation and comments; removed settings lose their line; new ones go
// after the entry's last setting. New hosts go into ~/.ssh/config before
// its first `Host *` or Match block, because ssh takes the first value it
// finds and a host after `Host *` would get its defaults instead of its own.
//
// Every change is checked first: the file must not have changed since it
// was listed, and `ssh -G` must accept the new contents (an unknown keyword
// would break every ssh command). Writes are atomic, follow a symlinked
// config to its target, and keep a copy of the old file in wtf's cache.

// Entry is a Host entry to write.
type Entry struct {
	// The config file; "" means ~/.ssh/config.
	File string `json:"file"`
	// The Host line (1-based) of the entry to change; 0 adds a new entry.
	Line int `json:"line"`
	// The file's version from List.
	Version  string   `json:"version"`
	Patterns []string `json:"patterns"`
	Options  []Option `json:"options"`
}

const (
	defaultIndent = "    "
	maxBackups    = 30
	checkTimeout  = 5 * time.Second
)

var (
	keyword    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)
	errChanged = errors.New("the file changed since the list was loaded — reload and try again")
)

// Values that are a single argument, quoted when they contain spaces (a
// key file under "~/My Keys"). Others, like LocalForward, take several.
var singleArg = map[string]bool{
	"hostname": true, "user": true, "port": true, "identityfile": true, "proxyjump": true,
	"certificatefile": true, "identityagent": true, "userknownhostsfile": true, "controlpath": true,
}

// Save adds a Host entry or changes one, and returns the new list.
func (s *Service) Save(e Entry) (List, error) {
	file, err := s.target(e.File)
	if err != nil {
		return List{}, err
	}
	patterns, opts, err := clean(e.Patterns, e.Options)
	if err != nil {
		return List{}, err
	}
	data, existed, err := s.read(file, e.Version)
	if err != nil {
		return List{}, err
	}
	if err := s.checkDuplicates(patterns, file, e.Line); err != nil {
		return List{}, err
	}

	lines := splitLines(data)
	if e.Line == 0 {
		lines = addEntry(lines, patterns, opts)
	} else {
		ent := findEntry(lines, e.Line)
		if ent == nil {
			return List{}, errChanged
		}
		lines = changeEntry(lines, ent, patterns, opts)
	}
	out := []byte(strings.Join(lines, "\n") + "\n")
	if err := checkConfig(out, patterns); err != nil {
		return List{}, err
	}
	if err := s.write(file, out, data, existed); err != nil {
		return List{}, err
	}
	return s.List(), nil
}

// Delete removes the Host entry at a line (1-based) of a config file.
func (s *Service) Delete(file string, line int, version string) (List, error) {
	file, err := s.target(file)
	if err != nil {
		return List{}, err
	}
	data, existed, err := s.read(file, version)
	if err != nil {
		return List{}, err
	}
	lines := splitLines(data)
	ent := findEntry(lines, line)
	if !existed || ent == nil {
		return List{}, errChanged
	}
	from, to := ent.line, ent.last+1
	// Don't leave two blank lines where the entry was.
	if to < len(lines) && strings.TrimSpace(lines[to]) == "" && (from == 0 || strings.TrimSpace(lines[from-1]) == "") {
		to++
	}
	lines = append(lines[:from:from], lines[to:]...)
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	out := []byte(strings.Join(lines, "\n"))
	if len(lines) > 0 {
		out = append(out, '\n')
	}
	if err := s.write(file, out, data, true); err != nil {
		return List{}, err
	}
	return s.List(), nil
}

// Keys lists the private keys in ~/.ssh (files with a .pub next to them),
// as ~/.ssh/… paths, for the IdentityFile field.
func (s *Service) Keys() []string {
	des, err := os.ReadDir(s.sshDir())
	if err != nil {
		return []string{}
	}
	out := []string{}
	for _, de := range des {
		name := de.Name()
		if de.IsDir() || strings.HasSuffix(name, ".pub") {
			continue
		}
		if _, err := os.Stat(filepath.Join(s.sshDir(), name+".pub")); err == nil {
			out = append(out, "~/.ssh/"+name)
		}
	}
	return out
}

// target resolves and checks the file an edit is for.
func (s *Service) target(file string) (string, error) {
	if file == "" {
		return s.mainConfig(), nil
	}
	file = filepath.Clean(file)
	if !filepath.IsAbs(file) || !s.editable(file) {
		return "", fmt.Errorf("%s isn't in ~/.ssh, so wtf leaves it to the program that manages it", file)
	}
	return file, nil
}

// read returns a file's contents if it is still the version that was listed.
func (s *Service) read(file, want string) (data []byte, existed bool, err error) {
	data, err = os.ReadFile(file)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if want != "" {
			return nil, false, errChanged
		}
		return nil, false, nil
	case err != nil:
		return nil, false, err
	case version(data) != want:
		return nil, false, errChanged
	}
	return data, true, nil
}

// clean validates an entry and trims its values. Options with no value are
// dropped: that's how a field is cleared.
func clean(patterns []string, opts []Option) ([]string, []Option, error) {
	var pats []string
	concrete := false
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.ContainsAny(p, " \t\"#\r\n") {
			return nil, nil, fmt.Errorf("host name %q can't contain spaces, quotes or #", p)
		}
		if !strings.ContainsAny(p, "*?!") {
			concrete = true
		}
		pats = append(pats, p)
	}
	if !concrete {
		return nil, nil, errors.New("give the host a name to connect with (without * or ?)")
	}

	var out []Option
	for _, o := range opts {
		key, value := strings.TrimSpace(o.Key), strings.TrimSpace(o.Value)
		if value == "" {
			continue
		}
		lower := strings.ToLower(key)
		switch {
		case !keyword.MatchString(key):
			return nil, nil, fmt.Errorf("%q isn't a setting name", key)
		case lower == "host" || lower == "match" || lower == "include":
			return nil, nil, fmt.Errorf("%s can't be used inside a host entry", key)
		case strings.ContainsAny(value, "\r\n"):
			return nil, nil, fmt.Errorf("%s: a value can't span lines", key)
		case lower == "port":
			if n, err := strconv.Atoi(value); err != nil || n < 1 || n > 65535 {
				return nil, nil, fmt.Errorf("port %q must be a number from 1 to 65535", value)
			}
		}
		if singleArg[lower] && strings.ContainsAny(value, " \t") && !strings.HasPrefix(value, `"`) {
			value = `"` + value + `"`
		}
		out = append(out, Option{Key: key, Value: value})
	}
	return pats, out, nil
}

// checkDuplicates refuses names another entry already declares: ssh would
// keep using the earlier entry's settings.
func (s *Service) checkDuplicates(patterns []string, file string, line int) error {
	p := parser{home: s.home, seen: map[string]bool{}, contents: map[string][]byte{}}
	p.parseFile(s.mainConfig(), 0)
	if !p.seen[file] {
		// A file the main config doesn't include: check it on its own too.
		p.parseFile(file, 0)
	}
	for _, pat := range patterns {
		key := strings.ToLower(pat)
		src, ok := p.sources[key]
		if !ok || strings.ContainsAny(pat, "*?!") {
			continue
		}
		if src != file || p.lines[key] != line-1 {
			return fmt.Errorf("%s is already defined in %s, line %d", pat, prettyHome(s.home, src), p.lines[key]+1)
		}
	}
	return nil
}

func prettyHome(home, path string) string {
	if home != "" && strings.HasPrefix(path, home+"/") {
		return "~" + path[len(home):]
	}
	return path
}

// findEntry is the entry whose Host line is `line` (1-based).
func findEntry(lines []string, line int) *entry {
	for _, e := range parseEntries(lines) {
		if e.line == line-1 {
			return e
		}
	}
	return nil
}

// indentOf is the indentation the file uses for settings.
func indentOf(lines []string) string {
	for _, e := range parseEntries(lines) {
		for _, o := range e.opts {
			if ind := o.prefix[:len(o.prefix)-len(strings.TrimLeft(o.prefix, " \t"))]; ind != "" {
				return ind
			}
		}
	}
	return defaultIndent
}

func hostLine(indent string, patterns []string) string {
	return indent + "Host " + strings.Join(patterns, " ")
}

// addEntry puts a new entry before the first `Host *` or Match block (and
// the comments right above it), else at the end.
func addEntry(lines []string, patterns []string, opts []Option) []string {
	indent := indentOf(lines)
	block := []string{hostLine("", patterns)}
	for _, o := range opts {
		block = append(block, indent+o.Key+" "+o.Value)
	}

	at := len(lines)
	for i, line := range lines {
		key, _, _ := splitRaw(line)
		if key == "match" {
			at = i
			break
		}
		if key == "host" {
			if _, args := splitLine(line); len(args) == 1 && args[0] == "*" {
				at = i
				break
			}
		}
	}
	for at > 0 && strings.HasPrefix(strings.TrimSpace(lines[at-1]), "#") {
		at--
	}

	var out []string
	out = append(out, lines[:at]...)
	if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
		out = append(out, "")
	}
	out = append(out, block...)
	if at < len(lines) {
		if strings.TrimSpace(lines[at]) != "" {
			out = append(out, "")
		}
		out = append(out, lines[at:]...)
	}
	return out
}

// changeEntry rewrites an entry in place: settings that stay keep their
// lines (the first line of each key is reused), the rest are removed, and
// new ones are added after the entry's last setting.
func changeEntry(lines []string, ent *entry, patterns []string, opts []Option) []string {
	out := append([]string(nil), lines...)
	if !sameStrings(ent.patterns, patterns) {
		indent := lines[ent.line][:len(lines[ent.line])-len(strings.TrimLeft(lines[ent.line], " \t"))]
		out[ent.line] = hostLine(indent, patterns)
	}

	used := map[int]bool{}
	var added []string
	indent := defaultIndent
	if len(ent.opts) > 0 {
		p := ent.opts[0].prefix
		indent = p[:len(p)-len(strings.TrimLeft(p, " \t"))]
	} else {
		indent = indentOf(lines)
	}
	for _, o := range opts {
		reused := false
		for i, old := range ent.opts {
			if !used[i] && old.key == strings.ToLower(o.Key) {
				used[i] = true
				reused = true
				out[old.line] = old.prefix + o.Value
				break
			}
		}
		if !reused {
			added = append(added, indent+o.Key+" "+o.Value)
		}
	}

	var drop []int
	for i, old := range ent.opts {
		if !used[i] {
			drop = append(drop, old.line)
		}
	}
	// Insert the new settings after the last line of the entry, then remove
	// dropped lines from the bottom up so indexes stay valid.
	at := ent.last + 1
	out = append(out[:at], append(added, out[at:]...)...)
	sort.Sort(sort.Reverse(sort.IntSlice(drop)))
	for _, i := range drop {
		out = append(out[:i], out[i+1:]...)
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// checkConfig asks ssh whether it accepts a config. `ssh -G` only reads
// the config and prints the result; it doesn't connect.
func checkConfig(data []byte, patterns []string) error {
	bin, err := exec.LookPath("ssh")
	if err != nil {
		bin = "/usr/bin/ssh"
		if _, err := os.Stat(bin); err != nil {
			return nil // nothing to check with
		}
	}
	f, err := os.CreateTemp("", "wtf-ssh-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	f.Close()

	alias := patterns[0]
	for _, p := range patterns {
		if !strings.ContainsAny(p, "*?!") {
			alias = p
			break
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-G", "-F", f.Name(), alias)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var lines []string
		for _, l := range strings.Split(strings.ReplaceAll(stderr.String(), f.Name(), "config"), "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.Contains(l, "Pseudo-terminal") && !strings.Contains(l, "terminating,") {
				lines = append(lines, l)
			}
		}
		msg := strings.Join(lines, "; ")
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("ssh doesn't accept this: %s", msg)
	}
	return nil
}

// write replaces a file atomically, keeping its permissions, after saving
// a copy of the old contents.
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
	tmp, err := os.CreateTemp(filepath.Dir(target), ".wtf-ssh-*")
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
	return os.Rename(tmp.Name(), target)
}

// backup keeps the last few versions of edited files in wtf's cache.
func (s *Service) backup(path string, data []byte) {
	if s.backupDir == "" || os.MkdirAll(s.backupDir, 0o700) != nil {
		return
	}
	name := strings.ReplaceAll(strings.TrimPrefix(prettyHome(s.home, path), "~/"), "/", "_")
	stamp := time.Now().Format("20060102-150405.000")
	_ = os.WriteFile(filepath.Join(s.backupDir, stamp+"-"+name), data, 0o600)

	des, err := os.ReadDir(s.backupDir)
	if err != nil || len(des) <= maxBackups {
		return
	}
	// Names start with the time, so the oldest sort first.
	for _, de := range des[:len(des)-maxBackups] {
		_ = os.Remove(filepath.Join(s.backupDir, de.Name()))
	}
}

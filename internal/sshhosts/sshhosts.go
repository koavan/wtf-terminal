// Package sshhosts lists the hosts a user can ssh into, for the side pane:
// the Host entries of ~/.ssh/config (and the files it includes), then the
// servers in ~/.ssh/known_hosts that the config doesn't already name.
//
// Only what's needed to show and open a host is read. Settings are worked
// out the way ssh does for Host blocks: the first value found wins, and
// blocks with wildcards (Host *) contribute to every host they match.
// Match blocks can't be evaluated without connecting, so they're skipped.
package sshhosts

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Host is one entry of the list.
type Host struct {
	// What to pass to ssh: the Host alias, or a known host's name.
	Alias    string `json:"alias"`
	HostName string `json:"hostName"`
	User     string `json:"user"`
	Port     string `json:"port"`
	// A jump host the connection goes through, if any.
	ProxyJump string `json:"proxyJump"`
	// The config file the Host entry is in; "" for known hosts.
	Source string `json:"source"`
	// Only in known_hosts: connected to before, but not configured.
	Known bool `json:"known"`
}

type List struct {
	Hosts []Host `json:"hosts"`
	// Files that couldn't be read, and other things worth knowing.
	Problems []string `json:"problems"`
}

// Hosts used for git, not for a shell: listing them would only be noise.
var gitHosts = map[string]bool{
	"github.com": true, "ssh.github.com": true, "gitlab.com": true, "altssh.gitlab.com": true,
	"bitbucket.org": true, "altssh.bitbucket.org": true, "ssh.dev.azure.com": true,
	"vs-ssh.visualstudio.com": true, "source.developers.google.com": true, "git.sr.ht": true,
	"codeberg.org": true,
}

// Includes nest at most this deep, like ssh.
const maxDepth = 16

type Service struct {
	home string
}

func NewService() *Service {
	home, _ := os.UserHomeDir()
	return &Service{home: home}
}

// List reads the hosts afresh: the files are small, and this runs only
// when the SSH view opens or is refreshed.
func (s *Service) List() List {
	p := parser{home: s.home, seen: map[string]bool{}}
	p.parseFile(filepath.Join(s.home, ".ssh", "config"), 0)

	out := List{Hosts: []Host{}, Problems: p.problems}
	configured := map[string]bool{}
	for _, alias := range p.aliases {
		h := p.resolve(alias)
		if gitHosts[strings.ToLower(h.HostName)] {
			continue
		}
		out.Hosts = append(out.Hosts, h)
		configured[strings.ToLower(alias)] = true
		configured[endpoint(h.HostName, h.Port)] = true
	}
	for _, h := range knownHosts(filepath.Join(s.home, ".ssh", "known_hosts")) {
		if configured[strings.ToLower(h.Alias)] || configured[endpoint(h.HostName, h.Port)] {
			continue
		}
		out.Hosts = append(out.Hosts, h)
	}
	return out
}

// endpoint identifies a server by name and port, for spotting duplicates.
func endpoint(host, port string) string {
	if port == "" {
		port = "22"
	}
	return strings.ToLower(host) + ":" + port
}

// ── ssh_config ───────────────────────────────────────────────────────────

type block struct {
	patterns []string
	match    bool // a Match block: never applies here
	options  map[string]string
	source   string
}

type parser struct {
	home     string
	blocks   []*block
	aliases  []string
	sources  map[string]string // alias → file it was declared in
	seen     map[string]bool
	problems []string
}

// parseFile reads one config file. The user's main config may be missing;
// a missing include is fine too (ssh ignores it), unreadable ones are noted.
func (p *parser) parseFile(path string, depth int) {
	if depth > maxDepth || p.seen[path] {
		return
	}
	p.seen[path] = true
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			p.problems = append(p.problems, err.Error())
		}
		return
	}
	if p.sources == nil {
		p.sources = map[string]string{}
	}
	// Lines before the first Host apply to every host.
	cur := &block{patterns: []string{"*"}, options: map[string]string{}, source: path}
	p.blocks = append(p.blocks, cur)

	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		key, args := splitLine(sc.Text())
		switch key {
		case "":
		case "host":
			cur = &block{patterns: args, options: map[string]string{}, source: path}
			p.blocks = append(p.blocks, cur)
			for _, pat := range args {
				if strings.ContainsAny(pat, "*?!") {
					continue
				}
				if _, dup := p.sources[strings.ToLower(pat)]; !dup {
					p.sources[strings.ToLower(pat)] = path
					p.aliases = append(p.aliases, pat)
				}
			}
		case "match":
			cur = &block{match: true, options: map[string]string{}, source: path}
			p.blocks = append(p.blocks, cur)
		case "include":
			if cur.match {
				continue
			}
			for _, pat := range args {
				for _, inc := range p.expandInclude(pat) {
					p.parseFile(inc, depth+1)
				}
			}
			// Settings after an Include belong to the same block again.
			cont := &block{patterns: cur.patterns, options: map[string]string{}, source: path}
			p.blocks = append(p.blocks, cont)
			cur = cont
		default:
			if len(args) > 0 {
				if _, set := cur.options[key]; !set {
					cur.options[key] = args[0]
				}
			}
		}
	}
}

// expandInclude resolves an Include argument: ~ is the home folder, and a
// relative path is relative to ~/.ssh (for the user's config).
func (p *parser) expandInclude(pat string) []string {
	switch {
	case pat == "~":
		pat = p.home
	case strings.HasPrefix(pat, "~/"):
		pat = filepath.Join(p.home, pat[2:])
	case !filepath.IsAbs(pat):
		pat = filepath.Join(p.home, ".ssh", pat)
	}
	matches, err := filepath.Glob(pat)
	if err != nil {
		return nil
	}
	sort.Strings(matches)
	return matches
}

// resolve works out a host's settings: the first value for each option,
// from every Host block that matches it, in file order.
func (p *parser) resolve(alias string) Host {
	h := Host{Alias: alias, Source: p.sources[strings.ToLower(alias)]}
	get := func(key string) string {
		for _, b := range p.blocks {
			if !b.match && matches(b.patterns, alias) {
				if v, ok := b.options[key]; ok {
					return v
				}
			}
		}
		return ""
	}
	h.HostName = strings.NewReplacer("%h", alias, "%%", "%").Replace(get("hostname"))
	if h.HostName == "" {
		h.HostName = alias
	}
	h.User = get("user")
	h.Port = get("port")
	if j := get("proxyjump"); !strings.EqualFold(j, "none") {
		h.ProxyJump = j
	}
	return h
}

// splitLine returns a config line's lowercased keyword and its arguments.
// "Key value", "Key=value" and "Key = value" are all allowed; arguments
// may be double-quoted.
func splitLine(line string) (string, []string) {
	line = strings.TrimSpace(line)
	if line == "" || line[0] == '#' {
		return "", nil
	}
	i := strings.IndexAny(line, " \t=")
	if i < 0 {
		return strings.ToLower(line), nil
	}
	key := strings.ToLower(line[:i])
	rest := strings.TrimLeft(line[i:], " \t")
	rest = strings.TrimLeft(strings.TrimPrefix(rest, "="), " \t")

	var args []string
	for rest != "" {
		if rest[0] == '"' {
			end := strings.IndexByte(rest[1:], '"')
			if end < 0 {
				args = append(args, rest[1:])
				break
			}
			args = append(args, rest[1:end+1])
			rest = strings.TrimLeft(rest[end+2:], " \t")
			continue
		}
		j := strings.IndexAny(rest, " \t")
		if j < 0 {
			args = append(args, rest)
			break
		}
		args = append(args, rest[:j])
		rest = strings.TrimLeft(rest[j:], " \t")
	}
	return key, args
}

// matches applies a Host pattern list: some pattern must match and no
// negated (!) one may.
func matches(patterns []string, host string) bool {
	ok := false
	for _, pat := range patterns {
		if neg := strings.HasPrefix(pat, "!"); neg {
			if globMatch(pat[1:], host) {
				return false
			}
		} else if globMatch(pat, host) {
			ok = true
		}
	}
	return ok
}

func globMatch(pattern, s string) bool {
	re := "^" + strings.NewReplacer(`\*`, ".*", `\?`, ".").Replace(regexp.QuoteMeta(pattern)) + "$"
	m, _ := regexp.MatchString("(?i)"+re, s)
	return m
}

// ── known_hosts ──────────────────────────────────────────────────────────

// knownHosts lists the servers in a known_hosts file, most recently added
// first. Hashed names (HashKnownHosts) can't be read back and are skipped,
// as are revoked keys and certificate authorities.
func knownHosts(path string) []Host {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []Host
	seen := map[string]bool{}
	lines := strings.Split(string(data), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		fields := strings.Fields(lines[i])
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") || strings.HasPrefix(fields[0], "@") {
			continue
		}
		for _, name := range strings.Split(fields[0], ",") {
			if name == "" || strings.HasPrefix(name, "|") || strings.ContainsAny(name, "*?!") {
				continue
			}
			host, port := name, ""
			if strings.HasPrefix(name, "[") {
				end := strings.Index(name, "]:")
				if end < 0 {
					continue
				}
				host, port = name[1:end], name[end+2:]
			}
			key := strings.ToLower(host) + ":" + port
			if seen[key] || gitHosts[strings.ToLower(host)] {
				continue
			}
			seen[key] = true
			out = append(out, Host{Alias: host, HostName: host, Port: port, Known: true})
		}
	}
	return out
}

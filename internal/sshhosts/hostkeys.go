package sshhosts

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Server host keys: what known_hosts holds for a host, what the server
// presents now, and forgetting or trusting keys — the fix for "REMOTE HOST
// IDENTIFICATION HAS CHANGED" after a server is rebuilt.
//
// How ssh names a host in known_hosts (HostKeyAlias, HostName, port) and
// which files it checks come from `ssh -G`, so they match what ssh does.
// Lookups and removal use ssh-keygen -F/-R, which also handle hashed names.

// HostKey is one server key.
type HostKey struct {
	Type        string `json:"type"`
	Bits        int    `json:"bits"`
	Fingerprint string `json:"fingerprint"`
	// Where it is stored (known_hosts file and 1-based line); empty for a
	// key just scanned from the server.
	File string `json:"file"`
	Line int    `json:"line"`
	// Marked @revoked or @cert-authority.
	Marker string `json:"marker"`
}

// HostKeys is what's known about one host's keys.
type HostKeys struct {
	// The name known_hosts files it under: "host" or "[host]:port".
	Target string `json:"target"`
	// The address ssh connects to, and the port.
	HostName string `json:"hostName"`
	Port     string `json:"port"`
	// The user's known_hosts files ssh checks (existing or not).
	Files  []string  `json:"files"`
	Stored []HostKey `json:"stored"`
	// A jump host or proxy command the connection goes through. The
	// server can't be scanned directly then.
	Via string `json:"via"`
}

const scanTimeout = 8 * time.Second

// Resolved ssh settings for a host, from `ssh -G`.
type resolvedHost struct {
	hostName, port, alias, via string
	knownFiles                 []string
	hash                       bool
}

func (r resolvedHost) target() string {
	name := r.hostName
	if r.alias != "" {
		name = r.alias
	}
	if r.port != "" && r.port != "22" {
		return "[" + name + "]:" + r.port
	}
	return name
}

// resolveHost asks ssh how it would connect to a host. `ssh -G` reads the
// config and prints the result without connecting.
func (s *Service) resolveHost(alias, port string) (resolvedHost, error) {
	if alias == "" || strings.HasPrefix(alias, "-") || strings.ContainsAny(alias, " \t\r\n") {
		return resolvedHost{}, fmt.Errorf("%q isn't a host name", alias)
	}
	args := []string{"-G"}
	if _, err := os.Stat(s.mainConfig()); err == nil {
		args = append(args, "-F", s.mainConfig())
	} else {
		args = append(args, "-F", "/dev/null")
	}
	if port != "" {
		if _, err := strconv.Atoi(port); err != nil {
			return resolvedHost{}, fmt.Errorf("bad port %q", port)
		}
		args = append(args, "-p", port)
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, sshTool("ssh"), append(args, "--", alias)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := firstLine(stderr.String()); msg != "" {
			return resolvedHost{}, errors.New(msg)
		}
		return resolvedHost{}, err
	}
	var r resolvedHost
	for _, line := range strings.Split(string(out), "\n") {
		key, value, _ := strings.Cut(line, " ")
		switch key {
		case "hostname":
			r.hostName = value
		case "port":
			r.port = value
		case "hostkeyalias":
			r.alias = value
		case "hashknownhosts":
			r.hash = value == "yes"
		case "proxyjump":
			if value != "none" {
				r.via = value
			}
		case "proxycommand":
			if value != "none" && r.via == "" {
				r.via = value
			}
		case "userknownhostsfile":
			for _, f := range strings.Fields(value) {
				if !strings.Contains(f, "%") && f != "/dev/null" {
					r.knownFiles = append(r.knownFiles, s.expandPath(f))
				}
			}
		}
	}
	if r.hostName == "" {
		return r, fmt.Errorf("ssh couldn't resolve %s", alias)
	}
	return r, nil
}

// HostKeys lists the keys known_hosts holds for a host. port is for known
// hosts listed by address; config hosts pass "".
func (s *Service) HostKeys(alias, port string) (HostKeys, error) {
	r, err := s.resolveHost(alias, port)
	if err != nil {
		return HostKeys{}, err
	}
	out := HostKeys{Target: r.target(), HostName: r.hostName, Port: r.port, Files: r.knownFiles, Stored: []HostKey{}, Via: r.via}
	for _, f := range r.knownFiles {
		keys, err := findKnown(f, out.Target)
		if err != nil {
			return out, err
		}
		out.Stored = append(out.Stored, keys...)
	}
	return out, nil
}

// findKnown looks a name up in one known_hosts file (ssh-keygen -F).
func findKnown(file, target string) ([]HostKey, error) {
	if _, err := os.Stat(file); err != nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, sshTool("ssh-keygen"), "-F", target, "-f", file).Output()
	if err != nil {
		// Exit status 1: not found.
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}
	var keys []HostKey
	line := 0
	for _, l := range strings.Split(string(out), "\n") {
		if n, ok := strings.CutPrefix(l, "# Host "); ok {
			if i := strings.LastIndex(n, "found: line "); i >= 0 {
				line, _ = strconv.Atoi(strings.TrimSpace(n[i+len("found: line "):]))
			}
			continue
		}
		if k, ok := parseKnownLine(l); ok {
			k.File, k.Line = file, line
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// parseKnownLine reads "[@marker] names type base64 [comment]".
func parseKnownLine(line string) (HostKey, bool) {
	fields := strings.Fields(line)
	var k HostKey
	if len(fields) > 0 && strings.HasPrefix(fields[0], "@") {
		k.Marker = strings.TrimPrefix(fields[0], "@")
		fields = fields[1:]
	}
	if len(fields) < 3 {
		return k, false
	}
	blob, _ := parseAuthorizedKey(strings.Join(fields[1:], " "))
	if blob == nil {
		return k, false
	}
	k.Type, k.Bits = keyInfo(blob)
	k.Fingerprint = fingerprint(blob)
	return k, true
}

// ScanHostKeys asks the server for its keys now (ssh-keyscan). It can't
// go through a jump host.
func (s *Service) ScanHostKeys(alias, port string) ([]HostKey, error) {
	r, err := s.resolveHost(alias, port)
	if err != nil {
		return nil, err
	}
	lines, err := scan(r)
	if err != nil {
		return nil, err
	}
	keys := []HostKey{}
	for _, l := range lines {
		if k, ok := parseKnownLine(l); ok {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// scan runs ssh-keyscan and returns its known_hosts lines, filed under the
// name ssh looks for (HostKeyAlias included).
func scan(r resolvedHost) ([]string, error) {
	if r.via != "" {
		return nil, fmt.Errorf("%s is reached through %s, so it can't be scanned directly — connect, and ssh will show its key", r.hostName, r.via)
	}
	out, stderr, err := keyscan(r.hostName, r.port)
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		fields := strings.Fields(l)
		if len(fields) < 3 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		// Refile under the name ssh will look up.
		lines = append(lines, r.target()+" "+strings.Join(fields[1:], " "))
	}
	if len(lines) == 0 {
		msg := ""
		for _, l := range strings.Split(stderr, "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
				msg = l
			}
		}
		switch {
		case msg != "":
			return nil, fmt.Errorf("%s didn't answer: %s", r.hostName, msg)
		case err != nil:
			return nil, fmt.Errorf("%s didn't answer: %v", r.hostName, err)
		}
		return nil, fmt.Errorf("%s didn't answer on port %s", r.hostName, r.port)
	}
	return lines, nil
}

// keyscan runs ssh-keyscan; tests replace it.
var keyscan = func(host, port string) (stdout, stderr string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), scanTimeout+2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, sshTool("ssh-keyscan"), "-T", strconv.Itoa(int(scanTimeout/time.Second)), "-p", port, "--", host)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	out, err := cmd.Output()
	return string(out), errBuf.String(), err
}

// ForgetHostKeys removes a host's entries from the user's known_hosts
// files (ssh-keygen -R, which keeps the old file as known_hosts.old).
func (s *Service) ForgetHostKeys(alias, port string) error {
	r, err := s.resolveHost(alias, port)
	if err != nil {
		return err
	}
	return forget(r)
}

func forget(r resolvedHost) error {
	for _, f := range r.knownFiles {
		if _, err := os.Stat(f); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
		out, err := exec.CommandContext(ctx, sshTool("ssh-keygen"), "-R", r.target(), "-f", f).CombinedOutput()
		cancel()
		if err != nil {
			return fmt.Errorf("removing %s from %s: %s", r.target(), f, firstLine(string(out)))
		}
	}
	return nil
}

// TrustHostKeys stores the server's current keys in known_hosts — those
// whose fingerprints the user saw — replacing any it had. The server is
// scanned again so that only keys matching what was shown get stored.
func (s *Service) TrustHostKeys(alias, port string, fingerprints []string) error {
	r, err := s.resolveHost(alias, port)
	if err != nil {
		return err
	}
	if len(r.knownFiles) == 0 {
		return errors.New("ssh is set to use no known_hosts file")
	}
	lines, err := scan(r)
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, f := range fingerprints {
		want[f] = true
	}
	var keep []string
	for _, l := range lines {
		if k, ok := parseKnownLine(l); ok && want[k.Fingerprint] {
			if r.hash {
				// Respect HashKnownHosts, like ssh does when it adds a key.
				_, rest, _ := strings.Cut(l, " ")
				l = hashName(r.target()) + " " + rest
			}
			keep = append(keep, l)
			delete(want, k.Fingerprint)
		}
	}
	if len(keep) == 0 || len(want) > 0 {
		return errors.New("the server's keys changed since they were shown — scan again")
	}
	if err := forget(r); err != nil {
		return err
	}
	file := r.knownFiles[0]
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	// Start on a fresh line if the file doesn't end with one.
	prefix := ""
	if fi, err := f.Stat(); err == nil && fi.Size() > 0 {
		last := make([]byte, 1)
		if rf, err := os.Open(file); err == nil {
			if _, err := rf.ReadAt(last, fi.Size()-1); err == nil && last[0] != '\n' {
				prefix = "\n"
			}
			rf.Close()
		}
	}
	_, werr := f.WriteString(prefix + strings.Join(keep, "\n") + "\n")
	return errors.Join(werr, f.Close())
}

// hashName hides a host name the way HashKnownHosts does:
// |1|base64(salt)|base64(HMAC-SHA1(salt, name)).
func hashName(name string) string {
	salt := make([]byte, sha1.Size)
	_, _ = rand.Read(salt)
	mac := hmac.New(sha1.New, salt)
	mac.Write([]byte(name))
	return "|1|" + base64.StdEncoding.EncodeToString(salt) + "|" + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

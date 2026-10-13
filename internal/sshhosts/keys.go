package sshhosts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The Keys section: key pairs in ~/.ssh and the keys loaded in ssh-agent.
//
// Everything here is read without a passphrase. Public keys come from the
// .pub file or, for OpenSSH-format private keys, from the private key file
// itself (its public half is stored in the clear). Things that may ask for
// a passphrase or password — loading a key into the agent, changing its
// passphrase, installing it on a server — run as real commands in a
// terminal tab (see the frontend), so they land in the shell history and
// the secret never passes through wtf. The exception is the Generate tab's
// key form (keygen.go), which takes a new key's passphrase to encrypt it.

// Key is a key pair in ~/.ssh, or a key only the agent holds.
type Key struct {
	// The private key file; "" for a key that is only in the agent.
	Path string `json:"path"`
	// ~/.ssh/… for files, the key's comment for agent-only keys.
	Name        string `json:"name"`
	Type        string `json:"type"` // ED25519, RSA, ECDSA, ED25519-SK…
	Bits        int    `json:"bits"`
	Fingerprint string `json:"fingerprint"` // SHA256:…
	Comment     string `json:"comment"`
	// Protected by a passphrase. Unknown (false) for formats that don't say.
	Encrypted bool `json:"encrypted"`
	// Loaded in ssh-agent.
	InAgent bool `json:"inAgent"`
	// The private key file can be read by other users, so ssh refuses it:
	// its permissions, e.g. "0644".
	Unprotected string `json:"unprotected"`
	// Has a .pub file next to it.
	HasPub bool `json:"hasPub"`
	// Hosts whose IdentityFile is this key.
	UsedBy []string `json:"usedBy"`
	// When the private key file was last written (RFC 3339): when it was
	// made, unless its passphrase was changed since. "" for agent-only keys.
	Modified string `json:"modified"`
}

type KeyList struct {
	Keys []Key `json:"keys"`
	// Why the agent couldn't be asked, e.g. none is running; "" when fine.
	AgentError string `json:"agentError"`
	// The agent's socket (SSH_AUTH_SOCK).
	AgentSocket string   `json:"agentSocket"`
	Problems    []string `json:"problems"`
}

const (
	agentTimeout = 5 * time.Second
	maxKeyFile   = 64 << 10
)

// KeyList reads the key pairs in ~/.ssh and asks the agent what it holds.
func (s *Service) KeyList() KeyList {
	out := KeyList{Keys: []Key{}, AgentSocket: os.Getenv("SSH_AUTH_SOCK")}
	byFP := map[string]int{}

	des, err := os.ReadDir(s.sshDir())
	if err != nil && !os.IsNotExist(err) {
		out.Problems = append(out.Problems, err.Error())
	}
	for _, de := range des {
		if !de.Type().IsRegular() || strings.HasSuffix(de.Name(), ".pub") {
			continue
		}
		k, ok := s.readKey(filepath.Join(s.sshDir(), de.Name()))
		if !ok {
			continue
		}
		byFP[k.Fingerprint] = len(out.Keys)
		out.Keys = append(out.Keys, k)
	}

	agent, err := agentKeys()
	if err != nil {
		out.AgentError = err.Error()
	}
	for _, a := range agent {
		if i, ok := byFP[a.Fingerprint]; ok {
			out.Keys[i].InAgent = true
			continue
		}
		a.InAgent = true
		a.Name = a.Comment
		if a.Name == "" {
			a.Name = a.Fingerprint
		}
		out.Keys = append(out.Keys, a)
	}

	s.markUsage(out.Keys)
	return out
}

// readKey reads a private key file and its public half. ok is false for
// files that aren't private keys (config, known_hosts…).
func (s *Service) readKey(path string) (Key, bool) {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() > maxKeyFile {
		return Key{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Key{}, false
	}
	first, _, _ := bytes.Cut(data, []byte("\n"))
	if !bytes.HasPrefix(first, []byte("-----BEGIN ")) || !bytes.Contains(first, []byte("PRIVATE KEY")) {
		return Key{}, false
	}
	k := Key{Path: path, Name: prettyHome(s.home, path), UsedBy: []string{}, Modified: fi.ModTime().UTC().Format(time.RFC3339)}
	if mode := fi.Mode().Perm(); mode&0o077 != 0 {
		k.Unprotected = fmt.Sprintf("%04o", mode)
	}

	var blob []byte
	if pub, err := os.ReadFile(path + ".pub"); err == nil {
		k.HasPub = true
		blob, k.Comment = parseAuthorizedKey(string(pub))
	}
	if bytes.Contains(first, []byte("OPENSSH PRIVATE KEY")) {
		inner, enc, comment := parseOpenSSHPrivate(data)
		k.Encrypted = enc
		if blob == nil {
			blob = inner
		}
		if k.Comment == "" {
			k.Comment = comment
		}
	} else {
		// PEM: an encrypted key says so in its headers.
		k.Encrypted = bytes.Contains(data, []byte("ENCRYPTED"))
	}
	if blob == nil {
		k.Type = "unknown"
		return k, true
	}
	k.Type, k.Bits = keyInfo(blob)
	k.Fingerprint = fingerprint(blob)
	return k, true
}

// markUsage fills in which configured hosts use each key file.
func (s *Service) markUsage(keys []Key) {
	p := parser{home: s.home, seen: map[string]bool{}, contents: map[string][]byte{}}
	p.parseFile(s.mainConfig(), 0)
	byPath := map[string]*Key{}
	for i := range keys {
		if keys[i].Path != "" {
			byPath[keys[i].Path] = &keys[i]
		}
	}
	for _, alias := range p.aliases {
		for _, f := range p.identities(alias) {
			if k := byPath[s.expandPath(f)]; k != nil {
				k.UsedBy = append(k.UsedBy, alias)
			}
		}
	}
}

// expandPath expands ~ and %d (the home folder) in a config path; relative
// paths are relative to the home folder, as for ssh.
func (s *Service) expandPath(p string) string {
	p = strings.Trim(p, `"`)
	p = strings.ReplaceAll(p, "%d", s.home)
	switch {
	case p == "~":
		return s.home
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(s.home, p[2:])
	case !filepath.IsAbs(p):
		return filepath.Join(s.home, p)
	}
	return filepath.Clean(p)
}

// PublicKey returns a key's public half as one authorized_keys line.
func (s *Service) PublicKey(path string) (string, error) {
	path, err := s.keyPath(path)
	if err != nil {
		return "", err
	}
	if pub, err := os.ReadFile(path + ".pub"); err == nil {
		return strings.TrimSpace(string(pub)), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	blob, _, comment := parseOpenSSHPrivate(data)
	if blob == nil {
		return "", errors.New("this key has no .pub file, and its public half can't be read without its passphrase")
	}
	typ, _ := readString(blob)
	line := string(typ) + " " + base64.StdEncoding.EncodeToString(blob)
	if comment != "" {
		line += " " + comment
	}
	return line, nil
}

// WritePublicKey creates the missing .pub file for a key, as ssh-copy-id
// and some tools need one.
func (s *Service) WritePublicKey(path string) error {
	line, err := s.PublicKey(path)
	if err != nil {
		return err
	}
	path, _ = s.keyPath(path)
	if _, err := os.Stat(path + ".pub"); err == nil {
		return nil
	}
	return os.WriteFile(path+".pub", []byte(line+"\n"), 0o644)
}

// ProtectKey makes a private key readable by its owner only (chmod 600),
// which ssh requires before it will use the key.
func (s *Service) ProtectKey(path string) error {
	path, err := s.keyPath(path)
	if err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// DeleteKey moves a key pair out of ~/.ssh into wtf's cache folder
// (ssh-backups/keys), where it can still be recovered.
func (s *Service) DeleteKey(path string) error {
	path, err := s.keyPath(path)
	if err != nil {
		return err
	}
	if s.backupDir == "" {
		return errors.New("no cache folder to move the key to")
	}
	dir := filepath.Join(s.backupDir, "keys", time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := moveFile(path, filepath.Join(dir, filepath.Base(path))); err != nil {
		return err
	}
	if _, err := os.Stat(path + ".pub"); err == nil {
		return moveFile(path+".pub", filepath.Join(dir, filepath.Base(path)+".pub"))
	}
	return nil
}

// KeyBackupDir is where deleted keys go.
func (s *Service) KeyBackupDir() string {
	if s.backupDir == "" {
		return ""
	}
	return filepath.Join(s.backupDir, "keys")
}

func moveFile(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	// Another file system: copy, then remove.
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	if err := os.WriteFile(to, data, 0o600); err != nil {
		return err
	}
	return os.Remove(from)
}

// keyPath checks that a path is a file directly in ~/.ssh.
func (s *Service) keyPath(path string) (string, error) {
	path = filepath.Clean(path)
	if filepath.Dir(path) != s.sshDir() {
		return "", fmt.Errorf("%s isn't a key in ~/.ssh", path)
	}
	return path, nil
}

// KeyNameFree says whether ~/.ssh/<name> (and .pub) can be created.
func (s *Service) KeyNameFree(name string) (bool, error) {
	if name == "" || strings.ContainsAny(name, "/\\") || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "-") {
		return false, errors.New("use a plain file name, like id_ed25519_work")
	}
	for _, p := range []string{name, name + ".pub"} {
		if _, err := os.Lstat(filepath.Join(s.sshDir(), p)); err == nil {
			return false, nil
		}
	}
	return true, nil
}

// ── ssh-agent ────────────────────────────────────────────────────────────

// agentKeys lists the agent's keys (ssh-add -L).
func agentKeys() ([]Key, error) {
	out, err := runAgent("-L")
	if err != nil {
		if strings.Contains(out, "no identities") {
			return nil, nil
		}
		return nil, agentError(out, err)
	}
	var keys []Key
	for _, line := range strings.Split(out, "\n") {
		blob, comment := parseAuthorizedKey(line)
		if blob == nil {
			continue
		}
		typ, bits := keyInfo(blob)
		keys = append(keys, Key{Type: typ, Bits: bits, Fingerprint: fingerprint(blob), Comment: comment, UsedBy: []string{}})
	}
	return keys, nil
}

// AgentRemove unloads one key from the agent, by fingerprint.
func (s *Service) AgentRemove(fp string) error {
	out, err := runAgent("-L")
	if err != nil {
		return agentError(out, err)
	}
	for _, line := range strings.Split(out, "\n") {
		blob, _ := parseAuthorizedKey(line)
		if blob == nil || fingerprint(blob) != fp {
			continue
		}
		// ssh-add -d takes a public key file; write the agent's own copy.
		f, err := os.CreateTemp("", "wtf-agent-*.pub")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		_, werr := f.WriteString(line + "\n")
		if cerr := f.Close(); werr != nil || cerr != nil {
			return errors.Join(werr, cerr)
		}
		if out, err := runAgent("-d", f.Name()); err != nil {
			return agentError(out, err)
		}
		return nil
	}
	return errors.New("the agent no longer holds that key")
}

// AgentRemoveAll unloads every key from the agent (ssh-add -D).
func (s *Service) AgentRemoveAll() error {
	if out, err := runAgent("-D"); err != nil {
		return agentError(out, err)
	}
	return nil
}

func runAgent(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), agentTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, sshTool("ssh-add"), args...)
	cmd.Stdin = nil // never prompt
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func agentError(out string, err error) error {
	switch {
	case strings.Contains(out, "Could not open a connection") || strings.Contains(out, "Error connecting to agent"):
		return errors.New("no ssh-agent is running")
	case out != "":
		return errors.New(firstLine(out))
	}
	return err
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// sshTool finds an OpenSSH program: on PATH, else in /usr/bin (a GUI app's
// PATH can be minimal).
func sshTool(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return "/usr/bin/" + name
}

// ── Key formats ──────────────────────────────────────────────────────────

// parseAuthorizedKey reads "type base64 [comment]" (a .pub file or a line
// of ssh-add -L) and returns the key blob and comment.
func parseAuthorizedKey(line string) ([]byte, string) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 2 {
		return nil, ""
	}
	blob, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil {
		return nil, ""
	}
	if typ, _ := readString(blob); string(typ) != fields[0] {
		return nil, ""
	}
	return blob, strings.Join(fields[2:], " ")
}

// parseOpenSSHPrivate reads the clear parts of an OpenSSH private key: the
// public key, whether a passphrase protects it, and — when it doesn't —
// the comment stored with it.
func parseOpenSSHPrivate(data []byte) (blob []byte, encrypted bool, comment string) {
	text := string(data)
	start := strings.Index(text, "-----BEGIN OPENSSH PRIVATE KEY-----")
	end := strings.Index(text, "-----END OPENSSH PRIVATE KEY-----")
	if start < 0 || end < start {
		return nil, false, ""
	}
	b64 := strings.Join(strings.Fields(text[start+35:end]), "")
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, false, ""
	}
	const magic = "openssh-key-v1\x00"
	if !strings.HasPrefix(string(raw), magic) {
		return nil, false, ""
	}
	rest := raw[len(magic):]
	cipher, rest := readString(rest)
	_, rest = readString(rest) // kdf name
	_, rest = readString(rest) // kdf options
	if len(rest) < 4 || binary.BigEndian.Uint32(rest) != 1 {
		return nil, false, ""
	}
	blob, rest = readString(rest[4:])
	encrypted = string(cipher) != "none"
	if encrypted || blob == nil {
		return blob, encrypted, ""
	}
	// Unencrypted: check ints, then the key, which ends with its comment.
	priv, _ := readString(rest)
	if len(priv) < 8 {
		return blob, false, ""
	}
	priv = priv[8:]
	typ, priv := readString(priv)
	// Fields before the comment. Security-key (sk-) formats contain a bare
	// flags byte, so their comment isn't read here; their .pub has it.
	fields := map[string]int{"ssh-ed25519": 2, "ssh-rsa": 6}
	n, ok := fields[string(typ)]
	if strings.HasPrefix(string(typ), "ecdsa-sha2-") {
		n, ok = 3, true
	}
	if !ok {
		return blob, false, ""
	}
	for i := 0; i < n && priv != nil; i++ {
		_, priv = readString(priv)
	}
	c, _ := readString(priv)
	return blob, false, string(c)
}

// readString reads one SSH wire-format string; nil when malformed.
func readString(b []byte) ([]byte, []byte) {
	if len(b) < 4 {
		return nil, nil
	}
	n := binary.BigEndian.Uint32(b)
	if uint64(n) > uint64(len(b)-4) {
		return nil, nil
	}
	return b[4 : 4+n], b[4+n:]
}

// fingerprint is the SHA256 fingerprint ssh shows for a key blob.
func fingerprint(blob []byte) string {
	sum := sha256.Sum256(blob)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// keyInfo names a key's algorithm and size, as ssh-keygen -l does.
func keyInfo(blob []byte) (string, int) {
	typ, rest := readString(blob)
	t := string(typ)
	t = strings.TrimSuffix(t, "-cert-v01@openssh.com")
	switch {
	case t == "ssh-ed25519":
		return "ED25519", 256
	case t == "sk-ssh-ed25519@openssh.com":
		return "ED25519-SK", 256
	case t == "ssh-rsa":
		_, rest = readString(rest) // e
		n, _ := readString(rest)
		return "RSA", new(big.Int).SetBytes(n).BitLen()
	case strings.HasPrefix(t, "ecdsa-sha2-nistp"):
		return "ECDSA", curveBits(strings.TrimPrefix(t, "ecdsa-sha2-nistp"))
	case strings.HasPrefix(t, "sk-ecdsa-sha2-nistp"):
		return "ECDSA-SK", curveBits(strings.TrimSuffix(strings.TrimPrefix(t, "sk-ecdsa-sha2-nistp"), "@openssh.com"))
	case t == "ssh-dss":
		return "DSA", 1024
	}
	return strings.ToUpper(t), 0
}

func curveBits(s string) int {
	switch s {
	case "256":
		return 256
	case "384":
		return 384
	case "521":
		return 521
	}
	return 0
}

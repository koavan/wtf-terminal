package sshhosts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// keygen makes a key with the real ssh-keygen, so the parsing is checked
// against what OpenSSH writes.
func keygen(t *testing.T, path string, args ...string) {
	t.Helper()
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not installed")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("ssh-keygen", append([]string{"-q", "-f", path}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v: %s", err, out)
	}
}

// sshFingerprint asks ssh-keygen for a key's fingerprint.
func sshFingerprint(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command("ssh-keygen", "-l", "-f", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(out))[1]
}

func findKey(l KeyList, name string) *Key {
	for i := range l.Keys {
		if l.Keys[i].Name == name {
			return &l.Keys[i]
		}
	}
	return nil
}

func TestKeyList(t *testing.T) {
	home := t.TempDir()
	ssh := filepath.Join(home, ".ssh")
	keygen(t, filepath.Join(ssh, "id_ed25519"), "-t", "ed25519", "-N", "", "-C", "me@laptop")
	keygen(t, filepath.Join(ssh, "locked"), "-t", "ed25519", "-N", "secret phrase", "-C", "locked key")
	keygen(t, filepath.Join(ssh, "id_rsa"), "-t", "rsa", "-b", "3072", "-N", "", "-C", "rsa")
	keygen(t, filepath.Join(ssh, "ecdsa"), "-t", "ecdsa", "-b", "384", "-N", "")
	// A key without its .pub: still listed, read from the private file.
	keygen(t, filepath.Join(ssh, "nopub"), "-t", "ed25519", "-N", "", "-C", "inside")
	nopubFP := sshFingerprint(t, filepath.Join(ssh, "nopub.pub"))
	os.Remove(filepath.Join(ssh, "nopub.pub"))
	os.Chmod(filepath.Join(ssh, "id_rsa"), 0o644)
	write(t, filepath.Join(ssh, "config"), "Host web\n  IdentityFile ~/.ssh/id_rsa\nHost *\n  IdentityFile %d/.ssh/id_ed25519\n")
	write(t, filepath.Join(ssh, "known_hosts"), "web ssh-ed25519 AAAA\n")
	t.Setenv("SSH_AUTH_SOCK", filepath.Join(home, "no-agent"))

	l := (&Service{home: home}).KeyList()
	if len(l.Keys) != 5 {
		t.Fatalf("want 5 keys, got %+v", l.Keys)
	}
	if l.AgentError == "" {
		t.Error("an unreachable agent should be reported")
	}

	ed := findKey(l, "~/.ssh/id_ed25519")
	if ed.Type != "ED25519" || ed.Bits != 256 || ed.Comment != "me@laptop" || ed.Encrypted || !ed.HasPub {
		t.Errorf("ed25519: %+v", ed)
	}
	if ed.Fingerprint != sshFingerprint(t, ed.Path+".pub") {
		t.Errorf("fingerprint %s differs from ssh-keygen's", ed.Fingerprint)
	}
	if strings.Join(ed.UsedBy, ",") != "web" {
		t.Errorf("ed25519 used by %v, want web (via Host *)", ed.UsedBy)
	}

	if k := findKey(l, "~/.ssh/locked"); !k.Encrypted || k.Comment != "locked key" {
		t.Errorf("locked: %+v", k)
	}
	rsa := findKey(l, "~/.ssh/id_rsa")
	if rsa.Type != "RSA" || rsa.Bits != 3072 || rsa.Unprotected != "0644" || rsa.Fingerprint != sshFingerprint(t, rsa.Path+".pub") {
		t.Errorf("rsa: %+v", rsa)
	}
	if strings.Join(rsa.UsedBy, ",") != "web" {
		t.Errorf("rsa used by %v", rsa.UsedBy)
	}
	if k := findKey(l, "~/.ssh/ecdsa"); k.Type != "ECDSA" || k.Bits != 384 {
		t.Errorf("ecdsa: %+v", k)
	}
	np := findKey(l, "~/.ssh/nopub")
	if np.HasPub || np.Fingerprint != nopubFP || np.Comment != "inside" {
		t.Errorf("nopub: %+v (want %s)", np, nopubFP)
	}
	if findKey(l, "~/.ssh/config") != nil || findKey(l, "~/.ssh/known_hosts") != nil {
		t.Error("non-key files must not be listed")
	}
}

func TestPublicKeyAndActions(t *testing.T) {
	home := t.TempDir()
	ssh := filepath.Join(home, ".ssh")
	keygen(t, filepath.Join(ssh, "k"), "-t", "ed25519", "-N", "", "-C", "c")
	want, _ := os.ReadFile(filepath.Join(ssh, "k.pub"))
	os.Remove(filepath.Join(ssh, "k.pub"))
	s := &Service{home: home, backupDir: filepath.Join(home, "cache")}

	got, err := s.PublicKey(filepath.Join(ssh, "k"))
	if err != nil || got != strings.TrimSpace(string(want)) {
		t.Fatalf("PublicKey = %q, %v; want %q", got, err, want)
	}
	if err := s.WritePublicKey(filepath.Join(ssh, "k")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(ssh, "k.pub")); string(b) != string(want) {
		t.Errorf("written .pub = %q", b)
	}

	os.Chmod(filepath.Join(ssh, "k"), 0o644)
	if err := s.ProtectKey(filepath.Join(ssh, "k")); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(ssh, "k")); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}

	if _, err := s.PublicKey(filepath.Join(home, "elsewhere")); err == nil {
		t.Error("paths outside ~/.ssh must be refused")
	}
	if _, err := s.PublicKey(filepath.Join(ssh, "..", "x")); err == nil {
		t.Error("paths escaping ~/.ssh must be refused")
	}

	if free, _ := s.KeyNameFree("k"); free {
		t.Error("k exists")
	}
	if free, err := s.KeyNameFree("new_key"); !free || err != nil {
		t.Errorf("new_key: %v %v", free, err)
	}
	if _, err := s.KeyNameFree("../evil"); err == nil {
		t.Error("names with slashes must be refused")
	}

	if err := s.DeleteKey(filepath.Join(ssh, "k")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ssh, "k")); !os.IsNotExist(err) {
		t.Error("key should be gone from ~/.ssh")
	}
	moved, _ := filepath.Glob(filepath.Join(s.KeyBackupDir(), "*", "k*"))
	if len(moved) != 2 {
		t.Errorf("backup has %v", moved)
	}
}

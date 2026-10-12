package sshhosts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestGenerateKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SSH_AUTH_SOCK", filepath.Join(home, "no-agent"))
	s := &Service{home: home}

	for _, typ := range []string{"ed25519", "ecdsa", "rsa"} {
		pass := ""
		if typ == "ecdsa" {
			pass = "correct horse"
		}
		res, err := s.GenerateKey(KeyGenRequest{Type: typ, Name: "id_" + typ, Comment: "ada@laptop", Passphrase: pass, AddToAgent: true})
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		if res.Path != filepath.Join(home, ".ssh", "id_"+typ) || res.InAgent || res.AgentError == "" {
			t.Errorf("%s: %+v", typ, res)
		}
		if fi, _ := os.Stat(res.Path); fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: private key mode %v", typ, fi.Mode().Perm())
		}
		if fi, _ := os.Stat(filepath.Join(home, ".ssh")); fi.Mode().Perm() != 0o700 {
			t.Errorf("~/.ssh mode %v", fi.Mode().Perm())
		}
		pubFile, _ := os.ReadFile(res.Path + ".pub")
		if strings.TrimSpace(string(pubFile)) != res.PublicKey || !strings.HasSuffix(res.PublicKey, " ada@laptop") {
			t.Errorf("%s: pub %q vs %q", typ, pubFile, res.PublicKey)
		}

		// The private key reads back, and only with its passphrase.
		data, _ := os.ReadFile(res.Path)
		var signer ssh.Signer
		if pass == "" {
			signer, err = ssh.ParsePrivateKey(data)
		} else {
			if _, err := ssh.ParsePrivateKey(data); err == nil {
				t.Errorf("%s: encrypted key parsed without a passphrase", typ)
			}
			signer, err = ssh.ParsePrivateKeyWithPassphrase(data, []byte(pass))
		}
		if err != nil {
			t.Fatalf("%s: parse: %v", typ, err)
		}
		if ssh.FingerprintSHA256(signer.PublicKey()) != res.Fingerprint {
			t.Errorf("%s: fingerprint mismatch", typ)
		}
		// The Keys list sees it.
		found := false
		for _, k := range s.KeyList().Keys {
			found = found || (k.Path == res.Path && k.Fingerprint == res.Fingerprint && k.Encrypted == (pass != ""))
		}
		if !found {
			t.Errorf("%s: not in KeyList", typ)
		}
	}

	// Never overwrites, refuses bad input.
	before, _ := os.ReadFile(filepath.Join(home, ".ssh", "id_ed25519"))
	for _, bad := range []KeyGenRequest{
		{Type: "ed25519", Name: "id_ed25519"},
		{Type: "ed25519", Name: "../evil"},
		{Type: "ed25519", Name: "x", Passphrase: "abc"},
		{Type: "ed25519", Name: "x", Comment: "a\nb"},
		{Type: "dsa", Name: "x"},
	} {
		if _, err := s.GenerateKey(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	after, _ := os.ReadFile(filepath.Join(home, ".ssh", "id_ed25519"))
	if string(before) != string(after) {
		t.Error("an existing key changed")
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "x")); err == nil {
		t.Error("a refused request left a file")
	}
}

// OpenSSH reads what GenerateKey writes, and only with the passphrase.
func TestGeneratedKeysWorkWithSSHKeygen(t *testing.T) {
	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("no ssh-keygen")
	}
	home := t.TempDir()
	s := &Service{home: home}
	for _, c := range []struct{ typ, pass string }{{"ed25519", "hunter22"}, {"rsa", ""}, {"ecdsa", "pass word"}} {
		res, err := s.GenerateKey(KeyGenRequest{Type: c.typ, Name: "k_" + c.typ, Comment: "c", Passphrase: c.pass})
		if err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(keygen, "-y", "-P", c.pass, "-f", res.Path).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: ssh-keygen: %v %s", c.typ, err, out)
		}
		if !strings.HasPrefix(res.PublicKey, strings.TrimSpace(string(out))) {
			t.Errorf("%s: %q vs %q", c.typ, out, res.PublicKey)
		}
		if c.pass != "" {
			if err := exec.Command(keygen, "-y", "-P", "wrong", "-f", res.Path).Run(); err == nil {
				t.Errorf("%s: wrong passphrase accepted", c.typ)
			}
		}
	}
}
